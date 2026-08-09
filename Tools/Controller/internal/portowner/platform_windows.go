//go:build windows

package portowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	rmSessionKeyLength     = 32
	rmMaxAppName           = 255
	rmMaxServiceName       = 63
	maxRMAffectedProcesses = 4096
	ownerLookupTimeout     = 2 * time.Second
	wmClose                = 0x0010
	swRestore              = 9
)

var (
	restartManagerDLL       = windows.NewLazySystemDLL("rstrtmgr.dll")
	rmStartSessionProc      = restartManagerDLL.NewProc("RmStartSession")
	rmRegisterResourcesProc = restartManagerDLL.NewProc("RmRegisterResources")
	rmGetListProc           = restartManagerDLL.NewProc("RmGetList")
	rmEndSessionProc        = restartManagerDLL.NewProc("RmEndSession")
	rmCancelCurrentTaskProc = restartManagerDLL.NewProc("RmCancelCurrentTask")
	user32                  = windows.NewLazySystemDLL("user32.dll")
	getWindowThreadPID      = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible         = user32.NewProc("IsWindowVisible")
	getWindowTextLengthW    = user32.NewProc("GetWindowTextLengthW")
	getWindowTextW          = user32.NewProc("GetWindowTextW")
	getClassNameW           = user32.NewProc("GetClassNameW")
	showWindowAsync         = user32.NewProc("ShowWindowAsync")
	setForegroundWindow     = user32.NewProc("SetForegroundWindow")
	postMessageW            = user32.NewProc("PostMessageW")
)

// rmUniqueProcess is the Restart Manager identity used to reject PID reuse.
type rmUniqueProcess struct {
	PID       uint32
	StartTime windows.Filetime
}

// rmProcessInfo mirrors RM_PROCESS_INFO from restartmanager.h.
type rmProcessInfo struct {
	Process           rmUniqueProcess
	ApplicationName   [rmMaxAppName + 1]uint16
	ServiceShortName  [rmMaxServiceName + 1]uint16
	ApplicationType   uint32
	ApplicationStatus uint32
	TerminalSessionID uint32
	Restartable       int32
}

// restartManagerQuery isolates the target-scoped native operations for tests.
type restartManagerQuery interface {
	StartSession(context.Context) (uint32, error)
	RegisterResources(context.Context, uint32, []string) error
	AffectedProcesses(context.Context, uint32) ([]rmProcessInfo, error)
	EndSession(uint32) error
}

type nativeRestartManager struct{}

type nativeEnumerator struct {
	restartManager restartManagerQuery
	helper         ownerHelperQuery
}

var defaultSystemEnumerator = &nativeEnumerator{
	restartManager: nativeRestartManager{},
	helper:         newProcessOwnerHelper(),
}

func systemEnumerator() Enumerator { return defaultSystemEnumerator }

func DefaultActions() Actions { return nativeActions{} }

func isAccessDenied(cause error) bool {
	return looksAccessDenied(cause) || errors.Is(cause, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(cause, windows.ERROR_SHARING_VIOLATION)
}

func isLocalSerialTarget(value string) bool { return isCOMPort(value) }

func (enumerator nativeEnumerator) FindOwner(ctx context.Context, port string) (Owner, bool, error) {
	if err := ctx.Err(); err != nil {
		return Owner{}, false, err
	}
	port = normalizeHelperPort(port)
	if !validHelperPort(port) {
		return Owner{}, false, errors.New("serial-owner lookup requires an exact COM number")
	}
	ctx, cancel := context.WithTimeout(ctx, ownerLookupTimeout)
	defer cancel()
	restartManager := enumerator.restartManager
	if restartManager == nil {
		restartManager = nativeRestartManager{}
	}
	resources, targetErr := deviceResourceCandidates(port)
	processes, queryErr := queryRestartManagerResources(ctx, restartManager, resources)
	for _, process := range processes {
		owner, valid := ownerFromRestartManager(process)
		if valid {
			return owner, true, nil
		}
	}
	port = normalizePortName(port)
	restartManagerUnsupported := restartManagerCannotAssociate(queryErr)
	if restartManagerUnsupported {
		queryErr = nil
	}
	if queryErr != nil {
		if targetErr != nil {
			queryErr = errors.Join(queryErr, targetErr)
		}
		return Owner{}, false, fmt.Errorf(
			"Restart Manager could not query %s: %w",
			port,
			queryErr,
		)
	}
	if enumerator.helper != nil {
		owner, found, helperErr := enumerator.helper.FindOwner(ctx, port)
		if helperErr != nil {
			return Owner{}, false, fmt.Errorf(
				"Restart Manager did not expose the %s owner; bounded helper fallback failed: %w",
				port,
				helperErr,
			)
		}
		if found {
			return owner, true, nil
		}
	} else {
		return Owner{}, false, fmt.Errorf(
			"Restart Manager did not associate %s with an application; Windows can report a serial device busy without exposing its owner",
			port,
		)
	}
	return Owner{}, false, fmt.Errorf(
		"neither Restart Manager nor the bounded native helper identified the application holding %s",
		port,
	)
}

func restartManagerCannotAssociate(err error) bool {
	return errors.Is(err, windows.ERROR_BAD_FILE_TYPE) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_INVALID_FUNCTION)
}

