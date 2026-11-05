//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	BackdropAuto    = "auto"
	BackdropMica    = "mica"
	BackdropAcrylic = "acrylic"
	BackdropBlur    = "blur"
	BackdropTabbed  = "tabbed"
	BackdropSolid   = "solid"
	// BackdropTransparent disables composition material without painting a
	// fallback surface. A WS_EX_LAYERED caller can then present genuine
	// alpha-zero pixels for per-pixel click-through.
	BackdropTransparent = "transparent"

	dwmwaUseImmersiveDarkMode = 20
	dwmwaUseImmersiveDarkOld  = 19
	dwmwaNCRenderingPolicy    = 2
	dwmwaWindowCornerPref     = 33
	dwmwaBorderColor          = 34
	dwmwaSystemBackdropType   = 38

	dwmNCRenderingDisabled = 1
	dwmNCRenderingEnabled  = 2
	dwmColorNone           = -2

	dwmSystemBackdropNone      = 1
	dwmSystemBackdropMain      = 2
	dwmSystemBackdropTransient = 3
	dwmSystemBackdropTabbed    = 4

	wcaAccentPolicy = 19

	accentDisabled            = 0
	accentEnableBlurBehind    = 3
	accentEnableAcrylicBehind = 4

	dwmBBEnable = 0x00000001

	redrawInvalidate = 0x0001
	redrawNoErase    = 0x0020
	redrawNoChildren = 0x0040
	redrawUpdateNow  = 0x0100
	redrawFrame      = 0x0400
)

var (
	ErrInvalidWindow       = errors.New("winfx: invalid window handle")
	ErrUnknownBackdrop     = errors.New("winfx: unknown backdrop")
	ErrBackdropUnavailable = errors.New("winfx: no requested backdrop API succeeded")
	ErrBackdropCombination = errors.New("winfx: unsupported backdrop and chrome combination")

	backdropDWMAPI                  = windows.NewLazySystemDLL("dwmapi.dll")
	backdropDwmGetWindowAttribute   = backdropDWMAPI.NewProc("DwmGetWindowAttribute")
	backdropDwmSetWindowAttribute   = backdropDWMAPI.NewProc("DwmSetWindowAttribute")
	backdropDwmEnableBlurBehind     = backdropDWMAPI.NewProc("DwmEnableBlurBehindWindow")
	backdropDwmExtendFrame          = backdropDWMAPI.NewProc("DwmExtendFrameIntoClientArea")
	backdropDwmFlush                = backdropDWMAPI.NewProc("DwmFlush")
	backdropUser32                  = windows.NewLazySystemDLL("user32.dll")
	backdropSetCompositionAttribute = backdropUser32.NewProc("SetWindowCompositionAttribute")
	backdropIsWindow                = backdropUser32.NewProc("IsWindow")
	backdropRedrawWindow            = backdropUser32.NewProc("RedrawWindow")
)

type accentPolicy struct {
	State         int32
	Flags         uint32
	GradientColor uint32
	AnimationID   int32
}

// compositionAttributeData intentionally stores the pointer as uintptr. This
// gives the native 12-byte (32-bit) / 24-byte (64-bit) layout without placing a
// Go pointer inside another object passed to a syscall.
type compositionAttributeData struct {
	Attribute uint32
	Data      uintptr
	Size      uintptr
}

type dwmBlurBehind struct {
	Flags                 uint32
	Enable                int32
	BlurRegion            uintptr
	TransitionOnMaximized int32
}

type dwmMargins struct {
	Left, Right, Top, Bottom int32
}

// Apply clears any prior material, then selects and applies the best available
// backdrop. requested accepts auto, mica, acrylic, blur, solid, transparent,
// none, and off (case-insensitive).
//
// The function is safe to call repeatedly after theme, composition, settings,
// or DWM colorization changes. Auto deliberately reproduces GlassDWM's final
// startup stack instead of treating these APIs as mutually exclusive:
// MainWindow system backdrop + transparent Accent blur + DWM blur + a full
// glass frame on a per-pixel top-level WS_EX_LAYERED window.
func Apply(
	hwnd uintptr,
	requested string,
	dark bool,
	nativeTitleBar bool,
) (actual string, err error) {
	return applyBackdrop(hwnd, requested, dark, nativeTitleBar, true)
}

