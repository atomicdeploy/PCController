//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/lxn/walk"
	"rayanlamp.local/app/assets"
	"rayanlamp.local/app/internal/appcore"
	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/design"
	"rayanlamp.local/app/internal/traymenu"
	"rayanlamp.local/app/internal/winfx"
	"rayanlamp.local/app/presentation"
)

type controllerButton struct {
	rect            walk.Rectangle
	action          actionID
	enabled, active bool
	tint            walk.Color
}

// PCController WebUI tokens (styles.css); donor themes remain intact for its
// retained standalone build. Controller mode uses the same semantic palette.
func controllerPalette(dark bool) palette {
	if !dark {
		p := lightPalette()
		p.backgroundTop = nativeWalkColor("#f5f3f6")
		p.backgroundBottom = nativeWalkColor("#ebe8ed")
		p.panel = nativeWalkColor("#ffffff")
		p.panelHover = nativeWalkColor("#ded8e5")
		p.text = nativeWalkColor("#242126")
		p.secondary = nativeWalkColor("#7b7382")
		p.accent = nativeWalkColor("#6845bf")
		p.accentHover = nativeWalkColor("#5334a3")
		p.success = nativeWalkColor("#307752")
		p.danger = nativeWalkColor("#b33f53")
		p.warning = nativeWalkColor("#8a5917")
		p.primaryText = nativeWalkColor("#ffffff")
		return p
	}
	p := darkPalette()
	p.backgroundTop = nativeWalkColor("#121116")
	p.backgroundBottom = nativeWalkColor("#0b0a0e")
	p.panel = nativeWalkColor("#1d1b22")
	p.panelHover = nativeWalkColor("#33303b")
	p.text = nativeWalkColor("#f5f2f7")
	p.secondary = nativeWalkColor("#938c9b")
	p.accent = nativeWalkColor("#a78bfa")
	p.accentHover = nativeWalkColor("#d8ccff")
	p.success = nativeWalkColor("#73c69a")
	p.danger = nativeWalkColor("#ed7889")
	p.warning = nativeWalkColor("#e7ae63")
	p.primaryText = nativeWalkColor("#121116")
	return p
}

