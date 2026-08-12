package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func newIPCArtifactService(t *testing.T) (*artifacts.Service, *controllerapi.Client) {
	t.Helper()
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	client := controllerapi.AttachIsolatedRuntime(runtime, shell.New(8))
	t.Cleanup(func() {
		service.Close()
		_ = client.Shutdown()
	})
	return service, client
}

func TestArtifactRPCUsesTypedPrimaryService(t *testing.T) {
	artifactService, client := newIPCArtifactService(t)
	service := &Service{Client: client, Artifacts: artifactService}
	response := service.Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.artifact.manifest",
	})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	manifest, ok := response.Result.(artifacts.Manifest)
	if !ok || !manifest.Enabled || !manifest.Policy.ExplicitAuthorizationRequired {
		t.Fatalf("manifest=%#v", response.Result)
	}
	params, _ := json.Marshal(artifacts.CaptureRequest{Authorized: false})
	denied := service.Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.artifact.capture", Params: params,
	})
	if denied.Error == nil || !strings.Contains(denied.Error.Message, "authorization") {
		t.Fatalf("capture=%#v", denied)
	}
}

func TestCapturedFlashRestoreRPCRequiresProgrammingCapability(t *testing.T) {
	if got := requestCapability("controller.restore.flash", nil); got != capabilityProgramming {
		t.Fatalf("capability=%q want %q", got, capabilityProgramming)
	}
}

func TestArtifactHTTPRequiresRemoteAuthenticationAndProgrammingPolicy(t *testing.T) {
	artifactService, client := newIPCArtifactService(t)
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	service := &Service{
		Client: client, Artifacts: artifactService,
		HostConfig: func() appconfig.Config { return config },
	}
	handler := websocketMux(context.Background(), service)

	request := httptest.NewRequest(http.MethodGet, "http://controller.example/api/artifacts/manifest", nil)
	request.RemoteAddr = "198.51.100.10:43100"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "authentication token") {
		t.Fatalf("anonymous remote status=%d body=%s", response.Code, response.Body.String())
	}

	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	request = httptest.NewRequest(http.MethodGet, "http://controller.example/api/artifacts/manifest", nil)
	request.RemoteAddr = "198.51.100.10:43100"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated read status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodPost, "http://controller.example/api/artifacts/capture",
		strings.NewReader(`{"components":["flash","eeprom"],"authorized":true}`),
	)
	request.RemoteAddr = "198.51.100.10:43100"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "programming") {
		t.Fatalf("remote capture status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodPost, "http://controller.example/api/restores/flash",
		strings.NewReader(`{"artifact_sha256":"`+strings.Repeat("a", 64)+`","authorized":true}`),
	)
	request.RemoteAddr = "198.51.100.10:43100"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "programming") {
		t.Fatalf("remote flash restore status=%d body=%s", response.Code, response.Body.String())
	}
}
