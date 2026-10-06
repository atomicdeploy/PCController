package ipcjson

import (
	"context"
	"encoding/json"
	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
	"testing"
	"time"
)

func TestMediaPlaybackRPCRequiresRegisteredIdentityAndPublishesEvent(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	service := Service{Client: controller.AttachSharedRuntime(runtime, shell.New(8)), AppInstances: hostui.NewInstanceRegistry()}
	params, _ := json.Marshal(controller.MediaPlaybackUpdate{ClientID: "pealayer:test", Sequence: 1, Loaded: true, PositionMS: 65_000, Rate: 1})
	call := Request{Method: "controller.media.playback.update", Params: params}
	if service.Dispatch(context.Background(), call).Error == nil {
		t.Fatal("accepted unregistered consumer")
	}
	_, err := service.AppInstances.Upsert(hostui.AppInstance{ID: "pealayer:test", Surface: "pealayer", LeaseSeconds: 30, Values: map[string]string{"application": "Pealayer", "version": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	cursor := service.Client.LatestEventID()
	result := service.Dispatch(context.Background(), call)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := service.Client.NextEventStream(ctx, cursor, "media.playback", "state")
	if err != nil || event.Source != "pealayer:test" || event.Metadata["position_ms"] != "65000" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	snapshot := service.controllerSnapshot()
	if snapshot.MediaPlayback.PositionMS != 65_000 {
		t.Fatalf("snapshot %v", snapshot.MediaPlayback)
	}
	if service.Dispatch(context.Background(), call).Error == nil {
		t.Fatal("accepted stale playback event")
	}
}
