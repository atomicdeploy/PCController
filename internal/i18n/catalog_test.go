package i18n

import (
	"maps"
	"testing"
)

func TestCatalogsHaveIdenticalNonEmptyKeys(t *testing.T) {
	if !maps.EqualFunc(
		englishCatalog,
		persianCatalog,
		func(english, persian string) bool {
			return english != "" && persian != ""
		},
	) {
		for id, english := range englishCatalog {
			if english == "" {
				t.Errorf("English message %q is empty", id)
			}
			if persianCatalog[id] == "" {
				t.Errorf("Persian catalog is missing %q", id)
			}
		}
		for id, persian := range persianCatalog {
			if persian == "" {
				t.Errorf("Persian message %q is empty", id)
			}
			if englishCatalog[id] == "" {
				t.Errorf("English catalog is missing %q", id)
			}
		}
	}
}

func TestLocalizerTranslationFallbackAndCatalogCopy(t *testing.T) {
	english := NewForSystem(Auto, "en-US")
	persian := NewForSystem(Auto, "fa-IR")
	if got := english.Text(MsgActionTurnOn); got != "Turn on" {
		t.Fatalf("English action = %q", got)
	}
	if got := persian.Text(MsgActionTurnOn); got != "روشن کردن" {
		t.Fatalf("Persian action = %q", got)
	}
	if got := persian.Choose("Page 2", "صفحهٔ 2"); got != "صفحهٔ ۲" {
		t.Fatalf("Persian Choose() = %q", got)
	}
	const unknown MessageID = "test.unknown"
	if got := persian.Text(unknown); got != string(unknown) {
		t.Fatalf("Unknown message = %q", got)
	}

	copy := Messages(Persian)
	copy[MsgActionTurnOn] = "changed"
	if got := New(Persian).Text(MsgActionTurnOn); got == "changed" {
		t.Fatal("Messages exposed the mutable catalog")
	}
}

func TestDigitShapingAndLocalizedFormatting(t *testing.T) {
	const mixed = "ASCII 0123456789 / Arabic ٠١٢٣٤٥٦٧٨٩"
	if got, want := ShapePersianDigits(mixed),
		"ASCII ۰۱۲۳۴۵۶۷۸۹ / Arabic ۰۱۲۳۴۵۶۷۸۹"; got != want {
		t.Fatalf("ShapePersianDigits() = %q, want %q", got, want)
	}
	if got, want := ShapeLatinDigits("۰۱۲۳۴۵۶۷۸۹ ٠١٢٣٤٥٦٧٨٩"),
		"0123456789 0123456789"; got != want {
		t.Fatalf("ShapeLatinDigits() = %q, want %q", got, want)
	}

	persian := New(Persian)
	if got, want := persian.Format(MsgNoticeRequestFailed, "کد 404"),
		"درخواست ناموفق بود: کد ۴۰۴"; got != want {
		t.Fatalf("Persian Format() = %q, want %q", got, want)
	}
	english := New(English)
	if got, want := english.ShapeDigits("کد ۴۰۴"), "کد 404"; got != want {
		t.Fatalf("English ShapeDigits() = %q, want %q", got, want)
	}
}
