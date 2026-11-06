//go:build windows

package dialogs

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/design"
	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/winfx"
)

const (
	hotkeyRecorderSubclassBase  uintptr = 0x524C4800
	wmHotkeyRecorderFocusCheck  uint32  = 0x8000 + 0x524
	hotkeyRecorderVisualTimerID uintptr = 0x5248
)

type hotkeyRecorderMessageKind uint8

const (
	hotkeyRecorderMessageNormal hotkeyRecorderMessageKind = iota
	hotkeyRecorderMessageWarning
	hotkeyRecorderMessageError
)

// hotkeyRecorderGroup coordinates the four bindings as one capture session.
// Only one surface records at a time, which also gives settings preview a
// single authoritative "global hotkeys are temporarily suspended" state.
type hotkeyRecorderGroup struct {
	bindings          []*hotkeyRecorderBinding
	active            *hotkeyRecorderBinding
	onChanged         func()
	onCaptureChanged  func(bool)
	availabilityCheck func(action string, chord string) error
	keyboardCapture   *winfx.KeyboardCapture
	capturing         bool
	dark              bool
	contrast          bool
	motion            bool
	attached          bool
	localizer         i18n.Localizer
}

type hotkeyRecorderBinding struct {
	group       *hotkeyRecorderGroup
	action      string
	description string
	chord       string
	stableChord string
	recorder    winfx.ChordRecorder

	surface      *walk.CustomWidget
	recordButton *walk.PushButton
	clearButton  *walk.PushButton
	statusLabel  *walk.Label

	subclassID   uintptr
	hovered      bool
	tracking     bool
	visualLevel  float64
	visualTarget float64
	visualTick   time.Time
	visualTimer  bool

	message     string
	messageKind hotkeyRecorderMessageKind
	captureHint string

	availabilityChord   string
	availabilityMessage string
}

func newHotkeyRecorderGroup() *hotkeyRecorderGroup {
	return &hotkeyRecorderGroup{
		motion:    true,
		localizer: i18n.New(i18n.English),
	}
}

func (group *hotkeyRecorderGroup) Add(
	action string,
	description string,
	chord string,
) *hotkeyRecorderBinding {
	binding := &hotkeyRecorderBinding{
		group:       group,
		action:      action,
		description: description,
		chord:       strings.TrimSpace(chord),
	}
	if binding.chord != "" {
		if parsed, err := winfx.ParseHotkey(binding.chord); err == nil {
			binding.chord = parsed.String()
		}
	}
	binding.stableChord = binding.chord
	group.bindings = append(group.bindings, binding)
	return binding
}

func (group *hotkeyRecorderGroup) SetLocalizer(localizer i18n.Localizer) {
	if group == nil {
		return
	}
	group.localizer = localizer
	group.RefreshValidation()
	for _, binding := range group.bindings {
		binding.updateControls()
		binding.invalidate()
	}
}

func (binding *hotkeyRecorderBinding) Declaration() decl.Widget {
	recordCaption, clearCaption := hotkeyRecorderButtonCaptions(binding.action)
	return decl.Composite{
		MinSize: decl.Size{Height: 66},
		Layout: decl.Grid{
			Columns: 4,
			Margins: decl.Margins{
				Left: 8, Top: 6, Right: 8, Bottom: 6,
			},
			Spacing: 8,
		},
		Children: []decl.Widget{
			decl.Composite{
				Row:       0,
				Column:    0,
				RowSpan:   2,
				MinSize:   decl.Size{Width: 140},
				Alignment: decl.AlignHNearVCenter,
				Layout: decl.VBox{
					MarginsZero: true,
					Spacing:     1,
				},
				Children: []decl.Widget{
					decl.Label{
						Text: binding.action,
						Font: decl.Font{
							Family:    "Segoe UI",
							PointSize: 10,
							Bold:      true,
						},
					},
					decl.Label{Text: binding.description},
				},
			},
			decl.CustomWidget{
				AssignTo:            &binding.surface,
				Row:                 0,
				Column:              1,
				MinSize:             decl.Size{Width: 225, Height: 38},
				StretchFactor:       1,
				Style:               win.WS_TABSTOP,
				PaintPixels:         binding.paint,
				PaintMode:           decl.PaintNormal,
				InvalidatesOnResize: true,
				Name:                binding.action + " shortcut recorder",
				ToolTipText:         "Select this surface, then press the complete keyboard shortcut.",
				Accessibility: decl.Accessibility{
					Name:          binding.action + " shortcut: " + binding.accessibleChord(),
					Description:   "Keyboard shortcut recorder. Press Enter or Space to begin recording.",
					DefaultAction: "Record shortcut",
					Role:          decl.AccRoleHotkeyfield,
					State:         decl.AccStateFocusable,
				},
				OnMouseDown: func(_ int, _ int, button walk.MouseButton) {
					if button == walk.LeftButton && binding.group != nil {
						binding.group.Start(binding)
					}
				},
			},
			decl.Label{
				AssignTo:    &binding.statusLabel,
				Row:         1,
				Column:      1,
				Text:        "Ready",
				ToolTipText: "Recorder and conflict status",
			},
			decl.PushButton{
				AssignTo:  &binding.recordButton,
				Row:       0,
				Column:    2,
				RowSpan:   2,
				MinSize:   decl.Size{Width: 132},
				Text:      recordCaption,
				OnClicked: func() { binding.group.Start(binding) },
			},
			decl.PushButton{
				AssignTo:  &binding.clearButton,
				Row:       0,
				Column:    3,
				RowSpan:   2,
				MinSize:   decl.Size{Width: 104},
				Text:      clearCaption,
				OnClicked: func() { binding.group.Clear(binding) },
			},
		},
	}
}

