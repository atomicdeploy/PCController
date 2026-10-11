package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestMirrorCapabilitiesAndStrictConfiguration(t *testing.T) {
	for method, want := range map[string]string{"controller.mirror.configure": capabilityHostConfig, "controller.mirror.inspect": capabilityRead, "controller.mirror.status": capabilityRead, "controller.mirror.apply": capabilityBoard, "controller.mirror.resync": capabilityBoard} {
		if got := requestCapability(method, nil); got != want {
			t.Fatalf("%s: %s != %s", method, got, want)
		}
	}
	updated := false
	service := Service{UpdateHostConfig: func(change func(*appconfig.Config) error) error {
		value := appconfig.Defaults()
		if err := change(&value); err != nil {
			return err
		}
		updated = true
		return value.Validate()
	}}
	for _, raw := range []string{`{"receive":{},"unknown":true}`, `{"local_board_id":"two","receive":{"domains":["pwm"]}}`, `{"receive":{"domains":["i2c"]}}`} {
		if _, err := service.dispatchMirror(context.Background(), Request{Method: "controller.mirror.configure", Params: json.RawMessage(raw)}); err == nil {
			t.Fatal("unsafe configuration accepted", raw)
		}
	}
	if updated {
		t.Fatal("invalid configuration reached persistence")
	}
	if _, err := service.dispatchMirror(context.Background(), Request{Method: "controller.mirror.configure", Params: json.RawMessage(`{"local_board_id":"two","receive":{}}`)}); err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("valid configuration not persisted")
	}
}
