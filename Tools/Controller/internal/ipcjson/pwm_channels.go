package ipcjson

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type pwmChannelValue struct {
	Channel        int                        `json:"channel"`
	RawValue       uint16                     `json:"raw_value"`
	LogicalPercent float64                    `json:"logical_percent"`
	Name           string                     `json:"name"`
	Config         appconfig.PWMChannelConfig `json:"config"`
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
	values := service.describePWMValues(*snapshot.PWMValues)
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
	config := service.hostConfig()
	result := pwmValues{
		Available: values.Available, SelectedChannel: values.SelectedChannel, Values: values.Values,
		Channels: make([]pwmChannelValue, 0, appconfig.PWMChannelCount),
	}
	names := make(map[string]string)
	for _, peripheral := range service.peripheralSettings().Peripherals {
		names[peripheral.Key] = peripheral.Name
	}
	for channel, raw := range values.Values {
		channelConfig := appconfig.PWMChannel(config.UI, channel)
		name, _ := appconfig.PeripheralDefaultName(appconfig.PWMChannelKey(channel))
		if configured := names[appconfig.PWMChannelKey(channel)]; strings.TrimSpace(configured) != "" {
			name = strings.TrimSpace(configured)
		}
		result.Channels = append(result.Channels, pwmChannelValue{
			Channel: channel, RawValue: raw,
			LogicalPercent: math.Round(appconfig.PWMPercentFromRaw(raw, channelConfig)*1000) / 1000,
			Name:           name, Config: channelConfig,
		})
	}
	return result
}

func validatePWMChannelConfig(channel int, value appconfig.PWMChannelConfig) error {
	return appconfig.ValidatePWMChannels(map[string]appconfig.PWMChannelConfig{
		appconfig.PWMChannelKey(channel): value,
	})
}

func (service *Service) updatePWMChannelConfig(channel int, value appconfig.PWMChannelConfig) (appconfig.PWMChannelConfig, error) {
	if channel < 0 || channel >= appconfig.PWMChannelCount {
		return appconfig.PWMChannelConfig{}, &RPCError{Code: -32602, Message: "channel must be 0..15"}
	}
	if err := validatePWMChannelConfig(channel, value); err != nil {
		return appconfig.PWMChannelConfig{}, &RPCError{Code: -32602, Message: err.Error()}
	}
	if service.UpdateHostConfig == nil {
		return appconfig.PWMChannelConfig{}, fmt.Errorf("persistent host configuration is unavailable")
	}
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		if config.UI.PWMChannels == nil {
			config.UI.PWMChannels = appconfig.DefaultPWMChannels()
		}
		config.UI.PWMChannels[appconfig.PWMChannelKey(channel)] = value
		return config.Validate()
	})
	if err != nil {
		return appconfig.PWMChannelConfig{}, err
	}
	profile, _ := service.activeBoardProfile()
	service.publishPeripheralChange(profile, []string{appconfig.PWMChannelKey(channel)}, []string{"output_type", "icon", "curve", "gamma"})
	return appconfig.PWMChannel(service.hostConfig().UI, channel), nil
}
