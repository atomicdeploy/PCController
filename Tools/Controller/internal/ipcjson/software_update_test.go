package ipcjson

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"pccontroller.local/controller/internal/hostui"
	"strings"
	"testing"
)

func softwareParams(t *testing.T, target softwareTarget, method string, params any) json.RawMessage {
	t.Helper()
	bytes, err := json.Marshal(map[string]any{"target": target.ID, "revision": target.Revision, "authorized": true,
		"request": map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}})
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

func softwareFixture(t *testing.T, handler http.HandlerFunc) (*Service, softwareTarget) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	artifacts, client := newIPCArtifactService(t)
	service := &Service{Client: client, Artifacts: artifacts, AppInstances: hostui.NewInstanceRegistry()}
	_, err := service.AppInstances.Upsert(hostui.AppInstance{ID: "pealayer:test", Surface: "pealayer", State: "active",
		Values: map[string]string{"peer_origin": server.URL}, Self: &hostui.InstanceSelf{Kind: "native", ProcessID: 100}})
	if err != nil {
		t.Fatal(err)
	}
	targets := service.softwareTargets()
	if len(targets) != 2 {
		t.Fatalf("targets=%v", targets)
	}
	return service, targets[1]
}

func TestSoftwareGatewayUsesRegisteredTargetAndExistingPealayerHTTPUpdater(t *testing.T) {
	var paths []string
	service, target := softwareFixture(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/update/manifest" {
			io.WriteString(w, `{"format":"pealayer-update","service":"pealayer"}`)
			return
		}
		if r.URL.Path == "/api/update/chunk" {
			body, _ := io.ReadAll(r.Body)
			if string(body) != "fixture" || r.URL.Query().Get("id") != "update-test" || r.URL.Query().Get("offset") != "0" {
				t.Errorf("bad forwarded chunk %q %s", body, r.URL)
			}
		}
		io.WriteString(w, `{"operation_id":"update-test","state":"receiving"}`)
	})
	_, err := service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, target, "pealayer.update.upload.begin", map[string]any{"size": 7, "sha256": strings.Repeat("a", 64)}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, target, "pealayer.update.upload.chunk", map[string]any{"operation_id": "update-test", "offset": 0, "data": base64.StdEncoding.EncodeToString([]byte("fixture"))}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "/api/update/manifest,/api/update/begin,/api/update/chunk?id=update-test&offset=0" {
		t.Fatalf("paths=%v", paths)
	}
	service.AppInstances.Remove("pealayer:test")
	if _, err = service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, target, "pealayer.update.status", map[string]any{})); err == nil {
		t.Fatal("expired target was forwarded")
	}
}

func TestSoftwareGatewayRejectsPivotsCredentialsWrongProductsAndChangedOwners(t *testing.T) {
	calls := 0
	service, target := softwareFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"format":"other","service":"other"}`)
	})
	for _, method := range []string{"controller.bridge.call", "controller.software.call", "pealayer.process.connect", "controller.relay.set"} {
		params := softwareParams(t, target, method, map[string]any{})
		if _, err := service.dispatchSoftware(context.Background(), "controller.software.call", params); err == nil {
			t.Fatal("accepted pivot", method)
		}
		if err := ValidateBridgeRequest(Request{Method: "controller.software.call", Params: params}); err == nil {
			t.Fatal("bridge accepted pivot", method)
		}
	}
	if calls != 0 {
		t.Fatal("rejected requests reached network")
	}
	stale := target
	stale.Revision = "stale"
	if _, err := service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, stale, "pealayer.update.status", map[string]any{})); err == nil {
		t.Fatal("stale owner accepted")
	}
	if _, err := service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, target, "pealayer.update.upload.begin", map[string]any{})); err == nil {
		t.Fatal("wrong product accepted")
	}
	if calls != 1 {
		t.Fatal("wrong product received a mutation")
	}
	valid := softwareParams(t, target, "pealayer.update.status", map[string]any{})
	if err := ValidateBridgeRequest(Request{Method: "controller.software.call", Params: valid}); err != nil {
		t.Fatal("scoped software relay rejected", err)
	}
}

func TestSoftwareGatewayDoesNotFollowRedirectsOrRetryUncertainMutations(t *testing.T) {
	calls := 0
	service, target := softwareFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "http://127.0.0.1:1/private", http.StatusTemporaryRedirect)
	})
	if _, err := service.dispatchSoftware(context.Background(), "controller.software.call", softwareParams(t, target, "pealayer.update.upload.chunk", map[string]any{"operation_id": "test", "offset": 0, "data": "eA=="})); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 {
		t.Fatal("mutation retried")
	}
	if requestCapability("controller.software.call", nil) != capabilityProgramming || requestCapability("controller.software.targets", nil) != capabilityRead {
		t.Fatal("incorrect software capabilities")
	}
}
