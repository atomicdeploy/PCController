package control

import (
	"context"
	"errors"
	"os"
	"testing"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/portowner"
	"pccontroller.local/controller/internal/ports"
)

func portProcessTestRuntime() *Runtime {
	return &Runtime{port: ports.Info{Name: "OFFLINE"}, programState: NewProgramStateManager(nil), eventNotify: make(chan struct{})}
}

func TestPortProcessUsesActiveSessionOwnership(t *testing.T) {
	runtime := portProcessTestRuntime()
	runtime.session = &link.Session{}
	runtime.refreshPortProcessWith(func(context.Context, string) (portowner.Owner, bool, error) {
		t.Fatal("active local session must not trigger an OS ownership lookup")
		return portowner.Owner{}, false, nil
	})
	if got := runtime.portProcess; got.State != "owned" || got.PID != uint32(os.Getpid()) || got.Error != "" || got.TakeoverReady {
		t.Fatalf("incorrect local ownership: %+v", got)
	}
}

func TestPortProcessPreservesDisconnectedLookupFailure(t *testing.T) {
	runtime := portProcessTestRuntime()
	runtime.refreshPortProcessWith(func(context.Context, string) (portowner.Owner, bool, error) {
		return portowner.Owner{}, false, errors.New("permission denied")
	})
	if got := runtime.portProcess; got.State != "unknown" || got.Error != "permission denied" {
		t.Fatalf("lost real diagnostic: %+v", got)
	}
}

func TestPortProcessDiscardsLookupAfterConnectionChange(t *testing.T) {
	runtime := portProcessTestRuntime()
	runtime.refreshPortProcessWith(func(context.Context, string) (portowner.Owner, bool, error) {
		runtime.mu.Lock()
		runtime.session = &link.Session{}
		runtime.mu.Unlock()
		return portowner.Owner{}, false, errors.New("stale diagnostic")
	})
	if runtime.portProcess.State != "" {
		t.Fatalf("stale lookup published: %+v", runtime.portProcess)
	}
}
