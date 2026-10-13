package ipcjson

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type pwmChannelValue struct {
	Channel           int                        `json:"channel"`
	RawValue          uint16                     `json:"raw_value"`
	LogicalPercent    float64                    `json:"logical_percent"`
	Name              string                     `json:"name"`
	Config            appconfig.PWMChannelConfig `json:"config"`
	RememberedKnown   bool                       `json:"remembered_known"`
	RememberedRaw     uint16                     `json:"remembered_raw"`
	RememberedPercent float64                    `json:"remembered_percent"`
}

type pwmValues struct {
	HostInstanceID       string            `json:"host_instance_id"`
	ConnectionGeneration uint64            `json:"connection_generation"`
	Revision             uint64            `json:"revision"`
	Time                 time.Time         `json:"time"`
	Available            bool              `json:"available"`
	SelectedChannel      byte              `json:"selected_channel"`
	Values               [16]uint16        `json:"values"`
	Channels             []pwmChannelValue `json:"channels"`
}

func (service *Service) pwmValues(ctx context.Context) (pwmValues, error) {
	ctx, cancel := context.WithTimeout(ctx, pwmOperationTimeout)
	defer cancel()
	values, err := service.Client.PWMValues(ctx)
	if err != nil {
		return pwmValues{}, err
	}
	_ = values
	if values := service.cachedPWMValues(); values != nil {
		return *values, nil
	}
	return pwmValues{}, fmt.Errorf("PWM readback owner changed")
}

func (service *Service) cachedPWMValues() *pwmValues {
	snapshot := service.Client.Snapshot()
	if snapshot.PWMValues == nil {
		return nil
	}
	values := service.describePWMValuesForSnapshot(*snapshot.PWMValues, snapshot)
	values.HostInstanceID, values.ConnectionGeneration = strings.TrimSpace(service.HostInstanceID), snapshot.ConnectionGeneration
	values.Revision, values.Time = snapshot.PWMRevision, snapshot.PWMUpdated
	return &values
}

// Write and readback share a budget below native clients' 3-second socket
// deadline. Never retry a write whose acknowledgement may have been lost.
const pwmOperationTimeout = 2 * time.Second

type pwmConfirmationError struct{ cause error }

func (err *pwmConfirmationError) Error() string {
	return "PWM change acknowledged, but readback was not received. Refresh hardware values; the change was not retried."
}
func (err *pwmConfirmationError) Unwrap() error { return err.cause }

func changePWMAndReadback(ctx context.Context, change func(context.Context) error, read func(context.Context) (pwmValues, error)) (pwmValues, error) {
	ctx, cancel := context.WithTimeout(ctx, pwmOperationTimeout)
	defer cancel()
	if err := change(ctx); err != nil {
		return pwmValues{}, err
	}
	values, err := read(ctx)
	if err != nil {
		return pwmValues{}, &pwmConfirmationError{cause: err}
	}
	return values, nil
}

func (service *Service) setPWMAndReadback(ctx context.Context, channel byte, raw uint16) (pwmValues, error) {
	if err := service.rejectLockedPeripheral(fmt.Sprintf("pwm.%d", channel)); err != nil {
		return pwmValues{}, err
	}
	return changePWMAndReadback(ctx, func(ctx context.Context) error {
		return service.Client.SetPWMChannel(ctx, channel, raw)
	}, service.pwmValues)
}

func (service *Service) clearPWMAndReadback(ctx context.Context) (pwmValues, error) {
	return changePWMAndReadback(ctx, service.Client.AllPWMOff, service.pwmValues)
}

func (service *Service) describePWMValues(values native.PWMValues) pwmValues {
	return service.describePWMValuesForSnapshot(values, service.Client.Snapshot())
}

func (service *Service) describePWMValuesForSnapshot(values native.PWMValues, snapshot controller.Snapshot) pwmValues {
	config := service.hostConfig()
	result := pwmValues{
		Available: values.Available, SelectedChannel: values.SelectedChannel, Values: values.Values,
		Channels: make([]pwmChannelValue, 0, appconfig.PWMChannelCount),
	}
	_, profile, _ := boardProfileForSnapshot(config, snapshot)
	names := appconfig.PWMDisplayNames(config.UI, profile.Presentation)
	for channel, raw := range values.Values {
		channelConfig := appconfig.PWMChannel(config.UI, channel)
		recall := snapshot.PWMRecall[channel]
		result.Channels = append(result.Channels, pwmChannelValue{
			Channel: channel, RawValue: raw,
			LogicalPercent: math.Round(appconfig.PWMPercentFromRaw(raw, channelConfig)*1000) / 1000,
			Name:           names[channel], Config: channelConfig,
			RememberedKnown: recall.Known, RememberedRaw: recall.Raw,
			RememberedPercent: math.Round(appconfig.PWMPercentFromRaw(recall.Raw, channelConfig)*1000) / 1000,
		})
	}
	return result
}

