package programmer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLibavrdudeAdapterProgressUsesExistingHook(t *testing.T) {
	var output bytes.Buffer
	var progress []Progress
	ctx := WithProgress(context.Background(), func(value Progress) {
		progress = append(progress, value)
	})
	stream := newLibavrdudeEventOutput(ctx, &output, "flash write")
	_, err := stream.Write([]byte(
		"{\"type\":\"message\",\"level\":0,\"message\":\"opening programmer\\n\"}\n" +
			"{\"type\":\"progress\",\"percent\":42,\"detail\":\"Writing ...\",\"finish\":0}\n" +
			"{\"type\":\"result\",\"ok\":true}\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "opening programmer\n" {
		t.Fatalf("unexpected message output %q", output.String())
	}
	if len(progress) != 1 || progress[0].Percent != 42 || progress[0].Stage != "flash write:writing" {
		t.Fatalf("unexpected progress %#v", progress)
	}
	if !stream.resultOK {
		t.Fatal("result event was not observed")
	}
}

func TestBuildPrefersExplicitLibavrdudeAdapter(t *testing.T) {
	directory := t.TempDir()
	adapter := filepath.Join(directory, "pccontroller-avrdude")
	if runtime.GOOS == "windows" {
		adapter += ".exe"
	}
	if err := os.WriteFile(adapter, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	command, err := Build(Options{
		Method: MethodUrclock, Operation: OperationProbe, Port: "COM7",
		MCU: "atmega328p", BaudRate: 115200,
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
		AvrdudeBackend: "library", AvrdudeAdapter: adapter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Name != adapter || command.ProgressProtocol != libavrdudeJSONLProtocol {
		t.Fatalf("unexpected adapter command %#v", command)
	}
	joined := strings.Join(command.Args, " ")
	for _, required := range []string{
		"--part atmega328p", "--programmer urclock", "--port COM7",
		"--extended bootsize=384", "--extended eepromrw",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("adapter command missing %q: %s", required, joined)
		}
	}
}

func TestAvrdudeCLIBackendRejectsAdapter(t *testing.T) {
	_, err := resolveAvrdudeAdapter("cli", "adapter")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestAutoBackendUsesCLIForUrclockMetadata(t *testing.T) {
	directory := t.TempDir()
	adapter := filepath.Join(directory, executableName("pccontroller-avrdude"))
	avrdude := filepath.Join(directory, executableName("avrdude"))
	configuration := filepath.Join(directory, "avrdude.conf")
	for _, path := range []string{adapter, avrdude, configuration} {
		if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	command, err := Build(Options{
		Method: MethodUrclock, Operation: OperationMetadata, Port: "COM7",
		MCU: "atmega328p", AvrdudeBackend: "auto", AvrdudeAdapter: adapter,
		AvrdudeConf: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Name != avrdude || command.ProgressProtocol != "" {
		t.Fatalf("metadata did not select CLI compatibility backend: %#v", command)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-xshowall") {
		t.Fatalf("metadata command missing showall: %s", joined)
	}
}

func TestNonDefaultAVRRequiresExplicitGeometry(t *testing.T) {
	_, err := Build(Options{
		Method: MethodUrclock, Operation: OperationProbe, Port: "COM7",
		MCU: "atmega2560", AvrdudeBackend: "cli",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	})
	if err == nil || !strings.Contains(err.Error(), "bootloader-bytes") {
		t.Fatalf("expected explicit geometry failure, got %v", err)
	}

	command, err := Build(Options{
		Method: MethodUrclock, Operation: OperationProbe, Port: "COM7",
		MCU: "atmega2560", AvrdudeBackend: "cli",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf", BootloaderBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-p atmega2560") &&
		!strings.Contains(joined, "-patmega2560") || !strings.Contains(joined, "-xbootsize=8192") {
		t.Fatalf("unexpected non-default AVR command: %s", joined)
	}
}

// The firmware identity occupies the end of application flash. Urclock metadata
// must not reserve its final byte; this must match the CLI uploader contract.
func TestLibavrdudeUrclockFlashDisablesMetadata(t *testing.T) {
	for _, programmer := range []string{"urclock", "usbasp"} {
		args, err := libavrdudeAdapterArguments(Options{
			MCU: "atmega328p", Operation: OperationWriteFlash,
			HexPath: "firmware.hex", BaudRate: 115200,
		}, "avrdude.conf", programmer)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		got := strings.Contains(joined, "--extended nometadata")
		if got != (programmer == "urclock") {
			t.Fatalf("%s metadata policy: %s", programmer, joined)
		}
	}
}
