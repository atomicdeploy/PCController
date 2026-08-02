//go:build windows

package hostui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"pccontroller.local/controller/internal/productidentity"
)

type windowsNotifier struct {
	mu     sync.RWMutex
	appID  string
	status NotificationStatus
	run    func(context.Context, string, ...string) error
	gate   chan struct{}
}

func newPlatformNotifier(options NotifierOptions) Notifier {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	return &windowsNotifier{
		appID:  appID,
		status: NotificationStatus{Supported: true, Available: true},
		run:    runPowerShell,
		gate:   make(chan struct{}, 1),
	}
}

func (notifier *windowsNotifier) Notify(ctx context.Context, notification Notification) error {
	select {
	case notifier.gate <- struct{}{}:
		defer func() { <-notifier.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	payload, err := buildToastXML(notification)
	if err != nil {
		return err
	}
	xmlBase64 := base64.StdEncoding.EncodeToString(payload)
	appBase64 := base64.StdEncoding.EncodeToString([]byte(notifier.appID))
	err = notifier.run(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedToastScript(xmlBase64, appBase64))
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if err != nil {
		notifier.status.LastError = err.Error()
		return err
	}
	notifier.status.Accepted++
	notifier.status.LastAt = time.Now()
	notifier.status.LastError = ""
	return nil
}

func (notifier *windowsNotifier) Status() NotificationStatus {
	notifier.mu.RLock()
	defer notifier.mu.RUnlock()
	return notifier.status
}

func runPowerShell(ctx context.Context, executable string, arguments ...string) error {
	command := exec.CommandContext(ctx, executable, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run PowerShell: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func encodedToastScript(xmlBase64, appBase64 string) string {
	script := fmt.Sprintf(`
$ErrorActionPreference='Stop'
[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime] > $null
[Windows.UI.Notifications.ToastNotification,Windows.UI.Notifications,ContentType=WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime] > $null
$xmlText=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$appId=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$document=New-Object Windows.Data.Xml.Dom.XmlDocument
$document.LoadXml($xmlText)
$toast=New-Object Windows.UI.Notifications.ToastNotification $document
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId).Show($toast)
`, xmlBase64, appBase64)
	encoded := base64.StdEncoding.EncodeToString(utf16LE(script))
	return encoded
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	result := make([]byte, len(units)*2)
	for index, unit := range units {
		result[index*2] = byte(unit)
		result[index*2+1] = byte(unit >> 8)
	}
	return result
}
