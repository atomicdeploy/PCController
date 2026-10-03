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
	Key             string `json:"key,omitempty"`
	BoardIdentity   string `json:"board_identity,omitempty"`
	IdentitySource  string `json:"identity_source"`
	IdentityStable  bool   `json:"identity_stable"`
	Mode            string `json:"mode"`
	ExposeRawRelays bool   `json:"expose_raw_relays"`
	Configured      bool   `json:"configured"`
	Attached        bool   `json:"attached"`
	Revision        string `json:"revision"`
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
		IdentityStable: identity.Stable, Mode: mode, ExposeRawRelays: profile.ExposeRawRelays, Configured: configured,
		Attached: snapshot.Connected && identity.Value != "",
	}
	descriptor.Revision = appconfig.BoardProfileRevision(identity.Value, profile, config.UI.PeripheralNames)
	return descriptor, profile
}

func (service *Service) updateActiveBoardProfile(key, mode string, exposeRawRelays *bool, expectedRevision string) (boardProfileDescriptor, error) {
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
		if exposeRawRelays != nil {
			profile.ExposeRawRelays = *exposeRawRelays
		}
		config.BoardProfiles[current.BoardIdentity] = profile
		return nil
	})
	if err != nil {
		return boardProfileDescriptor{}, err
	}
	updated, _ := service.activeBoardProfile()
	service.publishPeripheralChange(updated, []string{"board_profile"}, []string{"key", "mode", "expose_raw_relays"})
	return updated, nil
}

func (service *Service) updatePeripheralPresentation(key string, name, icon, color, group *string, hidden, locked *bool, expectedRevision string) (presentationUpdateResult, error) {
	if service.UpdateHostConfig == nil {
		return presentationUpdateResult{}, errors.New("persistent host configuration is unavailable")
	}
	current, _ := service.activeBoardProfile()
	if !current.Attached || !current.Configured {
		return presentationUpdateResult{}, errors.New("configure the attached board profile before changing presentation")
	}
	key = strings.TrimSpace(key)
	if key == "" || name == nil && icon == nil && color == nil && group == nil && hidden == nil && locked == nil {
		return presentationUpdateResult{}, &RPCError{Code: -32602, Message: "key and at least one of name, icon, color, group, hidden, or locked are required"}
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
	changedFields := make([]string, 0, 6)
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
		if color != nil {
			presentation.Color = strings.ToUpper(strings.TrimSpace(*color))
			changedFields = append(changedFields, "color")
		}
		if group != nil {
			presentation.Group = strings.TrimSpace(*group)
			changedFields = append(changedFields, "group")
		}
		if hidden != nil {
			presentation.Hidden = *hidden
			changedFields = append(changedFields, "hidden")
		}
		if locked != nil {
			presentation.Locked = *locked
			changedFields = append(changedFields, "locked")
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

func (service *Service) peripheralLocked(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, control := range service.peripheralSettings().Controls {
		if control.Key == key {
			return control.Locked
		}
	}
	return false
}

func (service *Service) rejectLockedPeripheral(key string) error {
	if service.peripheralLocked(key) {
		return fmt.Errorf("peripheral %q is locked by the active board profile", key)
	}
	return nil
}

func peripheralKeyForCommand(command string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	if len(words) < 3 || words[0] != "relay" {
		return ""
	}
	if words[1] == "side" && len(words) >= 4 {
		switch words[2] {
		case "a", "left", "1":
			return "seat.a"
		case "b", "right", "2":
			return "seat.b"
		}
		return ""
	}
	relay, err := strconv.Atoi(words[1])
	if err != nil || relay < 1 || relay > 8 {
		return ""
	}
	return fmt.Sprintf("relay.%d", relay)
}

func (service *Service) rejectLockedCommand(command string) error {
	if key := peripheralKeyForCommand(command); key != "" {
		return service.rejectLockedPeripheral(key)
	}
	return nil
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
	control, action, advertised := advertisedSemanticAction(service.peripheralSettings().Controls, actionID)
	if !advertised {
		return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
	}
	if control.Locked {
		return nil, fmt.Errorf("peripheral %q is locked by board profile %q", control.Key, profile.Key)
	}
	controlKey := control.Key
	var err error
	switch control.Kind {
	case "seat":
		actions := map[string]struct {
			side   byte
			motion controller.RelayMotion
		}{
			"seat.a.up": {1, controller.RelayMotionUp}, "seat.a.down": {1, controller.RelayMotionDown}, "seat.a.stop": {1, controller.RelayMotionStop},
			"seat.b.up": {2, controller.RelayMotionUp}, "seat.b.down": {2, controller.RelayMotionDown}, "seat.b.stop": {2, controller.RelayMotionStop},
		}
		mapped, exists := actions[action.ID]
		if !exists {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		err = service.Client.SetMotionSide(ctx, mapped.side, mapped.motion)
	case "relay":
		parts := strings.Split(action.ID, ".")
		if len(parts) != 3 || parts[0] != "relay" || parts[2] != "on" && parts[2] != "off" {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		number, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || number < 1 || number > 8 {
			return nil, fmt.Errorf("action %q is not advertised by board profile %q", actionID, profile.Key)
		}
		err = service.Client.SetRelay(ctx, byte(number), parts[2] == "on")
	default:
		return nil, fmt.Errorf("action %q is not executable by control %q", actionID, control.Key)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true, "action_id": actionID, "control_key": controlKey, "board_profile": profile}, nil
}

// advertisedSemanticAction resolves execution from the same catalog returned
// by controller.peripherals.get. Keeping advertisement and dispatch on one
// source of truth prevents a profile from rendering a valid control that the
// invocation path then rejects (for example R5-R8 in cinema-seat mode).
func advertisedSemanticAction(controls []appconfig.ControlDescriptor, actionID string) (appconfig.ControlDescriptor, appconfig.ActionDescriptor, bool) {
	actionID = strings.ToLower(strings.TrimSpace(actionID))
	for _, control := range controls {
		for _, action := range control.Actions {
			if strings.ToLower(strings.TrimSpace(action.ID)) == actionID {
				return control, action, true
			}
		}
	}
	return appconfig.ControlDescriptor{}, appconfig.ActionDescriptor{}, false
}
