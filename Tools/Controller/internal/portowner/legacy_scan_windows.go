//go:build windows

package portowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	legacyObjectNameInformation = 1
	legacyMaxHandleBuffer       = 64 * 1024 * 1024
	legacyMaxHandleCount        = 1_000_000
	legacyMaxCandidates         = 100_000
	legacyMaxHandlesPerProcess  = 4_096
	legacyMaxObjectNameBuffer   = 64 * 1024
)

var (
	legacyNTDLL         = windows.NewLazySystemDLL("ntdll.dll")
	legacyNTQueryObject = legacyNTDLL.NewProc("NtQueryObject")
)

type legacySystemHandleHeader struct {
	Count    uintptr
	Reserved uintptr
}

type legacySystemHandleEntry struct {
	Object                uintptr
	PID                   uintptr
	Handle                uintptr
	GrantedAccess         uint32
	CreatorBackTraceIndex uint16
	ObjectTypeIndex       uint16
	HandleAttributes      uint32
	Reserved              uint32
}

// scanLegacyNativeOwner runs only in the disposable canonical helper process.
// The parent enforces its lifetime, so a driver-blocked object-name query cannot
// strand a worker or poison later diagnostics in the long-running controller.
func scanLegacyNativeOwner(ctx context.Context, port string) (Owner, bool, error) {
	if !isCOMPort(port) {
		return Owner{}, false, errors.New("native serial-owner scan requires an exact COM number")
	}
	targets, err := queryDeviceTargets(port)
	if err != nil {
		return Owner{}, false, err
	}
	handles, fileType, err := legacyFileHandles(ctx)
	if err != nil {
		return Owner{}, false, err
	}
	names := legacyProcessNames(ctx)
	byProcess := make(map[uint32][]legacySystemHandleEntry)
	candidates := 0
	for _, entry := range handles {
		if err := ctx.Err(); err != nil {
			return Owner{}, false, err
		}
		if entry.ObjectTypeIndex != fileType || entry.PID == 0 || entry.PID > uintptr(^uint32(0)) {
			continue
		}
		pid := uint32(entry.PID)
		if len(byProcess[pid]) >= legacyMaxHandlesPerProcess {
			continue
		}
		if candidates >= legacyMaxCandidates {
			return Owner{}, false, fmt.Errorf(
				"NT file-handle candidates exceeded safety limit %d",
				legacyMaxCandidates,
			)
		}
		byProcess[pid] = append(byProcess[pid], entry)
		candidates++
	}
	preferredName := ""
	if executable, executableErr := os.Executable(); executableErr == nil {
		preferredName = filepath.Base(executable)
	}
	for _, pid := range legacyCandidateProcessIDs(byProcess, names, preferredName) {
		if err := ctx.Err(); err != nil {
			return Owner{}, false, err
		}
		process, openErr := windows.OpenProcess(
			windows.PROCESS_DUP_HANDLE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
			false,
			pid,
		)
		if openErr != nil {
			continue
		}
		matched := false
		for _, entry := range byProcess[pid] {
			var duplicate windows.Handle
			duplicateErr := windows.DuplicateHandle(
				process,
				windows.Handle(entry.Handle),
				windows.CurrentProcess(),
				&duplicate,
				0,
				false,
				windows.DUPLICATE_SAME_ACCESS,
			)
			if duplicateErr != nil {
				continue
			}
			if !legacySerialObjectNameCandidate(duplicate) {
				windows.CloseHandle(duplicate)
				continue
			}
			name, nameErr := legacyQueryObjectName(ctx, duplicate)
			windows.CloseHandle(duplicate)
			if nameErr == nil && legacyMatchesDeviceTarget(name, targets) {
				matched = true
				break
			}
		}
		if !matched {
			windows.CloseHandle(process)
			continue
		}
		owner := Owner{PID: pid, Name: names[pid]}
		owner.Executable = queryProcessPath(process)
		owner.ProcessStartTime, _ = queryProcessStartTime(process)
		windows.CloseHandle(process)
		if owner.Name == "" && owner.Executable != "" {
			owner.Name = filepath.Base(owner.Executable)
		}
		owner.Window = findProcessWindow(pid)
		return boundedOwner(owner), true, nil
	}
	return Owner{}, false, nil
}

func legacyCandidateProcessIDs(
	byProcess map[uint32][]legacySystemHandleEntry,
	names map[uint32]string,
	preferredName string,
) []uint32 {
	pids := make([]uint32, 0, len(byProcess))
	for pid := range byProcess {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(left, right int) bool {
		leftPreferred := preferredName != "" && strings.EqualFold(names[pids[left]], preferredName)
		rightPreferred := preferredName != "" && strings.EqualFold(names[pids[right]], preferredName)
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		return pids[left] < pids[right]
	})
	return pids
}

func legacySerialObjectNameCandidate(handle windows.Handle) bool {
	fileType, err := windows.GetFileType(handle)
	return err == nil && fileType == windows.FILE_TYPE_CHAR
}

