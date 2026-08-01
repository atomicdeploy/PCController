package appconfig

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// HostMenuConfig defines PC-owned front-panel pages. These values never enter
// MCU EEPROM; the host renders them through the display protocol while a
// session is active.
type HostMenuConfig struct {
	DefaultMenu       string                `json:"default_menu"`
	RequestGesture    string                `json:"request_gesture"`
	DisplayDurationMS int                   `json:"display_duration_ms"`
	SessionTimeoutMS  int                   `json:"session_timeout_ms"`
	BuiltinOverrides  []BuiltinMenuOverride `json:"builtin_overrides,omitempty"`
	Menus             []HostMenu            `json:"menus"`
}

type HostMenu struct {
	ID         string         `json:"id"`
	NodeID     byte           `json:"node_id"`
	ParentID   byte           `json:"parent_id"`
	OrderID    byte           `json:"order_id"`
	Label      string         `json:"label"`
	Title      string         `json:"title"`
	Content    string         `json:"content,omitempty"`
	Brightness byte           `json:"brightness"`
	EditVisual string         `json:"edit_visual"`
	Flags      HostMenuFlags  `json:"flags"`
	Items      []HostMenuItem `json:"items"`
}

// BuiltinMenuOverride changes only the online host presentation for an
// immutable firmware stable ID. The AVR flash label remains the offline and
// timeout fallback; reordering never changes StableID or command mappings.
type BuiltinMenuOverride struct {
	StableID   byte          `json:"stable_id"`
	ParentID   byte          `json:"parent_id"`
	OrderID    byte          `json:"order_id"`
	Label      string        `json:"label"`
	Title      string        `json:"title"`
	Content    string        `json:"content,omitempty"`
	Brightness byte          `json:"brightness"`
	EditVisual string        `json:"edit_visual"`
	Flags      HostMenuFlags `json:"flags"`
}

type HostMenuFlags struct {
	Visible    bool `json:"visible"`
	Selectable bool `json:"selectable"`
	Editable   bool `json:"editable,omitempty"`
	Action     bool `json:"action,omitempty"`
	ReadOnly   bool `json:"read_only,omitempty"`
}

const (
	HostMenuRoot          byte = 0xFF
	HostMenuCategoryFirst byte = 0x70
	HostMenuCategoryLast  byte = 0x73
	HostMenuNodeFirst     byte = 0x80
	HostMenuNodeLast      byte = 0xEF
)

type HostMenuItem struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Title       string           `json:"title"`
	Type        string           `json:"type"`
	Value       string           `json:"value,omitempty"`
	Min         float64          `json:"min,omitempty"`
	Max         float64          `json:"max,omitempty"`
	Step        float64          `json:"step,omitempty"`
	Options     []HostMenuOption `json:"options,omitempty"`
	ReadAction  string           `json:"read_action,omitempty"`
	WriteAction string           `json:"write_action,omitempty"`
	Submenu     string           `json:"submenu,omitempty"`
	Guarded     bool             `json:"guarded,omitempty"`
	Disabled    bool             `json:"disabled,omitempty"`
}

type HostMenuOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func DefaultHostMenus() HostMenuConfig {
	return HostMenuConfig{
		DefaultMenu: "host", RequestGesture: "status-hold-k4",
		DisplayDurationMS: 1500, SessionTimeoutMS: 120000,
		Menus: []HostMenu{
			{ID: "host", NodeID: 0x80, ParentID: HostMenuRoot, OrderID: 0x80,
				Label: "HOST", Title: "PC Host", Content: "Controls ready", Brightness: 5,
				EditVisual: "blink", Flags: HostMenuFlags{Visible: true, Selectable: true}, Items: []HostMenuItem{
					{ID: "host-status", Label: "PC", Title: "Host status", Type: "readonly", ReadAction: "host.status"},
					{ID: "device-status", Label: "DEV", Title: "Device status", Type: "readonly", ReadAction: "device.status"},
					{ID: "ip", Label: "IP", Title: "Host IP", Type: "readonly", ReadAction: "host.ip"},
					{ID: "api", Label: "API", Title: "API and links", Type: "readonly", ReadAction: "api.status"},
					{ID: "settings", Label: "CFG", Title: "PC settings", Type: "submenu", Submenu: "pc-settings"},
					{ID: "system", Label: "SYS", Title: "System actions", Type: "submenu", Submenu: "system-actions"},
				}},
			{ID: "pc-settings", NodeID: 0x81, ParentID: 0x80, OrderID: 0x81,
				Label: "CFG", Title: "PC Settings", Content: "Configuration", Brightness: 5,
				EditVisual: "blink", Flags: HostMenuFlags{Visible: true, Selectable: true, Editable: true}, Items: []HostMenuItem{
					{ID: "app-title", Label: "NAME", Title: "Application title", Type: "text", ReadAction: "pc.ui.app_title", WriteAction: "pc.ui.app_title"},
					{ID: "poll", Label: "POLL", Title: "Polling ms", Type: "number", Value: "200", Min: 100, Max: 5000, Step: 50, ReadAction: "pc.ui.status_interval_ms", WriteAction: "pc.ui.status_interval_ms"},
					{ID: "lcd-service", Label: "I2C", Title: "PC LCD service", Type: "bool", Value: "true", ReadAction: "pc.ui.lcd_service_enabled", WriteAction: "pc.ui.lcd_service_enabled"},
					{ID: "lcd-mirror", Label: "LCD", Title: "Prompt mirror", Type: "bool", Value: "false", ReadAction: "pc.ui.mirror_prompt_to_lcd", WriteAction: "pc.ui.mirror_prompt_to_lcd"},
					{ID: "dtr", Label: "DTR", Title: "Reset reconnect", Type: "bool", Value: "false", ReadAction: "pc.connection.reset_on_reconnect", WriteAction: "pc.connection.reset_on_reconnect"},
				}},
			{ID: "system-actions", NodeID: 0x82, ParentID: 0x80, OrderID: 0x82,
				Label: "SYS", Title: "System Actions", Content: "Guarded actions", Brightness: 5,
				EditVisual: "pulse", Flags: HostMenuFlags{Visible: true, Selectable: true, Action: true}, Items: []HostMenuItem{
					{ID: "brightness", Label: "BRIT", Title: "Monitor brightness", Type: "number", Value: "50", Min: 0, Max: 100, Step: 5, ReadAction: "os.brightness", WriteAction: "os.brightness", Guarded: true},
					{ID: "lock", Label: "LOCK", Title: "Lock Windows", Type: "action", WriteAction: "os.lock", Guarded: true},
					{ID: "suspend", Label: "SUSP", Title: "Suspend Windows", Type: "action", WriteAction: "os.sleep", Guarded: true},
					{ID: "hibernate", Label: "HIBR", Title: "Hibernate Windows", Type: "action", WriteAction: "os.hibernate", Guarded: true},
					{ID: "restart", Label: "RSTR", Title: "Restart Windows", Type: "action", WriteAction: "os.restart", Guarded: true},
					{ID: "shutdown", Label: "OFF", Title: "Shut down PC", Type: "action", WriteAction: "os.shutdown", Guarded: true},
				}},
		},
	}
}

var hostMenuIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

