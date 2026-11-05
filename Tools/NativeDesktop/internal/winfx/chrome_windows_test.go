//go:build windows

package winfx

import (
	"errors"
	"testing"
)

func TestTitleBarWindowStyleModes(t *testing.T) {
	t.Parallel()

	const unrelated = uint32(0x00000040)
	native := titleBarWindowStyle(wsPopup|unrelated, true)
	wantNative := uint32(
		wsCaption |
			wsSysMenu |
			wsMinimizeBox |
			wsMaximizeBox |
			wsThickFrame |
			wsClipChildren |
			wsClipSiblings,
	)
	if native&wantNative != wantNative || native&wsPopup != 0 ||
		native&unrelated == 0 {
		t.Fatalf("native style = %#x, required bits %#x", native, wantNative)
	}

	custom := titleBarWindowStyle(native, false)
	if custom&(wsCaption|wsSysMenu|wsMinimizeBox|wsMaximizeBox|wsPopup) != 0 {
		t.Fatalf("captionless style retained native chrome bits: %#x", custom)
	}
	wantCustom := uint32(wsThickFrame | wsClipChildren | wsClipSiblings)
	if custom&wantCustom != wantCustom || custom&unrelated == 0 {
		t.Fatalf("captionless style = %#x, required bits %#x", custom, wantCustom)
	}
}

func TestWindowChromeStyleBorderModes(t *testing.T) {
	t.Parallel()

	const unrelated = uint32(0x00000040)
	seed := uint32(
		wsCaption |
			wsThickFrame |
			wsMinimizeBox |
			wsMaximizeBox |
			wsPopup |
			wsSysMenu |
			unrelated,
	)
	tests := []struct {
		name       string
		native     bool
		borderMode string
		required   uint32
		forbidden  uint32
	}{
		{
			name:   "native automatic",
			native: true, borderMode: WindowBorderAuto,
			required: wsCaption | wsSysMenu | wsMinimizeBox |
				wsMaximizeBox | wsThickFrame |
				wsClipChildren | wsClipSiblings,
			forbidden: wsPopup,
		},
		{
			name:       "captionless automatic resize frame",
			borderMode: WindowBorderAuto,
			required:   wsThickFrame | wsClipChildren | wsClipSiblings,
			forbidden: wsCaption | wsSysMenu | wsMinimizeBox |
				wsMaximizeBox | wsPopup,
		},
		{
			name:       "captionless no border",
			borderMode: WindowBorderNone,
			required:   wsClipChildren | wsClipSiblings,
			forbidden: wsCaption | wsBorder | wsDlgFrame | wsThickFrame |
				wsSysMenu | wsMinimizeBox | wsMaximizeBox | wsPopup,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := windowChromeStyle(seed, test.native, test.borderMode)
			if err != nil {
				t.Fatal(err)
			}
			if got&test.required != test.required {
				t.Fatalf("style %#x does not contain required bits %#x", got, test.required)
			}
			if got&test.forbidden != 0 {
				t.Fatalf("style %#x contains forbidden bits %#x", got, test.forbidden)
			}
			if got&unrelated == 0 {
				t.Fatalf("style %#x discarded unrelated bits", got)
			}
		})
	}
}

func TestWindowChromeStyleRejectsImpossibleOrUnknownModes(t *testing.T) {
	t.Parallel()

	if _, err := windowChromeStyle(0, true, WindowBorderNone); err == nil {
		t.Fatal("native title bar unexpectedly accepted no-border mode")
	}
	if _, err := windowChromeStyle(0, false, "etched"); err == nil {
		t.Fatal("unknown border mode unexpectedly accepted")
	}
	got, err := windowChromeStyle(0, false, " AUTO ")
	if err != nil {
		t.Fatal(err)
	}
	if got&wsThickFrame == 0 {
		t.Fatalf("case-insensitive auto mode did not retain resize frame: %#x", got)
	}
}

func TestReassertWindowChromeOwnsBothCaptionlessBorders(t *testing.T) {
	t.Parallel()

	var borderColors []int32
	clearCalls := 0
	err := reassertWindowChrome(
		false,
		func(color int32) error {
			borderColors = append(borderColors, color)
			return nil
		},
		func() error {
			clearCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(borderColors) != 1 || borderColors[0] != dwmColorNone {
		t.Fatalf(
			"captionless DWM border colors = %v, want [%d]",
			borderColors,
			dwmColorNone,
		)
	}
	if clearCalls != 1 {
		t.Fatalf("captionless classic-edge clears = %d, want 1", clearCalls)
	}
}

func TestReassertWindowChromeRestoresNativeBorder(t *testing.T) {
	t.Parallel()

	var borderColor int32
	clearCalls := 0
	err := reassertWindowChrome(
		true,
		func(color int32) error {
			borderColor = color
			return nil
		},
		func() error {
			clearCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if borderColor != -1 {
		t.Fatalf("native DWM border color = %d, want default (-1)", borderColor)
	}
	if clearCalls != 0 {
		t.Fatalf("native classic-edge clears = %d, want 0", clearCalls)
	}
}

func TestReassertWindowChromeReportsIndependentPolicyFailures(t *testing.T) {
	t.Parallel()

	borderErr := errors.New("DWM border")
	edgeErr := errors.New("classic edge")
	err := reassertWindowChrome(
		false,
		func(int32) error { return borderErr },
		func() error { return edgeErr },
	)
	if !errors.Is(err, borderErr) || !errors.Is(err, edgeErr) {
		t.Fatalf("joined policy error = %v, want both native failures", err)
	}
}
