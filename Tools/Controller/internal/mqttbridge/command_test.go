package mqttbridge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMQTTConfigValidation(t *testing.T) {
	valid := Config{Enabled: true, Broker: "mqtts://broker.example", Topic: "pccontroller/cafe"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, broker := range []string{"mqtt://192.168.1.5", "mqtts://user:secret@broker.example", "mqtts://broker.example/path", "mqtts://broker.example:65536", "mqtts://broker.example?token=secret"} {
		config := valid
		config.Broker = broker
		if config.Validate() == nil {
			t.Errorf("invalid broker accepted: %q", broker)
		}
	}
	for _, topic := range []string{"", "pccontroller/#", "/test", "a//b", "a/+"} {
		config := valid
		config.Topic = topic
		if config.Validate() == nil {
			t.Errorf("invalid topic accepted: %q", topic)
		}
	}
	secret := valid
	secret.Username, secret.Password = "operator", "must-not-serialize"
	body, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(body, &fields) != nil || fields["password"] != nil {
		t.Fatal("MQTT runtime secret serialized")
	}
}

func TestMQTTCommandAdmission(t *testing.T) {
	now := time.Unix(1800000000, 0)
	owner := Identity{"board", "boot"}
	seen := make(map[string]int64)
	payload := []byte(`{"command":"rpc","method":"controller.pwm.set","request_id":"unique_1","board_id":"board","boot_id":"boot","expires_at":1800000030}`)
	check := func(want string, data []byte, retained, dup, commands bool, id Identity, when time.Time) {
		t.Helper()
		_, got := admit(data, retained, dup, Config{AllowCommands: commands}, id, seen, when)
		if got.Code != want || got.Accepted != (want == "accepted") {
			t.Fatalf("want %s: %+v", want, got)
		}
	}
	check("invalid_request_id", []byte(`{"command":"status"}`), false, false, false, owner, now)
	check("invalid_request_id", []byte(`{"command":"telemetry","request_id":""}`), false, false, false, owner, now)
	check("accepted", []byte(`{"command":"status","request_id":"read1"}`), false, false, false, owner, now)
	check("read_only", payload, false, false, false, owner, now)
	check("replayed_delivery", payload, true, false, true, owner, now)
	check("replayed_delivery", payload, false, true, true, owner, now)
	check("stale_owner", payload, false, false, true, Identity{"board", "new-boot"}, now)
	check("invalid_deadline", payload, false, false, true, owner, now.Add(30*time.Second))
	check("accepted", payload, false, false, true, owner, now)
	check("duplicate_request", payload, false, false, true, owner, now)
	check("invalid_json", []byte(`{"command":"status","request_id":"read1"} {}`), false, false, false, owner, now)
}
