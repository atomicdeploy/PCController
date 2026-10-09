package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/nativeshell"
	"pccontroller.local/controller/internal/productidentity"
)

func startNativeWebShell(
	ctx context.Context,
	stop context.CancelFunc,
	appURL string,
	runtime *control.Runtime,
	store *appconfig.Store,
	primary *primaryIPC,
) (nativeshell.Shell, error) {
	if runtime == nil || store == nil || primary == nil {
		return nil, errors.New("native web shell requires the primary runtime and configuration store")
	}
	if stop == nil {
		return nil, errors.New("native web shell stop callback is nil")
	}
	return nativeshell.Start(ctx, nativeshell.Options{
		Snapshot: func() nativeshell.State {
			live := runtime.Snapshot()
			return nativeshell.State{
				Title:           productidentity.Title(store.Current().UI.AppTitle),
				Connected:       live.Connected,
				Paused:          live.Paused,
				Port:            live.Port.Name,
				ConnectionState: live.ConnectionState,
			}
		},
		OpenPage: func(page string) error {
			// Keep this second check next to the side effect. It protects against
			// disconnects occurring after the native menu dispatched the action.
			if !runtime.Snapshot().Connected {
				return errors.New("controller disconnected before the page could open")
			}
			pageURL, err := nativeWebPageURL(appURL, page)
			if err != nil {
				return err
			}
			return openBrowser(pageURL)
		},
		Reconnect: func(actionContext context.Context) error {
			return runtime.Reconnect(actionContext, "native system-tray reconnect requested")
		},
		Exit: stop,
		HandleSystemEvent: func(event nativeshell.SystemEvent) {
			if hostEvent, ok := nativeSystemRuntimeEvent(event); ok {
				runtime.PublishStructuredEvent(hostEvent)
			}
			handleNativeLifecycleEvent(ctx, event, runtime, store, primary)
		},
		ReportError: func(err error) {
			runtime.PublishHostEvent("host.shell.error", err.Error())
		},
	})
}

func handleNativeLifecycleEvent(
	parent context.Context,
	event nativeshell.SystemEvent,
	runtime *control.Runtime,
	store *appconfig.Store,
	primary *primaryIPC,
) {
	safety, reconcile := nativeLifecycleMode(event.Kind)
	if !safety && !reconcile {
		return
	}
	run := func(timeout time.Duration) {
		requestContext, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		if reconcile && store.Current().Integrations.Lifecycle.RefreshOnResume {
			snapshot := runtime.Snapshot()
			if !snapshot.Connected && !snapshot.Paused {
				if err := runtime.EnsureConnected(requestContext); err != nil {
					runtime.PublishHostEvent(
						"host.lifecycle.reconcile.error",
						"controller discovery after "+string(event.Kind)+": "+err.Error(),
					)
				}
			}
		}
		if err := primary.HandleHostLifecycle(requestContext, string(event.Kind)); err != nil {
			runtime.PublishHostEvent(
				"host.lifecycle.safety.error",
				string(event.Kind)+": "+err.Error(),
			)
		}
	}
	if safety {
		// A short, synchronous deadline lets the firmware receive its stop
		// request before Windows finishes the lock/suspend transition.
		run(1500 * time.Millisecond)
		return
	}
	go run(5 * time.Second)
}

func nativeLifecycleMode(kind nativeshell.SystemEventKind) (safety, reconcile bool) {
	switch kind {
	case nativeshell.SystemEventSessionLocked,
		nativeshell.SystemEventPowerSuspending:
		return true, false
	case nativeshell.SystemEventSessionUnlocked,
		nativeshell.SystemEventPowerResumed,
		nativeshell.SystemEventNetworkChanged:
		return false, true
	default:
		return false, false
	}
}

func nativeSystemRuntimeEvent(event nativeshell.SystemEvent) (control.Event, bool) {
	hostEvent := control.Event{
		Lifecycle: "changed",
		Source:    "host.native",
	}
	switch event.Kind {
	case nativeshell.SystemEventSessionLocked:
		hostEvent.Kind = "host.session.locked"
		hostEvent.State = "locked"
		hostEvent.Text = "Host session locked"
	case nativeshell.SystemEventSessionUnlocked:
		hostEvent.Kind = "host.session.unlocked"
		hostEvent.State = "unlocked"
		hostEvent.Text = "Host session unlocked"
	case nativeshell.SystemEventPowerSuspending:
		hostEvent.Kind = "host.power.suspending"
		hostEvent.State = "suspending"
		hostEvent.Text = "Host is suspending"
	case nativeshell.SystemEventPowerResumed:
		hostEvent.Kind = "host.power.resumed"
		hostEvent.State = "resumed"
		hostEvent.Text = "Host resumed"
	case nativeshell.SystemEventNetworkChanged:
		hostEvent.Kind = "host.network.changed"
		hostEvent.State = "changed"
		hostEvent.Text = "Host network interfaces changed"
		hostEvent.Metadata = map[string]string{
			"signature":       event.NetworkSignature,
			"interface_count": strconv.Itoa(event.InterfaceCount),
			"address_count":   strconv.Itoa(event.AddressCount),
		}
	default:
		return control.Event{}, false
	}
	return hostEvent, true
}

func nativeWebPageURL(appURL, page string) (string, error) {
	page = strings.ToLower(strings.TrimSpace(page))
	switch page {
	case "dashboard", "controls", "workbench", "updates", "settings":
	default:
		return "", errors.New("native shell page is not recognized")
	}
	parsed, err := url.ParseRequestURI(appURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("native shell app URL must be an absolute HTTP(S) URL")
	}
	parsed.Fragment = "/" + page
	return parsed.String(), nil
}
