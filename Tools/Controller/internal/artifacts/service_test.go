package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeExecutor struct {
	mu       sync.Mutex
	firmware int
	restore  int
	eeprom   int
	host     int
	captured []CapturedFile
	err      error
}

type capturingExecutor struct {
	fakeExecutor
	firmwareArtifacts chan Descriptor
}

func (executor *capturingExecutor) ProgramFirmware(_ Context, artifact Descriptor, _ UpdateRequest, progress ProgressFunc) error {
	executor.firmwareArtifacts <- artifact
	progress("programming", 70, "programming firmware")
	return executor.err
}

func (fake *fakeExecutor) Capture(_ Context, _ CaptureRequest, progress ProgressFunc) ([]CapturedFile, error) {
	progress("reading", 50, "reading device")
	return fake.captured, fake.err
}

func (fake *fakeExecutor) ProgramFirmware(_ Context, _ Descriptor, _ UpdateRequest, progress ProgressFunc) error {
	fake.mu.Lock()
	fake.firmware++
	fake.mu.Unlock()
	progress("programming", 70, "programming firmware")
	return fake.err
}

func (fake *fakeExecutor) RestoreFlash(_ Context, artifact Descriptor, _ UpdateRequest, progress ProgressFunc) error {
	if artifact.Kind != KindFlashBackup {
		return errors.New("restore received a non-backup artifact")
	}
	fake.mu.Lock()
	fake.restore++
	fake.mu.Unlock()
	progress("verifying", 90, "restoring captured flash")
	return fake.err
}

func (fake *fakeExecutor) ProgramEEPROM(_ Context, _ Descriptor, _ UpdateRequest, progress ProgressFunc) error {
	fake.mu.Lock()
	fake.eeprom++
	fake.mu.Unlock()
	progress("programming", 70, "programming EEPROM")
	return fake.err
}

func (fake *fakeExecutor) StageHostUpdate(_ Context, _ Descriptor, _ UpdateRequest, progress ProgressFunc) error {
	fake.mu.Lock()
	fake.host++
	fake.mu.Unlock()
	progress("staging", 80, "staging host")
	return fake.err
}

