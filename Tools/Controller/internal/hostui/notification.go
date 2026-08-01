package hostui

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

type NotificationAction struct {
	Label string `json:"label"`
	URI   string `json:"uri"`
}

type Notification struct {
	ID        string               `json:"id,omitempty"`
	Title     string               `json:"title"`
	Body      string               `json:"body"`
	Severity  string               `json:"severity,omitempty"`
	LaunchURI string               `json:"launch_uri,omitempty"`
	Actions   []NotificationAction `json:"actions,omitempty"`
}

type NotificationStatus struct {
	Supported bool      `json:"supported"`
	Available bool      `json:"available"`
	Accepted  uint64    `json:"accepted"`
	LastAt    time.Time `json:"last_at,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

type Notifier interface {
	Notify(context.Context, Notification) error
	Status() NotificationStatus
}

type NotifierOptions struct {
	AppID string
}

func NewNotifier(options NotifierOptions) Notifier { return newPlatformNotifier(options) }

type toastXML struct {
	XMLName xml.Name      `xml:"toast"`
	Launch  string        `xml:"launch,attr,omitempty"`
	Visual  toastVisual   `xml:"visual"`
	Actions *toastActions `xml:"actions,omitempty"`
}
type toastVisual struct {
	Binding toastBinding `xml:"binding"`
}
type toastBinding struct {
	Template string   `xml:"template,attr"`
	Texts    []string `xml:"text"`
}
type toastActions struct {
	Actions []toastAction `xml:"action"`
}
type toastAction struct {
	Content        string `xml:"content,attr"`
	Arguments      string `xml:"arguments,attr"`
	ActivationType string `xml:"activationType,attr"`
}

func buildToastXML(notification Notification) ([]byte, error) {
	if strings.TrimSpace(notification.Title) == "" || len([]rune(notification.Title)) > 128 {
		return nil, errors.New("notification title must contain 1..128 characters")
	}
	if len([]rune(notification.Body)) > 4096 {
		return nil, errors.New("notification body exceeds 4096 characters")
	}
	if len(notification.Actions) > 5 {
		return nil, errors.New("a notification may contain at most five actions")
	}
	value := toastXML{
		Launch: notification.LaunchURI,
		Visual: toastVisual{Binding: toastBinding{Template: "ToastGeneric", Texts: []string{notification.Title, notification.Body}}},
	}
	if notification.LaunchURI != "" {
		if err := validateActionURI(notification.LaunchURI); err != nil {
			return nil, fmt.Errorf("launch URI: %w", err)
		}
	}
	if len(notification.Actions) != 0 {
		value.Actions = &toastActions{Actions: make([]toastAction, 0, len(notification.Actions))}
		for index, action := range notification.Actions {
			if strings.TrimSpace(action.Label) == "" || len([]rune(action.Label)) > 64 {
				return nil, fmt.Errorf("notification action %d label must contain 1..64 characters", index)
			}
			if err := validateActionURI(action.URI); err != nil {
				return nil, fmt.Errorf("notification action %d: %w", index, err)
			}
			value.Actions.Actions = append(value.Actions.Actions, toastAction{Content: action.Label, Arguments: action.URI, ActivationType: "protocol"})
		}
	}
	return xml.Marshal(value)
}

func validateActionURI(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("action URI must be absolute")
	}
	switch strings.ToLower(parsed.Scheme) {
	case productidentity.ProtocolScheme, "http", "https":
		return nil
	default:
		return fmt.Errorf("action URI scheme %q is not allowed", parsed.Scheme)
	}
}

type ImportantEvent struct {
	Kind     string
	Title    string
	Message  string
	AppTitle string
}

// NotificationForImportantEvent maps only actionable/high-signal events.
// Routine telemetry intentionally returns false to avoid notification spam.
func NotificationForImportantEvent(event ImportantEvent) (Notification, bool) {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	runningDoorWarning := kind == "warning.door-open-running"
	if !(kind == "error" || kind == "door" || strings.Contains(kind, "hot") ||
		strings.Contains(kind, "motion") || strings.Contains(kind, "relay") ||
		strings.HasPrefix(kind, "rf") || runningDoorWarning) {
		return Notification{}, false
	}
	title := strings.TrimSpace(event.Title)
	if title == "" {
		application := productidentity.Title(event.AppTitle)
		if runningDoorWarning {
			title = application + " · Door open during operation"
		} else {
			title = application + " · " + strings.ToUpper(kind)
		}
	}
	page := "events"
	if strings.Contains(kind, "relay") || strings.Contains(kind, "motion") {
		page = "outputs"
	}
	if strings.HasPrefix(kind, "rf") {
		page = "rf"
	}
	actionPrefix := productidentity.ProtocolScheme + "://"
	actions := []NotificationAction{{Label: "Open " + titleWord(page), URI: actionPrefix + "page/" + page}}
	if strings.Contains(kind, "motion") || strings.Contains(kind, "hot") ||
		kind == "error" || runningDoorWarning {
		actions = append(actions, NotificationAction{Label: "Stop outputs", URI: actionPrefix + "command/relay%20off"})
	}
	return Notification{
		ID: fmt.Sprintf("%s-%d", kind, time.Now().UnixNano()), Title: title,
		Body: event.Message, Severity: kind, LaunchURI: actionPrefix + "page/" + page,
		Actions: actions,
	}, true
}

func titleWord(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
