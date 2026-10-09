package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/shell"
)

const macroHostMenuOptionsSource = "macro.library"

// hostMacroMenuActions adapts the shared MacroRunner to compact physical-menu
// reads and actions. Macro definitions remain owned by the watched host config.
type hostMacroMenuActions struct {
	store   *appconfig.Store
	runtime *control.Runtime
	engine  *shell.Engine

	mu       sync.Mutex
	selected string
}

func newHostMacroMenuActions(
	store *appconfig.Store,
	runtime *control.Runtime,
	engine *shell.Engine,
) *hostMacroMenuActions {
	return &hostMacroMenuActions{store: store, runtime: runtime, engine: engine}
}

func (actions *hostMacroMenuActions) Sync(manager *hostmenu.Manager, macros []appconfig.Macro) error {
	options := macroHostMenuOptions(macros)
	actions.mu.Lock()
	if !macroReferenceExists(actions.selected, macros) {
		actions.selected = ""
		if len(options) != 0 {
			actions.selected = options[0].Value
		}
	}
	actions.mu.Unlock()
	return manager.UpdateSelectOptions(macroHostMenuOptionsSource, options)
}

func macroHostMenuOptions(macros []appconfig.Macro) []appconfig.HostMenuOption {
	ordered := append([]appconfig.Macro(nil), macros...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].ID < ordered[right].ID })
	options := make([]appconfig.HostMenuOption, 0, len(ordered))
	for _, macro := range ordered {
		label := fmt.Sprintf("%d %s", macro.ID, strings.TrimSpace(macro.Name))
		if len(label) > 16 {
			label = label[:16]
		}
		options = append(options, appconfig.HostMenuOption{
			Label: label, Value: strconv.Itoa(int(macro.ID)),
		})
	}
	return options
}

func macroReferenceExists(reference string, macros []appconfig.Macro) bool {
	if reference == "" {
		return false
	}
	for _, macro := range macros {
		if strconv.Itoa(int(macro.ID)) == reference {
			return true
		}
	}
	return false
}

func (actions *hostMacroMenuActions) Read(action string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "host.macro.selection":
		macro, err := actions.selectedMacro()
		if err != nil {
			return "", true, nil
		}
		return strconv.Itoa(int(macro.ID)), true, nil
	case "host.macro.selected":
		macro, err := actions.selectedMacro()
		if err != nil {
			return "None", true, nil
		}
		return fmt.Sprintf("%d %s %dst", macro.ID, macro.Name, len(macro.Steps)), true, nil
	case "host.macro.playback":
		runner := actions.runtime.MacroRunner()
		if runner == nil {
			return "Unavailable", true, nil
		}
		state := runner.State()
		if state.Name == "" {
			return "Idle", true, nil
		}
		if state.Running {
			elapsed := time.Since(state.StartedAt).Round(100 * time.Millisecond)
			return fmt.Sprintf("%d/%d %s", state.Step, state.StepCount, elapsed), true, nil
		}
		return fmt.Sprintf("%s %d/%d", compactMacroLifecycle(state.Lifecycle), state.Step, state.StepCount), true, nil
	case "host.macro.recording":
		runner := actions.runtime.MacroRunner()
		if runner == nil {
			return "Unavailable", true, nil
		}
		state := runner.RecordingState()
		if !state.Active {
			return "Idle", true, nil
		}
		return fmt.Sprintf("REC %d %dst", state.ID, state.Steps), true, nil
	default:
		return "", false, nil
	}
}

func compactMacroLifecycle(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "IDLE"
	}
	if len(value) > 7 {
		return value[:7]
	}
	return value
}

func (actions *hostMacroMenuActions) Write(action, value string) (string, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(action), "host.macro.selection") {
		return "", false, nil
	}
	value = strings.TrimSpace(value)
	if !macroReferenceExists(value, actions.store.Current().Macros) {
		return "", true, fmt.Errorf("macro ID %q is not configured", value)
	}
	actions.mu.Lock()
	actions.selected = value
	actions.mu.Unlock()
	macro, _ := actions.selectedMacro()
	return fmt.Sprintf("Selected %d/%s", macro.ID, macro.Name), true, nil
}

func (actions *hostMacroMenuActions) Execute(
	ctx context.Context,
	action string,
) (string, bool, error) {
	if actions.engine == nil {
		return "", true, errors.New("macro command engine is unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "host.macro.play":
		macro, err := actions.selectedMacro()
		if err != nil {
			return "", true, err
		}
		if _, err = actions.engine.Execute(ctx, shell.Join([]string{"macro", "play", strconv.Itoa(int(macro.ID))})); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("PLAY %d %s", macro.ID, macro.Name), true, nil
	case "host.macro.record.start":
		name := actions.uniqueRecordingName(time.Now())
		if _, err := actions.engine.Execute(ctx, shell.Join([]string{"macro", "record", "start", name, "Panel", "violet"})); err != nil {
			return "", true, err
		}
		return "REC " + name, true, nil
	case "host.macro.record.save":
		if _, err := actions.engine.Execute(ctx, "macro record save"); err != nil {
			return "", true, err
		}
		return "SAVED", true, nil
	case "host.macro.record.discard":
		if _, err := actions.engine.Execute(ctx, "macro record discard"); err != nil {
			return "", true, err
		}
		return "DISCARDED", true, nil
	case "host.macro.cancel":
		if _, err := actions.engine.Execute(ctx, "macro cancel"); err != nil {
			return "", true, err
		}
		return "STOPPED SAFE", true, nil
	case "host.macro.cancel.keep":
		if _, err := actions.engine.Execute(ctx, "macro cancel keep"); err != nil {
			return "", true, err
		}
		return "STOPPED KEEP", true, nil
	default:
		return "", false, nil
	}
}

func (actions *hostMacroMenuActions) selectedMacro() (appconfig.Macro, error) {
	macros := actions.store.Current().Macros
	actions.mu.Lock()
	reference := actions.selected
	if !macroReferenceExists(reference, macros) && len(macros) != 0 {
		options := macroHostMenuOptions(macros)
		reference = options[0].Value
		actions.selected = reference
	}
	actions.mu.Unlock()
	for _, macro := range macros {
		if strconv.Itoa(int(macro.ID)) == reference {
			return macro, nil
		}
	}
	return appconfig.Macro{}, errors.New("no macro is configured")
}

func (actions *hostMacroMenuActions) uniqueRecordingName(now time.Time) string {
	base := "panel-" + now.Format("060102-150405")
	used := make(map[string]bool)
	for _, macro := range actions.store.Current().Macros {
		used[strings.ToLower(macro.Name)] = true
	}
	if !used[strings.ToLower(base)] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if !used[strings.ToLower(candidate)] {
			return candidate
		}
	}
}
