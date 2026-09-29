package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/releaseplane"
)

func TestReleaseDiscoveryRPCAndRemotePolicy(t *testing.T) {
	artifactService, client := newIPCArtifactService(t)
	manifestServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"artifacts": []map[string]any{{
				"kind": "firmware", "name": "board.hex",
				"url": "/board.hex", "packed_timestamp": 1234,
			}},
		})
	}))
	defer manifestServer.Close()
	discovery, err := releaseplane.NewService(releaseplane.NewTrustedClient(manifestServer.Client()), artifactService, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = discovery.Close() })
	service := &Service{Client: client, Artifacts: artifactService, ReleaseDiscovery: discovery}
	params, _ := json.Marshal(releaseplane.ManifestRequest{URL: manifestServer.URL + "/manifest.json"})
	response := service.Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.discovery.manifest", Params: params,
	})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	result, ok := response.Result.(releaseplane.DiscoveryResult)
	if !ok || len(result.Candidates) != 1 || result.Candidates[0].PackedTimestamp != 1234 {
		t.Fatalf("discovery result=%#v", response.Result)
	}
	if got := requestCapability("controller.discovery.stage", nil); got != capabilityProgramming {
		t.Fatalf("stage capability=%q", got)
	}

	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	service.HostConfig = func() appconfig.Config { return config }
	handler := websocketMux(context.Background(), service)
	request := httptest.NewRequest(http.MethodPost, "http://controller.example/api/discovery/manifest", strings.NewReader(`{"url":"`+manifestServer.URL+`/manifest.json"}`))
	request.RemoteAddr = "198.51.100.10:43100"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remote discovery status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://controller.example/api/discovery/stage", strings.NewReader(`{"candidate":{"source":"manifest","kind":"firmware","name":"board.hex","url":"`+manifestServer.URL+`/board.hex"}}`))
	request.RemoteAddr = "198.51.100.10:43100"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "programming") {
		t.Fatalf("remote stage status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReleaseHTTPAlphaAuthorizationHonorsRemoteExposure(t *testing.T) {
	artifactService, client := newIPCArtifactService(t)
	manifestServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"artifacts": []map[string]any{{
				"kind": "firmware", "name": "board.hex",
				"url": "/board.hex", "packed_timestamp": 1234,
			}},
		})
	}))
	defer manifestServer.Close()
	discovery, err := releaseplane.NewService(releaseplane.NewTrustedClient(manifestServer.Client()), artifactService, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = discovery.Close() })
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	service := &Service{
		Client: client, Artifacts: artifactService, ReleaseDiscovery: discovery,
		AuthorizationDisabled: true,
		HostConfig:            func() appconfig.Config { return config },
	}
	handler := websocketMux(context.Background(), service)
	request := func(path, body string) *http.Request {
		value := httptest.NewRequest(
			http.MethodPost,
			"http://controller.example"+path,
			strings.NewReader(body),
		)
		value.RemoteAddr = "198.51.100.10:43100"
		return value
	}
	manifestRequest := func() *http.Request {
		return request("/api/discovery/manifest", `{"url":"`+manifestServer.URL+`/manifest.json"}`)
	}
	stageRequest := func() *http.Request {
		return request(
			"/api/discovery/stage",
			`{"candidate":{"source":"manifest","kind":"firmware","name":"board.hex","url":"`+manifestServer.URL+`/board.hex"}}`,
		)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, manifestRequest())
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-PCController-Authentication") != "disabled-alpha" {
		t.Fatalf("credentialless alpha release status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, stageRequest())
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("X-PCController-Authentication") != "disabled-alpha" {
		t.Fatalf("credentialless alpha stage status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}

	config.IPC.AllowRemote = false
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, stageRequest())
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "remote network access is disabled") {
		t.Fatalf("disabled remote release stage status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
