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
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	objectNameInformation = 1
	wmClose               = 0x0010
	swRestore             = 9
	maxSystemHandleBuffer = 64 * 1024 * 1024
	maxSystemHandleCount  = 1_000_000
	maxCandidateHandles   = 100_000
	maxHandlesPerProcess  = 4_096
	maxObjectNameBuffer   = 64 * 1024
)

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	ntQueryObject        = ntdll.NewProc("NtQueryObject")
	user32               = windows.NewLazySystemDLL("user32.dll")
	getWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible      = user32.NewProc("IsWindowVisible")
	getWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	getWindowTextW       = user32.NewProc("GetWindowTextW")
	getClassNameW        = user32.NewProc("GetClassNameW")
	showWindowAsync      = user32.NewProc("ShowWindowAsync")
	setForegroundWindow  = user32.NewProc("SetForegroundWindow")
	postMessageW         = user32.NewProc("PostMessageW")
)

type nativeEnumerator struct{}

var defaultOwnerScans = newOwnerScanCoordinator()

type systemHandleHeader struct {
	Count    uintptr
	Reserved uintptr
}

type systemHandleEntry struct {
	Object                uintptr
	PID                   uintptr
	Handle                uintptr
	GrantedAccess         uint32
	CreatorBackTraceIndex uint16
	ObjectTypeIndex       uint16
	HandleAttributes      uint32
	Reserved              uint32
}

func systemEnumerator() Enumerator { return nativeEnumerator{} }
func DefaultActions() Actions      { return nativeActions{} }

