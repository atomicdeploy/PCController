package tui

import (
	"charm.land/bubbles/v2/key"
	"pccontroller.local/desktop/internal/i18n"
)

type keyMap struct {
	Toggle      key.Binding
	On          key.Binding
	Off         key.Binding
	Refresh     key.Binding
	Reconnect   key.Binding
	Diagnostics key.Binding
	Terminal    key.Binding
	Commands    key.Binding
	Settings    key.Binding
	Device      key.Binding
	Serving     key.Binding
	Theme       key.Binding
	Help        key.Binding
	Quit        key.Binding
	Close       key.Binding
	Up          key.Binding
	Down        key.Binding
}

func defaultKeyMap(localizers ...i18n.Localizer) keyMap {
	localizer := i18n.New(i18n.English)
	if len(localizers) != 0 {
		localizer = localizers[0]
	}
	text := func(english string) string {
		return localizer.Choose(english, persianTUIText[english])
	}
	return keyMap{
		Toggle: key.NewBinding(
			key.WithKeys("space", "enter", "t"),
			key.WithHelp("space", text("toggle")),
		),
		On: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", text("on")),
		),
		Off: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", text("off")),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", text("refresh")),
		),
		Reconnect: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", text("reconnect")),
		),
		Diagnostics: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", text("diagnostics")),
		),
		Terminal: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", text("terminal")),
		),
		Commands: key.NewBinding(
			key.WithKeys(":", "ctrl+p"),
			key.WithHelp(":", text("commands")),
		),
		Settings: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", text("settings")),
		),
		Device: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", text("device")),
		),
		Serving: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", text("serving")),
		),
		Theme: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", text("theme")),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", text("more")),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", text("quit")),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", text("close")),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", text("events")),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", text("events")),
		),
	}
}

func (keys keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		keys.Toggle,
		keys.On,
		keys.Off,
		keys.Refresh,
		keys.Commands,
		keys.Settings,
		keys.Diagnostics,
		keys.Terminal,
		keys.Help,
		keys.Quit,
	}
}

func (keys keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Toggle, keys.On, keys.Off},
		{keys.Refresh, keys.Reconnect, keys.Commands, keys.Settings},
		{keys.Device, keys.Serving, keys.Diagnostics, keys.Terminal, keys.Up, keys.Down},
		{keys.Theme, keys.Help, keys.Quit},
	}
}
