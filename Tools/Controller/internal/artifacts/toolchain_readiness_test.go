package artifacts

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

type readinessExecutor struct {
	fakeExecutor
	ensureErr error
	mu        sync.Mutex
	called    bool
}

func (executor *readinessExecutor) EnsureToolchain(
	_ Context,
	progress ProgressFunc,
) (ToolchainReadiness, error) {
	progress("toolchain-provisioning", -1, "ensuring shared toolchain")
	if executor.ensureErr != nil {
		return ToolchainReadiness{}, executor.ensureErr
	}
	return ToolchainReadiness{
		Ready: true, Policy: "board-policy", Provider: "pccontroller-managed-arduino-cli",
		Version: "1.5.1", CompatibleSources: 2,
		Providers: []string{"pccontroller-policy:board-policy", "arduino-cli:1.5.1:selected"},
	}, nil
}

func (executor *readinessExecutor) ProgramFirmware(
	_ Context,
	_ Descriptor,
	_ UpdateRequest,
	progress ProgressFunc,
) error {
	executor.mu.Lock()
	executor.called = true
	executor.mu.Unlock()
	progress("writing", 0, "starting measured board write")
	progress("writing", 100, "board write complete")
	return nil
}

func TestToolchainIsReadyBeforeKnownZeroProgress(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "candidate.hex",
	})
	if err != nil {
		t.Fatal(err)
	}
	type observed struct {
		kind     string
		metadata map[string]string
	}
	var mu sync.Mutex
	var events []observed
	service, err := NewService(Options{
		Store: store, Executor: &readinessExecutor{}, ProgressURL: "/control",
		Events: func(kind, _ string, metadata map[string]string) {
			copyMetadata := make(map[string]string, len(metadata))
			for key, value := range metadata {
				copyMetadata[key] = value
			}
			mu.Lock()
			events = append(events, observed{kind: kind, metadata: copyMetadata})
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.StartFirmwareUpdate(UpdateRequest{
		ArtifactSHA256: firmware.SHA256, Authorized: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Progress.Transport != "websocket" || result.Progress.URL != "/control" ||
		result.Progress.OperationID != result.Operation.ID {
		t.Fatalf("progress stream=%#v", result.Progress)
	}
	status := waitOperation(t, service, result.Operation.ID)
	if status.State != "completed" || status.Toolchain == nil || !status.Toolchain.Ready ||
		status.Toolchain.CompatibleSources < 2 {
		t.Fatalf("status=%#v", status)
	}

	mu.Lock()
	defer mu.Unlock()
	readyIndex, zeroIndex := -1, -1
	for index, event := range events {
		if event.kind == "update.toolchain-ready" && event.metadata["toolchain_ready"] == "true" {
			readyIndex = index
		}
		if event.metadata["progress_known"] == "true" && event.metadata["progress_percent"] == "0" {
			zeroIndex = index
			if event.metadata["toolchain_ready"] != "true" {
				t.Fatalf("known zero progress lacks toolchain readiness: %#v", event)
			}
			break
		}
	}
	if readyIndex < 0 || zeroIndex < 0 || readyIndex >= zeroIndex {
		t.Fatalf("events=%#v ready=%d zero=%d", events, readyIndex, zeroIndex)
	}
}

func TestToolchainFailureStopsBeforeBoardExecution(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "candidate.hex",
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &readinessExecutor{ensureErr: errors.New("registry unavailable")}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.StartFirmwareUpdate(UpdateRequest{
		ArtifactSHA256: firmware.SHA256, Authorized: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitOperation(t, service, result.Operation.ID)
	if status.State != "failed" || status.ErrorCode != "toolchain_unavailable" ||
		status.ProgressKnown || status.BootloaderOutcome != BootloaderNotAttempted {
		t.Fatalf("status=%#v", status)
	}
	executor.mu.Lock()
	called := executor.called
	executor.mu.Unlock()
	if called {
		t.Fatal("board executor ran after toolchain failure")
	}
}