func normalizePortName(port string) string {
	name := strings.TrimSpace(strings.ToUpper(port))
	name = strings.TrimPrefix(name, `\\.\`)
	name = strings.TrimPrefix(name, `\\?\`)
	return name
}

// deviceResourceCandidates supplies only aliases for the selected device.
// Restart Manager officially promises full filename resources, not COM paths,
// so an empty association is an expected, explicit fallback on some drivers.
func deviceResourceCandidates(port string) ([]string, error) {
	name := normalizePortName(port)
	resources := make([]string, 0, 2)
	seen := make(map[string]struct{})
	add := func(resource string) {
		resource = strings.TrimSpace(resource)
		key := strings.ToLower(resource)
		if resource == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		resources = append(resources, resource)
	}
	add(`\\.\` + name)
	targets, err := queryDeviceTargets(name)
	for _, target := range targets {
		if strings.HasPrefix(strings.ToLower(target), `\device\`) {
			add(`\\?\GLOBALROOT` + target)
		}
	}
	return resources, err
}

func queryDeviceTargets(port string) ([]string, error) {
	name := normalizePortName(port)
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

func queryRestartManagerResources(
	ctx context.Context,
	restartManager restartManagerQuery,
	resources []string,
) (processes []rmProcessInfo, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session, err := restartManager.StartSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if endErr := restartManager.EndSession(session); resultErr == nil && endErr != nil {
			resultErr = endErr
		}
	}()
	if err := restartManager.RegisterResources(ctx, session, resources); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return restartManager.AffectedProcesses(ctx, session)
}

func (nativeRestartManager) StartSession(ctx context.Context) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var session uint32
	var key [rmSessionKeyLength + 1]uint16
	result, _, _ := rmStartSessionProc.Call(
		uintptr(unsafe.Pointer(&session)),
		0,
		uintptr(unsafe.Pointer(&key[0])),
	)
	if err := restartManagerResult("RmStartSession", result); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		_ = (nativeRestartManager{}).EndSession(session)
		return 0, err
	}
	return session, nil
}

func (nativeRestartManager) RegisterResources(ctx context.Context, session uint32, resources []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(resources) == 0 {
		return errors.New("Restart Manager requires at least one resource path")
	}
	values := make([]*uint16, 0, len(resources))
	for _, resource := range resources {
		value, err := windows.UTF16PtrFromString(resource)
		if err != nil {
			return fmt.Errorf("encode Restart Manager resource: %w", err)
		}
		values = append(values, value)
	}
	result, _, _ := rmRegisterResourcesProc.Call(
		uintptr(session),
		uintptr(len(values)),
		uintptr(unsafe.Pointer(&values[0])),
		0,
		0,
		0,
		0,
	)
	return restartManagerResult("RmRegisterResources", result)
}

func (nativeRestartManager) AffectedProcesses(ctx context.Context, session uint32) ([]rmProcessInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var needed uint32
	var count uint32
	var rebootReasons uint32
	result, err := restartManagerGetList(
		ctx,
		session,
		uintptr(session),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&count)),
		0,
		uintptr(unsafe.Pointer(&rebootReasons)),
	)
	if err != nil {
		return nil, err
	}
	if result == 0 && needed == 0 {
		return nil, ctx.Err()
	}
	if result != uintptr(windows.ERROR_MORE_DATA) {
		return nil, restartManagerResult("RmGetList(size)", result)
	}
	for attempts := 0; attempts < 4; attempts++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if needed == 0 || needed > maxRMAffectedProcesses {
			return nil, fmt.Errorf(
				"RmGetList reported %d affected processes; safety limit is %d",
				needed,
				maxRMAffectedProcesses,
			)
		}
		processes := make([]rmProcessInfo, needed)
		count = needed
		result, err = restartManagerGetList(
			ctx,
			session,
			uintptr(session),
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&count)),
			uintptr(unsafe.Pointer(&processes[0])),
			uintptr(unsafe.Pointer(&rebootReasons)),
		)
		if err != nil {
			return nil, err
		}
		if result == uintptr(windows.ERROR_MORE_DATA) {
			continue
		}
		if err := restartManagerResult("RmGetList", result); err != nil {
			return nil, err
		}
		if count > uint32(len(processes)) {
			return nil, errors.New("RmGetList returned more processes than its output buffer")
		}
		return processes[:count], ctx.Err()
	}
	return nil, errors.New("RmGetList changed repeatedly while sizing its target result")
}

// restartManagerGetList keeps the potentially blocking native call on the
// caller and requests cancellation through RmCancelCurrentTask.
// The only helper goroutine is a bounded cancellation watcher; no native-query
// worker can outlive the lookup and accumulate behind later retries.
func restartManagerGetList(
	ctx context.Context,
	session uint32,
	arguments ...uintptr,
) (uintptr, error) {
	return runCancellableRestartManagerTask(
		ctx,
		func() { _, _, _ = rmCancelCurrentTaskProc.Call(uintptr(session)) },
		func() uintptr {
			result, _, _ := rmGetListProc.Call(arguments...)
			return result
		},
	)
}

func runCancellableRestartManagerTask(
	ctx context.Context,
	cancelTask func(),
	task func() uintptr,
) (uintptr, error) {
	if ctx.Done() == nil {
		return task(), nil
	}
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			cancelTask()
		case <-done:
		}
	}()
	result := task()
	close(done)
	<-watcherDone
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func (nativeRestartManager) EndSession(session uint32) error {
	result, _, _ := rmEndSessionProc.Call(uintptr(session))
	return restartManagerResult("RmEndSession", result)
}

func restartManagerResult(operation string, result uintptr) error {
	if result == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, syscall.Errno(result))
}

func ownerFromRestartManager(process rmProcessInfo) (Owner, bool) {
	if process.Process.PID == 0 {
		return Owner{}, false
	}
	owner := Owner{
		PID:              process.Process.PID,
		Name:             windows.UTF16ToString(process.ApplicationName[:]),
		ProcessStartTime: filetimeValue(process.Process.StartTime),
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, owner.PID)
	if err == nil {
		defer windows.CloseHandle(handle)
		if owner.ProcessStartTime != 0 {
			startTime, startErr := queryProcessStartTime(handle)
			if startErr == nil && startTime != owner.ProcessStartTime {
				return Owner{}, false
			}
		}
		owner.Executable = queryProcessPath(handle)
	} else if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return Owner{}, false
	}
	if owner.Name == "" && owner.Executable != "" {
		owner.Name = filepath.Base(owner.Executable)
	}
	owner.Window = findProcessWindow(owner.PID)
	return owner, true
}

func filetimeValue(value windows.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func queryProcessStartTime(process windows.Handle) (uint64, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return filetimeValue(created), nil
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
	process, err := openVerifiedOwner(owner, windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return err
	}
	windows.CloseHandle(process)
	window, err := verifiedOwnerWindow(owner)
	if err != nil {
		return err
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
	process, err := openVerifiedOwner(owner, windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return err
	}
	windows.CloseHandle(process)
	window, err := verifiedOwnerWindow(owner)
	if err != nil {
		return err
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
	process, err := openVerifiedOwner(
		owner,
		windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
	)
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

func openVerifiedOwner(owner Owner, access uint32) (windows.Handle, error) {
	if owner.PID == 0 {
		return 0, errors.New("serial owner has no PID")
	}
	process, err := windows.OpenProcess(access, false, owner.PID)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", owner.Label(), err)
	}
	if owner.ProcessStartTime != 0 {
		startTime, startErr := queryProcessStartTime(process)
		if startErr != nil {
			windows.CloseHandle(process)
			return 0, fmt.Errorf("verify %s start time: %w", owner.Label(), startErr)
		}
		if startTime != owner.ProcessStartTime {
			windows.CloseHandle(process)
			return 0, fmt.Errorf("refusing action: PID %d no longer identifies the diagnosed serial owner", owner.PID)
		}
	}
	if owner.Executable != "" {
		executable := queryProcessPath(process)
		if executable == "" || !strings.EqualFold(filepath.Clean(executable), filepath.Clean(owner.Executable)) {
			windows.CloseHandle(process)
			return 0, fmt.Errorf("refusing action: executable identity for PID %d changed", owner.PID)
		}
	}
	return process, nil
}

func verifiedOwnerWindow(owner Owner) (Window, error) {
	window := owner.Window
	if window.Handle == 0 {
		window = findProcessWindow(owner.PID)
	}
	if window.Handle == 0 {
		return Window{}, fmt.Errorf("%s has no top-level window", owner.Label())
	}
	var windowPID uint32
	getWindowThreadPID.Call(window.Handle, uintptr(unsafe.Pointer(&windowPID)))
	if windowPID != owner.PID {
		return Window{}, fmt.Errorf("refusing action: the diagnosed window no longer belongs to %s", owner.Label())
	}
	return window, nil
}
