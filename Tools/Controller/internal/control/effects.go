package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

// EffectDescriptor is the single discovery model shared by GUI, Web, TUI,
// CLI, IPC, and bridge clients. A sequence uses the exact macro runner; a
// strip-stream uses the host renderer. Both are PCController-owned entries and
// are addressed through the same stable reference.
type EffectDescriptor struct {
	Reference     string                 `json:"reference"`
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Category      string                 `json:"category,omitempty"`
	Icon          string                 `json:"icon,omitempty"`
	GroupIcon     string                 `json:"group_icon,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Kind          string                 `json:"kind"`
	Engine        string                 `json:"engine"`
	Editable      bool                   `json:"editable"`
	DurationMS    int                    `json:"duration_ms"`
	DefaultFPS    int                    `json:"default_fps,omitempty"`
	DefaultPixels int                    `json:"default_pixels,omitempty"`
	Program       appconfig.StripProgram `json:"program,omitempty"`
	Steps         []appconfig.MacroStep  `json:"steps,omitempty"`
	Properties    map[string]interface{} `json:"properties,omitempty"`
}

// effectDocument is the portable PCController-owned effect library format.
// It deliberately has no protocol/schema version: alpha installations use the
// living contract and import validates every definition before changing the
// active library.
type effectDocument struct {
	Effects []EffectDescriptor               `json:"effects"`
	Groups  map[string]appconfig.EffectGroup `json:"groups,omitempty"`
}

type EffectGroupDescriptor struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

// Include empty persisted groups and groups already used by effects.
func EffectGroupCatalog(config appconfig.Config) []EffectGroupDescriptor {
	groups := make(map[string]EffectGroupDescriptor)
	add := func(name, icon string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, exists := groups[key]; !exists || icon != "" {
			groups[key] = EffectGroupDescriptor{Name: name, Icon: icon}
		}
	}
	for _, effect := range EffectCatalog(config.Macros, config.StripEffects) {
		add(effect.Category, "")
	}
	for name, group := range config.EffectGroups {
		add(name, group.Icon)
	}
	result := make([]EffectGroupDescriptor, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result
}

func EffectCatalog(macros []appconfig.Macro, strips []appconfig.StripEffect) []EffectDescriptor {
	return EffectCatalogWithGroups(macros, strips, nil)
}

func EffectCatalogWithGroups(macros []appconfig.Macro, strips []appconfig.StripEffect, groups map[string]appconfig.EffectGroup) []EffectDescriptor {
	result := make([]EffectDescriptor, 0, len(macros)+len(strips))
	groupIcon := func(category string) string {
		for name, group := range groups {
			if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(category)) {
				return group.Icon
			}
		}
		return ""
	}
	for _, macro := range macros {
		duration := 0
		for _, step := range macro.Steps {
			repeats := int(step.RepeatCount)
			if repeats < 1 {
				repeats = 1
			}
			interval := int(step.RepeatIntervalMS)
			if repeats > 1 && interval == 0 {
				interval = max(int(step.DurationMS), 1)
			}
			end := int(step.AtUS/1000) + (repeats-1)*interval + int(step.DurationMS)
			if end > duration {
				duration = end
			}
		}
		result = append(result, EffectDescriptor{
			Reference: "effect:" + strconv.Itoa(int(macro.ID)), ID: strconv.Itoa(int(macro.ID)),
			Name: macro.Name, Category: macro.Category, Icon: macro.Icon, GroupIcon: groupIcon(macro.Category), Kind: "sequence", Engine: macro.Mode,
			Editable: true, DurationMS: duration, Steps: append([]appconfig.MacroStep(nil), macro.Steps...),
			Properties: map[string]interface{}{
				"color": macro.Color, "label": macro.Label, "lcd_message": macro.LCDMessage,
				"timing_tolerance_us":    macro.TimingToleranceUS,
				"keep_outputs_on_cancel": macro.KeepOutputsOnCancel,
				"board_profile_key":      macro.BoardProfileKey, "board_profile_mode": macro.BoardProfileMode,
			},
		})
	}
	for _, effect := range strips {
		result = append(result, EffectDescriptor{
			Reference: "effect:" + effect.ID, ID: effect.ID, Name: effect.Name,
			Category: effect.Category, Icon: effect.Icon, GroupIcon: groupIcon(effect.Category), Description: effect.Description,
			Kind: "strip-stream", Engine: "host", Editable: true,
			DurationMS: effect.DefaultDurationMS, DefaultFPS: effect.DefaultFPS,
			DefaultPixels: effect.DefaultPixels, Program: effect.Program,
		})
	}
	return result
}

func splitEffectReference(reference string) (string, string) {
	kind, id, ok := strings.Cut(strings.TrimSpace(reference), ":")
	if !ok {
		return "", strings.TrimSpace(reference)
	}
	return strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(id)
}

func findEffect(catalog []EffectDescriptor, reference string) (EffectDescriptor, error) {
	kind, id := splitEffectReference(reference)
	if kind != "" && kind != "effect" {
		return EffectDescriptor{}, fmt.Errorf("effect reference %q must use effect:ID or ID", reference)
	}
	for _, effect := range catalog {
		if strings.EqualFold(effect.ID, id) || strings.EqualFold(effect.Name, id) || strings.EqualFold(effect.Reference, "effect:"+id) {
			return effect, nil
		}
	}
	return EffectDescriptor{}, fmt.Errorf("effect %q is not configured", reference)
}

func ensureEffectIdentityAvailable(catalog []EffectDescriptor, exceptReference, id, name string) error {
	for _, current := range catalog {
		if exceptReference != "" && strings.EqualFold(current.Reference, exceptReference) {
			continue
		}
		if strings.EqualFold(current.ID, strings.TrimSpace(id)) {
			return fmt.Errorf("effect id %q already exists", id)
		}
		if strings.EqualFold(current.Name, strings.TrimSpace(name)) {
			return fmt.Errorf("effect name %q already exists", name)
		}
	}
	return nil
}

func effectPropertyString(effect EffectDescriptor, key, fallback string) string {
	if value, ok := effect.Properties[key].(string); ok {
		return value
	}
	return fallback
}

func effectPropertyUint32(effect EffectDescriptor, key string) uint32 {
	switch value := effect.Properties[key].(type) {
	case float64:
		if value >= 0 && value <= float64(^uint32(0)) {
			return uint32(value)
		}
	case json.Number:
		parsed, _ := strconv.ParseUint(string(value), 10, 32)
		return uint32(parsed)
	}
	return 0
}

func effectPropertyBool(effect EffectDescriptor, key string) bool {
	value, _ := effect.Properties[key].(bool)
	return value
}

func setEffectIcon(config *appconfig.Config, effect EffectDescriptor, icon string) error {
	icon = strings.TrimSpace(icon)
	if icon == "-" {
		icon = ""
	}
	if effect.Kind == "sequence" {
		for index := range config.Macros {
			if strconv.Itoa(int(config.Macros[index].ID)) == effect.ID {
				config.Macros[index].Icon = icon
				return nil
			}
		}
	} else {
		for index := range config.StripEffects {
			if strings.EqualFold(config.StripEffects[index].ID, effect.ID) {
				config.StripEffects[index].Icon = icon
				return nil
			}
		}
	}
	return fmt.Errorf("effect %q is not configured", effect.Reference)
}

func stripProgramTemplate(primitive string) (appconfig.StripProgram, error) {
	switch strings.ToLower(strings.TrimSpace(primitive)) {
	case "alternating-zones":
		return appconfig.StripProgram{Primitive: "alternating-zones", Primary: appconfig.StripColor{Red: 255}, Secondary: appconfig.StripColor{Blue: 255}, PeriodMS: 800, StepMS: 100, SwapAfterSteps: 4, DimIntensity: 36}, nil
	case "envelope":
		return appconfig.StripProgram{Primitive: "envelope", Primary: appconfig.StripColor{Red: 255, Green: 255, Blue: 255}, PeriodMS: 1600, Envelope: []appconfig.StripEnvelopePoint{{AtMS: 0, Intensity: 255}, {AtMS: 45, Intensity: 24}, {AtMS: 90, Intensity: 220}, {AtMS: 145, Intensity: 52}, {AtMS: 220, Intensity: 150}, {AtMS: 360, Intensity: 0}}}, nil
	case "converging-points":
		return appconfig.StripProgram{Primitive: "converging-points", Primary: appconfig.StripColor{Red: 255}, PeriodMS: 2000}, nil
	default:
		return appconfig.StripProgram{}, fmt.Errorf("strip program primitive %q is unsupported", primitive)
	}
}

func decodeEffectDocument(path string) (effectDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return effectDocument{}, fmt.Errorf("read effect library: %w", err)
	}
	var document effectDocument
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return effectDocument{}, fmt.Errorf("parse effect library: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return effectDocument{}, errors.New("parse effect library trailing data: multiple JSON values are not allowed")
		}
		return effectDocument{}, fmt.Errorf("parse effect library trailing data: %w", err)
	}
	if len(document.Effects) == 0 {
		return effectDocument{}, errors.New("effect library contains no effects")
	}
	return document, nil
}

func writeEffectDocument(path string, document effectDocument) error {
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode effect library: %w", err)
	}
	content = append(content, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create effect library directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".effects-*.json")
	if err != nil {
		return fmt.Errorf("create temporary effect library: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect effect library: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write effect library: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush effect library: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close effect library: %w", err)
	}
	var previousPath string
	if _, err := os.Stat(path); err == nil {
		previous, err := os.CreateTemp(directory, ".effects-previous-*.json")
		if err != nil {
			return fmt.Errorf("prepare effect library replacement: %w", err)
		}
		previousPath = previous.Name()
		if err := previous.Close(); err != nil {
			_ = os.Remove(previousPath)
			return fmt.Errorf("prepare effect library replacement: %w", err)
		}
		if err := os.Remove(previousPath); err != nil {
			return fmt.Errorf("prepare effect library replacement: %w", err)
		}
		if err := os.Rename(path, previousPath); err != nil {
			return fmt.Errorf("stage existing effect library: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing effect library: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if previousPath != "" {
			if restoreErr := os.Rename(previousPath, path); restoreErr != nil {
				return fmt.Errorf("publish effect library: %w (restore previous library: %v)", err, restoreErr)
			}
		}
		return fmt.Errorf("publish effect library: %w", err)
	}
	keep = true
	if previousPath != "" {
		if err := os.Remove(previousPath); err != nil {
			return fmt.Errorf("remove replaced effect library: %w", err)
		}
	}
	return nil
}

func importEffectDocument(document effectDocument, replace bool, updateHostConfig func(func(*appconfig.Config) error) error) error {
	if updateHostConfig == nil {
		return errors.New("effect persistence is unavailable")
	}
	return updateHostConfig(func(config *appconfig.Config) error {
		macros := append([]appconfig.Macro(nil), config.Macros...)
		strips := append([]appconfig.StripEffect(nil), config.StripEffects...)
		if replace {
			macros = nil
			strips = nil
		}
		seen := make(map[string]struct{}, len(document.Effects))
		for _, effect := range document.Effects {
			id := strings.TrimSpace(effect.ID)
			if id == "" || strings.TrimSpace(effect.Name) == "" {
				return errors.New("every imported effect requires a stable id and name")
			}
			key := strings.ToLower(id)
			if _, exists := seen[key]; exists {
				return fmt.Errorf("effect id %q is duplicated in the imported library", id)
			}
			seen[key] = struct{}{}
			switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
			case "sequence":
				parsedID, err := strconv.ParseUint(id, 10, 8)
				if err != nil {
					return fmt.Errorf("sequence effect id %q must be 0..255: %w", id, err)
				}
				macro := appconfig.Macro{
					ID: byte(parsedID), Name: effect.Name, Mode: effect.Engine, Category: effect.Category,
					Icon:                effect.Icon,
					Color:               effectPropertyString(effect, "color", "green"),
					Label:               effectPropertyString(effect, "label", ""),
					LCDMessage:          effectPropertyString(effect, "lcd_message", ""),
					TimingToleranceUS:   effectPropertyUint32(effect, "timing_tolerance_us"),
					KeepOutputsOnCancel: effectPropertyBool(effect, "keep_outputs_on_cancel"),
					BoardProfileKey:     effectPropertyString(effect, "board_profile_key", ""),
					BoardProfileMode:    effectPropertyString(effect, "board_profile_mode", ""),
					Steps:               append([]appconfig.MacroStep(nil), effect.Steps...),
				}
				if macro.Mode == "" {
					macro.Mode = "auto"
				}
				replaced := false
				for index := range macros {
					if macros[index].ID == macro.ID {
						macros[index], replaced = macro, true
						break
					}
				}
				if !replaced {
					macros = append(macros, macro)
				}
			case "strip-stream":
				strip := appconfig.StripEffect{
					ID: id, Name: effect.Name, Category: effect.Category, Description: effect.Description,
					Icon:    effect.Icon,
					Program: effect.Program, DefaultFPS: effect.DefaultFPS,
					DefaultDurationMS: effect.DurationMS, DefaultPixels: effect.DefaultPixels,
				}
				replaced := false
				for index := range strips {
					if strings.EqualFold(strips[index].ID, strip.ID) {
						strips[index], replaced = strip, true
						break
					}
				}
				if !replaced {
					strips = append(strips, strip)
				}
			default:
				return fmt.Errorf("effect %q has unsupported kind %q", id, effect.Kind)
			}
		}
		ids := make(map[string]struct{}, len(macros)+len(strips))
		names := make(map[string]struct{}, len(macros)+len(strips))
		for _, effect := range EffectCatalog(macros, strips) {
			idKey := strings.ToLower(strings.TrimSpace(effect.ID))
			if _, exists := ids[idKey]; exists {
				return fmt.Errorf("effect id %q is used more than once", effect.ID)
			}
			ids[idKey] = struct{}{}
			nameKey := strings.ToLower(strings.TrimSpace(effect.Name))
			if _, exists := names[nameKey]; exists {
				return fmt.Errorf("effect name %q is used more than once", effect.Name)
			}
			names[nameKey] = struct{}{}
		}
		groups := make(map[string]appconfig.EffectGroup)
		for name, group := range document.Groups {
			groups[name] = group
		}
		for _, effect := range document.Effects {
			if strings.TrimSpace(effect.Category) != "" && strings.TrimSpace(effect.GroupIcon) != "" {
				groups[effect.Category] = appconfig.EffectGroup{Icon: effect.GroupIcon}
			}
		}
		config.Macros, config.StripEffects = macros, strips
		if replace {
			config.EffectGroups = groups
		} else {
			if config.EffectGroups == nil {
				config.EffectGroups = make(map[string]appconfig.EffectGroup)
			}
			for category, group := range groups {
				config.EffectGroups[category] = group
			}
		}
		return nil
	})
}

func effectCommand(
	ctx context.Context,
	runner *MacroRunner,
	outputs *OutputScheduler,
	hostConfig func() appconfig.Config,
	updateHostConfig func(func(*appconfig.Config) error) error,
	args []string,
) (string, error) {
	const usage = "effect list|inspect ID|play ID [auto|host|mcu|COUNT [FPS]]|stop ID|create|update|upsert-json|rename|category|icon|group update|delete|export|import|restore-examples|record|status|cancel"
	if len(args) == 0 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	config := appconfig.Defaults()
	if hostConfig != nil {
		config = hostConfig()
	}
	catalog := EffectCatalogWithGroups(runner.List(), config.StripEffects, config.EffectGroups)
	switch strings.ToLower(args[0]) {
	case "list", "catalog":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: effect list")
		}
		encoded, err := json.Marshal(catalog)
		return string(encoded), err
	case "inspect", "show", "properties":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: effect inspect REF")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(effect)
		return string(encoded), err
	case "play", "run":
		if len(args) < 2 || len(args) > 4 {
			return "", fmt.Errorf("usage: effect play REF [auto|host|mcu|COUNT [FPS]]")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		if effect.Kind == "sequence" {
			return macroCommand(ctx, runner, append([]string{"play", effect.ID}, args[2:]...))
		}
		return playStripProgramCommand(ctx, outputs, effect.ID, args[2:], config.StripEffects)
	case "stop":
		if len(args) == 1 {
			stripMessage, stripErr := stripStreamCommand(ctx, outputs, []string{"stop"}, config.StripEffects)
			if runner.State().Running {
				if err := runner.Cancel(ctx); err != nil {
					return "", err
				}
			}
			return stripMessage, stripErr
		}
		if len(args) != 2 {
			return "", errors.New("usage: effect stop [ID]")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		switch effect.Kind {
		case "strip-stream":
			return stripStreamCommand(ctx, outputs, []string{"stop"}, config.StripEffects)
		case "sequence":
			return macroCommand(ctx, runner, []string{"cancel"})
		default:
			return "", fmt.Errorf("effect %q cannot be stopped", args[1])
		}
	case "create":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: %s", usage)
		}
		switch strings.ToLower(args[1]) {
		case "sequence":
			if len(args) < 4 || len(args) > 7 {
				return "", errors.New("usage: effect create sequence ID NAME [CATEGORY [COLOR [ICON]]]")
			}
			if err := ensureEffectIdentityAvailable(catalog, "", args[2], args[3]); err != nil {
				return "", err
			}
			macroArgs := append([]string{"create"}, args[2:min(len(args), 6)]...)
			message, err := macroCommand(ctx, runner, macroArgs)
			if err != nil || len(args) < 7 {
				return message, err
			}
			created, err := findEffect(EffectCatalogWithGroups(runner.List(), config.StripEffects, config.EffectGroups), args[2])
			if err != nil {
				return "", err
			}
			if err := updateHostConfig(func(config *appconfig.Config) error { return setEffectIcon(config, created, args[6]) }); err != nil {
				return "", err
			}
			return message, nil
		case "strip":
			if len(args) < 5 || len(args) > 9 {
				return "", fmt.Errorf("usage: effect create strip ID NAME PRIMITIVE [CATEGORY [FPS [DURATION_MS [PIXELS]]]]")
			}
			program, err := stripProgramTemplate(args[4])
			if err != nil {
				return "", err
			}
			effect := appconfig.StripEffect{ID: args[2], Name: args[3], Program: program, Category: "Lighting", DefaultFPS: 20, DefaultDurationMS: 5000, DefaultPixels: 100}
			var parseErr error
			if len(args) > 5 {
				effect.Category = args[5]
			}
			if len(args) > 6 {
				effect.DefaultFPS, parseErr = strconv.Atoi(args[6])
				if parseErr != nil {
					return "", fmt.Errorf("FPS: %w", parseErr)
				}
			}
			if len(args) > 7 {
				effect.DefaultDurationMS, parseErr = strconv.Atoi(args[7])
				if parseErr != nil {
					return "", fmt.Errorf("duration: %w", parseErr)
				}
			}
			if len(args) > 8 {
				effect.DefaultPixels, parseErr = strconv.Atoi(args[8])
				if parseErr != nil {
					return "", fmt.Errorf("pixels: %w", parseErr)
				}
			}
			if updateHostConfig == nil {
				return "", errors.New("effect persistence is unavailable")
			}
			if err := updateHostConfig(func(config *appconfig.Config) error {
				for _, current := range EffectCatalog(config.Macros, config.StripEffects) {
					if strings.EqualFold(current.ID, effect.ID) || strings.EqualFold(current.Name, effect.Name) {
						return fmt.Errorf("effect %q already exists", effect.ID)
					}
				}
				config.StripEffects = append(config.StripEffects, effect)
				return nil
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("effect effect:%s created", effect.ID), nil
		case "strip-json":
			if len(args) != 10 && len(args) != 11 {
				return "", errors.New("usage: effect create strip-json ID NAME CATEGORY DESCRIPTION PROGRAM_HEX FPS DURATION_MS PIXELS [ICON]")
			}
			programJSON, err := hex.DecodeString(args[6])
			if err != nil {
				return "", fmt.Errorf("decode strip program: %w", err)
			}
			var program appconfig.StripProgram
			if err := json.Unmarshal(programJSON, &program); err != nil {
				return "", fmt.Errorf("parse strip program: %w", err)
			}
			fps, err := strconv.Atoi(args[7])
			if err != nil {
				return "", fmt.Errorf("FPS: %w", err)
			}
			duration, err := strconv.Atoi(args[8])
			if err != nil {
				return "", fmt.Errorf("duration: %w", err)
			}
			pixels, err := strconv.Atoi(args[9])
			if err != nil {
				return "", fmt.Errorf("pixels: %w", err)
			}
			description := args[5]
			if description == "-" {
				description = ""
			}
			icon := ""
			if len(args) == 11 && args[10] != "-" {
				icon = args[10]
			}
			effect := appconfig.StripEffect{ID: args[2], Name: args[3], Category: args[4], Icon: icon, Description: description, Program: program, DefaultFPS: fps, DefaultDurationMS: duration, DefaultPixels: pixels}
			if updateHostConfig == nil {
				return "", errors.New("effect persistence is unavailable")
			}
			if err := updateHostConfig(func(config *appconfig.Config) error {
				for _, current := range EffectCatalog(config.Macros, config.StripEffects) {
					if strings.EqualFold(current.ID, effect.ID) || strings.EqualFold(current.Name, effect.Name) {
						return fmt.Errorf("effect %q already exists", effect.ID)
					}
				}
				config.StripEffects = append(config.StripEffects, effect)
				return nil
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("effect effect:%s created", effect.ID), nil
		default:
			return "", fmt.Errorf("effect kind %q is unknown", args[1])
		}
	case "update":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: %s", usage)
		}
		effect, findErr := findEffect(catalog, args[1])
		if findErr != nil {
			return "", findErr
		}
		id := effect.ID
		if effect.Kind == "sequence" {
			if len(args) != 5 && len(args) != 6 {
				return "", errors.New("usage: effect update ID NAME CATEGORY COLOR [ICON]")
			}
			if err := ensureEffectIdentityAvailable(catalog, effect.Reference, effect.ID, args[2]); err != nil {
				return "", err
			}
			message, err := macroCommand(ctx, runner, []string{"update", id, args[2], args[3], args[4]})
			if err != nil || len(args) != 6 {
				return message, err
			}
			if err := updateHostConfig(func(config *appconfig.Config) error { return setEffectIcon(config, effect, args[5]) }); err != nil {
				return "", err
			}
			return message, nil
		}
		if len(args) != 9 {
			return "", fmt.Errorf("usage: effect update ID NAME CATEGORY DESCRIPTION PRIMITIVE FPS DURATION_MS PIXELS")
		}
		if effect.Kind != "strip-stream" {
			return "", errors.New("effect update requires a timed sequence or strip program")
		}
		if err := ensureEffectIdentityAvailable(catalog, effect.Reference, effect.ID, args[2]); err != nil {
			return "", err
		}
		fps, err := strconv.Atoi(args[6])
		if err != nil {
			return "", fmt.Errorf("FPS: %w", err)
		}
		duration, err := strconv.Atoi(args[7])
		if err != nil {
			return "", fmt.Errorf("duration: %w", err)
		}
		pixels, err := strconv.Atoi(args[8])
		if err != nil {
			return "", fmt.Errorf("pixels: %w", err)
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index := range config.StripEffects {
				if strings.EqualFold(config.StripEffects[index].ID, id) {
					description := args[4]
					if description == "-" {
						description = ""
					}
					program := config.StripEffects[index].Program
					if !strings.EqualFold(program.Primitive, args[5]) {
						var err error
						program, err = stripProgramTemplate(args[5])
						if err != nil {
							return err
						}
					}
					config.StripEffects[index].Name, config.StripEffects[index].Category, config.StripEffects[index].Description, config.StripEffects[index].Program = args[2], args[3], description, program
					config.StripEffects[index].DefaultFPS, config.StripEffects[index].DefaultDurationMS, config.StripEffects[index].DefaultPixels = fps, duration, pixels
					return nil
				}
			}
			return fmt.Errorf("effect %s is not configured", id)
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect effect:%s updated", id), nil
	case "update-json":
		if len(args) != 9 && len(args) != 10 {
			return "", errors.New("usage: effect update-json ID NAME CATEGORY DESCRIPTION PROGRAM_HEX FPS DURATION_MS PIXELS [ICON]")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		if effect.Kind != "strip-stream" {
			return "", errors.New("only lighting effects have a strip program")
		}
		if err := ensureEffectIdentityAvailable(catalog, effect.Reference, effect.ID, args[2]); err != nil {
			return "", err
		}
		programJSON, err := hex.DecodeString(args[5])
		if err != nil {
			return "", fmt.Errorf("decode strip program: %w", err)
		}
		var program appconfig.StripProgram
		if err := json.Unmarshal(programJSON, &program); err != nil {
			return "", fmt.Errorf("parse strip program: %w", err)
		}
		fps, err := strconv.Atoi(args[6])
		if err != nil {
			return "", fmt.Errorf("FPS: %w", err)
		}
		duration, err := strconv.Atoi(args[7])
		if err != nil {
			return "", fmt.Errorf("duration: %w", err)
		}
		pixels, err := strconv.Atoi(args[8])
		if err != nil {
			return "", fmt.Errorf("pixels: %w", err)
		}
		description := args[4]
		if description == "-" {
			description = ""
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index := range config.StripEffects {
				if strings.EqualFold(config.StripEffects[index].ID, effect.ID) {
					config.StripEffects[index].Name, config.StripEffects[index].Category, config.StripEffects[index].Description = args[2], args[3], description
					config.StripEffects[index].Program, config.StripEffects[index].DefaultFPS = program, fps
					config.StripEffects[index].DefaultDurationMS, config.StripEffects[index].DefaultPixels = duration, pixels
					return nil
				}
			}
			return fmt.Errorf("effect %q is not configured", args[1])
		}); err != nil {
			return "", err
		}
		if len(args) == 10 {
			if err := updateHostConfig(func(config *appconfig.Config) error { return setEffectIcon(config, effect, args[9]) }); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("effect %s updated", effect.Reference), nil
	case "upsert-json":
		if len(args) != 2 {
			return "", errors.New("usage: effect upsert-json EFFECT_JSON_HEX")
		}
		encoded, err := hex.DecodeString(args[1])
		if err != nil {
			return "", fmt.Errorf("decode effect definition: %w", err)
		}
		var effect EffectDescriptor
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.UseNumber()
		if err := decoder.Decode(&effect); err != nil {
			return "", fmt.Errorf("parse effect definition: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			if err == nil {
				return "", errors.New("parse effect definition trailing data: multiple JSON values are not allowed")
			}
			return "", fmt.Errorf("parse effect definition trailing data: %w", err)
		}
		if err := importEffectDocument(effectDocument{Effects: []EffectDescriptor{effect}}, false, updateHostConfig); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect effect:%s saved", strings.TrimSpace(effect.ID)), nil
	case "icon":
		if len(args) != 3 {
			return "", errors.New("usage: effect icon REF ICON_OR_DASH")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		icon := strings.TrimSpace(args[2])
		if icon == "-" {
			icon = ""
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error { return setEffectIcon(config, effect, icon) }); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect %s icon updated", effect.Reference), nil
	case "group":
		if len(args) == 2 && strings.EqualFold(args[1], "list") {
			encoded, err := json.Marshal(EffectGroupCatalog(config))
			return string(encoded), err
		}
		if len(args) == 4 && strings.EqualFold(args[1], "create") {
			name, icon := strings.TrimSpace(args[2]), strings.TrimSpace(args[3])
			if name == "" {
				return "", errors.New("effect group name must not be blank")
			}
			if icon == "-" {
				icon = ""
			}
			if updateHostConfig == nil {
				return "", errors.New("effect persistence is unavailable")
			}
			if err := updateHostConfig(func(config *appconfig.Config) error {
				for _, group := range EffectGroupCatalog(*config) {
					if strings.EqualFold(group.Name, name) {
						return fmt.Errorf("effect group %q already exists", name)
					}
				}
				if config.EffectGroups == nil {
					config.EffectGroups = make(map[string]appconfig.EffectGroup)
				}
				config.EffectGroups[name] = appconfig.EffectGroup{Icon: icon}
				return nil
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("effect group %q created", name), nil
		}
		if len(args) != 5 || !strings.EqualFold(args[1], "update") {
			return "", errors.New("usage: effect group list|create NAME ICON_OR_DASH|update CURRENT_GROUP NEW_GROUP ICON_OR_DASH")
		}
		current, next, icon := strings.TrimSpace(args[2]), strings.TrimSpace(args[3]), strings.TrimSpace(args[4])
		if current == "" || next == "" {
			return "", errors.New("effect group categories must not be blank")
		}
		if icon == "-" {
			icon = ""
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		changed := 0
		if err := updateHostConfig(func(config *appconfig.Config) error {
			found := false
			for _, group := range EffectGroupCatalog(*config) {
				if strings.EqualFold(group.Name, current) {
					found = true
				}
				if !strings.EqualFold(current, next) && strings.EqualFold(group.Name, next) {
					return fmt.Errorf("effect group %q already exists", next)
				}
			}
			if !found {
				return fmt.Errorf("effect group %q is not configured", current)
			}
			for index := range config.Macros {
				if strings.EqualFold(strings.TrimSpace(config.Macros[index].Category), current) {
					config.Macros[index].Category = next
					changed++
				}
			}
			for index := range config.StripEffects {
				if strings.EqualFold(strings.TrimSpace(config.StripEffects[index].Category), current) {
					config.StripEffects[index].Category = next
					changed++
				}
			}
			if config.EffectGroups == nil {
				config.EffectGroups = make(map[string]appconfig.EffectGroup)
			}
			for category := range config.EffectGroups {
				if strings.EqualFold(strings.TrimSpace(category), current) {
					delete(config.EffectGroups, category)
				}
			}
			config.EffectGroups[next] = appconfig.EffectGroup{Icon: icon}
			return nil
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect group %q updated as %q (%d effects)", current, next, changed), nil
	case "rename", "category":
		if len(args) != 3 {
			return "", fmt.Errorf("usage: effect %s REF VALUE", args[0])
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		if strings.EqualFold(args[0], "rename") {
			if err := ensureEffectIdentityAvailable(catalog, effect.Reference, effect.ID, args[2]); err != nil {
				return "", err
			}
		}
		if effect.Kind == "sequence" {
			return macroCommand(ctx, runner, []string{strings.ToLower(args[0]), effect.ID, args[2]})
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index := range config.StripEffects {
				if strings.EqualFold(config.StripEffects[index].ID, effect.ID) {
					if strings.EqualFold(args[0], "rename") {
						config.StripEffects[index].Name = args[2]
					} else {
						config.StripEffects[index].Category = args[2]
					}
					return nil
				}
			}
			return fmt.Errorf("effect %q is not configured", args[1])
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect %s updated", args[1]), nil
	case "program":
		if len(args) != 3 {
			return "", errors.New("usage: effect program ID JSON_HEX")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		if effect.Kind != "strip-stream" {
			return "", errors.New("only lighting effects have a strip program")
		}
		encoded, err := hex.DecodeString(args[2])
		if err != nil {
			return "", fmt.Errorf("decode strip program: %w", err)
		}
		var program appconfig.StripProgram
		if err := json.Unmarshal(encoded, &program); err != nil {
			return "", fmt.Errorf("parse strip program: %w", err)
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index := range config.StripEffects {
				if strings.EqualFold(config.StripEffects[index].ID, effect.ID) {
					config.StripEffects[index].Program = program
					return nil
				}
			}
			return fmt.Errorf("effect %q is not configured", args[1])
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect %s program updated", effect.Reference), nil
	case "delete", "remove":
		if len(args) != 2 {
			return "", errors.New("usage: effect delete REF")
		}
		effect, err := findEffect(catalog, args[1])
		if err != nil {
			return "", err
		}
		if effect.Kind == "sequence" {
			return macroCommand(ctx, runner, []string{"delete", effect.ID})
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index, configuredEffect := range config.StripEffects {
				if strings.EqualFold(configuredEffect.ID, effect.ID) {
					config.StripEffects = append(config.StripEffects[:index], config.StripEffects[index+1:]...)
					return nil
				}
			}
			return fmt.Errorf("effect %q is not configured", args[1])
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect %s deleted", args[1]), nil
	case "export":
		if len(args) > 2 {
			return "", errors.New("usage: effect export [PATH]")
		}
		document := effectDocument{Effects: catalog, Groups: config.EffectGroups}
		if len(args) == 1 {
			encoded, err := json.MarshalIndent(document, "", "  ")
			return string(encoded), err
		}
		path, err := filepath.Abs(args[1])
		if err != nil {
			return "", fmt.Errorf("resolve effect library path: %w", err)
		}
		if err := writeEffectDocument(path, document); err != nil {
			return "", err
		}
		return fmt.Sprintf("exported %d effects to %s", len(catalog), path), nil
	case "import":
		if len(args) < 2 || len(args) > 3 {
			return "", errors.New("usage: effect import PATH [merge|replace]")
		}
		replace := false
		if len(args) == 3 {
			switch strings.ToLower(args[2]) {
			case "merge":
			case "replace":
				replace = true
			default:
				return "", errors.New("effect import mode must be merge or replace")
			}
		}
		path, err := filepath.Abs(args[1])
		if err != nil {
			return "", fmt.Errorf("resolve effect library path: %w", err)
		}
		document, err := decodeEffectDocument(path)
		if err != nil {
			return "", err
		}
		if err := importEffectDocument(document, replace, updateHostConfig); err != nil {
			return "", err
		}
		return fmt.Sprintf("imported %d effects from %s (%s)", len(document.Effects), path, map[bool]string{true: "replace", false: "merge"}[replace]), nil
	case "restore-examples", "restore-samples":
		if len(args) != 1 {
			return "", errors.New("usage: effect restore-examples")
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		added := 0
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for _, example := range appconfig.DefaultStripEffects() {
				present := false
				for _, configured := range config.StripEffects {
					if strings.EqualFold(strings.TrimSpace(configured.ID), strings.TrimSpace(example.ID)) ||
						strings.EqualFold(strings.TrimSpace(configured.Name), strings.TrimSpace(example.Name)) {
						present = true
						break
					}
				}
				if !present {
					config.StripEffects = append(config.StripEffects, example)
					added++
				}
			}
			return nil
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("restored %d missing editable example effects; existing user effects were unchanged", added), nil
	case "record", "status", "cancel", "buffer":
		return macroCommand(ctx, runner, args)
	default:
		return "", fmt.Errorf("usage: %s", usage)
	}
}
