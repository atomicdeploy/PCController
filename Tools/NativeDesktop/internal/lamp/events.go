package lamp

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"rayanlamp.local/app/internal/model"
)

const (
	maxRemoteEventBytes   = 16 << 10
	maxRemoteMessageBytes = 4 << 10
	maxRemoteIcon         = 255
	maxRemoteBeeps        = 10
	remoteMessageTitle    = "Rayan Lamp"
)

// ParseEvent converts a supported firmware WebSocket message to a model event.
// It deliberately ignores unrelated frames. Supported messages are ON, OFF,
// message:<text>, msg:<icon>,<URL-encoded text>, and beep:<count>.
func ParseEvent(raw string) (model.Event, bool) {
	if raw == "" || len(raw) > maxRemoteEventBytes || !utf8.ValidString(raw) ||
		strings.IndexByte(raw, 0) >= 0 {
		return model.Event{}, false
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return model.Event{}, false
	}

	switch {
	case strings.EqualFold(text, "ON"), strings.EqualFold(text, "OFF"):
		state := model.ParseLampState(text)
		return model.Event{
			Kind: model.EventSnapshot,
			Snapshot: model.Snapshot{
				State:           state,
				WebSocketOnline: true,
				LastEvent:       text,
				LastChanged:     time.Now().UTC(),
			},
			Raw:        text,
			FromRemote: true,
		}, true

	case hasPrefixFold(text, "message:"):
		message := strings.TrimSpace(text[len("message:"):])
		if !validRemoteMessage(message) {
			return model.Event{}, false
		}
		return messageEvent(text, 0, message), true

	case hasPrefixFold(text, "msg:"):
		fields := text[len("msg:"):]
		iconText, payload, found := strings.Cut(fields, ",")
		if !found {
			return model.Event{}, false
		}
		icon, err := strconv.Atoi(strings.TrimSpace(iconText))
		if err != nil || icon < 0 || icon > maxRemoteIcon {
			return model.Event{}, false
		}
		message, err := url.QueryUnescape(payload)
		if err != nil {
			return model.Event{}, false
		}
		message = strings.TrimSpace(message)
		if !validRemoteMessage(message) {
			return model.Event{}, false
		}
		return messageEvent(text, icon, message), true

	case hasPrefixFold(text, "beep:"):
		count, err := strconv.Atoi(strings.TrimSpace(text[len("beep:"):]))
		if err != nil || count < 1 || count > maxRemoteBeeps {
			return model.Event{}, false
		}
		return model.Event{
			Kind:       model.EventBeep,
			BeepCount:  count,
			Raw:        text,
			FromRemote: true,
		}, true
	}

	return model.Event{}, false
}

func messageEvent(raw string, icon int, message string) model.Event {
	return model.Event{
		Kind:       model.EventMessage,
		Title:      remoteMessageTitle,
		Message:    message,
		Icon:       icon,
		Raw:        raw,
		FromRemote: true,
	}
}

func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

func validRemoteMessage(message string) bool {
	if message == "" || len(message) > maxRemoteMessageBytes || !utf8.ValidString(message) ||
		strings.IndexByte(message, 0) >= 0 {
		return false
	}
	for _, r := range message {
		if unicode.IsControl(r) && r != '\r' && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
