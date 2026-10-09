package native

import "testing"

func TestHostMenuDirectoryRoundTripAndGraphValidation(t *testing.T) {
	directory := HostMenuDirectory{Schema: 1, Generation: 7, Entries: []HostMenuDirectoryEntry{
		{ID: 0, Parent: 0x70, Flags: HostMenuVisible | HostMenuSelectable | HostMenuBuiltinLabelOverride | HostMenuLiveContent},
		{ID: 0x80, Parent: HostMenuRoot, Flags: HostMenuVisible | HostMenuSelectable | HostMenuLiveContent},
		{ID: 0x81, Parent: 0x80, Flags: HostMenuVisible | HostMenuSelectable | HostMenuReadOnly | HostMenuLiveContent},
	}}
	payload, err := EncodeHostMenuDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseHostMenuDirectory(payload)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Generation != 7 || len(parsed.Entries) != 3 || parsed.Entries[2].Parent != 0x80 {
		t.Fatalf("unexpected directory round trip: %+v", parsed)
	}

	bad := directory
	bad.Entries = append([]HostMenuDirectoryEntry(nil), directory.Entries...)
	bad.Entries[1].Parent = 0x81
	if _, err := EncodeHostMenuDirectory(bad); err == nil {
		t.Fatal("cyclic host-menu graph was accepted")
	}
}

func TestHostMenuDirectoryRejectsEntryCountOverflow(t *testing.T) {
	entries := make([]HostMenuDirectoryEntry, HostMenuMaximumEntries+1)
	if _, err := EncodeHostMenuDirectory(HostMenuDirectory{Entries: entries}); err == nil {
		t.Fatal("oversize host-menu directory was encoded")
	}
	payload := make([]byte, 3+(HostMenuMaximumEntries+1)*3)
	payload[0] = HostMenuSchema
	payload[2] = HostMenuMaximumEntries + 1
	if _, err := ParseHostMenuDirectory(payload); err == nil {
		t.Fatal("oversize host-menu directory was parsed")
	}
}

func TestHostMenuContentAndStateWireSchemas(t *testing.T) {
	payload, err := EncodeHostMenuContent(HostMenuContent{
		Generation: 4, ID: 0x80, Revision: 9,
		Flags:      HostMenuVisible | HostMenuLiveContent,
		Brightness: 5, Visual: HostMenuVisualEditDim,
		Segments: "HOST", LCDLine1: "PC menu", LCDLine2: "Ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 43 {
		t.Fatalf("content length=%d want 43", len(payload))
	}
	content, err := ParseHostMenuContent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if content.Segments != "HOST" || content.LCDLine1 != "PC menu" || content.LCDLine2 != "Ready" || content.Brightness != 5 || content.Visual != HostMenuVisualEditDim {
		t.Fatalf("unexpected content: %+v", content)
	}
	state, err := ParseHostMenuState([]byte{1, 4, 0x80, HostMenuPhaseLoading, 2, 9})
	if err != nil || state.ActiveID != 0x80 || state.Attempt != 2 {
		t.Fatalf("state parse=%+v err=%v", state, err)
	}
	request, err := ParseHostMenuContentRequest([]byte{1, 4, 0x80, HostMenuReasonRetry, 3})
	if err != nil || request.Reason != HostMenuReasonRetry {
		t.Fatalf("request parse=%+v err=%v", request, err)
	}
}

func TestHostMenuContentRejectsUnrenderableFields(t *testing.T) {
	if _, err := EncodeHostMenuContent(HostMenuContent{ID: 0x80, Segments: "TOO-LONG"}); err == nil {
		t.Fatal("oversize segment label was accepted")
	}
	if _, err := EncodeHostMenuContent(HostMenuContent{ID: 0x80, Segments: "OK", LCDLine1: "line\nfeed"}); err == nil {
		t.Fatal("control character was accepted")
	}
}
