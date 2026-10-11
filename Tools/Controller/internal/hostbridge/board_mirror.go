package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/boardmirror"
	"pccontroller.local/controller/internal/fleet"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/native"
)

// Inspect uses the existing serial owner. Optional hardware is advertised only
// when its live firmware capability AND peripheral readback are available.
func (manager *Manager) MirrorInspect(ctx context.Context) (boardmirror.State, error) {
	return manager.mirrorInspect(ctx, true)
}

func (manager *Manager) MirrorObserve(ctx context.Context) (boardmirror.State, error) {
	return manager.mirrorInspect(ctx, false)
}

// Periodic observations read push-capable display state only from the one
// authoritative runtime cache. Status/PWM and optional LCD have no changed-only
// push contract; their labelled legacy fallback is bounded to 250 ms.
func (manager *Manager) mirrorInspect(ctx context.Context, fresh bool) (boardmirror.State, error) {
	manager.mirrorObserveMu.Lock()
	defer manager.mirrorObserveMu.Unlock()
	config := manager.store.Current().Integrations.BoardMirror
	state := boardmirror.State{BoardID: config.LocalBoardID, Values: map[string]fleet.MirrorCommand{}, Unsupported: []string{"motion: coordinated safety/lease not commissioned", "raw_i2c: no device-specific write policy", "physical_inputs: observations only, never injected readings", "segments_brightness: local physical policy"}}
	snapshot := manager.client.Snapshot()
	state.Connected, state.Generation = snapshot.Connected, snapshot.ConnectionGeneration
	state.SegmentRevision = manager.client.SegmentRevision()
	if !snapshot.Connected {
		return state, nil
	}
	if manager.mirrorObservedGeneration != state.Generation {
		manager.mirrorPWM = native.PWMValues{}
	}
	legacyDue := fresh || manager.mirrorObservedGeneration != state.Generation || time.Since(manager.mirrorLegacyReadAt) >= 250*time.Millisecond
	status := snapshot.Status
	if legacyDue || !snapshot.HaveStatus {
		var err error
		status, err = manager.client.Status(ctx)
		if err != nil {
			return state, err
		}
	}
	state.Observations = map[string]any{"status": status}
	state.ObservationPaths = map[string]string{"menu": "bounded_legacy_readback", "relay": "bounded_legacy_readback", "pwm": "bounded_legacy_readback"}
	hello := snapshot.Hello
	if hello.BuildFeatures&native.BuildFeatureVolatileMenu != 0 {
		state.Values["menu"] = fleet.MirrorCommand{Kind: "menu", Page: status.MenuPage}
	}
	if hello.Capabilities&native.CapabilityRelayMotion != 0 {
		for relay := 5; relay <= 8; relay++ {
			command := fleet.MirrorCommand{Kind: "relay", Channel: relay, Active: status.ActiveRelays&(1<<uint(relay-1)) != 0}
			state.Values[command.Key()] = command
		}
	}
	if hello.Capabilities&native.CapabilityPWM != 0 && status.PWMAvailable {
		if legacyDue || !manager.mirrorPWM.Available {
			values, err := manager.client.PWMValues(ctx)
			if err != nil {
				return state, err
			}
			manager.mirrorPWM = values
		}
		values := manager.mirrorPWM
		if values.Available {
			for channel, value := range values.Values {
				command := fleet.MirrorCommand{Kind: "pwm", Channel: channel, Value: value}
				state.Values[command.Key()] = command
			}
		}
	} else {
		manager.mirrorPWM = native.PWMValues{}
	}
	if hello.Capabilities&native.CapabilityFrontPanelSnapshot != 0 {
		panel := manager.client.Snapshot().FrontPanel
		if mirrorReadPanel(snapshot, fresh, legacyDue, mirrorIncludesLCD(config)) {
			var err error
			panel, err = manager.client.RefreshFrontPanel(ctx)
			if err != nil {
				return state, err
			}
		}
		state.Observations["front_panel"] = panel
		state.ObservationPaths["segments"] = "bounded_legacy_readback"
		if hello.Capabilities&native.CapabilitySegmentPush != 0 {
			state.ObservationPaths["segments"] = "firmware_push"
		}
		state.ObservationPaths["lcd"] = "initial_snapshot"
		if mirrorIncludesLCD(config) {
			state.ObservationPaths["lcd"] = "bounded_legacy_readback"
		}
		if hello.Capabilities&native.CapabilityScheduledSegments != 0 && (!panel.TM1637DetectionKnown || panel.TM1637Detected) {
			state.Values["segments"] = fleet.MirrorCommand{Kind: "segments", Cells: panel.RawSegments}
		}
		if panel.LCDAvailable {
			state.Values["lcd"] = fleet.MirrorCommand{Kind: "lcd", Lines: [2]string{panel.LCDLine1, panel.LCDLine2}}
		}
	}
	snapshot = manager.client.Snapshot()
	if !snapshot.Connected || snapshot.ConnectionGeneration != state.Generation {
		return state, errors.New("connection changed during mirror observation")
	}
	if legacyDue {
		manager.mirrorLegacyReadAt, manager.mirrorObservedGeneration = time.Now(), state.Generation
	}
	if hello.Capabilities&native.CapabilityStatusLEDPush != 0 && snapshot.HaveStatusLED {
		led := snapshot.StatusLED
		state.Observations["status_led"] = led
		state.ObservationPaths["status_led"] = "firmware_push"
		// Safety/animated compositors remain board-local. Never translate a
		// red safety override into the base color of another healthy board.
		if mirrorStaticRGBAvailable(led) {
			// These are physical, already-scaled bytes. Full-scale receiver
			// brightness preserves them without applying the source scale twice.
			state.Values["status_led"] = fleet.MirrorCommand{Kind: "status_led", RGB: [3]byte{led.Red, led.Green, led.Blue}, Brightness: 255}
		}
	}
	if hello.Capabilities&native.CapabilityBuzzerPush != 0 {
		state.Values["buzzer"] = fleet.MirrorCommand{Kind: "buzzer", DurationMS: 1}
	}
	state.SegmentRevision = manager.client.SegmentRevision()
	return state, nil
}

