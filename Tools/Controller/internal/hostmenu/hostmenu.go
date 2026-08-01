// Package hostmenu implements PC-owned front-panel menus whose definitions live
// in the host JSON/YAML/TOML configuration, never in the controller EEPROM.
package hostmenu

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

// Callbacks connect declarative menu actions to application state and the
// existing validated command/API paths.
type Callbacks struct {
	Read              func(context.Context, string) (string, error)
	Write             func(context.Context, string, string) (string, error)
	Execute           func(context.Context, string) (string, error)
	SaveConfig        func(appconfig.HostMenuConfig) error
	DefinitionChanged func(DefinitionChange)
	Interaction       func(InteractionEvent)
}

// Panel is the exact host-owned content mirrored to the physical and virtual
// front panels.
type Panel struct {
	Segments   string `json:"segments"`
	LCDLine1   string `json:"lcd_line_1"`
	LCDLine2   string `json:"lcd_line_2"`
	Blink      bool   `json:"blink"`
	Brightness byte   `json:"brightness"`
	EditVisual string `json:"edit_visual"`
}

// Snapshot is safe to render from the TUI, API, IPC, or WebSocket bridge.
type Snapshot struct {
	Active       bool   `json:"active"`
	MenuID       string `json:"menu_id,omitempty"`
	NodeID       byte   `json:"node_id,omitempty"`
	MenuTitle    string `json:"menu_title,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	ItemTitle    string `json:"item_title,omitempty"`
	Value        string `json:"value,omitempty"`
	Cursor       int    `json:"cursor"`
	Count        int    `json:"count"`
	Depth        int    `json:"depth"`
	GuardPending bool   `json:"guard_pending"`
	Status       string `json:"status,omitempty"`
	Revision     uint64 `json:"revision"`
	Panel        Panel  `json:"panel"`
}

// DefinitionChange is the normalized event shared by the internal event bus,
// JSON-RPC/REST/WebSocket clients, scripts, and the TUI. It deliberately uses
// the stable numeric node identity rather than a presentation rank.
type DefinitionChange struct {
	Kind       string   `json:"kind"`
	MenuID     string   `json:"menu_id,omitempty"`
	NodeID     byte     `json:"node_id"`
	Builtin    bool     `json:"builtin"`
	Fields     []string `json:"fields"`
	Generation byte     `json:"generation"`
	Active     bool     `json:"active"`
	Snapshot   Snapshot `json:"snapshot,omitempty"`
}

// InteractionEvent reports denied or completed front-panel interactions to
// the host event bus without relying on a live board connection.
type InteractionEvent struct {
	Kind     string   `json:"kind"`
	Reason   string   `json:"reason"`
	Key      int      `json:"key"`
	Phase    string   `json:"phase"`
	MenuID   string   `json:"menu_id,omitempty"`
	ItemID   string   `json:"item_id,omitempty"`
	Snapshot Snapshot `json:"snapshot"`
}

var ErrInteractionDenied = errors.New("host-menu interaction is disabled or read-only")

type sessionFrame struct {
	menuID string
	cursor int
}

// Manager is a concurrency-safe host-menu session shared by TUI, IPC, API,
// WebSocket clients, and physical key events.
type Manager struct {
	mu         sync.Mutex
	config     appconfig.HostMenuConfig
	callbacks  Callbacks
	stack      []sessionFrame
	values     map[string]string
	guard      string
	status     string
	lastInput  time.Time
	revision   uint64
	generation byte
}

func New(config appconfig.HostMenuConfig, callbacks Callbacks) *Manager {
	return &Manager{config: cloneConfig(config), callbacks: callbacks, values: make(map[string]string), generation: 1}
}

func (manager *Manager) SetDefinitionChanged(callback func(DefinitionChange)) {
	manager.mu.Lock()
	manager.callbacks.DefinitionChanged = callback
	manager.mu.Unlock()
}

// UpdateConfig applies a validated file-watcher update without discarding an
// active session when its current menu still exists.
func (manager *Manager) UpdateConfig(config appconfig.HostMenuConfig) {
	manager.mu.Lock()
	previous := cloneConfig(manager.config)
	manager.config = cloneConfig(config)
	if len(manager.stack) != 0 {
		if _, ok := manager.menuLocked(manager.stack[len(manager.stack)-1].menuID); !ok {
			manager.stack = nil
			manager.status = "Menu removed or hidden by config reload"
		}
	}
	manager.generation++
	if manager.generation == 0 {
		manager.generation = 1
	}
	manager.revision++
	changes := definitionChanges(previous, manager.config, manager.generation)
	snapshot := manager.snapshotLocked()
	for index := range changes {
		changes[index].Active = snapshot.Active && changes[index].NodeID == snapshot.NodeID
		if changes[index].Active {
			preview := snapshot
			for _, menu := range manager.config.Menus {
				if menu.NodeID == changes[index].NodeID {
					preview.Panel = Panel{
						Segments: menu.Label, LCDLine1: fit(menu.Title, 16), LCDLine2: fit(menu.Content, 16),
						Brightness: menu.Brightness, EditVisual: menu.EditVisual,
						Blink: menu.EditVisual == "blink" || menu.EditVisual == "alternate" || menu.EditVisual == "pulse",
					}
					break
				}
			}
			changes[index].Snapshot = preview
		}
	}
	callback := manager.callbacks.DefinitionChanged
	manager.mu.Unlock()
	if callback != nil {
		for _, change := range changes {
			callback(change)
		}
	}
}

func (manager *Manager) Config() appconfig.HostMenuConfig {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return cloneConfig(manager.config)
}

// Directory returns one fully validated replace-all generation for firmware or
// the virtual board. The caller still capability-gates transmission.
func (manager *Manager) Directory() (native.HostMenuDirectory, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entries := make([]native.HostMenuDirectoryEntry, 0, len(manager.config.Menus)+len(manager.config.BuiltinOverrides))
	byID := make(map[byte]appconfig.HostMenu, len(manager.config.Menus))
	for _, menu := range manager.config.Menus {
		byID[menu.NodeID] = menu
	}
	advertised := func(parent byte) bool {
		seen := make(map[byte]bool)
		for parent >= appconfig.HostMenuNodeFirst && parent <= appconfig.HostMenuNodeLast {
			if seen[parent] {
				return false
			}
			seen[parent] = true
			menu, ok := byID[parent]
			if !ok || !menuIsVisible(menu.Flags) {
				return false
			}
			parent = menu.ParentID
		}
		return parent == appconfig.HostMenuRoot || (parent >= appconfig.HostMenuCategoryFirst && parent <= appconfig.HostMenuCategoryLast)
	}
	for _, override := range manager.config.BuiltinOverrides {
		if !override.Flags.Visible || !advertised(override.ParentID) {
			continue
		}
		entries = append(entries, native.HostMenuDirectoryEntry{
			ID: override.StableID, Parent: override.ParentID,
			Flags: presentationFlags(override.Flags) | native.HostMenuBuiltinLabelOverride | native.HostMenuLiveContent,
		})
	}
	for _, menu := range manager.config.Menus {
		if !menuIsVisible(menu.Flags) || !advertised(menu.ParentID) {
			continue
		}
		entries = append(entries, native.HostMenuDirectoryEntry{
			ID: menu.NodeID, Parent: menu.ParentID,
			Flags: presentationFlags(menu.Flags) | native.HostMenuLiveContent,
		})
	}
	directory := native.HostMenuDirectory{Schema: native.HostMenuSchema, Generation: manager.generation, Entries: entries}
	return directory, native.ValidateHostMenuDirectory(directory)
}

func (manager *Manager) Generation() byte {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.generation
}

// SaveConfig persists one PC-owned definition edit. The appconfig Store owns
// atomic JSON/YAML/TOML writes and validation; its watcher calls UpdateConfig,
// which then emits normalized changes and refreshes active previews.
func (manager *Manager) SaveConfig(change func(*appconfig.HostMenuConfig) error) error {
	if change == nil {
		return errors.New("host-menu configuration change is nil")
	}
	manager.mu.Lock()
	config := cloneConfig(manager.config)
	save := manager.callbacks.SaveConfig
	manager.mu.Unlock()
	if save == nil {
		return errors.New("persistent host-menu configuration is unavailable")
	}
	if err := change(&config); err != nil {
		return err
	}
	return save(config)
}

func (manager *Manager) SetDefinitionField(reference, field, value string) error {
	return manager.SaveConfig(func(config *appconfig.HostMenuConfig) error {
		builtin, numericID, index, err := findDefinition(config, reference)
		if err != nil {
			return err
		}
		field = strings.ToLower(strings.TrimSpace(field))
		if builtin {
			override := &config.BuiltinOverrides[index]
			return setBuiltinField(override, field, value)
		}
		menu := &config.Menus[index]
		oldID := menu.NodeID
		if err := setHostMenuField(menu, field, value); err != nil {
			return err
		}
		if field == "node_id" && oldID != menu.NodeID {
			for candidate := range config.Menus {
				if config.Menus[candidate].ParentID == oldID {
					config.Menus[candidate].ParentID = menu.NodeID
				}
			}
			for candidate := range config.BuiltinOverrides {
				if config.BuiltinOverrides[candidate].ParentID == oldID {
					config.BuiltinOverrides[candidate].ParentID = menu.NodeID
				}
			}
		}
		_ = numericID
		return nil
	})
}

func (manager *Manager) AddDefinition(nodeID byte, id, label, title string, parent byte) error {
	return manager.SaveConfig(func(config *appconfig.HostMenuConfig) error {
		config.Menus = append(config.Menus, appconfig.HostMenu{
			ID: strings.TrimSpace(id), NodeID: nodeID, ParentID: parent, OrderID: nodeID,
			Label: label, Title: title, Content: title, Brightness: 5, EditVisual: "blink",
			Flags: appconfig.HostMenuFlags{Visible: true, Selectable: true, ReadOnly: true},
			Items: []appconfig.HostMenuItem{{ID: "status", Label: label, Title: title, Type: "readonly", ReadAction: "host.status"}},
		})
		return nil
	})
}

func (manager *Manager) UpsertBuiltinOverride(stableID byte, label, title string, parent byte) error {
	return manager.SaveConfig(func(config *appconfig.HostMenuConfig) error {
		for index := range config.BuiltinOverrides {
			if config.BuiltinOverrides[index].StableID == stableID {
				config.BuiltinOverrides[index].Label = label
				config.BuiltinOverrides[index].Title = title
				config.BuiltinOverrides[index].ParentID = parent
				return nil
			}
		}
		config.BuiltinOverrides = append(config.BuiltinOverrides, appconfig.BuiltinMenuOverride{
			StableID: stableID, ParentID: parent, OrderID: stableID,
			Label: label, Title: title, Content: title, Brightness: 5, EditVisual: "blink",
			Flags: appconfig.HostMenuFlags{Visible: true, Selectable: true, ReadOnly: true},
		})
		return nil
	})
}

func (manager *Manager) RemoveDefinition(reference string) error {
	return manager.SaveConfig(func(config *appconfig.HostMenuConfig) error {
		builtin, nodeID, index, err := findDefinition(config, reference)
		if err != nil {
			return err
		}
		if builtin {
			config.BuiltinOverrides = append(config.BuiltinOverrides[:index], config.BuiltinOverrides[index+1:]...)
			return nil
		}
		for _, menu := range config.Menus {
			if menu.ParentID == nodeID {
				return fmt.Errorf("host-menu node 0x%02X still has child %q", nodeID, menu.ID)
			}
		}
		if config.DefaultMenu == config.Menus[index].ID {
			return errors.New("default host menu cannot be removed")
		}
		config.Menus = append(config.Menus[:index], config.Menus[index+1:]...)
		return nil
	})
}

func findDefinition(config *appconfig.HostMenuConfig, reference string) (bool, byte, int, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if strings.HasPrefix(reference, "builtin:") {
		value, err := strconv.ParseUint(strings.TrimPrefix(reference, "builtin:"), 0, 8)
		if err != nil {
			return false, 0, 0, fmt.Errorf("invalid built-in stable ID %q", reference)
		}
		for index, override := range config.BuiltinOverrides {
			if override.StableID == byte(value) {
				return true, byte(value), index, nil
			}
		}
		return false, 0, 0, fmt.Errorf("built-in override %d does not exist", value)
	}
	for index, menu := range config.Menus {
		if strings.EqualFold(menu.ID, reference) || strings.EqualFold(fmt.Sprintf("0x%02X", menu.NodeID), reference) || strconv.Itoa(int(menu.NodeID)) == reference {
			return false, menu.NodeID, index, nil
		}
	}
	return false, 0, 0, fmt.Errorf("host-menu definition %q does not exist", reference)
}

func setHostMenuField(menu *appconfig.HostMenu, field, value string) error {
	switch field {
	case "label", "segments":
		menu.Label = value
	case "title", "lcd_name":
		menu.Title = value
	case "content", "lcd_content":
		menu.Content = value
	case "id", "key":
		menu.ID = value
	case "node_id", "host_id":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		menu.NodeID, menu.OrderID = byte(parsed), byte(parsed)
	case "parent", "parent_id":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		menu.ParentID = byte(parsed)
	case "brightness":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		menu.Brightness = byte(parsed)
	case "edit_visual", "visual":
		menu.EditVisual = strings.ToLower(value)
	case "read_only", "readonly":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		menu.Flags.ReadOnly = parsed
		if parsed {
			menu.Flags.Editable, menu.Flags.Action = false, false
		}
	case "editable":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		menu.Flags.Editable = parsed
		if parsed {
			menu.Flags.ReadOnly = false
		}
	case "action":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		menu.Flags.Action = parsed
		if parsed {
			menu.Flags.ReadOnly = false
		}
	case "visible":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		menu.Flags.Visible = parsed
	case "selectable":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		menu.Flags.Selectable = parsed
	default:
		return fmt.Errorf("unknown host-menu field %q", field)
	}
	return nil
}

func setBuiltinField(override *appconfig.BuiltinMenuOverride, field, value string) error {
	switch field {
	case "label", "segments":
		override.Label = value
	case "title", "lcd_name":
		override.Title = value
	case "content", "lcd_content":
		override.Content = value
	case "parent", "parent_id":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		override.ParentID = byte(parsed)
	case "order", "order_id", "rank":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		override.OrderID = byte(parsed)
	case "brightness":
		parsed, err := strconv.ParseUint(value, 0, 8)
		if err != nil {
			return err
		}
		override.Brightness = byte(parsed)
	case "edit_visual", "visual":
		override.EditVisual = strings.ToLower(value)
	case "read_only", "readonly":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		override.Flags.ReadOnly = parsed
	default:
		return fmt.Errorf("unknown built-in override field %q", field)
	}
	return nil
}

// Content resolves a built-in label override or a host-only node from the same
// watched configuration model used by the TUI preview.
func (manager *Manager) Content(nodeID byte) (native.HostMenuContent, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, override := range manager.config.BuiltinOverrides {
		if override.StableID == nodeID {
			line1, line2 := splitLCDContent(override.Title, override.Content)
			return native.HostMenuContent{
				Schema: native.HostMenuSchema, Generation: manager.generation, ID: nodeID,
				Revision: byte(manager.revision), Flags: presentationFlags(override.Flags) | native.HostMenuBuiltinLabelOverride | native.HostMenuLiveContent,
				Brightness: override.Brightness, Visual: presentationVisual(override.EditVisual),
				Segments: override.Label, LCDLine1: line1, LCDLine2: line2,
			}, nil
		}
	}
	for _, menu := range manager.config.Menus {
		if menu.NodeID == nodeID {
			line1, line2 := splitLCDContent(menu.Title, menu.Content)
			return native.HostMenuContent{
				Schema: native.HostMenuSchema, Generation: manager.generation, ID: nodeID,
				Revision: byte(manager.revision), Flags: presentationFlags(menu.Flags) | native.HostMenuLiveContent,
				Brightness: menu.Brightness, Visual: presentationVisual(menu.EditVisual),
				Segments: menu.Label, LCDLine1: line1, LCDLine2: line2,
			}, nil
		}
	}
	return native.HostMenuContent{}, fmt.Errorf("host-menu node 0x%02X does not exist", nodeID)
}

func presentationFlags(flags appconfig.HostMenuFlags) byte {
	var result byte
	if flags.Visible {
		result |= native.HostMenuVisible
	}
	if flags.Selectable {
		result |= native.HostMenuSelectable
	}
	if flags.Editable {
		result |= native.HostMenuEditable
	}
	if flags.Action {
		result |= native.HostMenuAction
	}
	if flags.ReadOnly {
		result |= native.HostMenuReadOnly
	}
	return result
}

func presentationVisual(value string) byte {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blink":
		return native.HostMenuVisualBlink
	case "edit-dim":
		return native.HostMenuVisualEditDim
	case "alternate", "pulse":
		return native.HostMenuVisualAlternate
	default:
		return native.HostMenuVisualSteady
	}
}

func splitLCDContent(title, content string) (string, string) {
	return fit(title, 16), fit(content, 16)
}

func builtinCategory(stableID byte) byte {
	switch stableID {
	case 0, 1, 2, 3, 4:
		return 0x70
	case 5, 6, 7:
		return 0x71
	case 8, 9, 11, 12, 13:
		return 0x72
	case 10, 14:
		return 0x73
	default:
		return appconfig.HostMenuRoot
	}
}

func (manager *Manager) Open(menuID string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if strings.TrimSpace(menuID) == "" {
		menuID = manager.config.DefaultMenu
	}
	if _, ok := manager.menuLocked(menuID); !ok {
		return fmt.Errorf("host menu %q does not exist", menuID)
	}
	manager.stack = []sessionFrame{{menuID: menuID}}
	manager.guard = ""
	manager.status = "Host menu active"
	manager.lastInput = time.Now()
	manager.revision++
	return nil
}

func (manager *Manager) Close(reason string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.stack = nil
	manager.guard = ""
	manager.status = strings.TrimSpace(reason)
	manager.revision++
}

// Snapshot expires idle sessions and returns the exact shared representation.
func (manager *Manager) Snapshot() Snapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked(time.Now())
	return manager.snapshotLocked()
}

// HandleKey applies a physical or virtual K1..K4 gesture. K1/K2 navigate,
// K3/K4 decrease/increase editable values, K4 enters pages/actions, holding K3
// backs out, and a guarded action requires a second K4 hold.
func (manager *Manager) HandleKey(ctx context.Context, key int, phase string) (Snapshot, error) {
	phase = strings.ToLower(strings.TrimSpace(phase))
	if key < 1 || key > 4 {
		return manager.Snapshot(), fmt.Errorf("host-menu key %d is outside K1..K4", key)
	}
	manager.mu.Lock()
	manager.expireLocked(time.Now())
	if len(manager.stack) == 0 {
		manager.mu.Unlock()
		return manager.Snapshot(), errors.New("host-menu session is not active")
	}
	manager.lastInput = time.Now()
	if phase == "release" || phase == "double" {
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
	if key == 3 && phase == "hold" {
		if len(manager.stack) > 1 {
			manager.stack = manager.stack[:len(manager.stack)-1]
			manager.status = "Back"
		} else {
			manager.stack = nil
			manager.status = "Host menu closed"
		}
		manager.guard = ""
		manager.revision++
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
	menu, item, path, ok := manager.selectionLocked()
	if !ok {
		manager.mu.Unlock()
		return manager.Snapshot(), errors.New("host-menu selection is invalid")
	}
	if (key == 3 || key == 4) &&
		(menu.Flags.ReadOnly || item.Disabled || (key == 4 && (item.Type == "readonly" || item.Type == "text"))) {
		reason := "selected item is read-only"
		if item.Disabled {
			reason = "selected item is disabled by host configuration"
		} else if menu.Flags.ReadOnly {
			reason = "selected menu is read-only"
		}
		result := manager.snapshotLocked()
		callback := manager.callbacks.Interaction
		event := InteractionEvent{
			Kind: "menu.action.denied", Reason: reason, Key: key, Phase: phase,
			MenuID: menu.ID, ItemID: item.ID, Snapshot: result,
		}
		manager.mu.Unlock()
		if callback != nil {
			callback(event)
		}
		return result, fmt.Errorf("%w: %s", ErrInteractionDenied, reason)
	}
	frame := &manager.stack[len(manager.stack)-1]
	if phase == "press" || phase == "single" || phase == "repeat" {
		switch key {
		case 1:
			frame.cursor = wrap(frame.cursor-1, len(menu.Items))
			manager.guard = ""
			manager.status = "Previous"
			manager.revision++
			result := manager.snapshotLocked()
			manager.mu.Unlock()
			return result, nil
		case 2:
			frame.cursor = wrap(frame.cursor+1, len(menu.Items))
			manager.guard = ""
			manager.status = "Next"
			manager.revision++
			result := manager.snapshotLocked()
			manager.mu.Unlock()
			return result, nil
		case 3, 4:
			if item.Type == "number" || item.Type == "bool" || item.Type == "select" {
				value, err := manager.adjustLocked(item, path, map[bool]int{true: 1, false: -1}[key == 4])
				if err != nil {
					manager.mu.Unlock()
					return manager.Snapshot(), err
				}
				action := item.WriteAction
				manager.guard = ""
				manager.status = "Applying " + item.Title
				manager.revision++
				manager.mu.Unlock()
				return manager.finishWrite(ctx, action, value)
			}
		}
	}
	if key != 4 {
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
	switch item.Type {
	case "submenu":
		manager.stack = append(manager.stack, sessionFrame{menuID: item.Submenu})
		manager.guard = ""
		manager.status = "Opened " + item.Title
		manager.revision++
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	case "readonly":
		// K4 is rejected by the read-only gate above. Explicit Refresh remains
		// available to TUI/API callers without making a key press mutate state.
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, ErrInteractionDenied
	case "text":
		manager.status = "Edit this value from the PC TUI/API"
		manager.revision++
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	case "action":
		if item.Disabled {
			manager.status = "Disabled by host configuration"
			manager.revision++
			result := manager.snapshotLocked()
			manager.mu.Unlock()
			return result, errors.New("host-menu action is disabled")
		}
		if item.Guarded && manager.guard != path {
			manager.guard = path
			manager.status = "Hold K4 to confirm " + item.Title
			manager.revision++
			result := manager.snapshotLocked()
			manager.mu.Unlock()
			return result, nil
		}
		if item.Guarded && phase != "hold" {
			result := manager.snapshotLocked()
			manager.mu.Unlock()
			return result, nil
		}
		action := item.WriteAction
		manager.guard = ""
		manager.status = "Running " + item.Title
		manager.revision++
		manager.mu.Unlock()
		return manager.finishExecute(ctx, action)
	default:
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
}

func (manager *Manager) Refresh(ctx context.Context) (Snapshot, error) {
	manager.mu.Lock()
	_, item, path, ok := manager.selectionLocked()
	if !ok || item.ReadAction == "" {
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
	action := item.ReadAction
	manager.mu.Unlock()
	return manager.finishRead(ctx, path, action)
}

func (manager *Manager) finishRead(ctx context.Context, path, action string) (Snapshot, error) {
	if manager.callbacks.Read == nil {
		return manager.Snapshot(), errors.New("host-menu read callback is unavailable")
	}
	value, err := manager.callbacks.Read(ctx, action)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err != nil {
		manager.status = err.Error()
	} else {
		manager.values[path] = strings.TrimSpace(value)
		manager.status = "Updated"
	}
	manager.revision++
	return manager.snapshotLocked(), err
}

func (manager *Manager) finishWrite(ctx context.Context, action, value string) (Snapshot, error) {
	if strings.TrimSpace(action) == "" {
		manager.mu.Lock()
		manager.status = "Session-only value"
		manager.revision++
		result := manager.snapshotLocked()
		manager.mu.Unlock()
		return result, nil
	}
	if manager.callbacks.Write == nil {
		return manager.Snapshot(), errors.New("host-menu write callback is unavailable")
	}
	output, err := manager.callbacks.Write(ctx, action, value)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err != nil {
		manager.status = err.Error()
	} else if strings.TrimSpace(output) != "" {
		manager.status = strings.TrimSpace(output)
	} else {
		manager.status = "Saved"
	}
	manager.revision++
	return manager.snapshotLocked(), err
}

func (manager *Manager) finishExecute(ctx context.Context, action string) (Snapshot, error) {
	if manager.callbacks.Execute == nil {
		return manager.Snapshot(), errors.New("host-menu action callback is unavailable")
	}
	output, err := manager.callbacks.Execute(ctx, action)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err != nil {
		manager.status = err.Error()
	} else if strings.TrimSpace(output) != "" {
		manager.status = strings.TrimSpace(output)
	} else {
		manager.status = "Complete"
	}
	manager.revision++
	return manager.snapshotLocked(), err
}

func (manager *Manager) adjustLocked(item appconfig.HostMenuItem, path string, delta int) (string, error) {
	value := manager.valueLocked(item, path)
	switch item.Type {
	case "bool":
		parsed, _ := strconv.ParseBool(value)
		value = strconv.FormatBool(!parsed)
	case "number":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			parsed = item.Min
		}
		parsed += float64(delta) * item.Step
		if parsed > item.Max {
			parsed = item.Min
		} else if parsed < item.Min {
			parsed = item.Max
		}
		value = strconv.FormatFloat(parsed, 'f', decimalPlaces(item.Step), 64)
	case "select":
		index := 0
		for candidate, option := range item.Options {
			if option.Value == value {
				index = candidate
				break
			}
		}
		index = wrap(index+delta, len(item.Options))
		value = item.Options[index].Value
	default:
		return "", fmt.Errorf("host-menu item %q is not editable", item.ID)
	}
	manager.values[path] = value
	return value, nil
}

func (manager *Manager) snapshotLocked() Snapshot {
	menu, item, path, ok := manager.selectionLocked()
	if !ok {
		return Snapshot{Status: manager.status, Revision: manager.revision}
	}
	value := manager.valueLocked(item, path)
	line2 := item.Title
	if value != "" {
		line2 += ": " + displayValue(item, value)
	}
	return Snapshot{
		Active: true, MenuID: menu.ID, NodeID: menu.NodeID, MenuTitle: menu.Title,
		ItemID: item.ID, ItemTitle: item.Title, Value: value,
		Cursor: manager.stack[len(manager.stack)-1].cursor,
		Count:  len(menu.Items), Depth: len(manager.stack),
		GuardPending: manager.guard == path, Status: manager.status,
		Revision: manager.revision,
		Panel: Panel{
			Segments: fit(item.Label, 4), LCDLine1: fit(menu.Title, 16), LCDLine2: fit(line2, 16),
			Blink: manager.guard == path || menu.EditVisual == "blink", Brightness: menu.Brightness,
			EditVisual: menu.EditVisual,
		},
	}
}

func (manager *Manager) selectionLocked() (appconfig.HostMenu, appconfig.HostMenuItem, string, bool) {
	if len(manager.stack) == 0 {
		return appconfig.HostMenu{}, appconfig.HostMenuItem{}, "", false
	}
	frame := &manager.stack[len(manager.stack)-1]
	menu, ok := manager.menuLocked(frame.menuID)
	if !ok || len(menu.Items) == 0 {
		return appconfig.HostMenu{}, appconfig.HostMenuItem{}, "", false
	}
	frame.cursor = wrap(frame.cursor, len(menu.Items))
	item := menu.Items[frame.cursor]
	return menu, item, menu.ID + "/" + item.ID, true
}

func (manager *Manager) menuLocked(id string) (appconfig.HostMenu, bool) {
	for _, menu := range manager.config.Menus {
		if menu.ID == id && menuIsVisible(menu.Flags) {
			return menu, true
		}
	}
	return appconfig.HostMenu{}, false
}

func menuIsVisible(flags appconfig.HostMenuFlags) bool {
	return flags.Visible || flags == (appconfig.HostMenuFlags{})
}

func (manager *Manager) valueLocked(item appconfig.HostMenuItem, path string) string {
	if value, ok := manager.values[path]; ok {
		return value
	}
	return item.Value
}

func (manager *Manager) expireLocked(now time.Time) {
	if len(manager.stack) == 0 || manager.lastInput.IsZero() {
		return
	}
	timeout := time.Duration(manager.config.SessionTimeoutMS) * time.Millisecond
	if timeout > 0 && now.Sub(manager.lastInput) >= timeout {
		manager.stack = nil
		manager.guard = ""
		manager.status = "Host menu timed out"
		manager.revision++
	}
}

func wrap(value, length int) int {
	if length <= 0 {
		return 0
	}
	value %= length
	if value < 0 {
		value += length
	}
	return value
}

func decimalPlaces(step float64) int {
	for places := 0; places < 6; places++ {
		if math.Abs(step-math.Round(step)) < 1e-9 {
			return places
		}
		step *= 10
	}
	return 6
}

func displayValue(item appconfig.HostMenuItem, value string) string {
	if item.Type == "bool" {
		if parsed, _ := strconv.ParseBool(value); parsed {
			return "On"
		}
		return "Off"
	}
	if item.Type == "select" {
		for _, option := range item.Options {
			if option.Value == value {
				return option.Label
			}
		}
	}
	return value
}

func fit(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}

func cloneConfig(config appconfig.HostMenuConfig) appconfig.HostMenuConfig {
	result := config
	result.BuiltinOverrides = append([]appconfig.BuiltinMenuOverride(nil), config.BuiltinOverrides...)
	result.Menus = append([]appconfig.HostMenu(nil), config.Menus...)
	for menuIndex := range result.Menus {
		result.Menus[menuIndex].Items = append([]appconfig.HostMenuItem(nil), config.Menus[menuIndex].Items...)
		for itemIndex := range result.Menus[menuIndex].Items {
			result.Menus[menuIndex].Items[itemIndex].Options = append([]appconfig.HostMenuOption(nil), config.Menus[menuIndex].Items[itemIndex].Options...)
		}
	}
	return result
}

func definitionChanges(before, after appconfig.HostMenuConfig, generation byte) []DefinitionChange {
	previous := make(map[byte]appconfig.HostMenu)
	current := make(map[byte]appconfig.HostMenu)
	for _, menu := range before.Menus {
		previous[menu.NodeID] = menu
	}
	for _, menu := range after.Menus {
		current[menu.NodeID] = menu
	}
	changes := make([]DefinitionChange, 0)
	for id, menu := range current {
		old, exists := previous[id]
		fields := changedHostMenuFields(old, menu)
		if !exists {
			fields = []string{"added"}
		}
		if len(fields) != 0 {
			changes = append(changes, DefinitionChange{Kind: definitionChangeKind(fields), MenuID: menu.ID, NodeID: id, Fields: fields, Generation: generation})
		}
	}
	for id, menu := range previous {
		if _, exists := current[id]; !exists {
			changes = append(changes, DefinitionChange{Kind: "menu.definition.changed", MenuID: menu.ID, NodeID: id, Fields: []string{"removed"}, Generation: generation})
		}
	}
	previousBuiltin := make(map[byte]appconfig.BuiltinMenuOverride)
	for _, override := range before.BuiltinOverrides {
		previousBuiltin[override.StableID] = override
	}
	currentBuiltin := make(map[byte]appconfig.BuiltinMenuOverride)
	for _, override := range after.BuiltinOverrides {
		currentBuiltin[override.StableID] = override
	}
	for id, override := range currentBuiltin {
		old, exists := previousBuiltin[id]
		fields := changedBuiltinFields(old, override)
		if !exists {
			fields = []string{"added"}
		}
		if len(fields) != 0 {
			changes = append(changes, DefinitionChange{Kind: definitionChangeKind(fields), MenuID: fmt.Sprintf("builtin:%d", id), NodeID: id, Builtin: true, Fields: fields, Generation: generation})
		}
	}
	for id := range previousBuiltin {
		if _, exists := currentBuiltin[id]; !exists {
			changes = append(changes, DefinitionChange{Kind: "menu.definition.changed", MenuID: fmt.Sprintf("builtin:%d", id), NodeID: id, Builtin: true, Fields: []string{"removed"}, Generation: generation})
		}
	}
	return changes
}

func definitionChangeKind(fields []string) string {
	for _, field := range fields {
		if field == "label" || field == "title" || field == "content" || field == "brightness" || field == "edit_visual" {
			return "menu.content.changed"
		}
	}
	return "menu.definition.changed"
}

func changedHostMenuFields(left, right appconfig.HostMenu) []string {
	fields := make([]string, 0, 8)
	if left.ID != right.ID {
		fields = append(fields, "id")
	}
	if left.ParentID != right.ParentID {
		fields = append(fields, "parent_id")
	}
	if left.OrderID != right.OrderID {
		fields = append(fields, "order_id")
	}
	if left.Label != right.Label {
		fields = append(fields, "label")
	}
	if left.Title != right.Title {
		fields = append(fields, "title")
	}
	if left.Content != right.Content {
		fields = append(fields, "content")
	}
	if left.Brightness != right.Brightness {
		fields = append(fields, "brightness")
	}
	if left.EditVisual != right.EditVisual {
		fields = append(fields, "edit_visual")
	}
	if left.Flags != right.Flags {
		fields = append(fields, "flags")
	}
	return fields
}

func changedBuiltinFields(left, right appconfig.BuiltinMenuOverride) []string {
	fields := make([]string, 0, 7)
	if left.ParentID != right.ParentID {
		fields = append(fields, "parent_id")
	}
	if left.OrderID != right.OrderID {
		fields = append(fields, "order_id")
	}
	if left.Label != right.Label {
		fields = append(fields, "label")
	}
	if left.Title != right.Title {
		fields = append(fields, "title")
	}
	if left.Content != right.Content {
		fields = append(fields, "content")
	}
	if left.Brightness != right.Brightness {
		fields = append(fields, "brightness")
	}
	if left.EditVisual != right.EditVisual {
		fields = append(fields, "edit_visual")
	}
	if left.Flags != right.Flags {
		fields = append(fields, "flags")
	}
	return fields
}

// RegisterCommands exposes the same session through CLI, IPC, and API bridges.
func RegisterCommands(engine *shell.Engine, manager *Manager) error {
	return engine.Register(shell.Command{
		Name: "host-menu", Usage: "host-menu list|directory|content NODE|set REF FIELD VALUE|add NODE ID LABEL TITLE [PARENT]|override STABLE LABEL TITLE [PARENT]|remove REF|open [ID]|status|key K1..K4 PHASE|refresh|close",
		Summary: "control PC-owned front-panel menus",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 0 {
				return "", errors.New("usage: host-menu list|directory|content NODE|set REF FIELD VALUE|add NODE ID LABEL TITLE [PARENT]|override STABLE LABEL TITLE [PARENT]|remove REF|open [ID]|status|key K1..K4 PHASE|refresh|close")
			}
			switch strings.ToLower(args[0]) {
			case "list":
				config := manager.Config()
				lines := make([]string, 0, len(config.Menus)+len(config.BuiltinOverrides)+1)
				lines = append(lines, "TYPE     STABLE/HOST  ORDER  PARENT  7SEG  LCD NAME         KEY")
				for _, override := range config.BuiltinOverrides {
					lines = append(lines, fmt.Sprintf("builtin  0x%02X         %-5d  0x%02X    %-4s  %-16s builtin:%d", override.StableID, override.OrderID, override.ParentID, override.Label, override.Title, override.StableID))
				}
				for _, menu := range config.Menus {
					lines = append(lines, fmt.Sprintf("host     0x%02X         0x%02X   0x%02X    %-4s  %-16s %s", menu.NodeID, menu.OrderID, menu.ParentID, menu.Label, menu.Title, menu.ID))
				}
				return strings.Join(lines, "\n"), nil
			case "directory":
				directory, err := manager.Directory()
				if err != nil {
					return "", err
				}
				lines := []string{fmt.Sprintf("schema=%d generation=%d entries=%d", directory.Schema, directory.Generation, len(directory.Entries))}
				for _, entry := range directory.Entries {
					lines = append(lines, fmt.Sprintf("id=0x%02X parent=0x%02X flags=0x%02X", entry.ID, entry.Parent, entry.Flags))
				}
				return strings.Join(lines, "\n"), nil
			case "content":
				if len(args) != 2 {
					return "", errors.New("usage: host-menu content NODE")
				}
				value, err := strconv.ParseUint(args[1], 0, 8)
				if err != nil {
					return "", err
				}
				content, err := manager.Content(byte(value))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("node=0x%02X revision=%d panel=[%s|%s|%s] flags=0x%02X brightness=%d visual=%d", content.ID, content.Revision, content.Segments, content.LCDLine1, content.LCDLine2, content.Flags, content.Brightness, content.Visual), nil
			case "set":
				if len(args) < 4 {
					return "", errors.New("usage: host-menu set REF FIELD VALUE")
				}
				if err := manager.SetDefinitionField(args[1], args[2], strings.Join(args[3:], " ")); err != nil {
					return "", err
				}
				return "host-menu definition saved; watcher update pending", nil
			case "add":
				if len(args) < 5 || len(args) > 6 {
					return "", errors.New("usage: host-menu add NODE ID LABEL TITLE [PARENT]")
				}
				node, err := strconv.ParseUint(args[1], 0, 8)
				if err != nil {
					return "", err
				}
				parent := uint64(appconfig.HostMenuRoot)
				if len(args) == 6 {
					parent, err = strconv.ParseUint(args[5], 0, 8)
					if err != nil {
						return "", err
					}
				}
				if err := manager.AddDefinition(byte(node), args[2], args[3], args[4], byte(parent)); err != nil {
					return "", err
				}
				return fmt.Sprintf("host-menu node 0x%02X saved", node), nil
			case "override":
				if len(args) < 4 || len(args) > 5 {
					return "", errors.New("usage: host-menu override STABLE LABEL TITLE [PARENT]")
				}
				stable, err := strconv.ParseUint(args[1], 0, 8)
				if err != nil {
					return "", err
				}
				parent := uint64(builtinCategory(byte(stable)))
				if len(args) == 5 {
					parent, err = strconv.ParseUint(args[4], 0, 8)
					if err != nil {
						return "", err
					}
				}
				if err := manager.UpsertBuiltinOverride(byte(stable), args[2], args[3], byte(parent)); err != nil {
					return "", err
				}
				return fmt.Sprintf("built-in stable ID %d online label override saved", stable), nil
			case "remove":
				if len(args) != 2 {
					return "", errors.New("usage: host-menu remove REF")
				}
				if err := manager.RemoveDefinition(args[1]); err != nil {
					return "", err
				}
				return "host-menu definition removed", nil
			case "open":
				if len(args) > 2 {
					return "", errors.New("usage: host-menu open [ID]")
				}
				id := ""
				if len(args) == 2 {
					id = args[1]
				}
				if err := manager.Open(id); err != nil {
					return "", err
				}
				return formatSnapshot(manager.Snapshot()), nil
			case "status":
				return formatSnapshot(manager.Snapshot()), nil
			case "refresh":
				snapshot, err := manager.Refresh(ctx)
				return formatSnapshot(snapshot), err
			case "close":
				manager.Close("Closed by host")
				return "host menu closed", nil
			case "key":
				if len(args) != 3 {
					return "", errors.New("usage: host-menu key K1..K4 press|hold|repeat|release")
				}
				keyText := strings.TrimPrefix(strings.ToLower(args[1]), "k")
				key, err := strconv.Atoi(keyText)
				if err != nil {
					return "", err
				}
				snapshot, err := manager.HandleKey(ctx, key, args[2])
				if err == nil && snapshot.Active {
					if refreshed, refreshErr := manager.Refresh(ctx); refreshErr == nil {
						snapshot = refreshed
					} else {
						err = refreshErr
					}
				}
				return formatSnapshot(snapshot), err
			default:
				return "", fmt.Errorf("unknown host-menu operation %q", args[0])
			}
		},
	})
}

func formatSnapshot(snapshot Snapshot) string {
	if !snapshot.Active {
		return "host menu inactive: " + snapshot.Status
	}
	return fmt.Sprintf("host menu %s item %d/%d %s=%s panel=[%s|%s|%s] status=%s",
		snapshot.MenuID, snapshot.Cursor+1, snapshot.Count, snapshot.ItemID,
		snapshot.Value, snapshot.Panel.Segments, snapshot.Panel.LCDLine1,
		snapshot.Panel.LCDLine2, snapshot.Status)
}
