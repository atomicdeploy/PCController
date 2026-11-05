//go:build windows

package winfx

import (
	"testing"
	"unsafe"
)

func TestFallbackDialogText(t *testing.T) {
	got := fallbackDialogText(TaskDialogOptions{
		Title:           "Title",
		MainInstruction: "Main",
		Content:         "Content",
		Details:         "Diagnostic detail",
	})
	want := "Main\r\n\r\nContent\r\n\r\nDetails:\r\nDiagnostic detail"
	if got != want {
		t.Fatalf("fallbackDialogText = %q, want %q", got, want)
	}
}

func TestFallbackButtonStyle(t *testing.T) {
	tests := []struct {
		buttons DialogButtons
		want    uint32
	}{
		{DialogButtonOK, mbOK},
		{DialogButtonOK | DialogButtonCancel, mbOKCancel},
		{DialogButtonYes | DialogButtonNo, mbYesNo},
		{DialogButtonYes | DialogButtonNo | DialogButtonCancel, mbYesNoCancel},
		{DialogButtonRetry | DialogButtonCancel, mbRetryCancel},
	}
	for _, test := range tests {
		if got := fallbackButtonStyle(test.buttons); got != test.want {
			t.Errorf("fallbackButtonStyle(%#x) = %#x, want %#x", test.buttons, got, test.want)
		}
	}
}

func TestTaskDialogConfigLayout(t *testing.T) {
	// TASKDIALOGCONFIG is 96 bytes on 32-bit and 176 bytes on 64-bit Windows.
	size := unsafe.Sizeof(taskDialogConfig{})
	if size != 96 && size != 176 {
		t.Fatalf("unexpected TASKDIALOGCONFIG size %d", size)
	}

	config := taskDialogConfig{}
	pointerSize := unsafe.Sizeof(uintptr(0))
	if pointerSize == 8 {
		if got := unsafe.Offsetof(config.Callback); got != 152 {
			t.Fatalf("64-bit callback offset = %d, want 152", got)
		}
		if got := unsafe.Offsetof(config.CallbackData); got != 160 {
			t.Fatalf("64-bit callback data offset = %d, want 160", got)
		}
		if got := unsafe.Offsetof(config.Width); got != 168 {
			t.Fatalf("64-bit width offset = %d, want 168", got)
		}
	} else {
		if got := unsafe.Offsetof(config.Callback); got != 84 {
			t.Fatalf("32-bit callback offset = %d, want 84", got)
		}
		if got := unsafe.Offsetof(config.CallbackData); got != 88 {
			t.Fatalf("32-bit callback data offset = %d, want 88", got)
		}
		if got := unsafe.Offsetof(config.Width); got != 92 {
			t.Fatalf("32-bit width offset = %d, want 92", got)
		}
	}
}

func TestConfigureTaskDialogTheme(t *testing.T) {
	if taskDialogThemeCallbackPointer == 0 {
		t.Fatal("task dialog theme callback pointer is zero")
	}

	config := taskDialogConfig{}
	configureTaskDialogTheme(&config, false)
	if config.Callback != taskDialogThemeCallbackPointer {
		t.Fatalf(
			"light callback = %#x, want %#x",
			config.Callback,
			taskDialogThemeCallbackPointer,
		)
	}
	if config.CallbackData != taskDialogThemeDataLight {
		t.Fatalf(
			"light callback data = %#x, want %#x",
			config.CallbackData,
			taskDialogThemeDataLight,
		)
	}

	configureTaskDialogTheme(&config, true)
	if config.Callback != taskDialogThemeCallbackPointer {
		t.Fatalf(
			"dark callback = %#x, want %#x",
			config.Callback,
			taskDialogThemeCallbackPointer,
		)
	}
	if config.CallbackData != taskDialogThemeDataDark {
		t.Fatalf(
			"dark callback data = %#x, want %#x",
			config.CallbackData,
			taskDialogThemeDataDark,
		)
	}
}

func TestTaskDialogThemeNotifications(t *testing.T) {
	for _, notification := range []uintptr{
		tdnCreated,
		tdnDialogConstructed,
	} {
		if !isTaskDialogThemeNotification(notification) {
			t.Errorf("notification %d was not themed", notification)
		}
	}
	for _, notification := range []uintptr{1, 2, 5, 8, 10} {
		if isTaskDialogThemeNotification(notification) {
			t.Errorf("notification %d was unexpectedly themed", notification)
		}
	}
}

func TestPreferredAppModeForTaskDialog(t *testing.T) {
	tests := []struct {
		name      string
		selection taskDialogThemeSelection
		want      uintptr
	}{
		{
			name: "system light or explicit light",
			want: taskDialogPreferredForceLight,
		},
		{
			name: "dark",
			selection: taskDialogThemeSelection{
				Dark: true,
			},
			want: taskDialogPreferredForceDark,
		},
		{
			name: "high contrast overrides dark",
			selection: taskDialogThemeSelection{
				Dark:         true,
				HighContrast: true,
			},
			want: taskDialogPreferredDefault,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := preferredAppModeForTaskDialog(
				test.selection,
			); got != test.want {
				t.Fatalf(
					"preferred mode = %d, want %d",
					got,
					test.want,
				)
			}
		})
	}
}
