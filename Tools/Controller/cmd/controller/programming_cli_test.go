package main

import (
	"context"
	"errors"
	"testing"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

type applicationIdentityRuntimeStub struct {
	hello    native.Hello
	closeErr error
}

func (runtime *applicationIdentityRuntimeStub) EnsureConnected(context.Context) error {
	return nil
}

func (runtime *applicationIdentityRuntimeStub) Snapshot() control.Snapshot {
	return control.Snapshot{Hello: runtime.hello}
}

func (runtime *applicationIdentityRuntimeStub) Close() error {
	return runtime.closeErr
}

func TestReadApplicationIdentityPropagatesCloseFailure(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	runtime := &applicationIdentityRuntimeStub{
		hello:    native.Hello{Name: "BOARD-01"},
		closeErr: closeErr,
	}
	hello, err := readApplicationIdentityWithRuntime(runtime)
	if hello.Name != "BOARD-01" {
		t.Fatalf("hello = %#v, want BOARD-01", hello)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("identity cleanup error = %v, want %v", err, closeErr)
	}
}
