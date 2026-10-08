package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func TestMediaAuthorityRPCStructuredConflictAndOwnerConsent(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	service := Service{Client: controller.AttachSharedRuntime(runtime, shell.New(8)), AppInstances: hostui.NewInstanceRegistry()}
	call := func(id, operation, requester string) Response {
		params, _ := json.Marshal(map[string]string{"client_id": id, "operation": operation, "requester_id": requester})
		return service.Dispatch(context.Background(), Request{Method: "controller.media.authority.change", ClientID: id, Params: params})
	}
	if call("cafe", "request", "").Error == nil {
		t.Fatal("unregistered authority admitted")
	}
	for _, id := range []string{"cafe", "david"} {
		if _, err := service.AppInstances.Upsert(hostui.AppInstance{ID: id, Surface: "pealayer", LeaseSeconds: 30, Values: map[string]string{"host": id}}); err != nil {
			t.Fatal(err)
		}
	}
	if result := call("cafe", "request", ""); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := call("david", "request", ""); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := call("david", "accept", "david"); result.Error == nil || result.Error.Code != -32009 {
		t.Fatalf("requester stole ownership: %+v", result)
	}
	if result := call("cafe", "accept", "david"); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := call("david", "lock", ""); result.Error != nil {
		t.Fatal(result.Error)
	}
	result := call("cafe", "request", "")
	if result.Error == nil || result.Error.Code != -32009 {
		t.Fatalf("production lock: %+v", result)
	}
	data, err := json.Marshal(result.Error.Data)
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Kind      string                          `json:"kind"`
		Authority controller.MediaAuthorityStatus `json:"authority"`
	}
	if err = json.Unmarshal(data, &detail); err != nil || detail.Kind != "authority_locked" || detail.Authority.OwnerLabel != "david" {
		t.Fatalf("missing typed context: %s %v", data, err)
	}
}
