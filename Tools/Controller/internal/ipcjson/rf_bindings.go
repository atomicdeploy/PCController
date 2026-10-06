package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
)

func isRFBinding(rule appconfig.Automation) bool {
	return rule.Match.Kind == "rf.gesture" && rule.Match.Source == "rf"
}

func rfBindings(config appconfig.Config) []appconfig.Automation {
	result := []appconfig.Automation{}
	for _, rule := range config.Automations {
		if isRFBinding(rule) {
			result = append(result, rule)
		}
	}
	return result
}

func (service *Service) configureAutomationActions() {
	submit, outcome := service.AppActionSubmit, service.AppActionOutcome
	service.Client.ConfigureAutomationAppDispatcher(func(ctx context.Context, action appconfig.AutomationAction) error {
		if strings.EqualFold(action.Type, "control") {
			_, err := service.invokeSemanticAction(ctx, action.ActionID)
			return err
		}
		if submit == nil || outcome == nil {
			return errors.New("application coordinator is unavailable")
		}
		operation, err := submit(hostui.AppAction{Kind: action.AppKind, Value: action.AppValue, Target: action.AppTarget, Source: "rf-automation"}, 5*time.Second)
		if err != nil {
			return err
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			if operation.State != hostui.ActionStateQueued {
				if operation.State == hostui.ActionStateApplied {
					return nil
				}
				return fmt.Errorf("application action %s: %s %s", operation.OperationID, operation.State, operation.Reason)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			operation, err = outcome(operation.OperationID)
			if err != nil {
				return err
			}
		}
	})
}

func (service *Service) rfCatalog(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		ReadBoard bool `json:"read_board,omitempty"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, err
	}
	config := service.hostConfig()
	snapshot := service.Client.Snapshot()
	result := map[string]any{
		"bindings": rfBindings(config), "learning": service.Client.RFLearningState(),
		"activity": service.Client.RFActivity(), "keyboard": config.OSActions.VirtualKeys,
		"connected": snapshot.Connected, "hostname": "", "entries": []any{},
		"gestures":     []string{"down", "up", "click", "double", "hold", "repeat"},
		"action_types": []string{"app", "control", "board", "effect", "rf", "virtual-key", "host", "script", "emit"},
		"effects":      snapshot.Effects, "peripherals": service.peripheralSettings(),
	}
	hostname, _ := os.Hostname()
	result["hostname"] = hostname
	result["board_mapping_actions"] = []string{"none", "key", "menu", "relay", "side", "pwm"}
	result["board_mapping_options"] = []map[string]any{
		{"action": "none", "targets": []string{}, "behaviors": []string{}},
		{"action": "key", "targets": []string{"1", "2", "3", "4"}, "behaviors": []string{"press", "toggle", "momentary"}},
		{"action": "menu", "targets": []string{"prev", "next", "dec", "inc"}, "behaviors": []string{}},
		{"action": "relay", "targets": []string{"5", "6", "7", "8"}, "behaviors": []string{"press", "toggle", "momentary"}},
		{"action": "side", "targets": []string{"a", "b"}, "behaviors": []string{"up", "down", "stop"}},
		{"action": "pwm", "targets": []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "behaviors": []string{"press", "toggle", "momentary"}},
	}
	instances := []hostui.AppInstance{}
	if service.AppInstances != nil {
		instances = service.AppInstances.List()
	}
	result["applications"] = instances
	if params.ReadBoard && snapshot.Connected {
		entries, err := service.Client.ListLearnedDetailed(ctx)
		if err != nil {
			result["board_error"] = err.Error()
		} else {
			result["entries"] = entries
			result["entries_sampled"] = true
		}
	}
	return result, nil
}

func (service *Service) putRFBinding(raw json.RawMessage) (any, error) {
	var params struct {
		Binding       appconfig.Automation `json:"binding"`
		PreviousName  string               `json:"previous_name,omitempty"`
		AllowKeyboard bool                 `json:"allow_keyboard,omitempty"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, err
	}
	rule := params.Binding
	if !isRFBinding(rule) || rule.Match.RFCode == nil || *rule.Match.RFCode == 0 || rule.Match.RFBits < 1 || rule.Match.RFBits > 32 || rule.Match.RFProtocol < 1 || rule.Match.RFProtocol > 12 {
		return nil, errors.New("RF binding requires rf.gesture/source rf and complete code, bits and protocol")
	}
	if rule.Match.Gesture == "" {
		return nil, errors.New("RF binding gesture is required")
	}
	if len(rule.Name) > 64 || strings.TrimSpace(rule.Name) == "" {
		return nil, errors.New("RF binding name must be 1..64 bytes")
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		// Never overwrite unrelated automations or create ambiguous duplicate presses.
		foundPrevious := params.PreviousName == ""
		rules := make([]appconfig.Automation, 0, len(config.Automations)+1)
		for _, existing := range config.Automations {
			if strings.EqualFold(existing.Name, rule.Name) || (params.PreviousName != "" && strings.EqualFold(existing.Name, params.PreviousName)) {
				if !isRFBinding(existing) {
					return errors.New("name belongs to a non-RF automation")
				}
				if params.PreviousName != "" && !strings.EqualFold(existing.Name, params.PreviousName) {
					return errors.New("another binding already has that name")
				}
				foundPrevious = true
				continue
			}
			if rule.Enabled && existing.Enabled && isRFBinding(existing) && existing.Match.RFCode != nil && *existing.Match.RFCode == *rule.Match.RFCode && existing.Match.RFBits == rule.Match.RFBits && existing.Match.RFProtocol == rule.Match.RFProtocol && existing.Match.Gesture == rule.Match.Gesture {
				return fmt.Errorf("button gesture already assigned to %q; edit that assignment instead", existing.Name)
			}
			rules = append(rules, existing)
		}
		if !foundPrevious {
			return errors.New("assignment changed or was removed; refresh before saving")
		}
		for _, action := range rule.Actions {
			if action.Type == "virtual-key" && params.AllowKeyboard {
				key, err := hostos.ResolveVirtualKey(action.VirtualKey)
				if err != nil {
					return err
				}
				config.OSActions.VirtualKeys.Enabled = true
				allowed := false
				for _, value := range config.OSActions.VirtualKeys.Allowed {
					resolved, err := hostos.ResolveVirtualKey(value)
					if err == nil && resolved.Code == key.Code {
						allowed = true
					}
				}
				if !allowed {
					config.OSActions.VirtualKeys.Allowed = append(config.OSActions.VirtualKeys.Allowed, key.Name)
				}
			}
		}
		config.Automations = append(rules, rule)
		return config.Validate()
	})
	if err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("rf.bindings.changed", "RF assignment saved")
	return rfBindings(service.hostConfig()), nil
}

func (service *Service) removeRFBinding(raw json.RawMessage) (any, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, err
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		rules := make([]appconfig.Automation, 0, len(config.Automations))
		found := false
		for _, rule := range config.Automations {
			if strings.EqualFold(rule.Name, params.Name) && isRFBinding(rule) {
				found = true
				continue
			}
			rules = append(rules, rule)
		}
		if !found {
			return errors.New("RF assignment was not found")
		}
		config.Automations = rules
		return config.Validate()
	})
	if err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("rf.bindings.changed", "RF assignment removed")
	return rfBindings(service.hostConfig()), nil
}
