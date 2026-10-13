package controller

import (
	"encoding/json"
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestPublishPWMValuesDeduplicatesOnlyIdenticalSnapshots(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := &Client{runtime: runtime}
	initial := PWMValues{Available: true, SelectedChannel: 11}
	initial.Values[11] = 642
	client.PublishPWMValues(initial)
	if runtime.Snapshot().PWMRevision != 1 {
		t.Fatal("first readback missing")
	}
	client.PublishPWMValues(initial)
	if runtime.Snapshot().PWMRevision != 1 {
		t.Fatal("unchanged readback republished")
	}
	changed := initial
	changed.Values[11]++
	client.PublishPWMValues(changed)
	if runtime.Snapshot().PWMRevision != 2 {
		t.Fatal("changed value missing")
	}
	changed.SelectedChannel = 12
	client.PublishPWMValues(changed)
	if runtime.Snapshot().PWMRevision != 3 {
		t.Fatal("selection change missing")
	}
}

func TestSnapshotDecodeOfflineUnknownSettings(t *testing.T) {
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(`{"connected":false,"have_settings":false,"settings":{"motion_break":0}}`), &snapshot); err != nil {
		t.Fatalf("truthful offline snapshot must decode: %v", err)
	}
	if snapshot.HaveSettings {
		t.Fatal("unknown settings became available")
	}
	if err := json.Unmarshal([]byte(`{"connected":true,"have_settings":true,"settings":{"motion_break":0}}`), &snapshot); err == nil {
		t.Fatal("available settings must still pass the native write validator")
	}
}
