//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/lxn/win"

	"rayanlamp.local/app/assets"
	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/winfx"
)

const (
	taskbarButtonToggle uint32 = 0x52A1
	taskbarButtonOn     uint32 = 0x52A2
	taskbarButtonOff    uint32 = 0x52A3
	taskbarButtonRetry  uint32 = 0x52A4

	taskbarThumbnailClicked = 0x1800
)

type taskbarPresentation struct {
	progress winfx.TaskbarProgressState
	current  uint32
	total    uint32
	tooltip  string
}

type taskbarButtonIcons struct {
	dpi       uint32
	toggle    win.HICON
	on        win.HICON
	off       win.HICON
	reconnect win.HICON
}

func (icons taskbarButtonIcons) dispose() {
	for _, icon := range []win.HICON{
		icons.toggle,
		icons.on,
		icons.off,
		icons.reconnect,
	} {
		if icon != 0 {
			win.DestroyIcon(icon)
		}
	}
}

func taskbarPresentationFor(
	snapshot model.Snapshot,
	suspended bool,
) taskbarPresentation {
	return taskbarPresentationForLocalizer(
		snapshot,
		suspended,
		i18n.New(i18n.English),
	)
}

func taskbarPresentationForLocalizer(
	snapshot model.Snapshot,
	suspended bool,
	localizer i18n.Localizer,
) taskbarPresentation {
	presentation := taskbarPresentation{
		progress: winfx.TaskbarProgressNone,
		tooltip:  localizer.Text(i18n.MsgAppName),
	}
	switch {
	case suspended:
		presentation.progress = winfx.TaskbarProgressPaused
		presentation.current = 100
		presentation.total = 100
		presentation.tooltip = localizer.Text(i18n.MsgTaskbarSuspended)
	case snapshot.PendingCommand != "":
		presentation.progress = winfx.TaskbarProgressIndeterminate
		presentation.tooltip = fmt.Sprintf(
			"%s — %s",
			localizer.Text(i18n.MsgAppName),
			pendingTaskbarTextForLocalizer(snapshot.PendingCommand, localizer),
		)
	case snapshot.LastError != "":
		presentation.progress = winfx.TaskbarProgressError
		presentation.current = 100
		presentation.total = 100
		if !snapshot.WebSocketOnline && !snapshot.HTTPReachable {
			presentation.tooltip = localizer.Text(i18n.MsgTaskbarDisconnected)
		} else {
			presentation.tooltip = localizer.Text(i18n.MsgTaskbarOperationFailed)
		}
	case snapshot.State == model.StateUnknown:
		presentation.progress = winfx.TaskbarProgressIndeterminate
		presentation.tooltip = fmt.Sprintf(
			"%s — %s",
			localizer.Text(i18n.MsgAppName),
			localizer.Text(i18n.MsgStateConnecting),
		)
	case snapshot.State == model.StateOn:
		presentation.tooltip = fmt.Sprintf(
			"%s — %s",
			localizer.Text(i18n.MsgAppName),
			localizer.Text(i18n.MsgStateOn),
		)
	case snapshot.State == model.StateOff:
		presentation.tooltip = fmt.Sprintf(
			"%s — %s",
			localizer.Text(i18n.MsgAppName),
			localizer.Text(i18n.MsgStateOff),
		)
	}
	return presentation
}

func pendingTaskbarText(command string) string {
	return pendingTaskbarTextForLocalizer(
		command,
		i18n.New(i18n.English),
	)
}

func pendingTaskbarTextForLocalizer(
	command string,
	localizer i18n.Localizer,
) string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "on":
		return localizer.Text(i18n.MsgTaskbarTurningOn)
	case "off":
		return localizer.Text(i18n.MsgTaskbarTurningOff)
	case "toggle":
		return localizer.Text(i18n.MsgTaskbarToggling)
	default:
		return localizer.Text(i18n.MsgTaskbarWorking)
	}
}

func taskbarButtonsFor(
	snapshot model.Snapshot,
	suspended bool,
	icons taskbarButtonIcons,
) []winfx.ThumbnailButton {
	return taskbarButtonsForLocalizer(
		snapshot,
		suspended,
		icons,
		i18n.New(i18n.English),
	)
}

func taskbarButtonsForLocalizer(
	snapshot model.Snapshot,
	suspended bool,
	icons taskbarButtonIcons,
	localizer i18n.Localizer,
) []winfx.ThumbnailButton {
	busy := suspended || snapshot.PendingCommand != ""
	return []winfx.ThumbnailButton{
		{
			ID:       taskbarButtonToggle,
			Icon:     uintptr(icons.toggle),
			Tooltip:  localizer.Text(i18n.MsgActionToggle),
			Disabled: busy || snapshot.State == model.StateUnknown,
		},
		{
			ID:       taskbarButtonOn,
			Icon:     uintptr(icons.on),
			Tooltip:  localizer.Text(i18n.MsgActionTurnOn),
			Disabled: busy || snapshot.State == model.StateOn,
		},
		{
			ID:       taskbarButtonOff,
			Icon:     uintptr(icons.off),
			Tooltip:  localizer.Text(i18n.MsgActionTurnOff),
			Disabled: busy || snapshot.State == model.StateOff,
		},
		{
			ID:       taskbarButtonRetry,
			Icon:     uintptr(icons.reconnect),
			Tooltip:  localizer.Text(i18n.MsgActionReconnect),
			Disabled: busy,
		},
	}
}