func (binding *hotkeyRecorderBinding) Chord() string {
	if binding == nil {
		return ""
	}
	return binding.chord
}

func (binding *hotkeyRecorderBinding) SetChord(chord string) {
	if binding == nil {
		return
	}
	chord = strings.TrimSpace(chord)
	if chord != "" {
		if parsed, err := winfx.ParseHotkey(chord); err == nil {
			chord = parsed.String()
		}
	}
	if chord != binding.availabilityChord {
		binding.availabilityChord = ""
		binding.availabilityMessage = ""
	}
	binding.chord = chord
	binding.captureHint = ""
	binding.invalidate()
}

func (group *hotkeyRecorderGroup) SetOnChanged(changed func()) {
	if group == nil {
		return
	}
	group.onChanged = changed
	group.RefreshValidation()
	group.rememberStableIfValid()
}

func (group *hotkeyRecorderGroup) SetAvailabilityCheck(
	check func(action string, chord string) error,
) {
	if group != nil {
		group.availabilityCheck = check
	}
}

func (group *hotkeyRecorderGroup) SetOnCaptureChanged(
	changed func(bool),
) {
	if group == nil {
		return
	}
	group.onCaptureChanged = changed
	if changed != nil && group.capturing {
		changed(true)
	}
}

func (group *hotkeyRecorderGroup) Active() bool {
	return group != nil && group.active != nil &&
		group.active.recorder.Active()
}

// PreviewChords returns the chord set safe to publish as configuration.
// Runtime capture suspension is a separate signal so web tabs never see a
// transient "all bindings cleared" preview. A rejected completion (for
// example F12 or a duplicate) returns the last fully valid set.
func (group *hotkeyRecorderGroup) PreviewChords() []string {
	if group == nil {
		return nil
	}
	result := make([]string, len(group.bindings))
	useStable := group.hasValidationError()
	for index, binding := range group.bindings {
		if useStable {
			result[index] = binding.stableChord
		} else {
			result[index] = binding.chord
		}
	}
	return result
}

func (group *hotkeyRecorderGroup) Start(binding *hotkeyRecorderBinding) {
	if group == nil || binding == nil || binding.group != group {
		return
	}
	if group.active == binding && binding.recorder.Active() {
		group.CancelActive(true)
		return
	}
	if group.active != nil {
		group.closeKeyboardCapture()
		previous := group.active
		update := previous.recorder.Cancel()
		previous.chord = update.Chord
		previous.captureHint = ""
		group.active = nil
	}
	if _, err := binding.recorder.Start(binding.chord); err != nil {
		binding.message = "Replace the invalid value by recording a new shortcut."
		binding.messageKind = hotkeyRecorderMessageError
		binding.updateControls()
		return
	}

	group.active = binding
	binding.captureHint = "Listening — press a key with any modifiers · Esc cancels"
	group.RefreshValidation()
	group.setCapturing(true)
	captureErr := group.startKeyboardCapture(binding)
	for _, virtualKey := range winfx.SampleKeyboardModifiers().VirtualKeys() {
		group.handleUpdate(
			binding,
			binding.recorder.KeyDown(virtualKey),
		)
	}
	if captureErr != nil {
		// The focused-window subclass remains a safe fallback for ordinary
		// chords. Surface the degraded scope instead of silently promising
		// that Windows-owned combinations can be intercepted.
		binding.captureHint = "Standard-key capture active — Windows system-key interception is unavailable"
		binding.updateControls()
	}
	if binding.surface != nil {
		_ = binding.surface.SetFocus()
	}
}

func (group *hotkeyRecorderGroup) Clear(binding *hotkeyRecorderBinding) {
	if group == nil || binding == nil || binding.group != group {
		return
	}
	if group.active != nil && group.active != binding {
		group.CancelActive(false)
	}
	binding.recorder.Clear()
	binding.chord = ""
	binding.availabilityChord = ""
	binding.availabilityMessage = ""
	binding.captureHint = ""
	wasCapturing := group.capturing
	if group.active == binding {
		group.active = nil
	}
	group.RefreshValidation()
	group.rememberStableIfValid()
	group.notifyChanged()
	if wasCapturing {
		group.setCapturing(false)
	}
}

func (group *hotkeyRecorderGroup) CancelActive(_ bool) {
	if group == nil || group.active == nil {
		return
	}
	binding := group.active
	update := binding.recorder.Cancel()
	binding.chord = update.Chord
	binding.captureHint = ""
	group.active = nil
	group.RefreshValidation()
	group.rememberStableIfValid()
	group.setCapturing(false)
}

func (group *hotkeyRecorderGroup) handleUpdate(
	binding *hotkeyRecorderBinding,
	update winfx.RecorderUpdate,
) {
	if group == nil || binding == nil || group.active != binding {
		return
	}

	switch update.Action {
	case winfx.RecorderRecording:
		binding.captureHint = "Listening — press the main key · Esc cancels"
		binding.updateControls()
		binding.invalidate()
	case winfx.RecorderRejected:
		binding.captureHint = "That key cannot be registered. Try another key."
		binding.updateControls()
		binding.invalidate()
	case winfx.RecorderCompleted:
		binding.chord = update.Chord
		binding.captureHint = ""
		// A fresh recording is an explicit retry. Clear a cached external
		// ownership failure even when the user recorded the same chord, then
		// probe again while the shell's own registrations remain suspended.
		binding.availabilityChord = ""
		binding.availabilityMessage = ""
		group.active = nil
		group.RefreshValidation()
		group.validateAvailability()
		group.rememberStableIfValid()
		group.notifyChanged()
		group.setCapturing(false)
	case winfx.RecorderCancelled:
		binding.chord = update.Chord
		binding.captureHint = ""
		group.active = nil
		group.RefreshValidation()
		group.rememberStableIfValid()
		group.setCapturing(false)
	case winfx.RecorderCleared:
		binding.chord = ""
		binding.captureHint = ""
		group.active = nil
		group.RefreshValidation()
		group.rememberStableIfValid()
		group.notifyChanged()
		group.setCapturing(false)
	}
}

