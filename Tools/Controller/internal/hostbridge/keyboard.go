package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

// keyboardOperation is a side-effect-free action plan. Keeping planning
// separate lets tests verify every binding without opening a serial port.
type keyboardOperation struct {
	kind         string
	behavior     string
	down         bool
	side         byte
	motion       controller.RelayMotion
	relay        byte
	channel      byte
	value        uint16
	releaseValue uint16
	active       bool
}

type keyboardLatch struct {
	binding   string
	operation keyboardOperation
	started   time.Time
}

// KeyboardCommand exposes safe lifecycle controls through the shared command
// engine, so TUI, CLI, JSON-RPC, REST bridges, and secondary processes all use
// the same primary hook owner.
func (manager *Manager) KeyboardCommand(
	_ context.Context,
	args []string,
) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: keyboard status|list|enable|disable|stop")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		manager.keyboardLatchMu.Lock()
		latched := len(manager.keyboardLatches)
		manager.keyboardLatchMu.Unlock()
		encoded, err := json.MarshalIndent(struct {
			Configured bool                  `json:"configured"`
			Latched    int                   `json:"latched_outputs"`
			Hook       hostui.KeyboardStatus `json:"hook"`
		}{
			Configured: manager.store.Current().Integrations.Keyboard.Enabled,
			Latched:    latched, Hook: manager.KeyboardStatus(),
		}, "", "  ")
		return string(encoded), err
	case "list":
		encoded, err := json.MarshalIndent(
			manager.store.Current().Integrations.Keyboard,
			"",
			"  ",
		)
		return string(encoded), err
	case "enable":
		_, err := manager.store.Update(func(config *appconfig.Config) error {
			config.Integrations.Keyboard.Enabled = true
			return nil
		})
		if err != nil {
			return "", err
		}
		return "keyboard control enabled; use keyboard status to confirm the hook", nil
	case "disable":
		releaseErr := manager.ReleaseKeyboard("keyboard-disable")
		_, err := manager.store.Update(func(config *appconfig.Config) error {
			config.Integrations.Keyboard.Enabled = false
			return nil
		})
		if err != nil {
			return "", errors.Join(releaseErr, err)
		}
		return "keyboard control disabled; release was attempted for every held/latched output", releaseErr
	case "stop":
		if err := manager.ReleaseKeyboard("keyboard-stop-command"); err != nil {
			return "", err
		}
		return "keyboard-held and latched outputs released", nil
	default:
		return "", errors.New("usage: keyboard status|list|enable|disable|stop")
	}
}

func keyboardBindings(
	config appconfig.KeyboardControl,
) ([]hostui.KeyboardBinding, map[string]appconfig.KeyboardControlBinding) {
	bindings := make([]hostui.KeyboardBinding, 0, len(config.Bindings))
	configured := make(
		map[string]appconfig.KeyboardControlBinding,
		len(config.Bindings),
	)
	for _, binding := range config.Bindings {
		if !binding.Enabled {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(binding.Name))
		bindings = append(bindings, hostui.KeyboardBinding{
			Name: binding.Name,
			Key:  binding.Key,
		})
		configured[name] = binding
	}
	return bindings, configured
}

func planKeyboardOperation(
	binding appconfig.KeyboardControlBinding,
	event hostui.KeyboardEvent,
) (keyboardOperation, bool, error) {
	action := binding.Primary
	if event.Control && binding.Control != nil {
		action = *binding.Control
	}
	operation := keyboardOperation{
		kind:     strings.ToLower(strings.TrimSpace(action.Type)),
		behavior: strings.ToLower(strings.TrimSpace(action.Behavior)),
		down:     event.Down, relay: action.Relay, channel: action.Channel,
		value: action.Value, releaseValue: action.ReleaseValue,
		active: action.Active,
	}
	if !event.Down && operation.behavior != "momentary" {
		return operation, false, nil
	}
	switch operation.kind {
	case "motion":
		if strings.EqualFold(action.Side, "A") {
			operation.side = 1
		} else if strings.EqualFold(action.Side, "B") {
			operation.side = 2
		} else {
			return keyboardOperation{}, false, fmt.Errorf("unknown motion side %q", action.Side)
		}
		if !event.Down {
			operation.motion = controller.RelayMotionStop
		} else if strings.EqualFold(action.Direction, "up") {
			operation.motion = controller.RelayMotionUp
		} else if strings.EqualFold(action.Direction, "down") {
			operation.motion = controller.RelayMotionDown
		} else {
			return keyboardOperation{}, false, fmt.Errorf(
				"unknown motion direction %q", action.Direction,
			)
		}
	case "relay", "pwm":
	default:
		return keyboardOperation{}, false, fmt.Errorf("unknown keyboard action %q", action.Type)
	}
	return operation, true, nil
}

