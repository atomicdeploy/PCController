package control

import (
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestMenuGenerationFallbacksKeepStatusAtCorrectID(t *testing.T) {
	legacy := MenuPagesForCapabilities(0)
	current := MenuPagesForCapabilities(native.CapabilityMenuDirectory)
	legacyStatus, err := ResolveMenuPageIn(legacy, "status")
	if err != nil {
		t.Fatal(err)
	}
	currentStatus, err := ResolveMenuPageIn(current, "status")
	if err != nil {
		t.Fatal(err)
	}
	if legacyStatus.ID != 14 || currentStatus.ID != 0 {
		t.Fatalf("status IDs legacy=%d current=%d", legacyStatus.ID, currentStatus.ID)
	}
}

func TestLiveMenuDescriptionUsesLabelInsteadOfHistoricalID(t *testing.T) {
	page, ok := describeLiveMenuEntry(native.MenuEntry{ID: 0, Label: "STAT"})
	if !ok {
		t.Fatal("current STAT label was not described")
	}
	if page.Key != "status" {
		t.Fatalf("page 0 STAT described as %q", page.Key)
	}
	page, ok = describeLiveMenuEntry(native.MenuEntry{ID: 0, Label: "VOLT"})
	if !ok {
		t.Fatal("legacy VOLT label was not described")
	}
	if page.Key != "voltage" {
		t.Fatalf("page 0 VOLT described as %q", page.Key)
	}
}

func TestResolveMenuPageInRejectsIDFromDifferentGeneration(t *testing.T) {
	legacy := MenuPagesForCapabilities(0)
	page, err := ResolveMenuPageIn(legacy, "0")
	if err != nil {
		t.Fatal(err)
	}
	if page.Key != "voltage" {
		t.Fatalf("legacy page 0 resolved as %q", page.Key)
	}
	page, err = ResolveMenuPageIn(MenuPages(), "0")
	if err != nil {
		t.Fatal(err)
	}
	if page.Key != "status" {
		t.Fatalf("current page 0 resolved as %q", page.Key)
	}
}

func TestMenuLayoutRequiresPermutationAndAtLeastOneVisiblePage(t *testing.T) {
	pages := MenuPages()
	layout, err := DefaultMenuLayout(pages)
	if err != nil {
		t.Fatal(err)
	}
	if layout.VisibleMask != 0x7FFF || len(layout.Order) != 15 {
		t.Fatalf("default layout=%#v", layout)
	}

	moved, err := MoveMenuPage(pages, layout, 14, 1)
	if err != nil || moved.Order[1] != 14 || moved.Order[14] != 13 {
		t.Fatalf("moved layout=%#v err=%v", moved, err)
	}
	hidden, err := SetMenuPageVisible(pages, moved, 8, false)
	if err != nil || hidden.Visible(8) || !hidden.Visible(0) {
		t.Fatalf("hidden layout=%#v err=%v", hidden, err)
	}
	hidden, err = SetMenuPageVisible(pages, hidden, 0, false)
	if err != nil || hidden.Visible(0) {
		t.Fatalf("Status is a factory default, not a mandatory visible page: %#v err=%v", hidden, err)
	}
	none := hidden
	none.VisibleMask = 0
	if _, err := CanonicalMenuLayout(pages, none); err == nil {
		t.Fatal("layout with no visible pages was accepted")
	}

	duplicate := layout
	duplicate.Order = append([]byte(nil), layout.Order...)
	duplicate.Order[1] = duplicate.Order[0]
	if _, err := CanonicalMenuLayout(pages, duplicate); err == nil {
		t.Fatal("duplicate stable page ID was accepted")
	}
	extraMask := layout
	extraMask.VisibleMask |= 0x8000
	if _, err := CanonicalMenuLayout(pages, extraMask); err == nil {
		t.Fatal("out-of-catalog visibility bit was accepted")
	}
}
