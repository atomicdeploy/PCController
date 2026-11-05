//go:build windows

package winfx

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestTaskDialogRuntimeFallbackProbe(t *testing.T) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")
	postMessage := user32.NewProc("PostMessageW")
	go func() {
		for range 50 {
			for _, title := range []string{
				"RayanLamp TaskDialog probe",
				"CALLBACK-PROBE",
			} {
				value, _ := windows.UTF16PtrFromString(title)
				hwnd, _, _ := findWindow.Call(
					0,
					uintptr(unsafe.Pointer(value)),
				)
				runtime.KeepAlive(value)
				if hwnd != 0 {
					_, _, _ = postMessage.Call(hwnd, 0x0010, 0, 0)
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	options := TaskDialogOptions{
		Title:           "RayanLamp TaskDialog probe",
		MainInstruction: "Probe",
		Content:         "Probe",
		Icon:            DialogIconInformation,
		Buttons:         DialogButtonOK,
		AllowCancel:     true,
	}
	result, err := taskDialogIndirect(options, true)
	t.Logf("result=%+v err=%v", result, err)
}
