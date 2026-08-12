package ipcjson

import (
	"context"
	"encoding/json"
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

func TestMacroRESTCRUDUsesSharedTypedLibrary(t *testing.T) {
	client := controllerapi.New(controllerapi.Options{})
	defer client.Close()
	handler := websocketMux(context.Background(), &Service{
		Client: client,
		WebUI:  webui.Handler("/ipc"),
	})

	request := func(method, body string, wantStatus int) controllerapi.MacroSnapshot {
		t.Helper()
		httpRequest := httptest.NewRequest(method, "/api/macros", strings.NewReader(body))
		httpRequest.RemoteAddr = "127.0.0.1:43210"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httpRequest)
		if response.Code != wantStatus {
			t.Fatalf("%s /api/macros status=%d want=%d body=%s", method, response.Code, wantStatus, response.Body.String())
		}
		var snapshot controllerapi.MacroSnapshot
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatalf("decode %s macro snapshot: %v", method, err)
		}
		return snapshot
	}

	created := request(http.MethodPost, `{"id":42,"name":"Door close","category":"motion","color":"purple"}`, http.StatusCreated)
	if len(created.Library) != 1 || created.Library[0].ID != 42 ||
		created.Library[0].Name != "Door close" || created.Library[0].Category != "motion" {
		t.Fatalf("unexpected created library: %+v", created.Library)
	}

	category := "maintenance"
	updated := request(http.MethodPatch, `{"reference":"42","name":"Door park","category":"maintenance"}`, http.StatusOK)
	if len(updated.Library) != 1 || updated.Library[0].Name != "Door park" ||
		updated.Library[0].Category != category {
		t.Fatalf("unexpected updated library: %+v", updated.Library)
	}

	listed := request(http.MethodGet, "", http.StatusOK)
	if len(listed.Library) != 1 || listed.Library[0].Name != "Door park" {
		t.Fatalf("GET did not return authoritative macro library: %+v", listed.Library)
	}

	deleted := request(http.MethodDelete, `{"reference":"42"}`, http.StatusOK)
	if len(deleted.Library) != 0 {
		t.Fatalf("DELETE retained macro library entries: %+v", deleted.Library)
	}
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
	releaseClient := releaseplane.NewTrustedClient(&http.Client{Transport: inventoryRoundTripper(
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
		WebhookAdmin: func() WebhookAdminService {
			return &fakeWebhookAdmin{}
		},
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
		{name: "browser bootstrap", method: http.MethodGet, path: "/api/ui-config"},
		{name: "JSON-RPC", method: http.MethodPost, path: "/api/rpc", body: `{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`},
		{name: "snapshot", method: http.MethodGet, path: "/api/snapshot"},
		{name: "peripheral catalog", method: http.MethodGet, path: "/api/peripherals"},
		{name: "peripheral names", method: http.MethodPut, path: "/api/peripherals", body: `{"peripheral_names":{"relay.5":"Inventory relay"}}`},
		{name: "PWM values", method: http.MethodGet, path: "/api/pwm"},
		{name: "PWM mutation", method: http.MethodPut, path: "/api/pwm", body: `{"channel":0,"value":0}`},
		{name: "command catalog", method: http.MethodGet, path: "/api/commands"},
		{name: "program state", method: http.MethodGet, path: "/api/program-state"},
		{name: "menu catalog", method: http.MethodGet, path: "/api/menu/catalog"},
		{name: "menu layout", method: http.MethodGet, path: "/api/menu/layout"},
		{name: "host menus", method: http.MethodGet, path: "/api/host-menus"},
		{name: "OS status", method: http.MethodGet, path: "/api/os/status"},
		{name: "OS facts", method: http.MethodGet, path: "/api/os/facts?profile=list"},
		{name: "virtual key", method: http.MethodPost, path: "/api/os/key", body: `{}`},
		{name: "power action", method: http.MethodPost, path: "/api/os/power", body: `{}`},
		{name: "command", method: http.MethodPost, path: "/api/command", body: `{"command":"status"}`},
		{name: "message", method: http.MethodPost, path: "/api/messages", body: `{"source":"client","target":"host","type":"operator.notice","text":"inventory"}`},
		{name: "macro snapshot", method: http.MethodGet, path: "/api/macros"},
		{name: "macro create", method: http.MethodPost, path: "/api/macros", body: `{"id":254,"name":"Inventory"}`},
		{name: "macro update", method: http.MethodPatch, path: "/api/macros", body: `{"reference":"254","name":"Inventory updated"}`},
		{name: "macro delete", method: http.MethodDelete, path: "/api/macros", body: `{"reference":"254"}`},
		{name: "macro record start", method: http.MethodPost, path: "/api/macros/recording", body: `{"name":"Inventory recording"}`},
		{name: "macro record stop", method: http.MethodDelete, path: "/api/macros/recording", body: `{"save":false}`},
		{name: "board macro record start", method: http.MethodPost, path: "/api/macros/board-recording", body: `{"id":253}`},
		{name: "board macro record stop", method: http.MethodDelete, path: "/api/macros/board-recording"},
		{name: "board macro clear", method: http.MethodPost, path: "/api/macros/board-recording/clear", body: `{}`},
		{name: "macro playback", method: http.MethodPost, path: "/api/macros/playback", body: `{"reference":"Inventory"}`},
		{name: "macro playback cancel", method: http.MethodDelete, path: "/api/macros/playback", body: `{}`},
		{name: "display", method: http.MethodPost, path: "/api/display", body: `{"target":"segments","text":"TEST","repeat":"once"}`},
		{name: "app action", method: http.MethodPost, path: "/api/app/action", body: `{"kind":"app.progress","value":"normal 42","target":"tui"}`},
		{name: "bridge list", method: http.MethodGet, path: "/api/bridges"},
		{name: "bridge call", method: http.MethodPost, path: "/api/bridges/call", body: `{}`},
		{name: "outbound webhook status", method: http.MethodGet, path: "/api/webhooks/outbound/status"},
		{name: "outbound webhook pending", method: http.MethodGet, path: "/api/webhooks/outbound/pending"},
		{name: "outbound webhook dead", method: http.MethodGet, path: "/api/webhooks/outbound/dead"},
		{name: "outbound webhook replay", method: http.MethodPost, path: "/api/webhooks/outbound/replay", body: `{"delivery_id":"inventory"}`},
		{name: "outbound webhook clear", method: http.MethodPost, path: "/api/webhooks/outbound/clear", body: `{"delivery_id":"inventory"}`},
		{name: "webhook", method: http.MethodPost, path: "/api/webhooks/inbound", body: `{"source":"client","target":"host","type":"operator.notice","text":"inventory"}`},
		{name: "integration proxy", method: http.MethodGet, path: "/api/integrations/datahub/v1/status"},
		{name: "artifact manifest", method: http.MethodGet, path: "/api/artifacts/manifest"},
		{name: "artifact list", method: http.MethodGet, path: "/api/artifacts"},
		{name: "artifact upload", method: http.MethodPost, path: "/api/artifacts/upload", body: ""},
		{name: "artifact fetch", method: http.MethodPost, path: "/api/artifacts/fetch", body: `{}`},
		{name: "artifact capture", method: http.MethodPost, path: "/api/artifacts/capture", body: `{}`},
		{name: "artifact download", method: http.MethodGet, path: upload.Artifact.DownloadURL},
		{name: "firmware update", method: http.MethodPost, path: "/api/updates/firmware", body: `{}`},
		{name: "EEPROM update", method: http.MethodPost, path: "/api/updates/eeprom", body: `{}`},
		{name: "host update", method: http.MethodPost, path: "/api/updates/host", body: `{}`},
		{name: "flash restore", method: http.MethodPost, path: "/api/restores/flash", body: `{}`},
		{name: "update status", method: http.MethodGet, path: "/api/updates/status/" + upload.Operation.ID},
		{name: "workflow discovery", method: http.MethodPost, path: "/api/discovery/github/workflow", body: `{}`},
		{name: "release discovery", method: http.MethodPost, path: "/api/discovery/github/release", body: `{}`},
		{name: "local manifest", method: http.MethodGet, path: "/api/discovery/manifest"},
		{name: "remote manifest", method: http.MethodPost, path: "/api/discovery/manifest", body: `{}`},
		{name: "update comparison", method: http.MethodPost, path: "/api/discovery/check", body: `{}`},
		{name: "artifact staging", method: http.MethodPost, path: "/api/discovery/stage", body: `{}`},
		{name: "discovery status", method: http.MethodGet, path: "/api/discovery/status/" + stage.Operation.ID},
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
		if !strings.HasPrefix(route.path, "/api/") {
			continue
		}
		t.Run("reject versioned "+route.name, func(t *testing.T) {
			alias := "/api/v1/" + strings.TrimPrefix(route.path, "/api/")
			request := httptest.NewRequest(route.method, alias, strings.NewReader(route.body))
			request.RemoteAddr = "127.0.0.1:43210"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if (response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed) ||
				response.Header().Get("Location") != "" {
				t.Fatalf("versioned alias %s %s status=%d location=%q body=%s", route.method, alias, response.Code, response.Header().Get("Location"), response.Body.String())
			}
		})
	}
}

func TestVersionedRESTPreflightIsRejected(t *testing.T) {
	runtime := control.New(control.Options{})
	handler := websocketMux(context.Background(), &Service{
		Client:         controllerapi.AttachSharedRuntime(runtime, shell.New(8)),
		AllowedOrigins: []string{"console.example:*"},
		WebUI:          webui.Handler("/ipc"),
	})
	request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8787/api/v1/rpc", nil)
	request.RemoteAddr = "127.0.0.1:43210"
	request.Header.Set("Origin", "https://console.example:9443")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if (response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed) ||
		response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("versioned preflight status=%d origin=%q body=%s", response.Code, response.Header().Get("Access-Control-Allow-Origin"), response.Body.String())
	}
}
