//go:build windows

package dialogs

import (
	"reflect"
	"testing"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/i18n"
)

func TestDialogPersianTranslationRoundTrip(t *testing.T) {
	t.Parallel()
	persian := i18n.New(i18n.Persian)
	english := i18n.New(i18n.English)
	for _, source := range []string{
		"Settings — Rayan Lamp",
		"Lamp address:",
		"Enable desktop notifications",
		"Keyboard shortcuts",
		"Windows registration",
		"Rayan Lamp Diagnostics",
	} {
		translated := dialogText(persian, source)
		if translated == source {
			t.Errorf("%q was not translated", source)
			continue
		}
		if got := dialogText(english, translated); got != source {
			t.Errorf("round trip %q => %q => %q", source, translated, got)
		}
	}
}

func TestDialogLocaleChoicesKeepStableValueOrder(t *testing.T) {
	t.Parallel()
	got := dialogLabels(i18n.New(i18n.Persian), localeLabels)
	want := []string{"پیش‌فرض سیستم", "انگلیسی", "فارسی"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Persian locale labels = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(localeValues, []string{
		config.LocaleAuto,
		config.LocaleEnglish,
		config.LocalePersian,
	}) {
		t.Fatalf("locale values changed order: %#v", localeValues)
	}
}

func TestDialogTranslationDoesNotShapeOpaqueValues(t *testing.T) {
	t.Parallel()
	localizer := i18n.New(i18n.Persian)
	for _, value := range []string{
		"http://127.0.0.1:47831",
		"Ctrl+Alt+L",
		"C:\\RayanLamp\\script2.ps1",
		"/config/st",
	} {
		if got := dialogText(localizer, value); got != value {
			t.Errorf("opaque value %q changed to %q", value, got)
		}
	}
}
