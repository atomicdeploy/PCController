package localdevice

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseEventAcceptsTypedJSONOnly(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		event Event
	}{
		{
			name: "snapshot",
			event: Event{
				Contract: ContractVersion, Type: EventSnapshotUpdated, Sequence: 8, At: now,
				Snapshot: snapshotPointer(testSnapshot(8, PowerOn, now)),
			},
		},
		{
			name: "action",
			event: Event{
				Contract: ContractVersion, Type: EventActionCompleted, Sequence: 9, At: now,
				Result: &ActionResult{
					Contract: ContractVersion, Accepted: true, Action: ActionPowerOff,
					CompletedAt: now,
				},
			},
		},
		{
			name: "notice",
			event: Event{
				Contract: ContractVersion, Type: EventDeviceNotice, Sequence: 10, At: now,
				Notice: "maintenance window",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.event)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseEvent(encoded)
			if err != nil || parsed.Type != test.event.Type || parsed.Sequence != test.event.Sequence {
				t.Fatalf("parsed=%#v err=%v", parsed, err)
			}
		})
	}
}

func TestParseEventRejectsAmbiguousOrUnboundedDocuments(t *testing.T) {
	t.Parallel()
	now := "2026-08-02T12:00:00Z"
	invalid := [][]byte{
		nil,
		[]byte("not-json"),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"device.notice","sequence":1,"at":"` + now + `","notice":"ok","unknown":true}`),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"device.notice","sequence":0,"at":"` + now + `","notice":"ok"}`),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"device.notice","sequence":1,"at":"` + now + `","notice":""}`),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"snapshot.updated","sequence":1,"at":"` + now + `","notice":"wrong"}`),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"action.completed","sequence":1,"at":"` + now + `","result":{"contract":"pccontroller.local-device/v1","accepted":false,"action":"power.on","completed_at":"` + now + `"}}`),
		[]byte(`{"contract":"wrong","type":"device.notice","sequence":1,"at":"` + now + `","notice":"ok"}`),
		[]byte(`{"contract":"pccontroller.local-device/v1","type":"device.notice","sequence":1,"at":"` + now + `","notice":"bad\u0000value"}`),
		[]byte(strings.Repeat("x", maxEventBytes+1)),
	}
	for index, encoded := range invalid {
		if _, err := ParseEvent(encoded); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("invalid[%d] error=%v", index, err)
		}
	}
}
