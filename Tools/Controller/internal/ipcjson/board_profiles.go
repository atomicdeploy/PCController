package ipcjson

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

type boardProfileDescriptor struct {
	Key            string `json:"key,omitempty"`
	BoardIdentity  string `json:"board_identity,omitempty"`
	IdentitySource string `json:"identity_source"`
	IdentityStable bool   `json:"identity_stable"`
	Mode           string `json:"mode"`
	Configured     bool   `json:"configured"`
	Attached       bool   `json:"attached"`
	Revision       string `json:"revision"`
}

type presentationUpdateResult struct {
	BoardProfile boardProfileDescriptor         `json:"board_profile"`
	Peripheral   appconfig.PeripheralDescriptor `json:"peripheral"`
}

func (service *Service) activeBoardProfile() (boardProfileDescriptor, appconfig.BoardProfile) {
	config := service.hostConfig()
	snapshot := service.Client.Snapshot()
	port := snapshot.Port
	if strings.TrimSpace(port.SerialNumber) == "" && strings.TrimSpace(port.InstanceID) == "" && strings.TrimSpace(port.Name) == "" && config.Connection.LastDevice != nil {
		port.SerialNumber = config.Connection.LastDevice.SerialNumber
		port.InstanceID = config.Connection.LastDevice.InstanceID
		port.Name = config.Connection.LastDevice.Port
	}
	identity := appconfig.ResolveBoardIdentity(port.SerialNumber, port.InstanceID, port.Name)
	profile, configured := config.BoardProfiles[identity.Value]
	mode := appconfig.BoardModeUnconfigured
	if configured {
		mode = appconfig.NormalizeBoardMode(profile.Mode)
	}
	profile.Mode = mode
	descriptor := boardProfileDescriptor{
		Key: profile.Key, BoardIdentity: identity.Value, IdentitySource: identity.Source,
		IdentityStable: identity.Stable, Mode: mode, Configured: configured,
		Attached: snapshot.Connected && identity.Value != "",
	}
	descriptor.Revision = appconfig.BoardProfileRevision(identity.Value, profile, config.UI.PeripheralNames)
	return descriptor, profile
}

func (service *Service) updateActiveBoardProfile(key, mode, expectedRevision string) (boardProfileDescriptor, error) {
	if service.UpdateHostConfig == nil {
		return boardProfileDescriptor{}, errors.New("persistent host configuration is unavailable")
	}
	current, _ := service.activeBoardProfile()
	if !current.Attached || current.BoardIdentity == "" {
		return boardProfileDescriptor{}, errors.New("an authenticated board must be attached before configuring its profile")
	}
	key = strings.TrimSpace(key)
	mode = appconfig.NormalizeBoardMode(mode)
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		if config.BoardProfiles == nil {
			config.BoardProfiles = make(map[string]appconfig.BoardProfile)
		}
		profile := config.BoardProfiles[current.BoardIdentity]
		profile.Mode = appconfig.NormalizeBoardMode(profile.Mode)
		if expectedRevision != "" && expectedRevision != appconfig.BoardProfileRevision(current.BoardIdentity, profile, config.UI.PeripheralNames) {
			return &RPCError{Code: -32000, Message: "board profile changed; refresh controller.peripherals.get and retry"}
		}
		profile.Key, profile.Mode = key, mode
		config.BoardProfiles[current.BoardIdentity] = profile
		return nil
	})
	if err != nil {
		return boardProfileDescriptor{}, err
	}
	updated, _ := service.activeBoardProfile()
	service.publishPeripheralChange(updated, []string{"board_profile"}, []string{"key", "mode"})
	return updated, nil
}

