package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func appearanceTestService(t *testing.T) (*Service, *appconfig.Config, *int) {
	t.Helper()
	config := appconfig.Defaults()
	updates := 0
	runtime := control.New(control.Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	service := &Service{
		Client:     controllerapi.AttachIsolatedRuntime(runtime, shell.New(8)),
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			updates++
			return nil
		},
	}
	return service, &config, &updates
}

func TestBrowserAppearanceMutationUsesHostETagDiffAndNoOpSuppression(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	get := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.get"})
	initial, ok := get.Result.(browserUISettings)
	if get.Error != nil || !ok || len(initial.AppearanceETag) != 64 {
		t.Fatalf("initial appearance=%#v error=%v", get.Result, get.Error)
	}

	params, _ := json.Marshal(map[string]any{
		"if_match": initial.AppearanceETag,
		"appearance": map[string]any{
			"theme": "dark", "locale": "fa", "direction": "rtl",
			"reduceMotion": true, "compactNumbers": true,
			"audioMuted": true, "audioVolume": 0,
		},
	})
	set := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: params})
	updated, ok := set.Result.(browserUISettings)
	if set.Error != nil || !ok || updated.Changed == nil || !*updated.Changed {
		t.Fatalf("updated appearance=%#v error=%v", set.Result, set.Error)
	}
	wantFields := []string{
		"appearance.theme", "appearance.locale", "appearance.direction",
		"appearance.reduceMotion", "appearance.compactNumbers", "appearance.audioMuted",
		"appearance.audioVolume",
	}
	if !reflect.DeepEqual(updated.ChangedFields, wantFields) {
		t.Fatalf("changed fields=%#v want=%#v", updated.ChangedFields, wantFields)
	}
	if updated.Before["appearance.audioVolume"] != 0.42 || updated.After["appearance.audioVolume"] != float64(0) {
		t.Fatalf("volume diff lost explicit zero: before=%#v after=%#v", updated.Before, updated.After)
	}
	if *updates != 1 || config.UI.Appearance.Theme != "dark" || config.UI.Appearance.AudioVolume != 0 {
		t.Fatalf("updates=%d config=%#v", *updates, config.UI.Appearance)
	}

	noOpParams, _ := json.Marshal(map[string]any{
		"if_match": updated.AppearanceETag,
		"appearance": map[string]any{
			"theme": "dark", "locale": "fa", "direction": "rtl",
			"reduceMotion": true, "compactNumbers": true,
			"audioMuted": true, "audioVolume": 0,
		},
	})
	noOp := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: noOpParams})
	unchanged, ok := noOp.Result.(browserUISettings)
	if noOp.Error != nil || !ok || unchanged.Changed == nil || *unchanged.Changed ||
		len(unchanged.ChangedFields) != 0 || *updates != 1 {
		t.Fatalf("no-op was not suppressed: result=%#v error=%v updates=%d", noOp.Result, noOp.Error, *updates)
	}
}

func TestBrowserAppearanceRejectsStaleInvalidAndUnknownMutationsAtomically(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	staleETag := appearanceETag(config.UI.Appearance)
	config.UI.Appearance.Theme = "dark"

	stale, _ := json.Marshal(map[string]any{
		"if_match": staleETag, "appearance": map[string]any{"locale": "fa"},
	})
	response := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: stale})
	if response.Error == nil || response.Error.Code != -32000 || config.UI.Appearance.Locale != "en" || *updates != 0 {
		t.Fatalf("stale mutation response=%#v config=%#v updates=%d", response, config.UI.Appearance, *updates)
	}

	invalid, _ := json.Marshal(map[string]any{
		"if_match": appearanceETag(config.UI.Appearance), "appearance": map[string]any{"audioVolume": 2},
	})
	response = service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: invalid})
	if response.Error == nil || config.UI.Appearance.AudioVolume != 0.42 || *updates != 0 {
		t.Fatalf("invalid mutation response=%#v config=%#v updates=%d", response, config.UI.Appearance, *updates)
	}

	unknown, _ := json.Marshal(map[string]any{"appearance": map[string]any{"theme": "dark", "surprise": true}})
	response = service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: unknown})
	if response.Error == nil || response.Error.Code != -32602 || *updates != 0 {
		t.Fatalf("unknown mutation response=%#v updates=%d", response, *updates)
	}
}

func TestUIBootstrapPublishesHostAuthoritativeAppearance(t *testing.T) {
	service, config, _ := appearanceTestService(t)
	config.UI.Appearance = appconfig.Appearance{
		Theme: "light", Locale: "fa", Direction: "rtl",
		ReduceMotion: true, AudioMuted: true, AudioVolume: 0.25,
	}
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/ui-config")
	if err != nil {
		t.Fatalf("bootstrap GET: %v", err)
	}
	defer response.Body.Close()
	var document struct {
		Appearance     browserAppearance `json:"appearance"`
		AppearanceETag string            `json:"appearance_etag"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if response.StatusCode != http.StatusOK || document.Appearance.Theme != "light" ||
		document.Appearance.Locale != "fa" || document.Appearance.AudioVolume != 0.25 ||
		document.AppearanceETag != appearanceETag(config.UI.Appearance) {
		t.Fatalf("bootstrap status=%d document=%#v", response.StatusCode, document)
	}
}
