package terminal

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNoColorAndTerminalPolicyPrecedence(t *testing.T) {
	clearPolicyEnvironment(t)
	var output bytes.Buffer

	if ColorEnabled(&output) {
		t.Fatal("redirected output unexpectedly enabled color")
	}

	t.Setenv("FORCE_COLOR", "1")
	if !ColorEnabled(&output) {
		t.Fatal("FORCE_COLOR did not enable redirected color")
	}

	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(&output) {
		t.Fatal("NO_COLOR did not override FORCE_COLOR")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if ColorEnabled(&output) {
		t.Fatal("TERM=dumb did not disable forced color")
	}

	t.Setenv("TERM", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	if !ColorEnabled(&output) {
		t.Fatal("empty FORCE_COLOR incorrectly masked CLICOLOR_FORCE")
	}
}

func TestStyledUsesCompleteSGRAndReset(t *testing.T) {
	clearPolicyEnvironment(t)
	t.Setenv("FORCE_COLOR", "1")
	var output bytes.Buffer

	got := Styled(&output, BoldGreen, "OK")
	want := BoldGreen + "OK" + Reset
	if got != want {
		t.Fatalf("Styled() = %q, want %q", got, want)
	}
	if strings.Count(got, "\x1b[") != 2 {
		t.Fatalf("Styled() emitted incomplete or extra escapes: %q", got)
	}

	t.Setenv("NO_COLOR", "yes")
	if got := Styled(&output, BoldGreen, "OK"); got != "OK" {
		t.Fatalf("NO_COLOR Styled() = %q, want plain text", got)
	}
	if got := StyledIf(&output, false, BoldGreen, "OK"); got != "OK" {
		t.Fatalf("caller-disabled StyledIf() = %q, want plain text", got)
	}
}

func TestEmojiHasASCIIFallbackAndExplicitOverride(t *testing.T) {
	clearPolicyEnvironment(t)
	var output bytes.Buffer

	if got := Symbol(&output, "✅", "[OK]"); got != "[OK]" {
		t.Fatalf("redirected Symbol() = %q, want ASCII fallback", got)
	}

	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "always")
	if got := Symbol(&output, "✅", "[OK]"); got != "✅" {
		t.Fatalf("forced Symbol() = %q, want emoji", got)
	}

	t.Setenv("NO_EMOJI", "1")
	if got := Symbol(&output, "✅", "[OK]"); got != "[OK]" {
		t.Fatalf("NO_EMOJI Symbol() = %q, want ASCII fallback", got)
	}

	t.Setenv("NO_EMOJI", "off")
	if got := Symbol(&output, "✅", "[OK]"); got != "✅" {
		t.Fatalf("NO_EMOJI=off Symbol() = %q, want forced emoji", got)
	}
}

func TestUnknownWritersAreNotTerminals(t *testing.T) {
	clearPolicyEnvironment(t)
	var output bytes.Buffer
	capabilities := Detect(&output)
	if capabilities.Terminal || capabilities.Color || capabilities.Emoji {
		t.Fatalf("buffer capabilities = %+v, want plain redirected output", capabilities)
	}
}

func TestEnableVT100IgnoresNonFileWriters(t *testing.T) {
	var output bytes.Buffer
	enabled, err := EnableVT100(&output)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("non-file writer unexpectedly enabled VT output")
	}
}

func clearPolicyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NO_COLOR",
		"NO_EMOJI",
		"TERM",
		"CLICOLOR",
		"CLICOLOR_FORCE",
		"FORCE_COLOR",
		"PCCONTROLLER_DESKTOP_FORCE_COLOR",
		"PCCONTROLLER_DESKTOP_EMOJI",
	} {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}
