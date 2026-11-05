//go:build windows

package winfx

import (
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	dialogButtonIconDefaultCharset   = 1
	dialogButtonIconClearTypeQuality = 5
	dialogButtonIconMarkMissing      = 1
)

var (
	dialogButtonIconGDI32           = windows.NewLazySystemDLL("gdi32.dll")
	dialogButtonIconCreateFont      = dialogButtonIconGDI32.NewProc("CreateFontW")
	dialogButtonIconGetGlyphIndices = dialogButtonIconGDI32.NewProc("GetGlyphIndicesW")
	dialogButtonIconGetTextFace     = dialogButtonIconGDI32.NewProc("GetTextFaceW")
)

// dialogButtonIcon is a Segoe Fluent Icons / Segoe MDL2 Assets glyph. These
// two system fonts share the code points used here on supported Windows 10 and
// 11 versions. The renderer falls back to text-only when neither font exists.
type dialogButtonIcon uint16

const (
	dialogButtonIconNone         dialogButtonIcon = 0
	dialogButtonIconSave         dialogButtonIcon = 0xE74E
	dialogButtonIconCancel       dialogButtonIcon = 0xE711
	dialogButtonIconBrowse       dialogButtonIcon = 0xE8E5
	dialogButtonIconNotification dialogButtonIcon = 0xEA8F
	dialogButtonIconSound        dialogButtonIcon = 0xE767
	dialogButtonIconCopy         dialogButtonIcon = 0xE8C8
	dialogButtonIconRefresh      dialogButtonIcon = 0xE72C
	dialogButtonIconRecord       dialogButtonIcon = 0xE7C8
	dialogButtonIconClear        dialogButtonIcon = 0xE74D
	dialogButtonIconReveal       dialogButtonIcon = 0xE890
)

var dialogButtonSemanticGlyphs = [...]dialogButtonIcon{
	dialogButtonIconSave,
	dialogButtonIconCancel,
	dialogButtonIconBrowse,
	dialogButtonIconNotification,
	dialogButtonIconSound,
	dialogButtonIconCopy,
	dialogButtonIconRefresh,
	dialogButtonIconRecord,
	dialogButtonIconClear,
	dialogButtonIconReveal,
}

type dialogButtonContent struct {
	Text string
	Icon dialogButtonIcon
}

// dialogButtonContentFor maps the current native dialog labels to semantic
// icons. It strips only the legacy decorative Record/Clear prefixes from the
// painted label; the underlying HWND text remains untouched for UI Automation
// and native button behavior. Ampersand mnemonic markers remain in Text for
// DrawText, but are removed from the semantic lookup key.
func dialogButtonContentFor(raw string) dialogButtonContent {
	text := strings.TrimSpace(raw)
	text = trimDialogButtonLegacyPrefix(text)
	key := normalizeDialogButtonSemanticText(text)

	icon := dialogButtonIconNone
	switch {
	case key == "save", strings.HasPrefix(key, "save "),
		strings.HasPrefix(key, "saving"),
		key == "ذخیره", strings.HasPrefix(key, "ذخیره "),
		strings.HasPrefix(key, "در حال ذخیره"):
		icon = dialogButtonIconSave
	case key == "cancel", strings.HasPrefix(key, "cancel "), key == "close",
		key == "انصراف", key == "بستن":
		icon = dialogButtonIconCancel
	case strings.HasPrefix(key, "browse"), strings.HasPrefix(key, "انتخاب"):
		icon = dialogButtonIconBrowse
	case key == "test toast", strings.HasPrefix(key, "test notification"),
		strings.HasPrefix(key, "آزمایش اعلان"):
		icon = dialogButtonIconNotification
	case key == "test sound", strings.HasPrefix(key, "test audio"),
		strings.HasPrefix(key, "آزمایش صدا"):
		icon = dialogButtonIconSound
	case key == "copy", strings.HasPrefix(key, "copy "),
		key == "رونوشت", strings.HasPrefix(key, "رونوشت "):
		icon = dialogButtonIconCopy
	case strings.HasPrefix(key, "regenerate"),
		key == "refresh", strings.HasPrefix(key, "refresh "),
		strings.HasPrefix(key, "ساخت دوباره"),
		key == "تازه‌سازی", strings.HasPrefix(key, "تازه‌سازی "):
		icon = dialogButtonIconRefresh
	case key == "record", strings.HasPrefix(key, "record "),
		key == "ضبط", strings.HasPrefix(key, "ضبط "):
		icon = dialogButtonIconRecord
	case key == "clear", strings.HasPrefix(key, "clear "),
		key == "پاک‌کردن", strings.HasPrefix(key, "پاک‌کردن "):
		icon = dialogButtonIconClear
	case key == "reveal", strings.HasPrefix(key, "reveal "),
		key == "نمایش", strings.HasPrefix(key, "نمایش "):
		icon = dialogButtonIconReveal
	}
	return dialogButtonContent{Text: text, Icon: icon}
}

func trimDialogButtonLegacyPrefix(text string) string {
	for _, prefix := range []string{"●", "■", "×"} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return text
}

func normalizeDialogButtonSemanticText(text string) string {
	text = stripDialogButtonMnemonicMarkers(text)
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.TrimSpace(strings.TrimSuffix(text, "…"))
	text = strings.TrimSpace(strings.TrimSuffix(text, "..."))
	return strings.Join(strings.Fields(text), " ")
}

