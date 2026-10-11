//go:build linux

package pcspeaker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestExternalBeepArgumentsLinux(t *testing.T) {
	want := []string{"-f", "880", "-l", "250"}
	if got := externalBeepArguments(880, 250); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments=%q want %q", got, want)
	}
}

func TestCanceledExternalBeepIssuesLinuxEmergencyStop(t *testing.T) {
	original := externalBeepCommand
	originalStop := externalBeepStop
	defer func() {
		externalBeepCommand = original
		externalBeepStop = originalStop
	}()
	externalBeepStop = stopExternalBeep

	var lock sync.Mutex
	var calls [][]string
	externalBeepCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		lock.Lock()
		calls = append(calls, append([]string(nil), args...))
		call := len(calls)
		lock.Unlock()
		if call == 1 {
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestExternalBeepDeadlineHelper")
			command.Env = append(os.Environ(), "PCCONTROLLER_BEEP_DEADLINE_HELPER=1")
			return command
		}
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := playExternalBeep(ctx, "", os.Args[0], 440, 5_000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("external beep error=%v, want context deadline exceeded", err)
	}
	lock.Lock()
	defer lock.Unlock()
	want := [][]string{{"-f", "440", "-l", "5000"}, {"-f", "0", "-l", "0"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("external beep calls=%q want %q", calls, want)
	}
}