func (service *Service) updatePeripheralPresentation(key string, name, icon, group *string, expectedRevision string) (presentationUpdateResult, error) {
	if service.UpdateHostConfig == nil {
		return presentationUpdateResult{}, errors.New("persistent host configuration is unavailable")
	}
	current, _ := service.activeBoardProfile()
	if !current.Attached || !current.Configured {
		return presentationUpdateResult{}, errors.New("configure the attached board profile before changing presentation")
	}
	key = strings.TrimSpace(key)
	if key == "" || name == nil && icon == nil && group == nil {
		return presentationUpdateResult{}, &RPCError{Code: -32602, Message: "key and at least one of name, icon, or group are required"}
	}
	known := false
	for _, peripheral := range service.peripheralSettings().Peripherals {
		if peripheral.Key == key {
			known = true
			break
		}
	}
	if !known {
		return presentationUpdateResult{}, &RPCError{Code: -32602, Message: fmt.Sprintf("peripheral key %q is not advertised by the active profile", key)}
	}
	changedFields := make([]string, 0, 3)
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		profile := config.BoardProfiles[current.BoardIdentity]
		profile.Mode = appconfig.NormalizeBoardMode(profile.Mode)
		if expectedRevision != "" && expectedRevision != appconfig.BoardProfileRevision(current.BoardIdentity, profile, config.UI.PeripheralNames) {
			return &RPCError{Code: -32000, Message: "board profile changed; refresh controller.peripherals.get and retry"}
		}
		if profile.Presentation == nil {
			profile.Presentation = make(map[string]appconfig.PeripheralPresentation)
		}
		presentation := profile.Presentation[key]
		if name != nil {
			presentation.Name = strings.TrimSpace(*name)
			changedFields = append(changedFields, "name")
		}
		if icon != nil {
			presentation.Icon = strings.TrimSpace(*icon)
			changedFields = append(changedFields, "icon")
		}
		if group != nil {
			presentation.Group = strings.TrimSpace(*group)
			changedFields = append(changedFields, "group")
		}
		if presentation == (appconfig.PeripheralPresentation{}) {
			delete(profile.Presentation, key)
		} else {
			profile.Presentation[key] = presentation
		}
		config.BoardProfiles[current.BoardIdentity] = profile
		return nil
	})
	if err != nil {
		return presentationUpdateResult{}, err
	}
	settings := service.peripheralSettings()
	service.publishPeripheralChange(settings.BoardProfile, []string{key}, changedFields)
	for _, peripheral := range settings.Peripherals {
		if peripheral.Key == key {
			return presentationUpdateResult{BoardProfile: settings.BoardProfile, Peripheral: peripheral}, nil
		}
	}
	return presentationUpdateResult{}, errors.New("updated peripheral disappeared from the active profile")
}

func (service *Service) publishPeripheralChange(profile boardProfileDescriptor, changedKeys, changedFields []string) {
	service.Client.EmitHostActionEvent(
		"peripherals.changed", "board peripheral catalog changed", "host", "refresh",
		map[string]string{
			"board_identity": profile.BoardIdentity,
			"profile_key":    profile.Key,
			"revision":       profile.Revision,
			"changed_keys":   strings.Join(changedKeys, ","),
			"changed_fields": strings.Join(changedFields, ","),
			"action":         "refresh",
		},
	)
}

func (service *Service) invokeSemanticAction(ctx context.Context, actionID string) (map[string]any, error) {
	profile, _ := service.activeBoardProfile()
	if !profile.Attached || !profile.Configured {
		return nil, errors.New("the attached board has no configured control profile")
	}
	actionID = strings.ToLower(strings.TrimSpace(actionID))
	controlKey := ""
	var err error
	if profile.Mode == appconfig.BoardModeCinemaSeatMotion {
		actions := map[string]struct {
			side   byte
			motion controller.RelayMotion
		}{
			"seat.a.up": {1, controller.RelayMotionUp}, "seat.a.down": {1, controller.RelayMotionDown}, "seat.a.stop": {1, controller.RelayMotionStop},
			"seat.b.up": {2, controller.RelayMotionUp}, "seat.b.down": {2, controller.RelayMotionDown}, "seat.b.stop": {2, controller.RelayMotionStop},
		}
		action, exists := actions[actionID]
		if !exists {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		controlKey = strings.Join(strings.Split(actionID, ".")[:2], ".")
		err = service.Client.SetMotionSide(ctx, action.side, action.motion)
	} else if profile.Mode == appconfig.BoardModeOrdinaryRelays {
		parts := strings.Split(actionID, ".")
		if len(parts) != 3 || parts[0] != "relay" || parts[2] != "on" && parts[2] != "off" {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		number, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || number < 1 || number > 8 {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		controlKey = strings.Join(parts[:2], ".")
		err = service.Client.SetRelay(ctx, byte(number), parts[2] == "on")
	} else {
		return nil, errors.New("the attached board profile does not advertise actions")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true, "action_id": actionID, "control_key": controlKey, "board_profile": profile}, nil
}
