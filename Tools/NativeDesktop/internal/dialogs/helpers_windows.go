//go:build windows

package dialogs

import (
	"fmt"
	"strings"
	"time"

	"github.com/lxn/walk"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/design"
	"pccontroller.local/desktop/internal/winfx"
)

var (
	viewModeLabels = []string{"Compact device", "Dashboard"}
	viewModeValues = []string{config.ViewCompact, config.ViewDashboard}
	themeLabels    = []string{"Follow Windows", "Light", "Dark"}
	themeValues    = []string{"system", "light", "dark"}
	backdropLabels = []string{
		"Automatic",
		"Solid (no effect)",
		"Transparent (no backdrop)",
		"Blur",
		"Acrylic",
		"Mica",
		"Tabbed",
	}
	backdropValues = []string{
		config.BackdropAuto,
		config.BackdropSolid,
		config.BackdropTransparent,
		config.BackdropBlur,
		config.BackdropAcrylic,
		config.BackdropMica,
		config.BackdropTabbed,
	}
	renderBackendLabels = []string{
		"Automatic (DirectX when supported)",
		"DirectX 11 hardware",
		"Compatibility renderer (lower RAM)",
	}
	renderBackendValues = []string{
		config.RenderBackendAuto,
		config.RenderBackendDirectX,
		config.RenderBackendLegacy,
	}
	titleBarLabels = []string{"None", "Custom controls", "Native Windows title bar"}
	titleBarValues = []string{
		config.TitleBarNone,
		config.TitleBarCustom,
		config.TitleBarNative,
	}
	borderModeLabels  = []string{"Automatic (resizable)", "None (frameless)"}
	borderModeValues  = []string{config.BorderAuto, config.BorderNone}
	deliveryLabels    = []string{"Automatic", "Modern toast", "Legacy balloon"}
	deliveryValues    = []string{"auto", "toast", "balloon"}
	soundSourceLabels = []string{"Bundled PCController sounds", "Custom sound"}
	soundSourceValues = []string{
		config.SoundSourceBundled,
		config.SoundSourceCustom,
	}
	localeLabels = []string{"System default", "English", "Persian"}
	localeValues = []string{
		config.LocaleAuto,
		config.LocaleEnglish,
		config.LocalePersian,
	}
)

func indexOfValue(values []string, value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	for index, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return index
		}
	}
	return 0
}

func valueAt(values []string, index int, field string) (string, error) {
	if index < 0 || index >= len(values) {
		return "", fmt.Errorf("%s: select a value", field)
	}
	return values[index], nil
}

func parseDuration(text, field string) (config.Duration, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("%s: enter a duration such as 5s or 1m", field)
	}
	value, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a valid duration", field, text)
	}
	return config.Duration(value), nil
}

func canonicalizeHotkeys(hotkeys *config.HotkeysConfig) error {
	if hotkeys == nil {
		return fmt.Errorf("hotkeys configuration is nil")
	}
	values := []struct {
		name  string
		value *string
	}{
		{"Toggle hotkey", &hotkeys.Toggle},
		{"Turn-on hotkey", &hotkeys.On},
		{"Turn-off hotkey", &hotkeys.Off},
		{"Show-window hotkey", &hotkeys.Show},
	}
	for _, item := range values {
		text := strings.TrimSpace(*item.value)
		if text == "" {
			*item.value = ""
			continue
		}
		parsed, err := winfx.ParseHotkey(text)
		if err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
		*item.value = parsed.String()
	}
	return nil
}

func showValidationError(owner walk.Form, title string, err error) {
	showDialogError(
		owner,
		title,
		"Check the highlighted settings and try again.",
		err,
	)
}

