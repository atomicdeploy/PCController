//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnixHostInstanceLockRecoversStaleMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-instance.lock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	first, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("acquire stale marker: acquired=%t err=%v", acquired, err)
	}
	second, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || acquired || second != nil {
		t.Fatalf("concurrent acquire: lock=%v acquired=%t err=%v", second, acquired, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	third, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("reacquire after close: acquired=%t err=%v", acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}
