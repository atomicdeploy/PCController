package ipcjson

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type channelRoutingCatalog struct {
	HostInstanceID           string                           `json:"host_instance_id"`
	ConnectionGeneration     uint64                           `json:"connection_generation"`
	PWMConfigurationRevision string                           `json:"pwm_configuration_revision"`
	BoardProfile             boardProfileDescriptor           `json:"board_profile"`
	Routing                  appconfig.ChannelRouting         `json:"routing"`
	Targets                  []appconfig.PeripheralDescriptor `json:"targets"`
}

type channelRoutingUpdate struct {
	BoardIdentity    string                    `json:"board_identity"`
	ExpectedRevision string                    `json:"expected_revision"`
	Routing          *appconfig.ChannelRouting `json:"routing"`
}

type channelValueRequest struct {
	ExpectedHostInstanceID           string  `json:"expected_host_instance_id"`
	ExpectedConnectionGeneration     uint64  `json:"expected_connection_generation"`
	BoardIdentity                    string  `json:"board_identity"`
	ExpectedRevision                 string  `json:"expected_revision"`
	Key                              string  `json:"key"`
	Value                            *uint16 `json:"value"`
	PercentMilli                     *uint32 `json:"percent_milli,omitempty"`
	ExpectedPWMConfigurationRevision string  `json:"expected_pwm_configuration_revision,omitempty"`
}

// Physical scene restoration uses the same admission and readback contract,
// without creating an alias or reinterpreting an existing numbered cue.
type outputValueRequest struct {
	ExpectedHostInstanceID           string  `json:"expected_host_instance_id"`
	ExpectedConnectionGeneration     uint64  `json:"expected_connection_generation"`
	BoardIdentity                    string  `json:"board_identity"`
	ExpectedRevision                 string  `json:"expected_revision"`
	Target                           string  `json:"target"`
	Value                            *uint16 `json:"value"`
	PercentMilli                     *uint32 `json:"percent_milli,omitempty"`
	ExpectedPWMConfigurationRevision string  `json:"expected_pwm_configuration_revision,omitempty"`
}

func (service *Service) channelRoutingCatalog() channelRoutingCatalog {
	return service.routingCatalogFor(service.hostConfig(), service.Client.BoardSnapshot())
}

func (service *Service) routingCatalogFor(config appconfig.Config, snapshot controller.Snapshot) channelRoutingCatalog {
	descriptor, profile := describeBoardProfile(config, snapshot)
	peripherals, _ := appconfig.ProfileDescriptors(profile.Mode, profile.ExposeRawRelays, config.UI.PeripheralNames, profile.Presentation)
	result := channelRoutingCatalog{BoardProfile: descriptor, Routing: profile.ChannelRouting, Targets: []appconfig.PeripheralDescriptor{}}
	result.HostInstanceID, result.ConnectionGeneration = service.HostInstanceID, snapshot.ConnectionGeneration
	result.PWMConfigurationRevision = appconfig.PWMConfigurationRevision(config.UI)
	result.Routing.Channels = append([]appconfig.SemanticChannel{}, result.Routing.Channels...)
	for _, target := range peripherals {
		if _, _, ok := appconfig.RoutingTarget(target.Key); ok {
			result.Targets = append(result.Targets, target)
		}
	}
	return result
}

func requireRoutingOwner(profile boardProfileDescriptor, identity, revision string) error {
	if identity == "" || revision == "" {
		return &RPCError{Code: -32602, Message: "board_identity and expected_revision are required"}
	}
	if !profile.Configured || profile.BoardIdentity != identity || profile.Revision != revision {
		return &RPCError{Code: -32000, Message: "board profile changed; reload channel routing before retrying"}
	}
	return nil
}

func routingBindingsChanged(before, after appconfig.ChannelRouting) bool {
	if before.Enabled != after.Enabled || len(before.Channels) != len(after.Channels) {
		return true
	}
	previous := map[string]string{}
	for _, channel := range before.Channels {
		previous[channel.Key] = channel.Kind + "/" + channel.Target
	}
	for _, channel := range after.Channels {
		if previous[channel.Key] != channel.Kind+"/"+channel.Target {
			return true
		}
	}
	return false
}

