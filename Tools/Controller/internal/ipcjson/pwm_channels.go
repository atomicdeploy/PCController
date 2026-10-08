package ipcjson

import (
	"context"
	"fmt"
	"math"
	"strings"

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
	Available       bool              `json:"available"`
	SelectedChannel byte              `json:"selected_channel"`
	Values          [16]uint16        `json:"values"`
	Channels        []pwmChannelValue `json:"channels"`
}

func (service *Service) pwmValues(ctx context.Context) (pwmValues, error) {
	values, err := service.Client.PWMValues(ctx)
	if err != nil {
		return pwmValues{}, err
	}
	return service.describePWMValues(values), nil
}

func (service *Service) describePWMValues(values native.PWMValues) pwmValues {
	config := service.hostConfig()
	result := pwmValues{
		Available: values.Available, SelectedChannel: values.SelectedChannel, Values: values.Values,
		Channels: make([]pwmChannelValue, 0, appconfig.PWMChannelCount),
	}
	for channel, raw := range values.Values {
		channelConfig := appconfig.PWMChannel(config.UI, channel)
		name, _ := appconfig.PeripheralDefaultName(appconfig.PWMChannelKey(channel))
		if configured := config.UI.PeripheralNames[appconfig.PWMChannelKey(channel)]; strings.TrimSpace(configured) != "" {
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
