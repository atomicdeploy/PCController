package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	controller "pccontroller.local/controller"
)

func TestPWMWriteTraceRPCDoesNotRequireBoardOrChangeOutputs(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	for _, bad := range []string{`{}`, `{"channel":1}`, `{"channel":16,"duration_ms":15000}`, `{"channel":1,"duration_ms":0}`, `{"channel":1,"duration_ms":60001}`} {
		response := service.Dispatch(context.Background(), Request{Method: "controller.pwm.trace.start", Params: json.RawMessage(bad)})
		if response.Error == nil {
			t.Fatal("invalid trace accepted")
		}
	}
	response := service.Dispatch(context.Background(), Request{Method: "controller.pwm.trace.start", Params: json.RawMessage(`{"channel":1,"duration_ms":15000}`)})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	trace := response.Result.(controller.PWMWriteTrace)
	if !trace.Active || trace.Channel != 1 || len(trace.Records) != 0 {
		t.Fatalf("wrong trace %+v", trace)
	}
	if service.Client.Snapshot().Connected {
		t.Fatal("diagnostic opened a board")
	}
	if requestCapability("controller.pwm.trace.get", nil) != capabilityRead || requestCapability("controller.pwm.trace.start", nil) != capabilityEvents || requestCapability("controller.pwm.trace.stop", nil) != capabilityEvents {
		t.Fatal("diagnostic grants output permission")
	}
	stop := service.Dispatch(context.Background(), Request{Method: "controller.pwm.trace.stop", Params: json.RawMessage(`{}`)})
	if stop.Error != nil || stop.Result.(controller.PWMWriteTrace).Active {
		t.Fatalf("stop failed %+v", stop)
	}
}
