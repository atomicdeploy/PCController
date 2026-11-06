//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestNativeRFMetadataRefreshDoesNotPollTheSerialTable(t *testing.T) {
	owner := "first"
	reads := []bool{}
	p := nativePages{rfCache: &nativeRFCache{}, events: &nativeEventBuffer{}}
	p.rpc = func(_ context.Context, method string, params, result any) error {
		if method != "controller.rf.catalog" {
			t.Fatal(method)
		}
		read := params.(map[string]bool)["read_board"]
		reads = append(reads, read)
		catalog := nativeRFCatalog{HostInstanceID: owner}
		if read {
			catalog.Entries = []nativeRFEntry{{RFEntryView: controllerapi.RFEntryView{ID: 1, Code: 42, Bits: 24, Protocol: 1}}}
		}
		encoded, _ := json.Marshal(catalog)
		return json.Unmarshal(encoded, result)
	}
	state := control.Snapshot{Connected: true, ConnectionGeneration: 7}
	for i := 0; i < 3; i++ {
		catalog, err := p.loadNativeRFCatalog(context.Background(), state)
		if err != nil || len(catalog.Entries) != 1 {
			t.Fatalf("lost learned entries: %+v %v", catalog, err)
		}
		p.events.append(control.Event{Kind: "rf.receive"})
	}
	if len(reads) != 3 || !reads[0] || reads[1] || reads[2] {
		t.Fatalf("ordinary receipts reread the board table: %v", reads)
	}
	owner = "replacement"
	if _, err := p.loadNativeRFCatalog(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if !reads[len(reads)-1] {
		t.Fatal("host replacement reused the previous board catalog")
	}
}
