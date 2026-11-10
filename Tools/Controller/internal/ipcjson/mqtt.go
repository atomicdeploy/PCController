package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/mqttbridge"
)

type mqttRuntime struct {
	sync.Mutex
	status mqttbridge.Status
}

// StartMQTT is called by the primary host, never by an attached GUI or bridge.
// A settings change revokes and joins the old session before a new one dials.
func (s *Service) StartMQTT(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var updates <-chan appconfig.Config
		if s.SubscribeHostConfig != nil {
			updates = s.SubscribeHostConfig(ctx)
		}
		config := s.hostConfig().Integrations.MQTT
		var stop context.CancelFunc
		var stopped chan struct{}
		shutdown := func() {
			if stop != nil {
				stop()
				<-stopped
				stop = nil
			}
		}
		defer shutdown()
		report := func(status mqttbridge.Status) {
			s.mqttState.Lock()
			previous := s.mqttState.status
			s.mqttState.status = status
			s.mqttState.Unlock()
			if s.Client != nil && (previous.Enabled != status.Enabled || previous.Connected != status.Connected || status.Error != "" && previous.Error != status.Error) {
				message := "MQTT broker disconnected"
				if status.Connected {
					message = "MQTT broker connected"
				}
				if !status.Enabled {
					message = "MQTT disabled"
				}
				s.Client.EmitHostEvent("mqtt.connection", message)
			}
		}
		for {
			report(mqttbridge.Status{Available: mqttbridge.Available, Enabled: config.Enabled})
			if mqttbridge.Available && config.Enabled && s.Client != nil {
				var session context.Context
				session, stop = context.WithCancel(ctx)
				stopped = make(chan struct{})
				go func(config mqttbridge.Config, stopped chan struct{}) {
					defer close(stopped)
					s.runMQTT(session, config, report)
				}(config, stopped)
			}
			for {
				select {
				case <-ctx.Done():
					return
				case next, ok := <-updates:
					if !ok {
						return
					}
					if next.Integrations.MQTT == config {
						continue
					}
					shutdown()
					config = next.Integrations.MQTT
				}
				break
			}
		}
	}()
	return done
}

func (s *Service) mqttIdentity(snapshot controller.Snapshot) mqttbridge.Identity {
	profile, _ := describeBoardProfile(s.hostConfig(), snapshot)
	return mqttbridge.Identity{BoardID: profile.BoardIdentity, BootID: s.HostInstanceID + ":" + strconv.FormatUint(snapshot.ConnectionGeneration, 10)}
}

func (s *Service) mqttObservation() (mqttbridge.Identity, any) {
	snapshot := s.controllerSnapshot()
	config := s.hostConfig()
	profile, board := describeBoardProfile(config, snapshot.Snapshot)
	state := map[string]any{
		"connected": snapshot.Connected, "connection_generation": snapshot.ConnectionGeneration,
		"host_instance_id": s.HostInstanceID,
	}
	if snapshot.Connected {
		state["emergency_stop"] = snapshot.EmergencyStop
		state["program_state"] = snapshot.ProgramState
		if snapshot.HaveFrontPanel {
			state["front_panel"] = snapshot.FrontPanel
		}
		if snapshot.PWMValues != nil && snapshot.PWMValues.Available {
			state["pwm_values"] = snapshot.PWMValues
		}
		if profile.Configured {
			_, controls := appconfig.ProfileDescriptors(board.Mode, board.ExposeRawRelays, config.UI.PeripheralNames, board.Presentation)
			state["controls"] = controls
			state["control_owner"] = controlIntentOwner{HostInstanceID: s.HostInstanceID, Generation: snapshot.ConnectionGeneration, BoardIdentity: profile.BoardIdentity, Revision: profile.Revision, PWMRevision: appconfig.PWMConfigurationRevision(config.UI)}
		}
	}
	freshness := time.Duration(config.UI.MeasurementFreshnessMS) * time.Millisecond
	if snapshot.Connected && snapshot.HaveStatus && !snapshot.StatusUpdated.IsZero() && time.Since(snapshot.StatusUpdated) <= freshness {
		status := snapshot.Status
		state["relay_mask"], state["door_open"], state["mode"] = status.ActiveRelays, status.DoorOpen, status.ProgramMode
		state["status_updated"] = snapshot.StatusUpdated
		if status.INA219Available && status.INA219ValuesValid() {
			state["supply_mv"], state["bus_mv"], state["current_ma"], state["power_mw"] = status.SupplyMV, status.BusMV, status.CurrentMA, status.PowerMW
		}
		if status.TLEDAvailable && status.TLEDValueValid() {
			state["temperature_led_c"] = float64(status.TLEDCenti) / 100
		}
		if status.TBTAvailable && status.TBTValueValid() {
			state["temperature_bt_audio_c"] = float64(status.TBTCenti) / 100
		}
	}
	return mqttbridge.Identity{BoardID: profile.BoardIdentity, BootID: s.HostInstanceID + ":" + strconv.FormatUint(snapshot.ConnectionGeneration, 10)}, state
}