// Refresh reapplies one unchanged material without first disabling the active
// DWM/Accent stack. It is used after showing, activation-independent DWM
// notifications, and dialog returns, where a destructive clear/rebuild caused
// a visible opaque flash even though the requested mode had not changed.
func Refresh(
	hwnd uintptr,
	requested string,
	dark bool,
	nativeTitleBar bool,
) (actual string, err error) {
	return applyBackdrop(hwnd, requested, dark, nativeTitleBar, false)
}

func applyBackdrop(
	hwnd uintptr,
	requested string,
	dark bool,
	nativeTitleBar bool,
	reset bool,
) (actual string, err error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return BackdropSolid, ErrInvalidWindow
	}

	requested, err = normalizeRequestedBackdrop(requested)
	if err != nil {
		if reset {
			clearBackdrop(hwnd)
		}
		return BackdropSolid, err
	}

	version := CurrentVersion()
	if nativeTitleBar {
		// DWM can rebuild and cache its non-client visual while any of the
		// material, frame, or NC-rendering attributes below are changing. A
		// dark-mode write made only before those calls is therefore not enough:
		// the rebuilt caption can remain light until a later activation. Defer
		// the final pass so every successful and fallback branch performs it
		// after its last composition mutation.
		defer finalizeNativeCaption(
			func() { applyDarkMode(hwnd, dark, version) },
			func() { redrawNativeCaption(hwnd) },
		)
	}

	// Make switching among the individual experiment modes deterministic.
	// Unsupported clear calls are intentionally ignored.
	if reset {
		clearBackdrop(hwnd)
	}
	applyDarkMode(hwnd, dark, version)
	// A captionless WS_THICKFRAME is retained for resizing. Suppress its DWM
	// outline so it cannot become a second visible border around the glass.
	// Native chrome needs the opposite: retain the system-selected frame and
	// keep non-client rendering enabled across every backdrop reapplication.
	borderColor := int32(dwmColorNone)
	if nativeTitleBar {
		borderColor = -1
	}
	_ = setDWMInt32(hwnd, dwmwaBorderColor, borderColor)
	info, _ := GetOSInfo()
	if highContrastRequiresSolid(requested, info.HighContrast) {
		// High Contrast owns every visible pixel, including an explicitly
		// transparent client. Leaving alpha-zero holes or a live material in
		// place would make system-selected foreground colors unreadable.
		clearBackdropOnFallback(hwnd, reset)
		ncPolicy := int32(dwmNCRenderingDisabled)
		if nativeTitleBar {
			ncPolicy = dwmNCRenderingEnabled
		}
		_ = setDWMInt32(
			hwnd,
			dwmwaNCRenderingPolicy,
			ncPolicy,
		)
		_ = extendFrame(hwnd, false)
		return BackdropSolid, nil
	}
	if requested == BackdropSolid {
		ncPolicy := int32(dwmNCRenderingDisabled)
		if nativeTitleBar {
			ncPolicy = dwmNCRenderingEnabled
		}
		_ = setDWMInt32(
			hwnd,
			dwmwaNCRenderingPolicy,
			ncPolicy,
		)
		_ = extendFrame(hwnd, false)
		return BackdropSolid, nil
	}
	if requested == BackdropTransparent {
		if nativeTitleBar {
			_ = setDWMInt32(
				hwnd,
				dwmwaNCRenderingPolicy,
				dwmNCRenderingEnabled,
			)
			_ = extendFrame(hwnd, false)
			return BackdropSolid, fmt.Errorf(
				"%w: transparent per-pixel clients require custom or absent chrome",
				ErrBackdropCombination,
			)
		}
		_ = setDWMInt32(
			hwnd,
			dwmwaNCRenderingPolicy,
			dwmNCRenderingDisabled,
		)
		_ = extendFrame(hwnd, false)
		return BackdropTransparent, nil
	}
	if !info.TransparencyEnabled || !info.DWMComposition {
		// Refresh normally preserves an unchanged material to avoid a visible
		// flash. Here the effective mode has changed to solid, so preserving the
		// previous system-backdrop/Accent/DWM stack would violate the current
		// accessibility or composition policy.
		clearBackdropOnFallback(hwnd, reset)
		ncPolicy := int32(dwmNCRenderingDisabled)
		if nativeTitleBar {
			ncPolicy = dwmNCRenderingEnabled
		}
		_ = setDWMInt32(
			hwnd,
			dwmwaNCRenderingPolicy,
			ncPolicy,
		)
		_ = extendFrame(hwnd, false)
		return BackdropSolid, nil
	}

	var failures []error
	if err := setDWMInt32(
		hwnd,
		dwmwaNCRenderingPolicy,
		dwmNCRenderingEnabled,
	); err != nil {
		failures = append(failures, fmt.Errorf("enable DWM non-client rendering: %w", err))
	}
	if err := extendFrame(hwnd, true); err != nil {
		failures = append(failures, fmt.Errorf("extend glass into client: %w", err))
	}

	if requested == BackdropAuto {
		applied := false
		if info.SupportsSystemBackdrop {
			if err := setDWMInt32(
				hwnd,
				dwmwaSystemBackdropType,
				dwmSystemBackdropMain,
			); err == nil {
				applied = true
			} else {
				failures = append(
					failures,
					fmt.Errorf("GlassDWM MainWindow backdrop: %w", err),
				)
			}
		}
		if info.SupportsAccentPolicy {
			if err := applyAccent(hwnd, accentEnableBlurBehind, dark); err == nil {
				applied = true
			} else {
				failures = append(
					failures,
					fmt.Errorf("GlassDWM AccentPolicy blur: %w", err),
				)
			}
		}
		if info.SupportsDWMBlurBehind {
			if err := setDWMBlurBehind(hwnd, true); err == nil {
				applied = true
			} else {
				failures = append(
					failures,
					fmt.Errorf("GlassDWM DwmEnableBlurBehindWindow: %w", err),
				)
			}
		}
		if applied {
			return BackdropAuto, errors.Join(failures...)
		}
	}

	if info.SupportsSystemBackdrop &&
		(requested == BackdropMica ||
			requested == BackdropTabbed ||
			requested == BackdropAcrylic) {
		backdropType := int32(dwmSystemBackdropMain)
		resultName := BackdropMica
		if requested == BackdropTabbed {
			backdropType = dwmSystemBackdropTabbed
			resultName = BackdropTabbed
		} else if requested == BackdropAcrylic {
			backdropType = dwmSystemBackdropTransient
			resultName = BackdropAcrylic
		}
		if err := setDWMInt32(hwnd, dwmwaSystemBackdropType, backdropType); err == nil {
			return resultName, nil
		} else {
			failures = append(failures, fmt.Errorf("official system backdrop: %w", err))
		}
	}

	if info.SupportsAccentPolicy {
		wantAcrylic := requested == BackdropAcrylic && info.SupportsAcrylicAccent
		if wantAcrylic {
			if err := applyAccent(hwnd, accentEnableAcrylicBehind, dark); err == nil {
				return BackdropAcrylic, nil
			} else {
				failures = append(failures, fmt.Errorf("AccentPolicy acrylic: %w", err))
			}
		}
		if requested == BackdropBlur {
			if err := applyAccent(hwnd, accentEnableBlurBehind, dark); err == nil {
				// Match WindowEnableBlurBehind: Accent blur and DWM blur are
				// intentionally both enabled.
				if info.SupportsDWMBlurBehind {
					if err := setDWMBlurBehind(hwnd, true); err != nil {
						failures = append(
							failures,
							fmt.Errorf("DwmEnableBlurBehindWindow: %w", err),
						)
					}
				}
				return BackdropBlur, nil
			} else {
				failures = append(
					failures,
					fmt.Errorf("AccentPolicy blur: %w", err),
				)
			}
		}
	}

	if info.SupportsDWMBlurBehind {
		if err := setDWMBlurBehind(hwnd, true); err == nil {
			return BackdropBlur, nil
		} else {
			failures = append(failures, fmt.Errorf("DwmEnableBlurBehindWindow: %w", err))
		}
	}

	if len(failures) == 0 {
		failures = append(failures, ErrBackdropUnavailable)
	}
	// None of the requested APIs succeeded. Refresh must dismantle any
	// previously active stack before reporting the solid fallback.
	clearBackdropOnFallback(hwnd, reset)
	ncPolicy := int32(dwmNCRenderingDisabled)
	if nativeTitleBar {
		ncPolicy = dwmNCRenderingEnabled
	}
	_ = setDWMInt32(hwnd, dwmwaNCRenderingPolicy, ncPolicy)
	_ = extendFrame(hwnd, false)
	return BackdropSolid, errors.Join(failures...)
}