func (group *hotkeyRecorderGroup) notifyChanged() {
	if group != nil && group.onChanged != nil {
		group.onChanged()
	}
}

func (group *hotkeyRecorderGroup) setCapturing(active bool) {
	if group == nil || group.capturing == active {
		return
	}
	if !active {
		group.closeKeyboardCapture()
	}
	group.capturing = active
	if group.onCaptureChanged != nil {
		group.onCaptureChanged(active)
	}
}

func (group *hotkeyRecorderGroup) startKeyboardCapture(
	binding *hotkeyRecorderBinding,
) error {
	if group == nil || binding == nil {
		return errors.New("hotkey recorder capture is unavailable")
	}
	group.closeKeyboardCapture()
	capture, err := winfx.StartKeyboardCapture(
		winfx.KeyboardCaptureOptions{},
		func(event winfx.KeyboardCaptureEvent) {
			if group.active != binding || !binding.recorder.Active() {
				return
			}
			if event.KeyDown {
				if event.Repeat {
					return
				}
				group.handleUpdate(
					binding,
					binding.recorder.KeyDown(event.VirtualKey),
				)
				return
			}
			if event.KeyUp {
				group.handleUpdate(
					binding,
					binding.recorder.KeyUp(event.VirtualKey),
				)
			}
		},
	)
	if err != nil {
		return err
	}
	group.keyboardCapture = capture
	return nil
}

func (group *hotkeyRecorderGroup) closeKeyboardCapture() {
	if group == nil || group.keyboardCapture == nil {
		return
	}
	capture := group.keyboardCapture
	group.keyboardCapture = nil
	if err := capture.Close(); err != nil {
		// Close leaves a failed hook in pass-through mode. Retain the handle so
		// the next recorder start or dialog disposal can retry the unhook
		// instead of leaking the only process-wide capture slot.
		group.keyboardCapture = capture
	}
}

func (group *hotkeyRecorderGroup) Validate() error {
	if group == nil {
		return nil
	}
	if group.Active() {
		return errors.New("finish or cancel the shortcut currently being recorded")
	}
	group.RefreshValidation()
	for _, binding := range group.bindings {
		if binding.messageKind == hotkeyRecorderMessageError {
			return fmt.Errorf("%s: %s", binding.action, binding.message)
		}
	}
	return nil
}

func (group *hotkeyRecorderGroup) RefreshValidation() {
	if group == nil {
		return
	}
	type chordOwner struct {
		binding *hotkeyRecorderBinding
		key     string
	}
	owners := make(map[string]chordOwner)

	for _, binding := range group.bindings {
		if binding.availabilityChord != binding.chord {
			binding.availabilityChord = ""
			binding.availabilityMessage = ""
		}
		binding.message = ""
		binding.messageKind = hotkeyRecorderMessageNormal
		if binding.chord == "" {
			continue
		}
		parsed, err := winfx.ParseHotkey(binding.chord)
		if err != nil {
			binding.message = "Unsupported shortcut — select Record to replace it."
			binding.messageKind = hotkeyRecorderMessageError
			continue
		}
		if parsed.Key == 0x7B { // VK_F12 is reserved for the debugger.
			binding.message = "F12 is reserved by Windows."
			binding.messageKind = hotkeyRecorderMessageError
			continue
		}
		if parsed.Modifiers == 0 ||
			parsed.Modifiers == winfx.ModShift {
			binding.message = "System-wide single-key shortcut — use carefully."
			binding.messageKind = hotkeyRecorderMessageWarning
		}

		key := fmt.Sprintf("%d:%d", parsed.Modifiers, parsed.Key)
		if owner, found := owners[key]; found {
			if group.localizer.IsRTL() {
				binding.message = "با «" +
					dialogText(group.localizer, owner.binding.action) +
					"» تداخل دارد."
			} else {
				binding.message = "Conflicts with " + owner.binding.action + "."
			}
			binding.messageKind = hotkeyRecorderMessageError
			if group.localizer.IsRTL() {
				owner.binding.message = "با «" +
					dialogText(group.localizer, binding.action) +
					"» تداخل دارد."
			} else {
				owner.binding.message = "Conflicts with " + binding.action + "."
			}
			owner.binding.messageKind = hotkeyRecorderMessageError
		} else {
			owners[key] = chordOwner{binding: binding, key: key}
		}
	}
	for _, binding := range group.bindings {
		if binding.messageKind != hotkeyRecorderMessageError &&
			binding.availabilityMessage != "" &&
			binding.availabilityChord == binding.chord {
			binding.message = binding.availabilityMessage
			binding.messageKind = hotkeyRecorderMessageError
		}
	}
	for _, binding := range group.bindings {
		binding.updateControls()
		binding.invalidate()
	}
}

func (group *hotkeyRecorderGroup) rememberStableIfValid() {
	if group == nil || group.Active() || group.hasValidationError() {
		return
	}
	for _, binding := range group.bindings {
		binding.stableChord = binding.chord
	}
}

func (group *hotkeyRecorderGroup) hasValidationError() bool {
	if group == nil {
		return false
	}
	for _, binding := range group.bindings {
		if binding.messageKind == hotkeyRecorderMessageError {
			return true
		}
	}
	return false
}

