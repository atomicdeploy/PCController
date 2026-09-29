package ipcjson

import (
	"strings"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
)

// AppActionDeliveryEvent converts a validated broker delivery into the typed
// event consumed by WebSocket and Socket.IO clients. Ordinary actions require
// the action coordinator's complete correlated-delivery envelope. Navigation
// synchronization instead uses its own complete exact-target coordinator
// envelope and does not manufacture action-outcome receipts.
func AppActionDeliveryEvent(action hostui.AppAction) (control.Event, bool) {
	kind := strings.ToLower(strings.TrimSpace(action.Kind))
	value := strings.TrimSpace(action.Value)
	_, navigationDelivery := hostui.ParseNavigationUpdate(action)
	trackedDelivery := action.OperationID != "" &&
		action.Metadata[hostui.ActionDeliveryIDKey] != "" &&
		action.Metadata[hostui.ActionExpiresAtKey] != ""
	if (!strings.HasPrefix(kind, "app.") && !hostui.IsRegisteredCustomActionKind(kind)) ||
		(!trackedDelivery && !navigationDelivery) {
		return control.Event{}, false
	}
	target := strings.TrimSpace(action.Target)
	if target == "" {
		target = "*"
	}
	verb := strings.TrimPrefix(kind, "app.")
	text := verb
	if value != "" {
		text += " " + value
	}
	metadata := make(map[string]string, len(action.Metadata)+3)
	for key, item := range action.Metadata {
		metadata[key] = item
	}
	metadata["value"] = value
	metadata["target_instance"] = target
	if action.OperationID != "" {
		metadata["operation_id"] = action.OperationID
	}
	actionName := verb
	if kind == "app.page" {
		metadata["page"] = value
		actionName = "navigate"
		text = "Open page " + value
	}
	return control.Event{
		Kind:     kind,
		Stream:   control.EventStreamState,
		Text:     text,
		Source:   action.Source,
		Target:   "app.clients",
		Action:   actionName,
		Metadata: metadata,
	}, true
}