func (manager *Manager) handleKeyboard(
	configured map[string]appconfig.KeyboardControlBinding,
	event hostui.KeyboardEvent,
) error {
	if event.FailSafe {
		metadata := map[string]string{
			"reason": event.Reason,
			"type":   "fail-safe",
		}
		manager.client.EmitHostActionEvent(
			"keyboard.fail-safe",
			"keyboard fail-safe: "+event.Reason,
			"pc-keyboard",
			"release",
			metadata,
		)
		ctx, cancel := context.WithTimeout(manager.ctx, 5*time.Second)
		defer cancel()
		if err := manager.releaseKeyboardLatches(ctx, event.Reason); err != nil {
			manager.emitKeyboardError("fail-safe", err, metadata)
			return err
		}
		return nil
	}
	binding, ok := configured[strings.ToLower(strings.TrimSpace(event.Binding.Name))]
	if !ok {
		return fmt.Errorf("keyboard binding %q disappeared during dispatch", event.Binding.Name)
	}
	operation, execute, err := planKeyboardOperation(binding, event)
	metadata := map[string]string{
		"binding": binding.Name,
		"key":     event.Binding.Key,
		"edge":    map[bool]string{true: "down", false: "up"}[event.Down],
		"control": strconv.FormatBool(event.Control),
		"reason":  event.Reason,
	}
	if err == nil {
		metadata["type"] = operation.kind
		metadata["behavior"] = operation.behavior
	}
	manager.client.EmitHostActionEvent(
		"keyboard.input",
		fmt.Sprintf("%s %s (%s)", binding.Name, metadata["edge"], event.Reason),
		"pc-keyboard",
		operation.kind,
		metadata,
	)
	if err != nil {
		manager.emitKeyboardError(binding.Name, err, metadata)
		return err
	}
	if !execute {
		return nil
	}
	if !manager.client.Snapshot().Connected {
		err = errors.New("board is disconnected; keyboard action was not sent")
		manager.emitKeyboardError(binding.Name, err, metadata)
		return err
	}
	ctx, cancel := context.WithTimeout(manager.ctx, 2*time.Second)
	defer cancel()
	err = manager.executeKeyboardOperation(ctx, binding.Name, operation)
	if err != nil {
		manager.emitKeyboardError(binding.Name, err, metadata)
		return err
	}
	manager.client.EmitHostActionEvent(
		"keyboard.control",
		fmt.Sprintf("%s %s applied", binding.Name, metadata["edge"]),
		"pc-keyboard",
		operation.kind,
		metadata,
	)
	return nil
}

func (manager *Manager) executeKeyboardOperation(
	ctx context.Context,
	binding string,
	operation keyboardOperation,
) error {
	resource := operation.keyboardResource()
	if operation.behavior == "latch" {
		return manager.executeKeyboardLatch(ctx, binding, resource, operation)
	}
	// Any non-latch command for the same output supersedes a keyboard latch.
	// In particular, the opposite direction stops the previous motion before
	// the new direction is allowed to start.
	if resource != "" {
		if err := manager.releaseKeyboardResource(ctx, resource); err != nil {
			return err
		}
	}
	return manager.executeKeyboardDirect(ctx, operation)
}