func validatePWMChannelConfig(channel int, value appconfig.PWMChannelConfig) error {
	return appconfig.ValidatePWMChannels(map[string]appconfig.PWMChannelConfig{
		appconfig.PWMChannelKey(channel): value,
	})
}

// Pointer fields distinguish omitted edits from invalid explicit zero/empty
// input. The same decoder is used by RPC and HTTP, without contract aliases.
type pwmChannelConfigParams struct {
	Channel     *int     `json:"channel"`
	OutputType  *string  `json:"output_type"`
	Icon        *string  `json:"icon"`
	Curve       *string  `json:"curve"`
	Gamma       *float64 `json:"gamma"`
	StepPercent *float64 `json:"step_percent"`
	OnBehavior  *string  `json:"on_behavior"`
}

func (params pwmChannelConfigParams) patch() (int, appconfig.PWMChannelConfig, error) {
	bad := func(message string) (int, appconfig.PWMChannelConfig, error) {
		return 0, appconfig.PWMChannelConfig{}, &RPCError{Code: -32602, Message: message}
	}
	if params.Channel == nil || *params.Channel < 0 || *params.Channel >= appconfig.PWMChannelCount {
		return bad("channel must be explicitly specified as 0..15")
	}
	value := appconfig.PWMChannelConfig{StepPercent: params.StepPercent}
	for _, field := range []struct {
		name   string
		input  *string
		target *string
	}{
		{"output_type", params.OutputType, &value.OutputType}, {"icon", params.Icon, &value.Icon}, {"curve", params.Curve, &value.Curve}, {"on_behavior", params.OnBehavior, &value.OnBehavior},
	} {
		if field.input != nil {
			if strings.TrimSpace(*field.input) == "" {
				return bad(field.name + " must not be empty")
			}
			*field.target = *field.input
		}
	}
	if params.Gamma != nil {
		if math.IsNaN(*params.Gamma) || math.IsInf(*params.Gamma, 0) || *params.Gamma < appconfig.PWMMinimumGamma || *params.Gamma > appconfig.PWMMaximumGamma {
			return bad("gamma must be 0.10..5.00")
		}
		value.Gamma = *params.Gamma
	}
	return *params.Channel, value, nil
}

func mergePWMChannelConfig(current, value appconfig.PWMChannelConfig) appconfig.PWMChannelConfig {
	// Resolve omission inside the serialized config mutation, not from a stale
	// client catalog or a snapshot preceding another client's edit.
	if value.OutputType == "" {
		value.OutputType = current.OutputType
	}
	if value.Icon == "" {
		value.Icon = current.Icon
	}
	if value.Curve == "" {
		value.Curve = current.Curve
	}
	if value.Gamma == 0 {
		value.Gamma = current.Gamma
	}
	if value.OnBehavior == "" {
		value.OnBehavior = appconfig.PWMOnBehavior(current)
	}
	if value.StepPercent == nil {
		if current.StepPercent != nil {
			step := *current.StepPercent
			value.StepPercent = &step
		}
	}
	return value
}

func (service *Service) updatePWMChannelConfig(channel int, value appconfig.PWMChannelConfig) (appconfig.PWMChannelConfig, error) {
	if channel < 0 || channel >= appconfig.PWMChannelCount {
		return appconfig.PWMChannelConfig{}, &RPCError{Code: -32602, Message: "channel must be 0..15"}
	}
	if err := validatePWMChannelConfig(channel, mergePWMChannelConfig(appconfig.PWMChannel(service.hostConfig().UI, channel), value)); err != nil {
		return appconfig.PWMChannelConfig{}, &RPCError{Code: -32602, Message: err.Error()}
	}
	if service.UpdateHostConfig == nil {
		return appconfig.PWMChannelConfig{}, fmt.Errorf("persistent host configuration is unavailable")
	}
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		merged := mergePWMChannelConfig(appconfig.PWMChannel(config.UI, channel), value)
		if err := validatePWMChannelConfig(channel, merged); err != nil {
			return &RPCError{Code: -32602, Message: err.Error()}
		}
		if config.UI.PWMChannels == nil {
			config.UI.PWMChannels = appconfig.DefaultPWMChannels()
		}
		config.UI.PWMChannels[appconfig.PWMChannelKey(channel)] = merged
		return config.Validate()
	})
	if err != nil {
		return appconfig.PWMChannelConfig{}, err
	}
	profile, _ := service.activeBoardProfile()
	service.publishPeripheralChange(profile, []string{appconfig.PWMChannelKey(channel)}, []string{"output_type", "icon", "curve", "gamma", "step_percent", "on_behavior"})
	return appconfig.PWMChannel(service.hostConfig().UI, channel), nil
}
