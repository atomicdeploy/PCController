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

func TestRFApplicationAutomationUsesTrackedCoordinatorAndActualAck(t *testing.T) {
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
	service.configureAutomationActions()
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
