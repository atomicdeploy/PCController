package control

import (
	"context"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

func TestDevelopmentUploadDisconnectWhileWaitingForProgrammingLock(t *testing.T) {
	for _, method := range []string{"urclock", "usbasp"} {
		t.Run(method, func(t *testing.T) {
			root := t.TempDir()
			firmware := filepath.Join(root, "candidate.hex")
			if err := os.WriteFile(firmware, []byte(":020000000102FB\n:00000001FF\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			paths, err := programmer.HostDataPathsFor(filepath.Join(root, "data"))
			if err != nil {
				t.Fatal(err)
			}
			controller := New(Options{})
			// Snapshot only: no serial handle or goroutine is attached to this
			// sentinel. Disconnect it before any application command can run.
			controller.session = &link.Session{}
			controller.port = ports.Info{Name: "COM18"}
			if !controller.Snapshot().Connected {
				t.Fatal("preflight must start connected")
			}
			controller.programmingMu.Lock()
			locked := true
			defer func() {
				if locked {
					controller.mu.Lock()
					controller.session = nil
					controller.mu.Unlock()
					controller.programmingMu.Unlock()
				}
			}()
			reads, writes := 0, 0
			options := CommandOptions{
				ProgramDataPaths: paths,
				ProgramRunner:    programmer.CommandRunnerFunc(func(context.Context, programmer.Command, io.Writer) error { reads++; return nil }),
				ProgramExecute:   func(context.Context, programmer.Options, io.Writer) error { writes++; return nil },
			}
			args := []string{firmware, "--method", method, "--deployment", "development"}
			if method == "urclock" {
				args = append(args, "COM18")
			}
			finished := make(chan error, 1)
			go func() { _, err := safeFlashCommand(context.Background(), controller, options, args); finished <- err }()
			// Wait for actual mutex contention, not a sleep-based assumption:
			// this also proves the old preflight check has already succeeded.
			deadline := time.Now().Add(10 * time.Second)
			for {
				stack := make([]byte, 64*1024)
				stack = stack[:goruntime.Stack(stack, true)]
				waiting := false
				for _, goroutine := range strings.Split(string(stack), "\n\n") {
					if strings.Contains(goroutine, "control.safeFlashCommand(") && strings.Contains(goroutine, "Mutex).Lock") {
						waiting = true
						break
					}
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("flash command never waited for programming ownership")
				}
				time.Sleep(time.Millisecond)
			}
			controller.mu.Lock()
			controller.session = nil
			controller.mu.Unlock()
			controller.programmingMu.Unlock()
			locked = false
			select {
			case err := <-finished:
				if err == nil || !strings.Contains(err.Error(), "development upload requires an authenticated application") || reads != 0 || writes != 0 {
					t.Fatalf("disconnected development upload: reads=%d writes=%d err=%v", reads, writes, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("disconnected upload did not stop")
			}
		})
	}
}

func TestProgramFlashUsesAutomaticBackupGate(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "firmware.hex")
	const image = ":020000000102FB\n:00000001FF\n"
	if err := os.WriteFile(firmware, []byte(image), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := programmer.HostDataPathsFor(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	runner := programmer.CommandRunnerFunc(func(_ context.Context, command programmer.Command, output io.Writer) error {
		for _, argument := range command.Args {
			for _, prefix := range []string{"-Uflash:r:", "-Ueeprom:r:"} {
				if strings.HasPrefix(argument, prefix) && strings.HasSuffix(argument, ":i") {
					path := strings.TrimSuffix(strings.TrimPrefix(argument, prefix), ":i")
					return os.WriteFile(path, []byte(image), 0o600)
				}
			}
		}
		_, err := io.WriteString(output, "fake AVRDUDE metadata\n")
		return err
	})
	flashed := 0
	options := CommandOptions{
		ProgramDataPaths: paths,
		ProgramRunner:    runner,
		Avrdude:          "fake-avrdude",
		AvrdudeConf:      "fake-avrdude.conf",
		ProgramExecute: func(_ context.Context, options programmer.Options, _ io.Writer) error {
			flashed++
			if options.Method != programmer.MethodUrclock || options.Operation != programmer.OperationWriteFlash || options.HexPath != firmware {
				t.Fatalf("unexpected write options: %#v", options)
			}
			return nil
		},
	}
	output, err := programCommand(
		context.Background(), New(Options{}), options,
		[]string{"flash", firmware, "COM18"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"firmware SHA-256:", "verified backup: firmware-sha256:", "guarded firmware flash completed"} {
		if !strings.Contains(output, expected) {
			t.Errorf("guarded output missing %q:\n%s", expected, output)
		}
	}
	if flashed != 1 {
		t.Fatalf("flash calls=%d", flashed)
	}
}

func TestLegacyDirectFlashCommandsAreUnavailable(t *testing.T) {
	runtime := New(Options{})
	for _, args := range [][]string{
		{"write-flash", "urclock", "firmware.hex", "COM18"},
		{"urclock", "firmware.hex", "COM18"},
	} {
		_, err := programCommand(context.Background(), runtime, CommandOptions{}, args)
		if err == nil || !strings.Contains(err.Error(), "direct flash writes are disabled") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestDevelopmentReinitializeRequiresAuthenticatedLifecycle(t *testing.T) {
	firmware := filepath.Join(t.TempDir(), "firmware.hex")
	if err := os.WriteFile(firmware, []byte(":020000000102FB\n:00000001FF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := programCommand(
		context.Background(), New(Options{}), CommandOptions{},
		[]string{"flash", firmware, "COM18", "--reinitialize-eeprom"},
	)
	if err == nil || !strings.Contains(err.Error(), "authenticated application connection") {
		t.Fatalf("unrecoverable reinitialization was accepted: %v", err)
	}
}

func TestProgrammingReconnectSelectorUsesStableIdentityFirst(t *testing.T) {
	device := ports.Info{
		Name: "COM18", SerialNumber: "BOARD-1",
		InstanceID: `USB\VID_1A86&PID_7523\5&25b7e96&0&11`,
	}
	if got := programmingReconnectSelector(device); got != "instance:"+device.InstanceID {
		t.Fatalf("instance selector = %q", got)
	}
	device.InstanceID = ""
	if got := programmingReconnectSelector(device); got != "serial:BOARD-1" {
		t.Fatalf("serial selector = %q", got)
	}
	device.SerialNumber = ""
	if got := programmingReconnectSelector(device); got != "COM18" {
		t.Fatalf("port selector = %q", got)
	}
}
