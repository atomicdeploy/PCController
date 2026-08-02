package ipcjson

import (
	"context"
	"testing"

	controller "pccontroller.local/controller"
)

func TestLastSessionSnapshotRPCIsReadOnlyAndReturnsProviderResult(t *testing.T) {
	client := controller.New(controller.Options{})
	defer client.Shutdown()
	calls := 0
	service := &Service{
		Client: client,
		LastSessionSnapshot: func() (any, error) {
			calls++
			return map[string]any{
				"path": "C:/data/state/last-session.json", "exists": true,
			}, nil
		},
	}
	for _, method := range []string{
		"controller.session.snapshot", "controller.session.snapshot.last",
	} {
		response := service.Dispatch(context.Background(), Request{
			JSONRPC: Version, Method: method,
		})
		if response.Error != nil {
			t.Fatalf("%s: %v", method, response.Error)
		}
		result, ok := response.Result.(map[string]any)
		if !ok || result["exists"] != true || result["path"] == "" {
			t.Fatalf("%s result=%#v", method, response.Result)
		}
		if capability := requestCapability(method, nil); capability != capabilityRead {
			t.Fatalf("%s capability=%q", method, capability)
		}
	}
	if calls != 2 {
		t.Fatalf("provider calls=%d", calls)
	}

	missing := (&Service{Client: client}).Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.session.snapshot.last",
	})
	if missing.Error == nil {
		t.Fatal("missing graceful-exit snapshot provider was accepted")
	}
}