// RunController reuses the donor's actual shell, compositors, window lifecycle,
// artwork scaling, theme policy, input activation and styled tray implementation.
// The dormant donor Core supplies appearance configuration only: never Start it.
func RunController(ctx context.Context, options presentation.Options) error {
	if options.Execute == nil || options.Updates == nil || options.ConfigPath == "" {
		return errors.New("controller desktop requires an RPC bridge, observations and a configuration path")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Set before creating any windows/workers. The retained donor executable
	// keeps its own defaults; the embedded host shares PCController's palette.
	configureControllerTokens()
	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(options.ConfigPath); errors.Is(err, os.ErrNotExist) {
		cfg.UI.ViewMode = config.ViewDashboard
		cfg.UI.WindowBounds = config.WindowBounds{X: -1, Y: -1, Width: 900, Height: 640}
		cfg.UI.TitleBarMode = config.TitleBarCustom
		cfg.UI.Backdrop = config.BackdropAcrylic
		cfg.UI.MagneticSnap = true
	}
	cfg.Hotkeys = config.HotkeysConfig{}
	cfg.Automation.Enabled = false
	cfg.Notifications.Enabled, cfg.Notifications.Sound = false, false
	cfg.Integration.LocalAPIEnabled = false
	cfg.Integration.StartWithWindows = false
	// No lamp route may ever be reached, including during a system resume.
	cfg.Lamp.BaseURL = "http://127.0.0.1:1"
	// Window-position persistence reads the on-disk configuration. Persist the
	// adapted mode too, so it cannot resurrect donor lamp routes or chrome.
	if err := config.Save(options.ConfigPath, cfg); err != nil {
		return err
	}
	core, err := appcore.New(cfg)
	if err != nil {
		return err
	}
	defer core.Stop()
	dispatcher, err := commands.New(core, options.ConfigPath)
	if err != nil {
		return err
	}
	shell, err := newShell(core, dispatcher, options.ConfigPath, options.Log, &options)
	if err != nil {
		return err
	}
	defer shell.Close()
	dispatcher.SetHooks(shell.Hooks())
	// Restore saved standalone widgets even before the first host observation.
	// Channel widgets are restored when their catalog entries arrive.
	shell.mw.Starting().Attach(func() { shell.syncControllerWidgets() })
	go func() {
		for {
			select {
			case <-ctx.Done():
				shell.Quit()
				return
			case <-shell.ctx.Done():
				return
			case state, ok := <-options.Updates:
				if !ok {
					return
				}
				shell.mw.Synchronize(func() {
					if shell.closed.Load() {
						return
					}
					shell.controllerState = state
					shell.observeControllerTelemetry(state)
					shell.syncControllerWidgets()
					shell.updateControllerSegments(state)
					minimum := 680
					if state.HavePower || state.HaveTemperature {
						minimum += 84
					}
					if state.LCDLine1 != "" || state.LCDLine2 != "" {
						minimum += 78
					}
					minWidth := 560
					if shell.Config().UI.ViewMode == config.ViewCompact {
						minWidth, minimum = 340, 330
					}
					if shell.controllerMinimumHeight != minimum {
						shell.controllerMinimumHeight = minimum
						_ = shell.mw.SetMinMaxSizePixels(walk.Size{Width: minWidth, Height: minimum}, walk.Size{Width: 1400, Height: 1400})
					}
					shell.updateControllerTray()
					_ = shell.surface.Invalidate()
				})
			}
		}
	}()
	if options.PreviewPage != "" {
		shell.mw.Starting().Attach(func() {
			switch options.PreviewPage {
			case "outputs":
				shell.controllerChannels = true
			case "preferences":
				shell.controllerPreferences = true
			case "menu":
				shell.mw.Synchronize(func() { b := shell.mw.BoundsPixels(); shell.showControllerMenu(int32(b.X+36), int32(b.Y+115)) })
			default:
				shell.mw.Synchronize(func() { shell.openControllerForm(options.PreviewPage) })
			}
		})
	}
	if code := shell.Run(StartupForceShow); code != 0 {
		return fmt.Errorf("native desktop exited with code %d", code)
	}
	return nil
}

func controllerKeyboardAction(key walk.Key) actionID {
	switch key {
	case walk.Key1:
		return actionKey1
	case walk.Key2:
		return actionKey2
	case walk.Key3:
		return actionKey3
	case walk.Key4:
		return actionKey4
	case walk.KeyF2:
		return actionSettings
	case walk.KeyF4:
		return actionConsole
	case walk.KeyF5:
		return actionReconnect
	}
	return actionNone
}

func (s *Shell) controllerActionHint(action actionID) string {
	switch action {
	case actionConnection:
		return "Board connection"
	case actionFirmware:
		return "Firmware and backups"
	case actionEvents:
		return "Live event history"
	case actionSettings:
		return "Preferences · F2"
	case actionConsole:
		return "Show or hide Console / TUI · F4"
	case actionKey1:
		return "Previous · 1"
	case actionKey2:
		return "Next · 2"
	case actionKey3:
		return "Decrease · 3"
	case actionKey4:
		return "Increase · 4"
	case actionChannelPrevious:
		return "Previous channels"
	case actionChannelNext:
		return "Next channels"
	case actionChannelBack:
		return "Back to dashboard"
	}
	if strings.HasPrefix(s.controllerActionIDs[action], "edit:") {
		return "Rename channel or change its icon"
	}
	if s.controllerActionIDs[action] == "inline-save:" {
		return "Save · Enter"
	}
	if s.controllerActionIDs[action] == "inline-cancel:" {
		return "Cancel · Escape"
	}
	return ""
}

func (s *Shell) activateControllerAction(action actionID) bool {
	if action >= actionChannelFirst {
		id, ok := s.controllerActionIDs[action]
		if strings.HasPrefix(id, "edit:") {
			s.beginControllerInline(strings.TrimPrefix(id, "edit:"))
			return true
		}
		if id == "inline-save:" {
			s.saveControllerInline()
			return true
		}
		if id == "inline-cancel:" {
			if !s.controllerBusy {
				s.closeControllerInline()
			}
			return true
		}
		if strings.HasPrefix(id, "pref:") {
			s.activateControllerPreference(strings.TrimPrefix(id, "pref:"))
			return true
		}
		if ok && s.controller.InvokeAction != nil && s.controllerState.Connected && !s.controllerState.Busy && !s.controllerBusy {
			s.runControllerOperation(func(ctx context.Context) error { return s.controller.InvokeAction(ctx, id) })
		}
		return true
	}
	if s.controllerInline != nil && (action == actionSettings || action == actionChannels || action == actionChannelBack || action == actionChannelPrevious || action == actionChannelNext) {
		s.controllerInline.input.SetFocus()
		return true
	}
	if (action == actionSettings || action == actionChannels) && s.controllerWidgetID == "" && s.Config().UI.ViewMode == config.ViewCompact {
		s.activateControllerPreference("compact")
		if s.Config().UI.ViewMode == config.ViewCompact {
			return true // Saving the expanded view failed; retain compact layout.
		}
	}
	switch action {
	case actionSettings:
		s.controllerPreferences = true
		s.controllerChannels = false
		s.cancelButtonActivation()
		s.surface.Invalidate()
	case actionKey1, actionKey2, actionKey3, actionKey4:
		if s.controllerState.Connected && s.controllerState.CanNavigate && !s.controllerState.Busy && !s.controllerBusy {
			s.invokeController([]string{"menu prev", "menu next", "menu dec", "menu inc"}[int(action-actionKey1)])
		}
	case actionChannels:
		s.controllerPreferences = false
		s.controllerChannels = true
		s.controllerPage = 0
		s.cancelButtonActivation()
		_ = s.surface.Invalidate()
	case actionChannelBack:
		s.controllerPreferences = false
		s.controllerChannels = false
		s.cancelButtonActivation()
		_ = s.surface.Invalidate()
	case actionChannelPrevious:
		s.controllerPage = max(0, s.controllerPage-1)
		s.cancelButtonActivation()
		_ = s.surface.Invalidate()
	case actionChannelNext:
		s.controllerPage++
		s.cancelButtonActivation()
		_ = s.surface.Invalidate()
	case actionRF:
		s.openControllerForm("rf")
	case actionPrograms:
		s.openControllerForm("programs")
	case actionBuzzer:
		s.openControllerForm("buzzer")
	case actionStrip:
		s.openControllerForm("strip")
	case actionDisplay:
		s.openControllerForm("display")
	case actionConnection:
		s.openControllerForm("connection")
	case actionFirmware:
		s.openControllerForm("firmware")
	case actionEvents:
		s.openControllerForm("events")
	case actionConsole:
		if s.controller.ShowConsole != nil {
			s.runControllerOperation(func(context.Context) error { return s.controller.ShowConsole() })
		}
	case actionReconnect:
		s.invokeController("reconnect")
	case actionToggle, actionOn, actionOff: // Never reinterpret donor lamp actions as outputs.
	default:
		return false
	}
	return true
}

func (s *Shell) invokeController(command string) {
	if command == "lamp.on" || command == "lamp.off" || command == "lamp.toggle" {
		return
	}
	s.runControllerOperation(func(ctx context.Context) error { return s.controller.Execute(ctx, command) })
}

func (s *Shell) runControllerOperation(operation func(context.Context) error) {
	s.runControllerOperationThen(operation, nil)
}

func (s *Shell) runControllerOperationThen(operation func(context.Context) error, finished func(error)) {
	if s.controllerOwner != nil {
		s.controllerOwner.runControllerOperationThen(operation, finished)
		return
	}
	if s.controllerState.Busy || s.controllerBusy {
		return
	}
	s.controllerBusy = true
	s.syncControllerWidgets()
	s.controllerState.Error = ""
	_ = s.surface.Invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		err := operation(ctx)
		s.mw.Synchronize(func() {
			if s.closed.Load() {
				return
			}
			s.controllerBusy = false
			s.controllerPendingPWM = controllerChannelGesture{}
			if err != nil {
				s.controllerState.Error = err.Error()
			}
			if finished != nil {
				finished(err)
			}
			s.syncControllerWidgets()
			_ = s.surface.Invalidate()
		})
	}()
}

