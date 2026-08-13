//go:build windows

package hostui

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestLegacyBalloonNotifyIconDataMatchesWindowsSDK(t *testing.T) {
	want := uintptr(956)
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		want = 976
	}
	if got := unsafe.Sizeof(balloonNotifyIconData{}); got != want {
		t.Fatalf("NOTIFYICONDATAW size=%d; want %d", got, want)
	}
}

func TestLegacyBalloonUTF16CopyIsBoundedAndNULTerminated(t *testing.T) {
	value := make([]uint16, 4)
	copyBalloonUTF16(value, "ABCDE")
	if value[0] != 'A' || value[1] != 'B' || value[2] != 'C' || value[3] != 0 {
		t.Fatalf("copied=%v", value)
	}
}
