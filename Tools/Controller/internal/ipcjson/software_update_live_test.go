package ipcjson

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/hostui"
)

// Opt-in real-process acceptance. The external fixture must be an isolated,
// hardware-disconnected Pealayer, not the user's installed running instance.
func TestSoftwareGatewayLivePealayerUpdate(t *testing.T) {
	origin := os.Getenv("PEALAYER_UPDATE_FIXTURE_ORIGIN")
	if origin == "" {
		t.Skip("requires an explicitly started isolated player fixture")
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Port() == "8080" || u.Path != "" {
		t.Fatal("fixture must use an explicit non-production loopback port")
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	get := func(path string, value any) {
		t.Helper()
		response, err := client.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("fixture %s status=%d", path, response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(value); err != nil {
			t.Fatal(err)
		}
	}
	var config struct {
		AppName     string `json:"app_name"`
		AutoConnect bool   `json:"auto_connect_hardware"`
	}
	get("/api/config", &config)
	if config.AppName != "Pealayer mutual-update fixture" || config.AutoConnect {
		t.Fatal("not the isolated disconnected fixture")
	}
	artifactService, api := newIPCArtifactService(t)
	service := &Service{Client: api, Artifacts: artifactService, AppInstances: hostui.NewInstanceRegistry()}
	_, err = service.AppInstances.Upsert(hostui.AppInstance{ID: "pealayer:mutual-update-fixture", Surface: "pealayer", State: "active",
		Values: map[string]string{"peer_origin": origin}, Self: &hostui.InstanceSelf{Kind: "native", ProcessID: 1234}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/rpc" {
			http.NotFound(w, r)
			return
		}
		var request Request
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request) != nil || !strings.HasPrefix(request.Method, "controller.software.") {
			http.Error(w, "software-only fixture", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(service.Dispatch(context.Background(), request))
	}))
	defer gateway.Close()
	for _, name := range []string{"PEALAYER_UPDATE_FIXTURE_SCRIPT", "PEALAYER_UPDATE_FIXTURE_CANDIDATE", "PEALAYER_UPDATE_FIXTURE_MANIFEST"} {
		if os.Getenv(name) == "" {
			t.Fatal("missing ", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", os.Getenv("PEALAYER_UPDATE_FIXTURE_SCRIPT"), "push", "--via", gateway.URL,
		"--gateway", "pccontroller", "--target", "app:pealayer:mutual-update-fixture", "--file", os.Getenv("PEALAYER_UPDATE_FIXTURE_CANDIDATE"), "--manifest", os.Getenv("PEALAYER_UPDATE_FIXTURE_MANIFEST"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live gateway replacement failed: %v\n%s", err, output)
	}
	var result struct {
		State    string `json:"state"`
		Verified bool   `json:"terminal_verified"`
	}
	if json.Unmarshal(output, &result) != nil || result.State != "completed" || !result.Verified {
		t.Fatalf("unverified replacement: %s", output)
	}
	t.Logf("actual isolated player replacement through controller-only gateway: %s", output)
}
