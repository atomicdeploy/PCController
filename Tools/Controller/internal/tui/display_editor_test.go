package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func TestDisplayComposerUsesCanonicalCommandWithExactArbitraryText(t *testing.T) {
	editor := &displayEditor{
		Text: "A  quoted \"message\"", Targets: []string{"segments", "lcd", "both"}, Target: 2,
		SpeedMS: 180, DurationMS: 4200, Repeat: control.DisplayRepeatInterval,
		IntervalMS: 60000, ForceScroll: true,
	}
	line := displayEditorCommand(editor)
	words, err := shell.Split(line)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"display", "both", "--speed-ms", "180", "--duration-ms", "4200",
		"--repeat", "interval", "--interval-ms", "60000", "--scroll", "--", "A  quoted \"message\"",
	}
	if !reflect.DeepEqual(words, want) {
		t.Fatalf("display command words=%#v, want %#v", words, want)
	}
}

func TestDisplayComposerIsCapabilityAndAvailabilityGated(t *testing.T) {
	snapshot := RichPreviewSnapshot()
	model := readyModel(t, PageMenus)
	model.preview = &snapshot
	if got := model.displayTargetsFor(snapshot); !reflect.DeepEqual(got, []string{"segments", "lcd", "both"}) {
		t.Fatalf("available targets=%v", got)
	}
	snapshot.Status.LCDAddress = 0
	snapshot.FrontPanel.LCDAddress = 0
	snapshot.FrontPanel.LCDAvailable = false
	if got := model.displayTargetsFor(snapshot); !reflect.DeepEqual(got, []string{"segments"}) {
		t.Fatalf("missing LCD still exposed targets=%v", got)
	}
	snapshot.Hello.Capabilities &^= native.CapabilityScheduledSegments
	if got := model.displayTargetsFor(snapshot); len(got) != 0 {
		t.Fatalf("unadvertised displays exposed targets=%v", got)
	}

	updated, command, handled := model.beginDisplayEditor()
	if !handled || command != nil || updated.displayEditor != nil {
		t.Fatalf("unavailable display opened editor: handled=%t command=%v editor=%#v", handled, command, updated.displayEditor)
	}
}

