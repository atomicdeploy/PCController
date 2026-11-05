//go:build windows

package ui

import (
	"testing"

	"github.com/lxn/walk"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/winfx"
)

func TestShellLocaleSwitchesDirectionAndMainFontFamily(t *testing.T) {
	shell := &Shell{
		cfg: config.Config{
			UI: config.UIConfig{Locale: config.LocaleEnglish},
		},
	}
	shell.initializeLocalization()
	if !shell.persianFontAvailable {
		t.Fatal("embedded Yekan Bakh VF was not registered")
	}
	if err := shell.replaceMainFonts(shell.preferredFontFamily()); err != nil {
		t.Fatalf("create initial main fonts: %v", err)
	}
	defer func() {
		mainFontSet{
			title:   shell.titleFont,
			state:   shell.stateFont,
			body:    shell.bodyFont,
			button:  shell.buttonFont,
			caption: shell.captionFont,
		}.dispose()
		shell.disposeSurfaceFonts()
	}()

	shell.applyLocaleUI(config.LocalePersian)
	if !shell.isRTL() ||
		shell.localizerSnapshot().Locale() != i18n.Persian {
		t.Fatal("Persian preference did not activate RTL localization")
	}
	if family := shell.preferredFontFamily(); family != winfx.PersianFontFamily(false) {
		t.Fatalf("Persian font family = %q", family)
	}
	for name, font := range map[string]string{
		"title":   shell.titleFont.Family(),
		"state":   shell.stateFont.Family(),
		"body":    shell.bodyFont.Family(),
		"button":  shell.buttonFont.Family(),
		"caption": shell.captionFont.Family(),
	} {
		bold := name == "title" || name == "state" || name == "button"
		if want := winfx.PersianFontFamily(bold); font != want {
			t.Errorf("%s family = %q, want %q", name, font, want)
		}
	}
	for _, font := range []*walk.Font{shell.titleFont, shell.stateFont, shell.bodyFont, shell.buttonFont, shell.captionFont} {
		for _, dpi := range []int{96, 120, 144, 192} {
			handle, err := shell.surfaceFont(font, dpi)
			if err != nil {
				t.Fatal(err)
			}
			face, err := winfx.FontFaceName(uintptr(handle))
			if err != nil || face != font.Family() {
				t.Fatalf("%d DPI font selected %q, want %q: %v", dpi, face, font.Family(), err)
			}
		}
	}

	shell.applyLocaleUI(config.LocaleEnglish)
	if shell.isRTL() ||
		shell.localizerSnapshot().Locale() != i18n.English {
		t.Fatal("English preference did not restore LTR localization")
	}
	if family := shell.preferredFontFamily(); family != defaultMainFontFamily {
		t.Fatalf("English font family = %q, want %q", family, defaultMainFontFamily)
	}
}