func highContrastRequiresSolid(requested string, highContrast bool) bool {
	return highContrast && requested != BackdropSolid
}

func normalizeRequestedBackdrop(requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", BackdropAuto:
		return BackdropAuto, nil
	case BackdropMica:
		return BackdropMica, nil
	case BackdropAcrylic:
		return BackdropAcrylic, nil
	case BackdropBlur:
		return BackdropBlur, nil
	case BackdropTabbed:
		return BackdropTabbed, nil
	case BackdropTransparent:
		return BackdropTransparent, nil
	case BackdropSolid, "none", "off", "disabled":
		return BackdropSolid, nil
	default:
		return BackdropSolid, fmt.Errorf("%w %q", ErrUnknownBackdrop, requested)
	}
}

func isWindow(hwnd uintptr) bool {
	if backdropIsWindow.Find() != nil {
		return false
	}
	ok, _, _ := backdropIsWindow.Call(hwnd)
	return ok != 0
}

func clearBackdrop(hwnd uintptr) {
	_ = setDWMInt32(hwnd, dwmwaSystemBackdropType, dwmSystemBackdropNone)
	_ = applyAccent(hwnd, accentDisabled, false)
	_ = setDWMBlurBehind(hwnd, false)
}

func clearBackdropOnFallback(hwnd uintptr, reset bool) {
	// Apply already cleared before attempting a new material. Refresh did not,
	// and therefore needs the explicit teardown when its effective mode changes
	// to solid.
	if fallbackRequiresBackdropClear(reset) {
		clearBackdrop(hwnd)
	}
}

