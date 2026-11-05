//go:build windows

package main

import (
	"errors"
	"os"
	"testing"
)

func TestShouldReleaseGUIConsoleOnlyForSoleCurrentProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   uint32
		processes []uint32
		want      bool
	}{
		{
			name:      "private CUI console",
			current:   4200,
			processes: []uint32{4200},
			want:      true,
		},
		{
			name:      "private console with empty slots",
			current:   4200,
			processes: []uint32{0, 4200, 0},
			want:      true,
		},
		{
			name:      "inherited cmd or PowerShell console",
			current:   4200,
			processes: []uint32{1100, 4200},
		},
		{
			name:      "another sole owner",
			current:   4200,
			processes: []uint32{1100},
		},
		{
			name:    "no console",
			current: 4200,
		},
		{
			name:      "invalid current PID",
			processes: []uint32{4200},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldReleaseGUIConsole(
				test.current,
				test.processes,
			); got != test.want {
				t.Fatalf(
					"shouldReleaseGUIConsole(%d, %v) = %v, want %v",
					test.current,
					test.processes,
					got,
					test.want,
				)
			}
		})
	}
}

func TestPrepareGUIConsolePreservesInheritedConsole(t *testing.T) {
	t.Parallel()

	api := &fakeGUIConsoleAPI{
		current:   4200,
		processes: []uint32{1100, 4200},
	}
	prepareGUIConsoleWith(api)

	if api.freeCalls != 0 {
		t.Fatalf("inherited console free calls = %d, want 0", api.freeCalls)
	}
}

func TestPrepareGUIConsoleFreesPrivateConsole(t *testing.T) {
	t.Parallel()

	api := &fakeGUIConsoleAPI{
		current:   4200,
		processes: []uint32{4200},
	}
	prepareGUIConsoleWith(api)

	if api.freeCalls != 1 {
		t.Fatalf("private console free calls = %d, want 1", api.freeCalls)
	}
}

func TestPrepareGUIConsoleHandlesNoConsoleAndFailedFree(t *testing.T) {
	t.Parallel()

	t.Run("no console", func(t *testing.T) {
		t.Parallel()
		api := &fakeGUIConsoleAPI{processErr: errors.New("no console")}
		prepareGUIConsoleWith(api)
		if api.freeCalls != 0 {
			t.Fatalf("no-console free calls = %d, want 0", api.freeCalls)
		}
	})

	t.Run("failed detach remains visible", func(t *testing.T) {
		t.Parallel()
		api := &fakeGUIConsoleAPI{
			current:   4200,
			processes: []uint32{4200},
			freeErr:   errors.New("detach failed"),
		}
		prepareGUIConsoleWith(api)
		if api.freeCalls != 1 {
			t.Fatalf("failed-free calls = %d, want 1", api.freeCalls)
		}
	})
}

func TestStandardFileValidPreservesRedirectedPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	if !standardFileValid(writer) {
		t.Fatal("valid redirected pipe would be replaced by console bootstrap")
	}
}

type fakeGUIConsoleAPI struct {
	current    uint32
	processes  []uint32
	processErr error
	freeErr    error
	freeCalls  int
}

func (api *fakeGUIConsoleAPI) currentProcessID() uint32 {
	return api.current
}

func (api *fakeGUIConsoleAPI) processIDs() ([]uint32, error) {
	return append([]uint32(nil), api.processes...), api.processErr
}

func (api *fakeGUIConsoleAPI) free() error {
	api.freeCalls++
	return api.freeErr
}