func taskbarCommandMethod(wParam uintptr) (string, bool) {
	if uint16((wParam>>16)&0xffff) != taskbarThumbnailClicked {
		return "", false
	}
	switch uint32(wParam & 0xffff) {
	case taskbarButtonToggle:
		return "lamp.toggle", true
	case taskbarButtonOn:
		return "lamp.on", true
	case taskbarButtonOff:
		return "lamp.off", true
	case taskbarButtonRetry:
		return "reconnect", true
	default:
		return "", false
	}
}

func (s *Shell) initializeTaskbarIntegration() error {
	if s == nil || s.mw == nil {
		return fmt.Errorf("taskbar: main window is unavailable")
	}
	taskbar, err := winfx.NewTaskbar(s.HWND())
	if err != nil {
		return err
	}
	s.taskbarIntegration = taskbar
	if err := s.rebuildTaskbarButtonIcons(); err != nil {
		taskbar.Close()
		s.taskbarIntegration = nil
		return err
	}
	return nil
}

func (s *Shell) rebuildTaskbarButtonIcons() error {
	if s == nil || s.mw == nil {
		return nil
	}
	dpi := uint32(win.GetDpiForWindow(s.mw.Handle()))
	if dpi < 96 {
		dpi = 96
	}
	if s.taskbarButtonIcons.dpi == dpi &&
		s.taskbarButtonIcons.toggle != 0 {
		return nil
	}
	width := int(win.GetSystemMetricsForDpi(win.SM_CXSMICON, dpi))
	height := int(win.GetSystemMetricsForDpi(win.SM_CYSMICON, dpi))
	if width < 1 {
		width = int(16 * dpi / 96)
	}
	if height < 1 {
		height = int(16 * dpi / 96)
	}

	next := taskbarButtonIcons{dpi: dpi}
	toggle, err := newProductIconHandle(width, height)
	if err != nil {
		return fmt.Errorf("taskbar: create product icon: %w", err)
	}
	next.toggle = toggle

	for _, item := range []struct {
		path   string
		target *win.HICON
	}{
		{"icons/lamp-on.ico", &next.on},
		{"icons/lamp-off.ico", &next.off},
		{"icons/menu-reconnect.ico", &next.reconnect},
	} {
		data, err := assets.Files.ReadFile(item.path)
		if err != nil {
			next.dispose()
			return fmt.Errorf("taskbar: read %s: %w", item.path, err)
		}
		icon, err := newPackedIconHandle(data, width, height)
		if err != nil {
			next.dispose()
			return fmt.Errorf("taskbar: create %s: %w", item.path, err)
		}
		*item.target = icon
	}

	if s.taskbarIntegration != nil {
		if err := s.taskbarIntegration.SetButtons(taskbarButtonsForLocalizer(
			s.snapshot,
			s.taskbarSuspended,
			next,
			s.localizerSnapshot(),
		)); err != nil {
			next.dispose()
			return err
		}
	}
	previous := s.taskbarButtonIcons
	s.taskbarButtonIcons = next
	previous.dispose()
	return nil
}

func (s *Shell) handleTaskbarButtonCreated() {
	if s == nil || s.taskbarIntegration == nil {
		return
	}
	if err := s.rebuildTaskbarButtonIcons(); err != nil {
		s.logf("taskbar thumbnail icons: %v", err)
		return
	}
	if err := s.taskbarIntegration.NotifyButtonCreated(taskbarButtonsForLocalizer(
		s.snapshot,
		s.taskbarSuspended,
		s.taskbarButtonIcons,
		s.localizerSnapshot(),
	)); err != nil {
		s.logf("taskbar thumbnail toolbar: %v", err)
		return
	}
	s.updateTaskbarIntegration()
}

func (s *Shell) updateTaskbarIntegration() {
	if s == nil || s.taskbarIntegration == nil {
		return
	}
	localizer := s.localizerSnapshot()
	presentation := taskbarPresentationForLocalizer(
		s.snapshot,
		s.taskbarSuspended,
		localizer,
	)
	if err := s.taskbarIntegration.SetButtons(taskbarButtonsForLocalizer(
		s.snapshot,
		s.taskbarSuspended,
		s.taskbarButtonIcons,
		localizer,
	)); err != nil {
		s.logf("update taskbar thumbnail buttons: %v", err)
	}
	if err := s.taskbarIntegration.SetThumbnailTooltip(
		presentation.tooltip,
	); err != nil {
		s.logf("update taskbar thumbnail tooltip: %v", err)
	}
	if err := s.taskbarIntegration.SetProgress(
		presentation.progress,
		presentation.current,
		presentation.total,
	); err != nil {
		s.logf("update taskbar progress: %v", err)
	}
}

func (s *Shell) closeTaskbarIntegration() {
	if s == nil {
		return
	}
	if s.taskbarIntegration != nil {
		s.taskbarIntegration.Close()
		s.taskbarIntegration = nil
	}
	s.taskbarButtonIcons.dispose()
	s.taskbarButtonIcons = taskbarButtonIcons{}
}
