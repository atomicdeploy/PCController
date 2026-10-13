package ipcjson

import (
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type rfBoardAssignment struct {
	Action   string `json:"action"`
	Target   string `json:"target"`
	Behavior string `json:"behavior"`
	Label    string `json:"label"`
}

type rfCatalogEntry struct {
	controller.RFEntryView
	LastSeen        *time.Time        `json:"last_seen,omitempty"`
	BoardAssignment rfBoardAssignment `json:"board_assignment"`
}

func describeRFBoardAssignment(entry controller.RFEntryView, controls []appconfig.ControlDescriptor) rfBoardAssignment {
	result := rfBoardAssignment{Action: "unsupported", Label: "Unsupported board assignment"}
	behavior, valid := map[byte]string{native.RFBehaviorPress: "press", native.RFBehaviorToggle: "toggle", native.RFBehaviorMomentary: "momentary", native.RFBehaviorUp: "up", native.RFBehaviorDown: "down", native.RFBehaviorStop: "stop"}[entry.Behavior]
	if !valid {
		return result
	}
	label := ""
	key := ""
	switch entry.ActionKind {
	case native.RFActionNone:
		return rfBoardAssignment{Action: "none", Label: "Unassigned"}
	case native.RFActionKey:
		if entry.ActionValue > 3 || entry.Behavior > native.RFBehaviorMomentary {
			return result
		}
		result.Action, result.Target = "key", strconv.Itoa(int(entry.ActionValue)+1)
		label = "Key " + result.Target
	case native.RFActionMenu:
		target, ok := map[byte]string{native.MenuPrevious: "prev", native.MenuNext: "next", native.MenuDecrease: "dec", native.MenuIncrease: "inc"}[entry.ActionValue]
		if !ok {
			return result
		}
		return rfBoardAssignment{Action: "menu", Target: target, Label: "Menu " + target}
	case native.RFActionRelay:
		if entry.ActionValue > 7 || entry.Behavior > native.RFBehaviorMomentary {
			return result
		}
		result.Action, result.Target = "relay", strconv.Itoa(int(entry.ActionValue)+1)
		key, label = "relay."+result.Target, "Relay "+result.Target
	case native.RFActionSide:
		if entry.ActionValue > 1 || entry.Behavior < native.RFBehaviorUp {
			return result
		}
		result.Action, result.Target = "side", []string{"a", "b"}[entry.ActionValue]
		key, label = "seat."+result.Target, "Seat "+strings.ToUpper(result.Target)
	case native.RFActionPWM:
		if entry.ActionValue > 10 || entry.Behavior > native.RFBehaviorMomentary {
			return result
		}
		result.Action, result.Target = "pwm", strconv.Itoa(int(entry.ActionValue))
		key, label = "pwm."+result.Target, "MOSFET "+result.Target
	default:
		return result
	}
	for _, control := range controls {
		if control.Key == key && strings.TrimSpace(control.Name) != "" {
			label = control.Name
			break
		}
	}
	result.Behavior = behavior
	if result.Action == "pwm" {
		if behavior == "momentary" {
			label += " · full on while held, off after 350 ms silence"
		} else {
			label += " · toggle 0/100%"
		}
	} else {
		label += " · " + behavior
	}
	result.Label = label
	return result
}

func enrichRFCatalogEntries(entries []controller.RFEntryView, controls []appconfig.ControlDescriptor, lastSeen func(uint32, byte, byte) time.Time) []rfCatalogEntry {
	result := make([]rfCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		value := rfCatalogEntry{RFEntryView: entry, BoardAssignment: describeRFBoardAssignment(entry, controls)}
		if seen := lastSeen(entry.Code, entry.Bits, entry.Protocol); !seen.IsZero() {
			copy := seen
			value.LastSeen = &copy
		}
		result = append(result, value)
	}
	return result
}
