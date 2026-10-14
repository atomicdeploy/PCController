package ipcjson

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func rfBindingTestService(t *testing.T) (*Service, *appconfig.Config) {
	t.Helper()
	runtime := control.New(control.Options{})
	t.Cleanup(func() { runtime.Close() })
	config := appconfig.Defaults()
	clone := func(value appconfig.Config) appconfig.Config {
		raw, _ := json.Marshal(value)
		var copy appconfig.Config
		if err := json.Unmarshal(raw, &copy); err != nil {
			t.Fatal(err)
		}
		return copy
	}
	service := &Service{Client: controller.AttachSharedRuntime(runtime, shell.New(8)), HostConfig: func() appconfig.Config { return clone(config) }}
	service.UpdateHostConfig = func(mutate func(*appconfig.Config) error) error {
		candidate := clone(config)
		if err := mutate(&candidate); err != nil {
			return err
		}
		if err := candidate.Validate(); err != nil {
			return err
		}
		config = candidate
		return nil
	}
	return service, &config
}

func TestRFBindingCRUDPreservesUnrelatedRulesAndRejectsAmbiguity(t *testing.T) {
	service, config := rfBindingTestService(t)
	config.Automations = []appconfig.Automation{{Name: "existing", Enabled: true, Match: appconfig.AutomationMatch{Kind: "host.ready"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "ready"}}}}
	code := uint32(12345)
	rule := appconfig.Automation{Name: "Play", Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "down"}, Actions: []appconfig.AutomationAction{{Type: "app", AppKind: "pealayer.toggle", AppTarget: "pealayer"}}}
	put := func(rule appconfig.Automation, previous string) error {
		raw, _ := json.Marshal(map[string]any{"binding": rule, "previous_name": previous})
		_, err := service.putRFBinding(raw)
		return err
	}
	if err := put(rule, ""); err != nil {
		t.Fatal(err)
	}
	if len(config.Automations) != 2 || config.Automations[0].Name != "existing" {
		t.Fatal("unrelated rule lost")
	}
	rule.Name = "Duplicate"
	if err := put(rule, ""); err == nil {
		t.Fatal("duplicate signal gesture accepted")
	}
	rule.Name = "Pause"
	rule.Actions[0].AppKind = "pealayer.pause"
	if err := put(rule, "Play"); err != nil {
		t.Fatal(err)
	}
	if len(config.Automations) != 2 || config.Automations[1].Name != "Pause" {
		t.Fatal("rename duplicated rule")
	}
	if _, err := service.removeRFBinding(json.RawMessage(`{"name":"existing"}`)); err == nil {
		t.Fatal("removed non-RF rule")
	}
	if _, err := service.removeRFBinding(json.RawMessage(`{"name":"Pause"}`)); err != nil {
		t.Fatal(err)
	}
	if len(config.Automations) != 1 {
		t.Fatal("assignment not removed")
	}
}

func TestRFBindingControlValidationHasHumanMessageAndNoSideEffects(t *testing.T) {
	for _, test := range []struct{ id, reason string }{
		{"", "required"}, {"pwm.99.on", "not_advertised"},
		{"pwm.2.unknown", "not_advertised"}, {"pwm.2. on", "invalid_format"},
	} {
		t.Run(test.reason+test.id, func(t *testing.T) {
			service, config := rfBindingTestService(t)
			before, _ := json.Marshal(config)
			mutations := 0
			service.UpdateHostConfig = func(func(*appconfig.Config) error) error { mutations++; return nil }
			code := uint32(42)
			rule := appconfig.Automation{Name: "Draft", Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "down"}, Actions: []appconfig.AutomationAction{
				{Type: "virtual-key", VirtualKey: "F23", HoldMS: 50}, {Type: "control", ActionID: test.id},
			}}
			raw, _ := json.Marshal(map[string]any{"binding": rule, "allow_keyboard": true})
			response := service.Dispatch(context.Background(), Request{Method: "controller.rf.binding.put", Params: raw})
			if response.Error == nil || response.Error.Code != -32602 || response.Error.Message != "Choose an available control action before saving this assignment" {
				t.Fatalf("wrong human error %+v", response.Error)
			}
			data, ok := response.Error.Data.(map[string]any)
			if !ok || data["kind"] != "validation_error" || data["resource"] != "rf_binding" || data["field"] != "binding.actions[1].action_id" || data["reason"] != test.reason {
				t.Fatalf("missing technical disclosure %+v", response.Error.Data)
			}
			after, _ := json.Marshal(config)
			if mutations != 0 || string(before) != string(after) {
				t.Fatal("invalid draft touched persistence or keyboard policy")
			}
		})
	}
}

func TestRFBindingAdvertisedPWMControlCanBeSavedWithoutActuation(t *testing.T) {
	service, config := rfBindingTestService(t)
	raw := json.RawMessage(`{"binding":{"name":"Lighting","enabled":true,"match":{"kind":"rf.gesture","source":"rf","rf_code":42,"rf_bits":24,"rf_protocol":1,"gesture":"down"},"actions":[{"type":"control","action_id":"pwm.2.increase"}]}}`)
	if _, err := service.putRFBinding(raw); err != nil {
		t.Fatal(err)
	}
	if len(config.Automations) != 1 || config.Automations[0].Actions[0].ActionID != "pwm.2.increase" {
		t.Fatal("valid catalog action was not persisted")
	}
}

