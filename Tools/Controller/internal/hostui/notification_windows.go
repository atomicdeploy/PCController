//go:build windows

package hostui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

type windowsNotifier struct {
	mu      sync.RWMutex
	appID   string
	logoURI string
	logoErr error
	status  NotificationStatus
	deliver func(context.Context, []byte, string) error
	balloon func(context.Context, Notification, error) error
	dialog  func(context.Context, Notification, error) error
	gate    chan struct{}
}

func newPlatformNotifier(options NotifierOptions) Notifier {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	logoURI, logoErr := resolveWindowsToastLogo(options.LogoPath)
	return &windowsNotifier{
		appID:   appID,
		logoURI: logoURI,
		logoErr: logoErr,
		status:  NotificationStatus{Supported: true, Available: true, Branded: logoErr == nil, Backend: "winrt-toast"},
		deliver: deliverWindowsToast,
		balloon: showWindowsBalloon,
		dialog:  showWindowsNotificationDialog,
		gate:    make(chan struct{}, 1),
	}
}

func resolveWindowsToastLogo(value string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		var err error
		path, err = ResolveToastLogoPath("")
		if err != nil {
			return "", fmt.Errorf("resolve notification logo: %w", err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve notification logo: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("inspect notification logo: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || isWindowsReparsePoint(info) {
		return "", errors.New("notification logo must be a regular non-link file")
	}
	if info.Size() <= 8 || info.Size() > 1024*1024 {
		return "", fmt.Errorf("notification logo size %d is outside 9..1048576 bytes", info.Size())
	}
	file, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("open notification logo: %w", err)
	}
	defer file.Close()
	header := make([]byte, 8)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", fmt.Errorf("read notification logo: %w", err)
	}
	if string(header) != "\x89PNG\r\n\x1a\n" {
		return "", errors.New("notification logo is not a PNG image")
	}
	slashed := filepath.ToSlash(abs)
	if filepath.VolumeName(abs) != "" && !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String(), nil
}

func (notifier *windowsNotifier) Notify(ctx context.Context, notification Notification) error {
	select {
	case notifier.gate <- struct{}{}:
		defer func() { <-notifier.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	var err error
	if notifier.logoErr != nil {
		err = notifier.logoErr
	}
	var payload []byte
	if err == nil {
		payload, err = buildToastXML(notification, notifier.logoURI)
	}
	if err == nil {
		err = notifier.deliver(ctx, payload, notifier.appID)
	}
	backend := "winrt-toast"
	degraded := false
	branded := true
	fallbackReason := ""
	if err != nil {
		fallbackReason = err.Error()
		balloonErr := notifier.balloon(ctx, notification, err)
		if balloonErr == nil {
			err = nil
			backend = "legacy-balloon"
			degraded = true
		} else {
			fallbackReason = errors.Join(err, balloonErr).Error()
			dialogErr := notifier.dialog(ctx, notification, errors.Join(err, balloonErr))
			if dialogErr != nil {
				err = errors.Join(err, balloonErr, dialogErr)
			} else {
				err = nil
				backend = "task-dialog"
				degraded = true
				branded = false
			}
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
	notifier.status.Branded = branded
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
