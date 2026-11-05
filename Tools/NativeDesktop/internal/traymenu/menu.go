// Package traymenu implements a polished, keyboard-accessible popup menu for
// a Windows notification-area application. Its custom popup keeps Acrylic or
// Blur live through a native DWM host and a per-pixel-alpha foreground, and it
// uses the native Windows popup menu only when explicitly requested or when
// Windows high-contrast accessibility mode is active. Ordinary custom
// presentation failures are retried with a fresh window and then reported.
package traymenu

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxItems     = 128
	maxTextRunes = 512
	maxPNGBytes  = 2 << 20
)

var (
	ErrNoItems             = errors.New("traymenu: menu has no items")
	ErrTooManyItems        = errors.New("traymenu: too many items")
	ErrInvalidItem         = errors.New("traymenu: invalid item")
	ErrDuplicateAction     = errors.New("traymenu: duplicate action ID")
	ErrInvalidAnchor       = errors.New("traymenu: invalid anchor")
	ErrWrongThread         = errors.New("traymenu: owner belongs to another UI thread")
	ErrCustomPresentation  = errors.New("traymenu: custom presentation failed")
	ErrUnsupportedPlatform = errors.New("traymenu: unsupported platform")
)

// ActionID is an application-defined stable identifier. It is intentionally
// independent of transient native command numbers.
type ActionID string

// Item describes one command or separator. IconPNG accepts ordinary PNG bytes
// such as assets.Files.ReadFile("icons/lamp-on.png"). IconHandle accepts a
// caller-owned HICON and is never destroyed by this package. If both are set,
// IconPNG is preferred by the custom renderer.
type Item struct {
	ID         ActionID
	Label      string
	Shortcut   string
	IconPNG    []byte
	IconHandle uintptr
	Checked    bool
	Default    bool
	Disabled   bool
	Separator  bool
}

// Theme controls the custom palette.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// Backdrop selects the best-effort live native custom-window material.
type Backdrop string

const (
	BackdropAuto    Backdrop = "auto"
	BackdropAcrylic Backdrop = "acrylic"
	BackdropBlur    Backdrop = "blur"
	BackdropSolid   Backdrop = "solid"
)

// Alignment controls horizontal placement relative to the anchor. Start and
// End are logical edges, so RightToLeft reverses their physical side.
type Alignment uint8

const (
	AlignAuto Alignment = iota
	AlignStart
	AlignEnd
)

// Rect is a screen-coordinate anchor or work area.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (r Rect) validAnchor() bool {
	return r.Right >= r.Left && r.Bottom >= r.Top
}

func (r Rect) validArea() bool {
	return r.Right > r.Left && r.Bottom > r.Top
}

// Options configures one popup invocation.
type Options struct {
	Owner     uintptr
	Items     []Item
	Theme     Theme
	Backdrop  Backdrop
	Alignment Alignment
	// RightToLeft mirrors logical menu columns and Start/End alignment while
	// leaving caller-provided shortcuts and other technical values LTR.
	RightToLeft bool
	// FontFamily selects the face used by the custom owner-drawn popup. An
	// empty value uses Segoe UI; Windows font fallback remains available.
	FontFamily string

	// PreferNative explicitly bypasses the custom live-material popup.
	PreferNative bool
	// DisableWindowReuse restores recreate-on-open behavior. The zero value
	// keeps one hidden popup window per owner/UI thread after its first use.
	// Every invocation is still fully repainted while hidden before reveal.
	DisableWindowReuse bool
	// DisableAnimations presents hover and open/close states immediately.
	// Windows High Contrast and reduced-motion policy also disable motion
	// regardless of this value.
	DisableAnimations bool
}

// NativeReason identifies an intentional selection of the native menu.
type NativeReason string

const (
	NativeReasonExplicit     NativeReason = "explicit"
	NativeReasonHighContrast NativeReason = "high_contrast"
)

// Result describes the selection. A dismissed menu has Cancelled=true and an
// empty Action. UsedNativeFallback reports whether TrackPopupMenu was used by
// explicit policy or for Windows high-contrast accessibility.
type Result struct {
	Action             ActionID
	Cancelled          bool
	UsedNativeFallback bool
	NativeReason       NativeReason
	BackdropUsed       Backdrop
}

// CustomPresentationError preserves both bounded custom-popup attempts. It
// deliberately does not imply a native fallback.
type CustomPresentationError struct {
	Initial    error
	FreshRetry error
}

func (e *CustomPresentationError) Error() string {
	if e == nil {
		return ErrCustomPresentation.Error()
	}
	return fmt.Sprintf(
		"%s (initial attempt: %v; fresh-window retry: %v)",
		ErrCustomPresentation,
		e.Initial,
		e.FreshRetry,
	)
}

func (e *CustomPresentationError) Is(target error) bool {
	return target == ErrCustomPresentation
}

func (e *CustomPresentationError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return []error{e.Initial, e.FreshRetry}
}

