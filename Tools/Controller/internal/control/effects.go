package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Description   string                 `json:"description,omitempty"`
	Kind          string                 `json:"kind"`
	Engine        string                 `json:"engine"`
	Editable      bool                   `json:"editable"`
	DurationMS    int                    `json:"duration_ms"`
	DefaultFPS    int                    `json:"default_fps,omitempty"`
	DefaultPixels int                    `json:"default_pixels,omitempty"`
	Pattern       string                 `json:"pattern,omitempty"`
	Steps         []appconfig.MacroStep  `json:"steps,omitempty"`
	Properties    map[string]interface{} `json:"properties,omitempty"`
}

func EffectCatalog(macros []appconfig.Macro, strips []appconfig.StripEffect) []EffectDescriptor {
	result := make([]EffectDescriptor, 0, len(macros)+len(strips))
	for _, macro := range macros {
		duration := 0
		if len(macro.Steps) != 0 {
			duration = int(macro.Steps[len(macro.Steps)-1].AtUS / 1000)
		}
		result = append(result, EffectDescriptor{
			Reference: "sequence:" + strconv.Itoa(int(macro.ID)), ID: strconv.Itoa(int(macro.ID)),
			Name: macro.Name, Category: macro.Category, Kind: "sequence", Engine: macro.Mode,
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
			Reference: "strip:" + effect.ID, ID: effect.ID, Name: effect.Name,
			Category: effect.Category, Description: effect.Description,
			Kind: "strip-stream", Engine: "host", Editable: true,
			DurationMS: effect.DefaultDurationMS, DefaultFPS: effect.DefaultFPS,
			DefaultPixels: effect.DefaultPixels, Pattern: effect.Pattern,
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

func effectCommand(
	ctx context.Context,
	runner *MacroRunner,
	outputs *OutputScheduler,
	hostConfig func() appconfig.Config,
	updateHostConfig func(func(*appconfig.Config) error) error,
	args []string,
) (string, error) {
	const usage = "effect list|inspect REF|play REF [host|mcu|COUNT [FPS]]|stop REF|create sequence ID NAME [CATEGORY [COLOR]]|create strip ID NAME PATTERN [CATEGORY [FPS [DURATION_MS [PIXELS]]]]|update sequence:ID NAME CATEGORY COLOR|update strip:ID NAME CATEGORY DESCRIPTION PATTERN FPS DURATION_MS PIXELS|rename REF NAME|category REF CATEGORY|delete REF|record ...|status|cancel [keep]"
	if len(args) == 0 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	config := appconfig.Defaults()
	if hostConfig != nil {
		config = hostConfig()
	}
	catalog := EffectCatalog(runner.List(), config.StripEffects)
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
		kind, id := splitEffectReference(args[1])
		for _, effect := range catalog {
			if (kind == "" && (strings.EqualFold(effect.ID, id) || strings.EqualFold(effect.Name, id))) ||
				strings.EqualFold(effect.Reference, kind+":"+id) {
				encoded, err := json.Marshal(effect)
				return string(encoded), err
			}
		}
		return "", fmt.Errorf("effect %q is not configured", args[1])
	case "play", "run":
		if len(args) < 2 || len(args) > 4 {
			return "", fmt.Errorf("usage: effect play REF [host|mcu|COUNT [FPS]]")
		}
		kind, id := splitEffectReference(args[1])
		if kind == "sequence" || kind == "macro" {
			return macroCommand(ctx, runner, append([]string{"play", id}, args[2:]...))
		}
		if kind == "strip" {
			return stripStreamCommand(ctx, outputs, append([]string{"effect", "play", id}, args[2:]...), config.StripEffects)
		}
		if _, err := runner.find(id); err == nil {
			return macroCommand(ctx, runner, append([]string{"play", id}, args[2:]...))
		}
		return stripStreamCommand(ctx, outputs, append([]string{"effect", "play", id}, args[2:]...), config.StripEffects)
	case "stop":
		if len(args) != 2 {
			return "", errors.New("usage: effect stop REF")
		}
		kind, _ := splitEffectReference(args[1])
		switch kind {
		case "strip":
			return stripStreamCommand(ctx, outputs, []string{"stop"}, config.StripEffects)
		case "sequence", "macro":
			return macroCommand(ctx, runner, []string{"cancel"})
		default:
			return "", fmt.Errorf("effect stop requires a sequence: or strip: reference")
		}
	case "create":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: %s", usage)
		}
		switch strings.ToLower(args[1]) {
		case "sequence", "macro":
			return macroCommand(ctx, runner, append([]string{"create"}, args[2:]...))
		case "strip":
			if len(args) < 5 || len(args) > 9 {
				return "", fmt.Errorf("usage: effect create strip ID NAME PATTERN [CATEGORY [FPS [DURATION_MS [PIXELS]]]]")
			}
			effect := appconfig.StripEffect{ID: args[2], Name: args[3], Pattern: args[4], Category: "Lighting", DefaultFPS: 20, DefaultDurationMS: 5000, DefaultPixels: 100}
			var err error
			if len(args) > 5 {
				effect.Category = args[5]
			}
			if len(args) > 6 {
				effect.DefaultFPS, err = strconv.Atoi(args[6])
				if err != nil {
					return "", fmt.Errorf("FPS: %w", err)
				}
			}
			if len(args) > 7 {
				effect.DefaultDurationMS, err = strconv.Atoi(args[7])
				if err != nil {
					return "", fmt.Errorf("duration: %w", err)
				}
			}
			if len(args) > 8 {
				effect.DefaultPixels, err = strconv.Atoi(args[8])
				if err != nil {
					return "", fmt.Errorf("pixels: %w", err)
				}
			}
			if updateHostConfig == nil {
				return "", errors.New("effect persistence is unavailable")
			}
			if err := updateHostConfig(func(config *appconfig.Config) error {
				for _, current := range config.StripEffects {
					if strings.EqualFold(current.ID, effect.ID) || strings.EqualFold(current.Name, effect.Name) {
						return fmt.Errorf("effect %q already exists", effect.ID)
					}
				}
				config.StripEffects = append(config.StripEffects, effect)
				return nil
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("effect strip:%s created", effect.ID), nil
		default:
			return "", fmt.Errorf("effect kind %q is unknown", args[1])
		}
	case "update":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: %s", usage)
		}
		kind, id := splitEffectReference(args[1])
		if kind == "sequence" || kind == "macro" {
			if len(args) != 5 {
				return "", errors.New("usage: effect update sequence:ID NAME CATEGORY COLOR")
			}
			return macroCommand(ctx, runner, []string{"update", id, args[2], args[3], args[4]})
		}
		if len(args) != 9 {
			return "", fmt.Errorf("usage: effect update strip:ID NAME CATEGORY DESCRIPTION PATTERN FPS DURATION_MS PIXELS")
		}
		if kind != "strip" {
			return "", errors.New("effect update requires a sequence: or strip: reference")
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
					config.StripEffects[index].Name, config.StripEffects[index].Category, config.StripEffects[index].Description, config.StripEffects[index].Pattern = args[2], args[3], description, args[5]
					config.StripEffects[index].DefaultFPS, config.StripEffects[index].DefaultDurationMS, config.StripEffects[index].DefaultPixels = fps, duration, pixels
					return nil
				}
			}
			return fmt.Errorf("effect strip:%s is not configured", id)
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect strip:%s updated", id), nil
	case "rename", "category":
		if len(args) != 3 {
			return "", fmt.Errorf("usage: effect %s REF VALUE", args[0])
		}
		kind, id := splitEffectReference(args[1])
		if kind == "sequence" || kind == "macro" || kind == "" {
			command := strings.ToLower(args[0])
			if _, err := runner.find(id); err == nil {
				return macroCommand(ctx, runner, []string{command, id, args[2]})
			}
			if kind != "" {
				return "", fmt.Errorf("effect %q is not configured", args[1])
			}
		}
		if kind != "strip" && kind != "" {
			return "", fmt.Errorf("effect kind %q is unknown", kind)
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index := range config.StripEffects {
				if strings.EqualFold(config.StripEffects[index].ID, id) || (kind == "" && strings.EqualFold(config.StripEffects[index].Name, id)) {
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
	case "delete", "remove":
		if len(args) != 2 {
			return "", errors.New("usage: effect delete REF")
		}
		kind, id := splitEffectReference(args[1])
		if kind == "sequence" || kind == "macro" || kind == "" {
			_, findErr := runner.find(id)
			if findErr == nil {
				return macroCommand(ctx, runner, []string{"delete", id})
			}
			if kind != "" {
				return "", findErr
			}
		}
		if updateHostConfig == nil {
			return "", errors.New("effect persistence is unavailable")
		}
		if err := updateHostConfig(func(config *appconfig.Config) error {
			for index, effect := range config.StripEffects {
				if strings.EqualFold(effect.ID, id) || (kind == "" && strings.EqualFold(effect.Name, id)) {
					config.StripEffects = append(config.StripEffects[:index], config.StripEffects[index+1:]...)
					return nil
				}
			}
			return fmt.Errorf("effect %q is not configured", args[1])
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("effect %s deleted", args[1]), nil
	case "record", "status", "cancel", "buffer":
		return macroCommand(ctx, runner, args)
	default:
		return "", fmt.Errorf("usage: %s", usage)
	}
}
