package hostui

import (
	"errors"
	"pccontroller.local/controller/internal/productidentity"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UpdateProgress struct {
	OperationID, Kind, State, Stage, Detail, ErrorCode string
	Known                                              bool
	Percent                                            int
	StartedAt, UpdatedAt                               time.Time
}

func ParseUpdateProgress(kind, text string, metadata map[string]string, at time.Time) UpdateProgress {
	value := UpdateProgress{OperationID: metadata["operation_id"], Kind: metadata["kind"], State: strings.ToLower(strings.TrimSpace(metadata["state"])), Stage: metadata["stage"], Detail: metadata["detail"], ErrorCode: metadata["error_code"], UpdatedAt: at}
	if value.State == "" {
		value.State = strings.TrimPrefix(strings.ToLower(kind), "update.")
	}
	if value.Stage == "" {
		value.Stage = value.State
	}
	if value.Detail == "" {
		value.Detail = text
	}
	var err error
	value.Percent, err = strconv.Atoi(metadata["progress_percent"])
	value.Known = err == nil && metadata["progress_known"] == "true" && value.Percent >= 0 && value.Percent <= 100
	if parsed, err := time.Parse(time.RFC3339Nano, metadata["started_at"]); err == nil {
		value.StartedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, metadata["updated_at"]); err == nil {
		value.UpdatedAt = parsed
	}
	return value
}

func (value UpdateProgress) Active() bool {
	switch value.State {
	case "", "idle", "downloaded", "staged", "completed", "failed", "cancelled":
		return false
	default:
		return true
	}
}

func (value UpdateProgress) Terminal() TerminalProgress {
	if value.State == "failed" {
		return TerminalProgress{State: 2}
	}
	if !value.Active() {
		return TerminalProgress{}
	}
	if !value.Known {
		return TerminalProgress{State: 3}
	}
	return TerminalProgress{State: 1, Percent: value.Percent}
}

// Each operation/stage receives at most one toast even if progress repeats or
// retries arrive. A bounded history prevents a long-running host from growing.
type UpdateNotificationTracker struct {
	mu    sync.Mutex
	seen  map[string]bool
	order []string
}

func (tracker *UpdateNotificationTracker) Next(value UpdateProgress) (Notification, bool) {
	if value.OperationID == "" {
		return Notification{}, false
	}
	stage := value.Stage
	title, severity := "", "info"
	switch value.State {
	case "failed":
		title, severity, stage = "Firmware update failed", "error", "failed"
	case "completed":
		title, severity, stage = "Firmware update complete", "success", "completed"
	case "cancelled":
		title, severity, stage = "Firmware update cancelled", "warning", "cancelled"
	default:
		switch {
		case stage == "backup", stage == "backing-up":
			title, stage = "Saving board backup", "backup"
		case strings.Contains(stage, "verif"):
			title, stage = "Verifying board firmware", "verify"
		case stage == "flash", strings.Contains(stage, "writing"), strings.Contains(stage, "flash write"):
			title, stage = "Writing board firmware", "flash"
		default:
			return Notification{}, false
		}
	}
	key := value.OperationID + ":" + stage
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.seen == nil {
		tracker.seen = make(map[string]bool)
	}
	if tracker.seen[key] {
		return Notification{}, false
	}
	if len(tracker.order) >= 128 {
		delete(tracker.seen, tracker.order[0])
		tracker.order = tracker.order[1:]
	}
	tracker.seen[key] = true
	tracker.order = append(tracker.order, key)
	body := value.Detail
	if value.State == "failed" && value.Stage != "" {
		body = "Failed at " + strings.ReplaceAll(value.Stage, "-", " ") + ". " + body
	}
	if len([]rune(body)) > 4000 {
		body = string([]rune(body)[:3999]) + "…"
	}
	if value.Kind == "host" {
		title = strings.ReplaceAll(title, "Firmware", "Host")
		title = strings.ReplaceAll(title, "firmware", "host")
	}
	uri := productidentity.ProtocolScheme + "://page/programming"
	return Notification{ID: key, Title: title, Body: body, Severity: severity, LaunchURI: uri, Actions: []NotificationAction{{Label: "View update", URI: uri}}}, true
}

var updateTaskbarOrder struct {
	sync.Mutex
	latest   time.Time
	previous TerminalProgress
	have     bool
}

// Serialize COM updates and discard delayed older events so completion cannot
// be overwritten by an earlier percentage callback from the UI event loop.
func SetUpdateTaskbarProgress(value UpdateProgress) error {
	return PresentUpdateProgress(value, nil)
}

// PresentUpdateProgress orders terminal OSC and native Windows presentation as
// one update, so an older asynchronous percentage cannot resurrect a busy bar.
func PresentUpdateProgress(value UpdateProgress, writeOSC func(string) error) error {
	updateTaskbarOrder.Lock()
	defer updateTaskbarOrder.Unlock()
	if !value.UpdatedAt.IsZero() && value.UpdatedAt.Before(updateTaskbarOrder.latest) {
		return nil
	}
	progress := value.Terminal()
	updateTaskbarOrder.latest = value.UpdatedAt
	if updateTaskbarOrder.have && updateTaskbarOrder.previous == progress {
		return nil
	}
	payload, err := progress.OSCPayload()
	if err != nil {
		return err
	}
	var outputErr error
	if writeOSC != nil {
		outputErr = writeOSC(payload)
	}
	err = errors.Join(outputErr, SetTaskbarProgress(progress))
	if err == nil {
		updateTaskbarOrder.previous, updateTaskbarOrder.have = progress, true
	}
	return err
}

func ClearUpdateTaskbarProgress() error {
	updateTaskbarOrder.Lock()
	defer updateTaskbarOrder.Unlock()
	updateTaskbarOrder.latest = time.Now()
	updateTaskbarOrder.previous, updateTaskbarOrder.have = TerminalProgress{}, true
	return SetTaskbarProgress(TerminalProgress{})
}
