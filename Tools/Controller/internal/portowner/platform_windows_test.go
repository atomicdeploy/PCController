//go:build windows

package portowner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type fakeRestartManager struct {
	started     int
	ended       int
	registered  []string
	processes   []rmProcessInfo
	startErr    error
	registerErr error
	affectedErr error
}

type fakeOwnerHelper struct {
	owner Owner
	found bool
	err   error
	ports []string
}

func (fake *fakeOwnerHelper) FindOwner(_ context.Context, port string) (Owner, bool, error) {
	fake.ports = append(fake.ports, port)
	return fake.owner, fake.found, fake.err
}

func (fake *fakeRestartManager) StartSession(ctx context.Context) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fake.started++
	if fake.startErr != nil {
		return 0, fake.startErr
	}
	return uint32(fake.started), nil
}

func (fake *fakeRestartManager) RegisterResources(
	ctx context.Context,
	_ uint32,
	resources []string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fake.registered = append(fake.registered, resources...)
	return fake.registerErr
}

func (fake *fakeRestartManager) AffectedProcesses(
	ctx context.Context,
	_ uint32,
) ([]rmProcessInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return fake.processes, fake.affectedErr
}

func (fake *fakeRestartManager) EndSession(uint32) error {
	fake.ended++
	return nil
}

