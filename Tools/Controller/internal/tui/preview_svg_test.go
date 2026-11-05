package tui

import (
	"strings"
	"testing"
)

func TestPreviewSVGRetainsTerminalColorsAndEscapesText(t *testing.T) {
	svg := PreviewSVG("\x1b[38;2;167;139;250m<PCController>\x1b[0m\n\x1b[48;5;196m✓\x1b[0m")
	for _, want := range []string{"#a78bfa", "&lt;PCController&gt;", "#ff0000", "✓", "</svg>"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(svg, "\x1b") {
		t.Fatal("ANSI escape leaked into SVG")
	}
}

func TestPreviewPageNamesUseSharedRouting(t *testing.T) {
	for _, name := range []string{"outputs", "controls", "Control", "2", "workbench"} {
		page, err := ParsePage(name)
		if err != nil || page != PageOutputs {
			t.Fatalf("%q = %v, %v", name, page, err)
		}
	}
	if _, err := ParsePage("not-a-page"); err == nil {
		t.Fatal("unknown page accepted")
	}
}