func (group *hotkeyRecorderGroup) validateAvailability() {
	if group == nil || group.availabilityCheck == nil ||
		group.hasStructuralValidationError() {
		return
	}
	for _, binding := range group.bindings {
		binding.availabilityChord = ""
		binding.availabilityMessage = ""
		if binding.chord == "" {
			continue
		}
		if err := group.availabilityCheck(
			binding.action,
			binding.chord,
		); err != nil {
			binding.availabilityChord = binding.chord
			binding.availabilityMessage = "Unavailable — Windows or another app already owns this shortcut."
			binding.message = binding.availabilityMessage
			binding.messageKind = hotkeyRecorderMessageError
		}
	}
	for _, binding := range group.bindings {
		binding.updateControls()
		binding.invalidate()
	}
}

func (group *hotkeyRecorderGroup) hasStructuralValidationError() bool {
	if group == nil {
		return false
	}
	for _, binding := range group.bindings {
		if binding.messageKind != hotkeyRecorderMessageError {
			continue
		}
		if binding.availabilityMessage != "" &&
			binding.availabilityChord == binding.chord &&
			binding.message == binding.availabilityMessage {
			continue
		}
		return true
	}
	return false
}

func (group *hotkeyRecorderGroup) SetTheme(
	dark bool,
	highContrast bool,
	motion ...bool,
) {
	if group == nil {
		return
	}
	group.dark = dark
	group.contrast = highContrast
	if len(motion) > 0 {
		group.motion = motion[0]
	}
	for _, binding := range group.bindings {
		binding.updateControls()
		binding.invalidate()
	}
}

func (group *hotkeyRecorderGroup) Attach() error {
	if group == nil || group.attached {
		return nil
	}
	for index, binding := range group.bindings {
		if binding.surface == nil {
			group.Dispose()
			return fmt.Errorf("%s recorder surface is unavailable", binding.action)
		}
		// NullBrush tells Walk to inherit and origin-align the dialog's real
		// material brush for WM_ERASEBKGND. Rounded corners therefore expose
		// live acrylic/gradient content without a bitmap snapshot or a
		// COLOR_BTNFACE flash.
		binding.surface.SetBackground(walk.NullBrush())
		binding.subclassID = hotkeyRecorderSubclassBase + uintptr(index)
		if err := winfx.SetWindowSubclass(
			uintptr(binding.surface.Handle()),
			binding.subclassID,
			binding.windowMessage,
		); err != nil {
			group.Dispose()
			return fmt.Errorf("%s: %w", binding.action, err)
		}
		binding.surface.SetCursor(walk.CursorHand())
	}
	group.attached = true
	group.RefreshValidation()
	return nil
}

func (group *hotkeyRecorderGroup) Dispose() {
	if group == nil {
		return
	}
	group.CancelActive(false)
	for _, binding := range group.bindings {
		if binding.surface != nil && binding.subclassID != 0 {
			win.KillTimer(
				binding.surface.Handle(),
				hotkeyRecorderVisualTimerID,
			)
			_ = winfx.RemoveWindowSubclass(
				uintptr(binding.surface.Handle()),
				binding.subclassID,
			)
		}
		binding.subclassID = 0
		binding.visualTimer = false
		binding.surface = nil
		binding.recordButton = nil
		binding.clearButton = nil
		binding.statusLabel = nil
	}
	group.attached = false
	group.onChanged = nil
	group.onCaptureChanged = nil
	group.availabilityCheck = nil
	group.closeKeyboardCapture()
	group.capturing = false
}

func (binding *hotkeyRecorderBinding) windowMessage(
	hwnd uintptr,
	message uintptr,
	wParam uintptr,
	lParam uintptr,
) (uintptr, bool) {
	active := binding != nil && binding.group != nil &&
		binding.group.active == binding && binding.recorder.Active()
	switch uint32(message) {
	case win.WM_TIMER:
		if wParam == hotkeyRecorderVisualTimerID {
			binding.advanceVisualTransition()
			return 0, true
		}
	case win.WM_GETDLGCODE:
		if active || wParam == uintptr(win.VK_RETURN) ||
			wParam == uintptr(win.VK_SPACE) {
			return win.DLGC_WANTALLKEYS, true
		}
	case win.WM_KEYDOWN, win.WM_SYSKEYDOWN:
		if !active {
			if uint32(wParam) == win.VK_RETURN ||
				uint32(wParam) == win.VK_SPACE {
				binding.group.Start(binding)
				return 0, true
			}
			break
		}
		// Bit 30 indicates an auto-repeat. The first transition already
		// updated the recorder, so repeats should be silent and harmless.
		if lParam&(uintptr(1)<<30) == 0 {
			binding.group.handleUpdate(
				binding,
				binding.recorder.KeyDown(uint32(wParam)),
			)
		}
		return 0, true
	case win.WM_KEYUP, win.WM_SYSKEYUP:
		if active {
			binding.group.handleUpdate(
				binding,
				binding.recorder.KeyUp(uint32(wParam)),
			)
			return 0, true
		}
	case win.WM_CHAR, win.WM_DEADCHAR, win.WM_SYSCHAR, win.WM_SYSDEADCHAR:
		if active {
			// Prevent default-button activation, accelerators, and system
			// beeps while raw virtual-key messages are being learned.
			return 0, true
		}
	case win.WM_APPCOMMAND:
		if active {
			if virtualKey, ok := virtualKeyForAppCommand(lParam); ok {
				binding.group.handleUpdate(
					binding,
					binding.recorder.KeyDown(virtualKey),
				)
				return 1, true
			}
		}
	case win.WM_MOUSEMOVE:
		if !binding.hovered {
			binding.hovered = true
			binding.invalidate()
		}
		if !binding.tracking {
			event := win.TRACKMOUSEEVENT{
				CbSize:    uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})),
				DwFlags:   win.TME_LEAVE,
				HwndTrack: win.HWND(hwnd),
			}
			binding.tracking = win.TrackMouseEvent(&event)
		}
	case win.WM_MOUSELEAVE:
		binding.hovered = false
		binding.tracking = false
		binding.invalidate()
		return 0, true
	case win.WM_SETFOCUS:
		binding.invalidate()
	case win.WM_KILLFOCUS:
		binding.invalidate()
		if active {
			// Defer until the focus target is established. This lets Record
			// and Clear button clicks finish before deciding whether capture
			// genuinely left the recorder row.
			win.PostMessage(
				win.HWND(hwnd),
				wmHotkeyRecorderFocusCheck,
				0,
				0,
			)
		}
	case wmHotkeyRecorderFocusCheck:
		if active && !binding.focusRemainsInRecorder() {
			binding.group.CancelActive(true)
		}
		return 0, true
	}
	return 0, false
}

