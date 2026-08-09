//go:build linux

package pcspeaker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLinuxConsoleSpeakerProgramsAndStopsTone(t *testing.T) {
	var values []int
	var opened []string
	closed := 0
	operations := linuxSpeakerOperations{
		open: func(path string, _ int, _ uint32) (int, error) {
			opened = append(opened, path)
			if path == "/missing" {
				return -1, errors.New("missing")
			}
			return 37, nil
		},
		ioctl: func(fd int, request uint, value int) error {
			if fd != 37 || request != linuxKDMKTONE {
				t.Fatalf("ioctl fd=%d request=%#x", fd, request)
			}
			values = append(values, value)
			return nil
		},
		close: func(fd int) error {
			if fd != 37 {
				t.Fatalf("close fd=%d", fd)
			}
			closed++
			return nil
		},
	}
	if err := playLinuxConsoleSpeaker(
		context.Background(), []string{"/missing", "/console"}, operations, 440, 1,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened, []string{"/missing", "/console"}) {
		t.Fatalf("opened=%q", opened)
	}
	wantArgument := (1 << 16) | 2711
	if !reflect.DeepEqual(values, []int{wantArgument, 0}) || closed != 1 {
		t.Fatalf("ioctl values=%v closed=%d", values, closed)
	}
}

func TestLinuxConsoleSpeakerKernelDurationAndCleanupErrors(t *testing.T) {
	operations := linuxSpeakerOperations{
		open: func(string, int, uint32) (int, error) { return 19, nil },
		ioctl: func(_ int, _ uint, value int) error {
			if value == 0 {
				return errors.New("stop failed")
			}
			return nil
		},
		close: func(int) error { return errors.New("close failed") },
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := playLinuxConsoleSpeaker(ctx, []string{"/console"}, operations, 440, 60_000)
	if err == nil || !strings.Contains(err.Error(), "stop failed") || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("cleanup errors were lost: %v", err)
	}
}

func TestLinuxConsoleSpeakerReportsPermissionAndBackendFailures(t *testing.T) {
	operations := linuxSpeakerOperations{
		open: func(path string, _ int, _ uint32) (int, error) {
			return -1, errors.New("permission denied")
		},
		ioctl: func(int, uint, int) error { return nil },
		close: func(int) error { return nil },
	}
	err := playLinuxConsoleSpeaker(
		context.Background(), []string{"/dev/console"}, operations, 440, 1,
	)
	if err == nil || !strings.Contains(err.Error(), "pcspkr") ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unhelpful Linux speaker failure: %v", err)
	}
}

func TestLinuxPlayDoesNotRequireWinRing0Directory(t *testing.T) {
	if driverDirectoryRequired() {
		t.Fatal("Linux unexpectedly requires a WinRing0 directory")
	}
}
