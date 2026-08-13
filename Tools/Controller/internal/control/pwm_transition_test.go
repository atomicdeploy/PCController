package control

import (
	"testing"
	"time"
)

func TestPWMFadeUsesSharedBoundedCurves(t *testing.T) {
	duration := time.Second
	if got := pwmFadeValue(100, 4100, 0, duration, "ease"); got != 100 {
		t.Fatalf("start=%d", got)
	}
	if got := pwmFadeValue(100, 4100, duration/2, duration, "ease"); got != 2100 {
		t.Fatalf("ease midpoint=%d", got)
	}
	if got := pwmFadeValue(4100, 100, duration, duration, "linear"); got != 100 {
		t.Fatalf("end=%d", got)
	}
}

func TestPWMFadeValidation(t *testing.T) {
	if _, _, _, err := parsePWMFade([]string{"4096", "1000"}); err == nil {
		t.Fatal("accepted out-of-range target")
	}
	if _, _, _, err := parsePWMFade([]string{"4095", "19"}); err == nil {
		t.Fatal("accepted too-short fade")
	}
	if _, _, _, err := parsePWMFade([]string{"4095", "1000", "bounce"}); err == nil {
		t.Fatal("accepted unknown curve")
	}
}