func fallbackRequiresBackdropClear(reset bool) bool {
	return !reset
}

func setDWMInt32(hwnd uintptr, attribute uint32, value int32) error {
	if err := backdropDwmSetWindowAttribute.Find(); err != nil {
		return err
	}
	result, _, _ := backdropDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	runtime.KeepAlive(&value)
	return hresultError("DwmSetWindowAttribute", result)
}

func getDWMInt32(hwnd uintptr, attribute uint32) (int32, error) {
	if err := backdropDwmGetWindowAttribute.Find(); err != nil {
		return 0, err
	}
	var value int32
	result, _, _ := backdropDwmGetWindowAttribute.Call(
		hwnd,
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	runtime.KeepAlive(&value)
	if err := hresultError("DwmGetWindowAttribute", result); err != nil {
		return 0, err
	}
	return value, nil
}

func applyDarkMode(hwnd uintptr, dark bool, version VersionInfo) {
	if version.Major < 10 || version.Build < 17763 {
		return
	}
	value := int32(0)
	if dark {
		value = 1
	}
	if err := setDWMInt32(hwnd, dwmwaUseImmersiveDarkMode, value); err == nil {
		return
	}
	// Builds before Windows 10 20H1 used attribute 19 for this setting.
	if version.Build < 19041 {
		_ = setDWMInt32(hwnd, dwmwaUseImmersiveDarkOld, value)
	}
}

func finalizeNativeCaption(reapplyDarkMode, redrawFrame func()) {
	// Keep the order explicit and testable: the repaint must consume the final
	// DWM attribute state, never the other way around.
	if reapplyDarkMode != nil {
		reapplyDarkMode()
	}
	if redrawFrame != nil {
		redrawFrame()
	}
}

func nativeCaptionRedrawFlags() uintptr {
	// RDW_FRAME only schedules WM_NCPAINT when paired with RDW_INVALIDATE.
	// RDW_UPDATENOW prevents the light/default caption from surviving until
	// activation, while NOERASE/NOCHILDREN avoid disturbing the client glass.
	return redrawInvalidate |
		redrawNoErase |
		redrawNoChildren |
		redrawUpdateNow |
		redrawFrame
}

func redrawNativeCaption(hwnd uintptr) {
	if hwnd == 0 || backdropRedrawWindow.Find() != nil {
		return
	}
	_, _, _ = backdropRedrawWindow.Call(
		hwnd,
		0,
		0,
		nativeCaptionRedrawFlags(),
	)
}

func applyAccent(hwnd uintptr, state int32, dark bool) error {
	if err := backdropSetCompositionAttribute.Find(); err != nil {
		return err
	}

	// AccentPolicy colors use AABBGGRR. GlassDWM's Blur mode is genuinely
	// transparent; its Acrylic experiment uses alpha 1 over black.
	gradient := uint32(0x01000000)
	if state == accentEnableBlurBehind {
		gradient = 0
	}
	if state == accentDisabled {
		gradient = 0
	}
	policy := accentPolicy{
		State:         state,
		GradientColor: gradient,
	}
	data := compositionAttributeData{
		Attribute: wcaAccentPolicy,
		Data:      uintptr(unsafe.Pointer(&policy)),
		Size:      unsafe.Sizeof(policy),
	}
	ok, _, callErr := backdropSetCompositionAttribute.Call(
		hwnd,
		uintptr(unsafe.Pointer(&data)),
	)
	runtime.KeepAlive(&policy)
	runtime.KeepAlive(&data)
	if ok == 0 {
		return boolCallError("SetWindowCompositionAttribute", callErr)
	}
	return nil
}

func setDWMBlurBehind(hwnd uintptr, enabled bool) error {
	if err := backdropDwmEnableBlurBehind.Find(); err != nil {
		return err
	}
	value := dwmBlurBehind{Flags: dwmBBEnable}
	if enabled {
		value.Enable = 1
	}
	result, _, _ := backdropDwmEnableBlurBehind.Call(
		hwnd,
		uintptr(unsafe.Pointer(&value)),
	)
	runtime.KeepAlive(&value)
	return hresultError("DwmEnableBlurBehindWindow", result)
}

func extendFrame(hwnd uintptr, full bool) error {
	if err := backdropDwmExtendFrame.Find(); err != nil {
		return err
	}
	margins := dwmMargins{}
	if full {
		margins = dwmMargins{Left: -1, Right: -1, Top: -1, Bottom: -1}
	}
	result, _, _ := backdropDwmExtendFrame.Call(
		hwnd,
		uintptr(unsafe.Pointer(&margins)),
	)
	runtime.KeepAlive(&margins)
	return hresultError("DwmExtendFrameIntoClientArea", result)
}

// FlushComposition waits until DWM has processed the material, frame, and
// layered-surface commands already issued by the calling UI thread. The
// caller must still commit its pixels first; DwmFlush is a barrier, not a
// replacement for UpdateLayeredWindow or WM_PAINT.
func FlushComposition() error {
	if err := backdropDwmFlush.Find(); err != nil {
		return err
	}
	result, _, _ := backdropDwmFlush.Call()
	return hresultError("DwmFlush", result)
}