// stripDialogButtonMnemonicMarkers follows DrawText's ampersand rules: a
// single ampersand marks the following accelerator; a doubled ampersand is a
// literal ampersand.
func stripDialogButtonMnemonicMarkers(text string) string {
	var result strings.Builder
	result.Grow(len(text))
	for index := 0; index < len(text); index++ {
		if text[index] != '&' {
			result.WriteByte(text[index])
			continue
		}
		if index+1 < len(text) && text[index+1] == '&' {
			result.WriteByte('&')
			index++
		}
	}
	return result.String()
}

func dialogButtonContentLayout(
	body dialogButtonRect,
	textWidth, dpi int32,
	hasIcon bool,
) (icon, text dialogButtonRect, drawIcon bool) {
	inset := scaleDialogButtonMetric(4, dpi)
	content := body
	content.Left += inset
	content.Right -= inset
	if content.Right <= content.Left || content.Bottom <= content.Top {
		return dialogButtonRect{}, body, false
	}
	if !hasIcon {
		return dialogButtonRect{}, content, false
	}

	iconSize := scaleDialogButtonMetric(14, dpi)
	gap := scaleDialogButtonMetric(5, dpi)
	available := content.Right - content.Left
	maxText := available - iconSize - gap
	minText := scaleDialogButtonMetric(18, dpi)
	if maxText < minText {
		return dialogButtonRect{}, content, false
	}
	if textWidth < 1 {
		textWidth = minText
	}
	if textWidth > maxText {
		// Never sacrifice the action label just to keep a decorative glyph.
		// Walk sizes native BUTTON controls from their text, so a caller that
		// has not reserved icon space must fall back to the complete centered
		// label instead of displaying an ellipsis such as "Save setti…".
		return dialogButtonRect{}, content, false
	}
	groupWidth := iconSize + gap + textWidth
	left := content.Left + (available-groupWidth)/2
	top := content.Top + (content.Bottom-content.Top-iconSize)/2
	icon = dialogButtonRect{
		Left:   left,
		Top:    top,
		Right:  left + iconSize,
		Bottom: top + iconSize,
	}
	text = dialogButtonRect{
		Left:   icon.Right + gap,
		Top:    content.Top,
		Right:  icon.Right + gap + textWidth,
		Bottom: content.Bottom,
	}
	return icon, text, true
}

func createDialogButtonIconFont(dc uintptr, dpi int32) uintptr {
	if dc == 0 {
		return 0
	}
	for _, family := range []string{
		"Segoe Fluent Icons",
		"Segoe MDL2 Assets",
	} {
		face, err := windows.UTF16PtrFromString(family)
		if err != nil {
			continue
		}
		height := -scaleDialogButtonMetric(15, dpi)
		font, _, _ := dialogButtonIconCreateFont.Call(
			uintptr(uint32(height)),
			0,
			0,
			0,
			400,
			0,
			0,
			0,
			dialogButtonIconDefaultCharset,
			0,
			0,
			dialogButtonIconClearTypeQuality,
			0,
			uintptr(unsafe.Pointer(face)),
		)
		runtime.KeepAlive(face)
		if font == 0 {
			continue
		}
		if dialogButtonIconFontFaceMatches(dc, font, family) &&
			dialogButtonIconFontHasSemanticGlyphs(dc, font) {
			return font
		}
		_, _, _ = dialogButtonDeleteObject.Call(font)
	}
	return 0
}

func dialogButtonIconFontHasSemanticGlyphs(dc, font uintptr) bool {
	if dc == 0 || font == 0 {
		return false
	}
	previous, _, _ := dialogButtonSelectObject.Call(dc, font)
	if previous == 0 || previous == ^uintptr(0) {
		return false
	}
	defer func() {
		_, _, _ = dialogButtonSelectObject.Call(dc, previous)
	}()

	characters := [len(dialogButtonSemanticGlyphs)]uint16{}
	glyphs := [len(dialogButtonSemanticGlyphs)]uint16{}
	for index, glyph := range dialogButtonSemanticGlyphs {
		characters[index] = uint16(glyph)
	}
	result, _, _ := dialogButtonIconGetGlyphIndices.Call(
		dc,
		uintptr(unsafe.Pointer(&characters[0])),
		uintptr(len(characters)),
		uintptr(unsafe.Pointer(&glyphs[0])),
		dialogButtonIconMarkMissing,
	)
	runtime.KeepAlive(&characters)
	runtime.KeepAlive(&glyphs)
	if dialogButtonGlyphLookupFailed(result) {
		return false
	}
	for _, glyph := range glyphs {
		if glyph == 0xFFFF {
			return false
		}
	}
	return true
}

func dialogButtonGlyphLookupFailed(result uintptr) bool {
	// GetGlyphIndicesW returns a 32-bit GDI_ERROR even in a 64-bit process;
	// syscall.SyscallN therefore exposes 0x00000000FFFFFFFF rather than a
	// pointer-width all-ones value on x64.
	return uint32(result) == ^uint32(0)
}

func dialogButtonIconFontFaceMatches(dc, font uintptr, family string) bool {
	if dc == 0 || font == 0 {
		return false
	}
	previous, _, _ := dialogButtonSelectObject.Call(dc, font)
	if previous == 0 || previous == ^uintptr(0) {
		return false
	}
	defer func() {
		_, _, _ = dialogButtonSelectObject.Call(dc, previous)
	}()
	var face [64]uint16
	length, _, _ := dialogButtonIconGetTextFace.Call(
		dc,
		uintptr(len(face)),
		uintptr(unsafe.Pointer(&face[0])),
	)
	if length == 0 {
		return false
	}
	return strings.EqualFold(windows.UTF16ToString(face[:]), family)
}