func TestRFBindingPWMAbsoluteValueAndRelativeStepsPreserveIntent(t *testing.T) {
	for _, command := range []string{"pwm set 0 0", "pwm set 0 12.5", "pwm set 0 35", "pwm set 0 100"} {
		t.Run(command, func(t *testing.T) {
			service, config := rfBindingTestService(t)
			step := 2.5
			channel := appconfig.PWMChannel(config.UI, 0)
			channel.StepPercent = &step
			config.UI.PWMChannels[appconfig.PWMChannelKey(0)] = channel
			code := uint32(42)
			rule := appconfig.Automation{Name: "MOSFET", Enabled: true,
				Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "down"},
				Actions: []appconfig.AutomationAction{
					{Type: "board", Command: command},
					{Type: "control", ActionID: "pwm.0.increase"},
					{Type: "control", ActionID: "pwm.0.decrease"},
				},
			}
			raw, _ := json.Marshal(map[string]any{"binding": rule})
			if _, err := service.putRFBinding(raw); err != nil {
				t.Fatal(err)
			}
			if len(config.Automations) != 1 {
				t.Fatal("RF rule was not saved exactly once")
			}
			got := config.Automations[0].Actions
			if len(got) != 3 || got[0].Command != command || got[1].ActionID != "pwm.0.increase" || got[2].ActionID != "pwm.0.decrease" {
				t.Fatalf("RF value or ordered relative actions changed: %+v", got)
			}
			if appconfig.PWMStepPercent(appconfig.PWMChannel(config.UI, 0)) != step {
				t.Fatal("RF assignment rewrote channel step")
			}
			if service.Client.Snapshot().Connected {
				t.Fatal("saving an assignment connected or actuated a board")
			}
		})
	}
}

func TestRFBindingKeyboardRequiresExplicitPolicyConsent(t *testing.T) {
	service, config := rfBindingTestService(t)
	raw := json.RawMessage(`{"binding":{"name":"Keyboard","enabled":true,"match":{"kind":"rf.gesture","source":"rf","rf_code":12345,"rf_bits":24,"rf_protocol":1,"gesture":"down"},"actions":[{"type":"virtual-key","virtual_key":"F23","hold_ms":50}]}}`)
	if _, err := service.putRFBinding(raw); err == nil {
		t.Fatal("unallowlisted key accepted")
	}
	var value map[string]any
	json.Unmarshal(raw, &value)
	value["allow_keyboard"] = true
	allowed, _ := json.Marshal(value)
	if _, err := service.putRFBinding(allowed); err != nil {
		t.Fatal(err)
	}
	if !config.OSActions.VirtualKeys.Enabled {
		t.Fatal("explicit consent not applied")
	}
	if len(config.Automations) != 1 {
		t.Fatal("failed mutation left a partial assignment")
	}
}

func TestRFBindingChordConsentIsExact(t *testing.T) {
	service, config := rfBindingTestService(t)
	raw := json.RawMessage(`{"allow_keyboard":true,"binding":{"name":"Shortcut","enabled":true,"match":{"kind":"rf.gesture","source":"rf","rf_code":12345,"rf_bits":24,"rf_protocol":1,"gesture":"down"},"actions":[{"type":"virtual-key","virtual_key":"shift+ctrl+s","hold_ms":50}]}}`)
	if _, err := service.putRFBinding(raw); err != nil {
		t.Fatal(err)
	}
	if !hostos.KeyStrokeAllowed(config.OSActions.VirtualKeys, "CTRL+SHIFT+S") || hostos.KeyStrokeAllowed(config.OSActions.VirtualKeys, "S") || hostos.KeyStrokeAllowed(config.OSActions.VirtualKeys, "CTRL+S") {
		t.Fatal("chord authorization broadened")
	}
}

func TestRFApplicationAutomationStartsWithoutRPCAndUsesActualAck(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	registry := hostui.NewInstanceRegistry()
	if _, err := registry.Upsert(hostui.AppInstance{ID: "test:player", Surface: "pealayer", Values: map[string]string{"app_actions": "pealayer.toggle"}}); err != nil {
		t.Fatal(err)
	}
	var coordinator *hostui.ActionCoordinator
	coordinator = hostui.NewActionCoordinator(registry, func(action hostui.AppAction) error {
		go func() {
			_, _ = coordinator.Ack(hostui.ActionAck{OperationID: action.OperationID, InstanceID: "test:player", DeliveryID: action.Metadata[hostui.ActionDeliveryIDKey], State: hostui.ActionStateApplied})
		}()
		return nil
	})
	service := Service{Client: controller.AttachSharedRuntime(runtime, shell.New(8)), AppActionSubmit: coordinator.Submit, AppActionOutcome: coordinator.Outcome}
	service.InitializeAutomationActions()
	code := uint32(12345)
	config := appconfig.Defaults()
	config.Automations = []appconfig.Automation{{Name: "RF Play", Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "down"}, Actions: []appconfig.AutomationAction{{Type: "app", AppKind: "pealayer.toggle", AppTarget: "pealayer"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go control.RunAutomations(ctx, runtime, shell.New(8), func() appconfig.Config { return config })
	// Subscription initialization is bounded; repeatedly publish a single gesture
	// until the completion event is observable, without any network or OS keys.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		after := runtime.LatestEventID()
		runtime.PublishStructuredEvent(control.Event{Kind: "rf.gesture", Source: "rf", Gesture: "down", RFCode: code, RFBits: 24, RFProtocol: 1})
		wait, stop := context.WithTimeout(ctx, 50*time.Millisecond)
		event, err := runtime.WaitEvent(wait, after, "automation")
		stop()
		if err == nil {
			if !strings.HasPrefix(event.Text, "RF Play completed for event ") {
				t.Fatalf("unexpected outcome %s", event.Text)
			}
			return
		}
	}
	t.Fatal("RF application action never acknowledged")
}