func requireIdleRoutingOutputs(snapshot controller.Snapshot, before, after appconfig.ChannelRouting) error {
	if snapshot.Programming.Active || snapshot.Programming.RestorationPending {
		return errors.New("finish board programming and restoration before changing bindings")
	}
	if !snapshot.Connected {
		return nil
	} // Offline profile editing never writes outputs.
	if snapshot.Macros.Playback.Running || snapshot.Macros.Recording.Active {
		return errors.New("stop recording and effect playback before changing channel bindings")
	}
	for _, channel := range append(append([]appconfig.SemanticChannel{}, before.Channels...), after.Channels...) {
		kind, index, ok := appconfig.RoutingTarget(channel.Target)
		if !ok {
			return fmt.Errorf("invalid physical target %s", channel.Target)
		}
		switch kind {
		case "relay":
			if !snapshot.HaveStatus || time.Since(snapshot.StatusUpdated) > 2*time.Second {
				return errors.New("fresh relay state is required before changing bindings")
			}
			if snapshot.Status.ActiveRelays&(1<<index) != 0 {
				return fmt.Errorf("switch %s off before changing bindings", channel.Target)
			}
		case "pwm":
			if snapshot.PWMValues == nil || !snapshot.PWMValues.Available || time.Since(snapshot.PWMUpdated) > 2*time.Second {
				return errors.New("fresh PWM state is required before changing bindings")
			}
			if snapshot.PWMValues.Values[index] != 0 {
				return fmt.Errorf("set %s to zero before changing bindings", channel.Target)
			}
		}
	}
	return nil
}

