package hostui

import (
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

// DoorNotification describes one verified physical enclosure-door transition.
// Device and port labels are presentation details from the authenticated local
// controller snapshot; they never decide whether the event is trusted.
type DoorNotification struct {
	Open         bool
	Running      bool
	AppTitle     string
	Device       string
	Port         string
	ProgramState string
}

// NotificationForDoorTransition gives physical open/close changes distinct,
// dynamic presentation. The host bridge owns provenance filtering so a
// synthetic host/bridge event cannot impersonate the reed switch.
func NotificationForDoorTransition(event DoorNotification) Notification {
	application := productidentity.Title(event.AppTitle)
	state, transition := "closed", "closed"
	if event.Open {
		state, transition = "open", "opened"
	}
	title := application + " · Enclosure door " + transition
	severity := "door"
	if event.Open && event.Running {
		title = application + " · Door open during operation"
		severity = "warning"
	}
	device := strings.TrimSpace(event.Device)
	port := strings.TrimSpace(event.Port)
	if device == "" {
		device = "The connected controller"
	}
	if port != "" && !strings.EqualFold(device, port) {
		device += " (" + port + ")"
	}
	body := fmt.Sprintf("%s reports the enclosure door is %s.", device, state)
	programState := strings.TrimSpace(event.ProgramState)
	if programState != "" {
		body += " Program state: " + programState + "."
	}
	if event.Open && event.Running {
		body += " Stop outputs if this was not expected."
	}
	actionPrefix := productidentity.ProtocolScheme + "://"
	actions := []NotificationAction{{
		Label: "Open Events", URI: actionPrefix + "page/events",
	}}
	if event.Open && event.Running {
		actions = append(actions, NotificationAction{
			Label: "Stop outputs", URI: actionPrefix + "command/relay%20off",
		})
	}
	return Notification{
		ID:    fmt.Sprintf("door-%s-%d", state, time.Now().UnixNano()),
		Title: title, Body: body, Severity: severity,
		LaunchURI: actionPrefix + "page/events", Actions: actions,
	}
}