func (s *Service) runMQTT(ctx context.Context, config mqttbridge.Config, report func(mqttbridge.Status)) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := make(chan struct{}, 1)
	events := make(chan any, 16)
	var watchers sync.WaitGroup
	defer func() { cancel(); watchers.Wait() }()
	for _, stream := range []string{"state", "telemetry", "activity"} {
		after := s.Client.LatestEventID()
		watchers.Go(func() {
			for {
				event, err := s.Client.NextEventStream(ctx, after, "", stream)
				if err != nil {
					return
				}
				after = event.ID
				if stream != "activity" {
					select {
					case state <- struct{}{}:
					default:
					}
				}
				// Export board/runtime events, not arbitrary logs, RF codes,
				// file paths, host diagnostics or inbound text.
				switch event.Kind {
				case "connection", "connection.progress", "door", "relay", "pwm.changed", "front_panel.changed", "buzzer.note", "illumination.changed", "emergency_stop", "program.state":
					notice := map[string]any{"type": "controller-event", "id": event.ID, "time": event.Time, "kind": event.Kind}
					if event.Device != nil {
						switch event.Kind {
						case "relay":
							notice["relay_mask"] = event.Device.RelayMask
						case "door":
							notice["door_open"] = event.Device.DoorOpen
						}
					}
					select {
					case events <- notice:
					default:
					}
				}
			}
		})
	}
	mqttbridge.Run(ctx, config, mqttbridge.Hooks{
		Identity: func() mqttbridge.Identity { return s.mqttIdentity(s.Client.Snapshot()) },
		Observe:  s.mqttObservation, State: state, Events: events, Status: report,
		Execute: func(ctx context.Context, cmd mqttbridge.Command) mqttbridge.Receipt {
			return s.mqttCommand(ctx, config, cmd)
		},
	})
}