func validateHostMenus(config HostMenuConfig) error {
	if config.DisplayDurationMS < 250 || config.DisplayDurationMS > 60000 {
		return fmt.Errorf("host_menus.display_duration_ms must be 250..60000")
	}
	if config.SessionTimeoutMS < 5000 || config.SessionTimeoutMS > 3600000 {
		return fmt.Errorf("host_menus.session_timeout_ms must be 5000..3600000")
	}
	if config.RequestGesture != "status-hold-k4" && config.RequestGesture != "disabled" {
		return fmt.Errorf("host_menus.request_gesture must be status-hold-k4 or disabled")
	}
	if len(config.Menus) == 0 || len(config.Menus)+len(config.BuiltinOverrides) > 8 {
		return fmt.Errorf("host_menus runtime overlay must contain 1..8 host nodes and built-in overrides combined")
	}
	menus := make(map[string]bool)
	nodes := make(map[byte]bool)
	for index, menu := range config.Menus {
		if !hostMenuIDPattern.MatchString(menu.ID) || menus[menu.ID] {
			return fmt.Errorf("host_menus.menus[%d].id is invalid or duplicated", index)
		}
		menus[menu.ID] = true
		if menu.NodeID < HostMenuNodeFirst || menu.NodeID > HostMenuNodeLast || nodes[menu.NodeID] {
			return fmt.Errorf("host_menus.menus[%d].node_id must be unique in 128..239", index)
		}
		nodes[menu.NodeID] = true
		if menu.OrderID != menu.NodeID {
			return fmt.Errorf("host_menus.menus[%d].order_id must equal host node_id; host siblings sort by stable host ID", index)
		}
		if !shortHostLabel(menu.Label) || !hostMenuText(menu.Title, 16) {
			return fmt.Errorf("host_menus.menus[%d] requires a 1..4 ASCII label and 1..16 character title", index)
		}
		if menu.Content != "" && (!hostMenuText(menu.Content, 16) || !printableASCII(menu.Content)) {
			return fmt.Errorf("host_menus.menus[%d].content must fit LCD line 2 (16 printable ASCII bytes)", index)
		}
		if err := validateHostMenuPresentation(menu.Brightness, menu.EditVisual, menu.Flags); err != nil {
			return fmt.Errorf("host_menus.menus[%d]: %w", index, err)
		}
		if len(menu.Items) == 0 || len(menu.Items) > 32 {
			return fmt.Errorf("host_menus.menus[%d].items must contain 1..32 entries", index)
		}
		items := make(map[string]bool)
		for itemIndex, item := range menu.Items {
			path := fmt.Sprintf("host_menus.menus[%d].items[%d]", index, itemIndex)
			if !hostMenuIDPattern.MatchString(item.ID) || items[item.ID] {
				return fmt.Errorf("%s.id is invalid or duplicated", path)
			}
			items[item.ID] = true
			if !shortHostLabel(item.Label) || !hostMenuText(item.Title, 24) {
				return fmt.Errorf("%s requires a 1..4 ASCII label and 1..24 character title", path)
			}
			switch item.Type {
			case "readonly":
				if item.ReadAction == "" {
					return fmt.Errorf("%s readonly item requires read_action", path)
				}
			case "text":
			case "bool":
				if _, err := strconv.ParseBool(item.Value); item.Value != "" && err != nil {
					return fmt.Errorf("%s.value must be true or false", path)
				}
			case "number":
				if item.Max <= item.Min || item.Step <= 0 || item.Step > item.Max-item.Min {
					return fmt.Errorf("%s number range/step is invalid", path)
				}
				if item.Value != "" {
					value, err := strconv.ParseFloat(item.Value, 64)
					if err != nil || value < item.Min || value > item.Max {
						return fmt.Errorf("%s.value is outside its range", path)
					}
				}
			case "select":
				if len(item.Options) == 0 || len(item.Options) > 16 {
					return fmt.Errorf("%s.options must contain 1..16 values", path)
				}
				seen := make(map[string]bool)
				for optionIndex, option := range item.Options {
					if !hostMenuText(option.Label, 16) || option.Value == "" || seen[option.Value] {
						return fmt.Errorf("%s.options[%d] is invalid or duplicated", path, optionIndex)
					}
					seen[option.Value] = true
				}
			case "submenu":
				if item.Submenu == "" {
					return fmt.Errorf("%s submenu requires submenu target", path)
				}
			case "action":
				if item.WriteAction == "" {
					return fmt.Errorf("%s action requires write_action", path)
				}
			default:
				return fmt.Errorf("%s.type %q is unknown", path, item.Type)
			}
			for _, action := range []string{item.ReadAction, item.WriteAction} {
				if len(action) > 256 || strings.ContainsAny(action, "\r\n") {
					return fmt.Errorf("%s action is invalid", path)
				}
			}
			if strings.HasPrefix(item.WriteAction, "os.") && !item.Guarded {
				return fmt.Errorf("%s OS action must be guarded", path)
			}
		}
	}
	builtinIDs := make(map[byte]bool)
	for index, override := range config.BuiltinOverrides {
		if override.StableID > 14 || builtinIDs[override.StableID] {
			return fmt.Errorf("host_menus.builtin_overrides[%d].stable_id must be unique in 0..14", index)
		}
		builtinIDs[override.StableID] = true
		if override.OrderID > 14 {
			return fmt.Errorf("host_menus.builtin_overrides[%d].order_id must be a presentation rank in 0..14", index)
		}
		if !shortHostLabel(override.Label) || !hostMenuText(override.Title, 16) {
			return fmt.Errorf("host_menus.builtin_overrides[%d] requires a 1..4 ASCII label and 1..16 character title", index)
		}
		if override.Content != "" && (!hostMenuText(override.Content, 16) || !printableASCII(override.Content)) {
			return fmt.Errorf("host_menus.builtin_overrides[%d].content must fit LCD line 2 (16 printable ASCII bytes)", index)
		}
		if err := validateHostMenuPresentation(override.Brightness, override.EditVisual, override.Flags); err != nil {
			return fmt.Errorf("host_menus.builtin_overrides[%d]: %w", index, err)
		}
	}
	if !menus[config.DefaultMenu] {
		return fmt.Errorf("host_menus.default_menu %q does not exist", config.DefaultMenu)
	}
	for _, menu := range config.Menus {
		if menu.ID == config.DefaultMenu && (!menu.Flags.Visible || !menu.Flags.Selectable) {
			return fmt.Errorf("host_menus.default_menu %q must be visible and selectable", config.DefaultMenu)
		}
	}
	for menuIndex, menu := range config.Menus {
		if !validHostMenuParent(menu.ParentID, nodes) {
			return fmt.Errorf("host_menus.menus[%d].parent_id 0x%02X does not identify root, a fixed category, or another host node", menuIndex, menu.ParentID)
		}
		for itemIndex, item := range menu.Items {
			if item.Type == "submenu" && !menus[item.Submenu] {
				return fmt.Errorf("host_menus.menus[%d].items[%d].submenu %q does not exist", menuIndex, itemIndex, item.Submenu)
			}
		}
	}
	for index, override := range config.BuiltinOverrides {
		if !validHostMenuParent(override.ParentID, nodes) {
			return fmt.Errorf("host_menus.builtin_overrides[%d].parent_id 0x%02X is invalid", index, override.ParentID)
		}
	}
	for _, menu := range config.Menus {
		seen := map[byte]bool{menu.NodeID: true}
		parent := menu.ParentID
		for parent >= HostMenuNodeFirst && parent <= HostMenuNodeLast {
			if seen[parent] {
				return fmt.Errorf("host_menus contains a parent cycle through node_id 0x%02X", parent)
			}
			seen[parent] = true
			found := false
			for _, candidate := range config.Menus {
				if candidate.NodeID == parent {
					parent = candidate.ParentID
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
	}
	return nil
}

func validateHostMenuPresentation(brightness byte, visual string, flags HostMenuFlags) error {
	if brightness > 7 && brightness != 0xFF {
		return fmt.Errorf("brightness must be 0..7 or 255 to keep the board setting")
	}
	switch strings.ToLower(strings.TrimSpace(visual)) {
	case "steady", "blink", "edit-dim", "alternate", "pulse":
	default:
		return fmt.Errorf("edit_visual must be steady, blink, edit-dim, alternate, or pulse")
	}
	if flags.ReadOnly && (flags.Editable || flags.Action) {
		return fmt.Errorf("flags.read_only cannot be combined with editable or action")
	}
	return nil
}

func validHostMenuParent(parent byte, nodes map[byte]bool) bool {
	return parent == HostMenuRoot ||
		(parent >= HostMenuCategoryFirst && parent <= HostMenuCategoryLast) ||
		nodes[parent]
}

func shortHostLabel(value string) bool {
	return len(value) >= 1 && len(value) <= 4 && printableASCII(value)
}

func hostMenuText(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len([]rune(value)) <= maximum && printableText(value)
}

func cloneHostMenus(source HostMenuConfig) HostMenuConfig {
	result := source
	result.BuiltinOverrides = append([]BuiltinMenuOverride(nil), source.BuiltinOverrides...)
	result.Menus = make([]HostMenu, len(source.Menus))
	for menuIndex, menu := range source.Menus {
		result.Menus[menuIndex] = menu
		result.Menus[menuIndex].Items = make([]HostMenuItem, len(menu.Items))
		for itemIndex, item := range menu.Items {
			result.Menus[menuIndex].Items[itemIndex] = item
			result.Menus[menuIndex].Items[itemIndex].Options = append([]HostMenuOption(nil), item.Options...)
		}
	}
	return result
}

// canonicalizeHostMenus upgrades pre-directory development configurations in
// memory. It preserves human string IDs/actions while assigning compact host
// IDs and inferred parent relations; the next explicit save writes the fields.
func canonicalizeHostMenus(source HostMenuConfig) HostMenuConfig {
	result := cloneHostMenus(source)
	stringToNode := make(map[string]byte, len(result.Menus))
	legacy := make([]bool, len(result.Menus))
	for index := range result.Menus {
		menu := &result.Menus[index]
		if menu.NodeID < HostMenuNodeFirst || menu.NodeID > HostMenuNodeLast {
			legacy[index] = true
			menu.NodeID = HostMenuNodeFirst + byte(index)
		}
		menu.OrderID = menu.NodeID
		stringToNode[menu.ID] = menu.NodeID
		if menu.EditVisual == "" {
			menu.EditVisual = "blink"
			if menu.Brightness == 0 {
				menu.Brightness = 5
			}
		}
		if !menu.Flags.Visible && !menu.Flags.Selectable && !menu.Flags.Editable && !menu.Flags.Action && !menu.Flags.ReadOnly {
			menu.Flags.Visible, menu.Flags.Selectable = true, true
		}
		if menu.Content == "" {
			menu.Content = menu.Title
		}
		if legacy[index] {
			menu.ParentID = HostMenuRoot
		}
	}
	for _, parent := range result.Menus {
		for _, item := range parent.Items {
			if item.Type != "submenu" {
				continue
			}
			for index := range result.Menus {
				if legacy[index] && result.Menus[index].ID == item.Submenu {
					result.Menus[index].ParentID = stringToNode[parent.ID]
				}
			}
		}
	}
	for index := range result.BuiltinOverrides {
		override := &result.BuiltinOverrides[index]
		if override.EditVisual == "" {
			override.EditVisual = "blink"
			if override.Brightness == 0 {
				override.Brightness = 5
			}
		}
		if !override.Flags.Visible && !override.Flags.Selectable && !override.Flags.Editable && !override.Flags.Action && !override.Flags.ReadOnly {
			override.Flags.Visible, override.Flags.Selectable = true, true
		}
	}
	return result
}
