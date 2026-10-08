package ipcjson

import (
	"context"
	"encoding/json"
	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
	"testing"
)

func TestMediaTimelineRPCRequiresIdentityAndUsesStrictPlanContract(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	service := Service{Client: controller.AttachSharedRuntime(runtime, shell.New(8)), AppInstances: hostui.NewInstanceRegistry()}
	params, _ := json.Marshal(controller.MediaTimelinePlan{ClientID: "consumer:test", Revision: 1})
	request := Request{Method: "controller.media.timeline.prepare", Params: params}
	if service.Dispatch(context.Background(), request).Error == nil {
		t.Fatal("unregistered plan accepted")
	}
	if _, err := service.AppInstances.Upsert(hostui.AppInstance{ID: "consumer:test", Surface: "pealayer", LeaseSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if result := service.Dispatch(context.Background(), request); result.Error == nil {
		t.Fatal("plan accepted without attached board")
	}
	request.Params = json.RawMessage(`{"client_id":"consumer:test","revision":1,"typo":true}`)
	if service.Dispatch(context.Background(), request).Error == nil {
		t.Fatal("unknown field accepted")
	}
	result := service.Dispatch(context.Background(), Request{Method: "controller.media.timeline.get"})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if requestCapability("controller.media.timeline.get", nil) != capabilityRead {
		t.Fatal("timeline inspection not read-only")
	}
}