func (service *Service) updateChannelRouting(ctx context.Context, params channelRoutingUpdate) (channelRoutingCatalog, error) {
	service.channelRoutingMu.Lock()
	defer service.channelRoutingMu.Unlock()
	if service.UpdateHostConfig == nil {
		return channelRoutingCatalog{}, errors.New("persistent host configuration is unavailable")
	}
	current, previous := service.activeBoardProfile()
	if err := requireRoutingOwner(current, params.BoardIdentity, params.ExpectedRevision); err != nil {
		return channelRoutingCatalog{}, err
	}
	if params.Routing == nil {
		return channelRoutingCatalog{}, &RPCError{Code: -32602, Message: "routing is required"}
	}
	if err := appconfig.ValidateRoutingReplacement(previous.ChannelRouting, *params.Routing); err != nil {
		return channelRoutingCatalog{}, &RPCError{Code: -32602, Message: err.Error()}
	}
	if reflect.DeepEqual(previous.ChannelRouting, *params.Routing) {
		return service.channelRoutingCatalog(), nil
	}
	if routingBindingsChanged(previous.ChannelRouting, *params.Routing) {
		if service.Client.ChannelRoutingBusy() {
			return channelRoutingCatalog{}, errors.New("stop recording and effect playback before changing bindings")
		}
		plan := service.Client.MediaTimeline()
		if plan.State != "" && plan.State != "stopped" && plan.State != "faulted" {
			return channelRoutingCatalog{}, errors.New("clear the prepared media timeline before changing channel bindings")
		}
		// An explicit save may make bounded read-only requests; no background
		// polling or output reset is used to make a remapping appear safe.
		before := service.Client.BoardSnapshot()
		if before.Connected {
			readCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			relays, pwm := false, false
			for _, route := range append(append([]appconfig.SemanticChannel{}, previous.ChannelRouting.Channels...), params.Routing.Channels...) {
				relays = relays || route.Kind == "relay"
				pwm = pwm || route.Kind == "pwm"
			}
			if relays {
				if _, err := service.Client.Status(readCtx); err != nil {
					return channelRoutingCatalog{}, fmt.Errorf("read relay state before routing save: %w", err)
				}
			}
			if pwm {
				if _, err := service.Client.PWMValues(readCtx); err != nil {
					return channelRoutingCatalog{}, fmt.Errorf("read PWM state before routing save: %w", err)
				}
			}
		}
		after := service.Client.BoardSnapshot()
		if after.Connected != before.Connected || after.ConnectionGeneration != before.ConnectionGeneration {
			return channelRoutingCatalog{}, errors.New("board connection changed while validating bindings")
		}
		if err := requireIdleRoutingOutputs(after, previous.ChannelRouting, *params.Routing); err != nil {
			return channelRoutingCatalog{}, err
		}
	}
	var committed channelRoutingCatalog
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		// Config callbacks already hold the store writer lock. A full Snapshot
		// invokes effect providers and can re-enter that same store.
		snapshot := service.Client.BoardSnapshot()
		identity, profile, configured := boardProfileForSnapshot(*config, snapshot)
		if !configured || identity.Value != params.BoardIdentity || appconfig.BoardProfileRevision(identity.Value, profile, config.UI.PeripheralNames) != params.ExpectedRevision {
			return &RPCError{Code: -32000, Message: "board profile changed during routing save; reload before retrying"}
		}
		if routingBindingsChanged(profile.ChannelRouting, *params.Routing) {
			if service.Client.ChannelRoutingBusy() {
				return errors.New("stop recording and effect playback before changing bindings")
			}
			if err := requireIdleRoutingOutputs(snapshot, profile.ChannelRouting, *params.Routing); err != nil {
				return err
			}
		}
		profile.ChannelRouting = *params.Routing
		profile.ChannelRouting.Channels = append([]appconfig.SemanticChannel{}, params.Routing.Channels...)
		// Do not mutate a map shared with a previous config snapshot before save ACK.
		profiles := make(map[string]appconfig.BoardProfile, len(config.BoardProfiles))
		for key, value := range config.BoardProfiles {
			profiles[key] = value
		}
		profiles[identity.Value] = profile
		config.BoardProfiles = profiles
		committed = service.routingCatalogFor(*config, snapshot)
		return nil
	})
	if err != nil {
		return channelRoutingCatalog{}, err
	}
	// The selected board may have changed after persistence. Publish the saved
	// owner/revision, never relabel this transaction as belonging to its successor.
	result := committed
	keys := []string{}
	for _, channel := range previous.ChannelRouting.Channels {
		keys = append(keys, channel.Key)
	}
	for _, channel := range result.Routing.Channels {
		keys = append(keys, channel.Key)
	}
	service.publishPeripheralChange(result.BoardProfile, keys, []string{"channel_routing"})
	return result, nil
}

func (service *Service) setSemanticChannel(ctx context.Context, params channelValueRequest) (map[string]any, error) {
	service.channelRoutingMu.RLock()
	defer service.channelRoutingMu.RUnlock()
	if params.ExpectedHostInstanceID == "" || params.ExpectedHostInstanceID != service.HostInstanceID || params.ExpectedConnectionGeneration == 0 {
		return nil, &RPCError{Code: -32602, Message: "current expected_host_instance_id and expected_connection_generation are required"}
	}
	profile, configured := service.activeBoardProfile()
	if err := requireRoutingOwner(profile, params.BoardIdentity, params.ExpectedRevision); err != nil {
		return nil, err
	}
	if !profile.Attached {
		return nil, errors.New("attach the configured board before controlling a semantic channel")
	}
	var raw uint16
	if params.Value != nil {
		raw = *params.Value
	}
	channel, err := appconfig.ResolveSemanticChannel(configured.ChannelRouting, params.Key, raw)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	result, err := service.setPinnedOutput(ctx, outputValueRequest{
		ExpectedHostInstanceID: params.ExpectedHostInstanceID, ExpectedConnectionGeneration: params.ExpectedConnectionGeneration,
		BoardIdentity: params.BoardIdentity, ExpectedRevision: params.ExpectedRevision, Target: channel.Target, Value: params.Value,
		PercentMilli: params.PercentMilli, ExpectedPWMConfigurationRevision: params.ExpectedPWMConfigurationRevision,
	})
	if err != nil {
		return nil, err
	}
	result["key"] = channel.Key
	return result, nil
}