func TestWindowsQueryDosDeviceUsesHarmlessNULDevice(t *testing.T) {
	targets, err := queryDeviceTargets("NUL")
	if err != nil {
		t.Fatalf("QueryDosDevice(NUL): %v", err)
	}
	if len(targets) == 0 || !strings.HasPrefix(strings.ToLower(targets[0]), `\device\`) {
		t.Fatalf("unexpected NUL targets: %q", targets)
	}
	resources, err := deviceResourceCandidates("NUL")
	if err != nil {
		t.Fatalf("resource candidates: %v", err)
	}
	if resources[0] != `\\.\NUL` {
		t.Fatalf("unexpected primary resource candidate: %q", resources)
	}
	foundGlobalRoot := false
	for _, resource := range resources {
		if strings.HasPrefix(strings.ToLower(resource), `\\?\globalroot\device\`) {
			foundGlobalRoot = true
		}
	}
	if !foundGlobalRoot {
		t.Fatalf("NT target lacks a Win32 GLOBALROOT candidate: %q", resources)
	}
}

func TestWindowsRestartManagerQueryIsTargetScopedAndClosesSession(t *testing.T) {
	info := rmProcessInfo{Process: rmUniqueProcess{PID: uint32(os.Getpid())}}
	copy(info.ApplicationName[:], windows.StringToUTF16("Serial Console"))
	fake := &fakeRestartManager{processes: []rmProcessInfo{info}}
	owner, found, err := (nativeEnumerator{restartManager: fake}).FindOwner(context.Background(), "com7")
	if err != nil || !found {
		t.Fatalf("owner=%+v found=%t err=%v", owner, found, err)
	}
	if owner.PID != uint32(os.Getpid()) || owner.Name != "Serial Console" || owner.Executable == "" {
		t.Fatalf("incomplete owner metadata: %+v", owner)
	}
	if fake.started != 1 || fake.ended != 1 {
		t.Fatalf("Restart Manager sessions started=%d ended=%d", fake.started, fake.ended)
	}
	if len(fake.registered) == 0 || fake.registered[0] != `\\.\COM7` {
		t.Fatalf("registered resources=%q", fake.registered)
	}
	for _, resource := range fake.registered[1:] {
		if !strings.HasPrefix(strings.ToLower(resource), `\\?\globalroot\device\`) {
			t.Fatalf("non-target resource registered: %q", fake.registered)
		}
	}
}

func TestWindowsRestartManagerHonorsCancellationWithoutStartingWorker(t *testing.T) {
	fake := &fakeRestartManager{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := (nativeEnumerator{restartManager: fake}).FindOwner(ctx, "COM8")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup err=%v", err)
	}
	if fake.started != 0 || fake.ended != 0 || len(fake.registered) != 0 {
		t.Fatalf("cancelled lookup touched Restart Manager: %+v", fake)
	}
}

func TestWindowsRestartManagerCancellationInterruptsTaskAndJoinsWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	taskRelease := make(chan struct{})
	taskStarted := make(chan struct{})
	cancelTriggered := make(chan struct{})
	go func() {
		<-taskStarted
		cancel()
		close(cancelTriggered)
	}()
	result, err := runCancellableRestartManagerTask(
		ctx,
		func() { close(taskRelease) },
		func() uintptr {
			close(taskStarted)
			<-taskRelease
			return 17
		},
	)
	if result != 17 || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%d err=%v", result, err)
	}
	<-cancelTriggered
}

func TestWindowsRestartManagerNoAssociationExplainsNativeFallback(t *testing.T) {
	fake := &fakeRestartManager{}
	_, found, err := (nativeEnumerator{restartManager: fake}).FindOwner(context.Background(), "COM987")
	if found || err == nil {
		t.Fatalf("found=%t err=%v", found, err)
	}
	for _, text := range []string{"Restart Manager did not associate COM987", "without exposing its owner"} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("fallback diagnostic %q missing %q", err, text)
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "handle table") {
		t.Fatalf("legacy global handle-table fallback leaked into diagnostic: %v", err)
	}
	if fake.started == 0 || fake.started != fake.ended {
		t.Fatalf("Restart Manager session leak: started=%d ended=%d", fake.started, fake.ended)
	}
}

func TestWindowsRestartManagerBlockedDeviceTypeUsesNoAssociationFallback(t *testing.T) {
	fake := &fakeRestartManager{affectedErr: windows.ERROR_BAD_FILE_TYPE}
	_, found, err := (nativeEnumerator{restartManager: fake}).FindOwner(context.Background(), "COM987")
	if found || err == nil {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if !strings.Contains(err.Error(), "did not associate COM987") ||
		strings.Contains(err.Error(), "could not query") {
		t.Fatalf("blocked-device fallback is not human-readable: %v", err)
	}
}

func TestWindowsBlockedRestartManagerFallsBackToBoundedHelper(t *testing.T) {
	restartManager := &fakeRestartManager{affectedErr: windows.ERROR_BAD_FILE_TYPE}
	helper := &fakeOwnerHelper{
		found: true,
		owner: Owner{
			PID: 88, Name: "terminal.exe", Executable: `C:\Apps\terminal.exe`,
			ProcessStartTime: 1234,
			Window:           Window{Title: "Serial Console", Visible: true},
		},
	}
	owner, found, err := (&nativeEnumerator{
		restartManager: restartManager,
		helper:         helper,
	}).FindOwner(context.Background(), "COM987")
	if err != nil || !found || owner.PID != 88 || owner.ProcessStartTime != 1234 {
		t.Fatalf("owner=%+v found=%t err=%v", owner, found, err)
	}
	if len(helper.ports) != 1 || helper.ports[0] != "COM987" {
		t.Fatalf("helper ports=%q", helper.ports)
	}
}

func TestWindowsRestartManagerTransientFailureDoesNotLaunchGlobalFallback(t *testing.T) {
	restartManager := &fakeRestartManager{affectedErr: windows.ERROR_SEM_TIMEOUT}
	helper := &fakeOwnerHelper{found: true, owner: Owner{PID: 99}}
	_, found, err := (&nativeEnumerator{
		restartManager: restartManager,
		helper:         helper,
	}).FindOwner(context.Background(), "COM987")
	if found || err == nil || !strings.Contains(err.Error(), "could not query COM987") {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if len(helper.ports) != 0 {
		t.Fatalf("transient Restart Manager error launched helper: %q", helper.ports)
	}
}

func TestWindowsProcessHelperUsesCanonicalExecutableAndCachesResult(t *testing.T) {
	helper := newProcessOwnerHelper()
	var calls int
	helper.command = func(ctx context.Context, executable, port string) ([]byte, []byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if executable == "" || port != "COM77" {
			t.Fatalf("helper executable=%q port=%q", executable, port)
		}
		calls++
		return []byte(`{"version":1,"port":"COM77","found":true,"owner":{"pid":321,"name":"terminal.exe","process_start_time_100ns":99}}`), nil, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		owner, found, err := helper.FindOwner(context.Background(), "COM77")
		if err != nil || !found || owner.PID != 321 || owner.ProcessStartTime != 99 {
			t.Fatalf("attempt=%d owner=%+v found=%t err=%v", attempt, owner, found, err)
		}
	}
	if calls != 1 {
		t.Fatalf("bounded helper launches=%d; expected cached single launch", calls)
	}
}

func TestWindowsProcessHelperCancellationIsNotCached(t *testing.T) {
	helper := newProcessOwnerHelper()
	var calls int
	helper.command = func(ctx context.Context, _, _ string) ([]byte, []byte, error) {
		calls++
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, found, err := helper.FindOwner(ctx, "COM78")
		cancel()
		if found || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("attempt=%d found=%t err=%v", attempt, found, err)
		}
	}
	if calls != 2 {
		t.Fatalf("cancelled helper result was cached; launches=%d", calls)
	}
}

func TestWindowsProcessHelperSingleflightsConcurrentFallbacks(t *testing.T) {
	helper := newProcessOwnerHelper()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	helper.command = func(context.Context, string, string) ([]byte, []byte, error) {
		callsMu.Lock()
		calls++
		if calls == 1 {
			close(started)
		}
		callsMu.Unlock()
		<-release
		return []byte(`{"version":1,"port":"COM79","found":false}`), nil, nil
	}
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			_, found, err := helper.FindOwner(context.Background(), "COM79")
			if err == nil && found {
				err = errors.New("unexpected owner")
			}
			results <- err
		}()
	}
	<-started
	close(release)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Fatalf("concurrent bounded helper launches=%d", calls)
	}
}

func TestWindowsRestartManagerRegistrationFailureStillClosesSession(t *testing.T) {
	fake := &fakeRestartManager{registerErr: windows.ERROR_BAD_ARGUMENTS}
	_, found, err := (nativeEnumerator{restartManager: fake}).FindOwner(context.Background(), "COM987")
	if found || err == nil || !strings.Contains(err.Error(), "Restart Manager could not query COM987") {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if fake.started == 0 || fake.started != fake.ended {
		t.Fatalf("Restart Manager session leak: started=%d ended=%d", fake.started, fake.ended)
	}
}

func TestWindowsRestartManagerNativeSessionLifecycle(t *testing.T) {
	restartManager := nativeRestartManager{}
	session, err := restartManager.StartSession(context.Background())
	if err != nil {
		t.Fatalf("RmStartSession: %v", err)
	}
	if err := restartManager.EndSession(session); err != nil {
		t.Fatalf("RmEndSession: %v", err)
	}
}

func TestWindowsRestartManagerFindsProcessHoldingRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held-resource.bin")
	if err := os.WriteFile(path, []byte("owner-probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	resource, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		resource,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	processes, err := queryRestartManagerResources(
		context.Background(),
		nativeRestartManager{},
		[]string{path},
	)
	if err != nil {
		t.Fatalf("Restart Manager regular-file query: %v", err)
	}
	for _, process := range processes {
		if process.Process.PID == uint32(os.Getpid()) {
			owner, valid := ownerFromRestartManager(process)
			if !valid || owner.ProcessStartTime == 0 || owner.Executable == "" {
				t.Fatalf("Restart Manager metadata layout is invalid: process=%+v owner=%+v", process, owner)
			}
			return
		}
	}
	t.Fatalf("current PID %d absent from Restart Manager result: %+v", os.Getpid(), processes)
}

func TestWindowsVerifiedProcessIdentityRejectsPIDReuse(t *testing.T) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	startTime, err := queryProcessStartTime(process)
	windows.CloseHandle(process)
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{PID: uint32(os.Getpid()), ProcessStartTime: startTime}
	verified, err := openVerifiedOwner(owner, windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		t.Fatalf("current identity rejected: %v", err)
	}
	windows.CloseHandle(verified)
	owner.ProcessStartTime++
	if _, err := openVerifiedOwner(owner, windows.PROCESS_QUERY_LIMITED_INFORMATION); err == nil ||
		!strings.Contains(err.Error(), "no longer identifies") {
		t.Fatalf("stale PID identity err=%v", err)
	}
}

func TestWindowsVerifiedWindowRejectsStaleHandle(t *testing.T) {
	owner := Owner{
		PID:    uint32(os.Getpid()),
		Name:   "controller-test.exe",
		Window: Window{Handle: 1, Title: "stale"},
	}
	if _, err := verifiedOwnerWindow(owner); err == nil || !strings.Contains(err.Error(), "no longer belongs") {
		t.Fatalf("stale window identity err=%v", err)
	}
}

func TestWindowsLegacyHelperQueryHonorsCancellationBeforeSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := legacyQuerySystemHandles(ctx); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("pre-cancelled legacy query err=%v", err)
	}
}

func TestWindowsLegacyHelperBufferGrowthHasHardCap(t *testing.T) {
	next, err := legacyBoundedBufferGrowth(1024, 2048, 256, 4096)
	if err != nil || next != 2304 {
		t.Fatalf("explicit growth=%d err=%v", next, err)
	}
	if _, err = legacyBoundedBufferGrowth(3072, 0, 256, 4096); err == nil ||
		!strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("uncapped doubling err=%v", err)
	}
}

func TestWindowsLegacyHelperRejectsPipesBeforeObjectNameQuery(t *testing.T) {
	var readPipe, writePipe windows.Handle
	if err := windows.CreatePipe(&readPipe, &writePipe, nil, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(readPipe)
	defer windows.CloseHandle(writePipe)
	if legacySerialObjectNameCandidate(readPipe) || legacySerialObjectNameCandidate(writePipe) {
		t.Fatal("pipe handle reached the legacy object-name query")
	}
}

func TestWindowsLegacyHelperPrioritizesCanonicalExecutableName(t *testing.T) {
	byProcess := map[uint32][]legacySystemHandleEntry{
		44: {{PID: 44}},
		73: {{PID: 73}},
		12: {{PID: 12}},
	}
	names := map[uint32]string{44: "terminal.exe", 73: "controller.exe", 12: "CONTROLLER.EXE"}
	want := []uint32{12, 73, 44}
	if got := legacyCandidateProcessIDs(byProcess, names, "controller.exe"); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate process order=%v want=%v", got, want)
	}
}

func TestWindowsProcessMetadataAndProtectionWithoutSerialAccess(t *testing.T) {
	pid := uint32(os.Getpid())
	window := findProcessWindow(pid)
	_ = window // A console/test process is allowed to have no top-level window.
	actions := DefaultActions()
	owner := Owner{PID: pid, Name: "controller-test.exe"}
	err := actions.Terminate(context.Background(), owner, actions.TerminateConfirmation(owner))
	if err == nil || !strings.Contains(err.Error(), "current controller process") {
		t.Fatalf("current-process protection failed: %v", err)
	}
}
