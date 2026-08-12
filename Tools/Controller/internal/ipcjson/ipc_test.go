package ipcjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostfacts"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/webui"
)

type ipcHostFactsProvider struct {
	profile string
	calls   int
}

func (provider *ipcHostFactsProvider) Query(_ context.Context, profile string) (hostfacts.Result, error) {
	provider.profile = profile
	provider.calls++
	return hostfacts.Result{
		Profile: profile, Class: "Win32_SerialPort", Source: "wmi",
		Columns:     []string{"DeviceID", "Status"},
		Rows:        []map[string]any{{"DeviceID": "COM18", "Status": "OK"}},
		CollectedAt: time.Unix(1, 0).UTC(),
	}, nil
}

func TestListenRejectsNonLoopback(t *testing.T) {
	if listener, err := Listen("0.0.0.0:8787"); err == nil {
		listener.Close()
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestAppPageRPCPublishesValidatedTUIAction(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	broker := hostui.NewActionBroker()
	actions := broker.Events()
	service := Service{Client: client, AppAction: broker.Publish}
	params, _ := json.Marshal(map[string]string{"page": "events"})
	response := service.Dispatch(context.Background(), Request{
		Method: "controller.app.page", Params: params,
	})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	select {
	case action := <-actions:
		if action.Kind != "app.page" || action.Value != "events" || action.Source != "ipc" {
			t.Fatalf("action=%#v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("app.page did not reach TUI broker")
	}
}

func TestAppInstanceRPCQueriesAndTargetsNavigation(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	broker := hostui.NewActionBroker()
	actions := broker.Events()
	registry := hostui.NewInstanceRegistry()
	service := Service{Client: client, AppAction: broker.Publish, AppInstances: registry}

	report, _ := json.Marshal(hostui.AppInstance{
		ID: "web:tab-1", Surface: "webui", Page: "controls", State: "active", LeaseSeconds: 45,
	})
	response := service.Dispatch(context.Background(), Request{Method: "controller.app.instance.report", Params: report})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	response = service.Dispatch(context.Background(), Request{Method: "controller.app.instances", Params: json.RawMessage(`{}`)})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	instances, ok := response.Result.([]hostui.AppInstance)
	if !ok || len(instances) != 1 || instances[0].Page != "controls" {
		t.Fatalf("instances=%#v", response.Result)
	}

	navigate, _ := json.Marshal(map[string]string{"target": "web:tab-1", "page": "settings"})
	response = service.Dispatch(context.Background(), Request{Method: "controller.app.navigate", Params: navigate})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	select {
	case action := <-actions:
		if action.Kind != "app.page" || action.Value != "settings" || action.Target != "web:tab-1" {
			t.Fatalf("action=%#v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("targeted app navigation did not reach the action broker")
	}
}

func TestAppBridgeReturnsCoordinatorSelfInformation(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	registry := hostui.NewInstanceRegistry()
	coordinatorID := "primary:bridge"
	_, err := registry.Upsert(hostui.AppInstance{
		ID: coordinatorID, Surface: "bridge", State: "background",
		Self: &hostui.InstanceSelf{
			Kind: "process", ProcessID: 36152,
			ImagePath: `C:\Tools\Controller\controller.exe`,
			Vars:      map[string]string{"processor_architecture": "AMD64"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Client: client, AppInstances: registry, CoordinatorInstanceID: coordinatorID,
	}
	response := service.Dispatch(context.Background(), Request{Method: "controller.app.bridge"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	instance, ok := response.Result.(hostui.AppInstance)
	if !ok || instance.ID != coordinatorID || instance.Self == nil ||
		instance.Self.ProcessID != 36152 || instance.Self.ImagePath == "" ||
		instance.Self.Vars["processor_architecture"] != "AMD64" {
		t.Fatalf("bridge instance=%#v", response.Result)
	}
}

func TestExecuteRoutesAppPageThroughTypedActionBroker(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	broker := hostui.NewActionBroker()
	actions := broker.Events()
	service := Service{Client: client, AppAction: broker.Publish}
	params, _ := json.Marshal(map[string]string{"command": "app page settings"})
	response := service.Dispatch(context.Background(), Request{
		Method: "controller.command.execute", Params: params,
	})
	if response.Error != nil || !strings.Contains(fmt.Sprint(response.Result), "accepted") {
		t.Fatalf("execute app page response=%#v", response)
	}
	select {
	case action := <-actions:
		if action.Kind != "app.page" || action.Value != "settings" || action.Source != "ipc-command" {
			t.Fatalf("action=%#v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("controller.command.execute app page did not reach action broker")
	}
}

func TestOSRPCSurfacesAreAuditedAndDisabledByDefault(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	}
	status := service.Dispatch(context.Background(), Request{Method: "controller.os.status"})
	if status.Error != nil {
		t.Fatal(status.Error)
	}
	keyParams, _ := json.Marshal(hostos.VirtualKeyRequest{Key: "F13"})
	key := service.Dispatch(context.Background(), Request{
		Method: "controller.os.key", Params: keyParams,
	})
	if key.Error == nil || !strings.Contains(key.Error.Message, "disabled") {
		t.Fatalf("disabled virtual key RPC=%#v", key)
	}
	powerParams, _ := json.Marshal(hostos.PowerRequest{
		Action: "lock", Confirmation: "CONFIRM",
	})
	power := service.Dispatch(context.Background(), Request{
		Method: "controller.os.power", Params: powerParams,
	})
	if power.Error == nil || !strings.Contains(power.Error.Message, "disabled") {
		t.Fatalf("disabled power RPC=%#v", power)
	}
	policy := config.OSActions
	policy.VirtualKeys.Allowed = append(policy.VirtualKeys.Allowed, "F14")
	policyParams, _ := json.Marshal(policy)
	configured := service.Dispatch(context.Background(), Request{
		Method: "controller.os.configure", Params: policyParams,
	})
	if configured.Error != nil || len(config.OSActions.VirtualKeys.Allowed) != 6 {
		t.Fatalf("OS policy configure=%#v config=%#v", configured, config.OSActions)
	}
}

func TestHostFactsRPCAndRESTUseTypedReadOnlyProvider(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	provider := &ipcHostFactsProvider{}
	service := &Service{Client: client, HostFacts: provider}
	params, _ := json.Marshal(map[string]any{"profile": "serial", "timeout_ms": 500})
	response := service.Dispatch(context.Background(), Request{
		Method: "controller.host.facts", Params: params,
	})
	if response.Error != nil || provider.profile != "serial" || provider.calls != 1 {
		t.Fatalf("response=%#v profile=%q calls=%d", response, provider.profile, provider.calls)
	}
	catalog := service.Dispatch(context.Background(), Request{Method: "controller.os.facts.catalog"})
	if catalog.Error != nil || !strings.Contains(fmt.Sprint(catalog.Result), "Win32_OperatingSystem") || provider.calls != 1 {
		t.Fatalf("catalog=%#v calls=%d", catalog, provider.calls)
	}
	invalidParams, _ := json.Marshal(map[string]any{"profile": "system", "timeout_ms": 50})
	invalid := service.Dispatch(context.Background(), Request{
		Method: "controller.os.facts", Params: invalidParams,
	})
	if invalid.Error == nil || !strings.Contains(invalid.Error.Message, "100..5000") || provider.calls != 1 {
		t.Fatalf("invalid=%#v calls=%d", invalid, provider.calls)
	}
	arbitrary := service.Dispatch(context.Background(), Request{
		Method: "controller.os.facts",
		Params: json.RawMessage(`{"query":"SELECT * FROM Win32_Process"}`),
	})
	if arbitrary.Error == nil || arbitrary.Error.Code != -32602 ||
		!strings.Contains(arbitrary.Error.Message, "unknown field") || provider.calls != 1 {
		t.Fatalf("arbitrary query=%#v calls=%d", arbitrary, provider.calls)
	}

	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	rejectedResponse, err := http.Get(server.URL + "/api/os/facts?query=SELECT+*+FROM+Win32_Process")
	if err != nil {
		t.Fatal(err)
	}
	rejectedBody, _ := io.ReadAll(rejectedResponse.Body)
	_ = rejectedResponse.Body.Close()
	if rejectedResponse.StatusCode != http.StatusBadRequest || provider.calls != 1 {
		t.Fatalf("arbitrary REST query status=%d body=%s calls=%d", rejectedResponse.StatusCode, rejectedBody, provider.calls)
	}
	httpResponse, err := http.Get(server.URL + "/api/os/facts?profile=serial")
	if err != nil {
		t.Fatal(err)
	}
	defer httpResponse.Body.Close()
	body, _ := io.ReadAll(httpResponse.Body)
	if httpResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"DeviceID":"COM18"`) ||
		httpResponse.Header.Get("Cache-Control") != "private, max-age=5" {
		t.Fatalf("HTTP status=%d headers=%v body=%s", httpResponse.StatusCode, httpResponse.Header, body)
	}
}

func TestHotkeyRPCMutatesOneValidatedBindingAtATime(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := &Service{
		Client: client,
		HotkeyStatus: func() any {
			return map[string]any{"supported": true, "running": true}
		},
		HostConfig: func() appconfig.Config {
			return config
		},
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			candidate.Integrations.Hotkeys = append([]appconfig.Hotkey(nil), config.Integrations.Hotkeys...)
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.hotkeys.get"})
	if get.Error != nil || !strings.Contains(fmt.Sprint(get.Result), "open-dashboard") ||
		!strings.Contains(fmt.Sprint(get.Result), "running:true") {
		t.Fatalf("get=%#v", get)
	}
	upsertParams, _ := json.Marshal(map[string]any{
		"operation": "upsert", "name": "diagnostics", "enabled": true,
		"chord": "control + option + d", "command": "os facts system",
	})
	upsert := service.Dispatch(context.Background(), Request{
		Method: "controller.hotkeys.set", Params: upsertParams,
	})
	if upsert.Error != nil {
		t.Fatalf("upsert=%#v", upsert)
	}
	if !strings.Contains(fmt.Sprint(upsert.Result), "apply_pending:true") {
		t.Fatalf("upsert did not report asynchronous registrar apply: %#v", upsert.Result)
	}
	last := config.Integrations.Hotkeys[len(config.Integrations.Hotkeys)-1]
	if last.Name != "diagnostics" || last.Chord != "Ctrl+Alt+D" || last.Command != "os facts system" {
		t.Fatalf("normalized binding=%#v", last)
	}
	duplicateParams, _ := json.Marshal(map[string]any{
		"operation": "upsert", "name": "diagnostics", "chord": "F13",
	})
	duplicate := service.Dispatch(context.Background(), Request{
		Method: "controller.hotkeys.set", Params: duplicateParams,
	})
	if duplicate.Error == nil || !strings.Contains(duplicate.Error.Message, "duplicates accelerator") ||
		config.Integrations.Hotkeys[len(config.Integrations.Hotkeys)-1].Chord != "Ctrl+Alt+D" {
		t.Fatalf("duplicate=%#v config=%#v", duplicate, config.Integrations.Hotkeys)
	}
	unknown := service.Dispatch(context.Background(), Request{
		Method: "controller.hotkeys.set",
		Params: json.RawMessage(`{"operation":"remove","name":"diagnostics","bindings":[]}`),
	})
	if unknown.Error == nil || unknown.Error.Code != -32602 || !strings.Contains(unknown.Error.Message, "unknown field") {
		t.Fatalf("unknown mutation=%#v", unknown)
	}
	removeParams, _ := json.Marshal(map[string]any{"operation": "remove", "name": "diagnostics"})
	removed := service.Dispatch(context.Background(), Request{
		Method: "controller.hotkeys.set", Params: removeParams,
	})
	if removed.Error != nil || len(config.Integrations.Hotkeys) != len(appconfig.Defaults().Integrations.Hotkeys) {
		t.Fatalf("remove=%#v bindings=%#v", removed, config.Integrations.Hotkeys)
	}
	if requestCapability("controller.hotkeys.get", nil) != capabilityRead ||
		requestCapability("controller.hotkeys.set", nil) != capabilityHostConfig {
		t.Fatal("hotkey RPC capabilities are not read/configuration separated")
	}
}

func TestHostMenuConfigRPCAndRESTUsePersistentHostConfig(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
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
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.host_menu.config.get"})
	if get.Error != nil {
		t.Fatal(get.Error)
	}
	updated := config.HostMenus
	updated.Menus[0].Label = "MAIN"
	encoded, _ := json.Marshal(updated)
	set := service.Dispatch(context.Background(), Request{Method: "controller.host_menu.config.set", Params: encoded})
	if set.Error != nil || config.HostMenus.Menus[0].Label != "MAIN" {
		t.Fatalf("host-menu RPC set=%#v label=%q", set, config.HostMenus.Menus[0].Label)
	}

	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/host-menus")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"label":"MAIN"`) {
		t.Fatalf("host-menu REST GET status=%d body=%s", response.StatusCode, body)
	}
	updated.Menus[0].Content = "REST applied"
	payload, _ := json.Marshal(updated)
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/host-menus", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || config.HostMenus.Menus[0].Content != "REST applied" {
		t.Fatalf("host-menu REST PUT status=%d body=%s content=%q", response.StatusCode, body, config.HostMenus.Menus[0].Content)
	}
}

func TestListenPolicyAllowsExplicitRemoteBindWithoutOpeningSocket(t *testing.T) {
	address, err := validateListenAddress("0.0.0.0:8787", true)
	if err != nil || address != "0.0.0.0:8787" {
		t.Fatalf("remote listen policy address=%q err=%v", address, err)
	}
}

func TestUIConfigIsUnauthenticatedAndReportsActiveBrowserContract(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.UI.AppTitle = "Controller Lab"
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	service := &Service{
		Client: client, WebSocketPath: "/control", SocketIOPath: "/engine.io/",
		HostVersion: "1.2.3", HostSourceHash: "0123456789abcdef", HostBuildTime: "2026-08-02T00:00:00Z",
		HostConfig: func() appconfig.Config { return config },
	}
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/ui-config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Name          string `json:"name"`
		WebSocketPath string `json:"websocket_path"`
		SocketIOPath  string `json:"socket_io_path"`
		TicketPath    string `json:"session_ticket_path"`
		AuthRequired  bool   `json:"auth_required"`
		HostVersion   string `json:"host_version"`
		SourceHash    string `json:"source_hash"`
		BuildTime     string `json:"build_time"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || result.Name != "Controller Lab" ||
		result.WebSocketPath != "/control" ||
		result.SocketIOPath != "/engine.io/" || result.TicketPath != SessionTicketPath || !result.AuthRequired ||
		result.HostVersion != "1.2.3" || result.SourceHash != "0123456789abcdef" ||
		result.BuildTime != "2026-08-02T00:00:00Z" {
		t.Fatalf("UI config status=%d result=%+v", response.StatusCode, result)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/ui-config", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("UI config POST status=%d Allow=%q", response.StatusCode, response.Header.Get("Allow"))
	}
}

func TestIntegrationProxyUsesAuthenticatedReservedRoute(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	const token = "0123456789abcdefghijklmn"
	proxied := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Integration-Path", request.URL.RequestURI())
		writer.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(websocketMux(context.Background(), &Service{
		Client: client, AuthToken: token, IntegrationProxy: proxied,
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/integrations/datahub/v1/records?limit=5")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated integration status=%d", response.StatusCode)
	}

	request, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/integrations/datahub/v1/records?limit=5",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted ||
		response.Header.Get("X-Integration-Path") !=
			"/api/integrations/datahub/v1/records?limit=5" {
		t.Fatalf("authenticated integration status=%d path=%q", response.StatusCode, response.Header.Get("X-Integration-Path"))
	}

	policy := appconfig.DefaultRemoteAccessPolicy()
	if remoteCapabilityAllowed(policy, capabilityIntegrations) {
		t.Fatal("remote integration access must be opt-in")
	}
	policy.Integrations = true
	if !remoteCapabilityAllowed(policy, capabilityIntegrations) {
		t.Fatal("explicit remote integration capability was ignored")
	}
}

func TestHTTPRESTAndAuthenticationShareIPCListener(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	const token = "0123456789abcdefghijklmn"
	go func() {
		done <- Serve(ctx, listener, &Service{
			Client: client, WebSocketPath: "/ipc", AuthToken: token,
			InboundWebhooks: true, WebUI: webui.Handler("/ipc"),
		})
	}()

	base := "http://" + listener.Addr().String()
	response, err := http.Get(base + "/api/ui-config")
	if err != nil {
		t.Fatal(err)
	}
	uiConfigBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(uiConfigBody), `"name":"PCController"`) ||
		!strings.Contains(string(uiConfigBody), `"websocket_path":"/ipc"`) ||
		!strings.Contains(string(uiConfigBody), `"socket_io_path":"/socket.io/"`) ||
		!strings.Contains(string(uiConfigBody), `"auth_required":true`) {
		t.Fatalf("unauthenticated UI config status=%d body=%s err=%v", response.StatusCode, uiConfigBody, readErr)
	}
	response, err = http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	appShell, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(appShell), "data-pccontroller-shell") {
		t.Fatalf("unauthenticated app shell status=%d body=%s err=%v", response.StatusCode, appShell, readErr)
	}

	response, err = http.Get(base + "/api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.StatusCode)
	}

	snapshotRequest, err := http.NewRequestWithContext(
		ctx, http.MethodGet, base+"/api/snapshot", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRequest.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(snapshotRequest)
	if err != nil {
		t.Fatal(err)
	}
	snapshotBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(snapshotBody), `"uptime_ms":0`) ||
		strings.Contains(string(snapshotBody), `"UptimeMS"`) {
		t.Fatalf(
			"snapshot JSON status=%d body=%s err=%v",
			response.StatusCode, snapshotBody, readErr,
		)
	}

	osStatusRequest, err := http.NewRequestWithContext(
		ctx, http.MethodGet, base+"/api/os/status", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	osStatusRequest.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(osStatusRequest)
	if err != nil {
		t.Fatal(err)
	}
	osStatusBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(osStatusBody), `"serial_discovery_source"`) ||
		!strings.Contains(string(osStatusBody), "Windows SetupAPI") {
		t.Fatalf("OS status=%d body=%s err=%v", response.StatusCode, osStatusBody, readErr)
	}

	for path, body := range map[string]string{
		"/api/os/key":   `{"key":"F13"}`,
		"/api/os/power": `{"action":"lock","confirmation":"CONFIRM"}`,
	} {
		osRequest, requestErr := http.NewRequestWithContext(
			ctx, http.MethodPost, base+path, strings.NewReader(body),
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		osRequest.Header.Set("Authorization", "Bearer "+token)
		osRequest.Header.Set("Content-Type", "application/json")
		response, requestErr = http.DefaultClient.Do(osRequest)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		deniedBody, responseErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if responseErr != nil || response.StatusCode != http.StatusForbidden ||
			!strings.Contains(string(deniedBody), "disabled") {
			t.Fatalf("disabled OS action %s status=%d body=%s err=%v", path, response.StatusCode, deniedBody, responseErr)
		}
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		base+"/api/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("authenticated HTTP response status=%d body=%s err=%v", response.StatusCode, body, err)
	}

	rawResponse, err := Call(ctx, listener.Addr().String(), Request{
		Method: "controller.ping", Auth: token,
	})
	if err != nil || rawResponse.Error != nil {
		t.Fatalf("authenticated raw response=%#v err=%v", rawResponse, err)
	}
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		request, err := http.NewRequestWithContext(
			ctx, method,
			base+"/api/webhooks/inbound?text=door+event&type=automation",
			strings.NewReader(`{"value":1}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-PCController-Token", token)
		request.Header.Set("X-Test-Event", "method-"+method)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("inbound webhook %s status=%d", method, response.StatusCode)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authenticated IPC server did not stop")
	}
}

func TestSocketIOEngineV4WebSocketAdapter(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	server := httptest.NewServer(websocketMux(context.Background(), &Service{
		Client: client, WebSocketPath: "/ipc", SocketIOPath: "/socket.io/",
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialContext, stopDial := context.WithTimeout(ctx, 10*time.Second)
	connection, _, err := websocket.Dial(
		dialContext,
		"ws"+strings.TrimPrefix(server.URL, "http")+
			"/socket.io/?EIO=4&transport=websocket",
		nil,
	)
	stopDial()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	// A busy hosted runner may spend most of the dial budget scheduling the
	// listener. Protocol assertions get their own deadline after the upgrade.
	clientContext, stopClient := context.WithTimeout(ctx, 10*time.Second)
	defer stopClient()
	readPacket := func(stage string) string {
		_, data, readErr := connection.Read(clientContext)
		if readErr != nil {
			t.Fatalf("%s: %v", stage, readErr)
		}
		return string(data)
	}
	writePacket := func(packet string) {
		if err := connection.Write(clientContext, websocket.MessageText, []byte(packet)); err != nil {
			t.Fatal(err)
		}
	}
	if packet := readPacket("Engine.IO open"); !strings.HasPrefix(packet, "0{") {
		t.Fatalf("Engine.IO open=%q", packet)
	}
	writePacket("40")
	if packet := readPacket("Socket.IO connect"); !strings.HasPrefix(packet, "40{") {
		t.Fatalf("Socket.IO connect=%q", packet)
	}
	writePacket(`42["subscribe",{"topics":["events"]}]`)
	for {
		packet := readPacket("subscription acknowledgement")
		if strings.HasPrefix(packet, "42") {
			name, _, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "subscribed" {
				break
			}
		}
	}
	runtime.PublishHostEvent("door", "door opened")
	for {
		packet := readPacket("controller event")
		if strings.HasPrefix(packet, "42") {
			name, _, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "controller.event" {
				break
			}
		}
	}
	writePacket(`42["rpc",{"jsonrpc":"2.0","id":7,"method":"controller.ping"}]`)
	for {
		packet := readPacket("ping RPC response")
		if strings.HasPrefix(packet, "42") {
			name, raw, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "rpc.response" {
				if !strings.Contains(string(raw), `"ok":true`) {
					t.Fatalf("rpc response payload=%s", raw)
				}
				break
			}
		}
	}
	writePacket(`42["rpc",{"jsonrpc":"2.0","id":8,"method":"controller.snapshot"}]`)
	for {
		packet := readPacket("snapshot RPC response")
		if strings.HasPrefix(packet, "42") {
			name, raw, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "rpc.response" &&
				strings.Contains(string(raw), `"id":8`) {
				if !strings.Contains(string(raw), `"uptime_ms":0`) ||
					strings.Contains(string(raw), `"UptimeMS"`) {
					t.Fatalf("Socket.IO snapshot payload=%s", raw)
				}
				break
			}
		}
	}
}

func TestTwoWebSocketSubscribersReceiveSameActivityAndStateWithoutPolling(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	server := httptest.NewServer(websocketMux(context.Background(), &Service{
		Client: client, WebSocketPath: "/ipc",
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dial := func(name string) *websocket.Conn {
		connection, _, err := websocket.Dial(
			ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ipc", nil,
		)
		if err != nil {
			t.Fatalf("dial %s: %v", name, err)
		}
		t.Cleanup(func() { connection.CloseNow() })
		request := map[string]any{
			"jsonrpc": "2.0", "id": name, "method": "controller.subscribe",
			"params": map[string]any{"topics": []string{"events", "state"}},
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := connection.Write(ctx, websocket.MessageText, encoded); err != nil {
			t.Fatalf("subscribe %s: %v", name, err)
		}
		for {
			_, data, err := connection.Read(ctx)
			if err != nil {
				t.Fatalf("subscription acknowledgement %s: %v", name, err)
			}
			var response Response
			if json.Unmarshal(data, &response) == nil && string(response.ID) == `"`+name+`"` {
				if response.Error != nil {
					t.Fatalf("subscription %s rejected: %#v", name, response.Error)
				}
				return connection
			}
		}
	}
	first, second := dial("first"), dial("second")

	activity := runtime.PublishStructuredEvent(control.Event{
		Kind: "message", Text: "Inspect output 3", Source: "ipc",
		Target: "web,tui", Targets: []string{"web", "tui"},
		MessageType: "operator.prompt", Correlation: "job-23",
	})
	state := runtime.PublishStructuredEvent(control.Event{
		Kind: "status_led.changed", Text: "#12AB34", State: "#12AB34",
	})

	type pushedEvent struct {
		Method string        `json:"method"`
		Params control.Event `json:"params"`
	}
	readBoth := func(name string, connection *websocket.Conn) (control.Event, control.Event) {
		var gotActivity, gotState control.Event
		for gotActivity.ID == 0 || gotState.ID == 0 {
			_, data, err := connection.Read(ctx)
			if err != nil {
				t.Fatalf("read %s broadcast events: %v", name, err)
			}
			var pushed pushedEvent
			if json.Unmarshal(data, &pushed) != nil {
				continue
			}
			switch pushed.Params.ID {
			case activity.ID:
				gotActivity = pushed.Params
			case state.ID:
				gotState = pushed.Params
			}
		}
		return gotActivity, gotState
	}
	for name, connection := range map[string]*websocket.Conn{"first": first, "second": second} {
		gotActivity, gotState := readBoth(name, connection)
		if gotActivity.Kind != "message" || gotActivity.Stream != "activity" ||
			gotActivity.Correlation != "job-23" {
			t.Fatalf("%s activity=%+v", name, gotActivity)
		}
		if gotState.Kind != "status_led.changed" || gotState.Stream != "state" {
			t.Fatalf("%s state=%+v", name, gotState)
		}
	}
}

func TestRawJSONRPCAndWebSocketShareOneIPCListener(t *testing.T) {
	runtime := control.New(control.Options{})
	engine := shell.New(8)
	if err := engine.Register(shell.Command{
		Name: "echo", Usage: "echo VALUE", Summary: "duplex test command",
		Run: func(_ context.Context, args []string) (string, error) {
			return strings.Join(args, " "), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := controllerapi.AttachSharedRuntime(runtime, engine)
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, &Service{
			Client: client, WebSocketPath: "/ipc", WebUI: webui.Handler("/ipc"),
		})
	}()

	base := "http://" + listener.Addr().String()
	var shellBody string
	for _, check := range []struct {
		path, contains string
	}{
		{path: "/healthz", contains: `"ok":true`},
		{path: "/api/ui-config", contains: `"auth_required":false`},
		{path: "/api/snapshot", contains: `"uptime_ms":0`},
		{path: "/", contains: "data-pccontroller-shell"},
	} {
		response, requestErr := http.Get(base + check.path)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK ||
			!strings.Contains(string(body), check.contains) {
			t.Fatalf("GET %s status=%d body=%s err=%v", check.path, response.StatusCode, body, readErr)
		}
		if check.path == "/" {
			shellBody = string(body)
		}
	}
	cssEnd := strings.Index(shellBody, ".css\"")
	cssStart := -1
	if cssEnd >= 0 {
		cssStart = strings.LastIndex(shellBody[:cssEnd], "href=\"")
	}
	if cssStart < 0 || cssEnd < 0 {
		t.Fatalf("embedded shell has no stylesheet asset: %s", shellBody)
	}
	cssPath := shellBody[cssStart+len("href=\"") : cssEnd+len(".css")]
	rangeRequest, err := http.NewRequest(http.MethodGet, base+cssPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	rangeRequest.Header.Set("Range", "bytes=0-15")
	rangeResponse, err := http.DefaultClient.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	rangeBody, readErr := io.ReadAll(rangeResponse.Body)
	_ = rangeResponse.Body.Close()
	if readErr != nil || rangeResponse.StatusCode != http.StatusPartialContent ||
		len(rangeBody) != 16 || !strings.HasPrefix(rangeResponse.Header.Get("Content-Range"), "bytes 0-15/") {
		t.Fatalf("static range status=%d range=%q bytes=%d err=%v", rangeResponse.StatusCode, rangeResponse.Header.Get("Content-Range"), len(rangeBody), readErr)
	}
	dialContext, stopDial := context.WithTimeout(ctx, 5*time.Second)
	connection, _, err := websocket.Dial(
		dialContext,
		"ws://"+listener.Addr().String()+"/ipc",
		nil,
	)
	stopDial()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	websocketContext, stopWebSocket := context.WithTimeout(ctx, 5*time.Second)
	defer stopWebSocket()
	writeRPC := func(value any) {
		encoded, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if writeErr := connection.Write(
			websocketContext,
			websocket.MessageText,
			encoded,
		); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	writeRPC(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "controller.subscribe",
		"params": map[string]any{"topics": []string{"events", "state"}},
	})

	// The legacy NDJSON client remains usable while the WebSocket is active on
	// the exact same TCP address.
	rawContext, stopRaw := context.WithTimeout(ctx, time.Second)
	response, err := Call(rawContext, listener.Addr().String(), Request{
		Method: "controller.ping",
	})
	stopRaw()
	if err != nil || response.Error != nil {
		t.Fatalf("raw IPC ping response=%#v err=%v", response, err)
	}

	// Consume the subscription acknowledgement, then prove an event is pushed
	// without status polling or a second connection.
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message["id"] != nil {
			break
		}
	}
	runtime.PublishHostEvent("door", "door opened")
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Kind string `json:"kind"`
			} `json:"params"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Method == "controller.event" && message.Params.Kind == "door" {
			break
		}
	}
	runtime.PublishHostEvent("status_led.changed", "#12AB34")
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Kind   string `json:"kind"`
				Stream string `json:"stream"`
			} `json:"params"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Method == "controller.state" && message.Params.Kind == "status_led.changed" {
			if message.Params.Stream != "state" {
				t.Fatalf("state notification stream=%q", message.Params.Stream)
			}
			break
		}
	}

	// The browser terminal can send a correlated dispatcher command over the
	// same full-duplex WebSocket that just delivered the asynchronous event.
	writeRPC(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "controller.command.execute",
		"params": map[string]any{"command": "echo duplex-ready"},
	})
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope.ID) != "3" {
			continue
		}
		if !strings.Contains(string(data), `"output":"duplex-ready"`) {
			t.Fatalf("WebSocket command payload=%s", data)
		}
		break
	}

	writeRPC(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "controller.snapshot",
	})
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope.ID) != "2" {
			continue
		}
		if !strings.Contains(string(data), `"uptime_ms":0`) ||
			strings.Contains(string(data), `"UptimeMS"`) {
			t.Fatalf("WebSocket snapshot payload=%s", data)
		}
		break
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multiplexed IPC server did not stop")
	}
}

func TestStreamableEventKindSeparatesTimelineFromStatusTraffic(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{kind: "door", want: true},
		{kind: "rf.received", want: true},
		{kind: "macro.completed", want: true},
		{kind: " telemetry ", want: false},
		{kind: "RX", want: false},
		{kind: "tx", want: false},
		{kind: "front_panel.segment", want: false},
		{kind: "status_led.changed", want: false},
		{kind: "buzzer.note", want: false},
		{kind: "", want: false},
	}
	for _, test := range tests {
		if got := streamableEventKind(test.kind); got != test.want {
			t.Errorf("streamableEventKind(%q)=%v want %v", test.kind, got, test.want)
		}
	}
}

func TestCallRoundTripParseError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		_ = json.NewDecoder(connection).Decode(&request)
		_ = json.NewEncoder(connection).Encode(Response{
			JSONRPC: Version,
			ID:      request.ID,
			Result:  map[string]bool{"ok": true},
		})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Call(ctx, listener.Addr().String(), Request{
		Method: "controller.ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result == nil {
		t.Fatal("missing result")
	}
}

func TestRemoteCapabilityPolicyAndMessageProvenance(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	service := Service{Client: client, HostConfig: func() appconfig.Config { return config }}

	read := service.DispatchRemote(
		context.Background(),
		Request{Method: "controller.snapshot"},
		"websocket",
	)
	if read.Error != nil {
		t.Fatalf("default remote read=%#v", read)
	}
	reset := service.DispatchRemote(
		context.Background(),
		Request{Method: "controller.reset"},
		"websocket",
	)
	if reset.Error == nil || reset.Error.Code != -32003 ||
		!strings.Contains(reset.Error.Message, "reset") {
		t.Fatalf("default remote reset=%#v", reset)
	}
	automationParams, _ := json.Marshal(map[string]string{"command": "automation run missing"})
	automation := service.DispatchRemote(context.Background(), Request{
		Method: "controller.command.execute", Params: automationParams,
	}, "websocket")
	if automation.Error == nil || automation.Error.Code != -32003 ||
		!strings.Contains(automation.Error.Message, capabilityAutomations) {
		t.Fatalf("default remote host automation=%#v", automation)
	}
	config.IPC.RemotePolicy.HostAutomations = true
	automation = service.DispatchRemote(context.Background(), Request{
		Method: "controller.command.execute", Params: automationParams,
	}, "websocket")
	if automation.Error != nil && automation.Error.Code == -32003 {
		t.Fatalf("explicitly enabled host automation remained policy-blocked=%#v", automation)
	}

	messageParams, _ := json.Marshal(controllerapi.TextMessage{
		Source: "board", Target: "host", Type: "operator.notice", Text: "hello",
	})
	denied := service.DispatchRemote(context.Background(), Request{
		Method: "controller.message.send", Params: messageParams,
	}, "websocket")
	if denied.Error == nil || denied.Error.Code != -32003 {
		t.Fatalf("default remote message=%#v", denied)
	}
	config.IPC.RemotePolicy.Messages = true
	accepted := service.DispatchRemote(context.Background(), Request{
		Method: "controller.message.send", Params: messageParams,
	}, "websocket")
	if accepted.Error != nil {
		t.Fatal(accepted.Error)
	}
	event, ok := accepted.Result.(controllerapi.Event)
	if !ok || event.Source != "websocket" || event.Metadata["claimed_source"] != "board" {
		t.Fatalf("trusted message provenance=%#v", accepted.Result)
	}

	latest := client.LatestEventID()
	if latest == 0 {
		t.Fatal("remote authorization decisions were not audited")
	}
}

func TestRemoteRESTPolicyBlocksDisruptiveCommand(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
	}
	handler := websocketMux(context.Background(), service)

	request := httptest.NewRequest(
		http.MethodPost,
		"http://controller.example/api/command",
		strings.NewReader(`{"command":"program flash candidate.hex"}`),
	)
	request.RemoteAddr = "198.51.100.10:43210"
	request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), "programming") {
		t.Fatalf("remote programming status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"http://controller.example/api/snapshot",
		nil,
	)
	request.RemoteAddr = "198.51.100.10:43210"
	request.Header.Set("X-PCController-Token", config.IPC.AuthToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remote read status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBridgeCallPreservesNestedRPCResponse(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	service := Service{
		Client:     client,
		BridgeList: func() any { return []map[string]any{{"name": "lab", "connected": true}} },
		BridgeCall: func(
			_ context.Context,
			peer string,
			request Request,
		) (Response, error) {
			if peer != "lab" || request.Method != "controller.snapshot" {
				t.Fatalf("bridge peer=%q request=%#v", peer, request)
			}
			return Response{
				JSONRPC: Version, ID: request.ID,
				Result: map[string]bool{"connected": true},
			}, nil
		},
	}
	params, _ := json.Marshal(map[string]any{
		"peer": "lab",
		"request": map[string]any{
			"jsonrpc": "2.0", "id": 7, "method": "controller.snapshot",
		},
	})
	response := service.Dispatch(context.Background(), Request{
		Method: "controller.bridge.call", Params: params,
	})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	encoded, _ := json.Marshal(response.Result)
	if !strings.Contains(string(encoded), `"peer":"lab"`) ||
		!strings.Contains(string(encoded), `"connected":true`) {
		t.Fatalf("bridge result=%s", encoded)
	}
}

func TestInvalidParamsRetainStandardJSONRPCErrorCode(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	response := (&Service{Client: client}).Dispatch(context.Background(), Request{
		JSONRPC: Version, ID: json.RawMessage("9"),
		Method: "controller.open", Params: json.RawMessage(`{"port":42}`),
	})
	if response.Error == nil || response.Error.Code != -32602 ||
		string(response.ID) != "9" {
		t.Fatalf("invalid params response=%#v", response)
	}
}

func TestCommandCatalogAndProgramStateReachRPCAndREST(t *testing.T) {
	runtime := control.New(control.Options{})
	engine := control.NewCommandEngine(runtime, control.CommandOptions{})
	client := controllerapi.AttachSharedRuntime(runtime, engine)
	service := &Service{Client: client}

	catalog := service.Dispatch(context.Background(), Request{
		Method: "controller.command.catalog",
	})
	if catalog.Error != nil {
		t.Fatal(catalog.Error)
	}
	descriptors, ok := catalog.Result.([]shell.CommandDescriptor)
	if !ok || !catalogContains(descriptors, "settings") ||
		!catalogContains(descriptors, "relay") ||
		!catalogContains(descriptors, "program") {
		t.Fatalf("RPC command catalog=%#v", catalog.Result)
	}
	executeParams, _ := json.Marshal(map[string]string{"command": "help strip"})
	executed := service.Dispatch(context.Background(), Request{
		Method: "controller.command.execute", Params: executeParams,
	})
	if executed.Error != nil || !strings.Contains(fmt.Sprint(executed.Result), "strip pixel") {
		t.Fatalf("command.execute=%#v", executed)
	}

	stateParams, _ := json.Marshal(map[string]string{
		"owner": "test", "mode": "running", "reason": "matrix test",
	})
	set := service.Dispatch(context.Background(), Request{
		Method: "controller.program_state.set", Params: stateParams,
	})
	if set.Error != nil || client.ProgramState().Mode != controllerapi.ProgramRunning {
		t.Fatalf("program-state set=%#v state=%#v", set, client.ProgramState())
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.program_state.get"})
	if get.Error != nil {
		t.Fatalf("program-state get=%#v", get)
	}

	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	for _, path := range []string{"/api/commands", "/api/program-state"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s err=%v", path, response.StatusCode, body, readErr)
		}
	}
	restRequest, _ := http.NewRequest(
		http.MethodPut, server.URL+"/api/program-state",
		strings.NewReader(`{"owner":"test","mode":"idle"}`),
	)
	restRequest.Header.Set("Content-Type", "application/json")
	restResponse, err := http.DefaultClient.Do(restRequest)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(restResponse.Body)
	_ = restResponse.Body.Close()
	if restResponse.StatusCode != http.StatusOK || client.ProgramState().Mode != controllerapi.ProgramIdle {
		t.Fatalf("REST program-state status=%d body=%s state=%#v", restResponse.StatusCode, body, client.ProgramState())
	}

	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	remote := &Service{Client: client, HostConfig: func() appconfig.Config { return config }}
	if response := remote.DispatchRemote(context.Background(), Request{Method: "controller.command.catalog"}, "websocket"); response.Error != nil {
		t.Fatalf("safe default remote catalog=%#v", response)
	}
	if response := remote.DispatchRemote(context.Background(), Request{
		Method: "controller.program_state.set", Params: stateParams,
	}, "websocket"); response.Error == nil || response.Error.Code != -32003 {
		t.Fatalf("safe default remote program-state mutation=%#v", response)
	}
}

func TestGenericCommandRemoteCapabilitiesDistinguishReadsFromMutations(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"help relay", capabilityRead},
		{"status", capabilityRead},
		{"settings", capabilityRead},
		{"settings color 2", capabilityBoard},
		{"program-state status", capabilityRead},
		{"program-state running", capabilityBoard},
		{"menu layout", capabilityRead},
		{"menu layout reset", capabilityBoard},
		{"pwm get", capabilityRead},
		{"pwm set 0 2048", capabilityBoard},
		{"rf list", capabilityRead},
		{"rf status", capabilityRead},
		{"rf inspect 3", capabilityRead},
		{"rf send 0x1234 24 1 350", capabilityBoard},
		{"macro show demo", capabilityRead},
		{"macro monitor", capabilityRead},
		{"macro play demo", capabilityBoard},
		{"macro create 1 demo", capabilityHostConfig},
		{"macro update 1 renamed motion green", capabilityHostConfig},
		{"macro rename 1 renamed", capabilityHostConfig},
		{"macro category 1 motion", capabilityHostConfig},
		{"macro record save", capabilityHostConfig},
		{"melody create notify C4:100", capabilityHostConfig},
		{"automation run door-open", capabilityAutomations},
		{"webhook status", capabilityRead},
		{"webhook pending", capabilityRead},
		{"webhook dead", capabilityRead},
		{"webhook replay dead-id", capabilityIntegrations},
		{"webhook clear dead", capabilityIntegrations},
		{"hotkeys status", capabilityRead},
		{"keyboard status", capabilityRead},
		{"keyboard enable media", capabilityVirtualKeys},
		{"bridge list", capabilityRead},
		{"bridge call lab controller.status", capabilityBridgeCalls},
		{"os status", capabilityRead},
		{"os facts serial", capabilityRead},
		{"os key F13", capabilityVirtualKeys},
		{"program flash image.hex", capabilityProgramming},
		{"toolchain profile", capabilityRead},
		{"toolchain sync", capabilityProgramming},
		{"query 0x35 0x80 00", capabilityProgramming},
		{"write A50135010000", capabilityProgramming},
	}
	for _, test := range tests {
		if got := commandCapability(test.command); got != test.want {
			t.Errorf("commandCapability(%q)=%q want %q", test.command, got, test.want)
		}
	}

	params, err := json.Marshal(map[string]string{
		"command": "macro update 7 renamed motion green",
	})
	if err != nil {
		t.Fatal(err)
	}
	access := Access{Remote: true, Transport: "websocket", Principal: "config-peer"}
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.RemotePolicy.HostConfiguration = true
	config.IPC.RemotePolicy.BoardCommands = false
	service := &Service{HostConfig: func() appconfig.Config { return config }}
	if err := service.authorizeAccess(access, "controller.command.execute", params); err != nil {
		t.Fatalf("config-only peer could not rename macro metadata: %v", err)
	}
	config.IPC.RemotePolicy.HostConfiguration = false
	config.IPC.RemotePolicy.BoardCommands = true
	if err := service.authorizeAccess(access, "controller.command.execute", params); err == nil ||
		!strings.Contains(err.Error(), capabilityHostConfig) {
		t.Fatalf("board-only peer mutated host macro metadata: %v", err)
	}
}

func TestRFMapParamsNormalizeSemanticTargetsAndRejectUnsafeMappings(t *testing.T) {
	id := func(value int) *int { return &value }
	tests := []struct {
		name     string
		params   rfMapParams
		action   controllerapi.RFAction
		value    byte
		behavior controllerapi.RFBehavior
		wantErr  string
	}{
		{name: "guided key B", params: rfMapParams{ID: id(3), Action: "key", Target: "2", Behavior: "press"}, action: controllerapi.RFActionKey, value: 1, behavior: controllerapi.RFBehaviorPress},
		{name: "user relay", params: rfMapParams{ID: id(4), Action: "relay", Target: "8", Behavior: "momentary"}, action: controllerapi.RFActionRelay, value: 7, behavior: controllerapi.RFBehaviorMomentary},
		{name: "door gated motion", params: rfMapParams{ID: id(5), Action: "side", Target: "B", Behavior: "stop"}, action: controllerapi.RFActionSide, value: 1, behavior: controllerapi.RFBehaviorStop},
		{name: "unmapped", params: rfMapParams{ID: id(6), Action: "none"}, action: controllerapi.RFActionNone, behavior: controllerapi.RFBehaviorPress},
		{name: "protected relay", params: rfMapParams{ID: id(1), Action: "relay", Target: "4", Behavior: "toggle"}, wantErr: "5..8"},
		{name: "overflow PWM", params: rfMapParams{ID: id(1), Action: "pwm", Target: "256", Behavior: "press"}, wantErr: "0..10"},
		{name: "invalid slot", params: rfMapParams{ID: id(20), Action: "none"}, wantErr: "0..19"},
		{name: "ambiguous unmapped", params: rfMapParams{ID: id(1), Action: "none", Behavior: "press"}, wantErr: "do not accept"},
		{name: "missing slot", params: rfMapParams{Action: "none"}, wantErr: "required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapping, err := test.params.mapping()
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("mapping=%#v err=%v want %q", mapping, err, test.wantErr)
				}
				return
			}
			if err != nil || mapping.Action != test.action || mapping.Value != test.value || mapping.Behavior != test.behavior {
				t.Fatalf("mapping=%#v err=%v", mapping, err)
			}
		})
	}
}

func TestRFGuidedRPCMutationsValidateBeforeBoardAccess(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	service := &Service{Client: client}

	for _, test := range []struct {
		method string
		params string
		want   string
	}{
		{method: "controller.rf.map", params: `{"id":1,"action":"relay","target":"2","behavior":"toggle"}`, want: "5..8"},
		{method: "controller.rf.remove", params: `{"id":20}`, want: "0..19"},
		{method: "controller.rf.clear", params: `{"confirm":"clear"}`, want: "CLEAR RF"},
		{method: "controller.rf.transmit", params: `{"code":1234,"bits":33,"protocol":1}`, want: "1..32"},
		{method: "controller.rf.transmit", params: `{"code":1234,"bits":24,"protocol":13}`, want: "1..12"},
	} {
		response := service.Dispatch(context.Background(), Request{
			Method: test.method, Params: json.RawMessage(test.params),
		})
		if response.Error == nil || !strings.Contains(response.Error.Message, test.want) {
			t.Errorf("%s response=%#v want %q", test.method, response, test.want)
		}
	}

	for _, method := range []string{"controller.rf.map", "controller.rf.remove", "controller.rf.clear", "controller.rf.transmit"} {
		if capability := requestCapability(method, nil); capability != capabilityBoard {
			t.Errorf("%s capability=%q want %q", method, capability, capabilityBoard)
		}
	}
}

func catalogContains(catalog []shell.CommandDescriptor, name string) bool {
	for _, descriptor := range catalog {
		if descriptor.Name == name {
			return true
		}
	}
	return false
}