func TestServiceRequiresAuthorizationAndRunsExplicitFirmwareUpdate(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFirmware, Name: "firmware.hex"})
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	events := make(chan string, 20)
	service, err := NewService(Options{
		Store: store, Executor: executor,
		Events: func(kind, _ string, _ map[string]string) { events <- kind },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.StartFirmwareUpdate(UpdateRequest{ArtifactSHA256: firmware.SHA256}); err == nil {
		t.Fatal("unauthorized firmware update accepted")
	}
	result, err := service.StartFirmwareUpdate(UpdateRequest{
		ArtifactSHA256: firmware.SHA256, Authorized: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitOperation(t, service, result.Operation.ID)
	if status.State != "completed" || status.ProgressPercent != 100 {
		t.Fatalf("status=%#v", status)
	}
	executor.mu.Lock()
	count := executor.firmware
	executor.mu.Unlock()
	if count != 1 {
		t.Fatalf("firmware calls=%d", count)
	}
	current, _ := store.Current(KindFirmware)
	if current == nil || current.SHA256 != firmware.SHA256 {
		t.Fatalf("current=%#v", current)
	}
	seenCompleted := false
	deadline := time.After(time.Second)
	for !seenCompleted {
		select {
		case event := <-events:
			seenCompleted = event == "update.completed"
		case <-deadline:
			t.Fatal("completion event not published")
		}
	}
}

func TestStartFirmwareUpdateSnapshotsDescriptorBeforeAsyncHandoff(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "firmware.hex",
		Metadata: map[string]string{"channel": "stable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &capturingExecutor{firmwareArtifacts: make(chan Descriptor, 1)}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	// Hold the transaction token so the async executor cannot inspect its
	// descriptor until StartFirmwareUpdate has finalized the public response.
	<-service.transaction
	released := false
	defer func() {
		if !released {
			service.transaction <- struct{}{}
		}
	}()
	result, err := service.StartFirmwareUpdate(UpdateRequest{
		ArtifactSHA256: firmware.SHA256,
		Authorized:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact == nil || result.Artifact.DownloadURL == "" {
		t.Fatalf("public artifact was not decorated: %#v", result.Artifact)
	}
	result.Artifact.Metadata["channel"] = "mutated-response"

	service.transaction <- struct{}{}
	released = true
	select {
	case executionArtifact := <-executor.firmwareArtifacts:
		if executionArtifact.DownloadURL != "" {
			t.Fatalf("executor received mutable public descriptor state: %#v", executionArtifact)
		}
		if got := executionArtifact.Metadata["channel"]; got != "stable" {
			t.Fatalf("executor metadata changed through response alias: got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("executor did not receive the firmware descriptor")
	}
	if status := waitOperation(t, service, result.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%#v", status)
	}
}

func TestUpdateIdempotencySurvivesPrimaryRestart(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFirmware, Name: "firmware.hex"})
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	request := UpdateRequest{
		ArtifactSHA256: firmware.SHA256, Authorized: true,
		Method: "urclock", IdempotencyKey: "deployment:42",
	}
	first, err := service.StartFirmwareUpdate(request)
	if err != nil {
		t.Fatal(err)
	}
	if status := waitOperation(t, service, first.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%#v", status)
	}
	reused, err := service.StartFirmwareUpdate(request)
	if err != nil || !reused.Reused || reused.Operation.ID != first.Operation.ID {
		t.Fatalf("reused=%#v err=%v", reused, err)
	}
	service.Close()

	restarted, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	replayed, err := restarted.StartFirmwareUpdate(request)
	if err != nil || !replayed.Reused || replayed.Operation.ID != first.Operation.ID {
		t.Fatalf("replayed=%#v err=%v", replayed, err)
	}
	executor.mu.Lock()
	calls := executor.firmware
	executor.mu.Unlock()
	if calls != 1 {
		t.Fatalf("firmware programmed %d times", calls)
	}
}

func TestUrclockTimeoutHasTypedISPFallbackTelemetry(t *testing.T) {
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFirmware, Name: "firmware.hex"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{Store: store, Executor: &fakeExecutor{err: context.DeadlineExceeded}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.StartFirmwareUpdate(UpdateRequest{
		ArtifactSHA256: firmware.SHA256, Authorized: true, Method: "urclock",
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitOperation(t, service, result.Operation.ID)
	if status.ErrorCode != "deadline_exceeded" ||
		status.ProgrammingMethod != ProgrammingMethodUrclock ||
		status.BootloaderOutcome != BootloaderTimedOut || !status.ISPFallbackSuggested {
		t.Fatalf("status=%#v", status)
	}
}

type serializingExecutorCall struct {
	kind     string
	artifact Descriptor
}

type serializingExecutor struct {
	entered chan serializingExecutorCall
	release chan struct{}
}

func (executor *serializingExecutor) wait(kind string, artifact Descriptor) {
	executor.entered <- serializingExecutorCall{kind: kind, artifact: artifact}
	<-executor.release
}

func (executor *serializingExecutor) Capture(_ Context, _ CaptureRequest, _ ProgressFunc) ([]CapturedFile, error) {
	executor.wait("capture", Descriptor{})
	return nil, errors.New("test capture has no files")
}
func (executor *serializingExecutor) ProgramFirmware(_ Context, artifact Descriptor, _ UpdateRequest, _ ProgressFunc) error {
	executor.wait("firmware", artifact)
	return nil
}
func (executor *serializingExecutor) RestoreFlash(_ Context, artifact Descriptor, _ UpdateRequest, _ ProgressFunc) error {
	executor.wait("restore", artifact)
	return nil
}
func (executor *serializingExecutor) ProgramEEPROM(_ Context, artifact Descriptor, _ UpdateRequest, _ ProgressFunc) error {
	executor.wait("eeprom", artifact)
	return nil
}
func (executor *serializingExecutor) StageHostUpdate(_ Context, artifact Descriptor, _ UpdateRequest, _ ProgressFunc) error {
	executor.wait("host", artifact)
	return nil
}

func TestProgrammingTransactionsAreSerializedWithStableExecutionArtifacts(t *testing.T) {
	store := newTestStore(t)
	firmware, _ := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFirmware, Name: "firmware.hex"})
	readback, _ := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFlashBackup, Name: "flash.hex"})
	executor := &serializingExecutor{entered: make(chan serializingExecutorCall, 2), release: make(chan struct{}, 2)}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	first, err := service.StartFirmwareUpdate(UpdateRequest{ArtifactSHA256: firmware.SHA256, Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartFlashRestore(UpdateRequest{ArtifactSHA256: readback.SHA256, Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact == nil || first.Artifact.DownloadURL == "" ||
		second.Artifact == nil || second.Artifact.DownloadURL == "" {
		t.Fatalf("operation responses were not decorated: first=%#v second=%#v", first.Artifact, second.Artifact)
	}
	firstCall := <-executor.entered
	if firstCall.artifact.DownloadURL != "" {
		t.Fatalf("executor received response-only download URL %q", firstCall.artifact.DownloadURL)
	}
	select {
	case next := <-executor.entered:
		t.Fatalf("second transaction entered concurrently: %s", next.kind)
	case <-time.After(40 * time.Millisecond):
	}
	executor.release <- struct{}{}
	secondCall := <-executor.entered
	if secondCall.artifact.DownloadURL != "" {
		t.Fatalf("queued executor received response-only download URL %q", secondCall.artifact.DownloadURL)
	}
	executor.release <- struct{}{}
	if status := waitOperation(t, service, first.Operation.ID); status.State != "completed" {
		t.Fatalf("first=%#v", status)
	}
	if status := waitOperation(t, service, second.Operation.ID); status.State != "completed" {
		t.Fatalf("second=%#v", status)
	}
}

func TestServiceCapturePublishesOnlyVerifiedReadback(t *testing.T) {
	directory := t.TempDir()
	flash := filepath.Join(directory, "flash.hex")
	eeprom := filepath.Join(directory, "eeprom.eep")
	if err := os.WriteFile(flash, []byte(validIntelHEX), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eeprom, []byte(validIntelHEX), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{captured: []CapturedFile{
		{Kind: KindFlashBackup, Name: "flash.hex", Path: flash, BuildHash: "F6D76FE4"},
		{Kind: KindEEPROM, Name: "eeprom.eep", Path: eeprom},
	}}
	service, _ := NewService(Options{Store: newTestStore(t), Executor: executor})
	defer service.Close()
	result, err := service.StartCapture(CaptureRequest{Authorized: true, Components: []string{"flash", "eeprom"}})
	if err != nil {
		t.Fatal(err)
	}
	if status := waitOperation(t, service, result.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%#v", status)
	}
	manifest, err := service.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Current.FlashReadback == nil || !manifest.Current.FlashReadback.VerifiedReadback ||
		manifest.Current.EEPROM == nil || !manifest.Current.EEPROM.VerifiedReadback {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestServiceRunsCapturedFlashRestoreWithoutFirmwareUpdateAlias(t *testing.T) {
	store := newTestStore(t)
	readback, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFlashBackup, Name: "captured-flash.hex", Source: "device-readback",
		VerifiedReadback: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.StartFlashRestore(UpdateRequest{ArtifactSHA256: readback.SHA256}); err == nil {
		t.Fatal("unauthorized captured-flash restore accepted")
	}
	params, _ := json.Marshal(UpdateRequest{ArtifactSHA256: readback.SHA256, Authorized: true})
	value, handled, err := service.DispatchRPC(context.Background(), "controller.restore.flash", params)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("dedicated flash-restore RPC was not handled")
	}
	result, ok := value.(OperationResult)
	if !ok {
		t.Fatalf("RPC result type=%T", value)
	}
	status := waitOperation(t, service, result.Operation.ID)
	if status.State != "completed" || status.Kind != "flash-restore" {
		t.Fatalf("status=%#v", status)
	}
	executor.mu.Lock()
	restores, firmware := executor.restore, executor.firmware
	executor.mu.Unlock()
	if restores != 1 || firmware != 0 {
		t.Fatalf("restore calls=%d firmware calls=%d", restores, firmware)
	}
	current, err := store.Current(KindFlashBackup)
	if err != nil || current == nil || current.SHA256 != readback.SHA256 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
}

func TestArtifactUploadRPCImportsSecondaryBytesAsImmutableArtifact(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	request := UploadRequest{
		Kind: KindFirmware, Name: "secondary.hex",
		Data: []byte(validIntelHEX), Bytes: int64(len(validIntelHEX)),
	}
	params, _ := json.Marshal(request)
	value, handled, err := service.DispatchRPC(
		context.Background(), "controller.artifact.upload", params,
	)
	if err != nil || !handled {
		t.Fatalf("handled=%t err=%v", handled, err)
	}
	result, ok := value.(OperationResult)
	if !ok || result.Artifact == nil || result.Operation.State != "completed" {
		t.Fatalf("result=%#v", value)
	}
	_, file, err := store.Open(KindFirmware, result.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil || string(content) != validIntelHEX {
		t.Fatalf("stored upload mismatch err=%v content=%q", err, content)
	}
}

func waitOperation(t *testing.T, service *Service, id string) UpdateStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := service.Status(id)
		if err == nil && (status.State == "completed" || status.State == "failed") {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := service.Status(id)
	t.Fatalf("operation did not finish: %#v", status)
	return UpdateStatus{}
}