func (s *Shell) openControllerPage(page string) {
	if s.controller.OpenPage != nil {
		if err := s.controller.OpenPage(page); err != nil {
			s.showError("PCController", err)
		}
	}
}

func (s *Shell) updateControllerTray() {
	if s.tray == nil {
		return
	}
	status := "Disconnected"
	if s.controllerState.Connected {
		status = "Connected"
	}
	if s.controllerTrayStatus == status {
		return
	}
	s.controllerTrayStatus = status
	_ = s.tray.SetIcon(s.appIcon)
	_ = s.tray.SetToolTip(s.controller.Title + " · " + status)
}

func (s *Shell) drawControllerDigit(canvas *walk.Canvas, rect walk.Rectangle, digit int, bits, brightness byte, colors palette) {
	w, h := rect.Width, rect.Height
	t := max(5, w/7)
	segments := []walk.Rectangle{
		{X: t, Y: 0, Width: w - 2*t, Height: t}, {X: w - t, Y: t, Width: t, Height: h/2 - t},
		{X: w - t, Y: h/2 + t/2, Width: t, Height: h/2 - t}, {X: t, Y: h - t, Width: w - 2*t, Height: t},
		{X: 0, Y: h/2 + t/2, Width: t, Height: h/2 - t}, {X: 0, Y: t, Width: t, Height: h/2 - t},
		{X: t, Y: h/2 - t/2, Width: w - 2*t, Height: t}, {X: w + 2, Y: h - t, Width: t, Height: t},
	}
	for i, segment := range segments {
		// LED emission is a hardware visual, independent of the application accent.
		color := walk.RGB(44, 23, 28)
		lit := blend(walk.RGB(110, 25, 37), walk.RGB(255, 81, 96), 0.4+float64(brightness&7)/12)
		level := s.controllerSegmentLevels[digit][i]
		if !s.controllerSegmentsReady {
			level = 0
			if bits&(1<<uint(i)) != 0 {
				level = 1
			}
		}
		color = blend(color, lit, level)
		brush, err := walk.NewSolidColorBrush(color)
		if err != nil {
			continue
		}
		segment.X += rect.X
		segment.Y += rect.Y
		_ = canvas.FillRoundedRectanglePixels(brush, segment, walk.Size{Width: t / 2, Height: t / 2})
		brush.Dispose()
	}
}

