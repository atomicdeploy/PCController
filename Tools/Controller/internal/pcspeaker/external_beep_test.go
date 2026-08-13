package pcspeaker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestExternalBeepPreservesContextDeadline(t *testing.T) {
	original := externalBeepCommand
	defer func() { externalBeepCommand = original }()
	externalBeepCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestExternalBeepDeadlineHelper")
		command.Env = append(os.Environ(), "PCCONTROLLER_BEEP_DEADLINE_HELPER=1")
		return command
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := playExternalBeep(ctx, "", os.Args[0], 440, 5_000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("external beep error=%v, want context deadline exceeded", err)
	}
}

func TestExternalBeepDeadlineHelper(t *testing.T) {
	if os.Getenv("PCCONTROLLER_BEEP_DEADLINE_HELPER") != "1" {
		return
	}
	time.Sleep(5 * time.Second)
	os.Exit(0)
}
