package device

import (
	"net/url"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/model"
)

func TestParseEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		ok        bool
		kind      model.EventKind
		state     model.OutputState
		message   string
		icon      int
		beepCount int
	}{
		{name: "on", raw: "ON", ok: true, kind: model.EventSnapshot, state: model.StateOn},
		{name: "off case and whitespace", raw: "  off\r\n", ok: true, kind: model.EventSnapshot, state: model.StateOff},
		{name: "plain message", raw: "message: The device changed", ok: true, kind: model.EventMessage, message: "The device changed"},
		{name: "plain message case", raw: "MESSAGE:Hello", ok: true, kind: model.EventMessage, message: "Hello"},
		{name: "firmware message", raw: "msg:3,Hello+from%20Controller%21", ok: true, kind: model.EventMessage, message: "Hello from Controller!", icon: 3},
		{name: "firmware unicode", raw: "msg:12," + url.QueryEscape("کنترلر"), ok: true, kind: model.EventMessage, message: "کنترلر", icon: 12},
		{name: "beep", raw: "beep:3", ok: true, kind: model.EventBeep, beepCount: 3},
		{name: "unrelated", raw: "temperature:22", ok: false},
		{name: "state substring", raw: "device is ON", ok: false},
		{name: "empty message", raw: "message: ", ok: false},
		{name: "missing icon", raw: "msg:,hello", ok: false},
		{name: "negative icon", raw: "msg:-1,hello", ok: false},
		{name: "huge icon", raw: "msg:256,hello", ok: false},
		{name: "bad escape", raw: "msg:1,%ZZ", ok: false},
		{name: "zero beep", raw: "beep:0", ok: false},
		{name: "excessive beep", raw: "beep:11", ok: false},
		{name: "bad beep", raw: "beep:many", ok: false},
		{name: "embedded nul", raw: "message:a\x00b", ok: false},
		{name: "control character", raw: "message:a\x01b", ok: false},
		{name: "oversized event", raw: strings.Repeat("x", maxRemoteEventBytes+1), ok: false},
		{name: "oversized message", raw: "message:" + strings.Repeat("x", maxRemoteMessageBytes+1), ok: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event, ok := ParseEvent(test.raw)
			if ok != test.ok {
				t.Fatalf("ParseEvent(%q) ok = %v, want %v (event=%#v)", test.raw, ok, test.ok, event)
			}
			if !ok {
				return
			}
			if event.Kind != test.kind || event.Snapshot.State != test.state ||
				event.Message != test.message || event.Icon != test.icon ||
				event.BeepCount != test.beepCount {
				t.Fatalf("ParseEvent(%q) = %#v", test.raw, event)
			}
			if !event.FromRemote {
				t.Fatalf("ParseEvent(%q) did not mark event remote", test.raw)
			}
		})
	}
}
