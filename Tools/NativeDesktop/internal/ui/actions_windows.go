//go:build windows

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"rayanlamp.local/app/assets"
	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/dialogs"
	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/traymenu"
	"rayanlamp.local/app/internal/winfx"
)

var appMenuArtworkPaths = []string{
	"icons/app.png",
	"icons/lamp-on.png",
	"icons/lamp-off.png",
	"icons/lamp-unknown.png",
	"icons/menu-window-show.png",
	"icons/menu-window-hide.png",
	"icons/menu-settings.png",
	"icons/menu-diagnostics.png",
	"icons/menu-reconnect.png",
	"icons/menu-pin.png",
	"icons/menu-startup.png",
	"icons/menu-exit.png",
}

var appMenuStandardDPIs = []int32{
	96,  // 100%
	120, // 125%
	144, // 150%
	168, // 175%
	192, // 200%
	216, // 225%
	240, // 250%
	288, // 300%
	384, // 400%
}

func (s *Shell) prewarmAppMenuArtwork() {
	if s == nil {
		return
	}
	s.menuArtworkPrewarm.Do(func() {
		currentDPI := int32(win.GetDpiForWindow(win.HWND(s.HWND())))
		targetSizes := appMenuPrewarmTargetSizes(currentDPI)
		go func() {
			iconPNGs := make([][]byte, 0, len(appMenuArtworkPaths))
			for _, path := range appMenuArtworkPaths {
				data, err := assets.Files.ReadFile(path)
				if err == nil {
					iconPNGs = append(iconPNGs, data)
				}
			}
			traymenu.PrewarmIconPNGs(iconPNGs, targetSizes)
		}()
	})
}

func appMenuPrewarmTargetSizes(currentDPI int32) []int32 {
	if currentDPI < 48 || currentDPI > 768 {
		currentDPI = 96
	}
	dpis := make([]int32, 0, len(appMenuStandardDPIs)+1)
	dpis = append(dpis, currentDPI)
	dpis = append(dpis, appMenuStandardDPIs...)

	sizes := make([]int32, 0, len(dpis))
	seen := make(map[int32]struct{}, len(dpis))
	for _, dpi := range dpis {
		size := scaleMenuArtwork(22, dpi)
		if _, exists := seen[size]; exists {
			continue
		}
		seen[size] = struct{}{}
		sizes = append(sizes, size)
	}
	return sizes
}

func scaleMenuArtwork(value, dpi int32) int32 {
	if dpi < 1 {
		dpi = 96
	}
	return int32((int64(value)*int64(dpi) + 48) / 96)
}

func (s *Shell) OpenSettings() {
	if s.controller != nil {
		s.activateControllerAction(actionSettings)
		return
	}
	if s == nil || s.mw == nil || s.quitting.Load() || s.closed.Load() {
		return
	}
	s.mw.Synchronize(func() {
		if s.quitting.Load() || s.closed.Load() {
			return
		}
		s.resetDragSnap()
		if s.settings != nil && !s.settings.IsDisposed() {
			if err := s.settings.Activate(); err != nil {
				s.showError(s.tr(i18n.MsgErrorFocusSettings), err)
			}
			s.reapplyComposition()
			return
		}
		// Establish the preview baseline at the live window position. Appearance
		// previews may resize the HWND, but must never replay pre-dialog X/Y.
		s.saveWindowBounds()

		var settingsWindow *dialogs.SettingsWindow
		window, err := dialogs.ShowSettingsModeless(
			s.mw,
			s.Config(),
			dialogs.SettingsActions{
				Preview: s.ApplyConfig,
				Save: func(updated config.Config) error {
					return s.persistSettings(updated)
				},
				Accepted: func(updated config.Config) {
					s.logf(
						"settings saved with %s title bar",
						updated.UI.TitleBarMode,
					)
				},
				PersistSelectedTab:  s.persistSettingsTab,
				PersistWindowBounds: s.persistSettingsBounds,
				Closed: func() {
					s.resetDragSnap()
					s.saveWindowBounds()
					if !s.quitting.Load() && !s.closed.Load() {
						s.reapplyComposition()
					}
					if s.settings == settingsWindow {
						s.settings = nil
					}
				},
				Icon:                 s.settingsIcon,
				HotkeyCaptureChanged: s.setHotkeyCapture,
				TestToast: func() error {
					if s.notifier == nil {
						return fmt.Errorf("notification service is unavailable")
					}
					return s.notifier.ShowTest(
						s.tr(i18n.MsgNotificationTestTitle),
						s.tr(i18n.MsgNotificationTestBody),
						false,
					)
				},
				TestSound: func() error {
					return s.QueueAudio("message", 1)
				},
				ImportSound: func(path string) (string, string, error) {
					imported, importErr := s.ImportAudioFile(path)
					return imported.ID, imported.Name, importErr
				},
				RegistrationStatus: s.settingsRegistrationStatus,
				RegistrationAction: s.settingsRegistrationAction,
			},
		)
		if err != nil {
			s.showError(s.tr(i18n.MsgErrorOpenSettings), err)
			return
		}
		settingsWindow = window
		s.settings = window
	})
}

