package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

func TestLifecyclePurgeTargetsKeepExactConfigAndHonorDataOverride(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "source", "PCController", "config.json")
	data := filepath.Join(base, "custom", "controller-storage")
	t.Setenv("PCCONTROLLER_CONFIG", config)
	t.Setenv(programmer.HostDataDirectoryEnvironment, data)
	configs, roots, err := lifecyclePurgeTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0] != config {
		t.Fatalf("config purge targets=%#v, want exact file %q", configs, config)
	}
	if len(roots) != 1 || roots[0] != data {
		t.Fatalf("data purge roots=%#v, want override %q", roots, data)
	}
}

func TestLifecycleCommandContextIsBounded(t *testing.T) {
	ctx, cancel := lifecycleCommandContextWithTimeout(40 * time.Millisecond)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Second {
		t.Fatalf("lifecycle context deadline=%v ok=%v", deadline, ok)
	}
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("lifecycle context error=%v", ctx.Err())
	}
}
