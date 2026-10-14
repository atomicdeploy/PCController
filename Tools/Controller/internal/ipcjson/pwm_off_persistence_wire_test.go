package ipcjson_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/ipcjson"
)

func TestRPCAndSemanticPWMOffPersistOneConfirmedProfile(t *testing.T) {
	for _, method := range []string{"controller.pwm.set", "controller.action.invoke", "controller.pwm.off"} {
		t.Run(method, func(t *testing.T) {
			board := startRawVirtualBoard(t)
			defer board.close()
			board.pwmValues[1], board.pwmValues[2] = 239, 777
			config := appconfig.Defaults()
			value := appconfig.PWMChannel(config.UI, 1)
			value.OnBehavior = "restore"
			config.UI.PWMChannels["pwm.1"] = value
			identity := appconfig.ResolveBoardIdentity("", "", board.endpoint()).Value
			config.BoardProfiles = map[string]appconfig.BoardProfile{identity: {
				Key: "fixture", Mode: appconfig.BoardModeOrdinaryRelays,
				PWMRecall: map[string]uint16{"pwm.2": 555},
			}}
			saves := 0
			update := func(change func(*appconfig.Config) error) error {
				encoded, err := json.Marshal(config)
				if err != nil {
					return err
				}
				var copy appconfig.Config
				if err := json.Unmarshal(encoded, &copy); err != nil {
					return err
				}
				if err := change(&copy); err != nil {
					return err
				}
				config = copy
				saves++
				return nil
			}
			runtime := control.New(control.Options{RequestTimeout: 200 * time.Millisecond, HelloAttempts: 1})
			defer runtime.Close()
			engine := control.NewCommandEngine(runtime, control.CommandOptions{HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: update})
			client := controller.AttachSharedRuntime(runtime, engine)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := runtime.Open(ctx, board.endpoint()); err != nil {
				t.Fatal(err)
			}
			service := &ipcjson.Service{Client: client, HostInstanceID: "fixture-host",
				HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: update}
			params := json.RawMessage(`{"channel":1,"percent":0}`)
			if method == "controller.action.invoke" {
				params = json.RawMessage(`{"action_id":"pwm.1.off"}`)
			} else if method == "controller.pwm.off" {
				params = nil
			}
			response := service.Dispatch(ctx, ipcjson.Request{Method: method, Params: params})
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			if config.BoardProfiles[identity].PWMRecall["pwm.1"] != 239 || config.BoardProfiles[identity].PWMRecall["pwm.2"] != 555 || saves != 1 {
				t.Fatalf("persisted profile=%+v saves=%d", config.BoardProfiles[identity], saves)
			}
			board.pwmMu.Lock()
			if board.pwmValues[1] != 0 {
				board.pwmMu.Unlock()
				t.Fatal("Off did not reach the framed board")
			}
			board.pwmMu.Unlock()
		})
	}
}
