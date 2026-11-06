package tui

import (
	"context"
	"image/color"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"pccontroller.local/desktop/internal/design"
)

func TestThemePaletteUsesCanonicalDesignTokens(t *testing.T) {
	tests := []struct {
		name   string
		dark   bool
		tokens design.ThemePalette
	}{
		{name: "dark", dark: true, tokens: design.Default.Colors.Dark},
		{name: "light", dark: false, tokens: design.Default.Colors.Light},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			palette := themePalette(test.dark)
			assertThemeColor(t, "background", palette.background, test.tokens.Canvas)
			assertThemeColor(
				t,
				"bright text",
				palette.brightText,
				design.Default.Colors.Dark.Canvas,
			)
			assertThemeColor(t, "surface", palette.surface, test.tokens.Surface)
			assertThemeColor(
				t,
				"surface alt",
				palette.surfaceAlt,
				test.tokens.SurfaceHigh,
			)
			assertThemeColor(t, "border", palette.border, test.tokens.Stroke)
			assertThemeColor(t, "border hot", palette.borderHot, test.tokens.Accent)
			assertThemeColor(t, "text", palette.text, test.tokens.Text)
			assertThemeColor(t, "muted", palette.muted, test.tokens.TextMuted)
			assertThemeColor(t, "faint", palette.faint, test.tokens.TextSubtle)
			assertThemeColor(t, "accent", palette.accent, test.tokens.Accent)
			assertThemeColor(
				t,
				"accent alt",
				palette.accentAlt,
				test.tokens.AccentHover,
			)
			assertThemeColor(t, "on", palette.on, test.tokens.Warm)
			assertThemeColor(t, "off", palette.off, test.tokens.SurfaceHigh)
			assertThemeColor(t, "good", palette.good, test.tokens.Success)
			assertThemeColor(t, "warning", palette.warning, test.tokens.Warm)
			assertThemeColor(t, "bad", palette.bad, test.tokens.Danger)
		})
	}
}

func TestThemeCycleReturnsToAutoAndRequestsTerminalBackground(t *testing.T) {
	tuiModel := newThemeModel(t, ThemeAuto)
	_, _ = tuiModel.Update(tea.BackgroundColorMsg{
		Color: color.RGBA{R: 250, G: 250, B: 250, A: 255},
	})
	if tuiModel.isDark {
		t.Fatal("auto theme ignored the terminal's light background")
	}

	if command := tuiModel.cycleTheme(); command != nil {
		t.Fatal("auto to dark unexpectedly requested a background color")
	}
	if tuiModel.themeMode != ThemeDark || !tuiModel.isDark {
		t.Fatalf(
			"first cycle = %q dark=%v, want dark/true",
			tuiModel.themeMode,
			tuiModel.isDark,
		)
	}

	if command := tuiModel.cycleTheme(); command != nil {
		t.Fatal("dark to light unexpectedly requested a background color")
	}
	if tuiModel.themeMode != ThemeLight || tuiModel.isDark {
		t.Fatalf(
			"second cycle = %q dark=%v, want light/false",
			tuiModel.themeMode,
			tuiModel.isDark,
		)
	}

	command := tuiModel.cycleTheme()
	if tuiModel.themeMode != ThemeAuto {
		t.Fatalf("third cycle = %q, want auto", tuiModel.themeMode)
	}
	if tuiModel.isDark != systemThemeFallbackDark() {
		t.Fatalf(
			"auto fallback dark=%v, system fallback=%v",
			tuiModel.isDark,
			systemThemeFallbackDark(),
		)
	}
	if command == nil {
		t.Fatal("returning to auto did not request the terminal background")
	}
	if got, want := reflect.TypeOf(command()), reflect.TypeOf(tea.RequestBackgroundColor()); got != want {
		t.Fatalf("auto request message = %v, want %v", got, want)
	}
}

