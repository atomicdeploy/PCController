package artifacts

import (
	"strings"
	"testing"
)

type deploymentExecutor struct {
	fakeExecutor
	requests chan UpdateRequest
}

func (executor *deploymentExecutor) ProgramFirmware(_ Context, _ Descriptor, request UpdateRequest, _ ProgressFunc) error {
	executor.requests <- request
	return nil
}

func TestFirmwareDeploymentCapturedBeforeIdempotency(t *testing.T) {
	t.Setenv("PCCONTROLLER_DEPLOYMENT", "")
	store := newTestStore(t)
	firmware, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{Kind: KindFirmware, Name: "candidate.hex"})
	if err != nil {
		t.Fatal(err)
	}
	classification := "development"
	executor := &deploymentExecutor{requests: make(chan UpdateRequest, 2)}
	service, err := NewService(Options{Store: store, Executor: executor, Deployment: func() string { return classification }})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	request := UpdateRequest{ArtifactSHA256: firmware.SHA256, Authorized: true, IdempotencyKey: "deployment-case"}
	result, err := service.StartFirmwareUpdate(request)
	if err != nil {
		t.Fatal(err)
	}
	classification = "production"
	if status := waitOperation(t, service, result.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%+v", status)
	}
	if got := <-executor.requests; got.Deployment != "development" {
		t.Fatalf("queued classification changed: %+v", got)
	}
	if _, err := service.StartFirmwareUpdate(request); err == nil {
		t.Fatal("same idempotency key silently reused a different effective policy")
	}
	request.Deployment = "development"
	if reused, err := service.StartFirmwareUpdate(request); err != nil || !reused.Reused {
		t.Fatalf("explicit original policy not reusable: %+v %v", reused, err)
	}
}
