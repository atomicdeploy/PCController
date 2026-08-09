//go:build linux

package hostui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

type linuxNotifier struct {
	mu      sync.RWMutex
	appID   string
	tool    string
	status  NotificationStatus
	deliver func(context.Context, string, string, Notification) error
	gate    chan struct{}
}

var linuxNotifyLookPath = exec.LookPath
var linuxNotifyEUID = os.Geteuid

var linuxNotifyRun = func(ctx context.Context, environment []string, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	if environment != nil {
		command.Env = append(os.Environ(), environment...)
	}
	return command.CombinedOutput()
}

func newPlatformNotifier(options NotifierOptions) Notifier {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	tool, err := linuxNotifyLookPath("notify-send")
	status := NotificationStatus{Supported: true, Backend: "notify-send"}
	if err == nil {
		status.Available = true
	} else {
		status.LastError = "notify-send is not installed"
	}
	return &linuxNotifier{
		appID: appID, tool: tool, status: status, deliver: deliverLinuxNotification,
		gate: make(chan struct{}, 1),
	}
}

func (notifier *linuxNotifier) Notify(ctx context.Context, notification Notification) error {
	if notifier == nil || notifier.tool == "" || notifier.deliver == nil {
		return errors.New("Linux desktop notifications require notify-send")
	}
	select {
	case notifier.gate <- struct{}{}:
		defer func() { <-notifier.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if _, err := buildToastXML(notification); err != nil {
		return err
	}
	err := notifier.deliver(ctx, notifier.tool, notifier.appID, notification)
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if err != nil {
		notifier.status.Available = false
		notifier.status.LastError = err.Error()
		return err
	}
	notifier.status.Available = true
	notifier.status.Accepted++
	notifier.status.LastAt = time.Now().UTC()
	notifier.status.LastError = ""
	notifier.status.Degraded = notification.LaunchURI != "" || len(notification.Actions) != 0
	if notifier.status.Degraded {
		notifier.status.LastFallback = "notify-send displayed the message without protocol action buttons"
	} else {
		notifier.status.LastFallback = ""
	}
	return nil
}

func (notifier *linuxNotifier) Status() NotificationStatus {
	if notifier == nil {
		return NotificationStatus{Supported: true, LastError: "notification backend is nil"}
	}
	notifier.mu.RLock()
	defer notifier.mu.RUnlock()
	return notifier.status
}

func deliverLinuxNotification(
	ctx context.Context,
	tool, appID string,
	notification Notification,
) error {
	urgency := "normal"
	severity := strings.ToLower(strings.TrimSpace(notification.Severity))
	if strings.Contains(severity, "error") || strings.Contains(severity, "fault") || strings.Contains(severity, "hot") {
		urgency = "critical"
	} else if severity == "telemetry" || severity == "info" {
		urgency = "low"
	}
	arguments := []string{"--app-name=" + appID, "--urgency=" + urgency, "--expire-time=10000", notification.Title}
	if notification.Body != "" {
		arguments = append(arguments, notification.Body)
	}
	session, err := activeLinuxGraphicalSession(ctx)
	if err != nil {
		return err
	}
	runtimeDirectory := "/run/user/" + strconv.Itoa(session.uid)
	environment := []string{
		"XDG_RUNTIME_DIR=" + runtimeDirectory,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDirectory + "/bus",
	}
	currentUID := linuxNotifyEUID()
	if currentUID == session.uid {
		return runNotifyCommand(ctx, environment, tool, arguments)
	}
	if currentUID != 0 {
		return fmt.Errorf(
			"active physical desktop belongs to UID %d, but notification process runs as UID %d",
			session.uid, currentUID,
		)
	}
	runuserArguments := []string{
		"-u", session.user, "--", "env",
		environment[0],
		environment[1],
		tool,
	}
	runuserArguments = append(runuserArguments, arguments...)
	return runNotifyCommand(ctx, nil, "runuser", runuserArguments)
}

func runNotifyCommand(ctx context.Context, environment []string, name string, arguments []string) error {
	output, err := linuxNotifyRun(ctx, environment, name, arguments...)
	if err == nil {
		return nil
	}
	detail := strings.Join(strings.Fields(strings.ToValidUTF8(string(output), "")), " ")
	if len(detail) > 256 {
		detail = detail[:256] + "..."
	}
	if detail != "" {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(name), err, detail)
	}
	return fmt.Errorf("%s failed: %w", filepath.Base(name), err)
}

type linuxGraphicalSession struct {
	id, user string
	uid      int
}

func activeLinuxGraphicalSession(ctx context.Context) (linuxGraphicalSession, error) {
	output, err := linuxNotifyRun(ctx, nil, "loginctl", "list-sessions", "--no-legend", "--no-pager")
	if err != nil {
		return linuxGraphicalSession{}, fmt.Errorf("discover graphical login session: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		uid, uidErr := strconv.Atoi(fields[1])
		if uidErr != nil || uid < 0 || strings.TrimSpace(fields[2]) == "" {
			continue
		}
		candidate := linuxGraphicalSession{id: fields[0], uid: uid, user: fields[2]}
		properties, propertyErr := linuxNotifyRun(
			ctx, nil, "loginctl", "show-session", candidate.id,
			"--property=Active", "--property=Remote", "--property=Type", "--property=State",
			"--property=Seat", "--property=Class", "--property=User", "--property=Name", "--no-pager",
		)
		if propertyErr != nil {
			continue
		}
		values := parseLoginctlProperties(string(properties))
		graphical := strings.EqualFold(values["Type"], "wayland") || strings.EqualFold(values["Type"], "x11")
		state := strings.ToLower(values["State"])
		if values["Active"] == "yes" && values["Remote"] == "no" && graphical &&
			(state == "active" || state == "online") && values["Seat"] == "seat0" &&
			values["Class"] == "user" && values["User"] == strconv.Itoa(candidate.uid) &&
			values["Name"] == candidate.user {
			return candidate, nil
		}
	}
	return linuxGraphicalSession{}, errors.New("no active local seat0 user Wayland or X11 session is available for desktop notifications")
}

func parseLoginctlProperties(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}
