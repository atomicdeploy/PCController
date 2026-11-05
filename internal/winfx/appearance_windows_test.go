//go:build windows

package winfx

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveAppearanceSystemUsesWindowColorFallbackAndCollectsErrors(
	t *testing.T,
) {
	registryErr := errors.New("registry denied")
	transparencyErr := errors.New("transparency denied")
	motionErr := errors.New("motion unavailable")
	appearance, err := resolveAppearanceWithProbes(
		" SYSTEM ",
		appearanceProbes{
			systemDark: func() (bool, error) {
				return false, registryErr
			},
			windowDark: func() (bool, error) {
				return true, nil
			},
			highContrast: func() (bool, error) {
				return true, nil
			},
			transparency: func() (bool, error) {
				return false, transparencyErr
			},
			motion: func() (bool, error) {
				return false, motionErr
			},
		},
	)
	if appearance.Theme != AppearanceThemeSystem || !appearance.Dark {
		t.Fatalf("system appearance = %+v, want dark COLOR_WINDOW fallback", appearance)
	}
	if !appearance.HighContrast {
		t.Fatal("High Contrast result was not retained")
	}
	if !appearance.TransparencyEnabled {
		t.Fatal("transparency probe failure did not retain safe enabled fallback")
	}
	if appearance.MotionEnabled {
		t.Fatal("High Contrast did not force client-area motion off")
	}
	for _, want := range []string{
		registryErr.Error(),
		transparencyErr.Error(),
		motionErr.Error(),
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("joined probe error %q does not contain %q", err, want)
		}
	}
}

func TestResolveAppearanceExplicitThemeSkipsSystemColorProbes(t *testing.T) {
	systemCalls, fallbackCalls := 0, 0
	probes := appearanceProbes{
		systemDark: func() (bool, error) {
			systemCalls++
			return false, nil
		},
		windowDark: func() (bool, error) {
			fallbackCalls++
			return false, nil
		},
		highContrast: func() (bool, error) {
			return false, nil
		},
		transparency: func() (bool, error) {
			return false, nil
		},
		motion: func() (bool, error) {
			return true, nil
		},
	}
	dark, err := resolveAppearanceWithProbes(AppearanceThemeDark, probes)
	if err != nil {
		t.Fatalf("resolve explicit dark: %v", err)
	}
	if !dark.Dark || dark.TransparencyEnabled || !dark.MotionEnabled {
		t.Fatalf("explicit dark appearance = %+v", dark)
	}
	light, err := resolveAppearanceWithProbes(AppearanceThemeLight, probes)
	if err != nil {
		t.Fatalf("resolve explicit light: %v", err)
	}
	if light.Dark {
		t.Fatalf("explicit light appearance = %+v", light)
	}
	if systemCalls != 0 || fallbackCalls != 0 {
		t.Fatalf(
			"explicit themes called system=%d fallback=%d probes",
			systemCalls,
			fallbackCalls,
		)
	}
}

func TestResolveAppearanceMotionHonorsWindowsAndHighContrast(t *testing.T) {
	probes := appearanceProbes{
		systemDark: func() (bool, error) { return false, nil },
		windowDark: func() (bool, error) { return false, nil },
		highContrast: func() (bool, error) {
			return false, nil
		},
		transparency: func() (bool, error) {
			return true, nil
		},
		motion: func() (bool, error) {
			return false, nil
		},
	}
	appearance, err := resolveAppearanceWithProbes("", probes)
	if err != nil {
		t.Fatalf("resolve system appearance: %v", err)
	}
	if appearance.MotionEnabled {
		t.Fatal("disabled Windows client-area animation preference was ignored")
	}
}

func TestColorREFIsDark(t *testing.T) {
	tests := []struct {
		name  string
		color uint32
		dark  bool
	}{
		{name: "black", color: 0x000000, dark: true},
		{name: "white", color: 0xFFFFFF, dark: false},
		{name: "dark blue BGR", color: 0x401008, dark: true},
		{name: "light cream BGR", color: 0xE8F4FF, dark: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := colorREFIsDark(test.color); got != test.dark {
				t.Fatalf("colorREFIsDark(%#06x) = %v, want %v", test.color, got, test.dark)
			}
		})
	}
}
