//go:build windows

package winfx

import "testing"

func TestEffectiveMonitorDPIFallback(t *testing.T) {
	t.Parallel()

	if got := effectiveMonitorDPI(0, 144); got != 144 {
		t.Fatalf("effectiveMonitorDPI(0, 144) = %d, want fallback 144", got)
	}
	if got := effectiveMonitorDPI(0, 0); got != 96 {
		t.Fatalf("effectiveMonitorDPI(0, 0) = %d, want normalized fallback 96", got)
	}
	if got := effectiveMonitorDPI(0, 1000); got != 96 {
		t.Fatalf("effectiveMonitorDPI(0, 1000) = %d, want normalized fallback 96", got)
	}
}
