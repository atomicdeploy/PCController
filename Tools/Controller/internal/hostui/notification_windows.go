//go:build windows

package hostui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

type windowsNotifier struct {
	mu       sync.RWMutex
	appID    string
	status   NotificationStatus
	deliver  func(context.Context, []byte, string) error
	fallback func(context.Context, Notification, error) error
	gate     chan struct{}
}

func newPlatformNotifier(options NotifierOptions) Notifier {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	return &windowsNotifier{
		appID:    appID,
		status:   NotificationStatus{Supported: true, Available: true, Backend: "winrt-toast"},
		deliver:  deliverWindowsToast,
		fallback: showWindowsNotificationDialog,
		gate:     make(chan struct{}, 1),
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
	err = notifier.deliver(ctx, payload, notifier.appID)
	backend := "winrt-toast"
	degraded := false
	fallbackReason := ""
	if err != nil {
		fallbackReason = err.Error()
		fallbackErr := notifier.fallback(ctx, notification, err)
		if fallbackErr != nil {
			err = errors.Join(err, fallbackErr)
		} else {
			err = nil
			backend = "task-dialog"
			degraded = true
		}
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if err != nil {
		notifier.status.LastError = err.Error()
		notifier.status.Available = false
		return err
	}
	notifier.status.Accepted++
	notifier.status.Available = true
	notifier.status.Backend = backend
	notifier.status.Degraded = degraded
	notifier.status.LastFallback = fallbackReason
	notifier.status.LastAt = time.Now()
	notifier.status.LastError = ""
	return nil
}

func (notifier *windowsNotifier) Status() NotificationStatus {
	notifier.mu.RLock()
	defer notifier.mu.RUnlock()
	return notifier.status
}