func mirrorReadPanel(snapshot controller.Snapshot, fresh, legacyDue, lcd bool) bool {
	return fresh || !snapshot.HaveFrontPanel || (legacyDue && (snapshot.Hello.Capabilities&native.CapabilitySegmentPush == 0 || lcd))
}

func mirrorStaticRGBAvailable(led native.StatusLEDState) bool {
	if led.Effect != native.StatusEffectNone {
		return false
	}
	switch led.Condition {
	case native.StatusConditionOff, native.StatusConditionReady, native.StatusConditionCustom,
		native.StatusConditionBluetoothConnected, native.StatusConditionBluetoothOff, 0xff: // firmware ManualCondition
		return true
	}
	return false // boot, local safety, running state and transient cues stay local
}

func mirrorIncludesLCD(config fleet.MirrorConfig) bool {
	hasLCD := func(policy fleet.MirrorPolicy) bool {
		for _, domain := range policy.Domains {
			if domain == "lcd" {
				return true
			}
		}
		return false
	}
	if hasLCD(config.Receive) {
		return true
	}
	for _, group := range config.Groups {
		if hasLCD(group.Policy) {
			return true
		}
	}
	return false
}

func (manager *Manager) MirrorApply(ctx context.Context, write boardmirror.Write) (boardmirror.Result, error) {
	manager.mirrorWriteMu.Lock()
	defer manager.mirrorWriteMu.Unlock()
	config := manager.store.Current().Integrations.BoardMirror
	result := boardmirror.Result{RouteID: write.Route.RouteID, BoardID: config.LocalBoardID}
	if err := validateMirrorWrite(write, config, time.Now()); err != nil {
		return result, err
	}
	ctx, cancel := context.WithDeadline(ctx, write.Route.Deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	snapshot := manager.client.Snapshot()
	if !snapshot.Connected || snapshot.ConnectionGeneration != write.Generation {
		return result, errors.New("mirror target disconnected or connection generation changed")
	}
	if manager.mirrorReplay == nil {
		manager.mirrorReplay, _ = fleet.NewReplayCache(1024, time.Minute)
	}
	record, fresh, err := manager.mirrorReplay.Begin(write.Route.RouteID, time.Now())
	if err != nil {
		return result, err
	}
	if !fresh {
		if record.State != fleet.ReplayComplete {
			return result, errors.New("mirror route is already in progress")
		}
		if err := json.Unmarshal(record.Result, &result); err != nil {
			return result, err
		}
		return result, nil
	}
	// One coordinator owns receive ordering. A new process epoch must be newer;
	// stale/out-of-order requests are rejected, not executed then hidden.
	position := write.Route.Coordinator
	if position.Epoch < manager.mirrorEpoch || (position.Epoch == manager.mirrorEpoch && position.Sequence <= manager.mirrorSequence) {
		result.Error = "stale coordinator position"
	} else {
		manager.mirrorEpoch, manager.mirrorSequence = position.Epoch, position.Sequence
		state, inspectErr := manager.MirrorInspect(ctx)
		if inspectErr != nil {
			result.Error = inspectErr.Error()
		} else if _, ok := state.Values[write.Command.Key()]; !ok {
			result.Error = "mirror domain unavailable on target"
		} else {
			applyErr := manager.applyMirrorCommand(ctx, write.Command, write.SegmentRevision)
			result.Acknowledged = applyErr == nil
			if applyErr != nil {
				result.Error = applyErr.Error()
			} else if write.Command.Kind != "buzzer" {
				// ACK alone is not evidence of applied physical output.
				for {
					state, readErr := manager.MirrorInspect(ctx)
					if readErr != nil {
						result.Error = readErr.Error()
						break
					}
					if state.Generation != write.Generation {
						result.Error = "generation changed during mirror write"
						break
					}
					if actual, ok := state.Values[write.Command.Key()]; ok && actual == write.Command {
						if write.Command.Kind == "segments" && state.SegmentRevision != write.SegmentRevision {
							result.Error = "new local panel intention superseded mirror verification"
							break
						}
						result.Verified = true
						result.SegmentRevision = state.SegmentRevision
						break
					}
					select {
					case <-ctx.Done():
						result.Error = "mirror readback not verified: " + ctx.Err().Error()
					case <-time.After(20 * time.Millisecond):
						continue
					}
					break
				}
			}
		}
	}
	encoded, _ := json.Marshal(result)
	if err := manager.mirrorReplay.Complete(write.Route.RouteID, encoded, time.Now()); err != nil {
		return result, err
	}
	return result, nil
}

func validateMirrorWrite(write boardmirror.Write, config fleet.MirrorConfig, now time.Time) error {
	if err := write.Route.ValidateAt(now); err != nil {
		return err
	}
	if write.Route.Deadline.Sub(now) > 3*time.Second {
		return errors.New("mirror deadline must be within three seconds")
	}
	if write.Route.Target.BoardID == "" || write.Route.Target.BoardID != config.LocalBoardID {
		return errors.New("mirror target board identity mismatch")
	}
	if write.Route.OriginBoardID == "" || write.Route.OriginBoardID == config.LocalBoardID {
		return errors.New("mirror source must be a different identified board")
	}
	if write.Route.Coordinator.ID == "" || write.Route.Coordinator.ID != config.ReceiveFrom || write.Route.OriginInstanceID != config.ReceiveFrom {
		return errors.New("mirror coordinator is not allowed by receive_from")
	}
	if write.Generation == 0 {
		return errors.New("mirror requires an observed connection generation")
	}
	if !config.Receive.Allows(write.Command) {
		return errors.New("mirror domain/channel is not allowed by target receive policy")
	}
	encoded, _ := json.Marshal(write.Command)
	if write.Route.Intent.Domain != write.Command.Kind || write.Route.Intent.Name != "mirror" || len(write.Route.Intent.Arguments) != 1 || !jsonEqual(write.Route.Intent.Arguments["command"], encoded) {
		return errors.New("route intent and mirror command differ")
	}
	return nil
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}

func (manager *Manager) applyMirrorCommand(ctx context.Context, command fleet.MirrorCommand, segmentRevision uint64) error {
	switch command.Kind {
	case "menu":
		return manager.client.SetVolatileMenuPage(ctx, command.Page)
	case "segments":
		return manager.client.PresentMirrorSegments(ctx, command.Cells, segmentRevision)
	case "lcd":
		_, err := manager.client.PresentDisplay(ctx, controller.DisplayRequest{Target: "lcd", Text: fmt.Sprintf("%-16s%-16s", command.Lines[0], command.Lines[1])})
		return err
	case "status_led":
		return manager.client.SetStatusRGB(ctx, command.RGB[0], command.RGB[1], command.RGB[2], command.Brightness)
	case "relay":
		return manager.client.SetRelay(ctx, byte(command.Channel), command.Active)
	case "pwm":
		return manager.client.SetPWMChannel(ctx, byte(command.Channel), command.Value)
	case "buzzer":
		return manager.client.PlayTone(ctx, command.FrequencyHz, command.DurationMS)
	}
	return errors.New("unsupported mirror command")
}

func (manager *Manager) MirrorStatus() []boardmirror.GroupStatus { return manager.boardMirror.Status() }
func (manager *Manager) MirrorResync(ctx context.Context, group string, origin fleet.BoardID) ([]boardmirror.Result, error) {
	return manager.boardMirror.Resync(ctx, group, origin)
}

type mirrorAdapter struct{ manager *Manager }

func (adapter mirrorAdapter) Inspect(ctx context.Context, member fleet.MirrorMember) (boardmirror.State, error) {
	if member.Peer == "" {
		return adapter.manager.MirrorObserve(ctx)
	}
	var result boardmirror.State
	err := adapter.call(ctx, member.Peer, "controller.mirror.inspect", map[string]bool{"cached": true}, &result)
	return result, err
}
func (adapter mirrorAdapter) Apply(ctx context.Context, member fleet.MirrorMember, write boardmirror.Write) (boardmirror.Result, error) {
	if member.Peer == "" {
		return adapter.manager.MirrorApply(ctx, write)
	}
	var result boardmirror.Result
	err := adapter.call(ctx, member.Peer, "controller.mirror.apply", write, &result)
	return result, err
}
func (adapter mirrorAdapter) call(ctx context.Context, name, method string, params, result any) error {
	manager := adapter.manager
	manager.mu.RLock()
	peer := manager.peers[strings.ToLower(name)]
	manager.mu.RUnlock()
	if peer == nil {
		return errors.New("mirror peer is not configured")
	}
	peer.mu.RLock()
	session, allowed := peer.session, peer.allowCommands
	peer.mu.RUnlock()
	if session == nil {
		return errors.New("mirror peer is not connected")
	}
	if method == "controller.mirror.apply" && !allowed {
		return errors.New("mirror peer commands are disabled")
	}
	encoded, _ := json.Marshal(params)
	response, err := session.Call(ctx, ipcjson.Request{JSONRPC: ipcjson.Version, Method: method, Params: encoded})
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	encoded, err = json.Marshal(response.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func (manager *Manager) observeBoardMirror(event controller.Event) bool {
	if manager.boardMirror != nil {
		switch event.Kind {
		case "front_panel.segment", "status_led.changed", "pwm.changed", "connection":
			manager.boardMirror.NotifyState()
		}
	}
	if manager.boardMirror == nil || event.Kind != "buzzer.note" {
		return true
	}
	frequency, err := strconv.ParseUint(event.Metadata["frequency_hz"], 10, 16)
	if err != nil {
		return true
	}
	duration, err := strconv.ParseUint(event.Metadata["duration_ms"], 10, 16)
	if err != nil {
		return true
	}
	if frequency == 0 && duration == 0 {
		duration = 1
	}
	config := manager.store.Current().Integrations.BoardMirror
	board := config.LocalBoardID
	if ingress := event.Metadata["bridge.ingress"]; ingress != "" {
		board = ""
		for _, group := range config.Groups {
			for _, member := range group.Members {
				if strings.EqualFold(member.Peer, ingress) {
					board = member.BoardID
				}
			}
		}
	}
	if board != "" {
		return manager.boardMirror.ObserveNote(board, fleet.MirrorCommand{Kind: "buzzer", FrequencyHz: uint16(frequency), DurationMS: uint16(duration)})
	}
	return true
}
