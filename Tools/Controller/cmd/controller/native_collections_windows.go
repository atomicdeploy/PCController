//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
)

type nativePeerInventory struct {
	Peers []appconfig.WebSocketClient `json:"peers"`
}

func nativeCollectionRevision(value any) string {
	encoded, _ := json.Marshal(value)
	var canonical any
	_ = json.Unmarshal(encoded, &canonical)
	return nativeDigest(canonical)
}

func (p nativePages) readNativePeers(ctx context.Context) (nativePeerInventory, string, error) {
	inventory := nativePeerInventory{Peers: []appconfig.WebSocketClient{}}
	if !p.preview {
		if err := p.call(ctx, "controller.network.peers.get", map[string]any{}, &inventory); err != nil {
			return inventory, "", err
		}
	}
	values, err := nativeJSONMap(inventory)
	return inventory, nativeDigest(values), err
}

func (p nativePages) loadNativePeers(ctx context.Context) (desktop.Page, error) {
	inventory, revision, err := p.readNativePeers(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Persistent bridge peers", Columns: []string{"Name", "Enabled", "Endpoint", "Protocol", "Forward events", "Accept commands"}, RowValues: map[string]map[string]string{}}
	for _, peer := range inventory.Peers {
		page.Rows = append(page.Rows, []string{peer.Name, fmt.Sprint(peer.Enabled), peer.URL, peer.Protocol, fmt.Sprint(peer.ForwardEvents), fmt.Sprint(peer.AllowCommands)})
		page.RowValues[peer.Name] = map[string]string{"name": peer.Name, "enabled": fmt.Sprint(peer.Enabled), "url": peer.URL, "protocol": peer.Protocol, "topics": strings.Join(peer.Topics, ", "), "forward_events": fmt.Sprint(peer.ForwardEvents), "allow_commands": fmt.Sprint(peer.AllowCommands), "auth_reference": peer.AuthTokenRef}
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: revision, Hidden: true}, {Key: "auth_reference", Hidden: true}, field("name", "Peer name", ""), nativeBool("enabled", "Peer enabled", false), field("url", "Peer endpoint", ""), choice("protocol", "Protocol", "jsonrpc", "jsonrpc", "socketio"), field("topics", "Event topics (comma separated)", ""), nativeBool("forward_events", "Forward events", false), nativeBool("allow_commands", "Allow peer commands", false)}
	page.Actions = []desktop.PageAction{pageAction("new", "New peer", !p.preview), pageAction("save", "Save peer", !p.preview), {ID: "remove", Label: "Remove peer", Enabled: !p.preview, SelectionRequired: true, Confirm: true}}
	return page, nil
}

func (p nativePages) submitNativePeers(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify peers")
	}
	if action == "new" {
		page, err := p.loadNativePeers(ctx)
		page.ClearSelection = true
		return page, err
	}
	inventory, revision, err := p.readNativePeers(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	if revision != v["revision"] {
		return desktop.Page{}, errors.New("Peers changed; refresh before saving")
	}
	selected := v["selected"]
	index := -1
	for i, peer := range inventory.Peers {
		if peer.Name == selected {
			index = i
			break
		}
	}
	switch action {
	case "remove":
		if index < 0 {
			return desktop.Page{}, errors.New("select a current peer")
		}
		inventory.Peers = append(inventory.Peers[:index], inventory.Peers[index+1:]...)
	case "save":
		if selected != "" && (index < 0 || v["name"] != selected) {
			return desktop.Page{}, errors.New("Peer names are stable; create a new peer instead of renaming its key")
		}
		peer := appconfig.WebSocketClient{Name: v["name"], Enabled: v["enabled"] == "true", URL: v["url"], Protocol: v["protocol"], AuthTokenRef: v["auth_reference"], ForwardEvents: v["forward_events"] == "true", AllowCommands: v["allow_commands"] == "true"}
		for _, topic := range strings.Split(v["topics"], ",") {
			if topic = strings.TrimSpace(topic); topic != "" {
				peer.Topics = append(peer.Topics, topic)
			}
		}
		if index < 0 {
			for _, existing := range inventory.Peers {
				if strings.EqualFold(existing.Name, peer.Name) {
					return desktop.Page{}, errors.New("peer name already exists; select it to edit")
				}
			}
			inventory.Peers = append(inventory.Peers, peer)
		} else {
			inventory.Peers[index] = peer
		}
	default:
		return desktop.Page{}, errors.New("unknown peer operation")
	}
	var result any
	if err := p.call(ctx, "controller.network.peers.set", map[string]any{"peers": inventory.Peers, "if_match": revision}, &result); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativePeers(ctx)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	return page, nil
}

func (p nativePages) readNativeHotkeys(ctx context.Context) ([]appconfig.Hotkey, error) {
	var result struct {
		Bindings []appconfig.Hotkey `json:"bindings"`
	}
	if !p.preview {
		if err := p.call(ctx, "controller.hotkeys.get", map[string]any{}, &result); err != nil {
			return nil, err
		}
	}
	return result.Bindings, nil
}

func (p nativePages) loadNativeHotkeys(ctx context.Context) (desktop.Page, error) {
	bindings, err := p.readNativeHotkeys(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Host hotkey bindings", Columns: []string{"Name", "Enabled", "Chord", "Command"}, RowValues: map[string]map[string]string{}}
	for _, binding := range bindings {
		page.Rows = append(page.Rows, []string{binding.Name, fmt.Sprint(binding.Enabled), binding.Chord, binding.Command})
		page.RowValues[binding.Name] = map[string]string{"name": binding.Name, "enabled": fmt.Sprint(binding.Enabled), "chord": binding.Chord, "command": binding.Command}
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: nativeCollectionRevision(bindings), Hidden: true}, field("name", "Binding name", ""), nativeBool("enabled", "Binding enabled", false), field("chord", "Keyboard chord", ""), field("command", "Controller command", "")}
	page.Actions = []desktop.PageAction{pageAction("new", "New hotkey", !p.preview), pageAction("save", "Save hotkey", !p.preview), {ID: "remove", Label: "Remove hotkey", Enabled: !p.preview, SelectionRequired: true, Confirm: true}}
	return page, nil
}

func (p nativePages) submitNativeHotkeys(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify hotkeys")
	}
	if action == "new" {
		page, err := p.loadNativeHotkeys(ctx)
		page.ClearSelection = true
		return page, err
	}
	bindings, err := p.readNativeHotkeys(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	if nativeCollectionRevision(bindings) != v["revision"] {
		return desktop.Page{}, errors.New("Hotkeys changed; refresh before saving")
	}
	selected := v["selected"]
	found := false
	for _, binding := range bindings {
		if binding.Name == selected {
			found = true
		} else if strings.EqualFold(binding.Name, v["name"]) {
			return desktop.Page{}, errors.New("binding name already exists; select it to edit")
		}
	}
	if selected != "" && (!found || v["name"] != selected) {
		return desktop.Page{}, errors.New("Hotkey names are stable; create a new binding instead of renaming its key")
	}
	params := map[string]any{"name": v["name"], "if_match": v["revision"]}
	switch action {
	case "save":
		params["operation"] = "upsert"
		params["enabled"] = v["enabled"] == "true"
		params["chord"] = v["chord"]
		params["command"] = v["command"]
	case "remove":
		if !found {
			return desktop.Page{}, errors.New("select a binding first")
		}
		params["operation"] = "remove"
	default:
		return desktop.Page{}, errors.New("unknown hotkey operation")
	}
	var result json.RawMessage
	if err := p.call(ctx, "controller.hotkeys.set", params, &result); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativeHotkeys(ctx)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	return page, nil
}