func (binding *hotkeyRecorderBinding) focusRemainsInRecorder() bool {
	if binding == nil {
		return false
	}
	focus := win.GetFocus()
	if binding.surface != nil && focus == binding.surface.Handle() {
		return true
	}
	if binding.recordButton != nil && focus == binding.recordButton.Handle() {
		return true
	}
	return binding.clearButton != nil && focus == binding.clearButton.Handle()
}

func virtualKeyForAppCommand(lParam uintptr) (uint32, bool) {
	command := uint32(lParam>>16) & 0x7FF
	virtualKeys := map[uint32]uint32{
		1:  0xA6, // APPCOMMAND_BROWSER_BACKWARD / VK_BROWSER_BACK
		2:  0xA7, // APPCOMMAND_BROWSER_FORWARD / VK_BROWSER_FORWARD
		3:  0xA8, // APPCOMMAND_BROWSER_REFRESH / VK_BROWSER_REFRESH
		4:  0xA9, // APPCOMMAND_BROWSER_STOP / VK_BROWSER_STOP
		5:  0xAA, // APPCOMMAND_BROWSER_SEARCH / VK_BROWSER_SEARCH
		6:  0xAB, // APPCOMMAND_BROWSER_FAVORITES / VK_BROWSER_FAVORITES
		7:  0xAC, // APPCOMMAND_BROWSER_HOME / VK_BROWSER_HOME
		8:  0xAD, // APPCOMMAND_VOLUME_MUTE / VK_VOLUME_MUTE
		9:  0xAE, // APPCOMMAND_VOLUME_DOWN / VK_VOLUME_DOWN
		10: 0xAF, // APPCOMMAND_VOLUME_UP / VK_VOLUME_UP
		11: 0xB0, // APPCOMMAND_MEDIA_NEXTTRACK / VK_MEDIA_NEXT_TRACK
		12: 0xB1, // APPCOMMAND_MEDIA_PREVIOUSTRACK / VK_MEDIA_PREV_TRACK
		13: 0xB2, // APPCOMMAND_MEDIA_STOP / VK_MEDIA_STOP
		14: 0xB3, // APPCOMMAND_MEDIA_PLAY_PAUSE / VK_MEDIA_PLAY_PAUSE
		15: 0xB4, // APPCOMMAND_LAUNCH_MAIL / VK_LAUNCH_MAIL
		16: 0xB5, // APPCOMMAND_LAUNCH_MEDIA_SELECT / VK_LAUNCH_MEDIA_SELECT
		17: 0xB6, // APPCOMMAND_LAUNCH_APP1 / VK_LAUNCH_APP1
		18: 0xB7, // APPCOMMAND_LAUNCH_APP2 / VK_LAUNCH_APP2
	}
	virtualKey, ok := virtualKeys[command]
	return virtualKey, ok
}

func (binding *hotkeyRecorderBinding) updateControls() {
	if binding == nil {
		return
	}
	active := binding.group != nil && binding.group.active == binding &&
		binding.recorder.Active()
	if binding.recordButton != nil {
		text, _ := hotkeyRecorderButtonCaptions(binding.action)
		text = dialogText(binding.group.localizer, text)
		tooltip := "Record a new keyboard shortcut."
		if active {
			text = "Cancel r&ecording"
			tooltip = "Cancel recording and restore the previous shortcut."
		}
		text = dialogText(binding.group.localizer, text)
		tooltip = dialogText(binding.group.localizer, tooltip)
		_ = binding.recordButton.SetText(text)
		_ = binding.recordButton.SetToolTipText(tooltip)
	}
	if binding.clearButton != nil {
		binding.clearButton.SetEnabled(binding.chord != "" || active)
	}
	if binding.statusLabel != nil {
		text := binding.message
		kind := binding.messageKind
		if active {
			text = binding.captureHint
			kind = hotkeyRecorderMessageNormal
		} else if text == "" && binding.chord == "" {
			text = "Disabled — record a shortcut to enable"
		} else if text == "" {
			text = "Ready"
		}
		text = dialogText(binding.group.localizer, text)
		_ = binding.statusLabel.SetText(text)
		_ = binding.statusLabel.SetToolTipText(text)
		binding.statusLabel.SetTextColor(binding.statusColor(kind))
	}
	if binding.surface != nil {
		action := dialogText(binding.group.localizer, binding.action)
		chord := binding.accessibleChord()
		name := action + " shortcut: " + chord
		description := "Press Enter or Space to record. Escape cancels; unmodified Backspace or Delete clears."
		if binding.group.localizer.IsRTL() {
			name = action + "؛ میان‌بر: " + chord
			description = "برای ضبط Enter یا Space را بزنید. Escape لغو می‌کند؛ Backspace یا Delete بدون تغییردهنده میان‌بر را پاک می‌کند."
		}
		_ = binding.surface.Accessibility().SetName(name)
		if binding.messageKind == hotkeyRecorderMessageError {
			description += " " + dialogText(
				binding.group.localizer,
				binding.message,
			)
		}
		_ = binding.surface.Accessibility().SetDescription(description)
	}
}

