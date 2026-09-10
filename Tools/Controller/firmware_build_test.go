package controller

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestTypedFirmwareBuildFailureRetainsCorrelationAndSanitizes(t *testing.T) {
	projectRoot, _ := filepath.Abs(filepath.Join("..", ".."))
	executable, _ := os.Executable()
	for _, preflight := range []bool{false, true} {
		runtime := control.New(control.Options{})
		options := control.CommandOptions{ProjectPath: projectRoot, ArduinoCLI: executable,
			ProgramExecute: func(_ context.Context, _ programmer.Options, output io.Writer) error {
				_, _ = io.WriteString(output, projectRoot+"/source.cpp: compile failed\n")
				return errors.New(projectRoot + "/source.cpp: compiler failure")
			}}
		if preflight {
			options.FirmwareFeaturesError = errors.New(projectRoot + "/profile: invalid feature selection")
		}
		client := AttachSharedRuntime(runtime, control.NewCommandEngine(runtime, options))
		client.commandOptions = options
		result, err := client.BuildFirmware(context.Background(), FirmwareBuildRequest{})
		if err == nil || result.OperationID == "" || strings.Contains(err.Error(), projectRoot) || strings.Contains(result.Output, projectRoot) {
			t.Fatalf("preflight=%v result=%#v err=%v", preflight, result, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		started, startErr := runtime.WaitEvent(ctx, 0, "program.started")
		failed, failErr := runtime.WaitEvent(ctx, started.ID, "program.failed")
		cancel()
		if startErr != nil || failErr != nil || failed.Metadata["operation_id"] != result.OperationID || strings.Contains(failed.Metadata["error"], projectRoot) {
			t.Fatalf("started=%#v failed=%#v errors=%v/%v", started, failed, startErr, failErr)
		}
		_ = client.Shutdown()
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
