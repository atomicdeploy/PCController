package tui

import (
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestAdaptiveEngineeringUnits(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{formatPower(3500, 1), "3.5 W"},
		{formatPower(350, 1), "350.0 mW"},
		{formatCurrent(286, 1), "286.0 mA"},
		{formatCurrent(2400, 2), "2.40 A"},
		{formatVoltage(12224, 2), "12.22 V"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("format got %q want %q", test.got, test.want)
		}
	}
}

func TestFreshnessRemainsLiveAcrossRemoteConvergenceWindow(t *testing.T) {
	now := time.Unix(100, 0)
	for _, age := range []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, freshnessLiveThreshold - time.Millisecond} {
		if got := freshnessLabel(now.Add(-age), now); got != "live" {
			t.Errorf("age %s=%q", age, got)
		}
	}
	if got := freshnessLabel(now.Add(-freshnessLiveThreshold), now); got != "1.5 s ago" {
		t.Fatalf("stale age=%q", got)
	}
}

func TestFormatUptime(t *testing.T) {
	if got := formatUptime(4_392_210); got != "1h13m12s" {
		t.Fatalf("formatUptime=%q", got)
	}
	if got := formatUptime(250); got != "250 ms" {
		t.Fatalf("short formatUptime=%q", got)
	}
}

func TestSparklineTracksRange(t *testing.T) {
	got := sparkline([]float64{1, 2, 3, 4}, 4)
	if len([]rune(got)) != 4 || !strings.HasPrefix(got, "▁") || !strings.HasSuffix(got, "█") {
		t.Fatalf("sparkline=%q", got)
	}
}

func structFrameHello() native.Frame {
	return native.Frame{Opcode: native.OpHelloResp, Seq: 9, Payload: []byte{1, 2, 3}}
}