func isAccessDenied(cause error) bool {
	return looksAccessDenied(cause) || errors.Is(cause, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(cause, windows.ERROR_SHARING_VIOLATION)
}

func (nativeEnumerator) FindOwner(ctx context.Context, port string) (Owner, bool, error) {
	waitContext, cancel := context.WithTimeout(ctx, ownerLookupHardTimeout+100*time.Millisecond)
	defer cancel()
	return defaultOwnerScans.find(waitContext, port, scanNativeOwner)
}

func scanNativeOwner(ctx context.Context, port string) (Owner, bool, error) {
	targets, err := queryDeviceTargets(port)
	if err != nil {
		return Owner{}, false, err
	}
	handles, fileType, err := fileHandles(ctx)
	if err != nil {
		return Owner{}, false, err
	}
	names := processNames(ctx)
	byProcess := make(map[uint32][]systemHandleEntry)
	candidates := 0
	for _, entry := range handles {
		if err := ctx.Err(); err != nil {
			return Owner{}, false, err
		}
		if entry.ObjectTypeIndex != fileType || entry.PID == 0 || entry.PID > uintptr(^uint32(0)) {
			continue
		}
		pid := uint32(entry.PID)
		if len(byProcess[pid]) >= maxHandlesPerProcess {
			continue
		}
		if candidates >= maxCandidateHandles {
			return Owner{}, false, fmt.Errorf(
				"NT file-handle candidates exceeded safety limit %d",
				maxCandidateHandles,
			)
		}
		byProcess[pid] = append(byProcess[pid], entry)
		candidates++
	}
	preferredName := ""
	if executable, executableErr := os.Executable(); executableErr == nil {
		preferredName = filepath.Base(executable)
	}
	pids := candidateProcessIDs(byProcess, names, preferredName)
	for _, pid := range pids {
		entries := byProcess[pid]
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
		for _, entry := range entries {
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
			// Serial ports are character devices. Discard disk and pipe handles
			// before NtQueryObject: named-pipe drivers are the common source of
			// indefinitely blocked object-name queries on Windows.
			if !serialObjectNameCandidate(duplicate) {
				windows.CloseHandle(duplicate)
				continue
			}
			name, nameErr := queryObjectName(ctx, duplicate)
			windows.CloseHandle(duplicate)
			if nameErr == nil && matchesDeviceTarget(name, targets) {
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
		windows.CloseHandle(process)
		if owner.Name == "" && owner.Executable != "" {
			owner.Name = filepath.Base(owner.Executable)
		}
		owner.Window = findProcessWindow(pid)
		return owner, true, nil
	}
	return Owner{}, false, nil
}

func candidateProcessIDs(
	byProcess map[uint32][]systemHandleEntry,
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

func serialObjectNameCandidate(handle windows.Handle) bool {
	fileType, err := windows.GetFileType(handle)
	return err == nil && fileType == windows.FILE_TYPE_CHAR
}

func queryDeviceTargets(port string) ([]string, error) {
	name := strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(port), `\\.\`))
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("encode serial device name: %w", err)
	}
	buffer := make([]uint16, 1024)
	count, err := windows.QueryDosDevice(pointer, &buffer[0], uint32(len(buffer)))
	if err != nil {
		return nil, fmt.Errorf("QueryDosDevice(%s): %w", name, err)
	}
	var targets []string
	start := 0
	for index := 0; index < int(count); index++ {
		if buffer[index] != 0 {
			continue
		}
		if index > start {
			targets = append(targets, windows.UTF16ToString(buffer[start:index]))
		}
		start = index + 1
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("QueryDosDevice(%s) returned no NT target", name)
	}
	return targets, nil
}

func fileHandles(ctx context.Context) ([]systemHandleEntry, uint16, error) {
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
	handles, err := querySystemHandles(ctx)
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

func querySystemHandles(ctx context.Context) ([]systemHandleEntry, error) {
	size := uint32(1 << 20)
	for attempts := 0; attempts < 8; attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if size > maxSystemHandleBuffer {
			return nil, fmt.Errorf(
				"NT handle table requires %d bytes; safety limit is %d",
				size,
				maxSystemHandleBuffer,
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
			nextSize, growthErr := boundedBufferGrowth(size, needed, 64*1024, maxSystemHandleBuffer)
			if growthErr != nil {
				return nil, fmt.Errorf("NT handle table: %w", growthErr)
			}
			size = nextSize
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("NtQuerySystemInformation(handles): %w", err)
		}
		headerSize := unsafe.Sizeof(systemHandleHeader{})
		entrySize := unsafe.Sizeof(systemHandleEntry{})
		header := (*systemHandleHeader)(unsafe.Pointer(&buffer[0]))
		maximum := uintptr(len(buffer)-int(headerSize)) / entrySize
		count := header.Count
		if count > maximum {
			return nil, errors.New("NT handle table count exceeds returned buffer")
		}
		if count > maxSystemHandleCount {
			return nil, fmt.Errorf(
				"NT handle table count %d exceeds safety limit %d",
				count,
				maxSystemHandleCount,
			)
		}
		result := make([]systemHandleEntry, count)
		for index := uintptr(0); index < count; index++ {
			if index%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			entry := (*systemHandleEntry)(unsafe.Add(unsafe.Pointer(&buffer[0]), headerSize+index*entrySize))
			result[index] = *entry
		}
		return result, nil
	}
	return nil, errors.New("NT handle table changed repeatedly while sizing the query")
}

func queryObjectName(ctx context.Context, handle windows.Handle) (string, error) {
	size := uint32(1024)
	for attempts := 0; attempts < 5; attempts++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if size > maxObjectNameBuffer {
			return "", fmt.Errorf(
				"NT object name requires %d bytes; safety limit is %d",
				size,
				maxObjectNameBuffer,
			)
		}
		buffer := make([]byte, size)
		var needed uint32
		status, _, _ := ntQueryObject.Call(
			uintptr(handle),
			objectNameInformation,
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&needed)),
		)
		result := windows.NTStatus(status)
		if result == windows.STATUS_INFO_LENGTH_MISMATCH || result == windows.STATUS_BUFFER_OVERFLOW {
			nextSize, growthErr := boundedBufferGrowth(size, needed, 256, maxObjectNameBuffer)
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

func boundedBufferGrowth(current, needed, padding, limit uint32) (uint32, error) {
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

func matchesDeviceTarget(name string, targets []string) bool {
	name = strings.TrimSpace(name)
	for _, target := range targets {
		if strings.EqualFold(name, strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func processNames(ctx context.Context) map[uint32]string {
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
	count := 0
	for {
		if err := ctx.Err(); err != nil || count >= 65536 {
			break
		}
		result[entry.ProcessID] = windows.UTF16ToString(entry.ExeFile[:])
		count++
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return result
}

func queryProcessPath(process windows.Handle) string {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buffer[:size])
}

func findProcessWindow(pid uint32) Window {
	var best Window
	callback := syscall.NewCallback(func(handle uintptr, _ uintptr) uintptr {
		var candidatePID uint32
		getWindowThreadPID.Call(handle, uintptr(unsafe.Pointer(&candidatePID)))
		if candidatePID != pid {
			return 1
		}
		visible, _, _ := isWindowVisible.Call(handle)
		candidate := Window{
			Handle: handle, Visible: visible != 0,
			Title: windowText(handle), Class: windowClass(handle),
		}
		if best.Handle == 0 || (!best.Visible && candidate.Visible) ||
			(best.Title == "" && candidate.Title != "") {
			best = candidate
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return best
}

func windowText(handle uintptr) string {
	length, _, _ := getWindowTextLengthW.Call(handle)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, length+1)
	written, _, _ := getWindowTextW.Call(handle, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return windows.UTF16ToString(buffer[:written])
}

func windowClass(handle uintptr) string {
	buffer := make([]uint16, 256)
	written, _, _ := getClassNameW.Call(handle, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if written == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer[:written])
}

type nativeActions struct{}

func (nativeActions) BringToForeground(ctx context.Context, owner Owner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	window := owner.Window
	if window.Handle == 0 {
		window = findProcessWindow(owner.PID)
	}
	if window.Handle == 0 {
		return fmt.Errorf("%s has no top-level window", owner.Label())
	}
	showWindowAsync.Call(window.Handle, swRestore)
	ok, _, callErr := setForegroundWindow.Call(window.Handle)
	if ok == 0 {
		return fmt.Errorf("Windows foreground policy refused %s: %v", owner.Label(), callErr)
	}
	return nil
}

func (nativeActions) RequestGracefulClose(ctx context.Context, owner Owner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	window := owner.Window
	if window.Handle == 0 {
		window = findProcessWindow(owner.PID)
	}
	if window.Handle == 0 {
		return fmt.Errorf("%s has no top-level window for WM_CLOSE", owner.Label())
	}
	ok, _, callErr := postMessageW.Call(window.Handle, wmClose, 0, 0)
	if ok == 0 {
		return fmt.Errorf("request graceful close from %s: %v", owner.Label(), callErr)
	}
	return nil
}

func (nativeActions) Terminate(ctx context.Context, owner Owner, confirmation string) error {
	currentExecutable, _ := os.Executable()
	if err := validateTermination(owner, confirmation, uint32(os.Getpid()), currentExecutable); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, owner.PID)
	if err != nil {
		return fmt.Errorf("open %s for explicit termination: %w", owner.Label(), err)
	}
	defer windows.CloseHandle(process)
	if err := windows.TerminateProcess(process, 1); err != nil {
		return fmt.Errorf("terminate %s: %w", owner.Label(), err)
	}
	return nil
}

func (nativeActions) TerminateConfirmation(owner Owner) string {
	return terminationConfirmation(owner)
}
