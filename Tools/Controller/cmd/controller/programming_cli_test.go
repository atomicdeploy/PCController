package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/programmer"
)

type applicationIdentityRuntimeStub struct {
	hello       native.Hello
	closeErrors []error
	closeCalls  int
}

func (runtime *applicationIdentityRuntimeStub) EnsureConnected(context.Context) error {
	return nil
}

func (runtime *applicationIdentityRuntimeStub) Snapshot() control.Snapshot {
	return control.Snapshot{Hello: runtime.hello}
}

func (runtime *applicationIdentityRuntimeStub) Close() error {
	index := runtime.closeCalls
	runtime.closeCalls++
	if index < len(runtime.closeErrors) {
		return runtime.closeErrors[index]
	}
	return nil
}

func TestReadApplicationIdentityPropagatesCloseFailure(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	runtime := &applicationIdentityRuntimeStub{
		hello:       native.Hello{Name: "BOARD-01"},
		closeErrors: []error{closeErr},
	}
	hello, err := readApplicationIdentityWithRuntime(runtime)
	if hello.Name != "BOARD-01" {
		t.Fatalf("hello = %#v, want BOARD-01", hello)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("identity cleanup error = %v, want %v", err, closeErr)
	}
	if !errors.Is(err, errApplicationIdentityCleanup) {
		t.Fatalf("identity cleanup error = %v, want cleanup classification", err)
	}
	if retainedCommandRuntimeCount() != 1 {
		t.Fatal("identity close failure did not retain the Runtime owner")
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("drain identity Runtime: %v", err)
	}
	if runtime.closeCalls != 2 || retainedCommandRuntimeCount() != 0 {
		t.Fatalf("drained identity owner: close calls=%d retained=%d", runtime.closeCalls, retainedCommandRuntimeCount())
	}
}

func TestApplyApplicationIdentityAbortsOnCleanupFailure(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	identityErr := fmt.Errorf("%w: %w", errApplicationIdentityCleanup, closeErr)
	options := programmer.Options{}
	var stderr bytes.Buffer

	err := applyApplicationIdentity(&options, native.Hello{}, identityErr, &stderr)
	if !errors.Is(err, errApplicationIdentityCleanup) || !errors.Is(err, closeErr) {
		t.Fatalf("identity result error = %v, want cleanup and close failures", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("cleanup failure was downgraded to warning: %q", stderr.String())
	}
}

func TestApplyApplicationIdentityWarnsOnOrdinaryUnavailability(t *testing.T) {
	identityErr := errors.New("HELLO unavailable")
	options := programmer.Options{}
	var stderr bytes.Buffer

	if err := applyApplicationIdentity(&options, native.Hello{}, identityErr, &stderr); err != nil {
		t.Fatalf("ordinary identity failure became fatal: %v", err)
	}
	if !strings.Contains(stderr.String(), "continuing with programmer metadata") {
		t.Fatalf("identity warning = %q", stderr.String())
	}
}

func TestReconnectApplicationPropagatesDeferredCloseFailure(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	runtime := &applicationIdentityRuntimeStub{
		hello:       native.Hello{Name: "BOARD-01"},
		closeErrors: []error{closeErr},
	}
	var output bytes.Buffer

	err := reconnectApplicationWithRuntime(context.Background(), runtime, &output)
	if !errors.Is(err, closeErr) {
		t.Fatalf("application reconnect cleanup error = %v, want %v", err, closeErr)
	}
	if retainedCommandRuntimeCount() != 1 {
		t.Fatal("reconnect close failure did not retain the Runtime owner")
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("drain reconnect Runtime: %v", err)
	}
	if runtime.closeCalls != 2 || retainedCommandRuntimeCount() != 0 {
		t.Fatalf("drained reconnect owner: close calls=%d retained=%d", runtime.closeCalls, retainedCommandRuntimeCount())
	}
}

func TestRunRejectsRetainedCommandRuntimeUntilDrainSucceeds(t *testing.T) {
	closeErr := errors.New("CancelIoEx still failing")
	runtime := &applicationIdentityRuntimeStub{
		closeErrors: []error{closeErr, closeErr},
	}
	if err := closeCommandRuntime(
		runtime, runtime.Close, "close prior command runtime",
	); !errors.Is(err, closeErr) {
		t.Fatalf("install retained owner: %v", err)
	}

	err := run([]string{"version"}, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "before starting") {
		t.Fatalf("run with retained owner = %v, want preflight close failure", err)
	}
	if runtime.closeCalls != 2 || retainedCommandRuntimeCount() != 1 {
		t.Fatalf("preflight quarantine: close calls=%d retained=%d", runtime.closeCalls, retainedCommandRuntimeCount())
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("final retained-owner drain: %v", err)
	}
	if runtime.closeCalls != 3 || retainedCommandRuntimeCount() != 0 {
		t.Fatalf("final drain: close calls=%d retained=%d", runtime.closeCalls, retainedCommandRuntimeCount())
	}
}
