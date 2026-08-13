package control

import (
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestMenuCapabilityFallbackKeepsCanonicalCatalog(t *testing.T) {
	fallback := MenuPagesForCapabilities(0)
	current := MenuPagesForCapabilities(native.CapabilityMenuDirectory)
	fallbackDoor, err := ResolveMenuPageIn(fallback, "door")
	if err != nil {
		t.Fatal(err)
	}
	currentDoor, err := ResolveMenuPageIn(current, "door")
	if err != nil {
		t.Fatal(err)
	}
	if fallbackDoor.ID != 0 || currentDoor.ID != 0 {
		t.Fatalf("door IDs fallback=%d current=%d", fallbackDoor.ID, currentDoor.ID)
	}
	if _, err := ResolveMenuPageIn(current, "status"); err == nil {
		t.Fatal("retired status page alias was accepted by the current-only catalog")
	}
}

func TestMenuFallbackUsesVerifiedVoltageFirstBuildIdentity(t *testing.T) {
	pages := menuPagesForHello(native.Hello{
		IdentitySchema: native.IdentitySchemaCompact,
		BuildHash:      voltageFirstMenuBuildHash,
	})
	voltage, err := ResolveMenuPageIn(pages, "voltage")
	if err != nil {
		t.Fatal(err)
	}
	status, err := ResolveMenuPageIn(pages, "status")
	if err != nil {
		t.Fatal(err)
	}
	if voltage.ID != 0 || status.ID != 14 || len(pages) != 15 {
		t.Fatalf("voltage-first compatibility catalog voltage=%d status=%d pages=%d", voltage.ID, status.ID, len(pages))
	}
}

func TestMenuFallbackKeepsUnknownBuildOnCurrentCatalog(t *testing.T) {
	pages := menuPagesForHello(native.Hello{
		IdentitySchema: native.IdentitySchemaCompact,
		BuildHash:      0x01020304,
	})
	door, err := ResolveMenuPageIn(pages, "door")
	if err != nil {
		t.Fatal(err)
	}
	if door.ID != 0 || len(pages) != 10 {
		t.Fatalf("unknown-build fallback door=%d pages=%d", door.ID, len(pages))
	}
	if _, err := ResolveMenuPageIn(pages, "status"); err == nil {
		t.Fatal("unknown build inherited the historical Status page")
	}
}

func TestMenuDirectoryCapabilityOverridesHistoricalBuildIdentity(t *testing.T) {
	pages := menuPagesForHello(native.Hello{
		IdentitySchema: native.IdentitySchemaCompact,
		BuildHash:      voltageFirstMenuBuildHash,
		Capabilities:   native.CapabilityMenuDirectory,
	})
	door, err := ResolveMenuPageIn(pages, "door")
	if err != nil {
		t.Fatal(err)
	}
	if door.ID != 0 || len(pages) != 10 {
		t.Fatalf("advertised current catalog door=%d pages=%d", door.ID, len(pages))
	}
	if _, err := ResolveMenuPageIn(pages, "status"); err == nil {
		t.Fatal("advertised current catalog was replaced by historical IDs")
	}
}

func TestLiveMenuDescriptionUsesCurrentLabelInsteadOfNumericID(t *testing.T) {
	page, ok := describeLiveMenuEntry(native.MenuEntry{ID: 0, Label: "door"})
	if !ok {
		t.Fatal("current door label was not described")
	}
	if page.Key != "door" {
		t.Fatalf("page 0 door described as %q", page.Key)
	}
	if _, ok := describeLiveMenuEntry(native.MenuEntry{ID: 0, Label: "STAT"}); ok {
		t.Fatal("retired STAT label was described by the current-only catalog")
	}
}

func TestResolveMenuPageInUsesCurrentStableID(t *testing.T) {
	fallback := MenuPagesForCapabilities(0)
	page, err := ResolveMenuPageIn(fallback, "0")
	if err != nil {
		t.Fatal(err)
	}
	if page.Key != "door" {
		t.Fatalf("fallback page 0 resolved as %q", page.Key)
	}
	page, err = ResolveMenuPageIn(MenuPages(), "0")
	if err != nil {
		t.Fatal(err)
	}
	if page.Key != "door" {
		t.Fatalf("current page 0 resolved as %q", page.Key)
	}
}

func TestCurrentCatalogRemovesStandaloneBluetoothPage(t *testing.T) {
	current := MenuPages()
	page, err := ResolveMenuPageIn(current, "6")
	if err != nil {
		t.Fatal(err)
	}
	if page.Key != "settings" || len(current) != 10 {
		t.Fatalf("dense current page 6=%#v catalog=%#v", page, current)
	}
	for _, candidate := range current {
		if candidate.Key == "bt-audio" {
			t.Fatalf("current catalog still contains standalone BT page %#v", candidate)
		}
	}
}

func TestMenuLayoutRequiresPermutationAndAtLeastOneVisiblePage(t *testing.T) {
	pages := MenuPages()
	layout, err := DefaultMenuLayout(pages)
	if err != nil {
		t.Fatal(err)
	}
	if layout.VisibleMask != 0x237F || len(layout.Order) != 10 {
		t.Fatalf("default layout=%#v", layout)
	}

	moved, err := MoveMenuPage(pages, layout, 13, 1)
	if err != nil || moved.Order[1] != 13 || moved.Order[9] != 9 {
		t.Fatalf("moved layout=%#v err=%v", moved, err)
	}
	hidden, err := SetMenuPageVisible(pages, moved, 8, false)
	if err != nil || hidden.Visible(8) || !hidden.Visible(0) {
		t.Fatalf("hidden layout=%#v err=%v", hidden, err)
	}
	hidden, err = SetMenuPageVisible(pages, hidden, 0, false)
	if err != nil || hidden.Visible(0) {
		t.Fatalf("Door is a factory default, not a mandatory visible page: %#v err=%v", hidden, err)
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
