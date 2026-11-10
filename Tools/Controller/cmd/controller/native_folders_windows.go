//go:build windows && !no_native_gui

package main

import (
	"context"
	"errors"
	"fmt"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
	"strings"
)

func (p nativePages) loadNativeFolders(ctx context.Context) (desktop.Page, error) {
	c, err := p.readNativePeripherals(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Channel folders", Columns: []string{"Identity", "Kind", "Folder", "Icon"}, RowValues: map[string]map[string]string{}}
	for _, g := range c.Folders {
		key := g.Kind + ":" + g.Name
		page.Rows = append(page.Rows, []string{key, g.Kind, g.Name, g.Icon})
		page.RowValues[key] = map[string]string{"kind": g.Kind, "name": g.Name, "icon": g.Icon}
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: c.BoardProfile.Revision, Hidden: true}, choice("kind", "Output kind", "relay", "relay", "pwm", "motion", "board"), field("name", "Folder name", ""), {Key: "icon", Label: "Folder icon", IconPicker: true}}
	ready := !p.preview && c.BoardProfile.Attached && c.BoardProfile.Configured
	page.Actions = []desktop.PageAction{pageAction("new", "New folder", ready), {ID: "create", Label: "Create folder", Enabled: ready}, {ID: "save", Label: "Save folder", Enabled: ready, SelectionRequired: true}, {ID: "remove", Label: "Delete folder", Enabled: ready, SelectionRequired: true, Confirm: true}}
	return page, nil
}
func (p nativePages) submitNativeFolders(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify folders")
	}
	if action == "new" {
		page, err := p.loadNativeFolders(ctx)
		page.ClearSelection = true
		return page, err
	}
	c, err := p.readNativePeripherals(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	if v["revision"] == "" || v["revision"] != c.BoardProfile.Revision {
		return desktop.Page{}, errors.New("Board profile changed; refresh before saving folders")
	}
	change := appconfig.ChannelFolderMutation{Operation: "create", Kind: v["kind"], Name: strings.TrimSpace(v["name"])}
	icon := v["icon"]
	change.Icon = &icon
	if action == "save" || action == "remove" {
		found := false
		for _, g := range c.Folders {
			if g.Kind+":"+g.Name == v["selected"] {
				change.Kind = g.Kind
				change.Name = g.Name
				found = true
				break
			}
		}
		if !found {
			return desktop.Page{}, errors.New("select a current folder")
		}
		if action == "save" {
			if v["kind"] != change.Kind {
				return desktop.Page{}, errors.New("folder output kind is fixed; create a separate folder")
			}
			change.Operation = "update"
			name := strings.TrimSpace(v["name"])
			change.NextName = &name
		} else {
			change.Operation = "delete"
		}
	} else if action != "create" {
		return desktop.Page{}, errors.New("unknown folder operation")
	}
	var result any
	params := map[string]any{"operation": change.Operation, "kind": change.Kind, "name": change.Name, "icon": change.Icon, "expected_revision": c.BoardProfile.Revision}
	if change.NextName != nil {
		params["next_name"] = *change.NextName
	}
	if err := p.call(ctx, "controller.peripheral.folder.update", params, &result); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativeFolders(ctx)
	if err != nil {
		return page, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	if action == "remove" {
		page.ClearSelection = true
	} else {
		name := change.Name
		if change.NextName != nil {
			name = *change.NextName
		}
		page.SelectedRow = change.Kind + ":" + name
	}
	return page, nil
}
