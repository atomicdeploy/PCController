package fleet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validRoute(now time.Time) RouteEnvelope {
	return RouteEnvelope{
		RouteID:          "00112233445566778899aabbccddeeff",
		OperationID:      "operation-1",
		TraceID:          "trace-1",
		OriginInstanceID: "host-a",
		OriginBoardID:    "board-a",
		Target:           Destination{BoardID: "board-b"},
		HopLimit:         DefaultHopLimit,
		Deadline:         now.Add(time.Minute),
		Intent: Intent{
			Domain: "indicator",
			Name:   "led.set",
			Arguments: map[string]json.RawMessage{
				"enabled": json.RawMessage(`true`),
			},
		},
	}
}

func TestRouteJSONUsesLivingUnversionedContractAndToleratesAdditions(t *testing.T) {
	now := time.Now().UTC()
	encoded, err := json.Marshal(validRoute(now))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if _, present := document["schema"]; present {
		t.Fatalf("writer emitted a protocol generation: %s", encoded)
	}

	document["future_capability"] = json.RawMessage(`{"enabled":true}`)
	extended, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(extended)))
	decoder.DisallowUnknownFields()
	var decoded RouteEnvelope
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("tolerant living-contract decode rejected additive fields: %v", err)
	}
	if err := decoded.ValidateAt(now); err != nil {
		t.Fatalf("decoded route rejected: %v", err)
	}
}

func TestRouteValidateAndAdvance(t *testing.T) {
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	route := validRoute(now)
	if err := route.ValidateAt(now); err != nil {
		t.Fatalf("valid route rejected: %v", err)
	}
	advanced, err := route.Advance(now)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if advanced.HopCount != 1 || route.HopCount != 0 {
		t.Fatalf("hop mutation original=%d advanced=%d", route.HopCount, advanced.HopCount)
	}
	advanced.Intent.Arguments["enabled"][0] = 'f'
	if string(route.Intent.Arguments["enabled"]) != "true" {
		t.Fatal("advance did not isolate intent arguments")
	}
}

func TestRouteRejectsAmbiguousOrMissingTarget(t *testing.T) {
	now := time.Now().UTC()
	for name, target := range map[string]Destination{
		"missing":   {},
		"ambiguous": {BoardID: "board-a", GroupID: "group-a"},
	} {
		t.Run(name, func(t *testing.T) {
			route := validRoute(now)
			route.Target = target
			if err := route.ValidateAt(now); err == nil {
				t.Fatal("invalid target accepted")
			}
		})
	}
}

func TestGroupRouteRequiresCompleteCoordinatorPosition(t *testing.T) {
	now := time.Now().UTC()
	route := validRoute(now)
	route.Target = Destination{GroupID: "bench-mirrors"}
	if err := route.ValidateAt(now); err == nil {
		t.Fatal("group route without coordinator accepted")
	}
	route.Coordinator = CoordinatorPosition{ID: "host-a", Epoch: 3, Sequence: 9}
	if err := route.ValidateAt(now); err != nil {
		t.Fatalf("group route rejected: %v", err)
	}
	route.Coordinator.Sequence = 0
	if err := route.ValidateAt(now); err == nil {
		t.Fatal("partial coordinator position accepted")
	}
}

func TestRouteDeadlineAndHopLimit(t *testing.T) {
	now := time.Now().UTC()
	route := validRoute(now)
	route.Deadline = now
	if err := route.ValidateAt(now); !errors.Is(err, ErrDeadlineExpired) {
		t.Fatalf("deadline error=%v", err)
	}
	route = validRoute(now)
	route.HopCount = route.HopLimit
	if _, err := route.Advance(now); !errors.Is(err, ErrHopLimitReached) {
		t.Fatalf("hop error=%v", err)
	}
}

func TestRouteRejectsMalformedIdentityAndIntent(t *testing.T) {
	now := time.Now().UTC()
	tests := map[string]func(*RouteEnvelope){
		"route id": func(route *RouteEnvelope) { route.RouteID = "not-a-route" },
		"uppercase route id": func(route *RouteEnvelope) {
			route.RouteID = "00112233445566778899AABBCCDDEEFF"
		},
		"operation id": func(route *RouteEnvelope) { route.OperationID = "contains space" },
		"padded operation id": func(route *RouteEnvelope) {
			route.OperationID = " operation-1"
		},
		"origin board": func(route *RouteEnvelope) { route.OriginBoardID = "bad/board" },
		"intent domain": func(route *RouteEnvelope) {
			route.Intent.Domain = "Host OS"
		},
		"argument json": func(route *RouteEnvelope) {
			route.Intent.Arguments["enabled"] = json.RawMessage(`{`)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			route := validRoute(now)
			mutate(&route)
			if err := route.ValidateAt(now); err == nil {
				t.Fatal("invalid route accepted")
			}
		})
	}
}
