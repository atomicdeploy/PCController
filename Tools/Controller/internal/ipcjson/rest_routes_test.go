package ipcjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/releaseplane"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/webui"
)

type inventoryRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip inventoryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

// TestCanonicalRESTRouteInventory exercises every implemented REST group through
// the real top-level multiplexer. A missing registration therefore fails here
// before a browser client, peer, or updater can silently drift from the server.
func TestCanonicalRESTRouteInventory(t *testing.T) {
	store, err := artifacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()

	upload, err := artifactService.UploadOperation(
		strings.NewReader(":00000001FF\n"),
		artifacts.PutOptions{Kind: artifacts.KindFirmware, Name: "inventory.hex"},
	)
	if err != nil {
		t.Fatal(err)
	}
	releaseClient := releaseplane.NewClient(&http.Client{Transport: inventoryRoundTripper(
		func(*http.Request) (*http.Response, error) {
			return nil, errors.New("route inventory transport is intentionally offline")
		},
	)})
	releaseService, err := releaseplane.NewService(releaseClient, artifactService, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseService.Close()
	stage, err := releaseService.StartStage(releaseplane.StageRequest{Candidate: releaseplane.Candidate{
		Kind: artifacts.KindFirmware, Name: "inventory.hex",
		URL: "https://inventory.invalid/inventory.hex",
	}})
	if err != nil {
		t.Fatal(err)
	}

	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, control.NewCommandEngine(runtime, control.CommandOptions{}))
	config := appconfig.Defaults()
	config.Integrations.InboundWebhooksEnabled = true
	service := &Service{
		Client: client,
		HostConfig: func() appconfig.Config {
			return config
		},
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
		Artifacts:        artifactService,
		ReleaseDiscovery: releaseService,
		IntegrationProxy: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
		InboundWebhooks: true,
		WebUI:           webui.Handler("/ipc"),
	}
	handler := websocketMux(context.Background(), service)

	routes := []struct {
		name, method, path, body string
	}{
		{name: "browser bootstrap", method: http.MethodGet, path: "/api/v1/ui-config"},
		{name: "JSON-RPC", method: http.MethodPost, path: "/api/v1/rpc", body: `{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`},
		{name: "snapshot", method: http.MethodGet, path: "/api/v1/snapshot"},
		{name: "peripheral catalog", method: http.MethodGet, path: "/api/v1/peripherals"},
		{name: "peripheral names", method: http.MethodPut, path: "/api/v1/peripherals", body: `{"peripheral_names":{"relay.5":"Inventory relay"}}`},
		{name: "PWM values", method: http.MethodGet, path: "/api/v1/pwm"},
		{name: "PWM mutation", method: http.MethodPut, path: "/api/v1/pwm", body: `{"channel":0,"value":0}`},
		{name: "command catalog", method: http.MethodGet, path: "/api/v1/commands"},
		{name: "program state", method: http.MethodGet, path: "/api/v1/program-state"},
		{name: "menu catalog", method: http.MethodGet, path: "/api/v1/menu/catalog"},
		{name: "menu layout", method: http.MethodGet, path: "/api/v1/menu/layout"},
		{name: "host menus", method: http.MethodGet, path: "/api/v1/host-menus"},
		{name: "OS status", method: http.MethodGet, path: "/api/v1/os/status"},
		{name: "OS facts", method: http.MethodGet, path: "/api/v1/os/facts?profile=list"},
		{name: "virtual key", method: http.MethodPost, path: "/api/v1/os/key", body: `{}`},
		{name: "power action", method: http.MethodPost, path: "/api/v1/os/power", body: `{}`},
		{name: "command", method: http.MethodPost, path: "/api/v1/command", body: `{"command":"status"}`},
		{name: "message", method: http.MethodPost, path: "/api/v1/messages", body: `{"source":"client","target":"host","type":"operator.notice","text":"inventory"}`},
		{name: "bridge list", method: http.MethodGet, path: "/api/v1/bridges"},
		{name: "bridge call", method: http.MethodPost, path: "/api/v1/bridges/call", body: `{}`},
		{name: "webhook", method: http.MethodPost, path: "/api/v1/webhooks/inbound", body: `{"source":"client","target":"host","type":"operator.notice","text":"inventory"}`},
		{name: "integration proxy", method: http.MethodGet, path: "/api/v1/integrations/datahub/v1/status"},
		{name: "artifact manifest", method: http.MethodGet, path: "/api/v1/artifacts/manifest"},
		{name: "artifact list", method: http.MethodGet, path: "/api/v1/artifacts"},
		{name: "artifact upload", method: http.MethodPost, path: "/api/v1/artifacts/upload", body: ""},
		{name: "artifact fetch", method: http.MethodPost, path: "/api/v1/artifacts/fetch", body: `{}`},
		{name: "artifact capture", method: http.MethodPost, path: "/api/v1/artifacts/capture", body: `{}`},
		{name: "artifact download", method: http.MethodGet, path: upload.Artifact.DownloadURL},
		{name: "firmware update", method: http.MethodPost, path: "/api/v1/updates/firmware", body: `{}`},
		{name: "EEPROM update", method: http.MethodPost, path: "/api/v1/updates/eeprom", body: `{}`},
		{name: "host update", method: http.MethodPost, path: "/api/v1/updates/host", body: `{}`},
		{name: "flash restore", method: http.MethodPost, path: "/api/v1/restores/flash", body: `{}`},
		{name: "update status", method: http.MethodGet, path: "/api/v1/updates/status/" + upload.Operation.ID},
		{name: "workflow discovery", method: http.MethodPost, path: "/api/v1/discovery/github/workflow", body: `{}`},
		{name: "release discovery", method: http.MethodPost, path: "/api/v1/discovery/github/release", body: `{}`},
		{name: "local manifest", method: http.MethodGet, path: "/api/v1/discovery/manifest"},
		{name: "remote manifest", method: http.MethodPost, path: "/api/v1/discovery/manifest", body: `{}`},
		{name: "update comparison", method: http.MethodPost, path: "/api/v1/discovery/check", body: `{}`},
		{name: "artifact staging", method: http.MethodPost, path: "/api/v1/discovery/stage", body: `{}`},
		{name: "discovery status", method: http.MethodGet, path: "/api/v1/discovery/status/" + stage.Operation.ID},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			request.RemoteAddr = "127.0.0.1:43210"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusNotFound {
				t.Fatalf("canonical route %s %s was not registered: %s", route.method, route.path, response.Body.String())
			}
		})
	}

	for _, route := range routes {
		if !strings.HasPrefix(route.path, "/api/v1/") {
			continue
		}
		t.Run("reject versionless "+route.name, func(t *testing.T) {
			alias := "/api/" + strings.TrimPrefix(route.path, "/api/v1/")
			request := httptest.NewRequest(route.method, alias, strings.NewReader(route.body))
			request.RemoteAddr = "127.0.0.1:43210"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if (response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed) ||
				response.Header().Get("Location") != "" {
				t.Fatalf("versionless alias %s %s status=%d location=%q body=%s", route.method, alias, response.Code, response.Header().Get("Location"), response.Body.String())
			}
		})
	}
}

func TestVersionlessRESTPreflightIsRejected(t *testing.T) {
	runtime := control.New(control.Options{})
	handler := websocketMux(context.Background(), &Service{
		Client:         controllerapi.AttachSharedRuntime(runtime, shell.New(8)),
		AllowedOrigins: []string{"console.example:*"},
		WebUI:          webui.Handler("/ipc"),
	})
	request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8787/api/rpc", nil)
	request.RemoteAddr = "127.0.0.1:43210"
	request.Header.Set("Origin", "https://console.example:9443")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if (response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed) ||
		response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("versionless preflight status=%d origin=%q body=%s", response.Code, response.Header().Get("Access-Control-Allow-Origin"), response.Body.String())
	}
}
