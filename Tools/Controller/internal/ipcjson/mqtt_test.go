package ipcjson

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/mqttbridge"
)

func TestMQTTProvisioningIsLocalAndRedacted(t *testing.T) {
	s, config := browserUIConfigTestService(t)
	ctx := context.Background()
	params := json.RawMessage(`{"enabled":false,"broker":"mqtts://broker.example","topic":"pccontroller/test","username":"board","password_ref":"env:PCCONTROLLER_TEST_MQTT_PASSWORD","allow_commands":false}`)
	request := Request{Method: "controller.mqtt.config.set", Params: params}
	s.AuthorizationDisabled = true
	config.IPC.AllowRemote = true
	denied := s.dispatch(ctx, request, Access{Transport: "websocket", Remote: true, authenticated: true})
	if denied.Error == nil || config.Integrations.MQTT.Broker != "" {
		t.Fatal("remote provision bypassed local-only fence")
	}
	result := s.Dispatch(ctx, request)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if config.Integrations.MQTT.Broker != "mqtts://broker.example" || config.UI.AppTitle != "Workshop Controller" {
		t.Fatal("unexpected configuration mutation")
	}
	config.Integrations.MQTT.Password = "never-disclose-this"
	encoded, err := json.Marshal(s.Dispatch(ctx, Request{Method: "controller.mqtt.status"}))
	if err != nil || strings.Contains(string(encoded), "never-disclose-this") || !strings.Contains(string(encoded), "password_ref") {
		t.Fatalf("unexpected status projection: %s (%v)", encoded, err)
	}
}

func TestMQTTDoesNotInheritLocalOrAlphaAuthority(t *testing.T) {
	s, config := browserUIConfigTestService(t)
	s.AuthorizationDisabled = true
	access := Access{Remote: true, Transport: "mqtt", authenticated: true}
	if s.authorizeCapability(access, "controller.pwm.set", capabilityBoard) == nil {
		t.Fatal("disabled MQTT gained control")
	}
	config.Integrations.MQTT = mqttbridge.Config{Enabled: true, AllowCommands: true}
	if err := s.authorizeCapability(access, "controller.pwm.set", capabilityBoard); err != nil {
		t.Fatal("outbound-only board grant requires unrelated LAN listener:", err)
	}
	if s.authorizeCapability(access, "controller.quit", capabilityShutdown) == nil {
		t.Fatal("MQTT gained shutdown authority")
	}
	if s.authorizeCapability(access, "controller.motion.hold.start", capabilityBoard) == nil {
		t.Fatal("MQTT gained non-allowlisted board authority")
	}
	ctx := context.Background()
	for _, method := range []string{"controller.opcode.send", "controller.command.execute", "controller.reset", "controller.update.firmware", "controller.bridge.call", "controller.motion.hold.start", "controller.mqtt.config.set"} {
		got := s.mqttCommand(ctx, config.Integrations.MQTT, mqttbridge.Command{Method: method})
		if got.Accepted || got.Code != "method_denied" {
			t.Fatalf("%s: %+v", method, got)
		}
	}
	got := s.mqttCommand(ctx, config.Integrations.MQTT, mqttbridge.Command{Method: "controller.action.invoke", Params: json.RawMessage(`{"action_id":"motion.left.up"}`)})
	if got.Code != "method_denied" {
		t.Fatalf("motion through catalog: %+v", got)
	}
	got = s.mqttCommand(ctx, config.Integrations.MQTT, mqttbridge.Command{Method: "controller.pwm.set", Params: json.RawMessage(`{"channel":0,"percent":50}`)})
	if got.Code != "invalid_params" {
		t.Fatalf("accepted unfenced PWM command: %+v", got)
	}
	_, observed := s.mqttObservation()
	state := observed.(map[string]any)
	if state["connected"] != false || state["supply_mv"] != nil || state["pwm_values"] != nil {
		t.Fatal("offline state fabricated observations")
	}
}
