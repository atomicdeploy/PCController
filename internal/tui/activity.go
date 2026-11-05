package tui

import (
	"strings"

	"rayanlamp.local/app/internal/terminal"
)

// activityLevel and activityEvent intentionally use the same string contract
// as the Web UI browser console. Keeping the vocabulary stable makes an event
// recognizable whether the user is watching DevTools or the terminal UI.
type activityLevel string

const (
	activityLevelDebug   activityLevel = "debug"
	activityLevelInfo    activityLevel = "info"
	activityLevelSuccess activityLevel = "success"
	activityLevelWarn    activityLevel = "warn"
	activityLevelError   activityLevel = "error"
)

type activityEvent string

const (
	activityAppStart              activityEvent = "app.start"
	activityTransportReady        activityEvent = "transport.ready"
	activityTransportError        activityEvent = "transport.error"
	activityLampState             activityEvent = "lamp.state"
	activityLampCommand           activityEvent = "lamp.command"
	activityWebSocketConnected    activityEvent = "websocket.connected"
	activityWebSocketDisconnected activityEvent = "websocket.disconnected"
	activityMessageReceived       activityEvent = "message.received"
	activityAudioCue              activityEvent = "audio.cue"
	activityDeviceEvent           activityEvent = "device.event"
	activityDiagnosticsLoaded     activityEvent = "diagnostics.loaded"
	activityDiagnosticsError      activityEvent = "diagnostics.error"
)

type activityPresentation struct {
	emoji string
	ascii string
}

var activityPresentations = map[activityEvent]activityPresentation{
	activityAppStart:              {emoji: "🚀", ascii: "[START]"},
	activityTransportReady:        {emoji: "🔌", ascii: "[LINK]"},
	activityTransportError:        {emoji: "⚠️", ascii: "[WARN]"},
	activityLampState:             {emoji: "💡", ascii: "[LAMP]"},
	activityLampCommand:           {emoji: "⚡", ascii: "[CMD]"},
	activityWebSocketConnected:    {emoji: "📡", ascii: "[WS]"},
	activityWebSocketDisconnected: {emoji: "📡", ascii: "[WS]"},
	activityMessageReceived:       {emoji: "💬", ascii: "[MSG]"},
	activityAudioCue:              {emoji: "🔊", ascii: "[AUDIO]"},
	activityDeviceEvent:           {emoji: "📟", ascii: "[DEVICE]"},
	activityDiagnosticsLoaded:     {emoji: "🩺", ascii: "[DIAG]"},
	activityDiagnosticsError:      {emoji: "⚠️", ascii: "[WARN]"},
}

func activitySymbol(event activityEvent) string {
	if presentation, ok := activityPresentations[event]; ok {
		return terminal.DisplaySymbol(
			presentation.emoji,
			presentation.ascii,
		)
	}
	return "*"
}

func normalizeActivityLevel(level activityLevel) activityLevel {
	switch level {
	case activityLevelDebug,
		activityLevelInfo,
		activityLevelSuccess,
		activityLevelWarn,
		activityLevelError:
		return level
	default:
		return activityLevelInfo
	}
}

func formatActivityLevel(level activityLevel) string {
	return strings.ToUpper(string(normalizeActivityLevel(level)))
}
