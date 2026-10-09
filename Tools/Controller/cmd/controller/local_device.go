package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/localdevice"
	"pccontroller.local/controller/internal/productidentity"
)

type localDeviceBrowserSnapshot struct {
	Configured           bool                   `json:"configured"`
	Phase                string                 `json:"phase"`
	Power                localdevice.PowerState `json:"power"`
	HTTPReachable        bool                   `json:"http_reachable"`
	EventsOnline         bool                   `json:"events_online"`
	HaveCapabilities     bool                   `json:"have_capabilities"`
	DeviceID             string                 `json:"device_id,omitempty"`
	Name                 string                 `json:"name,omitempty"`
	Model                string                 `json:"model,omitempty"`
	Firmware             string                 `json:"firmware,omitempty"`
	UpdatedAt            time.Time              `json:"updated_at,omitempty"`
	LastEvent            localdevice.EventType  `json:"last_event,omitempty"`
	LastError            string                 `json:"last_error,omitempty"`
	BaseURL              string                 `json:"base_url,omitempty"`
	ConfigurationVersion uint64                 `json:"configuration_version,omitempty"`
}

type localDeviceHost struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *controllerapi.Client
	store  *appconfig.Store

	mu        sync.RWMutex
	manager   *localdevice.Manager
	config    appconfig.LocalDevice
	lastError string
	closeOnce sync.Once
}

func startLocalDeviceHost(
	parent context.Context,
	client *controllerapi.Client,
	store *appconfig.Store,
) *localDeviceHost {
	ctx, cancel := context.WithCancel(parent)
	host := &localDeviceHost{ctx: ctx, cancel: cancel, client: client, store: store}
	if store == nil {
		host.lastError = "configuration store is unavailable"
		return host
	}
	host.apply(store.Current().Integrations.LocalDevice)
	go func() {
		for config := range store.Subscribe(ctx) {
			host.apply(config.Integrations.LocalDevice)
		}
	}()
	return host
}

func (host *localDeviceHost) apply(config appconfig.LocalDevice) {
	host.mu.Lock()
	if host.config == config {
		host.mu.Unlock()
		return
	}
	host.config = config
	current := host.manager
	if !config.Enabled {
		host.manager = nil
		host.lastError = ""
		host.mu.Unlock()
		if current != nil {
			_ = current.Close()
		}
		return
	}
	managerConfig := localdevice.ManagerConfig{
		BaseURL:      config.BaseURL,
		EnableEvents: true,
	}
	if current != nil {
		err := current.Update(managerConfig)
		if err != nil {
			host.lastError = err.Error()
		} else {
			host.lastError = ""
		}
		host.mu.Unlock()
		if err != nil {
			host.emit("integration.local_device.error", err.Error())
		}
		return
	}
	manager, err := localdevice.NewManager(host.ctx, managerConfig, localdevice.ManagerOptions{
		UserAgent: productidentity.ProtocolToken() + "/1 LocalDevice",
	})
	if err != nil {
		host.lastError = err.Error()
		host.mu.Unlock()
		host.emit("integration.local_device.error", err.Error())
		return
	}
	host.manager = manager
	host.lastError = ""
	host.mu.Unlock()
	go host.consume(manager)
}

func (host *localDeviceHost) consume(manager *localdevice.Manager) {
	for event := range manager.Events() {
		host.mu.RLock()
		active := host.manager == manager
		host.mu.RUnlock()
		if !active {
			return
		}
		text := string(event.Event.Type)
		switch event.Event.Type {
		case localdevice.EventSnapshotUpdated:
			if event.Event.Snapshot != nil {
				text = string(event.Event.Snapshot.Power)
			}
		case localdevice.EventActionCompleted:
			if event.Event.Result != nil {
				text = string(event.Event.Result.Action)
			}
		case localdevice.EventDeviceNotice:
			text = event.Event.Notice
		}
		host.emit("integration.local_device."+string(event.Event.Type), text)
	}
}

func (host *localDeviceHost) emit(kind, text string) {
	if host.client != nil {
		host.client.EmitHostEvent(kind, text)
	}
}