func (s *Shell) controllerMenuItems() []traymenu.Item {
	if s.controllerOwner != nil {
		return s.controllerOwner.controllerMenuItems()
	}
	consoleLabel := "Show Console / TUI"
	if s.controller.ConsoleVisible != nil && s.controller.ConsoleVisible() {
		consoleLabel = "Hide Console / TUI"
	}
	items := []traymenu.Item{
		{ID: "show", Label: "Show PCController"}, {ID: "hide", Label: "Minimize to tray"},
		{Separator: true}, {ID: "console", Label: consoleLabel, Disabled: s.controller.ShowConsole == nil}, {ID: "control", Label: "Hardware channels"},
		{ID: "rf", Label: "RF assignments"}, {ID: "programs", Label: "Programs"}, {ID: "firmware", Label: "Firmware"},
		{ID: "events", Label: "Events"}, {ID: "settings", Label: "Preferences"},
		{Separator: true}, {ID: "appearance", Label: "Window appearance…"},
		{ID: "compact", Label: "Compact front panel", Checked: s.Config().UI.ViewMode == config.ViewCompact},
		{ID: "widgets", Label: "Desktop widgets…"},
		{Separator: true}, {ID: "quit", Label: "Close desktop"},
	}
	// The old host tray is authoritative; both custom and native renderers
	// receive this exact list, with desktop-only presentation actions appended.
	var hostItems []traymenu.Item
	for i, item := range s.controllerState.HostMenu {
		id := "host:" + item.ID
		if item.ID == "" {
			id = fmt.Sprintf("host-status:%d", i)
		}
		hostItems = append(hostItems, traymenu.Item{ID: traymenu.ActionID(id), Label: item.Label, Disabled: item.Disabled || s.controller.InvokeHostMenu == nil, Separator: item.Separator})
	}
	if len(hostItems) > 0 {
		items = append(append(hostItems, traymenu.Item{Separator: true}), items...)
	}
	icons := map[traymenu.ActionID]string{"show": "window-show", "hide": "window-hide", "console": "diagnostics", "control": "settings", "rf": "reconnect", "programs": "startup", "firmware": "startup", "events": "diagnostics", "settings": "settings", "appearance": "settings", "reconnect": "reconnect", "quit": "exit"}
	for index := range items {
		if strings.HasPrefix(string(items[index].ID), "host:") {
			name := "settings"
			switch items[index].ID {
			case "host:exit":
				name = "exit"
			case "host:reconnect":
				name = "reconnect"
			case "host:dashboard":
				name = "window-show"
			case "host:updates", "host:workbench":
				name = "startup"
			}
			icons[items[index].ID] = name
		}
		if icon := icons[items[index].ID]; icon != "" {
			items[index].IconPNG, _ = assets.Files.ReadFile("icons/menu-" + icon + ".png")
		}
	}
	return items
}

