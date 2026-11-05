//go:build windows

package ui

import (
	"testing"

	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/winfx"
)

func TestTaskbarPresentationMapping(t *testing.T) {
	tests := []struct {
		name      string
		snapshot  model.Snapshot
		suspended bool
		progress  winfx.TaskbarProgressState
		tooltip   string
	}{
		{
			name:      "suspend overrides pending and error",
			snapshot:  model.Snapshot{PendingCommand: "on", LastError: "old"},
			suspended: true,
			progress:  winfx.TaskbarProgressPaused,
			tooltip:   "PCController — Suspended",
		},
		{
			name:     "pending overrides old error",
			snapshot: model.Snapshot{PendingCommand: "off", LastError: "old"},
			progress: winfx.TaskbarProgressIndeterminate,
			tooltip:  "PCController — Turning off…",
		},
		{
			name:     "fully disconnected failure",
			snapshot: model.Snapshot{State: model.StateOn, LastError: "failed"},
			progress: winfx.TaskbarProgressError,
			tooltip:  "PCController — Disconnected",
		},
		{
			name: "connected operation failure",
			snapshot: model.Snapshot{
				State:         model.StateOn,
				LastError:     "failed",
				HTTPReachable: true,
			},
			progress: winfx.TaskbarProgressError,
			tooltip:  "PCController — Operation failed",
		},
		{
			name:     "initial connect",
			snapshot: model.Snapshot{State: model.StateUnknown},
			progress: winfx.TaskbarProgressIndeterminate,
			tooltip:  "PCController — Connecting…",
		},
		{
			name:     "on",
			snapshot: model.Snapshot{State: model.StateOn},
			progress: winfx.TaskbarProgressNone,
			tooltip:  "PCController — On",
		},
		{
			name:     "off",
			snapshot: model.Snapshot{State: model.StateOff},
			progress: winfx.TaskbarProgressNone,
			tooltip:  "PCController — Off",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := taskbarPresentationFor(test.snapshot, test.suspended)
			if got.progress != test.progress || got.tooltip != test.tooltip {
				t.Fatalf(
					"presentation = (%d, %q), want (%d, %q)",
					got.progress,
					got.tooltip,
					test.progress,
					test.tooltip,
				)
			}
			determinate := test.progress == winfx.TaskbarProgressError ||
				test.progress == winfx.TaskbarProgressPaused
			if determinate && (got.current != 100 || got.total != 100) {
				t.Fatalf("determinate value = %d/%d, want 100/100",
					got.current, got.total)
			}
		})
	}
}

func TestTaskbarPresentationAndButtonsUsePersianCopy(t *testing.T) {
	localizer := i18n.New(i18n.Persian)
	presentation := taskbarPresentationForLocalizer(
		model.Snapshot{State: model.StateOff},
		false,
		localizer,
	)
	if presentation.tooltip != "PCController — خاموش" {
		t.Fatalf("Persian tooltip = %q", presentation.tooltip)
	}
	buttons := taskbarButtonsForLocalizer(
		model.Snapshot{State: model.StateOff},
		false,
		taskbarButtonIcons{},
		localizer,
	)
	if len(buttons) != 4 ||
		buttons[0].Tooltip != "تغییر وضعیت دستگاه" ||
		buttons[1].Tooltip != "روشن کردن" ||
		buttons[2].Tooltip != "خاموش کردن" ||
		buttons[3].Tooltip != "اتصال دوباره" {
		t.Fatalf("Persian taskbar buttons = %#v", buttons)
	}
}

func TestTaskbarThumbnailCommandRouting(t *testing.T) {
	tests := []struct {
		id     uint32
		method string
	}{
		{taskbarButtonToggle, "device.toggle"},
		{taskbarButtonOn, "device.on"},
		{taskbarButtonOff, "device.off"},
		{taskbarButtonRetry, "reconnect"},
	}
	for _, test := range tests {
		wParam := uintptr(uint32(taskbarThumbnailClicked)<<16 | test.id)
		got, ok := taskbarCommandMethod(wParam)
		if !ok || got != test.method {
			t.Fatalf("button %#x = (%q, %v), want (%q, true)",
				test.id, got, ok, test.method)
		}
	}
	for _, wParam := range []uintptr{
		uintptr(taskbarButtonOn),
		uintptr(uint32(taskbarThumbnailClicked)<<16 | 0x7fff),
		uintptr(1<<16 | taskbarButtonOn),
	} {
		if method, ok := taskbarCommandMethod(wParam); ok || method != "" {
			t.Fatalf("unrelated WM_COMMAND %#x routed as %q", wParam, method)
		}
	}
}

func TestTaskbarButtonsReflectStateAndBusyPolicy(t *testing.T) {
	icons := taskbarButtonIcons{
		toggle:    win.HICON(1),
		on:        win.HICON(2),
		off:       win.HICON(3),
		reconnect: win.HICON(4),
	}
	on := taskbarButtonsFor(model.Snapshot{State: model.StateOn}, false, icons)
	if len(on) != 4 || !on[1].Disabled || on[2].Disabled ||
		on[0].Disabled || on[3].Disabled {
		t.Fatalf("on-state buttons = %#v", on)
	}
	unknown := taskbarButtonsFor(
		model.Snapshot{State: model.StateUnknown},
		false,
		icons,
	)
	if !unknown[0].Disabled || unknown[1].Disabled ||
		unknown[2].Disabled || unknown[3].Disabled {
		t.Fatalf("unknown-state buttons = %#v", unknown)
	}
	busy := taskbarButtonsFor(
		model.Snapshot{State: model.StateOff, PendingCommand: "on"},
		false,
		icons,
	)
	for _, button := range busy {
		if !button.Disabled {
			t.Fatalf("busy button %d remained enabled", button.ID)
		}
	}
}
