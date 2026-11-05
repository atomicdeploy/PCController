//go:build windows

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/ipc"
)

func TestIPCAndStandaloneTypedCommandCapabilities(t *testing.T) {
	cfg := config.Default()
	cfg.UI.AlwaysOnTop = true
	patch := map[string]any{
		"ui": map[string]bool{"always_on_top": true},
	}

	t.Run("IPC", func(t *testing.T) {
		var requests []ipc.Request
		transport := &ipcTransport{
			commandCall: func(
				_ context.Context,
				request ipc.Request,
			) (ipc.Response, error) {
				requests = append(requests, request)
				if request.Method == "ping" {
					return ipc.Success(
						request.ID,
						map[string]string{"instance": "primary"},
					), nil
				}
				return ipc.Success(request.ID, cfg), nil
			},
		}
		assertTypedTransportCapabilities(t, transport, cfg, patch)
		if len(requests) != 3 {
			t.Fatalf("request count = %d, want 3", len(requests))
		}
		assertTypedTransportRequests(t, requests, patch)
	})

	t.Run("standalone", func(t *testing.T) {
		var requests []ipc.Request
		transport := &standaloneTransport{
			commandCall: func(
				_ context.Context,
				request ipc.Request,
			) (any, error) {
				requests = append(requests, request)
				if request.Method == "ping" {
					return map[string]string{"instance": "primary"}, nil
				}
				return cfg, nil
			},
		}
		assertTypedTransportCapabilities(t, transport, cfg, patch)
		if len(requests) != 3 {
			t.Fatalf("request count = %d, want 3", len(requests))
		}
		assertTypedTransportRequests(t, requests, patch)
	})
}

func assertTypedTransportCapabilities(
	t *testing.T,
	transport interface {
		ConfigTransport
		CommandTransport
	},
	want config.Config,
	patch any,
) {
	t.Helper()
	result, err := transport.ExecuteCommand(
		context.Background(),
		"ping",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	var ping map[string]string
	if err := json.Unmarshal(result, &ping); err != nil {
		t.Fatal(err)
	}
	if ping["instance"] != "primary" {
		t.Errorf("ping result = %#v", ping)
	}
	loaded, err := transport.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.AlwaysOnTop != want.UI.AlwaysOnTop {
		t.Errorf("GetConfig AlwaysOnTop = %t", loaded.UI.AlwaysOnTop)
	}
	updated, err := transport.PatchConfig(context.Background(), patch)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UI.AlwaysOnTop != want.UI.AlwaysOnTop {
		t.Errorf("PatchConfig AlwaysOnTop = %t", updated.UI.AlwaysOnTop)
	}
}

func assertTypedTransportRequests(
	t *testing.T,
	requests []ipc.Request,
	patch any,
) {
	t.Helper()
	wantMethods := []string{"ping", "config.get", "config.patch"}
	for index, request := range requests {
		if request.ProtocolVersion != ipc.ProtocolVersion {
			t.Errorf("request %d protocol = %d", index, request.ProtocolVersion)
		}
		if request.ID == "" {
			t.Errorf("request %d has no ID", index)
		}
		if request.Method != wantMethods[index] {
			t.Errorf(
				"request %d method = %q, want %q",
				index,
				request.Method,
				wantMethods[index],
			)
		}
	}
	wantPatch, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	if string(requests[2].Params) != string(wantPatch) {
		t.Errorf(
			"config.patch params = %s, want %s",
			requests[2].Params,
			wantPatch,
		)
	}
}

func TestTransportIdentitySemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport Transport
		wantMode  TransportMode
		wantLabel string
	}{
		{
			name:      "IPC attaches to GUI",
			transport: &ipcTransport{},
			wantMode:  ModeIPC,
			wantLabel: "GUI · named pipe",
		},
		{
			name:      "standalone is headless primary",
			transport: &standaloneTransport{},
			wantMode:  ModeStandalone,
			wantLabel: "headless primary · direct",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.transport.Mode(); got != test.wantMode {
				t.Errorf("Mode() = %q, want %q", got, test.wantMode)
			}
			if got := test.transport.Label(); got != test.wantLabel {
				t.Errorf("Label() = %q, want %q", got, test.wantLabel)
			}
			if test.transport.Events() != nil {
				t.Error("zero-value transport unexpectedly exposes events")
			}
		})
	}
}

func TestSanitizeSelectionErrors(t *testing.T) {
	t.Parallel()

	err := sanitizeSelectionErrors([]error{
		errors.New("IPC unavailable"),
		nil,
		errors.New("API unavailable"),
		errors.New("IPC unavailable"),
		errors.New("  API unavailable  "),
	})
	if err == nil {
		t.Fatal("sanitizeSelectionErrors returned nil")
	}
	message := err.Error()
	if strings.Count(message, "IPC unavailable") != 1 {
		t.Errorf("IPC error appears more than once: %q", message)
	}
	if strings.Count(message, "API unavailable") != 1 {
		t.Errorf("API error appears more than once: %q", message)
	}

	if got := sanitizeSelectionErrors(nil); got == nil ||
		got.Error() != "no transport was available" {
		t.Errorf("empty attempts error = %v", got)
	}
}

func TestExplicitAPIWithCompleteCredentialsDoesNotLoadConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"state":"off"}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(configPath, []byte(`{"not valid"`), 0o600); err != nil {
		t.Fatal(err)
	}

	transport, err := OpenTransport(context.Background(), TransportOptions{
		Mode:           ModeAPI,
		ConfigPath:     configPath,
		APIURL:         server.URL,
		APIToken:       "test-token",
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("OpenTransport loaded an unnecessary configuration: %v", err)
	}
	defer transport.Close()
	if transport.Mode() != ModeAPI {
		t.Fatalf("mode = %q, want %q", transport.Mode(), ModeAPI)
	}
}

func TestDiagnosticCommandMappingUsesCanonicalDispatcherMethod(t *testing.T) {
	t.Parallel()

	request, err := newDiagnosticCommandRequest("mapping-test", "/info")
	if err != nil {
		t.Fatal(err)
	}
	if request.ID != "mapping-test" || request.Method != "diagnostics.fetch" {
		t.Errorf("request mapping = %+v", request)
	}
	var params map[string]string
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params["endpoint"] != "/info" {
		t.Errorf("request params = %#v", params)
	}
}