func (s *Shell) showControllerMenu(x, y int32) {
	if s.controllerOwner != nil {
		s.controllerOwner.showControllerMenu(x, y)
		return
	}
	items := s.controllerMenuItems()
	result, err := traymenu.ShowAtAnchor(s.controllerMenuOptions(items), traymenu.Rect{Left: x, Top: y, Right: x, Bottom: y})
	s.logControllerMenuMaterial(result, err)
	if err != nil {
		s.showError("Menu", err)
		return
	}
	if result.Cancelled {
		return
	}
	if strings.HasPrefix(string(result.Action), "host:") && s.controller.InvokeHostMenu != nil {
		id := strings.TrimPrefix(string(result.Action), "host:")
		s.runControllerOperationThen(func(ctx context.Context) error { return s.controller.InvokeHostMenu(ctx, id) }, func(err error) {
			if err == nil && id == "exit" {
				s.Quit()
			}
		})
		return
	}
	switch result.Action {
	case "widgets":
		s.showControllerWidgetMenu(x, y)
	case "compact":
		s.activateControllerPreference("compact")
	case "show":
		s.Show()
	case "hide":
		s.Hide()
	case "quit":
		s.Quit()
	case "appearance":
		s.openControllerAppearance()
	case "console":
		s.activateControllerAction(actionConsole)
	case "control":
		s.activateControllerAction(actionChannels)
		s.Show()
	case "rf", "programs", "settings", "firmware", "events", "connection":
		s.openControllerForm(string(result.Action))
	case "reconnect":
		s.invokeController("reconnect")
	default:
		s.openControllerPage(string(result.Action))
	}
}

