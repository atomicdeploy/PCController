package ipcjson

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestPrimaryPingBypassesSerializedDeviceOperation(t *testing.T) {
	service := &Service{
		Client: controllerapi.AttachSharedRuntime(
			control.New(control.Options{}),
			shell.New(1),
		),
		HostInstanceID: "primary-test",
		HostProcessID:  36152,
		HostSurface:    "web",
	}
	service.mu.Lock()
	responses := make(chan Response, 1)
	go func() {
		responses <- service.Dispatch(context.Background(), Request{
			JSONRPC: Version,
			ID:      json.RawMessage("1"),
			Method:  "controller.ping",
		})
	}()

	select {
	case response := <-responses:
		service.mu.Unlock()
		if response.Error != nil {
			t.Fatalf("ping error=%v", response.Error)
		}
		encoded, err := json.Marshal(response.Result)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			OK        bool   `json:"ok"`
			ProcessID int    `json:"process_id"`
			Instance  string `json:"instance_id"`
		}
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if !result.OK || result.ProcessID != 36152 || result.Instance != "primary-test" {
			t.Fatalf("ping result=%+v", result)
		}
	case <-time.After(100 * time.Millisecond):
		service.mu.Unlock()
		<-responses
		t.Fatal("primary ping waited behind the serialized operation lock")
	}
}
