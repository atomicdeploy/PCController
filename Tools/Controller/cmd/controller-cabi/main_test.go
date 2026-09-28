//go:build controllerlib

package main

import (
	"context"
	"errors"
	"testing"

	hostapi "pccontroller.local/controller/host"
)

func TestStartLibraryHostHonorsCanceledOperationContext(t *testing.T) {
	embedded, err := hostapi.New(hostapi.Options{
		DataRoot:           t.TempDir(),
		Branding:           hostapi.Branding{AppID: "pccontroller.cabi.timeout-test"},
		DisableAutoConnect: true,
		DisableNative:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	operationContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := startLibraryHost(operationContext, embedded); !errors.Is(err, context.Canceled) {
		t.Fatalf("start error = %v, want context.Canceled", err)
	}
	select {
	case <-embedded.Done():
	default:
		t.Fatal("timed-out Host startup did not complete rollback")
	}
}