func (s *Shell) openControllerAppearance() {
	// Reuse the donor's themed menu and transactional appearance implementation;
	// omit its lamp address, lamp diagnostics and registration settings.
	items := []traymenu.Item{{ID: "system", Label: "System theme"}, {ID: "dark", Label: "Dark theme"}, {ID: "light", Label: "Light theme"}, {Separator: true}, {ID: "auto", Label: "Automatic material"}, {ID: "mica", Label: "Mica"}, {ID: "acrylic", Label: "Acrylic"}, {ID: "solid", Label: "Solid"}, {Separator: true}, {ID: "motion", Label: "Animations", Checked: s.Config().UI.Animations}, {ID: "pin", Label: "Always on top", Checked: s.Config().UI.AlwaysOnTop}}
	items = append(items, traymenu.Item{Separator: true}, traymenu.Item{ID: "glass", Label: "Raised glass controls", Checked: s.Config().UI.LayeredForeground}, traymenu.Item{ID: "flat", Label: "Flat controls", Checked: !s.Config().UI.LayeredForeground})
	items = append(items, traymenu.Item{Separator: true}, traymenu.Item{ID: "macos", Label: "macOS titlebar", Checked: s.Config().UI.ControllerChrome != "windows"}, traymenu.Item{ID: "windows", Label: "Windows titlebar", Checked: s.Config().UI.ControllerChrome == "windows"})
	items = append(items, traymenu.Item{Separator: true}, traymenu.Item{ID: "native-menu", Label: "Native Windows menus", Checked: s.Config().UI.NativeMenus})
	for index := range items {
		if !items[index].Separator {
			items[index].IconPNG, _ = assets.Files.ReadFile("icons/menu-settings.png")
		}
	}
	result, err := traymenu.ShowAtAnchor(s.controllerMenuOptions(items), traymenu.Rect{Left: int32(s.mw.BoundsPixels().X + 40), Top: int32(s.mw.BoundsPixels().Y + 70)})
	s.logControllerMenuMaterial(result, err)
	if err != nil {
		s.showError("Appearance", err)
		return
	}
	if result.Cancelled {
		return
	}
	cfg := s.Config()
	switch result.Action {
	case "native-menu":
		cfg.UI.NativeMenus = !cfg.UI.NativeMenus
	case "macos", "windows":
		cfg.UI.ControllerChrome = string(result.Action)
	case "glass", "flat":
		cfg.UI.LayeredForeground = result.Action == "glass"
	case "system", "dark", "light":
		cfg.UI.Theme = string(result.Action)
	case "auto", "mica", "acrylic", "solid":
		cfg.UI.Backdrop = string(result.Action)
	case "motion":
		cfg.UI.Animations = !cfg.UI.Animations
	case "pin":
		cfg.UI.AlwaysOnTop = !cfg.UI.AlwaysOnTop
	}
	if err := config.Save(s.configPath, cfg); err != nil {
		s.showError("Save appearance", err)
		return
	}
	s.ApplyConfig(cfg)
	_ = winfx.ReassertWindowChrome(s.HWND(), s.nativeTitleBar)
}

// Both popup entry points share the actual donor compositor, not an opaque
// native HMENU or a painted imitation. OS accessibility policy still wins.
func (s *Shell) controllerMenuOptions(items []traymenu.Item) traymenu.Options {
	return traymenu.Options{Owner: s.HWND(), Items: items,
		Theme: traymenu.Theme(s.Config().UI.Theme), Backdrop: traymenu.BackdropAcrylic,
		PreferNative: s.Config().UI.NativeMenus,
		FontFamily:   s.preferredFontFamily(), DisableAnimations: !s.motionEnabled}
}

func (s *Shell) logControllerMenuMaterial(result traymenu.Result, err error) {
	if err == nil {
		s.logf("native menu: material=%s native=%t reason=%s", result.BackdropUsed, result.UsedNativeFallback, result.NativeReason)
	}
}

func configureControllerTokens() {
	design.Default.Colors.Dark = design.ThemePalette{
		Accent: "#a78bfa", AccentHover: "#d8ccff", Warm: "#e7ae63", Success: "#73c69a", Danger: "#ed7889",
		Canvas: "#121116", CanvasAlt: "#0b0a0e", Surface: "#1d1b22", SurfaceHigh: "#33303b", SurfaceOverlay: "#33303b",
		Text: "#f5f2f7", TextMuted: "#938c9b", TextSubtle: "#938c9b", Stroke: "#4a4353", StrokeHighlight: "#70657f", Shadow: "#000000",
	}
	design.Default.Colors.Light = design.ThemePalette{
		Accent: "#6845bf", AccentHover: "#5334a3", Warm: "#8a5917", Success: "#307752", Danger: "#b33f53",
		Canvas: "#f5f3f6", CanvasAlt: "#ebe8ed", Surface: "#ffffff", SurfaceHigh: "#ded8e5", SurfaceOverlay: "#ebe8ed",
		Text: "#242126", TextMuted: "#7b7382", TextSubtle: "#7b7382", Stroke: "#c2b9cc", StrokeHighlight: "#ffffff", Shadow: "#000000",
	}
	design.Default.Text.FontFamily = "Segoe UI"
}
