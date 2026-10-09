package nativeshell

import "strings"

// Command is the stable, testable action identifier used by every platform
// menu implementation.
type Command uint16

const (
	CommandNone Command = iota
	CommandDashboard
	CommandControls
	CommandWorkbench
	CommandUpdates
	CommandSettings
	CommandReconnect
	CommandExit
)

type ItemKind uint8

const (
	ItemStatus ItemKind = iota
	ItemAction
	ItemSeparator
)

type MenuItem struct {
	Kind    ItemKind
	Command Command
	Label   string
	Enabled bool
	Default bool
}

// State is the minimal host-authenticated state a native shell may render.
type State struct {
	Title           string
	Connected       bool
	Paused          bool
	Port            string
	ConnectionState string
}

type IconState string

const (
	IconConnected    IconState = "connected"
	IconReconnecting IconState = "reconnecting"
	IconPaused       IconState = "paused"
	IconOffline      IconState = "offline"
)

func normalizeState(state State) State {
	state.Title = normalizeTitle(state.Title)
	state.Port = strings.TrimSpace(strings.ReplaceAll(state.Port, "\x00", ""))
	state.ConnectionState = strings.ToLower(strings.TrimSpace(state.ConnectionState))
	return state
}

// StatusLabel never treats an IPC/WebSocket transport as a controller
// connection. Connected is set only from the authenticated serial runtime.
func StatusLabel(state State) string {
	state = normalizeState(state)
	if state.Connected {
		if state.Port != "" {
			return "Controller connected — " + state.Port
		}
		return "Controller connected"
	}
	if state.Paused {
		return "Controller connection paused"
	}
	if state.ConnectionState == "reconnecting" || state.ConnectionState == "connecting" {
		return "Controller reconnecting…"
	}
	return "Controller offline"
}

func Tooltip(state State) string {
	state = normalizeState(state)
	status := "Offline"
	if state.Connected {
		status = "Connected"
		if state.Port != "" {
			status += " (" + state.Port + ")"
		}
	} else if state.Paused {
		status = "Connection paused"
	} else if state.ConnectionState == "reconnecting" || state.ConnectionState == "connecting" {
		status = "Reconnecting"
	}
	return state.Title + " — " + status
}

// StateIcon gives the tray a truthful semantic mark without changing the
// executable's stable product icon. Browser favicons use the same four-state
// vocabulary, so native and Web surfaces never disagree about connectivity.
func StateIcon(state State) IconState {
	state = normalizeState(state)
	if state.Connected {
		return IconConnected
	}
	if state.Paused {
		return IconPaused
	}
	if state.ConnectionState == "reconnecting" || state.ConnectionState == "connecting" {
		return IconReconnecting
	}
	return IconOffline
}

// BuildMenu is independent of Win32 so offline gating and command ordering are
// covered by ordinary unit tests. Page actions do not exist while offline.
func BuildMenu(state State) []MenuItem {
	state = normalizeState(state)
	items := []MenuItem{{Kind: ItemStatus, Label: StatusLabel(state)}}
	if state.Connected {
		items = append(items,
			MenuItem{Kind: ItemSeparator},
			MenuItem{Kind: ItemAction, Command: CommandDashboard, Label: "Dashboard", Enabled: true, Default: true},
			MenuItem{Kind: ItemAction, Command: CommandControls, Label: "Controls", Enabled: true},
			MenuItem{Kind: ItemAction, Command: CommandWorkbench, Label: "Workbench", Enabled: true},
			MenuItem{Kind: ItemAction, Command: CommandUpdates, Label: "Updates", Enabled: true},
			MenuItem{Kind: ItemAction, Command: CommandSettings, Label: "Settings", Enabled: true},
		)
	}
	reconnectLabel := "Connect now"
	if state.Connected {
		reconnectLabel = "Reconnect controller"
	} else if state.ConnectionState == "connecting" || state.ConnectionState == "reconnecting" {
		reconnectLabel = "Reconnect now"
	}
	items = append(items,
		MenuItem{Kind: ItemSeparator},
		MenuItem{Kind: ItemAction, Command: CommandReconnect, Label: reconnectLabel, Enabled: true},
		MenuItem{Kind: ItemSeparator},
		MenuItem{Kind: ItemAction, Command: CommandExit, Label: "Exit", Enabled: true},
	)
	return items
}

func PageForCommand(command Command) (string, bool) {
	switch command {
	case CommandDashboard:
		return "dashboard", true
	case CommandControls:
		return "controls", true
	case CommandWorkbench:
		return "workbench", true
	case CommandUpdates:
		return "updates", true
	case CommandSettings:
		return "settings", true
	default:
		return "", false
	}
}