func TestMenusPageRendersOnlyAdvertisedFetchedPanelAndDisplayControls(t *testing.T) {
	render := func(snapshot control.Snapshot) (string, menuPageGeometry) {
		model := readyModel(t, PageMenus)
		model.preview = &snapshot
		model.hostMenus = nil
		model.frontPanelKey = func(int, string) error { return nil }
		model.mirrorLCD = func(string, string) error { return nil }
		lines, geometry := model.menuPagePrefix(snapshot)
		return ansi.Strip(strings.Join(lines, "\n")), geometry
	}
	assertAbsent := func(t *testing.T, rendered string, values ...string) {
		t.Helper()
		for _, value := range values {
			if strings.Contains(rendered, value) {
				t.Fatalf("unavailable %q rendered:\n%s", value, rendered)
			}
		}
	}

	unknown := control.Snapshot{Connected: true}
	rendered, geometry := render(unknown)
	assertAbsent(t, rendered,
		"4-DIGIT DISPLAY", "K1 · previous", "D · Send arbitrary message",
		"LCD prompt mirroring", "active 0 · Door", "loading advertised front-panel state",
	)
	if geometry.frontPanelStart != geometry.frontPanelEnd {
		t.Fatalf("unavailable front-panel retained hit target: %#v", geometry)
	}
	hostModel := readyModel(t, PageMenus)
	hostModel.preview = &unknown
	lines, _ := hostModel.menuPagePrefix(unknown)
	hostRendered := ansi.Strip(strings.Join(lines, "\n"))
	assertAbsent(t, hostRendered, "K1/K2 navigate", "K3/K4 adjust")
	if !strings.Contains(hostRendered, "waiting for exact panel and RemoteKeys readback") {
		t.Fatalf("active host menu did not explain unavailable key controls:\n%s", hostRendered)
	}

	pending := unknown
	pending.Hello.Capabilities = native.CapabilityFrontPanelSnapshot | native.CapabilityRemoteKeys
	rendered, geometry = render(pending)
	if !strings.Contains(rendered, "loading advertised front-panel state") {
		t.Fatalf("advertised unfetched panel did not render loading state:\n%s", rendered)
	}
	assertAbsent(t, rendered, "4-DIGIT DISPLAY", "K1 · previous")
	if geometry.frontPanelStart != geometry.frontPanelEnd {
		t.Fatalf("unfetched front-panel retained hit target: %#v", geometry)
	}

	fetched := pending
	fetched.Hello.Capabilities &^= native.CapabilityRemoteKeys
	fetched.Hello.Capabilities |= native.CapabilityLCD
	fetched.HaveFrontPanel = true
	fetched.FrontPanel = native.FrontPanel{
		Schema: 2, RawSegments: [4]byte{segA, segB, segC, segD},
		Brightness: 4, LCDAddress: 0x27, LCDAvailable: true,
		LCDLine1: "Fetched", LCDLine2: "Panel", LCDBacklight: true, MenuPage: 3,
	}
	rendered, geometry = render(fetched)
	for _, expected := range []string{"4-DIGIT DISPLAY", "Fetched", "active 3"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("fetched panel missing %q:\n%s", expected, rendered)
		}
	}
	assertAbsent(t, rendered, "K1 · previous")
	if geometry.frontPanelStart != geometry.frontPanelEnd {
		t.Fatalf("panel without remote keys retained hit target: %#v", geometry)
	}

	fetched.Hello.Capabilities |= native.CapabilityRemoteKeys
	rendered, geometry = render(fetched)
	if !strings.Contains(rendered, "K1 · previous") || geometry.frontPanelEnd <= geometry.frontPanelStart {
		t.Fatalf("advertised exact remote-key panel missing controls: geometry=%#v\n%s", geometry, rendered)
	}
	panelWithoutLCD := fetched
	panelWithoutLCD.FrontPanel.LCDAvailable = false
	panelWithoutLCD.FrontPanel.LCDAddress = 0
	panelWithoutLCD.HaveStatus = true
	panelWithoutLCD.Status.LCDAddress = 0x27
	rendered, _ = render(panelWithoutLCD)
	assertAbsent(t, rendered, "2×16 LCD", "Fetched")

	segments := unknown
	segments.Hello.Capabilities = native.CapabilityScheduledSegments
	rendered, _ = render(segments)
	if !strings.Contains(rendered, "D · Send arbitrary message") {
		t.Fatalf("advertised scheduled-segment action missing:\n%s", rendered)
	}
	assertAbsent(t, rendered, "LCD prompt mirroring")

	lcd := unknown
	lcd.Hello.Capabilities = native.CapabilityLCD
	rendered, _ = render(lcd)
	assertAbsent(t, rendered, "D · Send arbitrary message", "LCD prompt mirroring")
	lcd.HaveStatus = true
	lcd.Status.LCDAddress = 0x27
	rendered, _ = render(lcd)
	for _, expected := range []string{"D · Send arbitrary message", "LCD prompt mirroring"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("fetched LCD missing %q:\n%s", expected, rendered)
		}
	}

	remoteLCD := control.Snapshot{
		Connected: true,
		Hello:     native.Hello{Capabilities: native.CapabilityLCD | native.CapabilityI2CTransfer},
	}
	remoteModel := readyModel(t, PageMenus)
	remoteModel.preview = nil
	remoteModel.hostMenus = nil
	remoteModel.remote = &RemoteBackend{}
	remoteModel.remoteSnapshot = remoteLCD
	remoteModel.mirrorLCD = func(string, string) error { return nil }
	remoteModel.haveLCDPresentation = true
	remoteModel.lcdPresentation = control.LCDPresentationState{
		Enabled: true, Physical: true, Address: 0x3F,
		PhysicalLine1: "Remote", PhysicalLine2: "readback",
	}
	lines, _ = remoteModel.menuPagePrefix(remoteLCD)
	rendered = ansi.Strip(strings.Join(lines, "\n"))
	for _, expected := range []string{"D · Send arbitrary message", "LCD prompt mirroring"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("typed remote LCD readback missing %q:\n%s", expected, rendered)
		}
	}

	disconnected := RichPreviewSnapshot()
	disconnected.Connected = false
	rendered, geometry = render(disconnected)
	assertAbsent(t, rendered,
		"4-DIGIT DISPLAY", "K1 · previous", "D · Send arbitrary message", "LCD prompt mirroring",
	)
	if geometry.frontPanelStart != geometry.frontPanelEnd {
		t.Fatalf("disconnected front-panel retained hit target: %#v", geometry)
	}
}

func TestDisplayComposerEditsParametersAndAutoScrollsOverflow(t *testing.T) {
	model := readyModel(t, PageMenus)
	updated, _, handled := model.beginDisplayEditor()
	model = updated
	if !handled || model.displayEditor == nil {
		t.Fatal("advertised display did not open composer")
	}
	for _, character := range "HELLO BOARD" {
		updatedModel, _, _ := model.handleDisplayEditorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		model = updatedModel
	}
	rendered := ansi.Strip(renderDisplayEditor(model.displayEditor, 100))
	for _, expected := range []string{"HELLO BOARD", "Marquee · automatic", "segments"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("composer missing %q:\n%s", expected, rendered)
		}
	}

	model.displayEditor.Cursor = 4
	model.displayEditor.Repeat = control.DisplayRepeatOnce
	model.adjustDisplayEditor(1)
	if model.displayEditor.Repeat != control.DisplayRepeatLoop {
		t.Fatalf("repeat adjustment=%q", model.displayEditor.Repeat)
	}
	model.adjustDisplayEditor(1)
	if model.displayEditor.Repeat != control.DisplayRepeatInterval {
		t.Fatalf("repeat adjustment=%q", model.displayEditor.Repeat)
	}
}
