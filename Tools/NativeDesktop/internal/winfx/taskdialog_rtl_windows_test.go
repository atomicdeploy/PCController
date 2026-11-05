//go:build windows

package winfx

import "testing"

func TestTaskDialogRTLConstantsMatchWin32(t *testing.T) {
	t.Parallel()
	if tdfRTLLayout != 0x00002000 {
		t.Fatalf("TDF_RTL_LAYOUT = %#x", tdfRTLLayout)
	}
	if mbRight != 0x00080000 || mbRTLReading != 0x00100000 {
		t.Fatalf(
			"MessageBox RTL flags = %#x/%#x",
			mbRight,
			mbRTLReading,
		)
	}
}

func TestTaskDialogDetectsPersianWithoutTreatingTechnicalTextAsRTL(t *testing.T) {
	t.Parallel()
	if !taskDialogContainsRTL("تنظیمات چراغ رایان") {
		t.Fatal("Persian task-dialog content was not detected")
	}
	for _, value := range []string{
		"http://rayanlamp.local/",
		"Ctrl+Alt+L",
		"C:\\RayanLamp\\script.ps1",
		"/config/st",
	} {
		if taskDialogContainsRTL(value) {
			t.Errorf("technical value %q was classified as RTL", value)
		}
	}
}