func (manager *Manager) executeKeyboardDirect(
	ctx context.Context,
	operation keyboardOperation,
) error {
	if manager.keyboardActuator != nil {
		return manager.keyboardActuator(ctx, operation)
	}
	switch operation.kind {
	case "motion":
		return manager.client.SetMotionSide(ctx, operation.side, operation.motion)
	case "relay":
		switch operation.behavior {
		case "momentary":
			return manager.client.SetRelay(ctx, operation.relay, operation.down)
		case "toggle":
			_, err := manager.client.ToggleRelay(ctx, operation.relay)
			return err
		case "latch":
			return manager.client.SetRelay(ctx, operation.relay, operation.active)
		}
	case "pwm":
		switch operation.behavior {
		case "momentary":
			value := operation.releaseValue
			if operation.down {
				value = operation.value
			}
			return manager.client.SetPWMChannel(ctx, operation.channel, value)
		case "toggle":
			values, err := manager.client.PWMValues(ctx)
			if err != nil {
				return fmt.Errorf("query PWM channel for toggle: %w", err)
			}
			value := operation.releaseValue
			if values.Values[operation.channel] == operation.releaseValue {
				value = operation.value
			}
			return manager.client.SetPWMChannel(ctx, operation.channel, value)
		case "latch":
			return manager.client.SetPWMChannel(ctx, operation.channel, operation.value)
		}
	}
	return fmt.Errorf(
		"unsupported keyboard operation %s/%s",
		operation.kind,
		operation.behavior,
	)
}

func (operation keyboardOperation) keyboardResource() string {
	switch operation.kind {
	case "motion":
		return fmt.Sprintf("motion:%d", operation.side)
	case "relay":
		return fmt.Sprintf("relay:%d", operation.relay)
	case "pwm":
		return fmt.Sprintf("pwm:%d", operation.channel)
	default:
		return ""
	}
}

func (operation keyboardOperation) latchActive() bool {
	switch operation.kind {
	case "motion":
		return operation.motion != controller.RelayMotionStop
	case "relay":
		return operation.active
	case "pwm":
		return operation.value != operation.releaseValue
	default:
		return false
	}
}

func (operation keyboardOperation) releaseOperation() keyboardOperation {
	release := operation
	release.behavior = "momentary"
	release.down = false
	switch release.kind {
	case "motion":
		release.motion = controller.RelayMotionStop
	case "relay":
		release.active = false
	case "pwm":
		release.value = release.releaseValue
	}
	return release
}

func (manager *Manager) executeKeyboardLatch(
	ctx context.Context,
	binding, resource string,
	operation keyboardOperation,
) error {
	manager.keyboardLatchMu.Lock()
	current, exists := manager.keyboardLatches[resource]
	if exists {
		delete(manager.keyboardLatches, resource)
	}
	manager.keyboardLatchMu.Unlock()
	if exists {
		if err := manager.executeKeyboardDirect(ctx, current.operation.releaseOperation()); err != nil {
			manager.keyboardLatchMu.Lock()
			manager.keyboardLatches[resource] = current
			manager.keyboardLatchMu.Unlock()
			return fmt.Errorf("release existing %s latch: %w", resource, err)
		}
		// Pressing the same Ctrl binding a second time is the explicit stop.
		if strings.EqualFold(current.binding, binding) {
			return nil
		}
	}
	if !operation.latchActive() {
		return manager.executeKeyboardDirect(ctx, operation)
	}
	if err := manager.executeKeyboardDirect(ctx, operation); err != nil {
		return err
	}
	manager.keyboardLatchMu.Lock()
	manager.keyboardLatches[resource] = keyboardLatch{
		binding: binding, operation: operation, started: time.Now(),
	}
	manager.keyboardLatchMu.Unlock()
	return nil
}

