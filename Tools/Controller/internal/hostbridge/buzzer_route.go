package hostbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const buzzerRouteMaximumAttempts = 3

func (manager *Manager) observeBuzzerRoute(path string, reset bool) {
	path = strings.ToLower(strings.TrimSpace(path))
	snapshot := manager.currentBuzzerRouteSnapshot()
	manager.buzzerRouteMu.Lock()
	if path != manager.buzzerRoutePath {
		manager.buzzerRoutePath = path
		manager.buzzerRouteRevision++
		manager.buzzerRouteAttempts = 0
		manager.buzzerRouteError = ""
		reset = true
	}
	if reset {
		manager.buzzerRouteAttempts = 0
		manager.buzzerRouteError = ""
	}
	switch {
	case path == "":
		manager.buzzerRouteState = "unspecified"
	case !snapshot.HaveSettings:
		manager.buzzerRouteState = "pending"
	case boardSilentForBuzzerPath(path) == (snapshot.Settings.Flags&native.SettingsSilent != 0):
		manager.buzzerRouteState = "verified"
		manager.buzzerRouteAttempts = 0
		manager.buzzerRouteError = ""
	case manager.buzzerRouteState != "applying" && manager.buzzerRouteState != "error":
		manager.buzzerRouteState = "pending"
	}
	manager.buzzerRouteMu.Unlock()
	select {
	case manager.buzzerRouteWake <- struct{}{}:
	default:
	}
}

func boardSilentForBuzzerPath(path string) bool {
	return path == appconfig.BuzzerPathHost || path == appconfig.BuzzerPathNone
}

func (manager *Manager) currentBuzzerRouteSnapshot() controller.Snapshot {
	if manager.buzzerRouteSnapshot != nil {
		return manager.buzzerRouteSnapshot()
	}
	if manager.client != nil {
		return manager.client.Snapshot()
	}
	return controller.Snapshot{}
}

func (manager *Manager) buzzerRouteLoop() {
	defer manager.wait.Done()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case <-manager.buzzerRouteWake:
			manager.applyBuzzerRoute()
		}
	}
}

func (manager *Manager) applyBuzzerRoute() {
	snapshot := manager.currentBuzzerRouteSnapshot()
	manager.buzzerRouteMu.Lock()
	path := manager.buzzerRoutePath
	revision := manager.buzzerRouteRevision
	if path == "" || !snapshot.HaveSettings {
		manager.buzzerRouteMu.Unlock()
		return
	}
	desiredSilent := boardSilentForBuzzerPath(path)
	if desiredSilent == (snapshot.Settings.Flags&native.SettingsSilent != 0) {
		manager.buzzerRouteState = "verified"
		manager.buzzerRouteAttempts = 0
		manager.buzzerRouteError = ""
		manager.buzzerRouteMu.Unlock()
		return
	}
	if manager.buzzerRouteAttempts >= buzzerRouteMaximumAttempts {
		manager.buzzerRouteMu.Unlock()
		return
	}
	manager.buzzerRouteAttempts++
	attempt := manager.buzzerRouteAttempts
	manager.buzzerRouteState = "applying"
	manager.buzzerRouteError = ""
	manager.buzzerRouteMu.Unlock()

	ctx, cancel := context.WithTimeout(manager.ctx, 5*time.Second)
	var err error
	if manager.buzzerRouteExecute == nil {
		err = fmt.Errorf("board buzzer route actuator is unavailable")
	} else {
		err = manager.buzzerRouteExecute(ctx, desiredSilent)
	}
	cancel()
	verified := manager.currentBuzzerRouteSnapshot()
	if err == nil && (!verified.HaveSettings || desiredSilent != (verified.Settings.Flags&native.SettingsSilent != 0)) {
		err = fmt.Errorf("board silent-state readback did not match requested buzzer path %s", path)
	}

	manager.buzzerRouteMu.Lock()
	if revision != manager.buzzerRouteRevision || path != manager.buzzerRoutePath {
		manager.buzzerRouteMu.Unlock()
		return
	}
	if err == nil {
		manager.buzzerRouteState = "verified"
		manager.buzzerRouteAttempts = 0
		manager.buzzerRouteError = ""
		manager.buzzerRouteMu.Unlock()
		return
	}
	manager.buzzerRouteState = "error"
	manager.buzzerRouteError = err.Error()
	manager.buzzerRouteMu.Unlock()
	if attempt < buzzerRouteMaximumAttempts {
		delay := time.Duration(attempt*attempt) * 250 * time.Millisecond
		timer := time.NewTimer(delay)
		go func(expectedRevision uint64) {
			defer timer.Stop()
			select {
			case <-manager.ctx.Done():
				return
			case <-timer.C:
				manager.buzzerRouteMu.RLock()
				current := manager.buzzerRouteRevision
				manager.buzzerRouteMu.RUnlock()
				if current == expectedRevision {
					select {
					case manager.buzzerRouteWake <- struct{}{}:
					default:
					}
				}
			}
		}(revision)
	}
}
