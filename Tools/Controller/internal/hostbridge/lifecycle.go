package hostbridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

const (
	LifecycleSessionLocked   = "session.locked"
	LifecycleSessionUnlocked = "session.unlocked"
	LifecyclePowerSuspending = "power.suspending"
	LifecyclePowerResumed    = "power.resumed"
)

func lifecycleInstruction(
	kind string,
	policy appconfig.LifecycleSafety,
) (action string, refresh bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case LifecycleSessionLocked:
		return policy.SessionLock, false
	case LifecyclePowerSuspending:
		return policy.Suspend, false
	case LifecycleSessionUnlocked, LifecyclePowerResumed:
		return "", policy.RefreshOnResume
	default:
		return "", false
	}
}

func applyLifecycleAction(
	ctx context.Context,
	action string,
	connected bool,
	actuator func(context.Context, string) error,
) error {
	if !connected || action == "" || action == appconfig.LifecycleActionLeave {
		return nil
	}
	if actuator == nil {
		return errors.New("host lifecycle actuator is unavailable")
	}
	return actuator(ctx, action)
}

func (manager *Manager) applyLifecycleAction(ctx context.Context, action string) error {
	if manager.lifecycleActuator != nil {
		return manager.lifecycleActuator(ctx, action)
	}
	switch action {
	case appconfig.LifecycleActionStopMotion:
		return errors.Join(
			manager.client.SetMotionSide(ctx, 1, controller.RelayMotionStop),
			manager.client.SetMotionSide(ctx, 2, controller.RelayMotionStop),
		)
	case appconfig.LifecycleActionAllOff:
		return manager.client.AllRelaysOff(ctx)
	case appconfig.LifecycleActionLeave, "":
		return nil
	default:
		return fmt.Errorf("unsupported host lifecycle action %q", action)
	}
}

// HandleLifecycle applies the configured fail-safe action for host lock and
// suspend transitions. Resume/unlock refreshes telemetry only when a board is
// already connected; reconnect ownership remains with the primary runtime.
func (manager *Manager) HandleLifecycle(ctx context.Context, kind string) error {
	if manager == nil {
		return errors.New("host integration manager is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	policy := manager.store.Current().Integrations.Lifecycle
	action, refresh := lifecycleInstruction(kind, policy)
	connected := manager.client.Snapshot().Connected
	if refresh {
		if !connected {
			return nil
		}
		_, err := manager.client.Status(ctx)
		if err == nil {
			manager.client.EmitHostActionEvent(
				"host.lifecycle.reconciled", "Controller telemetry refreshed after host resume",
				"host", "refresh", map[string]string{"event": kind},
			)
		}
		return err
	}
	if action == "" {
		return nil
	}
	actionErr := applyLifecycleAction(
		ctx, action, connected, manager.applyLifecycleAction,
	)
	// Clear software ownership even if the transport detached during the
	// safety request. Direct motion/all-off was attempted first, so release
	// callbacks cannot prolong movement while they drain.
	releaseErr := manager.ReleaseKeyboardContext(ctx, "host lifecycle: "+kind)
	if actionErr == nil && releaseErr == nil {
		state := "applied"
		if !connected || action == appconfig.LifecycleActionLeave {
			state = "not-required"
		}
		manager.client.EmitHostActionEvent(
			"host.lifecycle.safety", "Host lifecycle safety "+state,
			"host", action,
			map[string]string{"event": kind, "state": state},
		)
	}
	return errors.Join(actionErr, releaseErr)
}