// hotkeyRecorderButtonCaptions gives every command on the Hotkeys page a
// deterministic accelerator. Reusing Alt+R/Alt+C on all four rows made
// keyboard invocation depend on whichever native child Walk happened to
// enumerate first.
func hotkeyRecorderButtonCaptions(action string) (record, clear string) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "toggle device":
		return "Record &toggle", "Clear to&ggle"
	case "turn on":
		return "Record o&n", "Clea&r on"
	case "turn off":
		return "Record of&f", "Clear &off"
	case "show window":
		return "Record sho&w", "C&lear show"
	default:
		return "&Record", "C&lear"
	}
}

func (binding *hotkeyRecorderBinding) accessibleChord() string {
	if binding == nil {
		return "not assigned"
	}
	if binding.group != nil && binding.group.active == binding &&
		binding.recorder.Active() {
		display := strings.TrimSpace(binding.recorder.Display())
		if display == "" {
			return "recording"
		}
		return "recording " + strings.ReplaceAll(display, "...", "…")
	}
	if binding.chord == "" {
		return "not assigned"
	}
	return binding.chord
}

func (binding *hotkeyRecorderBinding) statusColor(
	kind hotkeyRecorderMessageKind,
) walk.Color {
	if binding != nil && binding.group != nil && binding.group.contrast {
		if kind == hotkeyRecorderMessageError {
			return walk.Color(win.GetSysColor(win.COLOR_HOTLIGHT))
		}
		return walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT))
	}
	palette := design.Default.Colors.Light
	if binding != nil && binding.group != nil && binding.group.dark {
		palette = design.Default.Colors.Dark
	}
	switch kind {
	case hotkeyRecorderMessageWarning:
		return dialogWalkColor(palette.Warm)
	case hotkeyRecorderMessageError:
		return dialogWalkColor(palette.Danger)
	default:
		return dialogWalkColor(palette.TextMuted)
	}
}

type hotkeyRecorderPalette struct {
	surface   walk.Color
	elevated  walk.Color
	key       walk.Color
	text      walk.Color
	muted     walk.Color
	stroke    walk.Color
	highlight walk.Color
	accent    walk.Color
	shadow    walk.Color
}

func (binding *hotkeyRecorderBinding) palette() hotkeyRecorderPalette {
	if binding != nil && binding.group != nil && binding.group.contrast {
		return hotkeyRecorderPalette{
			surface:   walk.Color(win.GetSysColor(win.COLOR_WINDOW)),
			elevated:  walk.Color(win.GetSysColor(win.COLOR_BTNFACE)),
			key:       walk.Color(win.GetSysColor(win.COLOR_WINDOW)),
			text:      walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT)),
			muted:     walk.Color(win.GetSysColor(win.COLOR_GRAYTEXT)),
			stroke:    walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT)),
			highlight: walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHTTEXT)),
			accent:    walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHT)),
			shadow:    walk.Color(win.GetSysColor(win.COLOR_3DSHADOW)),
		}
	}
	palette := design.Default.Colors.Light
	if binding != nil && binding.group != nil && binding.group.dark {
		palette = design.Default.Colors.Dark
	}
	return hotkeyRecorderPalette{
		surface:   dialogWalkColor(palette.Surface),
		elevated:  dialogWalkColor(palette.SurfaceHigh),
		key:       dialogWalkColor(palette.SurfaceOverlay),
		text:      dialogWalkColor(palette.Text),
		muted:     dialogWalkColor(palette.TextMuted),
		stroke:    dialogWalkColor(palette.Stroke),
		highlight: dialogWalkColor(palette.StrokeHighlight),
		accent:    dialogWalkColor(palette.Accent),
		shadow:    dialogWalkColor(palette.Shadow),
	}
}