func legacyFileHandles(ctx context.Context) ([]legacySystemHandleEntry, uint16, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	nul, err := windows.CreateFile(
		windows.StringToUTF16Ptr("NUL"),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("open NUL handle-type probe: %w", err)
	}
	defer windows.CloseHandle(nul)
	handles, err := legacyQuerySystemHandles(ctx)
	if err != nil {
		return nil, 0, err
	}
	pid := uintptr(os.Getpid())
	for _, entry := range handles {
		if entry.PID == pid && entry.Handle == uintptr(nul) {
			return handles, entry.ObjectTypeIndex, nil
		}
	}
	return nil, 0, errors.New("NT handle table omitted the local file-type probe")
}

func legacyQuerySystemHandles(ctx context.Context) ([]legacySystemHandleEntry, error) {
	size := uint32(1 << 20)
	for attempts := 0; attempts < 8; attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if size > legacyMaxHandleBuffer {
			return nil, fmt.Errorf(
				"NT handle table requires %d bytes; safety limit is %d",
				size,
				legacyMaxHandleBuffer,
			)
		}
		buffer := make([]byte, size)
		var needed uint32
		err := windows.NtQuerySystemInformation(
			windows.SystemExtendedHandleInformation,
			unsafe.Pointer(&buffer[0]),
			uint32(len(buffer)),
			&needed,
		)
		if err == windows.STATUS_INFO_LENGTH_MISMATCH {
			nextSize, growthErr := legacyBoundedBufferGrowth(
				size, needed, 64*1024, legacyMaxHandleBuffer,
			)
			if growthErr != nil {
				return nil, fmt.Errorf("NT handle table: %w", growthErr)
			}
			size = nextSize
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("NtQuerySystemInformation(handles): %w", err)
		}
		headerSize := unsafe.Sizeof(legacySystemHandleHeader{})
		entrySize := unsafe.Sizeof(legacySystemHandleEntry{})
		header := (*legacySystemHandleHeader)(unsafe.Pointer(&buffer[0]))
		maximum := uintptr(len(buffer)-int(headerSize)) / entrySize
		count := header.Count
		if count > maximum {
			return nil, errors.New("NT handle table count exceeds returned buffer")
		}
		if count > legacyMaxHandleCount {
			return nil, fmt.Errorf(
				"NT handle table count %d exceeds safety limit %d",
				count,
				legacyMaxHandleCount,
			)
		}
		result := make([]legacySystemHandleEntry, count)
		for index := uintptr(0); index < count; index++ {
			if index%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			entry := (*legacySystemHandleEntry)(unsafe.Add(
				unsafe.Pointer(&buffer[0]),
				headerSize+index*entrySize,
			))
			result[index] = *entry
		}
		return result, nil
	}
	return nil, errors.New("NT handle table changed repeatedly while sizing the query")
}

func legacyQueryObjectName(ctx context.Context, handle windows.Handle) (string, error) {
	size := uint32(1024)
	for attempts := 0; attempts < 5; attempts++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if size > legacyMaxObjectNameBuffer {
			return "", fmt.Errorf(
				"NT object name requires %d bytes; safety limit is %d",
				size,
				legacyMaxObjectNameBuffer,
			)
		}
		buffer := make([]byte, size)
		var needed uint32
		status, _, _ := legacyNTQueryObject.Call(
			uintptr(handle),
			legacyObjectNameInformation,
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&needed)),
		)
		result := windows.NTStatus(status)
		if result == windows.STATUS_INFO_LENGTH_MISMATCH || result == windows.STATUS_BUFFER_OVERFLOW {
			nextSize, growthErr := legacyBoundedBufferGrowth(
				size, needed, 256, legacyMaxObjectNameBuffer,
			)
			if growthErr != nil {
				return "", fmt.Errorf("NT object name: %w", growthErr)
			}
			size = nextSize
			continue
		}
		if result != 0 {
			return "", fmt.Errorf("NtQueryObject(name): %w", result)
		}
		name := (*windows.NTUnicodeString)(unsafe.Pointer(&buffer[0]))
		if name.Length == 0 || name.Buffer == nil {
			return "", nil
		}
		value := windows.UTF16ToString(unsafe.Slice(name.Buffer, int(name.Length/2)))
		runtime.KeepAlive(buffer)
		return value, nil
	}
	return "", errors.New("NT object name changed repeatedly while sizing the query")
}

func legacyBoundedBufferGrowth(current, needed, padding, limit uint32) (uint32, error) {
	next := needed
	if next <= current {
		if current > limit/2 {
			return 0, fmt.Errorf("buffer exceeded safety limit %d bytes", limit)
		}
		next = current * 2
	}
	if padding > limit || next > limit-padding {
		return 0, fmt.Errorf(
			"buffer requires at least %d bytes plus %d bytes slack; safety limit is %d",
			next,
			padding,
			limit,
		)
	}
	return next + padding, nil
}

func legacyMatchesDeviceTarget(name string, targets []string) bool {
	name = strings.TrimSpace(name)
	for _, target := range targets {
		if strings.EqualFold(name, strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func legacyProcessNames(ctx context.Context) map[uint32]string {
	result := make(map[uint32]string)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return result
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return result
	}
	for count := 0; count < 65536; count++ {
		if err := ctx.Err(); err != nil {
			break
		}
		result[entry.ProcessID] = windows.UTF16ToString(entry.ExeFile[:])
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return result
}