func TestCtrlTKeyCyclesAutoDarkLightAuto(t *testing.T) {
	tuiModel := newThemeModel(t, ThemeAuto)
	themeKey := tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl})

	updated, command := tuiModel.Update(themeKey)
	tuiModel = updated.(*Model)
	if command != nil {
		t.Fatal("auto to dark unexpectedly requested a background color")
	}
	if tuiModel.themeMode != ThemeDark || !tuiModel.isDark {
		t.Fatalf(
			"first Ctrl+T = %q dark=%v, want dark/true",
			tuiModel.themeMode,
			tuiModel.isDark,
		)
	}

	updated, command = tuiModel.Update(themeKey)
	tuiModel = updated.(*Model)
	if command != nil {
		t.Fatal("dark to light unexpectedly requested a background color")
	}
	if tuiModel.themeMode != ThemeLight || tuiModel.isDark {
		t.Fatalf(
			"second Ctrl+T = %q dark=%v, want light/false",
			tuiModel.themeMode,
			tuiModel.isDark,
		)
	}

	updated, command = tuiModel.Update(themeKey)
	tuiModel = updated.(*Model)
	if tuiModel.themeMode != ThemeAuto {
		t.Fatalf("third Ctrl+T = %q, want auto", tuiModel.themeMode)
	}
	if command == nil {
		t.Fatal("third Ctrl+T did not re-request the terminal background")
	}
}

func TestExplicitThemeIgnoresTerminalBackground(t *testing.T) {
	dark := newThemeModel(t, ThemeDark)
	_, _ = dark.Update(tea.BackgroundColorMsg{
		Color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
	})
	if !dark.isDark {
		t.Fatal("explicit dark theme was replaced by terminal detection")
	}

	light := newThemeModel(t, ThemeLight)
	_, _ = light.Update(tea.BackgroundColorMsg{
		Color: color.RGBA{R: 0, G: 0, B: 0, A: 255},
	})
	if light.isDark {
		t.Fatal("explicit light theme was replaced by terminal detection")
	}

	if backgroundColorRequest(ThemeDark) != nil ||
		backgroundColorRequest(ThemeLight) != nil {
		t.Fatal("an explicit theme requested the terminal background")
	}
	if backgroundColorRequest(ThemeAuto) == nil {
		t.Fatal("auto theme omitted the terminal background request")
	}
}

func TestNoColorConventionDisablesEveryPaletteColor(t *testing.T) {
	setColorCapableTestTerminal(t)
	t.Setenv("NO_COLOR", "")
	if noColorRequested() {
		t.Fatal("empty NO_COLOR unexpectedly disabled color")
	}
	if _, disabled := newStyles(true).palette.accent.(lipgloss.NoColor); disabled {
		t.Fatal("empty NO_COLOR produced a no-color palette")
	}

	t.Setenv("NO_COLOR", "any non-empty value")
	if !noColorRequested() {
		t.Fatal("non-empty NO_COLOR did not disable color")
	}
	palette := newStyles(true).palette
	for name, candidate := range map[string]color.Color{
		"background":  palette.background,
		"bright text": palette.brightText,
		"surface":     palette.surface,
		"surface alt": palette.surfaceAlt,
		"border":      palette.border,
		"border hot":  palette.borderHot,
		"text":        palette.text,
		"muted":       palette.muted,
		"faint":       palette.faint,
		"accent":      palette.accent,
		"accent alt":  palette.accentAlt,
		"on":          palette.on,
		"off":         palette.off,
		"good":        palette.good,
		"warning":     palette.warning,
		"bad":         palette.bad,
	} {
		if _, ok := candidate.(lipgloss.NoColor); !ok {
			t.Errorf("%s remains colored as %T", name, candidate)
		}
	}
}

func TestHuhThemeUsesCanonicalSurfacesInsteadOfANSIBlackAndWhite(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := themePalette(dark)
		theme := diagnosticHuhTheme(dark, palette).Theme(dark)
		assertSameRGBA(
			t,
			"form background",
			theme.Form.Base.GetBackground(),
			palette.surface,
		)
		assertSameRGBA(
			t,
			"focused field background",
			theme.Focused.Base.GetBackground(),
			palette.surfaceAlt,
		)
		assertSameRGBA(
			t,
			"focused button background",
			theme.Focused.FocusedButton.GetBackground(),
			palette.accent,
		)
		assertSameRGBA(
			t,
			"unselected button background",
			theme.Focused.BlurredButton.GetBackground(),
			palette.surface,
		)
		assertSameRGBA(
			t,
			"blurred field background",
			theme.Blurred.Base.GetBackground(),
			palette.surface,
		)
		assertSameRGBA(
			t,
			"blurred input background",
			theme.Blurred.TextInput.Text.GetBackground(),
			palette.surface,
		)
	}
}