func showDialogError(
	owner walk.Form,
	title string,
	mainInstruction string,
	err error,
) {
	if err == nil {
		return
	}
	parent := uintptr(0)
	rtl := false
	if owner != nil {
		parent = uintptr(owner.Handle())
		rtl = owner.RightToLeftLayout()
	}
	if rtl {
		localizer := dialogLocalizer(config.Config{
			UI: config.UIConfig{Locale: config.LocalePersian},
		})
		title = dialogText(localizer, title)
		mainInstruction = dialogText(localizer, mainInstruction)
	}
	if _, taskErr := winfx.TaskDialog(winfx.TaskDialogOptions{
		Parent:          parent,
		Title:           title,
		MainInstruction: mainInstruction,
		Content:         err.Error(),
		Icon:            winfx.DialogIconWarning,
		Buttons:         winfx.DialogButtonOK,
		RightToLeft:     rtl,
	}); taskErr != nil {
		style := walk.MsgBoxOK | walk.MsgBoxIconWarning
		if rtl {
			style |= walk.MsgBoxRight | walk.MsgBoxRTLReading
		}
		walk.MsgBox(owner, title, err.Error(), style)
	}
}

type dialogThemeSession struct {
	dialog          *walk.Dialog
	brushes         []walk.Brush
	applied         bool
	dark            bool
	motion          bool
	highContrast    bool
	controlsThemed  bool
	requestedTheme  string
	requestedMotion bool
}

func newDialogThemeSession(dialog *walk.Dialog) *dialogThemeSession {
	return &dialogThemeSession{dialog: dialog}
}

// ThemeSession lets embedded native pages share the donor's complete control,
// brush, accessibility and caption lifecycle instead of theming only HWNDs.
type ThemeSession = dialogThemeSession

func NewThemeSession(dialog *walk.Dialog) *ThemeSession {
	return newDialogThemeSession(dialog)
}

// Refresh also themes controls created since the previous Apply call.
func (session *dialogThemeSession) Refresh(theme string, motion bool) bool {
	session.controlsThemed = false
	return session.Apply(theme, motion)
}

func (session *dialogThemeSession) Apply(
	theme string,
	motion ...bool,
) bool {
	return session.apply(theme, true, motion...)
}

// ApplyDeferred changes native controls and Walk brushes without publishing
// the DWM caption yet. Settings uses this while it also refreshes its retained
// tab surfaces and hotkey recorder, then calls Commit once for the complete
// hierarchy.
func (session *dialogThemeSession) ApplyDeferred(
	theme string,
	motion ...bool,
) bool {
	return session.apply(theme, false, motion...)
}

func (session *dialogThemeSession) apply(
	theme string,
	commit bool,
	motion ...bool,
) bool {
	if session == nil || session.dialog == nil {
		return false
	}
	animate := true
	if len(motion) > 0 {
		animate = motion[0]
	}
	session.requestedTheme = theme
	session.requestedMotion = animate
	appearance, _ := winfx.ResolveAppearance(theme)
	dark := appearance.Dark
	animate = animate && appearance.MotionEnabled
	if session.applied &&
		session.controlsThemed &&
		session.dark == dark &&
		session.highContrast == appearance.HighContrast {
		if session.motion != animate {
			if err := winfx.ApplyDialogMotion(
				uintptr(session.dialog.Handle()),
				animate,
			); err == nil {
				session.motion = animate
			} else {
				return false
			}
		}
		if commit {
			if err := session.Commit(); err != nil {
				return false
			}
		}
		return true
	}
	requestedDark := dark
	previousBrushes := session.brushes
	session.brushes = nil
	native, _ := winfx.ApplyDialogClientThemeWithMotionResult(
		uintptr(session.dialog.Handle()),
		dark,
		animate,
	)
	highContrast := native.HighContrast
	if highContrast {
		dark = false
		session.applyPalette(nil, nil, nil, 0, false)
	} else {
		tokens := design.Default.Colors.Light
		if dark {
			tokens = design.Default.Colors.Dark
		}
		surfaceColor := dialogWalkColor(tokens.Surface)
		inputColor := dialogWalkColor(tokens.SurfaceHigh)
		textColor := dialogWalkColor(tokens.Text)
		material, materialErr := newDialogMaterialBrush(dark)
		surface, surfaceErr := walk.NewSolidColorBrush(surfaceColor)
		input, inputErr := walk.NewSolidColorBrush(inputColor)
		if materialErr == nil && surfaceErr == nil && inputErr == nil {
			session.brushes = append(
				session.brushes,
				material,
				surface,
				input,
			)
			session.applyPalette(
				material,
				surface,
				input,
				textColor,
				true,
			)
		} else {
			if material != nil {
				material.Dispose()
			}
			if surface != nil {
				surface.Dispose()
			}
			if input != nil {
				input.Dispose()
			}
			session.applyPalette(nil, nil, nil, 0, false)
		}
	}
	for _, brush := range previousBrushes {
		if brush != nil {
			brush.Dispose()
		}
	}
	session.applied = true
	session.dark = requestedDark
	session.motion = animate
	session.highContrast = native.HighContrast
	session.controlsThemed = native.ControlsThemed
	if commit {
		if err := session.Commit(); err != nil {
			return false
		}
	}
	return native.ControlsThemed
}

