package ipcjson

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

func TestRFCatalogEntryHasSemanticBoardAssignmentAndObservedTime(t *testing.T) {
	_, controls := appconfig.ProfileDescriptors(appconfig.BoardModeCinemaSeatMotion, false, map[string]string{"pwm.3": "Desk lighting"}, nil)
	seen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	entries := []controller.RFEntryView{{ID: 5, Code: 42, Bits: 24, Protocol: 1, ActionKind: native.RFActionPWM, ActionValue: 3, Behavior: native.RFBehaviorPress}, {ID: 6, Code: 43, Bits: 24, Protocol: 1, ActionKind: native.RFActionNone}}
	result := enrichRFCatalogEntries(entries, controls, func(code uint32, bits, protocol byte) time.Time {
		if code == 42 && bits == 24 && protocol == 1 {
			return seen
		}
		return time.Time{}
	})
	if result[0].ID != 5 || result[0].BoardAssignment.Action != "pwm" || result[0].BoardAssignment.Target != "3" || result[0].BoardAssignment.Behavior != "press" || result[0].BoardAssignment.Label != "Desk lighting · toggle 0/100%" {
		t.Fatalf("wrong mapping %+v", result[0])
	}
	encoded, _ := json.Marshal(result)
	var wire []map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire[0]["last_seen"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("invented or wrong date %s", encoded)
	}
	if _, exists := wire[1]["last_seen"]; exists {
		t.Fatal("unknown date emitted")
	}
	if wire[0]["action_kind"] != float64(native.RFActionPWM) {
		t.Fatal("diagnostic MCU fields lost")
	}
	for _, entry := range []controller.RFEntryView{{ActionKind: 99}, {ActionKind: native.RFActionPWM, ActionValue: 11}, {ActionKind: native.RFActionSide, ActionValue: 2}} {
		if got := describeRFBoardAssignment(entry, controls); got.Action != "unsupported" {
			t.Fatalf("guessed invalid mapping %+v", got)
		}
	}
}

func TestRFCatalogRefreshDoesNotObserveAReception(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	for i := 0; i < 2; i++ {
		if _, err := service.rfCatalog(context.Background(), json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if !service.Client.RFLastSeen(42, 24, 1).IsZero() {
		t.Fatal("catalog refresh invented reception")
	}
}

func TestRFControlPWMConfirmationKeepsStructuredFailure(t *testing.T) {
	err := dispatchRPCError(&control.PWMActionConfirmationError{Cause: context.DeadlineExceeded})
	data, ok := err.Data.(map[string]any)
	if !ok || err.Code != -32010 || data["kind"] != "confirmation_unavailable" || data["resource"] != "pwm" || data["retryable"] != false || data["write_acknowledged"] != true {
		t.Fatalf("lost error envelope %+v", err)
	}
}
