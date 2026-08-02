//go:build windows

package portowner

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsQueryDosDeviceUsesHarmlessNULDevice(t *testing.T) {
	targets, err := queryDeviceTargets("NUL")
	if err != nil {
		t.Fatalf("QueryDosDevice(NUL): %v", err)
	}
	if len(targets) == 0 || !strings.HasPrefix(strings.ToLower(targets[0]), `\device\`) {
		t.Fatalf("unexpected NUL targets: %q", targets)
	}
}

func TestWindowsSystemHandleProbeUsesOnlyNUL(t *testing.T) {
	handles, fileType, err := fileHandles(context.Background())
	if err != nil {
		t.Fatalf("native file-handle probe: %v", err)
	}
	if len(handles) == 0 || fileType == 0 {
		t.Fatalf("native file-handle probe returned handles=%d type=%d", len(handles), fileType)
	}
}

func TestWindowsHandleQueryHonorsCancellationBeforeSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := querySystemHandles(ctx); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("pre-cancelled handle query err=%v", err)
	}
}

func TestWindowsNativeBufferGrowthHasHardCap(t *testing.T) {
	next, err := boundedBufferGrowth(1024, 2048, 256, 4096)
	if err != nil || next != 2304 {
		t.Fatalf("explicit growth=%d err=%v", next, err)
	}
	next, err = boundedBufferGrowth(1024, 0, 256, 4096)
	if err != nil || next != 2304 {
		t.Fatalf("doubling growth=%d err=%v", next, err)
	}
	if _, err = boundedBufferGrowth(3072, 0, 256, 4096); err == nil ||
		!strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("uncapped doubling err=%v", err)
	}
	if _, err = boundedBufferGrowth(1024, 4096, 256, 4096); err == nil ||
		!strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("uncapped explicit growth err=%v", err)
	}
}

func TestWindowsSerialCandidateRejectsPipesBeforeObjectNameQuery(t *testing.T) {
	var readPipe, writePipe windows.Handle
	if err := windows.CreatePipe(&readPipe, &writePipe, nil, 0); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(readPipe)
	defer windows.CloseHandle(writePipe)
	if serialObjectNameCandidate(readPipe) || serialObjectNameCandidate(writePipe) {
		t.Fatal("pipe handle reached the serial object-name query path")
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
		t.Fatal(err)
	}
	defer windows.CloseHandle(nul)
	if !serialObjectNameCandidate(nul) {
		t.Fatal("character-device handle was excluded from the serial query path")
	}
}

func TestWindowsCandidateProcessesPrioritizeCurrentExecutableName(t *testing.T) {
	byProcess := map[uint32][]systemHandleEntry{
		44: {{PID: 44}},
		73: {{PID: 73}},
		12: {{PID: 12}},
	}
	names := map[uint32]string{44: "terminal.exe", 73: "controller.exe", 12: "CONTROLLER.EXE"}
	if got, want := candidateProcessIDs(byProcess, names, "controller.exe"), []uint32{12, 73, 44}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate process order=%v want=%v", got, want)
	}
}

func TestWindowsNativeOwnerScanFindsHarmlessCharacterDevice(t *testing.T) {
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
		t.Fatal(err)
	}
	defer windows.CloseHandle(nul)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	owner, found, err := scanNativeOwner(ctx, "NUL")
	if err != nil || !found || owner.PID != uint32(os.Getpid()) {
		t.Fatalf("NUL owner=%+v found=%t err=%v", owner, found, err)
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
