package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/native"
)

func (model Model) hostMenuKeyCommand(key int, phase string) tea.Cmd {
	manager := model.hostMenus
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		snapshot, err := manager.HandleKey(ctx, key, phase)
		if err == nil && snapshot.Active {
			if refreshed, refreshErr := manager.Refresh(ctx); refreshErr == nil {
				snapshot = refreshed
			} else {
				err = refreshErr
			}
		}
		return hostMenuResultMsg{snapshot: snapshot, err: err}
	}
}

func (model Model) openHostMenuCommand(menuID string) tea.Cmd {
	manager := model.hostMenus
	return func() tea.Msg {
		if err := manager.Open(menuID); err != nil {
			return hostMenuResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		snapshot, err := manager.Refresh(ctx)
		return hostMenuResultMsg{snapshot: snapshot, err: err}
	}
}

func (model Model) closeHostMenuCommand(reason string) tea.Cmd {
	manager := model.hostMenus
	return func() tea.Msg {
		manager.Close(reason)
		return hostMenuResultMsg{snapshot: manager.Snapshot()}
	}
}

// hostMenuDeviceEventCommand routes only physical gestures while firmware has
// yielded the front panel. Down is the immediate press; the later Click is
// ignored so a normal press cannot be applied twice.
func (model Model) hostMenuDeviceEventCommand(event control.Event) tea.Cmd {
	if model.hostMenus == nil || event.Frame.Opcode != native.OpEvent {
		return nil
	}
	device, err := native.ParseDeviceEvent(event.Frame.Payload)
	if err != nil || device.Type != native.EventKey || device.Source != native.InputSourcePhysical {
		return nil
	}
	gesture := control.NormalizeGesture(device.Gesture)
	session := model.hostMenus.Snapshot()
	if !session.Active {
		config := model.hostMenus.Config()
		doorPage, haveDoorPage := model.doorMenuID()
		if config.RequestGesture == "door-hold-k4" && device.Key == 3 &&
			gesture == "hold" && haveDoorPage && model.snapshot().Status.MenuPage == doorPage {
			return model.openHostMenuCommand(config.DefaultMenu)
		}
		return nil
	}
	phase := ""
	switch gesture {
	case "down":
		phase = "press"
	case "hold", "repeat", "release":
		phase = gesture
	default:
		return nil
	}
	return model.hostMenuKeyCommand(int(device.Key)+1, phase)
}

func (model *Model) syncHostPanelCommand() tea.Cmd {
	if model.hostMenus == nil || model.hostPanelPending {
		return nil
	}
	snapshot := model.hostMenus.Snapshot()
	if !snapshot.Active {
		if !model.hostPanelCaptured || model.releaseHostPanel == nil {
			return nil
		}
		model.hostPanelPending = true
		release := model.releaseHostPanel
		return func() tea.Msg {
			return hostPanelResultMsg{released: true, err: release()}
		}
	}
	if model.pushHostPanel == nil {
		return nil
	}
	if snapshot.Revision == model.hostPanelRevision && time.Since(model.hostPanelLastPush) < time.Second {
		return nil
	}
	model.hostPanelPending = true
	push := model.pushHostPanel
	return func() tea.Msg {
		return hostPanelResultMsg{revision: snapshot.Revision, err: push(snapshot)}
	}
}

func hostMenuPanelState(snapshot hostmenu.Snapshot) FrontPanelState {
	state := byte(0)
	if snapshot.GuardPending {
		state = 2
	}
	value, _ := strconv.ParseUint(snapshot.Value, 10, 12)
	return FrontPanelState{
		Segments: snapshot.Panel.Segments, Blink: snapshot.Panel.Blink,
		Brightness: snapshot.Panel.Brightness, LCDLine1: snapshot.Panel.LCDLine1,
		LCDLine2: snapshot.Panel.LCDLine2, LCDBacklight: true,
		MenuName:    "Host · " + snapshot.MenuTitle,
		Submode:     fmt.Sprintf("%s · %d/%d · state=%d value=%d", snapshot.ItemID, snapshot.Cursor+1, snapshot.Count, state, value),
		InputSource: "physical / virtual host keys", Exact: true,
	}
}

func renderHostMenuDirectory(manager *hostmenu.Manager, width int) string {
	if manager == nil {
		return labelStyle.Render("Host-owned menus unavailable in this frontend.")
	}
	config := manager.Config()
	active := manager.Snapshot()
	lines := []string{sectionHeader(width, "HOST MENUS", "watched config · E edits selected/active definition · H opens/closes")}
	if active.Active {
		lines = append(lines,
			valueStyle.Render(fmt.Sprintf("ACTIVE · %s / %s · %d of %d", active.MenuTitle, active.ItemTitle, active.Cursor+1, active.Count)),
			labelStyle.Render("K1/K2 navigate · K3/K4 adjust · hold K3 back · guarded actions require hold K4"),
		)
	} else {
		lines = append(lines, labelStyle.Render("inactive · default "+config.DefaultMenu))
	}
	for _, override := range config.BuiltinOverrides {
		lines = append(lines, fmt.Sprintf("  B stable=%02d order-rank=%02d parent=0x%02X  %-4s %s · flash fallback retained",
			override.StableID, override.OrderID, override.ParentID, override.Label, strings.TrimSpace(override.Title)))
	}
	menus := append([]appconfig.HostMenu(nil), config.Menus...)
	sort.SliceStable(menus, func(i, j int) bool { return menus[i].NodeID < menus[j].NodeID })
	byID := make(map[byte]appconfig.HostMenu, len(menus))
	for _, menu := range menus {
		byID[menu.NodeID] = menu
	}
	for _, menu := range menus {
		marker := "  "
		if active.Active && menu.ID == active.MenuID {
			marker = "● "
		}
		depth := 0
		for parent := menu.ParentID; byID[parent].NodeID != 0 && depth < 8; parent = byID[parent].ParentID {
			depth++
		}
		lines = append(lines, fmt.Sprintf("%s%sH host-id=0x%02X parent=0x%02X %-4s %-16s key=%s",
			marker, strings.Repeat("  ", depth), menu.NodeID, menu.ParentID, menu.Label, strings.TrimSpace(menu.Title), menu.ID))
	}
	lines = append(lines, labelStyle.Render("Built-in stable/wire IDs never change; ranks reorder only presentation. Host siblings sort by editable host ID."))
	return strings.Join(lines, "\n")
}