func (binding *hotkeyRecorderBinding) paint(
	canvas *walk.Canvas,
	_ walk.Rectangle,
) error {
	if binding == nil || binding.surface == nil {
		return nil
	}
	bounds := binding.surface.ClientBoundsPixels()
	if bounds.Width < 2 || bounds.Height < 2 {
		return nil
	}
	palette := binding.palette()
	active := binding.group != nil && binding.group.active == binding &&
		binding.recorder.Active()
	focused := win.GetFocus() == binding.surface.Handle()

	shadow, err := walk.NewSolidColorBrush(palette.shadow)
	if err != nil {
		return err
	}
	defer shadow.Dispose()
	fillColor := blendRecorderColor(
		palette.surface,
		palette.elevated,
		binding.visualLevel,
	)
	body, err := walk.NewSolidColorBrush(fillColor)
	if err != nil {
		return err
	}
	defer body.Dispose()
	keyBrush, err := walk.NewSolidColorBrush(palette.key)
	if err != nil {
		return err
	}
	defer keyBrush.Dispose()

	targetBorderColor := palette.highlight
	if active {
		targetBorderColor = palette.accent
	} else if focused {
		targetBorderColor = palette.accent
	}
	borderColor := blendRecorderColor(
		palette.stroke,
		targetBorderColor,
		binding.visualLevel,
	)
	border, err := walk.NewCosmeticPen(walk.PenSolid, borderColor)
	if err != nil {
		return err
	}
	defer border.Dispose()
	keyBorder, err := walk.NewCosmeticPen(walk.PenSolid, palette.highlight)
	if err != nil {
		return err
	}
	defer keyBorder.Dispose()

	radius := maxInt(8, binding.surface.IntFrom96DPI(8))
	outer := walk.Rectangle{
		X:      1,
		Y:      1,
		Width:  bounds.Width - 2,
		Height: bounds.Height - 3,
	}
	shadowBounds := outer
	shadowBounds.Y += 2
	if !paintRecorderRounded(canvas, shadowBounds, palette.shadow, palette.shadow, radius, 0) {
		_ = canvas.FillRoundedRectanglePixels(
			shadow,
			shadowBounds,
			walk.Size{Width: radius * 2, Height: radius * 2},
		)
	}
	if !paintRecorderRounded(canvas, outer, fillColor, borderColor, radius, 1) {
		if err := canvas.FillRoundedRectanglePixels(
			body,
			outer,
			walk.Size{Width: radius * 2, Height: radius * 2},
		); err != nil {
			return err
		}
		if err := canvas.DrawRoundedRectanglePixels(
			border,
			outer,
			walk.Size{Width: radius * 2, Height: radius * 2},
		); err != nil {
			return err
		}
	}

	display := binding.chord
	if active {
		display = binding.recorder.Display()
	}
	if display == "" {
		display = "Not assigned"
	}
	display = strings.ReplaceAll(display, "...", "…")
	font := binding.surface.Font()

	left := binding.surface.IntFrom96DPI(12)
	right := binding.surface.IntFrom96DPI(10)
	available := outer.Width - left - right
	if active {
		dotSize := maxInt(6, binding.surface.IntFrom96DPI(6))
		dotBrush, dotErr := walk.NewSolidColorBrush(palette.accent)
		if dotErr == nil {
			_ = canvas.FillEllipsePixels(
				dotBrush,
				walk.Rectangle{
					X:      outer.X + left,
					Y:      outer.Y + (outer.Height-dotSize)/2,
					Width:  dotSize,
					Height: dotSize,
				},
			)
			dotBrush.Dispose()
			left += dotSize + binding.surface.IntFrom96DPI(8)
			available -= dotSize + binding.surface.IntFrom96DPI(8)
		}
	}

	parts := strings.Split(display, "+")
	if display == "Not assigned" || strings.Contains(display, "Press shortcut") {
		color := palette.muted
		if active {
			color = palette.accent
		}
		return canvas.DrawTextPixels(
			display,
			font,
			color,
			walk.Rectangle{
				X:      outer.X + left,
				Y:      outer.Y,
				Width:  available,
				Height: outer.Height,
			},
			walk.TextLeft|walk.TextVCenter|walk.TextSingleLine|
				walk.TextEndEllipsis|walk.TextNoPrefix,
		)
	}

	type measuredPart struct {
		text  string
		width int
	}
	measured := make([]measuredPart, 0, len(parts))
	total := 0
	horizontalPadding := binding.surface.IntFrom96DPI(9)
	gap := binding.surface.IntFrom96DPI(5)
	for _, part := range parts {
		textBounds, _, measureErr := canvas.MeasureTextPixels(
			part,
			font,
			walk.Rectangle{Width: 500, Height: outer.Height},
			walk.TextCalcRect|walk.TextSingleLine|walk.TextNoPrefix,
		)
		if measureErr != nil {
			return measureErr
		}
		width := textBounds.Width + horizontalPadding*2
		measured = append(measured, measuredPart{text: part, width: width})
		total += width
	}
	if len(measured) > 1 {
		total += (len(measured) - 1) * (gap*2 + 5)
	}
	if total > available {
		return binding.paintSingleChordPill(
			canvas,
			font,
			display,
			outer.X+left,
			outer,
			available,
			palette,
			shadow,
			keyBrush,
			keyBorder,
		)
	}

	x := outer.X + left
	keyHeight := minInt(
		binding.surface.IntFrom96DPI(27),
		outer.Height-binding.surface.IntFrom96DPI(8),
	)
	y := outer.Y + (outer.Height-keyHeight)/2
	keyRadius := maxInt(5, binding.surface.IntFrom96DPI(5))
	for index, part := range measured {
		if index > 0 {
			plusBounds := walk.Rectangle{
				X:      x,
				Y:      outer.Y,
				Width:  gap*2 + 5,
				Height: outer.Height,
			}
			if err := canvas.DrawTextPixels(
				"+",
				font,
				palette.muted,
				plusBounds,
				walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
					walk.TextNoPrefix,
			); err != nil {
				return err
			}
			x += plusBounds.Width
		}
		keyBounds := walk.Rectangle{
			X: x, Y: y, Width: part.width, Height: keyHeight,
		}
		keyShadow := keyBounds
		keyShadow.Y++
		if !paintRecorderRounded(canvas, keyShadow, palette.shadow, palette.shadow, keyRadius, 0) {
			_ = canvas.FillRoundedRectanglePixels(
				shadow,
				keyShadow,
				walk.Size{Width: keyRadius * 2, Height: keyRadius * 2},
			)
		}
		if !paintRecorderRounded(canvas, keyBounds, palette.key, palette.highlight, keyRadius, 1) {
			if err := canvas.FillRoundedRectanglePixels(
				keyBrush,
				keyBounds,
				walk.Size{Width: keyRadius * 2, Height: keyRadius * 2},
			); err != nil {
				return err
			}
			if err := canvas.DrawRoundedRectanglePixels(
				keyBorder,
				keyBounds,
				walk.Size{Width: keyRadius * 2, Height: keyRadius * 2},
			); err != nil {
				return err
			}
		}
		if err := canvas.DrawTextPixels(
			part.text,
			font,
			palette.text,
			keyBounds,
			walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
				walk.TextNoPrefix,
		); err != nil {
			return err
		}
		x += part.width
	}
	return nil
}

