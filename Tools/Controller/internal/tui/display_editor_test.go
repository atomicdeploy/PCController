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
	if got := displayTargetsFor(snapshot); !reflect.DeepEqual(got, []string{"segments", "lcd", "both"}) {
		t.Fatalf("available targets=%v", got)
	}
	snapshot.Status.LCDAddress = 0
	if got := displayTargetsFor(snapshot); !reflect.DeepEqual(got, []string{"segments"}) {
		t.Fatalf("missing LCD still exposed targets=%v", got)
	}
	snapshot.Hello.Capabilities &^= native.CapabilityScheduledSegments
	if got := displayTargetsFor(snapshot); len(got) != 0 {
		t.Fatalf("unadvertised displays exposed targets=%v", got)
	}

	model := readyModel(t, PageMenus)
	model.preview = &snapshot
	updated, command, handled := model.beginDisplayEditor()
	if !handled || command != nil || updated.displayEditor != nil {
		t.Fatalf("unavailable display opened editor: handled=%t command=%v editor=%#v", handled, command, updated.displayEditor)
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
