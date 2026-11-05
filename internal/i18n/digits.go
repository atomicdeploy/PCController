package i18n

import "strings"

var toPersianDigits = strings.NewReplacer(
	"0", "۰",
	"1", "۱",
	"2", "۲",
	"3", "۳",
	"4", "۴",
	"5", "۵",
	"6", "۶",
	"7", "۷",
	"8", "۸",
	"9", "۹",
	"٠", "۰",
	"١", "۱",
	"٢", "۲",
	"٣", "۳",
	"٤", "۴",
	"٥", "۵",
	"٦", "۶",
	"٧", "۷",
	"٨", "۸",
	"٩", "۹",
)

var toLatinDigits = strings.NewReplacer(
	"۰", "0",
	"۱", "1",
	"۲", "2",
	"۳", "3",
	"۴", "4",
	"۵", "5",
	"۶", "6",
	"۷", "7",
	"۸", "8",
	"۹", "9",
	"٠", "0",
	"١", "1",
	"٢", "2",
	"٣", "3",
	"٤", "4",
	"٥", "5",
	"٦", "6",
	"٧", "7",
	"٨", "8",
	"٩", "9",
)

// ShapePersianDigits converts ASCII and Arabic-Indic decimal digits to the
// Persian digit glyphs U+06F0 through U+06F9.
func ShapePersianDigits(value string) string {
	return toPersianDigits.Replace(value)
}

// ShapeLatinDigits converts Persian and Arabic-Indic decimal digits to ASCII.
func ShapeLatinDigits(value string) string {
	return toLatinDigits.Replace(value)
}
