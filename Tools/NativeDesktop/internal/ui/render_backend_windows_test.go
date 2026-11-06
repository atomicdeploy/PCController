//go:build windows

package ui

import (
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/windx"
)

func TestNormalizedRenderingBackend(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  windx.Backend
	}{
		{"", windx.BackendAuto},
		{"AUTO", windx.BackendAuto},
		{" directx ", windx.BackendDirectX},
		{"legacy", windx.BackendLegacy},
		{"unknown", windx.BackendAuto},
	} {
		if got := normalizedRenderingBackend(test.input); got != test.want {
			t.Fatalf(
				"normalizedRenderingBackend(%q) = %q, want %q",
				test.input,
				got,
				test.want,
			)
		}
	}
}

func TestRenderingPolicyPreservesExactTransparentHitTesting(t *testing.T) {
	t.Parallel()
	ordinary := config.UIConfig{Backdrop: config.BackdropAcrylic}
	transparent := config.UIConfig{Backdrop: config.BackdropTransparent}

	if !renderingPolicyEligible(windx.BackendAuto, ordinary) {
		t.Fatal("automatic DirectX was rejected for acrylic")
	}
	if !renderingPolicyEligible(windx.BackendDirectX, ordinary) {
		t.Fatal("explicit DirectX was rejected for acrylic")
	}
	if renderingPolicyEligible(windx.BackendLegacy, ordinary) {
		t.Fatal("legacy selection was treated as DirectX eligible")
	}
	if renderingPolicyEligible(windx.BackendDirectX, transparent) {
		t.Fatal("transparent click-through mode was routed through DirectComposition")
	}
}

func TestRenderingSummaryReportsHardwareAndFallback(t *testing.T) {
	t.Parallel()
	shell := &Shell{
		cfg: config.Config{
			UI: config.UIConfig{RenderBackend: config.RenderBackendDirectX},
		},
		rendering: renderingState{
			requested: windx.BackendDirectX,
			capability: windx.Capability{
				Backend:              windx.BackendDirectX,
				Available:            true,
				Hardware:             true,
				DirectComposition:    true,
				Adapter:              "Test GPU",
				FeatureLevel:         windx.FeatureLevel12_1,
				DedicatedVideoMemory: 8 * 1024 * 1024 * 1024,
			},
			probeAttempted: true,
			fallbackReason: "test fallback",
			deviceLosses:   2,
		},
	}
	summary := shell.renderingSummary()
	for _, want := range []string{
		"Requested: DirectX 11 hardware",
		"Effective: Compatibility GDI / UpdateLayeredWindow",
		"Adapter: Test GPU",
		"D3D feature level: 12.1",
		"Dedicated video memory: 8192 MiB",
		"Hardware / DirectComposition: Yes / Yes",
		"Recovered device losses: 2",
		"Fallback: test fallback",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("rendering summary %q does not contain %q", summary, want)
		}
	}
}
