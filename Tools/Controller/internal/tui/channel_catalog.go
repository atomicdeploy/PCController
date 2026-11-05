package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/ipcjson"
)

// ChannelCatalogSource uses the same host-owned catalog and revision-fenced
// mutation as Pealayer and the browser. Terminal preferences never own names.
type ChannelCatalogSource struct {
	Read   func(context.Context) (ipcjson.PeripheralSettings, error)
	Rename func(context.Context, string, string, string) (ipcjson.PresentationUpdateResult, error)
}

type channelCatalogState struct {
	source         ChannelCatalogSource
	value          ipcjson.PeripheralSettings
	owner          string
	version        uint64
	pending        bool
	renameRevision string
}

type channelCatalogRefreshMsg struct{}
type channelCatalogResultMsg struct {
	value   ipcjson.PeripheralSettings
	owner   string
	version uint64
	err     error
}

func (model Model) channelCatalogOwner() string {
	snapshot := model.snapshot()
	return fmt.Sprintf("%s/%s/%d", snapshot.Port.Name, snapshot.Port.SerialNumber, snapshot.ConnectionGeneration)
}

func (model *Model) refreshChannelCatalog() tea.Cmd {
	if model.channels.source.Read == nil {
		return nil
	}
	model.channels.version++
	if model.channels.pending {
		return nil
	}
	return model.readChannelCatalog()
}

func (model *Model) readChannelCatalog() tea.Cmd {
	model.channels.pending = true
	read, version, owner := model.channels.source.Read, model.channels.version, model.channelCatalogOwner()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		value, err := read(ctx)
		if err == nil && (value.BoardProfile.Revision == "" || value.Peripherals == nil || value.Controls == nil) {
			err = fmt.Errorf("controller returned an incomplete channel catalog")
		}
		return channelCatalogResultMsg{value: value, owner: owner, version: version, err: err}
	}
}

func (model *Model) acceptChannelCatalog(message channelCatalogResultMsg) tea.Cmd {
	model.channels.pending = false
	// Events arriving during a read invalidate it, even if its HTTP response
	// arrives later. Coalesce a burst into one trailing authoritative read.
	if message.version != model.channels.version || message.owner != model.channelCatalogOwner() {
		return model.readChannelCatalog()
	}
	if message.err != nil {
		model.appendLog("warning", "channel names/icons refresh: "+message.err.Error())
		return nil // Retain the last good catalog; next event/reconnect retries.
	}
	model.channels.value, model.channels.owner = message.value, message.owner
	return nil
}

func (model Model) channelPresentation(key string) (name, icon string) {
	if model.channels.owner != model.channelCatalogOwner() {
		return "", ""
	}
	for _, control := range model.channels.value.Controls {
		if control.Key == key {
			return control.Name, control.Icon
		}
	}
	for _, peripheral := range model.channels.value.Peripherals {
		if peripheral.Key == key {
			return peripheral.Name, peripheral.Icon
		}
	}
	return "", ""
}

func (model Model) motionControlKey(side string) string {
	if model.channels.owner == model.channelCatalogOwner() {
		for _, control := range model.channels.value.Controls {
			if control.Key == "seat."+side || control.Key == "motion."+side {
				return control.Key
			}
		}
	}
	return "motion." + side
}

func (model Model) channelDescriptorByKey(key string) (appconfig.PeripheralDescriptor, bool) {
	if model.channels.source.Read != nil {
		if model.channels.owner == model.channelCatalogOwner() {
			for _, descriptor := range model.channels.value.Peripherals {
				if descriptor.Key == key {
					return descriptor, true
				}
			}
		}
		return appconfig.PeripheralDescriptor{}, false
	}
	return peripheralDescriptorByKey(key)
}

func (model Model) catalogSettingDescriptor(key string) (appconfig.PeripheralDescriptor, bool) {
	if !strings.HasPrefix(key, peripheralNameSettingPrefix) {
		return appconfig.PeripheralDescriptor{}, false
	}
	return model.channelDescriptorByKey(strings.TrimPrefix(key, peripheralNameSettingPrefix))
}

// Terminals cannot load Pealayer's private-use vector font. Render portable
// Unicode equivalents for the shared semantic IDs, and retain unknown IDs as
// text rather than silently replacing a user's choice with a generic icon.
var terminalChannelIcons = map[string]string{
	"plug": "🔌", "lightning": "ϟ", "lightbulb": "💡", "lamp": "💡", "chandelier": "💡", "loostr": "💡",
	"stage-light": "☀", "garden": "♣", "sparkle": "✧", "folder": "▣", "fan": "✣", "power": "⏻",
	"speaker": "♪", "radio": "📻", "seat": "🪑", "car": "🚗", "door": "🚪", "bell": "🔔",
	"fire": "♨", "snowflake": "❄", "thermometer": "🌡", "waveform": "∿", "circuitry": "⌘",
	"gear": "⚙", "settings": "⚙", "sliders-horizontal": "☷", "led": "●", "window": "▣",
}

func terminalChannelIcon(icon string) string {
	icon = strings.ToLower(strings.TrimSpace(icon))
	if glyph, ok := terminalChannelIcons[icon]; ok {
		return glyph
	}
	if icon == "" {
		return ""
	}
	return "[" + icon + "]"
}

func (model Model) saveCatalogName(descriptor appconfig.PeripheralDescriptor, name string) (Model, string, bool, error) {
	catalog := model.channels.value
	if model.channels.source.Rename == nil || model.channels.owner != model.channelCatalogOwner() ||
		!catalog.BoardProfile.Configured || !catalog.BoardProfile.Attached || catalog.BoardProfile.Revision == "" {
		return model, "", false, fmt.Errorf("current board channel catalog is not ready for editing")
	}
	revision := catalog.BoardProfile.Revision
	if model.channels.renameRevision != "" {
		revision = model.channels.renameRevision
	}
	restored := name == "" || name == descriptor.DefaultName
	if restored {
		name = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := model.channels.source.Rename(ctx, descriptor.Key, name, revision)
	if err != nil {
		return model, "", false, err
	}
	if updated.BoardProfile.BoardIdentity != catalog.BoardProfile.BoardIdentity || updated.Peripheral.Key != descriptor.Key || updated.BoardProfile.Revision == "" {
		return model, "", false, fmt.Errorf("rename acknowledgement does not match the selected board channel")
	}
	model.channels.version++
	model.channels.value.BoardProfile = updated.BoardProfile
	model.channels.value.Controls = updated.Controls
	for index := range model.channels.value.Peripherals {
		if model.channels.value.Peripherals[index].Key == descriptor.Key {
			// Copy the slice: Bubble Tea model copies must not mutate prior state.
			model.channels.value.Peripherals = append([]appconfig.PeripheralDescriptor(nil), model.channels.value.Peripherals...)
			model.channels.value.Peripherals[index] = updated.Peripheral
			break
		}
	}
	name = model.peripheralName(descriptor.Key, descriptor.DefaultName)
	return model, name, restored, nil
}