func TestActivityViewportOwnsEveryRenderedBackgroundCell(t *testing.T) {
	model := newThemeModel(t, ThemeDark)
	const width = 116
	_ = model.renderActivityPanel(width, 8)

	wantWidth := width - model.styles.panel.GetHorizontalFrameSize()
	lines := strings.Split(model.viewport.GetContent(), "\n")
	if len(lines) == 0 {
		t.Fatal("activity viewport has no rows")
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got != wantWidth {
			t.Fatalf(
				"activity row %d width = %d, want %d; default terminal background can leak",
				index,
				got,
				wantWidth,
			)
		}
	}
}

func TestOverlayUsesItsSurfaceAsTheDefaultCompositingBackground(t *testing.T) {
	model := newThemeModel(t, ThemeDark)
	dashboard := model.configuredView("")
	assertSameRGBA(
		t,
		"dashboard default background",
		dashboard.BackgroundColor,
		model.styles.palette.background,
	)

	model.settings.mode = settingsMenu
	overlay := model.configuredView("")
	assertSameRGBA(
		t,
		"overlay default background",
		overlay.BackgroundColor,
		model.styles.palette.surface,
	)
}

func TestKeyValueRowsOwnTheirFullRequestedWidth(t *testing.T) {
	model := newThemeModel(t, ThemeDark)
	for _, width := range []int{12, 40, 96} {
		row := model.renderKVOn(
			"Device",
			"Not reported",
			width,
			model.styles.palette.surfaceAlt,
		)
		if got := lipgloss.Width(row); got != width {
			t.Fatalf("renderKVOn width = %d, want %d", got, width)
		}
	}
}

func TestColorRefFallbackLuminance(t *testing.T) {
	for _, test := range []struct {
		name  string
		value uint32
		dark  bool
	}{
		{name: "black", value: 0x00000000, dark: true},
		{name: "white", value: 0x00ffffff, dark: false},
		{name: "dark blue COLORREF", value: 0x00402010, dark: true},
		{name: "light yellow COLORREF", value: 0x00d8f8ff, dark: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := colorRefIsDark(test.value); got != test.dark {
				t.Fatalf("colorRefIsDark(%#08x) = %v", test.value, got)
			}
		})
	}
}

func newThemeModel(t *testing.T, mode ThemeMode) *Model {
	t.Helper()
	result, err := NewModel(
		context.Background(),
		&fakeTransport{},
		ModelOptions{
			Poll:    time.Second,
			Timeout: time.Second,
			Theme:   mode,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertThemeColor(
	t *testing.T,
	name string,
	actual color.Color,
	token string,
) {
	t.Helper()
	expected := design.MustColor(token)
	red, green, blue, _ := actual.RGBA()
	if byte(red>>8) != expected.R ||
		byte(green>>8) != expected.G ||
		byte(blue>>8) != expected.B {
		t.Errorf(
			"%s = #%02X%02X%02X, want %s",
			name,
			byte(red>>8),
			byte(green>>8),
			byte(blue>>8),
			token,
		)
	}
}

func assertSameRGBA(
	t *testing.T,
	name string,
	actual color.Color,
	expected color.Color,
) {
	t.Helper()
	if actual == nil || expected == nil {
		t.Fatalf("%s has nil color: actual=%v expected=%v", name, actual, expected)
	}
	ar, ag, ab, aa := actual.RGBA()
	er, eg, eb, ea := expected.RGBA()
	if ar != er || ag != eg || ab != eb || aa != ea {
		t.Errorf(
			"%s = rgba(%d,%d,%d,%d), want rgba(%d,%d,%d,%d)",
			name,
			ar,
			ag,
			ab,
			aa,
			er,
			eg,
			eb,
			ea,
		)
	}
}