// observeKeyboardStatus relinquishes ownership when a live telemetry reply
// proves that a relay/motion latch was stopped or superseded by another host,
// RF, or physical action. A settling window protects direction reversals.
func (manager *Manager) observeKeyboardStatus(
	status controller.Status,
	now time.Time,
) {
	const settle = 500 * time.Millisecond
	manager.keyboardLatchMu.Lock()
	defer manager.keyboardLatchMu.Unlock()
	for resource, latch := range manager.keyboardLatches {
		if now.Sub(latch.started) < settle {
			continue
		}
		operation := latch.operation
		matches := true
		switch operation.kind {
		case "motion":
			directionBit, enableBit := byte(0), byte(1)
			if operation.side == 2 {
				directionBit, enableBit = 2, 3
			}
			enabled := status.ActiveRelays&(1<<enableBit) != 0
			reverse := status.ActiveRelays&(1<<directionBit) != 0
			matches = enabled &&
				reverse == (operation.motion == controller.RelayMotionDown)
		case "relay":
			matches = (status.ActiveRelays&(1<<(operation.relay-1)) != 0) == operation.active
		case "pwm":
			if !status.PWMAvailable || status.PWMChannel != operation.channel {
				continue
			}
			matches = status.PWMValue == operation.value
		}
		if !matches {
			delete(manager.keyboardLatches, resource)
		}
	}
}

// keyboardPWMQueryDue requests the full 16-channel state only while a mature
// latch exists outside STATUS's selected channel. This closes the external
// ownership gap without doubling every normal telemetry poll.
func (manager *Manager) keyboardPWMQueryDue(status controller.Status, now time.Time) bool {
	const interval = 750 * time.Millisecond
	const settle = 500 * time.Millisecond
	manager.keyboardLatchMu.Lock()
	defer manager.keyboardLatchMu.Unlock()
	if !manager.lastPWMReconcile.IsZero() && now.Sub(manager.lastPWMReconcile) < interval {
		return false
	}
	for _, latch := range manager.keyboardLatches {
		operation := latch.operation
		if operation.kind != "pwm" || now.Sub(latch.started) < settle {
			continue
		}
		if status.PWMAvailable && status.PWMChannel == operation.channel {
			continue
		}
		manager.lastPWMReconcile = now
		return true
	}
	return false
}

func (manager *Manager) observeKeyboardPWMValues(values controller.PWMValues, now time.Time) {
	const settle = 500 * time.Millisecond
	manager.keyboardLatchMu.Lock()
	defer manager.keyboardLatchMu.Unlock()
	for resource, latch := range manager.keyboardLatches {
		operation := latch.operation
		if operation.kind != "pwm" || now.Sub(latch.started) < settle {
			continue
		}
		if !values.Available || values.Values[operation.channel] != operation.value {
			delete(manager.keyboardLatches, resource)
		}
	}
}

func (manager *Manager) releaseKeyboardResource(
	ctx context.Context,
	resource string,
) error {
	manager.keyboardLatchMu.Lock()
	current, exists := manager.keyboardLatches[resource]
	if exists {
		delete(manager.keyboardLatches, resource)
	}
	manager.keyboardLatchMu.Unlock()
	if !exists {
		return nil
	}
	if err := manager.executeKeyboardDirect(ctx, current.operation.releaseOperation()); err != nil {
		manager.keyboardLatchMu.Lock()
		manager.keyboardLatches[resource] = current
		manager.keyboardLatchMu.Unlock()
		return fmt.Errorf("release %s latch: %w", resource, err)
	}
	return nil
}

func (manager *Manager) releaseKeyboardLatches(
	ctx context.Context,
	reason string,
) error {
	manager.keyboardLatchMu.Lock()
	resources := make([]string, 0, len(manager.keyboardLatches))
	for resource := range manager.keyboardLatches {
		resources = append(resources, resource)
	}
	manager.keyboardLatchMu.Unlock()
	sort.Strings(resources)
	var result error
	for _, resource := range resources {
		if err := manager.releaseKeyboardResource(ctx, resource); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return fmt.Errorf("%s: %w", reason, result)
	}
	return nil
}

func (manager *Manager) emitKeyboardError(
	binding string,
	err error,
	metadata map[string]string,
) {
	message := binding + ": " + err.Error()
	manager.mu.Lock()
	manager.status.LastError = "keyboard: " + message
	manager.mu.Unlock()
	manager.client.EmitHostActionEvent(
		"keyboard.control.error",
		message,
		"pc-keyboard",
		metadata["type"],
		metadata,
	)
}
