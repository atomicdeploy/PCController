//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"rayanlamp.local/app/internal/config"
	"strings"
)

func (s *Shell) paintControllerPreferences(c *walk.Canvas, b walk.Rectangle, p palette) error {
	s.controllerActionIDs = map[actionID]string{}
	s.controllerControl(c, walk.Rectangle{X: b.X, Y: b.Y, Width: 36, Height: 36}, actionChannelBack, "\uE72B", "", true, false, p)
	s.controllerText(c, "Preferences", s.titleFont, p.text, walk.Rectangle{X: b.X + 48, Y: b.Y, Width: b.Width - 48, Height: 36}, false)
	cfg := s.Config().UI
	type option struct {
		id, label string
		selected  bool
	}
	row := func(y int, title, icon string, items []option) {
		r := walk.Rectangle{X: b.X, Y: y, Width: b.Width, Height: 96}
		s.controllerCard(c, r, p)
		s.controllerGlyph(c, icon, p.accent, walk.Rectangle{X: r.X + 16, Y: y + 12, Width: 24, Height: 24})
		s.controllerText(c, title, s.bodyFont, p.text, walk.Rectangle{X: r.X + 50, Y: y + 10, Width: r.Width - 66, Height: 28}, false)
		width := (r.Width - 32 - (len(items)-1)*8) / len(items)
		for i, item := range items {
			id := actionChannelFirst + actionID(len(s.controllerActionIDs))
			s.controllerActionIDs[id] = "pref:" + item.id
			rect := walk.Rectangle{X: r.X + 16 + i*(width+8), Y: y + 48, Width: width, Height: 32}
			s.controllerControl(c, rect, id, "", "", true, item.selected, p)
			s.controllerText(c, item.label, s.captionFont, p.text, rect, true)
		}
	}
	y := b.Y + 50
	row(y, "Color theme", "\uE790", []option{{"theme:system", "System", cfg.Theme == "system" || cfg.Theme == ""}, {"theme:dark", "Dark", cfg.Theme == "dark"}, {"theme:light", "Light", cfg.Theme == "light"}})
	y += 108
	row(y, "Window chrome", "\uE737", []option{{"chrome:macos", "macOS", cfg.ControllerChrome != "windows"}, {"chrome:windows", "Windows", cfg.ControllerChrome == "windows"}})
	y += 108
	row(y, "Controls & motion", "\uE713", []option{{"glass", "Raised glass", cfg.LayeredForeground}, {"flat", "Flat", !cfg.LayeredForeground}, {"motion", "Animations", cfg.Animations}})
	y += 108
	row(y, "Desktop", "\uE7F4", []option{{"acrylic", "Acrylic", cfg.Backdrop == config.BackdropAcrylic}, {"solid", "Solid", cfg.Backdrop == config.BackdropSolid}, {"pin", "Always on top", cfg.AlwaysOnTop}, {"compact", "Compact", cfg.ViewMode == config.ViewCompact}, {"native-menu", "Native menus", cfg.NativeMenus}})
	y += 108
	for i, item := range []option{{"desktop-settings", "Full desktop settings…", false}, {"host", "Host configuration", false}} {
		id := actionChannelFirst + actionID(len(s.controllerActionIDs))
		s.controllerActionIDs[id] = "pref:" + item.id
		r := walk.Rectangle{X: b.X + i*(b.Width/2+6), Y: y, Width: b.Width/2 - 6, Height: 38}
		s.controllerControl(c, r, id, "\uE713", item.label, item.id != "console" || s.controller.ShowConsole != nil, item.selected, p)
	}
	return nil
}

func (s *Shell) activateControllerPreference(id string) {
	if id == "desktop-settings" {
		s.OpenSettings()
		return
	}
	if id == "host" {
		s.openControllerForm("settings")
		return
	}
	if id == "console" {
		s.activateControllerAction(actionConsole)
		return
	}
	cfg := s.Config()
	switch {
	case id == "compact":
		if s.controllerInline != nil {
			s.controllerInline.input.SetFocus()
			return
		}
		if cfg.UI.ViewMode == config.ViewCompact {
			cfg.UI.ViewMode = config.ViewDashboard
		} else {
			cfg.UI.ViewMode = config.ViewCompact
		}
		s.controllerPreferences, s.controllerChannels = false, false
		s.controllerMinimumHeight = 0
	case id == "native-menu":
		cfg.UI.NativeMenus = !cfg.UI.NativeMenus
	case strings.HasPrefix(id, "theme:"):
		cfg.UI.Theme = strings.TrimPrefix(id, "theme:")
	case strings.HasPrefix(id, "chrome:"):
		cfg.UI.ControllerChrome = strings.TrimPrefix(id, "chrome:")
	case id == "glass" || id == "flat":
		cfg.UI.LayeredForeground = id == "glass"
	case id == "motion":
		cfg.UI.Animations = !cfg.UI.Animations
	case id == "pin":
		cfg.UI.AlwaysOnTop = !cfg.UI.AlwaysOnTop
	case id == "acrylic":
		cfg.UI.Backdrop = config.BackdropAcrylic
	case id == "solid":
		cfg.UI.Backdrop = config.BackdropSolid
	default:
		return
	}
	cfg = controllerViewConfig(s.Config(), cfg)
	if err := config.Save(s.configPath, cfg); err != nil {
		s.showError("Preferences", err)
		return
	}
	s.ApplyConfig(cfg)
}
