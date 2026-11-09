package ipcjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestHostSensorsUseExistingReadOnlyRPCAndREST(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	provider := &ipcHostFactsProvider{}
	service := &Service{Client: client, HostFacts: provider}
	for _, method := range []string{"controller.os.facts", "controller.host.facts"} {
		response := service.Dispatch(context.Background(), Request{Method: method, Params: json.RawMessage(`{"profile":"sensors"}`)})
		if response.Error != nil || provider.profile != "sensors" {
			t.Fatalf("response=%v profile=%s", response, provider.profile)
		}
	}
	catalog := service.Dispatch(context.Background(), Request{Method: "controller.os.facts.catalog"})
	if catalog.Error != nil || !strings.Contains(fmt.Sprint(catalog.Result), "HostSensor") {
		t.Fatalf("catalog=%v", catalog)
	}
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/os/facts?profile=sensors")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || provider.profile != "sensors" || !strings.Contains(string(body), `"profile":"sensors"`) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}
