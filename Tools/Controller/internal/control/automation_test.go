package control

import (
	"context"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func TestAutomationMatchesNormalizedKeyAndRFEvents(t *testing.T) {
	rfID := byte(9)
	key := Event{Kind: "key", Frame: native.Frame{
		Opcode: native.OpEvent,
		Payload: []byte{
			native.EventKey, 2, 5, native.InputSourceRF, rfID,
		},
	}}
	if !automationMatches(appconfig.AutomationMatch{
		Kind: "key", Key: 3, Gesture: "down", Source: "rf", RFID: &rfID,
	}, key) {
		t.Fatal("normalized RF-sourced key event did not match")
	}
	code := uint32(0x123456)
	received := Event{Kind: "rf.receive", Frame: native.Frame{
		Opcode: native.OpEvent,
		Payload: []byte{
			native.EventRFReceived,
			0x56, 0x34, 0x12, 0,
			24, 1, 0x5E, 0x01, rfID,
		},
	}}
	if !automationMatches(appconfig.AutomationMatch{
		Kind: "rf", RFCode: &code, RFProtocol: 1, RFID: &rfID,
	}, received) {
		t.Fatal("raw RF receive event did not match hierarchical rf kind")
	}
	synthetic := Event{
		Kind: "rf.gesture", Gesture: "up", Source: "rf",
		RFCode: code, RFProtocol: 1, RFID: rfID, HaveRFID: true,
	}
	if !automationMatches(appconfig.AutomationMatch{
		Kind: "rf", Gesture: "up", RFCode: &code, RFID: &rfID,
	}, synthetic) {
		t.Fatal("inferred RF gesture event did not match")
	}
}

func TestAutomationEmitActionProducesIPCVisibleEvent(t *testing.T) {
	runtime := New(Options{})
	engine := shell.New(10)
	config := appconfig.Defaults()
	config.Automations = []appconfig.Automation{{
		Name: "notify", Enabled: true,
		Match: appconfig.AutomationMatch{Kind: "door"},
		Actions: []appconfig.AutomationAction{{
			Type: "emit", Event: "door-rule",
		}},
	}}
	after := runtime.LatestEventID()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ExecuteAutomationByName(
		ctx,
		runtime,
		engine,
		config,
		"notify",
	); err != nil {
		t.Fatal(err)
	}
	event, err := runtime.WaitEvent(ctx, after, "automation.door-rule")
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != "automation.door-rule" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestAutomationCanInvokeNamedMacroThroughSharedCommandEngine(t *testing.T) {
	runtime := New(Options{})
	engine := shell.New(10)
	var received []string
	if err := engine.Register(shell.Command{
		Name: "macro",
		Run: func(_ context.Context, args []string) (string, error) {
			received = append([]string(nil), args...)
			return "started", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	config := appconfig.Defaults()
	config.Automations = []appconfig.Automation{{
		Name: "door-macro", Enabled: true,
		Match:   appconfig.AutomationMatch{Kind: "door"},
		Actions: []appconfig.AutomationAction{{Type: "macro", Macro: "welcome effect"}},
	}}
	if err := ExecuteAutomationByName(
		context.Background(), runtime, engine, config, "door-macro",
	); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != "play" || received[1] != "welcome effect" {
		t.Fatalf("automation did not route through macro play: %q", received)
	}
}