// Commit synchronously presents the fully themed dialog hierarchy. This is
// intentionally separate from Apply so Settings can update its retained tab
// surfaces and hotkey recorder chrome, then perform one final atomic-looking
// native paint before returning to the message loop.
func (session *dialogThemeSession) Commit() error {
	if session == nil || session.dialog == nil ||
		session.dialog.IsDisposed() {
		return winfx.ErrInvalidWindow
	}
	return winfx.CommitDialogThemeFrame(
		uintptr(session.dialog.Handle()),
		session.dark && !session.highContrast,
	)
}

// WatchSystemChanges makes a modeless dialog follow live Windows appearance
// and accessibility policy. Apply refreshes native chrome and the Walk
// brush/text palette; afterApply lets Settings refresh its retained page
// compositor and custom hotkey surfaces in the same UI dispatch.
func (session *dialogThemeSession) WatchSystemChanges(
	afterApply func(),
) error {
	if session == nil || session.dialog == nil {
		return winfx.ErrInvalidWindow
	}
	return winfx.SetDialogThemeAccessibilityHandler(
		uintptr(session.dialog.Handle()),
		func() bool {
			applied := session.ApplyDeferred(
				session.requestedTheme,
				session.requestedMotion,
			)
			if afterApply != nil {
				afterApply()
			}
			return applied && session.Commit() == nil
		},
	)
}

func newDialogMaterialBrush(dark bool) (walk.Brush, error) {
	stops := dialogMaterialStops(dark)
	return walk.NewVerticalGradientBrush(stops)
}

func dialogMaterialStops(dark bool) []walk.GradientStop {
	if dark {
		tokens := design.Default.Colors.Dark
		return []walk.GradientStop{
			{Offset: 0, Color: dialogWalkColor(tokens.SurfaceHigh)},
			{Offset: 0.42, Color: dialogWalkColor(tokens.Surface)},
			{Offset: 1, Color: dialogWalkColor(tokens.CanvasAlt)},
		}
	}
	tokens := design.Default.Colors.Light
	return []walk.GradientStop{
		{Offset: 0, Color: dialogWalkColor(tokens.Surface)},
		{Offset: 0.42, Color: dialogWalkColor(tokens.CanvasAlt)},
		{Offset: 1, Color: dialogWalkColor(tokens.Canvas)},
	}
}

func dialogWalkColor(value string) walk.Color {
	color := design.MustColor(value)
	return walk.RGB(color.R, color.G, color.B)
}

func (session *dialogThemeSession) Dispose() {
	if session == nil {
		return
	}
	if session.dialog != nil {
		_ = winfx.SetDialogThemeAccessibilityHandler(
			uintptr(session.dialog.Handle()),
			nil,
		)
		session.applyPalette(nil, nil, nil, 0, false)
	}
	for _, brush := range session.brushes {
		if brush != nil {
			brush.Dispose()
		}
	}
	session.brushes = nil
	session.dialog = nil
	session.applied = false
}

