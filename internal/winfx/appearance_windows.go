//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

const (
	AppearanceThemeSystem = "system"
	AppearanceThemeLight  = "light"
	AppearanceThemeDark   = "dark"

	appearanceColorWindow               = 5
	appearanceSPIGetClientAreaAnimation = 0x1042
)

var appearanceGetSysColor = osUser32.NewProc("GetSysColor")

// ResolvedAppearance is one coherent snapshot of the Windows policies that
// influence custom application chrome. Theme is normalized to system, light,
// or dark. Dark remains the requested color-scheme result even in High
// Contrast; HighContrast tells renderers when Windows must own the palette.
type ResolvedAppearance struct {
	Theme               string
	Dark                bool
	HighContrast        bool
	TransparencyEnabled bool
	MotionEnabled       bool
}

type appearanceProbes struct {
	systemDark   func() (bool, error)
	windowDark   func() (bool, error)
	highContrast func() (bool, error)
	transparency func() (bool, error)
	motion       func() (bool, error)
}

// ResolveAppearance resolves an app theme override together with current
// Windows accessibility, transparency, and motion preferences. The returned
// snapshot is always usable. Probe failures are joined in err, while safe
// fallbacks keep rendering deterministic.
func ResolveAppearance(requestedTheme string) (ResolvedAppearance, error) {
	return resolveAppearanceWithProbes(requestedTheme, appearanceProbes{
		systemDark:   SystemDarkMode,
		windowDark:   systemWindowDarkMode,
		highContrast: HighContrast,
		transparency: TransparencyEnabled,
		motion:       clientAreaAnimationEnabled,
	})
}

func resolveAppearanceWithProbes(
	requestedTheme string,
	probes appearanceProbes,
) (ResolvedAppearance, error) {
	appearance := ResolvedAppearance{
		Theme:               strings.ToLower(strings.TrimSpace(requestedTheme)),
		TransparencyEnabled: true,
		MotionEnabled:       true,
	}
	if appearance.Theme == "" {
		appearance.Theme = AppearanceThemeSystem
	}

	var probeErrors []error
	switch appearance.Theme {
	case AppearanceThemeDark:
		appearance.Dark = true
	case AppearanceThemeLight:
		appearance.Dark = false
	case AppearanceThemeSystem:
		dark, err := probes.systemDark()
		if err == nil {
			appearance.Dark = dark
			break
		}
		probeErrors = append(
			probeErrors,
			fmt.Errorf("query Windows app color preference: %w", err),
		)
		fallback, fallbackErr := probes.windowDark()
		appearance.Dark = fallback
		if fallbackErr != nil {
			probeErrors = append(
				probeErrors,
				fmt.Errorf("infer app color preference from COLOR_WINDOW: %w", fallbackErr),
			)
		}
	default:
		probeErrors = append(
			probeErrors,
			fmt.Errorf("winfx: unsupported appearance theme %q", appearance.Theme),
		)
		// Preserve the historical behavior of an invalid explicit value:
		// render a readable light palette while reporting the configuration
		// problem to callers that surface diagnostics.
		appearance.Theme = AppearanceThemeLight
	}

	if probes.highContrast != nil {
		value, err := probes.highContrast()
		if err != nil {
			probeErrors = append(
				probeErrors,
				fmt.Errorf("query Windows High Contrast: %w", err),
			)
		} else {
			appearance.HighContrast = value
		}
	}
	if probes.transparency != nil {
		value, err := probes.transparency()
		if err != nil {
			probeErrors = append(
				probeErrors,
				fmt.Errorf("query Windows transparency preference: %w", err),
			)
		} else {
			appearance.TransparencyEnabled = value
		}
	}
	if probes.motion != nil {
		value, err := probes.motion()
		if err != nil {
			probeErrors = append(
				probeErrors,
				fmt.Errorf("query Windows client-area animation preference: %w", err),
			)
		} else {
			appearance.MotionEnabled = value
		}
	}
	if appearance.HighContrast {
		appearance.MotionEnabled = false
	}
	return appearance, errors.Join(probeErrors...)
}

func systemWindowDarkMode() (bool, error) {
	if err := appearanceGetSysColor.Find(); err != nil {
		return false, err
	}
	color, _, _ := appearanceGetSysColor.Call(appearanceColorWindow)
	return colorREFIsDark(uint32(color)), nil
}

func colorREFIsDark(color uint32) bool {
	r := int(color & 0xff)
	g := int((color >> 8) & 0xff)
	b := int((color >> 16) & 0xff)
	return (299*r+587*g+114*b)/1000 < 128
}

func clientAreaAnimationEnabled() (bool, error) {
	if err := osSystemParametersInfoW.Find(); err != nil {
		return true, err
	}
	enabled := int32(1)
	ok, _, callErr := osSystemParametersInfoW.Call(
		appearanceSPIGetClientAreaAnimation,
		0,
		uintptr(unsafe.Pointer(&enabled)),
		0,
	)
	if ok == 0 {
		return true, boolCallError(
			"SystemParametersInfoW(SPI_GETCLIENTAREAANIMATION)",
			callErr,
		)
	}
	return enabled != 0, nil
}
