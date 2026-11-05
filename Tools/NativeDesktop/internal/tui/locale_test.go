package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/i18n"
)

func TestPersianLocaleRendersRTLWithoutRewritingTechnicalValues(t *testing.T) {
	model, transport := newWorkspaceModel(t)
	transport.snapshot.DeviceURL = "http://127.0.0.1:47831"
	model.acceptSnapshot(transport.snapshot, "test")
	model.applyLocalePreference(config.LocalePersian)

	if !model.rtl() || model.localizer.Locale() != i18n.Persian {
		t.Fatalf("locale = %q, rtl = %v", model.localizer.Locale(), model.rtl())
	}
	rendered := ansi.Strip(model.View().Content)
	for _, expected := range []string{"چراغ", "وضعیت و کنترل"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("Persian dashboard omitted %q:\n%s", expected, rendered)
		}
	}
	if !strings.Contains(rendered, "http://127.0.0.1:47831") {
		t.Fatalf("technical URL was localized or omitted:\n%s", rendered)
	}
	if strings.Contains(rendered, "http://۱۲۷.۰.۰.۱:۴۷۸۳۱") {
		t.Fatalf("technical URL digits were shaped:\n%s", rendered)
	}
	if got := model.text("1–128 physical-independent pixels."); got != "۱ تا ۱۲۸ پیکسل مستقل از مقیاس." {
		t.Fatalf("localized prose digits = %q", got)
	}
}

func TestLocaleConfigAndAppearancePatchUseStableLocaleCodes(t *testing.T) {
	model, transport := newWorkspaceModel(t)
	cfg := transport.cfg
	cfg.UI.Locale = config.LocalePersian
	model.localeRequestID = 7
	model.acceptLocaleConfig(localeConfigMsg{requestID: 7, cfg: cfg})
	if !model.rtl() {
		t.Fatal("active configuration did not switch the TUI to RTL")
	}

	model.settings.mode = settingsEditing
	model.settings.section = settingsAppearance
	model.settings.cfg = transport.cfg
	model.settings.original = transport.cfg
	model.settings.draft = newSettingsDraft(transport.cfg)
	model.settings.draft.cfg.UI.Locale = config.LocalePersian
	patch, err := model.settingsPatch()
	if err != nil {
		t.Fatal(err)
	}
	ui, ok := patch["ui"].(map[string]any)
	if !ok {
		t.Fatalf("appearance patch = %#v", patch)
	}
	if got := ui["locale"]; got != config.LocalePersian {
		t.Fatalf("ui.locale = %#v, want stable code %q", got, config.LocalePersian)
	}
}

func TestAppearanceLanguagePreviewIsImmediateAndCancelRestoresSavedLocale(
	t *testing.T,
) {
	model, transport := newWorkspaceModel(t)
	transport.cfg.UI.Locale = config.LocaleEnglish
	model.settings.cfg = transport.cfg
	model.settings.original = transport.cfg
	model.settings.mode = settingsEditing
	model.settings.section = settingsAppearance
	model.settings.draft = newSettingsDraft(transport.cfg)
	model.settings.form = model.newSettingsForm(model.appearanceSettingsGroups()...)
	model.settings.draft.cfg.UI.Locale = config.LocalePersian

	_ = model.updateSettingsForm(tea.WindowSizeMsg{Width: 110, Height: 38})
	if !model.rtl() {
		t.Fatal("language selection did not preview immediately")
	}
	_ = model.openSettingsMenu()
	if model.rtl() || model.localizer.Locale() != i18n.English {
		t.Fatalf(
			"cancel did not restore saved locale: locale=%q rtl=%v",
			model.localizer.Locale(),
			model.rtl(),
		)
	}
}