func (s *Service) mqttCommand(ctx context.Context, cfg mqttbridge.Config, cmd mqttbridge.Command) mqttbridge.Receipt {
	fail := func(code, message string) mqttbridge.Receipt { return mqttbridge.Receipt{Code: code, Message: message} }
	pwm := cmd.Method == "controller.pwm.set"
	switch cmd.Method {
	case "controller.action.invoke":
		var params struct {
			ActionID string `json:"action_id"`
		}
		if json.Unmarshal(cmd.Params, &params) != nil {
			return fail("invalid_params", "action_id is required")
		}
		parts := strings.Split(params.ActionID, ".")
		if len(parts) != 3 || parts[0] != "relay" && parts[0] != "pwm" ||
			parts[2] != "on" && parts[2] != "off" && parts[2] != "increase" && parts[2] != "decrease" {
			return fail("method_denied", "MQTT action must be a catalog relay or PWM setpoint action")
		}
		pwm = parts[0] == "pwm"
	case "controller.pwm.set":
		var params struct {
			Channel *int `json:"channel"`
		}
		if json.Unmarshal(cmd.Params, &params) != nil || params.Channel == nil {
			return fail("invalid_params", "channel is required")
		}
	case "controller.display.send", "controller.buzzer.tone", "controller.menu.jump", "controller.illumination.set":
	default:
		return fail("method_denied", "RPC method is not exposed over MQTT")
	}
	var owner controlIntentOwner
	if json.Unmarshal(cmd.Params, &owner) != nil || owner == (controlIntentOwner{}) {
		return fail("invalid_params", "current controller ownership fields are required")
	}
	guard := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := s.hostConfig()
		if !cfg.AllowCommands || current.Integrations.MQTT != cfg {
			return errors.New("MQTT command authority changed")
		}
		snapshot := s.Client.BoardSnapshot()
		identity := s.mqttIdentity(snapshot)
		if !snapshot.Connected || identity.BoardID == "" || identity.BoardID != cmd.BoardID || !strings.HasPrefix(cmd.BootID, identity.BootID+":") {
			return errors.New("MQTT board owner changed")
		}
		return validateControlIntent(owner, s.HostInstanceID, current, snapshot, pwm)
	}
	if err := guard(); err != nil {
		return fail("stale_owner", err.Error())
	}
	ctx = link.WithRequestWriteGuard(ctx, guard)
	result := s.dispatch(ctx, Request{JSONRPC: Version, Method: cmd.Method, Params: cmd.Params}, Access{
		Remote: true, Transport: "mqtt", Principal: "mqtt-broker", Authentication: "broker-acl", authenticated: true,
	})
	if result.Error != nil {
		return fail("handler_rejected", result.Error.Message)
	}
	return mqttbridge.Receipt{Accepted: true, Code: "accepted", Result: result.Result}
}

func mqttMethodAllowed(method string) bool {
	switch method {
	case "controller.action.invoke", "controller.pwm.set", "controller.display.send", "controller.buzzer.tone", "controller.menu.jump", "controller.illumination.set":
		return true
	}
	return false
}

func (s *Service) mqttSettings() any {
	s.mqttState.Lock()
	status := s.mqttState.status
	s.mqttState.Unlock()
	config := s.persistentHostConfig().Integrations.MQTT
	config.Password = ""
	status.Available, status.Enabled = mqttbridge.Available, config.Enabled
	return struct {
		Config mqttbridge.Config `json:"config"`
		Status mqttbridge.Status `json:"status"`
	}{config, status}
}

func (s *Service) dispatchMQTT(r Request, access Access) Response {
	result := Response{JSONRPC: Version, ID: r.ID}
	fail := func(code int, message string) Response {
		result.Error = &RPCError{Code: code, Message: message}
		return result
	}
	if access.Remote || access.Transport == "udp" || access.Transport == "bridge" {
		return fail(-32003, "MQTT provisioning requires local IPC or the local Web UI")
	}
	switch r.Method {
	case "controller.mqtt.config.get", "controller.mqtt.status":
		result.Result = s.mqttSettings()
	case "controller.mqtt.config.set":
		if s.UpdateHostConfig == nil {
			return fail(-32000, "persistent host configuration unavailable")
		}
		var config mqttbridge.Config
		if err := decodeStrictParams(r.Params, &config); err != nil {
			return fail(-32602, err.Error())
		}
		var presence struct {
			Enabled *bool `json:"enabled"`
		}
		if json.Unmarshal(r.Params, &presence) != nil || presence.Enabled == nil {
			return fail(-32602, "enabled is required")
		}
		if err := config.Validate(); err != nil {
			return fail(-32602, err.Error())
		}
		if config.Enabled && !mqttbridge.Available {
			return fail(-32602, "this host was built without MQTT support")
		}
		if err := s.UpdateHostConfig(func(c *appconfig.Config) error { c.Integrations.MQTT = config; return c.Validate() }); err != nil {
			return fail(-32602, err.Error())
		}
		result.Result = s.mqttSettings()
	default:
		return fail(-32601, "unknown MQTT management method")
	}
	return result
}
