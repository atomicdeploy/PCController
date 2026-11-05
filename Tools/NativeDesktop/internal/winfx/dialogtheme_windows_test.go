//go:build windows

package winfx

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDialogControlTheme(t *testing.T) {
	tests := []struct {
		className string
		dark      bool
		want      string
	}{
		{"Button", false, "Explorer"},
		{"Button", true, "DarkMode_Explorer"},
		{"Edit", true, "DarkMode_CFD"},
		{"ComboBox", true, "DarkMode_CFD"},
		{"SysTabControl32", true, "DarkMode_Explorer"},
	}
	for _, test := range tests {
		if got := dialogControlTheme(test.className, test.dark); got != test.want {
			t.Errorf(
				"dialogControlTheme(%q, %v) = %q, want %q",
				test.className,
				test.dark,
				got,
				test.want,
			)
		}
	}
}

func TestDialogThemeHRESULTOnlyFallsBackOnFailure(t *testing.T) {
	for _, success := range []uintptr{0, 1, 0x7FFFFFFF} {
		if !dialogThemeHRESULTSucceeded(success) {
			t.Fatalf("successful HRESULT %#x was treated as failure", success)
		}
	}
	for _, failure := range []uintptr{0x80004005, 0xFFFFFFFF} {
		if dialogThemeHRESULTSucceeded(failure) {
			t.Fatalf("failed HRESULT %#x was treated as success", failure)
		}
	}
}

func TestDialogEnumerationErrorIgnoresStaleLastErrorOnSuccess(t *testing.T) {
	stale := windows.ERROR_INVALID_HANDLE
	if err := dialogEnumerationError(1, stale); err != nil {
		t.Fatalf("successful enumeration reported stale last error: %v", err)
	}
	if err := dialogEnumerationError(0, windows.ERROR_SUCCESS); err != nil {
		t.Fatalf("ERROR_SUCCESS was reported as a failure: %v", err)
	}
	if err := dialogEnumerationError(0, stale); !errors.Is(err, stale) {
		t.Fatalf("failed enumeration error = %v, want %v", err, stale)
	}
}

func TestDialogCustomChromeYieldsCompletelyToHighContrast(t *testing.T) {
	if !dialogCustomControlChromeEnabled(false) {
		t.Fatal("custom dialog chrome was disabled outside high contrast")
	}
	if dialogCustomControlChromeEnabled(true) {
		t.Fatal("custom dialog chrome overrode the system high-contrast renderer")
	}
}

func TestDialogThemeObserverRefreshMessages(t *testing.T) {
	refreshMessages := []uint32{
		dialogWMSysColorChange,
		dialogWMSettingChange,
		dialogWMThemeChanged,
		dialogWMDWMCompositionChanged,
		dialogWMDWMColorizationColorChanged,
	}
	for _, message := range refreshMessages {
		if !dialogThemeObserverRefreshMessage(message) {
			t.Errorf("message %#x did not request a dialog theme refresh", message)
		}
	}
	for _, message := range []uint32{0, 0x000F, wmNCDestroy} {
		if dialogThemeObserverRefreshMessage(message) {
			t.Errorf("unrelated message %#x requested a dialog theme refresh", message)
		}
	}
}

func TestDialogThemeReconcileRequiredForAppearanceWithStableContrast(
	t *testing.T,
) {
	if !dialogThemeReconcileRequired(false, true, false) {
		t.Fatal("system appearance change was ignored when High Contrast was stable")
	}
	if !dialogThemeReconcileRequired(false, false, true) {
		t.Fatal("High Contrast change was ignored")
	}
	if dialogThemeReconcileRequired(false, false, false) {
		t.Fatal("unchanged policy requested a refresh")
	}
	if dialogThemeReconcileRequired(true, true, true) {
		t.Fatal("reentrant theme application requested another refresh")
	}
}

func TestBeginDialogThemeApplyIsNestedAndRestoresPriorState(t *testing.T) {
	const hwnd = uintptr(0x524C)
	state := &dialogThemeObserverState{}
	dialogThemeObserverMu.Lock()
	previous, hadPrevious := dialogThemeObserverStates[hwnd]
	dialogThemeObserverStates[hwnd] = state
	dialogThemeObserverMu.Unlock()
	t.Cleanup(func() {
		dialogThemeObserverMu.Lock()
		if hadPrevious {
			dialogThemeObserverStates[hwnd] = previous
		} else {
			delete(dialogThemeObserverStates, hwnd)
		}
		dialogThemeObserverMu.Unlock()
	})

	finishOuter := beginDialogThemeApply(hwnd)
	if !state.applying {
		t.Fatal("outer dialog theme application did not engage reentrancy guard")
	}
	finishInner := beginDialogThemeApply(hwnd)
	finishInner()
	if !state.applying {
		t.Fatal("nested application cleared its outer reentrancy guard")
	}
	finishOuter()
	if state.applying {
		t.Fatal("outer application did not restore the prior idle state")
	}

	state.applying = true
	finishAlreadyApplying := beginDialogThemeApply(hwnd)
	finishAlreadyApplying()
	if !state.applying {
		t.Fatal("application did not preserve an observer-owned guard")
	}
}

func TestCommitDialogThemeFrameRejectsInvalidWindow(t *testing.T) {
	if err := CommitDialogThemeFrame(0, false); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("zero HWND commit error = %v, want ErrInvalidWindow", err)
	}
}

func TestDialogThemeCommitKeepsClientAndCaptionPaintsOrdered(t *testing.T) {
	for _, synchronous := range []bool{false, true} {
		flags := dialogClientThemeRedrawFlags(synchronous)
		if flags&dialogRDWFrame != 0 || flags&dialogRDWNoFrame == 0 {
			t.Fatalf(
				"client redraw flags %#x can repaint non-client frame",
				flags,
			)
		}
		if got := flags&dialogRDWUpdateNow != 0; got != synchronous {
			t.Fatalf(
				"client redraw synchronous=%v UpdateNow=%v",
				synchronous,
				got,
			)
		}
	}

	captionFlags := dialogCaptionThemeRedrawFlags()
	if captionFlags&dialogRDWFrame == 0 ||
		captionFlags&dialogRDWNoFrame != 0 ||
		captionFlags&dialogRDWNoChildren == 0 ||
		captionFlags&dialogRDWUpdateNow == 0 {
		t.Fatalf("caption redraw flags do not isolate final frame: %#x", captionFlags)
	}
}