func (host *localDeviceHost) activeManager() (*localdevice.Manager, error) {
	host.mu.RLock()
	defer host.mu.RUnlock()
	if !host.config.Enabled {
		return nil, errors.New("local device integration is disabled")
	}
	if host.lastError != "" {
		return nil, fmt.Errorf("local device integration is unavailable: %s", host.lastError)
	}
	if host.manager == nil {
		return nil, errors.New("local device integration is unavailable")
	}
	return host.manager, nil
}

// Status returns a scrubbed view suitable for authenticated IPC clients.
func (host *localDeviceHost) Status() any {
	host.mu.RLock()
	config := host.config
	manager := host.manager
	lastError := host.lastError
	host.mu.RUnlock()
	result := localDeviceBrowserSnapshot{
		Configured: config.Enabled,
		Phase:      "disabled",
		Power:      localdevice.PowerUnknown,
		BaseURL:    config.BaseURL,
		LastError:  lastError,
	}
	if !config.Enabled {
		return result
	}
	result.Phase = "connecting"
	if manager == nil {
		result.Phase = "unavailable"
		return result
	}
	snapshot := manager.Snapshot()
	result.HTTPReachable = snapshot.HTTPReachable
	result.EventsOnline = snapshot.EventsConnected
	result.HaveCapabilities = snapshot.HaveCapabilities
	result.UpdatedAt = snapshot.UpdatedAt
	result.LastEvent = snapshot.LastEvent
	result.ConfigurationVersion = snapshot.ConfigurationVersion
	if snapshot.HaveCapabilities {
		result.DeviceID = snapshot.Capabilities.DeviceID
		result.Name = snapshot.Capabilities.Name
		result.Model = snapshot.Capabilities.Model
		result.Firmware = snapshot.Capabilities.Firmware
	}
	if snapshot.HaveDeviceSnapshot {
		result.Power = snapshot.Device.Power
		if result.DeviceID == "" {
			result.DeviceID = snapshot.Device.DeviceID
		}
	}
	if snapshot.LastError != "" {
		result.LastError = snapshot.LastError
	}
	switch {
	case lastError != "":
		result.Phase = "unavailable"
	case snapshot.EventsConnected:
		result.Phase = "online"
	case snapshot.HTTPReachable:
		result.Phase = "http"
	case snapshot.LastError != "":
		result.Phase = "reconnecting"
	}
	return result
}

// Action dispatches only the fixed Local Device living action vocabulary. Passive
// refresh is a GET and is never translated into an upstream action.
func (host *localDeviceHost) Action(
	ctx context.Context,
	actionName string,
	message string,
	pulses int,
) (any, error) {
	manager, err := host.activeManager()
	if err != nil {
		return host.Status(), err
	}
	actionName = strings.ToLower(strings.TrimSpace(actionName))
	if actionName == "passive.refresh" {
		_, err = manager.Refresh(ctx)
	} else {
		action := localdevice.Action{Type: localdevice.ActionType(actionName)}
		switch action.Type {
		case localdevice.ActionDisplayMessage:
			action.Message = message
		case localdevice.ActionAlertPulse:
			action.Pulses = pulses
		}
		_, err = manager.Action(ctx, action)
	}
	if err != nil {
		return host.Status(), err
	}
	host.emit("integration.local_device.action", actionName)
	return host.Status(), nil
}

// Inspect exposes only the fixed scrubbed capability and snapshot projections.
func (host *localDeviceHost) Inspect(ctx context.Context, resource string) (any, error) {
	manager, err := host.activeManager()
	if err != nil {
		return nil, err
	}
	return manager.Inspect(ctx, strings.ToLower(strings.TrimSpace(resource)))
}

func (host *localDeviceHost) Close() {
	host.closeOnce.Do(func() {
		host.cancel()
		host.mu.Lock()
		manager := host.manager
		host.manager = nil
		host.mu.Unlock()
		if manager != nil {
			_ = manager.Close()
		}
	})
}