func (binding *hotkeyRecorderBinding) paintSingleChordPill(
	canvas *walk.Canvas,
	font *walk.Font,
	display string,
	x int,
	outer walk.Rectangle,
	available int,
	palette hotkeyRecorderPalette,
	shadow walk.Brush,
	keyBrush walk.Brush,
	keyBorder walk.Pen,
) error {
	height := minInt(
		binding.surface.IntFrom96DPI(27),
		outer.Height-binding.surface.IntFrom96DPI(8),
	)
	bounds := walk.Rectangle{
		X:      x,
		Y:      outer.Y + (outer.Height-height)/2,
		Width:  available,
		Height: height,
	}
	radius := maxInt(5, binding.surface.IntFrom96DPI(5))
	shadowBounds := bounds
	shadowBounds.Y++
	if !paintRecorderRounded(canvas, shadowBounds, palette.shadow, palette.shadow, radius, 0) {
		_ = canvas.FillRoundedRectanglePixels(
			shadow,
			shadowBounds,
			walk.Size{Width: radius * 2, Height: radius * 2},
		)
	}
	if !paintRecorderRounded(canvas, bounds, palette.key, palette.highlight, radius, 1) {
		if err := canvas.FillRoundedRectanglePixels(
			keyBrush,
			bounds,
			walk.Size{Width: radius * 2, Height: radius * 2},
		); err != nil {
			return err
		}
		if err := canvas.DrawRoundedRectanglePixels(
			keyBorder,
			bounds,
			walk.Size{Width: radius * 2, Height: radius * 2},
		); err != nil {
			return err
		}
	}
	padding := binding.surface.IntFrom96DPI(8)
	bounds.X += padding
	bounds.Width -= padding * 2
	return canvas.DrawTextPixels(
		display,
		font,
		palette.text,
		bounds,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
			walk.TextEndEllipsis|walk.TextNoPrefix,
	)
}

func paintRecorderRounded(canvas *walk.Canvas, bounds walk.Rectangle, fill, border walk.Color, radius, stroke int) bool {
	return winfx.PaintRoundedControl(uintptr(canvas.HDC()), winfx.Rect{Left: int32(bounds.X), Top: int32(bounds.Y), Right: int32(bounds.X + bounds.Width), Bottom: int32(bounds.Y + bounds.Height)}, uint32(fill), uint32(border), int32(radius), int32(stroke))
}

func (binding *hotkeyRecorderBinding) invalidate() {
	if binding != nil && binding.surface != nil &&
		!binding.surface.IsDisposed() {
		binding.syncVisualTransition()
		binding.surface.Invalidate()
	}
}

func (binding *hotkeyRecorderBinding) syncVisualTransition() {
	if binding == nil || binding.surface == nil || binding.group == nil {
		return
	}
	target := 0.0
	if binding.hovered ||
		win.GetFocus() == binding.surface.Handle() ||
		binding.group.active == binding && binding.recorder.Active() {
		target = 1
	}
	if target == binding.visualTarget {
		return
	}
	binding.visualTarget = target
	if !binding.group.motion || binding.group.contrast {
		binding.visualLevel = target
		if binding.visualTimer {
			win.KillTimer(
				binding.surface.Handle(),
				hotkeyRecorderVisualTimerID,
			)
			binding.visualTimer = false
		}
		return
	}
	binding.visualTick = time.Now()
	if !binding.visualTimer {
		if win.SetTimer(
			binding.surface.Handle(),
			hotkeyRecorderVisualTimerID,
			15,
			0,
		) != 0 {
			binding.visualTimer = true
		} else {
			binding.visualLevel = target
		}
	}
}

func (binding *hotkeyRecorderBinding) advanceVisualTransition() {
	if binding == nil || binding.surface == nil {
		return
	}
	now := time.Now()
	elapsed := now.Sub(binding.visualTick)
	binding.visualTick = now
	if elapsed <= 0 || elapsed > 50*time.Millisecond {
		elapsed = 15 * time.Millisecond
	}
	// Exponential easing feels Fluent-like and remains stable if Windows
	// coalesces timer messages during a drag or high system load.
	amount := float64(elapsed) / float64(92*time.Millisecond)
	if amount > 1 {
		amount = 1
	}
	binding.visualLevel +=
		(binding.visualTarget - binding.visualLevel) * amount
	difference := binding.visualTarget - binding.visualLevel
	if difference < 0 {
		difference = -difference
	}
	if difference < 0.012 {
		binding.visualLevel = binding.visualTarget
		win.KillTimer(
			binding.surface.Handle(),
			hotkeyRecorderVisualTimerID,
		)
		binding.visualTimer = false
	}
	binding.surface.Invalidate()
}

func blendRecorderColor(
	from walk.Color,
	to walk.Color,
	amount float64,
) walk.Color {
	if amount <= 0 {
		return from
	}
	if amount >= 1 {
		return to
	}
	channel := func(left byte, right byte) byte {
		return byte(float64(left) +
			(float64(right)-float64(left))*amount + 0.5)
	}
	return walk.RGB(
		channel(from.R(), to.R()),
		channel(from.G(), to.G()),
		channel(from.B(), to.B()),
	)
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