// Callback is invoked exactly once after a popup successfully completes,
// including cancellation. It runs synchronously on the calling UI thread.
type Callback func(Result)

// ShowAtCursor displays the menu at the current cursor position. When Owner is
// set, call it on the UI thread that owns that HWND. Walk notification-icon
// event handlers already run on that thread; code running in another goroutine
// should enqueue the call with Window.Synchronize.
func ShowAtCursor(options Options) (Result, error) {
	if err := validateOptions(options); err != nil {
		return Result{}, err
	}
	return showAtCursor(cloneOptions(options))
}

// Validate checks a shared menu model without creating windows or invoking an
// action. Embedders can preflight both presentation paths with the same rules.
func Validate(options Options) error { return validateOptions(options) }

// ShowAtAnchor displays the menu adjacent to a screen-coordinate anchor. The
// same owner-thread requirement as ShowAtCursor applies.
func ShowAtAnchor(options Options, anchor Rect) (Result, error) {
	if err := validateOptions(options); err != nil {
		return Result{}, err
	}
	if !anchor.validAnchor() {
		return Result{}, ErrInvalidAnchor
	}
	return showAtAnchor(cloneOptions(options), anchor)
}

// ShowAtCursorCallback is the callback form of ShowAtCursor.
func ShowAtCursorCallback(options Options, callback Callback) error {
	result, err := ShowAtCursor(options)
	if err != nil {
		return err
	}
	if callback != nil {
		callback(result)
	}
	return nil
}

// ShowAtAnchorCallback is the callback form of ShowAtAnchor.
func ShowAtAnchorCallback(options Options, anchor Rect, callback Callback) error {
	result, err := ShowAtAnchor(options, anchor)
	if err != nil {
		return err
	}
	if callback != nil {
		callback(result)
	}
	return nil
}

func validateOptions(options Options) error {
	if len(options.Items) == 0 {
		return ErrNoItems
	}
	if len(options.Items) > maxItems {
		return ErrTooManyItems
	}
	if err := validateTheme(options.Theme); err != nil {
		return err
	}
	if err := validateBackdrop(options.Backdrop); err != nil {
		return err
	}
	if options.Alignment > AlignEnd {
		return fmt.Errorf("%w: unknown alignment %d", ErrInvalidItem, options.Alignment)
	}

	seen := make(map[ActionID]struct{}, len(options.Items))
	defaultSeen := false
	for index, item := range options.Items {
		if item.Separator {
			if item.ID != "" || item.Label != "" || item.Shortcut != "" ||
				len(item.IconPNG) != 0 || item.IconHandle != 0 ||
				item.Checked || item.Default || item.Disabled {
				return fmt.Errorf("%w at index %d: separator has command fields", ErrInvalidItem, index)
			}
			continue
		}
		if item.ID == "" {
			return fmt.Errorf("%w at index %d: action ID is empty", ErrInvalidItem, index)
		}
		if item.Default {
			if defaultSeen {
				return fmt.Errorf("%w: multiple default actions", ErrInvalidItem)
			}
			defaultSeen = true
		}
		if err := validateText(string(item.ID), false); err != nil {
			return fmt.Errorf("%w at index %d: action ID: %v", ErrInvalidItem, index, err)
		}
		if _, exists := seen[item.ID]; exists {
			return fmt.Errorf("%w %q", ErrDuplicateAction, item.ID)
		}
		seen[item.ID] = struct{}{}
		if err := validateText(item.Label, false); err != nil {
			return fmt.Errorf("%w at index %d: label: %v", ErrInvalidItem, index, err)
		}
		if err := validateText(item.Shortcut, true); err != nil {
			return fmt.Errorf("%w at index %d: shortcut: %v", ErrInvalidItem, index, err)
		}
		if len(item.IconPNG) > maxPNGBytes {
			return fmt.Errorf("%w at index %d: PNG exceeds %d bytes", ErrInvalidItem, index, maxPNGBytes)
		}
	}
	return nil
}

func cloneOptions(options Options) Options {
	options.Items = append([]Item(nil), options.Items...)
	for index := range options.Items {
		options.Items[index].IconPNG = append([]byte(nil), options.Items[index].IconPNG...)
	}
	return options
}

func validateText(value string, emptyAllowed bool) error {
	if value == "" {
		if emptyAllowed {
			return nil
		}
		return errors.New("text is empty")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxTextRunes {
		return errors.New("text is invalid or too long")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New("text contains NUL")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("text contains a control character")
		}
	}
	return nil
}

func validateTheme(theme Theme) error {
	switch theme {
	case "", ThemeSystem, ThemeLight, ThemeDark:
		return nil
	default:
		return fmt.Errorf("%w: unknown theme %q", ErrInvalidItem, theme)
	}
}

func validateBackdrop(backdrop Backdrop) error {
	switch backdrop {
	case "", BackdropAuto, BackdropAcrylic, BackdropBlur, BackdropSolid:
		return nil
	default:
		return fmt.Errorf("%w: unknown backdrop %q", ErrInvalidItem, backdrop)
	}
}
