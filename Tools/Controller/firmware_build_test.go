package controller

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/programmer"
)

func TestTypedFirmwareBuildUsesCanonicalProjectAndCorrelatedEvents(t *testing.T) {
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	engine := control.NewCommandEngine(runtime, control.CommandOptions{
		ProjectPath: projectRoot,
		ArduinoCLI:  executable,
		ProgramExecute: func(_ context.Context, options programmer.Options, output io.Writer) error {
			if options.Method != programmer.MethodCompile {
				t.Fatalf("method = %q", options.Method)
			}
			if got := programmer.FirmwareFeatureNames(options.FirmwareFeatures); len(got) != 1 || got[0] != "eeprom-menu-labels" {
				t.Fatalf("features = %v", got)
			}
			_, err := io.WriteString(output, projectRoot+`\Project\PCController.ino: build ok`+"\n")
			return err
		},
	})
	client := AttachSharedRuntime(runtime, engine)
	client.commandOptions.ProjectPath = projectRoot

	result, err := client.BuildFirmware(context.Background(), FirmwareBuildRequest{
		FirmwareFeatures: []string{"eeprom-menu-labels"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.OperationID, "firmware-build-") || len(result.OperationID) != len("firmware-build-")+24 {
		t.Fatalf("operation ID = %q", result.OperationID)
	}
	if strings.Contains(result.Output, projectRoot) || !strings.Contains(result.Output, "<project>") {
		t.Fatalf("typed output was not normalized: %q", result.Output)
	}
	started, err := runtime.WaitEvent(context.Background(), 0, "program.started")
	if err != nil {
		t.Fatal(err)
	}
	if started.Metadata["operation_id"] != result.OperationID {
		t.Fatalf("started metadata = %#v", started.Metadata)
	}
	completed, err := runtime.WaitEvent(context.Background(), started.ID, "program.completed")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Metadata["operation_id"] != result.OperationID {
		t.Fatalf("completed metadata = %#v", completed.Metadata)
	}
}

func TestTypedFirmwareBuildRejectsAmbiguousFeatureSelection(t *testing.T) {
	client := &Client{}
	_, err := client.BuildFirmware(context.Background(), FirmwareBuildRequest{
		FirmwareFeatures: []string{"eeprom-menu-labels"}, NoFirmwareFeatures: true,
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("ambiguous request error = %v", err)
	}
}