func (service *Service) setPinnedOutput(ctx context.Context, params outputValueRequest) (map[string]any, error) {
	if params.ExpectedHostInstanceID == "" || params.ExpectedHostInstanceID != service.HostInstanceID || params.ExpectedConnectionGeneration == 0 {
		return nil, &RPCError{Code: -32602, Message: "current expected_host_instance_id and expected_connection_generation are required"}
	}
	config, snapshot := service.hostConfig(), service.Client.BoardSnapshot()
	profile, _ := describeBoardProfile(config, snapshot)
	if err := requireRoutingOwner(profile, params.BoardIdentity, params.ExpectedRevision); err != nil {
		return nil, err
	}
	kind, _, valid := appconfig.RoutingTarget(params.Target)
	value, err := resolvePinnedOutputValue(params, config.UI)
	if !valid || err != nil {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprint(err)}
	}
	if err := service.rejectLockedPeripheral(params.Target); err != nil {
		return nil, err
	}
	identity, _, _ := boardProfileForSnapshot(config, snapshot)
	if !snapshot.Connected || identity.Value != params.BoardIdentity || snapshot.ConnectionGeneration != params.ExpectedConnectionGeneration {
		return nil, errors.New("board owner changed before channel dispatch")
	}
	capability := uint32(native.CapabilityRelayMotion)
	if kind == "pwm" {
		capability = native.CapabilityPWM
	}
	if snapshot.Hello.Capabilities&capability == 0 {
		return nil, errors.New("the attached board does not advertise this output capability")
	}
	result := map[string]any{"accepted": true, "outcome": "confirmed", "target": params.Target, "value": value, "observed_value": value, "host_instance_id": service.HostInstanceID, "board_identity": profile.BoardIdentity, "revision": profile.Revision, "connection_generation": snapshot.ConnectionGeneration}
	if params.PercentMilli != nil {
		result["percent_milli"], result["pwm_configuration_revision"] = *params.PercentMilli, params.ExpectedPWMConfigurationRevision
	}
	if err := service.Client.SetRoutedOutput(ctx, snapshot.ConnectionGeneration, params.Target, value); err != nil {
		// Once runtime admission starts, conservatively report uncertainty. A
		// timeout or transport failure must never be interpreted as safe to retry.
		delete(result, "observed_value")
		result["accepted"], result["outcome"] = false, "unknown"
		return nil, &RPCError{Code: -32000, Message: err.Error(), Data: result}
	}
	return result, nil
}

func resolvePinnedOutputValue(params outputValueRequest, ui appconfig.UI) (uint16, error) {
	kind, index, valid := appconfig.RoutingTarget(params.Target)
	if !valid || (params.Value == nil) == (params.PercentMilli == nil) {
		return 0, errors.New("a general-purpose target and exactly one of value or percent_milli are required")
	}
	if params.PercentMilli != nil {
		if kind != "pwm" || *params.PercentMilli > 100000 {
			return 0, errors.New("percent_milli requires a PWM target and a logical value in 0..100000")
		}
		if params.ExpectedPWMConfigurationRevision == "" || params.ExpectedPWMConfigurationRevision != appconfig.PWMConfigurationRevision(ui) {
			return 0, errors.New("PWM configuration changed; reload before applying a logical value")
		}
		return appconfig.PWMRawFromPercent(float64(*params.PercentMilli)/1000, appconfig.PWMChannel(ui, int(index))), nil
	}
	if *params.Value > appconfig.PWMMaximumValue || kind == "relay" && *params.Value > 1 {
		return 0, errors.New("value must be relay 0/1 or raw PWM 0..4095")
	}
	return *params.Value, nil
}

func outputRPCError(err error) error {
	if err == nil {
		return nil
	}
	var rpc *RPCError
	if errors.As(err, &rpc) {
		if rpc.Data != nil {
			return rpc
		}
		return &RPCError{Code: rpc.Code, Message: rpc.Message, Data: map[string]any{"outcome": "rejected"}}
	}
	return &RPCError{Code: -32000, Message: err.Error(), Data: map[string]any{"outcome": "rejected"}}
}
