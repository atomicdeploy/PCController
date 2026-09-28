package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestFirmwareBuildRPCErrorRetainsSanitizedOperation(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	engine := shell.New(8)
	home, _ := os.UserHomeDir()
	if err := engine.Register(shell.Command{Name: "program", Run: func(context.Context, []string) (string, error) {
		return home + "/build: diagnostic", errors.New(home + "/compiler: failed")
	}}); err != nil {
		t.Fatal(err)
	}
	client := controllerapi.AttachSharedRuntime(runtime, engine)
	service := &Service{Client: client, AuthorizationDisabled: true}
	response := service.Dispatch(context.Background(), Request{Method: "controller.firmware.build", Params: json.RawMessage("{}")})
	if response.Error == nil {
		t.Fatalf("missing RPC error: %#v", response)
	}
	data, ok := response.Error.Data.(controllerapi.FirmwareBuildResult)
	if !ok || data.OperationID == "" {
		t.Fatalf("lost correlated error data: %#v", response.Error)
	}
	if strings.Contains(response.Error.Message, home) || strings.Contains(data.Output, home) {
		t.Fatalf("private path leaked: %#v %#v", response.Error, data)
	}
}
