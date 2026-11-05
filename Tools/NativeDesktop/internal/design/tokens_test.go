package design

import (
	"encoding/json"
	"testing"
)

func TestCanonicalTokensLoad(t *testing.T) {
	t.Parallel()
	tokens, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.Colors.Accent.Cyan != AccentCyan ||
		tokens.Colors.Accent.Warm != AccentWarm {
		t.Fatalf("accent palette = %+v", tokens.Colors.Accent)
	}
	if tokens.Colors.Dark.Accent != DarkAccent ||
		tokens.Colors.Dark.AccentHover != DarkAccentHover ||
		tokens.Colors.Dark.Warm != DarkWarm ||
		tokens.Colors.Dark.Success != DarkSuccess ||
		tokens.Colors.Dark.Danger != DarkDanger ||
		tokens.Colors.Light.Accent != LightAccent ||
		tokens.Colors.Light.AccentHover != LightAccentHover ||
		tokens.Colors.Light.Warm != LightWarm ||
		tokens.Colors.Light.Success != LightSuccess ||
		tokens.Colors.Light.Danger != LightDanger {
		t.Fatalf("theme palettes: dark=%+v light=%+v",
			tokens.Colors.Dark, tokens.Colors.Light)
	}
	if tokens.Radii.CardPX <= tokens.Radii.ControlPX {
		t.Fatalf("radii = %+v, card should exceed control", tokens.Radii)
	}
	var raw map[string]any
	if err := json.Unmarshal(JSON, &raw); err != nil {
		t.Fatalf("embedded JSON: %v", err)
	}
}
