package control

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

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