func (session *dialogThemeSession) applyPalette(
	material walk.Brush,
	surface walk.Brush,
	input walk.Brush,
	text walk.Color,
	materialContinuity bool,
) {
	if session == nil || session.dialog == nil {
		return
	}
	session.dialog.SetBackground(material)
	walkDialogWidgets(session.dialog, func(widget walk.Widget) {
		if table, ok := widget.(*walk.TableView); ok {
			if materialContinuity {
				tokens := design.Default.Colors.Light
				if session.requestedTheme == "dark" || session.dark {
					tokens = design.Default.Colors.Dark
				}
				// Use the resolved brushes/text, including system-theme changes.
				background := dialogWalkColor(tokens.Surface)
				if solid, ok := surface.(*walk.SolidColorBrush); ok {
					background = solid.Color()
				}
				selected := dialogWalkColor(tokens.SurfaceHigh)
				if solid, ok := input.(*walk.SolidColorBrush); ok {
					selected = solid.Color()
				}
				table.SetPresentationColors(&walk.TableViewColors{Background: background, Text: text, SelectedBackground: selected, SelectedText: text})
			} else {
				table.SetPresentationColors(nil)
			}
		}
		switch widget.(type) {
		case *walk.Composite, *walk.TabWidget:
			if materialContinuity {
				widget.AsWindowBase().SetBackground(walk.NullBrush())
			} else {
				widget.AsWindowBase().SetBackground(material)
			}
		case *walk.PushButton:
			if materialContinuity {
				// The dialog button subclass asks its parent for this brush
				// before drawing the rounded body, preserving the continuous
				// root material in the transparent corner pixels.
				widget.AsWindowBase().SetBackground(walk.NullBrush())
			} else {
				widget.AsWindowBase().SetBackground(surface)
			}
		case *walk.GroupBox, *walk.TabPage, *walk.CheckBox, *walk.RadioButton:
			if materialContinuity {
				// Null child brushes inherit the one real root gradient.
				// Walk aligns the inherited brush origin to the ancestor,
				// preserving a continuous material through nested HWNDs.
				widget.AsWindowBase().SetBackground(walk.NullBrush())
			} else {
				widget.AsWindowBase().SetBackground(surface)
			}
		case *walk.LineEdit, *walk.NumberEdit, *walk.TextEdit:
			if setter, ok := widget.(interface{ SetBackground(walk.Brush) }); ok {
				setter.SetBackground(input)
			}
		}
		if setter, ok := widget.(interface{ SetTextColor(walk.Color) }); ok {
			setter.SetTextColor(text)
		}
		widget.Invalidate()
	})
	session.dialog.Invalidate()
}

func walkDialogWidgets(container walk.Container, visit func(walk.Widget)) {
	if container == nil || visit == nil {
		return
	}
	children := container.Children()
	for index := 0; index < children.Len(); index++ {
		child := children.At(index)
		visit(child)
		if tabs, ok := child.(*walk.TabWidget); ok {
			for pageIndex := 0; pageIndex < tabs.Pages().Len(); pageIndex++ {
				page := tabs.Pages().At(pageIndex)
				visit(page)
				walkDialogWidgets(page, visit)
			}
		}
		if nested, ok := child.(walk.Container); ok {
			walkDialogWidgets(nested, visit)
		}
	}
}

func compactTime(value time.Time) string {
	if value.IsZero() {
		return "Never"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func displayBool(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}

func cleanDisplayText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.ReplaceAll(value, "\x00", "␀")
	value = strings.TrimSpace(value)
	if limit > 0 {
		runes := []rune(value)
		if len(runes) > limit {
			value = string(runes[:limit]) + "…"
		}
	}
	return value
}
