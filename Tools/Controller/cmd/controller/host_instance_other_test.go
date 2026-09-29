//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const unixHostInstanceLockWaiterEnvironment = "PCCONTROLLER_TEST_HOST_LOCK_WAITER"

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
	firstInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUnixHostInstanceLockWaiterHelper$")
	command.Env = append(os.Environ(), unixHostInstanceLockWaiterEnvironment+"="+path)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiterRunning := true
	defer func() {
		if waiterRunning {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	expectUnixLockHelperLine(t, reader, &stderr, "ready")

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release waiting contender: %v", err)
	}
	expectUnixLockHelperLine(t, reader, &stderr, "acquired")

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
	if !os.SameFile(firstInfo, pathInfo) {
		t.Fatal("lock path changed inode during ownership handoff")
	}

	if err := command.Process.Kill(); err != nil {
		t.Fatalf("force waiting owner exit: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("force-exited waiting owner returned success")
	}
	waiterRunning = false
	_ = stdin.Close()
	fourth, acquired, err := platformTryHostInstanceLock("unused", path)
	if err != nil || !acquired {
		t.Fatalf("acquire after forced waiter exit: acquired=%t err=%v", acquired, err)
	}
	if err := fourth.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixHostInstanceLockWaiterHelper(t *testing.T) {
	path := strings.TrimSpace(os.Getenv(unixHostInstanceLockWaiterEnvironment))
	if path == "" {
		t.Skip("helper process only")
	}
	waiter, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		t.Fatal(err)
	}
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(waiter.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("waiting contender acquire: %v", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "acquired"); err != nil {
		t.Fatal(err)
	}
	select {}
}

func expectUnixLockHelperLine(
	t *testing.T,
	reader *bufio.Reader,
	stderr *bytes.Buffer,
	want string,
) {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read lock helper %q: %v; stderr=%s", want, err, stderr.String())
	}
	if got := strings.TrimSpace(line); got != want {
		t.Fatalf("lock helper line=%q want=%q; stderr=%s", got, want, stderr.String())
	}
}
