//go:build windows && !no_native_gui

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
)

func (p nativePages) loadNativeEffectLibrary(ctx context.Context) (desktop.Page, error) {
	library, err := p.readNativeEffectLibrary(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Effect library files & folders", Columns: []string{"Reference", "Name", "Kind", "Folder"}}
	groups := map[string]bool{}
	for name := range library.Library.Groups {
		groups[name] = true
	}
	for _, effect := range library.Library.Effects {
		page.Rows = append(page.Rows, []string{effect.Reference, effect.Name, effect.Kind, effect.Category})
		if effect.Category != "" {
			groups[effect.Category] = true
		}
	}
	names := []string{}
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	selected := ""
	if len(names) > 0 {
		selected = names[0]
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: library.ETag, Hidden: true}, {Key: "import_path", Label: "Effect library file", FileFilter: "Effect library (*.json)|*.json|All files (*.*)|*.*", Section: "Import"}, choice("mode", "Import mode", "merge", "merge", "replace"), {Key: "export_path", Label: "Export library to new file", FileFilter: "Effect library (*.json)|*.json", SaveFile: true, Section: "Export"}, choice("group", "Current folder", selected, names...), field("new_name", "New folder name", ""), field("group_icon", "New folder icon (optional)", ""), nativeBool("clear_icon", "Clear folder icon", false)}
	for i := range page.Fields {
		if page.Fields[i].Key == "group_icon" {
			page.Fields[i].IconPicker = true
		}
		if page.Fields[i].Key == "mode" {
			page.Fields[i].ChoiceLabels = []string{"Merge", "Replace"}
		}
		switch page.Fields[i].Key {
		case "mode":
			page.Fields[i].Section = "Import"
		case "group", "new_name", "group_icon", "clear_icon":
			page.Fields[i].Section = "Folders"
		}
	}
	page.Actions = []desktop.PageAction{{ID: "import", Label: "Import library", Section: "Import", Enabled: !p.preview, Confirm: true}, {ID: "export", Label: "Export library", Section: "Export", Enabled: !p.preview}, {ID: "group-update", Label: "Update folder", Section: "Folders", Enabled: !p.preview && len(names) > 0, Confirm: true}}
	return page, nil
}

func nativeReadEffectLibrary(path string) (control.EffectLibrary, error) {
	file, err := os.Open(path)
	if err != nil {
		return control.EffectLibrary{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return control.EffectLibrary{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return control.EffectLibrary{}, errors.New("effect library must be a regular JSON file no larger than 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return control.EffectLibrary{}, err
	}
	if len(data) > 2<<20 {
		return control.EffectLibrary{}, errors.New("effect library exceeds 2 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var library control.EffectLibrary
	if err := decoder.Decode(&library); err != nil {
		return library, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return library, errors.New("effect library contains trailing data")
	}
	return library, nil
}

func nativeWriteEffectLibrary(path string, library control.EffectLibrary) error {
	if !filepath.IsAbs(path) {
		return errors.New("choose an absolute export filename")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pccontroller-effects-*")
	if err != nil {
		return err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(library); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(file.Name())
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func nativeRenameEffectGroup(library control.EffectLibrary, current, next, icon string, clear bool) (control.EffectLibrary, error) {
	if current == "" || strings.TrimSpace(next) == "" {
		return library, errors.New("choose an existing folder and enter its new name")
	}
	next = strings.TrimSpace(next)
	found := false
	for name := range library.Groups {
		if name == current {
			found = true
		}
		if name != current && strings.EqualFold(name, next) {
			return library, errors.New("folder name already exists")
		}
	}
	effects := append([]control.EffectDescriptor(nil), library.Effects...)
	for i, effect := range effects {
		if effect.Category == current {
			found = true
			effects[i].Category = next
			effects[i].GroupIcon = ""
		} else if strings.EqualFold(effect.Category, next) && !strings.EqualFold(current, next) {
			return library, errors.New("folder name already exists")
		}
	}
	if !found {
		return library, errors.New("folder changed; refresh before saving")
	}
	groups := map[string]appconfig.EffectGroup{}
	for name, group := range library.Groups {
		groups[name] = group
	}
	group := groups[current]
	delete(groups, current)
	if clear {
		group.Icon = ""
	} else if strings.TrimSpace(icon) != "" {
		group.Icon = icon
	}
	groups[next] = group
	return control.EffectLibrary{Effects: effects, Groups: groups}, nil
}

func (p nativePages) submitNativeEffectLibrary(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot import effect libraries")
	}
	current, err := p.readNativeEffectLibrary(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	if current.ETag != v["revision"] {
		return desktop.Page{}, errors.New("effect library changed; refresh before continuing")
	}
	library := current.Library
	mode := "replace"
	switch action {
	case "export":
		if err := nativeWriteEffectLibrary(v["export_path"], library); err != nil {
			return desktop.Page{}, err
		}
		page, err := p.loadNativeEffectLibrary(ctx)
		page.Output = fmt.Sprintf("Saved %d effects to %s", len(library.Effects), v["export_path"])
		return preserveNativeFields(page, v, func(f desktop.Field) bool { return f.Section != "Export" }), err
	case "import":
		library, err = nativeReadEffectLibrary(v["import_path"])
		mode = v["mode"]
		if err != nil {
			return desktop.Page{}, err
		}
		if len(library.Effects) == 0 {
			return desktop.Page{}, errors.New("effect library file contains no effects")
		}
	case "group-update":
		library, err = nativeRenameEffectGroup(library, v["group"], v["new_name"], v["group_icon"], v["clear_icon"] == "true")
		if err != nil {
			return desktop.Page{}, err
		}
	default:
		return desktop.Page{}, errors.New("unknown library operation")
	}
	var saved nativeEffectLibrary
	if err := p.call(ctx, "controller.effects.library.set", map[string]any{"library": library, "if_match": current.ETag, "mode": mode}, &saved); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativeEffectLibrary(ctx)
	if err != nil {
		return page, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	section := "Import"
	if action == "group-update" {
		section = "Folders"
	}
	return preserveNativeFields(page, v, func(f desktop.Field) bool { return f.Section != section }), nil
}