func (s *Shell) settingsRegistrationStatus() (commands.RegistrationStatus, error) {
	if s == nil || s.dispatcher == nil {
		return commands.RegistrationStatus{}, fmt.Errorf(
			"registration dispatcher is unavailable",
		)
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID: fmt.Sprintf(
			"settings-registration-status-%d",
			time.Now().UnixNano(),
		),
		Method: commands.MethodIntegrationRegistrationStatus,
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	result, err := s.dispatcher.Handler(ctx, request)
	if err != nil {
		return commands.RegistrationStatus{}, err
	}
	status, ok := result.(commands.RegistrationStatus)
	if !ok {
		return commands.RegistrationStatus{}, fmt.Errorf(
			"registration status returned %T",
			result,
		)
	}
	return status, nil
}

func (s *Shell) settingsRegistrationAction(method string) error {
	if s == nil || s.dispatcher == nil {
		return fmt.Errorf("registration dispatcher is unavailable")
	}
	switch method {
	case commands.MethodIntegrationShellRegister,
		commands.MethodIntegrationShellUnregister,
		commands.MethodIntegrationCOMRegister,
		commands.MethodIntegrationCOMUnregister:
	default:
		return fmt.Errorf("unsupported registration action %q", method)
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID: fmt.Sprintf(
			"settings-registration-action-%d",
			time.Now().UnixNano(),
		),
		Method: method,
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	_, err := s.dispatcher.Handler(ctx, request)
	return err
}

func (s *Shell) persistSettings(updated config.Config) error {
	updated = s.applyRememberedWindowSession(updated)
	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID:              fmt.Sprintf("settings-%d", time.Now().UnixNano()),
		Method:          "config.patch",
		Params:          data,
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	_, err = s.dispatcher.Handler(ctx, request)
	return err
}

func (s *Shell) persistSettingsTab(index int) {
	index = max(0, min(index, config.SettingsTabCount-1))
	// Closing or canceling Settings first rolls every live appearance preview
	// back. Retain the selected tab in the Shell immediately so an immediate
	// reopen cannot observe the old page while the durable patch is in flight.
	s.cfgMu.Lock()
	s.cfg.UI.SettingsTab = index
	s.cfgMu.Unlock()

	patch := struct {
		UI struct {
			SettingsTab int `json:"settings_tab"`
		} `json:"ui"`
	}{}
	patch.UI.SettingsTab = index
	data, err := json.Marshal(patch)
	if err != nil {
		s.logf("encode settings tab preference: %v", err)
		return
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID:              fmt.Sprintf("settings-tab-%d", time.Now().UnixNano()),
		Method:          "config.patch",
		Params:          data,
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if _, applyErr := s.dispatcher.Handler(ctx, request); applyErr != nil {
			s.logf("save settings tab preference: %v", applyErr)
		}
	}()
}

func (s *Shell) persistSettingsBounds(bounds config.WindowBounds) {
	bounds.Width = max(
		config.SettingsWindowMinWidth,
		min(bounds.Width, config.SettingsWindowMaxWidth),
	)
	bounds.Height = max(
		config.SettingsWindowMinHeight,
		min(bounds.Height, config.SettingsWindowMaxHeight),
	)
	// Update the in-memory source immediately so closing and reopening Settings
	// cannot race the durable patch and briefly fall back to its default size.
	s.cfgMu.Lock()
	s.cfg.UI.SettingsBounds = bounds
	s.cfgMu.Unlock()

	patch := struct {
		UI struct {
			SettingsBounds config.WindowBounds `json:"settings_bounds"`
		} `json:"ui"`
	}{}
	patch.UI.SettingsBounds = bounds
	data, err := json.Marshal(patch)
	if err != nil {
		s.logf("encode settings window bounds: %v", err)
		return
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID: fmt.Sprintf(
			"settings-bounds-%d",
			time.Now().UnixNano(),
		),
		Method: "config.patch",
		Params: data,
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if _, applyErr := s.dispatcher.Handler(ctx, request); applyErr != nil {
			s.logf("save settings window bounds: %v", applyErr)
		}
	}()
}

func (s *Shell) OpenDiagnostics() {
	if s.controller != nil {
		s.openControllerPage("events")
		return
	}
	if s == nil || s.mw == nil || s.quitting.Load() || s.closed.Load() {
		return
	}
	s.mw.Synchronize(func() {
		if s.quitting.Load() || s.closed.Load() {
			return
		}
		s.resetDragSnap()
		err := dialogs.ShowDiagnostics(
			s.mw,
			s.Config(),
			s.snapshot,
			s.core,
			func(dialog *walk.Dialog, syncTheme func(config.Config)) {
				s.diagnostics = dialog
				s.diagnosticsSyncTheme = syncTheme
			},
			s.renderingSummary(),
			s.diagnosticsIcon,
		)
		s.resetDragSnap()
		if !s.quitting.Load() && !s.closed.Load() {
			s.reapplyComposition()
		}
		if err != nil {
			s.showError(s.tr(i18n.MsgErrorOpenDiagnostics), err)
		}
	})
}

// OpenWebUI launches the embedded SPA at the same configured loopback
// authority used by the local API. No bearer token is placed in the URL; the
// page establishes its own short-lived, same-origin browser session.
func (s *Shell) OpenWebUI() {
	if s.controller != nil {
		s.openControllerPage("")
		return
	}
	if s == nil || s.quitting.Load() || s.closed.Load() {
		return
	}
	cfg := s.Config()
	if !cfg.Integration.LocalAPIEnabled {
		s.showError(
			s.tr(i18n.MsgErrorWebUnavailable),
			fmt.Errorf("%s", s.localizerSnapshot().Choose(
				"Enable the loopback API in Integration settings first.",
				"ابتدا API محلی را در تنظیمات یکپارچه‌سازی فعال کنید.",
			)),
		)
		return
	}
	url := "http://" + cfg.Integration.LocalAPIListen + "/app/"
	if err := winfx.OpenURL(url); err != nil {
		s.showError(s.tr(i18n.MsgErrorOpenWeb), err)
	}
}

func (s *Shell) showAppMenuAt(x, y int32) {
	if s.controller != nil {
		s.showControllerMenu(x, y)
		return
	}
	cfg := s.Config()
	stateIcon := "icons/lamp-unknown.png"
	stateLabel := s.tr(i18n.MsgTrayStateUnknown)
	switch s.snapshot.State {
	case model.StateOn:
		stateIcon, stateLabel = "icons/lamp-on.png", s.tr(i18n.MsgMainLightOn)
	case model.StateOff:
		stateIcon, stateLabel = "icons/lamp-off.png", s.tr(i18n.MsgMainLightOff)
	}

	item := func(id, label, shortcut, iconPath string) traymenu.Item {
		iconData, _ := assets.Files.ReadFile(iconPath)
		return traymenu.Item{
			ID:       traymenu.ActionID(id),
			Label:    label,
			Shortcut: shortcut,
			IconPNG:  iconData,
		}
	}
	viewLabel := s.tr(i18n.MsgActionDashboardView)
	nextView := config.ViewDashboard
	if cfg.UI.ViewMode == config.ViewDashboard {
		viewLabel = s.tr(i18n.MsgActionCompactView)
		nextView = config.ViewCompact
	}
	visibilityID, visibilityLabel, visibilityShortcut, visibilityIcon :=
		s.mainWindowVisibilityMenuAction(
			s.mw != nil &&
				win.IsWindowVisible(s.mw.Handle()) &&
				!win.IsIconic(s.mw.Handle()),
		)
	stateItem := item("lamp_state", stateLabel, "", stateIcon)
	stateItem.Disabled = true
	items := []traymenu.Item{
		stateItem,
		{Separator: true},
		item("toggle", s.tr(i18n.MsgActionToggle), cfg.Hotkeys.Toggle, "icons/app.png"),
		item("on", s.tr(i18n.MsgActionTurnOn), cfg.Hotkeys.On, "icons/lamp-on.png"),
		item("off", s.tr(i18n.MsgActionTurnOff), cfg.Hotkeys.Off, "icons/lamp-off.png"),
		{Separator: true},
		item("view_mode", viewLabel, "", "icons/app.png"),
		item("settings", s.tr(i18n.MsgActionSettings)+"…", "", "icons/menu-settings.png"),
		item("diagnostics", s.tr(i18n.MsgActionDiagnostics)+"…", "", "icons/menu-diagnostics.png"),
		item("web", s.tr(i18n.MsgActionWebDashboard), "", "icons/app.png"),
		item("reconnect", s.tr(i18n.MsgActionReconnect), "F5", "icons/menu-reconnect.png"),
		{Separator: true},
		item("always_on_top", s.tr(i18n.MsgActionAlwaysOnTop), "", "icons/menu-pin.png"),
		item("startup", s.tr(i18n.MsgActionStartWithWindows), "", "icons/menu-startup.png"),
		{Separator: true},
		item(
			visibilityID,
			visibilityLabel,
			visibilityShortcut,
			visibilityIcon,
		),
		item("quit", s.tr(i18n.MsgActionQuit), "", "icons/menu-exit.png"),
	}
	for index := range items {
		switch items[index].ID {
		case "always_on_top":
			items[index].Checked = cfg.UI.AlwaysOnTop
		case "startup":
			items[index].Checked = cfg.Integration.StartWithWindows
		}
	}

	theme := traymenu.ThemeSystem
	switch strings.ToLower(cfg.UI.Theme) {
	case "dark":
		theme = traymenu.ThemeDark
	case "light":
		theme = traymenu.ThemeLight
	}
	backdrop := traymenu.BackdropAuto
	switch strings.ToLower(cfg.UI.Backdrop) {
	case "acrylic":
		backdrop = traymenu.BackdropAcrylic
	case "blur":
		backdrop = traymenu.BackdropBlur
	case "none", "solid":
		backdrop = traymenu.BackdropSolid
	}

	result, err := traymenu.ShowAtAnchor(traymenu.Options{
		Owner:              s.HWND(),
		Items:              items,
		Theme:              theme,
		Backdrop:           backdrop,
		Alignment:          traymenu.AlignAuto,
		RightToLeft:        s.isRTL(),
		FontFamily:         s.preferredFontFamily(),
		DisableWindowReuse: !cfg.UI.PrewarmTrayMenu,
		DisableAnimations:  !cfg.UI.Animations,
	}, traymenu.Rect{
		Left: x, Top: y, Right: x, Bottom: y,
	})
	if err != nil {
		s.logf("styled app menu: %v", err)
		s.showError(s.tr(i18n.MsgErrorOpenMenu), err)
		return
	}
	if result.UsedNativeFallback {
		s.logf("app menu used intentional native renderer: %s", result.NativeReason)
	}
	if result.Cancelled {
		return
	}
	switch result.Action {
	case "show":
		s.Show()
	case "hide":
		s.Hide()
	case "toggle":
		s.invoke("lamp.toggle", nil)
	case "on":
		s.invoke("lamp.on", nil)
	case "off":
		s.invoke("lamp.off", nil)
	case "view_mode":
		s.patchConfig(map[string]any{
			"ui": map[string]any{"view_mode": nextView},
		})
	case "settings":
		s.OpenSettings()
	case "diagnostics":
		s.OpenDiagnostics()
	case "web":
		s.OpenWebUI()
	case "reconnect":
		s.invoke("reconnect", nil)
	case "always_on_top":
		s.patchConfig(map[string]any{
			"ui": map[string]any{"always_on_top": !cfg.UI.AlwaysOnTop},
		})
	case "startup":
		s.patchConfig(map[string]any{
			"integration": map[string]any{"start_with_windows": !cfg.Integration.StartWithWindows},
		})
	case "quit":
		s.Quit()
	}
}

func mainWindowVisibilityMenuAction(
	visible bool,
) (id, label, shortcut, iconPath string) {
	if visible {
		return "hide", "Hide main window", "Hide", "icons/menu-window-hide.png"
	}
	return "show", "Show main window", "Open", "icons/menu-window-show.png"
}

func (s *Shell) mainWindowVisibilityMenuAction(
	visible bool,
) (id, label, shortcut, iconPath string) {
	id, _, shortcut, iconPath = mainWindowVisibilityMenuAction(visible)
	if visible {
		return id, s.tr(i18n.MsgActionHideWindow), shortcut, iconPath
	}
	return id, s.tr(i18n.MsgActionShowWindow), shortcut, iconPath
}

func (s *Shell) patchConfig(patch map[string]any) {
	data, err := json.Marshal(patch)
	if err != nil {
		s.showError(s.tr(i18n.MsgErrorUpdateSettings), err)
		return
	}
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID:              fmt.Sprintf("tray-config-%d", time.Now().UnixNano()),
		Method:          "config.patch",
		Params:          data,
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if _, updateErr := s.dispatcher.Handler(ctx, request); updateErr != nil {
			s.showError(s.tr(i18n.MsgErrorUpdateSettings), updateErr)
		}
	}()
}
