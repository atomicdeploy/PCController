//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
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
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persistent lock marker after close: %v", err)
	}

	third, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("reacquire after close: acquired=%t err=%v", acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixHostInstanceLockKeepsOneInodeAcrossHandoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-instance.lock")
	first, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%t err=%v", acquired, err)
	}

	waiter, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	waiterOpen := true
	defer func() {
		if waiterOpen {
			_ = waiter.Close()
		}
	}()
	waiterInfo, err := waiter.Stat()
	if err != nil {
		t.Fatal(err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(waiter.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("waiting contender acquire: %v", err)
	}

	third, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || acquired || third != nil {
		if third != nil {
			_ = third.Close()
		}
		t.Fatalf("third contender while waiter owns original inode: lock=%v acquired=%t err=%v", third, acquired, err)
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("persistent lock marker during handoff: %v", err)
	}
	if !os.SameFile(waiterInfo, pathInfo) {
		t.Fatal("lock path changed inode during ownership handoff")
	}

	if err := waiter.Close(); err != nil {
		t.Fatal(err)
	}
	waiterOpen = false
	fourth, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("acquire after waiter close: acquired=%t err=%v", acquired, err)
	}
	if err := fourth.Close(); err != nil {
		t.Fatal(err)
	}
}
