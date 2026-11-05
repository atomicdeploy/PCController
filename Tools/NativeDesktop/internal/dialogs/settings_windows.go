//go:build windows

package dialogs

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"

	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

const (
	settingsSaveCaption          = "&Save settings"
	settingsCancelCaption        = "&Cancel"
	settingsTestToastCaption     = "&Test toast"
	settingsTestSoundCaption     = "Test &audio"
	settingsImportSoundCaption   = "&Import WAV…"
	settingsBrowseOnCaption      = "Browse &on…"
	settingsBrowseOffCaption     = "Browse o&ff…"
	settingsBrowseMessageCaption = "Browse &event…"
	settingsBrowseLockCaption    = "Browse &lock…"
	settingsBrowseUnlockCaption  = "Browse &unlock…"
	settingsBrowseSuspendCaption = "Browse &suspend…"
	settingsBrowseResumeCaption  = "Browse &resume…"
	settingsBrowseNetworkCaption = "Browse &network…"
	settingsBrowseFolderCaption  = "Browse fol&der…"
	settingsRevealCaption        = "&Reveal"
	settingsCopyTokenCaption     = "Cop&y token"
	settingsRegenerateCaption    = "Re&generate…"
	settingsRegisterShellCaption = "Register shell &URI"
	settingsRemoveShellCaption   = "Unregister shell &URI"
	settingsRegisterCOMCaption   = "Register &automation"
	settingsRemoveCOMCaption     = "Unregister &automation"
)

type settingsControls struct {
	controllerMode   bool
	localePreference string
	baseURL          *walk.LineEdit
	requestTimeout   *walk.LineEdit
	webSocket        *walk.CheckBox
	reconnectMin     *walk.LineEdit
	reconnectMax     *walk.LineEdit
	httpPoll         *walk.LineEdit
	telnetPort       *walk.NumberEdit

	viewMode          *walk.ComboBox
	theme             *walk.ComboBox
	locale            *walk.ComboBox
	backdrop          *walk.ComboBox
	renderBackend     *walk.ComboBox
	titleBar          *walk.ComboBox
	borderMode        *walk.ComboBox
	tabs              *walk.TabWidget
	roundedCorners    *walk.CheckBox
	layeredForeground *walk.CheckBox
	alwaysOnTop       *walk.CheckBox
	closeToTray       *walk.CheckBox
	prewarmTrayMenu   *walk.CheckBox
	startMinimized    *walk.CheckBox
	showTaskbar       *walk.CheckBox
	animations        *walk.CheckBox
	magneticSnap      *walk.CheckBox
	snapDistance      *walk.NumberEdit

	notificationsEnabled *walk.CheckBox
	stateChanges         *walk.CheckBox
	initialSync          *walk.CheckBox
	customMessage        *walk.CheckBox
	sound                *walk.CheckBox
	soundSource          *walk.ComboBox
	soundName            *walk.LineEdit
	soundBrowse          *walk.PushButton
	soundFile            string
	soundImportAvailable bool
	soundChanged         func()
	delivery             *walk.ComboBox
	testToast            *walk.PushButton
	testSound            *walk.PushButton

	hotkeyRecorders *hotkeyRecorderGroup
	hotkeyToggle    *hotkeyRecorderBinding
	hotkeyOn        *hotkeyRecorderBinding
	hotkeyOff       *hotkeyRecorderBinding
	hotkeyShow      *hotkeyRecorderBinding

	automationEnabled *walk.CheckBox
	onScript          *walk.LineEdit
	offScript         *walk.LineEdit
	messageScript     *walk.LineEdit
	lockScript        *walk.LineEdit
	unlockScript      *walk.LineEdit
	suspendScript     *walk.LineEdit
	resumeScript      *walk.LineEdit
	networkScript     *walk.LineEdit
	workingDirectory  *walk.LineEdit
	automationTimeout *walk.LineEdit
	captureOutput     *walk.CheckBox
	onScriptBrowse    *walk.PushButton
	offScriptBrowse   *walk.PushButton
	messageBrowse     *walk.PushButton
	lockBrowse        *walk.PushButton
	unlockBrowse      *walk.PushButton
	suspendBrowse     *walk.PushButton
	resumeBrowse      *walk.PushButton
	networkBrowse     *walk.PushButton
	workingDirBrowse  *walk.PushButton

	startWithWindows *walk.CheckBox
	localAPIEnabled  *walk.CheckBox
	localAPIListen   *walk.LineEdit
	localAPIToken    *walk.LineEdit
	revealToken      *walk.CheckBox
	copyToken        *walk.PushButton
	generateToken    *walk.PushButton

	registrationExecutable  *walk.LineEdit
	shellRegistration       *walk.Label
	shellRegistrationButton *walk.PushButton
	comRegistration         *walk.Label
	comRegistrationButton   *walk.PushButton
	registrationStatus      commands.RegistrationStatus
	registrationKnown       bool
	registrationError       error
	registrationBusyMethod  string

	okButton     *walk.PushButton
	cancelButton *walk.PushButton
}

// SettingsPreviewFunc receives normalized working configurations while the
// settings dialog remains open. The dialog sends the original configuration
// again when it is canceled or closed, making preview transactional.
type SettingsPreviewFunc func(config.Config)

// SettingsActionFunc performs a user-requested settings test. Returning an
// error keeps presentation inside the settings window while runtime behavior
// remains owned by the caller.
type SettingsActionFunc func() error

// SettingsSoundImportFunc imports one user-selected WAV into the shared,
// content-addressed native/Web asset store.
type SettingsSoundImportFunc func(path string) (id, name string, err error)

// SettingsRegistrationStatusFunc reads the exact registrations owned by the
// executable hosting the authoritative dispatcher. It must not mutate
// registration state.
type SettingsRegistrationStatusFunc func() (commands.RegistrationStatus, error)

// SettingsRegistrationActionFunc performs one explicit registration mutation.
// The native surface always follows it with RegistrationStatus, including when
// the mutation reports a partial failure.
type SettingsRegistrationActionFunc func(method string) error

// SettingsSaveFunc persists an accepted configuration. Modeless settings call
// it away from the UI thread, keeping both the settings window and its owner
// responsive while validation, disk replacement, and runtime reconfiguration
// complete.
type SettingsSaveFunc func(config.Config) error

// SettingsActions supplies optional live-preview, notification-test, and
// explicit Windows-registration hooks. TestToast and TestSound are
// deliberately separate so neither action depends on, gates, or triggers the
// other.
type SettingsActions struct {
	ControllerMode       bool
	OpenHostSettings     func()
	OpenWebUI            func()
	Preview              SettingsPreviewFunc
	Save                 SettingsSaveFunc
	Accepted             func(config.Config)
	PersistSelectedTab   func(int)
	PersistWindowBounds  func(config.WindowBounds)
	Closed               func()
	Icon                 walk.Image
	TestToast            SettingsActionFunc
	TestSound            SettingsActionFunc
	ImportSound          SettingsSoundImportFunc
	RegistrationStatus   SettingsRegistrationStatusFunc
	RegistrationAction   SettingsRegistrationActionFunc
	HotkeyCaptureChanged func(bool)
}

func (actions SettingsActions) runTestToast() error {
	if actions.TestToast == nil {
		return nil
	}
	return actions.TestToast()
}

func (actions SettingsActions) runTestSound() error {
	if actions.TestSound == nil {
		return nil
	}
	return actions.TestSound()
}

func (actions SettingsActions) runImportSound(path string) (string, string, error) {
	if actions.ImportSound == nil {
		return "", "", errors.New("custom sound import is unavailable")
	}
	return actions.ImportSound(path)
}

func (actions SettingsActions) queryRegistrationStatus() (commands.RegistrationStatus, error) {
	if actions.RegistrationStatus == nil {
		return commands.RegistrationStatus{}, errors.New(
			"Windows registration status is unavailable",
		)
	}
	return actions.RegistrationStatus()
}

func (actions SettingsActions) runRegistrationAction(method string) error {
	if actions.RegistrationAction == nil {
		return errors.New("Windows registration actions are unavailable")
	}
	return actions.RegistrationAction(method)
}

type settingsPreviewController struct {
	original  config.Config
	current   config.Config
	apply     SettingsPreviewFunc
	previewed bool
	committed bool
}

func newSettingsPreviewController(
	original config.Config,
	preview []SettingsPreviewFunc,
) *settingsPreviewController {
	var apply SettingsPreviewFunc
	for _, candidate := range preview {
		if candidate != nil {
			apply = candidate
			break
		}
	}
	return &settingsPreviewController{
		original: original,
		current:  original,
		apply:    apply,
	}
}

func (controller *settingsPreviewController) Preview(candidate config.Config) {
	if controller == nil || controller.apply == nil {
		return
	}
	previous := controller.current
	if reflect.DeepEqual(candidate, previous) {
		return
	}
	controller.current = candidate
	// SettingsTab is a navigation preference, not a live runtime setting.
	// Applying a tab-only change used to synchronously rebuild the main
	// window's chrome/backdrop, re-register hotkeys, touch startup state, and
	// repaint both windows on the Settings UI thread. Besides doing needless
	// work, that starved the native tab's hover/selection timer and made its
	// otherwise buffered transition visibly hitch. Keep the current candidate
	// in sync so a later real edit and Save retain the selected page, but defer
	// persistence to PersistSelectedTab/Save.
	if settingsPreviewEquivalent(previous, candidate) {
		return
	}
	controller.previewed = true
	controller.apply(candidate)
}

func settingsPreviewEquivalent(first, second config.Config) bool {
	first.UI.SettingsTab = second.UI.SettingsTab
	return reflect.DeepEqual(first, second)
}

func (controller *settingsPreviewController) Commit(candidate config.Config) {
	if controller == nil {
		return
	}
	controller.Preview(candidate)
	controller.current = candidate
	controller.committed = true
}

// Reset adopts an externally committed configuration as the new transactional
// baseline. Closing the modeless window after a web/API change must never
// replay the configuration that happened to be current when Settings opened.
func (controller *settingsPreviewController) Reset(candidate config.Config) {
	if controller == nil {
		return
	}
	controller.original = candidate
	controller.current = candidate
	controller.previewed = false
	controller.committed = false
}

func (controller *settingsPreviewController) Close() {
	if controller == nil || controller.apply == nil ||
		controller.committed || !controller.previewed {
		return
	}
	controller.current = controller.original
	controller.apply(controller.original)
}

// ShowSettings displays a DPI-aware, natively themed settings dialog. A
// supplied preview callback is invoked as controls change; persistence remains
// the caller's responsibility after an accepted result.
func ShowSettings(
	owner walk.Form,
	cfg config.Config,
	preview ...SettingsPreviewFunc,
) (updated config.Config, accepted bool, err error) {
	var selected SettingsPreviewFunc
	for _, candidate := range preview {
		if candidate != nil {
			selected = candidate
			break
		}
	}
	return ShowSettingsWithActions(owner, cfg, SettingsActions{
		Preview: selected,
	})
}

// SettingsWindow is a modeless settings surface. The shell keeps one of these
// while it is open so repeated tray, hotkey, API, or in-window requests focus
// the existing window instead of constructing duplicates.
type SettingsWindow struct {
	session *settingsDialogSession
}

func (window *SettingsWindow) HWND() uintptr {
	if window.IsDisposed() {
		return 0
	}
	return uintptr(window.session.dialog.Handle())
}

func (window *SettingsWindow) IsDisposed() bool {
	return window == nil || window.session == nil ||
		window.session.dialog == nil || window.session.dialog.IsDisposed()
}

func (window *SettingsWindow) Activate() error {
	if window.IsDisposed() {
		return fmt.Errorf("settings window is closed")
	}
	window.session.dialog.Show()
	_ = window.session.dialog.BringToTop()
	return window.session.dialog.Activate()
}

func (window *SettingsWindow) Dispose() {
	if window.IsDisposed() {
		return
	}
	window.session.dialog.Dispose()
}

// SyncConfig immediately reflects a configuration committed by another
// surface (web UI, tray, IPC, CLI, or COM) in an already-open modeless
// Settings window. The synchronization guard prevents programmatic control
// updates from being mistaken for a new native preview.
func (window *SettingsWindow) SyncConfig(cfg config.Config) {
	if window.IsDisposed() {
		return
	}
	session := window.session
	session.dialog.Synchronize(func() {
		if session.dialog == nil || session.dialog.IsDisposed() ||
			session.saving.Load() || session.accepted {
			return
		}
		if err := cfg.Normalize(); err != nil {
			return
		}
		session.syncing.Store(true)
		defer session.syncing.Store(false)

		session.working = cfg
		session.result = cfg
		session.preview.Reset(cfg)
		populateSettingsControls(session.controls, cfg)
		applySettingsLocalization(session, cfg)
		updateSettingsEnabled(session.controls)
		if session.theme != nil {
			session.theme.ApplyDeferred(cfg.UI.Theme, cfg.UI.Animations)
			if session.pageTransitions != nil {
				session.pageTransitions.SetMotion(
					session.theme.motion,
				)
				session.pageTransitions.RefreshAppearance()
			}
			if session.controls.hotkeyRecorders != nil {
				session.controls.hotkeyRecorders.SetTheme(
					session.theme.dark,
					session.theme.highContrast,
					session.theme.motion,
				)
			}
			_ = session.theme.Commit()
		}
	})
}

type settingsDialogSession struct {
	dialog          *walk.Dialog
	controls        *settingsControls
	working         config.Config
	result          config.Config
	preview         *settingsPreviewController
	theme           *dialogThemeSession
	geometry        *winfx.DialogGeometrySession
	pageTransitions *winfx.DialogPageTransitions
	actions         SettingsActions
	modeless        bool
	shown           bool

	accepted    bool
	saving      atomic.Bool
	syncing     atomic.Bool
	cleanup     sync.Once
	localeFonts dialogFontCache
}

// ShowSettingsModeless creates and shows an owned settings window without
// entering Walk's nested modal message loop. The main device window therefore
// remains interactive. Save completion and errors are marshaled back onto the
// UI thread.
func ShowSettingsModeless(
	owner walk.Form,
	cfg config.Config,
	actions SettingsActions,
) (*SettingsWindow, error) {
	session, err := newSettingsDialogSession(owner, cfg, actions, true)
	if err != nil {
		return nil, err
	}
	session.shown = true
	session.dialog.Show()
	return &SettingsWindow{session: session}, nil
}

// ShowSettingsWithActions retains the synchronous API for embedders that need
// it. PCControllerDesktop's shell uses ShowSettingsModeless.
func ShowSettingsWithActions(
	owner walk.Form,
	cfg config.Config,
	actions SettingsActions,
) (updated config.Config, accepted bool, err error) {
	session, err := newSettingsDialogSession(owner, cfg, actions, false)
	if err != nil {
		return cfg, false, err
	}
	session.shown = true
	session.dialog.Run()
	if !session.accepted {
		return cfg, false, nil
	}
	return session.result, true, nil
}

func newSettingsDialogSession(
	owner walk.Form,
	cfg config.Config,
	actions SettingsActions,
	modeless bool,
) (*settingsDialogSession, error) {
	working := cfg
	if err := working.Normalize(); err != nil {
		return nil, fmt.Errorf("normalize settings input: %w", err)
	}
	session := &settingsDialogSession{
		controls: new(settingsControls),
		working:  working,
		result:   working,
		preview: newSettingsPreviewController(
			working,
			[]SettingsPreviewFunc{actions.Preview},
		),
		actions:  actions,
		modeless: modeless,
	}
	controls := session.controls
	controls.controllerMode = actions.ControllerMode
	localizer := dialogLocalizer(working)
	// Register before Walk creates any HWND/HFONT, including ownerless SDK use
	// and dialogs opened in English before a live switch to Persian.
	_ = winfx.EnsurePersianFont()
	pages := []decl.TabPage{}
	if !actions.ControllerMode {
		pages = append(pages, settingsDevicePage(controls, working))
	}
	pages = append(pages, settingsAppearancePage(controls, working), settingsNotificationPage(controls, working, actions, &session.dialog), settingsHotkeyPage(controls, working), settingsAutomationPage(controls, working, &session.dialog))
	if !actions.ControllerMode {
		pages = append(pages, settingsIntegrationPage(controls, working, actions, &session.dialog))
	} else {
		pages = append(pages, settingsControllerIntegrationPage(controls, working, actions))
	}

	title := dialogText(localizer, "Settings — PCController")
	if actions.ControllerMode {
		title = dialogText(localizer, "Desktop settings — PCController")
	}
	spec := decl.Dialog{
		AssignTo: &session.dialog,
		Title:    title,
		Size: decl.Size{
			Width:  config.SettingsWindowWidth,
			Height: config.SettingsWindowHeight,
		},
		// The tab surface is scrollable below its ideal size, so the outer
		// dialog can remain fully reachable on high-DPI 1080p displays.
		MinSize: decl.Size{
			Width:  config.SettingsWindowMinWidth,
			Height: config.SettingsWindowMinHeight,
		},
		Font:               decl.Font{Family: dialogFontFamily(localizer), PointSize: 10},
		RightToLeftLayout:  localizer.IsRTL(),
		RightToLeftReading: localizer.IsRTL(),
		Layout:             decl.VBox{Margins: decl.Margins{Left: 18, Top: 16, Right: 18, Bottom: 16}, Spacing: 12},
		DefaultButton:      &controls.okButton,
		CancelButton:       &controls.cancelButton,
		Children: []decl.Widget{
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true},
				Children: []decl.Widget{
					decl.Composite{
						Layout: decl.VBox{MarginsZero: true, Spacing: 2},
						Children: []decl.Widget{
							decl.Label{
								Text: dialogText(localizer, "Settings"),
								Font: decl.Font{
									Family:    dialogFontFamily(localizer),
									PointSize: 16,
									Bold:      true,
								},
							},
							decl.Label{
								Visible: !actions.ControllerMode,
								Text:    dialogText(localizer, "Changes preview immediately. Save keeps them; Cancel restores the previous setup."),
							},
						},
					},
					decl.HSpacer{},
				},
			},
			decl.ScrollView{
				StretchFactor: 1,
				Layout:        decl.VBox{MarginsZero: true},
				Children: []decl.Widget{
					decl.TabWidget{
						AssignTo:      &controls.tabs,
						StretchFactor: 1,
						Pages:         pages,
					},
				},
			},
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					decl.HSpacer{},
					decl.PushButton{
						AssignTo:  &controls.okButton,
						Text:      dialogText(localizer, settingsSaveCaption),
						MinSize:   decl.Size{Width: 132},
						OnClicked: session.requestSave,
					},
					decl.PushButton{
						AssignTo: &controls.cancelButton,
						Text:     dialogText(localizer, settingsCancelCaption),
						MinSize:  decl.Size{Width: 94},
						OnClicked: func() {
							if !session.saving.Load() {
								session.dialog.Cancel()
							}
						},
					},
				},
			},
		},
	}

	if err := spec.Create(owner); err != nil {
		session.preview.Close()
		return nil, fmt.Errorf("create settings dialog: %w", err)
	}
	geometry, geometryErr := winfx.AttachResponsiveDialogGeometry(
		uintptr(session.dialog.Handle()),
	)
	if geometryErr != nil {
		session.dialog.Dispose()
		session.preview.Close()
		return nil, fmt.Errorf("initialize responsive settings geometry: %w", geometryErr)
	}
	session.geometry = geometry
	// Preserve the final hidden placement through every Show, including the
	// ownerless SDK path and repeated activation of modeless Settings.
	session.dialog.SetCenterInOwnerWhenRun(false)
	if actions.Icon != nil {
		if err := session.dialog.SetIcon(actions.Icon); err != nil {
			session.geometry.Dispose()
			session.geometry = nil
			session.dialog.Dispose()
			session.preview.Close()
			return nil, fmt.Errorf("set settings dialog icon: %w", err)
		}
	}
	session.theme = newDialogThemeSession(session.dialog)
	session.dialog.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if session.saving.Load() {
			*canceled = true
			return
		}
		if session.modeless {
			// Walk's modal close path re-enables and demotes an owner. A
			// modeless owned window never disabled it, so detach immediately
			// before destruction to preserve the owner's topmost state.
			_ = session.dialog.SetOwner(nil)
		}
	})
	session.dialog.Disposing().Attach(session.dispose)

	if controls.tabs != nil {
		_ = controls.tabs.SetCurrentIndex(min(settingsTabIndex(working.UI.SettingsTab), controls.tabs.Pages().Len()-1))
	}
	applySettingsLocalization(session, working)
	updateSettingsEnabled(controls)
	session.theme.Apply(working.UI.Theme, working.UI.Animations)
	if controls.tabs != nil {
		pages := controls.tabs.Pages()
		pageHandles := make([]uintptr, 0, pages.Len())
		for index := 0; index < pages.Len(); index++ {
			pageHandles = append(
				pageHandles,
				uintptr(pages.At(index).Handle()),
			)
		}
		if transitions, transitionErr := winfx.AttachDialogPageTransitions(
			uintptr(controls.tabs.Handle()),
			pageHandles,
			controls.tabs.CurrentIndex(),
			session.theme.motion,
		); transitionErr == nil {
			session.pageTransitions = transitions
			controls.tabs.CurrentIndexChanged().Attach(func() {
				if session.pageTransitions != nil {
					session.pageTransitions.SelectionChanged(
						controls.tabs.CurrentIndex(),
					)
				}
			})
		}
	}
	if controls.hotkeyRecorders != nil {
		controls.hotkeyRecorders.SetOnCaptureChanged(
			actions.HotkeyCaptureChanged,
		)
		controls.hotkeyRecorders.SetAvailabilityCheck(
			func(_ string, chord string) error {
				const probeID int32 = 0x5200
				if _, err := winfx.RegisterHotkeyChord(
					uintptr(session.dialog.Handle()),
					probeID,
					chord,
				); err != nil {
					return err
				}
				return winfx.UnregisterHotKey(
					uintptr(session.dialog.Handle()),
					probeID,
				)
			},
		)
		if err := controls.hotkeyRecorders.Attach(); err != nil {
			session.dialog.Dispose()
			session.preview.Close()
			return nil, fmt.Errorf("initialize hotkey recorders: %w", err)
		}
		controls.hotkeyRecorders.SetTheme(
			session.theme.dark,
			session.theme.highContrast,
			session.theme.motion,
		)
	}
	if watchErr := session.theme.WatchSystemChanges(func() {
		if session.pageTransitions != nil {
			session.pageTransitions.SetMotion(
				session.theme.motion,
			)
			session.pageTransitions.RefreshAppearance()
		}
		if controls.hotkeyRecorders != nil {
			controls.hotkeyRecorders.SetTheme(
				session.theme.dark,
				session.theme.highContrast,
				session.theme.motion,
			)
		}
	}); watchErr != nil {
		session.dialog.Dispose()
		return nil, fmt.Errorf(
			"watch settings accessibility theme: %w",
			watchErr,
		)
	}
	wireSettingsPreview(
		controls,
		session.preview,
		session.theme,
		session.pageTransitions,
		&session.syncing,
	)
	if controls.locale != nil {
		controls.locale.CurrentIndexChanged().Attach(func() {
			if session.syncing.Load() || session.dialog == nil ||
				session.dialog.IsDisposed() {
				return
			}
			candidate := collectPreviewSettings(
				session.preview.current,
				controls,
			)
			session.working.UI.Locale = candidate.UI.Locale
			applySettingsLocalization(session, candidate)
			if session.pageTransitions != nil {
				session.pageTransitions.RefreshAppearance()
			}
		})
	}
	if !actions.ControllerMode {
		beginSettingsRegistrationRequest(
			controls,
			actions,
			session.dialog,
			"",
		)
	}
	// Localization and theming can request a fresh layout and change the
	// hidden HWND's size. Commit the requested rectangle only after all of
	// those setup passes, immediately before the first reveal.
	var ownerHandle uintptr
	if owner != nil {
		ownerHandle = uintptr(owner.Handle())
	}
	if err := winfx.PrepareOwnedDialogPlacement(
		uintptr(session.dialog.Handle()), ownerHandle,
		winfx.DialogPlacement{
			X: working.UI.SettingsBounds.X, Y: working.UI.SettingsBounds.Y,
			Width: working.UI.SettingsBounds.Width, Height: working.UI.SettingsBounds.Height,
		},
	); err != nil {
		session.dialog.Dispose()
		return nil, fmt.Errorf("prepare settings dialog monitor geometry: %w", err)
	}
	return session, nil
}

func (session *settingsDialogSession) requestSave() {
	if session == nil || session.dialog == nil ||
		session.dialog.IsDisposed() || session.saving.Load() {
		return
	}
	if session.controls != nil && session.controls.hotkeyRecorders != nil {
		if err := session.controls.hotkeyRecorders.Validate(); err != nil {
			showValidationError(
				session.dialog,
				"Hotkeys need attention",
				err,
			)
			return
		}
	}
	candidate, err := collectSettings(session.working, session.controls)
	if err != nil {
		showValidationError(
			session.dialog,
			"Settings need attention",
			err,
		)
		return
	}
	if session.actions.Save == nil {
		session.completeSave(candidate)
		return
	}
	if !session.saving.CompareAndSwap(false, true) {
		return
	}
	session.controls.okButton.SetEnabled(false)
	session.controls.cancelButton.SetEnabled(false)
	localizer := dialogLocalizer(candidate)
	_ = session.controls.okButton.SetText(
		dialogText(localizer, "Saving…"),
	)

	go func() {
		saveErr := session.actions.Save(candidate)
		if session.dialog == nil || session.dialog.IsDisposed() {
			return
		}
		session.dialog.Synchronize(func() {
			if session.dialog == nil || session.dialog.IsDisposed() {
				return
			}
			session.saving.Store(false)
			_ = session.controls.okButton.SetText(
				dialogText(localizer, settingsSaveCaption),
			)
			session.controls.okButton.SetEnabled(true)
			session.controls.cancelButton.SetEnabled(true)
			if saveErr != nil {
				showValidationError(
					session.dialog,
					"Unable to save settings",
					saveErr,
				)
				return
			}
			session.completeSave(candidate)
		})
	}()
}

func (session *settingsDialogSession) completeSave(candidate config.Config) {
	session.preview.Commit(candidate)
	session.result = candidate
	session.accepted = true
	if session.actions.Accepted != nil {
		session.actions.Accepted(candidate)
	}
	session.dialog.Accept()
}

func (session *settingsDialogSession) dispose() {
	if session == nil {
		return
	}
	session.cleanup.Do(func() {
		selectedTab := currentSettingsTab(session.controls)
		settingsBounds, haveSettingsBounds := session.settingsBounds()
		session.preview.Close()
		if session.controls != nil && session.controls.hotkeyRecorders != nil {
			session.controls.hotkeyRecorders.Dispose()
		}
		if session.pageTransitions != nil {
			session.pageTransitions.Dispose()
			session.pageTransitions = nil
		}
		if session.geometry != nil {
			session.geometry.Dispose()
			session.geometry = nil
		}
		if session.theme != nil {
			session.theme.Dispose()
			session.theme = nil
		}
		session.localeFonts.dispose()
		// A successful Save already persisted SettingsTab as part of the full
		// candidate. Cancel and ordinary close still retain this navigation
		// preference independently from the rolled-back appearance preview.
		if !session.accepted && session.actions.PersistSelectedTab != nil {
			session.actions.PersistSelectedTab(selectedTab)
		}
		if haveSettingsBounds &&
			session.actions.PersistWindowBounds != nil {
			session.actions.PersistWindowBounds(settingsBounds)
		}
		if session.actions.Closed != nil {
			session.actions.Closed()
		}
	})
}

func (session *settingsDialogSession) settingsBounds() (
	config.WindowBounds,
	bool,
) {
	if session == nil || !session.shown || session.dialog == nil ||
		session.dialog.IsDisposed() {
		return config.WindowBounds{}, false
	}
	placement, err := winfx.CaptureDialogPlacement(
		uintptr(session.dialog.Handle()),
	)
	if err != nil {
		return config.WindowBounds{}, false
	}
	bounds := config.WindowBounds{
		X:      placement.X,
		Y:      placement.Y,
		Width:  placement.Width,
		Height: placement.Height,
	}
	// Do not allow native rounding or a transient DPI transition to persist a
	// size outside the same limits used by Config.Normalize.
	bounds.Width = max(
		config.SettingsWindowMinWidth,
		min(bounds.Width, config.SettingsWindowMaxWidth),
	)
	bounds.Height = max(
		config.SettingsWindowMinHeight,
		min(bounds.Height, config.SettingsWindowMaxHeight),
	)
	return bounds, true
}

func settingsTabIndex(index int) int {
	if index < 0 || index >= config.SettingsTabCount {
		return 0
	}
	return index
}

func currentSettingsTab(controls *settingsControls) int {
	if controls == nil || controls.tabs == nil {
		return 0
	}
	return settingsTabIndex(controls.tabs.CurrentIndex())
}

func settingsDevicePage(controls *settingsControls, cfg config.Config) decl.TabPage {
	return decl.TabPage{
		Title:  "Device",
		Layout: decl.Grid{Columns: 3, Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []decl.Widget{
			decl.Label{Row: 0, Column: 0, Text: "Device address:"},
			decl.LineEdit{
				AssignTo:    &controls.baseURL,
				Row:         0,
				Column:      1,
				ColumnSpan:  2,
				Text:        cfg.Device.BaseURL,
				CueBanner:   "http://pccontroller-desktop.local",
				ToolTipText: "Only http and https URLs without embedded credentials are accepted.",
			},
			decl.Label{Row: 1, Column: 0, Text: "Request timeout:"},
			decl.LineEdit{
				AssignTo:   &controls.requestTimeout,
				Row:        1,
				Column:     1,
				ColumnSpan: 2,
				Text:       cfg.Device.RequestTimeout.String(),
				CueBanner:  "5s",
			},
			decl.CheckBox{
				AssignTo:   &controls.webSocket,
				Row:        2,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Listen continuously for WebSocket events",
				Checked:    cfg.Device.WebSocket,
				OnCheckedChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 3, Column: 0, Text: "Reconnect minimum:"},
			decl.LineEdit{
				AssignTo:   &controls.reconnectMin,
				Row:        3,
				Column:     1,
				ColumnSpan: 2,
				Text:       cfg.Device.ReconnectMin.String(),
				CueBanner:  "1s",
			},
			decl.Label{Row: 4, Column: 0, Text: "Reconnect maximum:"},
			decl.LineEdit{
				AssignTo:   &controls.reconnectMax,
				Row:        4,
				Column:     1,
				ColumnSpan: 2,
				Text:       cfg.Device.ReconnectMax.String(),
				CueBanner:  "30s",
			},
			decl.Label{Row: 5, Column: 0, Text: "HTTP poll interval:"},
			decl.LineEdit{
				AssignTo:   &controls.httpPoll,
				Row:        5,
				Column:     1,
				ColumnSpan: 2,
				Text:       cfg.Device.HTTPPoll.String(),
				CueBanner:  "30s (0 disables polling)",
			},
			decl.Label{Row: 6, Column: 0, Text: "Device terminal port:"},
			decl.NumberEdit{
				AssignTo:    &controls.telnetPort,
				Row:         6,
				Column:      1,
				MinValue:    1,
				MaxValue:    65535,
				Value:       float64(cfg.Device.TelnetPort),
				Decimals:    0,
				ToolTipText: "The audited firmware defaults to TCP port 23. The terminal connects only when you explicitly open it.",
			},
			decl.Label{
				Row:         6,
				Column:      2,
				Text:        "Manual only",
				ToolTipText: "PCControllerDesktop never occupies the firmware's single terminal client slot at startup.",
			},
			decl.Label{
				Row:         7,
				Column:      0,
				ColumnSpan:  3,
				Text:        "The .local address is resolved once and refreshed only after a cached connection fails.",
				ToolTipText: "This avoids repeated multicast DNS lookups during normal operation.",
			},
			decl.VSpacer{Row: 8, Column: 0, ColumnSpan: 3},
		},
	}
}

func settingsAppearancePage(controls *settingsControls, cfg config.Config) decl.TabPage {
	localizer := dialogLocalizer(cfg)
	layoutLabels := viewModeLabels
	keepAbove := "Keep the device window above other windows"
	captionHelp := "None removes the title bar, Custom uses PCController controls, and Native uses the Windows caption. Window border controls the resize frame separately."
	if controls.controllerMode {
		layoutLabels = []string{"Compact front panel", "Dashboard"}
		keepAbove = "Keep PCController above other windows"
		captionHelp = "None hides the caption; Custom uses PCController controls; Native uses the Windows caption."
	}
	return decl.TabPage{
		Title:  "Appearance",
		Layout: decl.Grid{Columns: 3, Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []decl.Widget{
			decl.Label{Row: 0, Column: 0, Text: "Window layout:"},
			decl.ComboBox{
				AssignTo:     &controls.viewMode,
				Row:          0,
				Column:       1,
				ColumnSpan:   2,
				Model:        layoutLabels,
				CurrentIndex: indexOfValue(viewModeValues, cfg.UI.ViewMode),
			},
			decl.Label{Row: 1, Column: 0, Text: "Color theme:"},
			decl.ComboBox{
				AssignTo:     &controls.theme,
				Row:          1,
				Column:       1,
				ColumnSpan:   2,
				Model:        themeLabels,
				CurrentIndex: indexOfValue(themeValues, cfg.UI.Theme),
			},
			decl.Label{Row: 2, Column: 0, Text: "Window backdrop:"},
			decl.ComboBox{
				AssignTo:     &controls.backdrop,
				Row:          2,
				Column:       1,
				ColumnSpan:   2,
				Model:        backdropLabels,
				CurrentIndex: indexOfValue(backdropValues, cfg.UI.Backdrop),
				ToolTipText:  "Transparent uses per-pixel alpha and automatically selects Custom controls when a native caption is active.",
			},
			decl.CheckBox{
				AssignTo:   &controls.roundedCorners,
				Row:        4,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Use rounded window corners",
				Checked:    cfg.UI.RoundedCorners,
			},
			decl.CheckBox{
				AssignTo:    &controls.layeredForeground,
				Row:         5,
				Column:      0,
				ColumnSpan:  3,
				Text:        "Enable layered glass foreground effects",
				Checked:     cfg.UI.LayeredForeground,
				ToolTipText: "Controls alpha-composited glass styling independently of the DirectX or compatibility presentation backend.",
			},
			decl.Label{Row: 3, Column: 0, Text: "Rendering backend:"},
			decl.ComboBox{
				AssignTo:     &controls.renderBackend,
				Row:          3,
				Column:       1,
				ColumnSpan:   2,
				Model:        renderBackendLabels,
				CurrentIndex: indexOfValue(renderBackendValues, cfg.UI.RenderBackend),
				ToolTipText:  "Automatic uses the hardware DirectX compositor when compatible and falls back safely. DirectX requests D3D11 explicitly. Compatibility retains every UI feature while using less private memory.",
			},
			decl.Label{Row: 6, Column: 0, Text: "Title bar:"},
			decl.ComboBox{
				AssignTo:   &controls.titleBar,
				Row:        6,
				Column:     1,
				ColumnSpan: 2,
				Model:      titleBarLabels,
				CurrentIndex: indexOfValue(
					titleBarValues,
					cfg.UI.TitleBarMode,
				),
				ToolTipText: captionHelp,
			},
			decl.Label{Row: 7, Column: 0, Text: "Window border:"},
			decl.ComboBox{
				AssignTo:     &controls.borderMode,
				Row:          7,
				Column:       1,
				ColumnSpan:   2,
				Model:        borderModeLabels,
				CurrentIndex: indexOfValue(borderModeValues, cfg.UI.BorderMode),
				ToolTipText:  "Automatic keeps the native resize frame. None removes the outer frame and disables edge resizing.",
			},
			decl.CheckBox{
				AssignTo:   &controls.alwaysOnTop,
				Row:        8,
				Column:     0,
				ColumnSpan: 3,
				Text:       keepAbove,
				Checked:    cfg.UI.AlwaysOnTop,
			},
			decl.CheckBox{
				AssignTo:   &controls.closeToTray,
				Row:        9,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Close button hides the app in the notification area",
				Checked:    cfg.UI.CloseToTray,
			},
			decl.CheckBox{
				AssignTo:    &controls.prewarmTrayMenu,
				Row:         10,
				Column:      0,
				ColumnSpan:  3,
				Text:        "Preload and reuse the notification-area menu",
				Checked:     cfg.UI.PrewarmTrayMenu,
				ToolTipText: "Prepares the menu invisibly and reuses its window between openings to avoid first-frame flashes. Disable to recreate it each time.",
			},
			decl.CheckBox{
				AssignTo:   &controls.startMinimized,
				Row:        11,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Start with the main window hidden",
				Checked:    cfg.UI.StartMinimized,
			},
			decl.CheckBox{
				AssignTo:   &controls.showTaskbar,
				Row:        12,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Show the main window on the taskbar",
				Checked:    cfg.UI.ShowTaskbar,
			},
			decl.CheckBox{
				AssignTo:   &controls.animations,
				Row:        13,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Enable APNG animation and motion",
				Checked:    cfg.UI.Animations,
			},
			decl.CheckBox{
				AssignTo:    &controls.magneticSnap,
				Row:         14,
				Column:      0,
				Text:        "Magnetic edge snap",
				Checked:     cfg.UI.MagneticSnap,
				ToolTipText: "Hold Ctrl while dragging to temporarily disable snapping; release Ctrl to resume it immediately.",
				OnCheckedChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 14, Column: 1, Text: "Distance:"},
			decl.NumberEdit{
				AssignTo:           &controls.snapDistance,
				Row:                14,
				Column:             2,
				Value:              float64(cfg.UI.MagneticSnapDistance),
				MinValue:           1,
				MaxValue:           128,
				Increment:          1,
				Decimals:           0,
				SpinButtonsVisible: true,
				Suffix:             " px",
			},
			decl.Label{Row: 15, Column: 0, Text: dialogText(localizer, "Language:")},
			decl.ComboBox{
				AssignTo:     &controls.locale,
				Row:          15,
				Column:       1,
				ColumnSpan:   2,
				Model:        dialogLabels(localizer, localeLabels),
				CurrentIndex: indexOfValue(localeValues, cfg.UI.Locale),
			},
			decl.VSpacer{Row: 16, Column: 0, ColumnSpan: 3},
		},
	}
}

func settingsNotificationPage(
	controls *settingsControls,
	cfg config.Config,
	actions SettingsActions,
	dialog **walk.Dialog,
) decl.TabPage {
	stateCaption, messageCaption := "Notify when the device turns on or off", "Show sanitized message: events from the device"
	soundLabels := soundSourceLabels
	if controls.controllerMode {
		stateCaption, messageCaption = "Notify when the program enters Running or Idle", "Show controller messages"
		soundLabels = []string{"Bundled PCController sounds", "Custom WAV"}
	}
	controls.soundFile = cfg.Notifications.SoundFile
	controls.soundImportAvailable = actions.ImportSound != nil
	return decl.TabPage{
		Title:  "Notifications",
		Layout: decl.Grid{Columns: 3, Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []decl.Widget{
			decl.CheckBox{
				AssignTo:   &controls.notificationsEnabled,
				Row:        0,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Enable desktop notifications",
				Checked:    cfg.Notifications.Enabled,
				OnCheckedChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 1, Column: 0, Text: "Delivery method:"},
			decl.ComboBox{
				AssignTo:     &controls.delivery,
				Row:          1,
				Column:       1,
				ColumnSpan:   2,
				Model:        deliveryLabels,
				CurrentIndex: indexOfValue(deliveryValues, cfg.Notifications.Delivery),
			},
			decl.CheckBox{
				AssignTo:   &controls.stateChanges,
				Row:        2,
				Column:     0,
				ColumnSpan: 3,
				Text:       stateCaption,
				Checked:    cfg.Notifications.StateChanges,
			},
			decl.CheckBox{
				AssignTo:   &controls.initialSync,
				Row:        3,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Notify after the initial state synchronization",
				Checked:    cfg.Notifications.InitialSync,
			},
			decl.CheckBox{
				AssignTo:   &controls.customMessage,
				Row:        4,
				Column:     0,
				ColumnSpan: 3,
				Text:       messageCaption,
				Checked:    cfg.Notifications.CustomMessage,
			},
			decl.CheckBox{
				AssignTo:   &controls.sound,
				Row:        5,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Play audible cues",
				Checked:    cfg.Notifications.Sound,
			},
			decl.Label{Row: 6, Column: 0, Text: "Sound source:"},
			decl.ComboBox{
				AssignTo:   &controls.soundSource,
				Row:        6,
				Column:     1,
				ColumnSpan: 2,
				Model:      soundLabels,
				CurrentIndex: indexOfValue(
					soundSourceValues,
					cfg.Notifications.SoundSource,
				),
				OnCurrentIndexChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 7, Column: 0, Text: "Custom WAV:"},
			decl.LineEdit{
				AssignTo: &controls.soundName,
				Row:      7,
				Column:   1,
				Text:     cfg.Notifications.SoundName,
				ReadOnly: true,
			},
			decl.PushButton{
				AssignTo:    &controls.soundBrowse,
				Row:         7,
				Column:      2,
				MinSize:     decl.Size{Width: 118},
				Text:        settingsImportSoundCaption,
				Enabled:     actions.ImportSound != nil,
				ToolTipText: "Import one PCM WAV used by native and Web playback without duplicating it in configuration.",
				OnClicked: func() {
					if dialog == nil || *dialog == nil {
						return
					}
					chooseCustomSound(*dialog, controls, actions)
				},
			},
			decl.Label{Row: 8, Column: 0, Text: "Try notifications:"},
			decl.PushButton{
				AssignTo:    &controls.testToast,
				Row:         8,
				Column:      1,
				MinSize:     decl.Size{Width: 118},
				Text:        settingsTestToastCaption,
				Enabled:     actions.TestToast != nil,
				ToolTipText: "Show a test desktop notification without changing the saved settings.",
				OnClicked: func() {
					if err := actions.runTestToast(); err != nil &&
						dialog != nil && *dialog != nil {
						showDialogError(
							*dialog,
							"Test toast failed",
							"PCController could not show the test notification.",
							err,
						)
					}
				},
			},
			decl.PushButton{
				AssignTo:    &controls.testSound,
				Row:         8,
				Column:      2,
				MinSize:     decl.Size{Width: 118},
				Text:        settingsTestSoundCaption,
				Enabled:     actions.TestSound != nil,
				ToolTipText: "Play the notification cue without changing the saved settings.",
				OnClicked: func() {
					if err := actions.runTestSound(); err != nil &&
						dialog != nil && *dialog != nil {
						showDialogError(
							*dialog,
							"Test sound failed",
							"PCController could not play the test cue.",
							err,
						)
					}
				},
			},
			decl.Label{
				Row:        9,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Automatic delivery prefers a modern toast and falls back to the existing tray balloon.",
			},
			decl.VSpacer{Row: 10, Column: 0, ColumnSpan: 3},
		},
	}
}

func settingsHotkeyPage(controls *settingsControls, cfg config.Config) decl.TabPage {
	label := func(donor, controller string) string {
		if controls.controllerMode {
			return controller
		}
		return donor
	}
	group := newHotkeyRecorderGroup()
	controls.hotkeyRecorders = group
	controls.hotkeyToggle = group.Add(
		label("Toggle device", "Compact / dashboard"),
		label("Switch on ↔ off", "Switch the desktop view"),
		cfg.Hotkeys.Toggle,
	)
	controls.hotkeyOn = group.Add(
		label("Turn on", "Hardware channels"),
		label("Force the device on", "Show channel controls"),
		cfg.Hotkeys.On,
	)
	controls.hotkeyOff = group.Add(
		label("Turn off", "Console / TUI"),
		label("Force the device off", "Show or hide the console"),
		cfg.Hotkeys.Off,
	)
	controls.hotkeyShow = group.Add(
		"Show window",
		label("Bring PCController forward", "Bring PCController forward"),
		cfg.Hotkeys.Show,
	)

	return decl.TabPage{
		Title: "Hotkeys",
		Layout: decl.VBox{
			Margins: decl.Margins{
				Left: 12, Top: 14, Right: 12, Bottom: 12,
			},
			Spacing: 8,
		},
		Children: []decl.Widget{
			decl.Label{
				Text: "Keyboard shortcuts",
				Font: decl.Font{
					Family:    "Segoe UI",
					PointSize: 12,
					Bold:      true,
				},
			},
			decl.Label{
				Text: label("Select Record, then press the complete combination. Existing global shortcuts pause while PCController is listening.", "Select Record, then press the shortcut. Desktop shortcuts pause during recording."),
			},
			controls.hotkeyToggle.Declaration(),
			controls.hotkeyOn.Declaration(),
			controls.hotkeyOff.Declaration(),
			controls.hotkeyShow.Declaration(),
			decl.Label{
				Text: "Escape cancels · Backspace or Delete clears · left/right modifiers are normalized automatically",
			},
			decl.VSpacer{},
		},
	}
}

func settingsAutomationPage(
	controls *settingsControls,
	cfg config.Config,
	dialog **walk.Dialog,
) decl.TabPage {
	label := func(donor, controller string) string {
		if controls.controllerMode {
			return controller
		}
		return donor
	}
	browse := func(title string, target func() *walk.LineEdit) {
		if *dialog == nil || target() == nil {
			return
		}
		chooseScript(*dialog, title, target())
	}
	return decl.TabPage{
		Title:  "Automation",
		Layout: decl.Grid{Columns: 3, Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []decl.Widget{
			decl.CheckBox{
				AssignTo:   &controls.automationEnabled,
				Row:        0,
				Column:     0,
				ColumnSpan: 3,
				Text:       label("Run local scripts for device and Windows events", "Run local scripts for program and Windows events"),
				Checked:    cfg.Automation.Enabled,
				OnCheckedChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 1, Column: 0, Text: label("Device-on script:", "Running-mode script:")},
			decl.LineEdit{
				AssignTo:  &controls.onScript,
				Row:       1,
				Column:    1,
				Text:      cfg.Automation.OnScript,
				CueBanner: ".ps1, .cmd, .bat, .exe, or .com",
			},
			decl.PushButton{
				AssignTo: &controls.onScriptBrowse,
				Row:      1,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseOnCaption,
				OnClicked: func() {
					browse(label("Select device-on script", "Select Running-mode script"), func() *walk.LineEdit { return controls.onScript })
				},
			},
			decl.Label{Row: 2, Column: 0, Text: label("Device-off script:", "Idle-mode script:")},
			decl.LineEdit{
				AssignTo:  &controls.offScript,
				Row:       2,
				Column:    1,
				Text:      cfg.Automation.OffScript,
				CueBanner: ".ps1, .cmd, .bat, .exe, or .com",
			},
			decl.PushButton{
				AssignTo: &controls.offScriptBrowse,
				Row:      2,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseOffCaption,
				OnClicked: func() {
					browse(label("Select device-off script", "Select Idle-mode script"), func() *walk.LineEdit { return controls.offScript })
				},
			},
			decl.Label{Row: 3, Column: 0, Text: "Message script:"},
			decl.LineEdit{
				AssignTo:  &controls.messageScript,
				Row:       3,
				Column:    1,
				Text:      cfg.Automation.MessageScript,
				CueBanner: ".ps1, .cmd, .bat, .exe, or .com",
			},
			decl.PushButton{
				AssignTo: &controls.messageBrowse,
				Row:      3,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseMessageCaption,
				OnClicked: func() {
					browse("Select message-event script", func() *walk.LineEdit { return controls.messageScript })
				},
			},
			decl.Label{Row: 4, Column: 0, Text: "Windows lock script:"},
			decl.LineEdit{
				AssignTo:  &controls.lockScript,
				Row:       4,
				Column:    1,
				Text:      cfg.Automation.LockScript,
				CueBanner: "Optional session-lock automation",
			},
			decl.PushButton{
				AssignTo: &controls.lockBrowse,
				Row:      4,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseLockCaption,
				OnClicked: func() {
					browse("Select Windows lock script", func() *walk.LineEdit { return controls.lockScript })
				},
			},
			decl.Label{Row: 5, Column: 0, Text: "Windows unlock script:"},
			decl.LineEdit{
				AssignTo:  &controls.unlockScript,
				Row:       5,
				Column:    1,
				Text:      cfg.Automation.UnlockScript,
				CueBanner: "Optional session-unlock automation",
			},
			decl.PushButton{
				AssignTo: &controls.unlockBrowse,
				Row:      5,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseUnlockCaption,
				OnClicked: func() {
					browse("Select Windows unlock script", func() *walk.LineEdit { return controls.unlockScript })
				},
			},
			decl.Label{Row: 6, Column: 0, Text: "System suspend script:"},
			decl.LineEdit{
				AssignTo:  &controls.suspendScript,
				Row:       6,
				Column:    1,
				Text:      cfg.Automation.SuspendScript,
				CueBanner: "Optional suspend automation",
			},
			decl.PushButton{
				AssignTo: &controls.suspendBrowse,
				Row:      6,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseSuspendCaption,
				OnClicked: func() {
					browse("Select system suspend script", func() *walk.LineEdit { return controls.suspendScript })
				},
			},
			decl.Label{Row: 7, Column: 0, Text: "System resume script:"},
			decl.LineEdit{
				AssignTo:  &controls.resumeScript,
				Row:       7,
				Column:    1,
				Text:      cfg.Automation.ResumeScript,
				CueBanner: "Optional resume automation",
			},
			decl.PushButton{
				AssignTo: &controls.resumeBrowse,
				Row:      7,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseResumeCaption,
				OnClicked: func() {
					browse("Select system resume script", func() *walk.LineEdit { return controls.resumeScript })
				},
			},
			decl.Label{Row: 8, Column: 0, Text: "Network-change script:"},
			decl.LineEdit{
				AssignTo:  &controls.networkScript,
				Row:       8,
				Column:    1,
				Text:      cfg.Automation.NetworkChangeScript,
				CueBanner: "Runs after an IP-interface change",
			},
			decl.PushButton{
				AssignTo: &controls.networkBrowse,
				Row:      8,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseNetworkCaption,
				OnClicked: func() {
					browse("Select network-change script", func() *walk.LineEdit { return controls.networkScript })
				},
			},
			decl.Label{Row: 9, Column: 0, Text: "Working directory:"},
			decl.LineEdit{
				AssignTo:  &controls.workingDirectory,
				Row:       9,
				Column:    1,
				Text:      cfg.Automation.WorkingDirectory,
				CueBanner: "Optional",
			},
			decl.PushButton{
				AssignTo: &controls.workingDirBrowse,
				Row:      9,
				Column:   2,
				MinSize:  decl.Size{Width: 132},
				Text:     settingsBrowseFolderCaption,
				OnClicked: func() {
					if *dialog != nil && controls.workingDirectory != nil {
						chooseFolder(*dialog, controls.workingDirectory)
					}
				},
			},
			decl.Label{Row: 10, Column: 0, Text: "Script timeout:"},
			decl.LineEdit{
				AssignTo:   &controls.automationTimeout,
				Row:        10,
				Column:     1,
				ColumnSpan: 2,
				Text:       cfg.Automation.Timeout.String(),
				CueBanner:  "15s",
			},
			decl.CheckBox{
				AssignTo:   &controls.captureOutput,
				Row:        11,
				Column:     0,
				ColumnSpan: 3,
				Text:       "Capture bounded script output in the application log",
				Checked:    cfg.Automation.CaptureOutput,
			},
			decl.Label{
				Row:        12,
				Column:     0,
				ColumnSpan: 3,
				Text:       label("Scripts run hidden with PCCONTROLLER_DESKTOP_* event variables. Only explicitly configured local files are executed.", "Scripts receive PCCONTROLLER_* event variables."),
			},
			decl.VSpacer{Row: 13, Column: 0, ColumnSpan: 3},
		},
	}
}

func settingsIntegrationPage(
	controls *settingsControls,
	cfg config.Config,
	actions SettingsActions,
	dialog **walk.Dialog,
) decl.TabPage {
	return decl.TabPage{
		Title:  "Integration",
		Layout: decl.Grid{Columns: 4, Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []decl.Widget{
			decl.CheckBox{
				AssignTo:   &controls.startWithWindows,
				Row:        0,
				Column:     0,
				ColumnSpan: 4,
				Text:       "Start PCController when I sign in",
				Checked:    cfg.Integration.StartWithWindows,
			},
			decl.CheckBox{
				AssignTo:   &controls.localAPIEnabled,
				Row:        1,
				Column:     0,
				ColumnSpan: 4,
				Text:       "Enable the authenticated loopback API",
				Checked:    cfg.Integration.LocalAPIEnabled,
				OnCheckedChanged: func() {
					updateSettingsEnabled(controls)
				},
			},
			decl.Label{Row: 2, Column: 0, Text: "Listen address:"},
			decl.LineEdit{
				AssignTo:    &controls.localAPIListen,
				Row:         2,
				Column:      1,
				ColumnSpan:  3,
				Text:        cfg.Integration.LocalAPIListen,
				CueBanner:   "127.0.0.1:47831",
				ToolTipText: "Only localhost and loopback IP addresses are accepted.",
			},
			decl.Label{Row: 3, Column: 0, Text: "Bearer token:"},
			decl.LineEdit{
				AssignTo:     &controls.localAPIToken,
				Row:          3,
				Column:       1,
				ColumnSpan:   3,
				Text:         cfg.Integration.LocalAPIToken,
				PasswordMode: true,
				ReadOnly:     true,
			},
			decl.CheckBox{
				AssignTo: &controls.revealToken,
				Row:      4,
				Column:   1,
				Text:     settingsRevealCaption,
				OnCheckedChanged: func() {
					if controls.localAPIToken != nil && controls.revealToken != nil {
						controls.localAPIToken.SetPasswordMode(!controls.revealToken.Checked())
						_ = controls.localAPIToken.Invalidate()
					}
				},
			},
			decl.PushButton{
				AssignTo: &controls.copyToken,
				Row:      4,
				Column:   2,
				MinSize:  decl.Size{Width: 118},
				Text:     settingsCopyTokenCaption,
				OnClicked: func() {
					if controls.localAPIToken == nil {
						return
					}
					if err := walk.Clipboard().SetText(controls.localAPIToken.Text()); err != nil && *dialog != nil {
						showValidationError(*dialog, "Could not copy token", err)
					}
				},
			},
			decl.PushButton{
				AssignTo: &controls.generateToken,
				Row:      4,
				Column:   3,
				MinSize:  decl.Size{Width: 130},
				Text:     settingsRegenerateCaption,
				OnClicked: func() {
					if *dialog == nil || controls.localAPIToken == nil {
						return
					}
					confirmed, confirmErr := winfx.Confirm(
						uintptr((*dialog).Handle()),
						"Regenerate API token",
						"Replace the current local API token?",
						"Existing clients will stop working until they are updated with the new token.",
					)
					if confirmErr != nil {
						showValidationError(*dialog, "Could not confirm token change", confirmErr)
						return
					}
					if !confirmed {
						return
					}
					token, tokenErr := config.NewAPIToken()
					if tokenErr != nil {
						showValidationError(*dialog, "Could not create token", tokenErr)
						return
					}
					_ = controls.localAPIToken.SetText(token)
				},
			},
			decl.Label{
				Row:        5,
				Column:     0,
				ColumnSpan: 4,
				Text:       "The API remains bound to loopback. Treat the token like a password and rotate it if exposed.",
			},
			decl.Label{
				Row:        6,
				Column:     0,
				ColumnSpan: 4,
				Text:       "Windows registration",
				Font: decl.Font{
					Family:    "Segoe UI",
					PointSize: 11,
					Bold:      true,
				},
			},
			decl.Label{
				Row:        7,
				Column:     0,
				ColumnSpan: 4,
				Text:       "Portable mode does not register these automatically. Register only when you want this exact executable to own them.",
			},
			decl.Label{Row: 8, Column: 0, Text: "Current executable:"},
			decl.LineEdit{
				AssignTo:   &controls.registrationExecutable,
				Row:        8,
				Column:     1,
				ColumnSpan: 3,
				Text:       "Checking…",
				ReadOnly:   true,
			},
			decl.Label{Row: 9, Column: 0, Text: "Shell (URI + Jump List):"},
			decl.Label{
				AssignTo:   &controls.shellRegistration,
				Row:        9,
				Column:     1,
				ColumnSpan: 2,
				Text:       "Checking URI protocol and Jump List…",
			},
			decl.PushButton{
				AssignTo:    &controls.shellRegistrationButton,
				Row:         9,
				Column:      3,
				MinSize:     decl.Size{Width: 170},
				Text:        settingsRegisterShellCaption,
				Enabled:     false,
				ToolTipText: "Opt in or out of pccontroller-desktop: URI handling and PCController Jump List tasks for this executable.",
				OnClicked: func() {
					requestSettingsRegistrationAction(
						controls,
						actions,
						dialog,
						settingsRegistrationShell,
					)
				},
			},
			decl.Label{Row: 10, Column: 0, Text: "COM automation:"},
			decl.Label{
				AssignTo:   &controls.comRegistration,
				Row:        10,
				Column:     1,
				ColumnSpan: 2,
				Text:       "Checking VBA and AutoHotkey registration…",
			},
			decl.PushButton{
				AssignTo:    &controls.comRegistrationButton,
				Row:         10,
				Column:      3,
				MinSize:     decl.Size{Width: 170},
				Text:        settingsRegisterCOMCaption,
				Enabled:     false,
				ToolTipText: "Opt in or out of the PCControllerDesktop.Application COM object for this executable.",
				OnClicked: func() {
					requestSettingsRegistrationAction(
						controls,
						actions,
						dialog,
						settingsRegistrationCOM,
					)
				},
			},
			decl.Label{
				Row:        11,
				Column:     0,
				ColumnSpan: 4,
				Text:       "Unregister removes only entries still owned by the executable shown above; foreign registrations are preserved.",
			},
			decl.VSpacer{Row: 12, Column: 0, ColumnSpan: 4},
		},
	}
}

type settingsRegistrationKind uint8

const (
	settingsRegistrationShell settingsRegistrationKind = iota
	settingsRegistrationCOM
)

type settingsRegistrationPresentation struct {
	executable   string
	shellStatus  string
	shellButton  string
	shellEnabled bool
	comStatus    string
	comButton    string
	comEnabled   bool
}

func registrationPresentation(
	status commands.RegistrationStatus,
	known bool,
	statusErr error,
	busyMethod string,
	statusAvailable bool,
	actionAvailable bool,
) settingsRegistrationPresentation {
	presentation := settingsRegistrationPresentation{
		executable:  "Checking…",
		shellStatus: "Checking URI protocol and Jump List…",
		shellButton: settingsRegisterShellCaption,
		comStatus:   "Checking VBA and AutoHotkey registration…",
		comButton:   settingsRegisterCOMCaption,
	}
	if !statusAvailable {
		presentation.executable = "Unavailable in this host"
		presentation.shellStatus = "Registration status is unavailable"
		presentation.comStatus = "Registration status is unavailable"
		return presentation
	}
	if status.Executable != "" {
		presentation.executable = status.Executable
	}
	if statusErr != nil {
		detail := "Unable to read exact-owned status: " + statusErr.Error()
		presentation.shellStatus = detail
		presentation.comStatus = detail
		return presentation
	}
	if !known {
		return presentation
	}
	if status.Executable == "" {
		presentation.executable = "Not reported"
		presentation.shellStatus = "Exact current executable was not reported"
		presentation.comStatus = "Exact current executable was not reported"
		return presentation
	}

	if status.ShellRegistered {
		presentation.shellStatus = "Registered to this executable"
		presentation.shellButton = settingsRemoveShellCaption
	} else {
		presentation.shellStatus = "Not registered (registration is opt-in)"
	}
	if status.COMRegistered {
		presentation.comStatus = "Registered to this executable"
		presentation.comButton = settingsRemoveCOMCaption
	} else {
		presentation.comStatus = "Not registered (registration is opt-in)"
	}
	presentation.shellEnabled = actionAvailable && busyMethod == ""
	presentation.comEnabled = actionAvailable && busyMethod == ""
	switch busyMethod {
	case commands.MethodIntegrationShellRegister,
		commands.MethodIntegrationShellUnregister:
		presentation.shellStatus = "Updating URI protocol and Jump List…"
	case commands.MethodIntegrationCOMRegister,
		commands.MethodIntegrationCOMUnregister:
		presentation.comStatus = "Updating COM automation registration…"
	}
	return presentation
}

func applyRegistrationPresentation(
	controls *settingsControls,
	actions SettingsActions,
) {
	if controls == nil {
		return
	}
	presentation := registrationPresentation(
		controls.registrationStatus,
		controls.registrationKnown,
		controls.registrationError,
		controls.registrationBusyMethod,
		actions.RegistrationStatus != nil,
		actions.RegistrationAction != nil,
	)
	if controls.registrationExecutable != nil {
		_ = controls.registrationExecutable.SetText(presentation.executable)
	}
	if controls.shellRegistration != nil {
		_ = controls.shellRegistration.SetText(
			settingsControlText(controls, presentation.shellStatus),
		)
	}
	if controls.shellRegistrationButton != nil {
		_ = controls.shellRegistrationButton.SetText(
			settingsControlText(controls, presentation.shellButton),
		)
		controls.shellRegistrationButton.SetEnabled(presentation.shellEnabled)
	}
	if controls.comRegistration != nil {
		_ = controls.comRegistration.SetText(
			settingsControlText(controls, presentation.comStatus),
		)
	}
	if controls.comRegistrationButton != nil {
		_ = controls.comRegistrationButton.SetText(
			settingsControlText(controls, presentation.comButton),
		)
		controls.comRegistrationButton.SetEnabled(presentation.comEnabled)
	}
}

func registrationMethod(
	kind settingsRegistrationKind,
	status commands.RegistrationStatus,
) (method string, unregister bool) {
	switch kind {
	case settingsRegistrationShell:
		if status.ShellRegistered {
			return commands.MethodIntegrationShellUnregister, true
		}
		return commands.MethodIntegrationShellRegister, false
	case settingsRegistrationCOM:
		if status.COMRegistered {
			return commands.MethodIntegrationCOMUnregister, true
		}
		return commands.MethodIntegrationCOMRegister, false
	default:
		return "", false
	}
}

func registrationUnregisterPrompt(
	kind settingsRegistrationKind,
	executable string,
) (title, instruction, content string) {
	name := "Windows shell integration"
	if kind == settingsRegistrationCOM {
		name = "COM automation"
	}
	return "Unregister " + name,
		"Remove " + name + " for the current executable?",
		"PCController will remove only entries still owned by this exact executable:\n\n" +
			executable +
			"\n\nRegistrations owned by another executable are preserved."
}

func requestSettingsRegistrationAction(
	controls *settingsControls,
	actions SettingsActions,
	dialog **walk.Dialog,
	kind settingsRegistrationKind,
) {
	if controls == nil || dialog == nil || *dialog == nil ||
		(*dialog).IsDisposed() || !controls.registrationKnown ||
		controls.registrationBusyMethod != "" ||
		actions.RegistrationAction == nil {
		return
	}
	method, unregister := registrationMethod(kind, controls.registrationStatus)
	if method == "" {
		return
	}
	if unregister {
		title, instruction, content := registrationUnregisterPrompt(
			kind,
			controls.registrationStatus.Executable,
		)
		confirmed, err := winfx.Confirm(
			uintptr((*dialog).Handle()),
			title,
			instruction,
			content,
		)
		if err != nil {
			showValidationError(*dialog, "Could not confirm unregister", err)
			return
		}
		if !confirmed {
			return
		}
	}
	beginSettingsRegistrationRequest(controls, actions, *dialog, method)
}

func beginSettingsRegistrationRequest(
	controls *settingsControls,
	actions SettingsActions,
	dialog *walk.Dialog,
	method string,
) {
	if controls == nil {
		return
	}
	controls.registrationBusyMethod = method
	controls.registrationError = nil
	if method == "" {
		controls.registrationKnown = false
	}
	applyRegistrationPresentation(controls, actions)
	if dialog == nil || dialog.IsDisposed() {
		return
	}

	go func() {
		var actionErr error
		if method != "" {
			actionErr = actions.runRegistrationAction(method)
		}
		status, statusErr := actions.queryRegistrationStatus()
		if dialog.IsDisposed() {
			return
		}
		dialog.Synchronize(func() {
			if dialog.IsDisposed() {
				return
			}
			controls.registrationBusyMethod = ""
			if statusErr == nil {
				controls.registrationStatus = status
				controls.registrationKnown = true
				controls.registrationError = nil
			} else {
				controls.registrationKnown = false
				controls.registrationError = statusErr
			}
			applyRegistrationPresentation(controls, actions)
			if method == "" {
				return
			}
			switch {
			case actionErr != nil && statusErr != nil:
				showDialogError(
					dialog,
					registrationActionErrorTitle(actionErr),
					"PCController could not complete the requested registration change or refresh its exact-owned status.",
					errors.Join(actionErr, fmt.Errorf("refresh status: %w", statusErr)),
				)
			case actionErr != nil:
				showDialogError(
					dialog,
					registrationActionErrorTitle(actionErr),
					"PCController refreshed the current registration status after the requested change failed.",
					actionErr,
				)
			case statusErr != nil:
				showDialogError(
					dialog,
					"Registration status refresh failed",
					"The registration command completed, but PCController could not verify the resulting exact-owned state.",
					statusErr,
				)
			}
		})
	}()
}

func registrationActionErrorTitle(err error) string {
	var coded interface {
		Code() string
	}
	if errors.As(err, &coded) && coded.Code() == "partial" {
		return "Registration was only partly changed"
	}
	return "Windows registration failed"
}

func wireSettingsPreview(
	controls *settingsControls,
	preview *settingsPreviewController,
	theme *dialogThemeSession,
	pageTransitions *winfx.DialogPageTransitions,
	syncing *atomic.Bool,
) {
	if controls == nil || preview == nil {
		return
	}
	changed := func() {
		if syncing != nil && syncing.Load() {
			return
		}
		preview.Preview(collectPreviewSettings(preview.current, controls))
	}
	controls.soundChanged = changed
	themeChanged := func() {
		if syncing != nil && syncing.Load() {
			return
		}
		if controls.theme != nil && theme != nil {
			if value, err := valueAt(
				themeValues,
				controls.theme.CurrentIndex(),
				"Color theme",
			); err == nil {
				animate := controls.animations == nil ||
					controls.animations.Checked()
				theme.ApplyDeferred(value, animate)
				if pageTransitions != nil {
					pageTransitions.SetMotion(
						theme.motion,
					)
					pageTransitions.RefreshAppearance()
				}
				if controls.hotkeyRecorders != nil {
					controls.hotkeyRecorders.SetTheme(
						theme.dark,
						theme.highContrast,
						theme.motion,
					)
				}
				_ = theme.Commit()
			}
		}
		changed()
	}

	for _, control := range []*walk.ComboBox{
		controls.viewMode,
		controls.renderBackend,
		controls.delivery,
		controls.soundSource,
	} {
		if control != nil {
			control.CurrentIndexChanged().Attach(changed)
		}
	}
	adjustingAppearance := false
	if controls.backdrop != nil {
		controls.backdrop.CurrentIndexChanged().Attach(func() {
			if adjustingAppearance {
				return
			}
			if controls.titleBar != nil {
				backdrop, backdropErr := valueAt(
					backdropValues,
					controls.backdrop.CurrentIndex(),
					"Window backdrop",
				)
				titleBar, titleBarErr := valueAt(
					titleBarValues,
					controls.titleBar.CurrentIndex(),
					"Title bar",
				)
				if backdropErr == nil && titleBarErr == nil &&
					backdrop == config.BackdropTransparent &&
					titleBar == config.TitleBarNative {
					adjustingAppearance = true
					_ = controls.titleBar.SetCurrentIndex(
						indexOfValue(titleBarValues, config.TitleBarCustom),
					)
					adjustingAppearance = false
				}
			}
			changed()
		})
	}
	if controls.borderMode != nil {
		controls.borderMode.CurrentIndexChanged().Attach(func() {
			if adjustingAppearance {
				return
			}
			if controls.titleBar != nil {
				borderMode, borderErr := valueAt(
					borderModeValues,
					controls.borderMode.CurrentIndex(),
					"Window border",
				)
				titleBar, titleBarErr := valueAt(
					titleBarValues,
					controls.titleBar.CurrentIndex(),
					"Title bar",
				)
				if borderErr == nil && titleBarErr == nil &&
					borderMode == config.BorderNone &&
					titleBar == config.TitleBarNative {
					adjustingAppearance = true
					_ = controls.titleBar.SetCurrentIndex(
						indexOfValue(titleBarValues, config.TitleBarCustom),
					)
					adjustingAppearance = false
				}
			}
			changed()
		})
	}
	if controls.titleBar != nil {
		controls.titleBar.CurrentIndexChanged().Attach(func() {
			if adjustingAppearance {
				return
			}
			adjustingAppearance = true
			if controls.backdrop != nil {
				backdrop, backdropErr := valueAt(
					backdropValues,
					controls.backdrop.CurrentIndex(),
					"Window backdrop",
				)
				titleBar, titleBarErr := valueAt(
					titleBarValues,
					controls.titleBar.CurrentIndex(),
					"Title bar",
				)
				if backdropErr == nil && titleBarErr == nil &&
					backdrop == config.BackdropTransparent &&
					titleBar == config.TitleBarNative {
					_ = controls.backdrop.SetCurrentIndex(
						indexOfValue(backdropValues, config.BackdropSolid),
					)
				}
			}
			if controls.borderMode != nil {
				borderMode, borderErr := valueAt(
					borderModeValues,
					controls.borderMode.CurrentIndex(),
					"Window border",
				)
				titleBar, titleBarErr := valueAt(
					titleBarValues,
					controls.titleBar.CurrentIndex(),
					"Title bar",
				)
				if borderErr == nil && titleBarErr == nil &&
					borderMode == config.BorderNone &&
					titleBar == config.TitleBarNative {
					_ = controls.borderMode.SetCurrentIndex(
						indexOfValue(borderModeValues, config.BorderAuto),
					)
				}
			}
			adjustingAppearance = false
			changed()
		})
	}
	if controls.theme != nil {
		controls.theme.CurrentIndexChanged().Attach(themeChanged)
	}
	if controls.locale != nil {
		controls.locale.CurrentIndexChanged().Attach(changed)
	}
	for _, control := range []*walk.CheckBox{
		controls.webSocket,
		controls.roundedCorners,
		controls.layeredForeground,
		controls.alwaysOnTop,
		controls.closeToTray,
		controls.prewarmTrayMenu,
		controls.startMinimized,
		controls.showTaskbar,
		controls.magneticSnap,
		controls.notificationsEnabled,
		controls.stateChanges,
		controls.initialSync,
		controls.customMessage,
		controls.sound,
		controls.automationEnabled,
		controls.captureOutput,
		controls.startWithWindows,
		controls.localAPIEnabled,
	} {
		if control != nil {
			control.CheckedChanged().Attach(changed)
		}
	}
	if controls.animations != nil {
		controls.animations.CheckedChanged().Attach(themeChanged)
	}
	if controls.snapDistance != nil {
		controls.snapDistance.ValueChanged().Attach(changed)
	}
	if controls.telnetPort != nil {
		controls.telnetPort.ValueChanged().Attach(changed)
	}
	if controls.tabs != nil {
		controls.tabs.CurrentIndexChanged().Attach(changed)
	}
	if controls.hotkeyRecorders != nil {
		controls.hotkeyRecorders.SetOnChanged(changed)
	}
	for _, control := range []*walk.LineEdit{
		controls.baseURL,
		controls.requestTimeout,
		controls.reconnectMin,
		controls.reconnectMax,
		controls.httpPoll,
		controls.onScript,
		controls.offScript,
		controls.messageScript,
		controls.lockScript,
		controls.unlockScript,
		controls.suspendScript,
		controls.resumeScript,
		controls.networkScript,
		controls.workingDirectory,
		controls.automationTimeout,
		controls.localAPIListen,
		controls.localAPIToken,
	} {
		if control != nil {
			control.TextChanged().Attach(changed)
		}
	}
}

func populateSettingsControls(controls *settingsControls, cfg config.Config) {
	if controls == nil {
		return
	}
	setText := func(control *walk.LineEdit, value string) {
		if control != nil && control.Text() != value {
			_ = control.SetText(value)
		}
	}
	setChecked := func(control *walk.CheckBox, value bool) {
		if control != nil && control.Checked() != value {
			control.SetChecked(value)
		}
	}
	setIndex := func(control *walk.ComboBox, values []string, value string) {
		if control == nil {
			return
		}
		index := indexOfValue(values, value)
		if control.CurrentIndex() != index {
			_ = control.SetCurrentIndex(index)
		}
	}

	setText(controls.baseURL, cfg.Device.BaseURL)
	setText(controls.requestTimeout, cfg.Device.RequestTimeout.String())
	setChecked(controls.webSocket, cfg.Device.WebSocket)
	setText(controls.reconnectMin, cfg.Device.ReconnectMin.String())
	setText(controls.reconnectMax, cfg.Device.ReconnectMax.String())
	setText(controls.httpPoll, cfg.Device.HTTPPoll.String())
	if controls.telnetPort != nil &&
		int(math.Round(controls.telnetPort.Value())) != cfg.Device.TelnetPort {
		_ = controls.telnetPort.SetValue(float64(cfg.Device.TelnetPort))
	}

	setIndex(controls.viewMode, viewModeValues, cfg.UI.ViewMode)
	setIndex(controls.theme, themeValues, cfg.UI.Theme)
	setIndex(controls.locale, localeValues, cfg.UI.Locale)
	setIndex(controls.backdrop, backdropValues, cfg.UI.Backdrop)
	setIndex(
		controls.renderBackend,
		renderBackendValues,
		cfg.UI.RenderBackend,
	)
	setIndex(controls.titleBar, titleBarValues, cfg.UI.TitleBarMode)
	setIndex(controls.borderMode, borderModeValues, cfg.UI.BorderMode)
	setChecked(controls.roundedCorners, cfg.UI.RoundedCorners)
	setChecked(controls.layeredForeground, cfg.UI.LayeredForeground)
	setChecked(controls.alwaysOnTop, cfg.UI.AlwaysOnTop)
	setChecked(controls.closeToTray, cfg.UI.CloseToTray)
	setChecked(controls.prewarmTrayMenu, cfg.UI.PrewarmTrayMenu)
	setChecked(controls.startMinimized, cfg.UI.StartMinimized)
	setChecked(controls.showTaskbar, cfg.UI.ShowTaskbar)
	setChecked(controls.animations, cfg.UI.Animations)
	setChecked(controls.magneticSnap, cfg.UI.MagneticSnap)
	if controls.snapDistance != nil &&
		int(math.Round(controls.snapDistance.Value())) != cfg.UI.MagneticSnapDistance {
		_ = controls.snapDistance.SetValue(float64(cfg.UI.MagneticSnapDistance))
	}
	if controls.tabs != nil {
		index := min(settingsTabIndex(cfg.UI.SettingsTab), controls.tabs.Pages().Len()-1)
		if controls.tabs.CurrentIndex() != index {
			_ = controls.tabs.SetCurrentIndex(index)
		}
	}

	setChecked(controls.notificationsEnabled, cfg.Notifications.Enabled)
	setChecked(controls.stateChanges, cfg.Notifications.StateChanges)
	setChecked(controls.initialSync, cfg.Notifications.InitialSync)
	setChecked(controls.customMessage, cfg.Notifications.CustomMessage)
	setChecked(controls.sound, cfg.Notifications.Sound)
	setIndex(
		controls.soundSource,
		soundSourceValues,
		cfg.Notifications.SoundSource,
	)
	controls.soundFile = cfg.Notifications.SoundFile
	setText(controls.soundName, cfg.Notifications.SoundName)
	setIndex(controls.delivery, deliveryValues, cfg.Notifications.Delivery)

	if controls.hotkeyRecorders != nil {
		controls.hotkeyRecorders.CancelActive(false)
	}
	if controls.hotkeyToggle != nil {
		controls.hotkeyToggle.SetChord(cfg.Hotkeys.Toggle)
	}
	if controls.hotkeyOn != nil {
		controls.hotkeyOn.SetChord(cfg.Hotkeys.On)
	}
	if controls.hotkeyOff != nil {
		controls.hotkeyOff.SetChord(cfg.Hotkeys.Off)
	}
	if controls.hotkeyShow != nil {
		controls.hotkeyShow.SetChord(cfg.Hotkeys.Show)
	}
	if controls.hotkeyRecorders != nil {
		controls.hotkeyRecorders.RefreshValidation()
		controls.hotkeyRecorders.rememberStableIfValid()
	}

	setChecked(controls.automationEnabled, cfg.Automation.Enabled)
	setText(controls.onScript, cfg.Automation.OnScript)
	setText(controls.offScript, cfg.Automation.OffScript)
	setText(controls.messageScript, cfg.Automation.MessageScript)
	setText(controls.lockScript, cfg.Automation.LockScript)
	setText(controls.unlockScript, cfg.Automation.UnlockScript)
	setText(controls.suspendScript, cfg.Automation.SuspendScript)
	setText(controls.resumeScript, cfg.Automation.ResumeScript)
	setText(controls.networkScript, cfg.Automation.NetworkChangeScript)
	setText(controls.workingDirectory, cfg.Automation.WorkingDirectory)
	setText(controls.automationTimeout, cfg.Automation.Timeout.String())
	setChecked(controls.captureOutput, cfg.Automation.CaptureOutput)

	setChecked(controls.startWithWindows, cfg.Integration.StartWithWindows)
	setChecked(controls.localAPIEnabled, cfg.Integration.LocalAPIEnabled)
	setText(controls.localAPIListen, cfg.Integration.LocalAPIListen)
	setText(controls.localAPIToken, cfg.Integration.LocalAPIToken)
	if controls.revealToken != nil && controls.revealToken.Checked() {
		controls.revealToken.SetChecked(false)
	}
	if controls.localAPIToken != nil {
		controls.localAPIToken.SetPasswordMode(true)
	}
}

type appearanceSettingsValues struct {
	viewMode          string
	theme             string
	locale            string
	backdrop          string
	renderBackend     string
	roundedCorners    bool
	layeredForeground bool
	titleBarMode      string
	borderMode        string
	alwaysOnTop       bool
	closeToTray       bool
	prewarmTrayMenu   bool
	startMinimized    bool
	showTaskbar       bool
	animations        bool
	magneticSnap      bool
	snapDistance      int
}

func readAppearanceSettings(
	controls *settingsControls,
) (appearanceSettingsValues, error) {
	if controls == nil {
		return appearanceSettingsValues{}, fmt.Errorf("appearance controls are unavailable")
	}
	viewMode, err := valueAt(
		viewModeValues,
		controls.viewMode.CurrentIndex(),
		"Window layout",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	theme, err := valueAt(
		themeValues,
		controls.theme.CurrentIndex(),
		"Color theme",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	locale, err := valueAt(
		localeValues,
		controls.locale.CurrentIndex(),
		"Language",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	backdrop, err := valueAt(
		backdropValues,
		controls.backdrop.CurrentIndex(),
		"Window backdrop",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	renderBackend, err := valueAt(
		renderBackendValues,
		controls.renderBackend.CurrentIndex(),
		"Rendering backend",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	titleBarMode, err := valueAt(
		titleBarValues,
		controls.titleBar.CurrentIndex(),
		"Title bar",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	borderMode, err := valueAt(
		borderModeValues,
		controls.borderMode.CurrentIndex(),
		"Window border",
	)
	if err != nil {
		return appearanceSettingsValues{}, err
	}
	if backdrop == config.BackdropTransparent &&
		titleBarMode == config.TitleBarNative {
		return appearanceSettingsValues{}, fmt.Errorf(
			"Transparent background requires None or Custom title bar so alpha-zero pixels can pass pointer input through",
		)
	}
	if borderMode == config.BorderNone &&
		titleBarMode == config.TitleBarNative {
		return appearanceSettingsValues{}, fmt.Errorf(
			"No border requires None or Custom title bar because the native Windows caption owns a non-client frame",
		)
	}
	return appearanceSettingsValues{
		viewMode:          viewMode,
		theme:             theme,
		locale:            locale,
		backdrop:          backdrop,
		renderBackend:     renderBackend,
		roundedCorners:    controls.roundedCorners.Checked(),
		layeredForeground: controls.layeredForeground.Checked(),
		titleBarMode:      titleBarMode,
		borderMode:        borderMode,
		alwaysOnTop:       controls.alwaysOnTop.Checked(),
		closeToTray:       controls.closeToTray.Checked(),
		prewarmTrayMenu:   controls.prewarmTrayMenu.Checked(),
		startMinimized:    controls.startMinimized.Checked(),
		showTaskbar:       controls.showTaskbar.Checked(),
		animations:        controls.animations.Checked(),
		magneticSnap:      controls.magneticSnap.Checked(),
		snapDistance:      int(math.Round(controls.snapDistance.Value())),
	}, nil
}

func applyAppearanceSettings(
	ui *config.UIConfig,
	values appearanceSettingsValues,
) {
	if ui == nil {
		return
	}
	ui.ViewMode = values.viewMode
	ui.Theme = values.theme
	ui.Locale = values.locale
	ui.Backdrop = values.backdrop
	ui.RenderBackend = values.renderBackend
	ui.RoundedCorners = values.roundedCorners
	ui.LayeredForeground = values.layeredForeground
	ui.TitleBarMode = values.titleBarMode
	ui.BorderMode = values.borderMode
	ui.ShowTitleBar = values.titleBarMode == config.TitleBarCustom
	ui.AlwaysOnTop = values.alwaysOnTop
	ui.CloseToTray = values.closeToTray
	ui.PrewarmTrayMenu = values.prewarmTrayMenu
	ui.StartMinimized = values.startMinimized
	ui.ShowTaskbar = values.showTaskbar
	ui.Animations = values.animations
	ui.MagneticSnap = values.magneticSnap
	ui.MagneticSnapDistance = values.snapDistance
}

// collectPreviewSettings keeps choice-based UI preview responsive even while
// a text field contains a temporarily incomplete value. Once the complete
// form validates, every setting participates in preview.
func collectPreviewSettings(
	base config.Config,
	controls *settingsControls,
) config.Config {
	if candidate, err := collectSettings(base, controls); err == nil {
		applyHotkeyRecorderPreview(&candidate, controls)
		return candidate
	}
	if controls == nil {
		return base
	}

	candidate := base
	if appearance, err := readAppearanceSettings(controls); err == nil {
		applyAppearanceSettings(&candidate.UI, appearance)
	}
	candidate.UI.SettingsTab = currentSettingsTab(controls)

	if controls.webSocket != nil {
		candidate.Device.WebSocket = controls.webSocket.Checked()
	}
	candidate.Notifications.Enabled = controls.notificationsEnabled.Checked()
	candidate.Notifications.StateChanges = controls.stateChanges.Checked()
	candidate.Notifications.InitialSync = controls.initialSync.Checked()
	candidate.Notifications.CustomMessage = controls.customMessage.Checked()
	candidate.Notifications.Sound = controls.sound.Checked()
	if value, err := valueAt(
		soundSourceValues,
		controls.soundSource.CurrentIndex(),
		"Sound source",
	); err == nil {
		candidate.Notifications.SoundSource = value
	}
	candidate.Notifications.SoundFile = controls.soundFile
	candidate.Notifications.SoundName = controls.soundName.Text()
	if value, err := valueAt(
		deliveryValues,
		controls.delivery.CurrentIndex(),
		"Notification delivery",
	); err == nil {
		candidate.Notifications.Delivery = value
	}
	candidate.Automation.Enabled = controls.automationEnabled.Checked()
	candidate.Automation.CaptureOutput = controls.captureOutput.Checked()
	if controls.startWithWindows != nil {
		candidate.Integration.StartWithWindows = controls.startWithWindows.Checked()
	}
	if controls.localAPIEnabled != nil {
		candidate.Integration.LocalAPIEnabled = controls.localAPIEnabled.Checked()
	}
	applyHotkeyRecorderPreview(&candidate, controls)

	if err := candidate.Normalize(); err != nil {
		return base
	}
	return candidate
}

func applyHotkeyRecorderPreview(
	candidate *config.Config,
	controls *settingsControls,
) {
	if candidate == nil || controls == nil || controls.hotkeyRecorders == nil {
		return
	}
	chords := controls.hotkeyRecorders.PreviewChords()
	if len(chords) < 4 {
		return
	}
	// Capture suspension is runtime-only, so this always publishes real
	// bindings. Invalid completions return the recorder group's last fully
	// valid set.
	candidate.Hotkeys.Toggle = chords[0]
	candidate.Hotkeys.On = chords[1]
	candidate.Hotkeys.Off = chords[2]
	candidate.Hotkeys.Show = chords[3]
}

func collectSettings(base config.Config, controls *settingsControls) (config.Config, error) {
	if controls == nil {
		return config.Config{}, fmt.Errorf("settings controls are unavailable")
	}
	candidate := base

	var err error
	if !controls.controllerMode {
		candidate.Device.BaseURL = controls.baseURL.Text()
		if candidate.Device.RequestTimeout, err = parseDuration(controls.requestTimeout.Text(), "Request timeout"); err != nil {
			return config.Config{}, err
		}
		candidate.Device.WebSocket = controls.webSocket.Checked()
		if candidate.Device.ReconnectMin, err = parseDuration(controls.reconnectMin.Text(), "Reconnect minimum"); err != nil {
			return config.Config{}, err
		}
		if candidate.Device.ReconnectMax, err = parseDuration(controls.reconnectMax.Text(), "Reconnect maximum"); err != nil {
			return config.Config{}, err
		}
		if candidate.Device.HTTPPoll, err = parseDuration(controls.httpPoll.Text(), "HTTP poll interval"); err != nil {
			return config.Config{}, err
		}
		candidate.Device.TelnetPort = int(math.Round(controls.telnetPort.Value()))
	}

	appearance, err := readAppearanceSettings(controls)
	if err != nil {
		return config.Config{}, err
	}
	applyAppearanceSettings(&candidate.UI, appearance)
	candidate.UI.SettingsTab = currentSettingsTab(controls)

	candidate.Notifications.Enabled = controls.notificationsEnabled.Checked()
	candidate.Notifications.StateChanges = controls.stateChanges.Checked()
	candidate.Notifications.InitialSync = controls.initialSync.Checked()
	candidate.Notifications.CustomMessage = controls.customMessage.Checked()
	candidate.Notifications.Sound = controls.sound.Checked()
	if candidate.Notifications.SoundSource, err = valueAt(
		soundSourceValues,
		controls.soundSource.CurrentIndex(),
		"Sound source",
	); err != nil {
		return config.Config{}, err
	}
	candidate.Notifications.SoundFile = controls.soundFile
	candidate.Notifications.SoundName = controls.soundName.Text()
	if candidate.Notifications.Delivery, err = valueAt(deliveryValues, controls.delivery.CurrentIndex(), "Notification delivery"); err != nil {
		return config.Config{}, err
	}

	candidate.Hotkeys.Toggle = controls.hotkeyToggle.Chord()
	candidate.Hotkeys.On = controls.hotkeyOn.Chord()
	candidate.Hotkeys.Off = controls.hotkeyOff.Chord()
	candidate.Hotkeys.Show = controls.hotkeyShow.Chord()
	if err := canonicalizeHotkeys(&candidate.Hotkeys); err != nil {
		return config.Config{}, err
	}

	candidate.Automation.Enabled = controls.automationEnabled.Checked()
	candidate.Automation.OnScript = controls.onScript.Text()
	candidate.Automation.OffScript = controls.offScript.Text()
	candidate.Automation.MessageScript = controls.messageScript.Text()
	candidate.Automation.LockScript = controls.lockScript.Text()
	candidate.Automation.UnlockScript = controls.unlockScript.Text()
	candidate.Automation.SuspendScript = controls.suspendScript.Text()
	candidate.Automation.ResumeScript = controls.resumeScript.Text()
	candidate.Automation.NetworkChangeScript = controls.networkScript.Text()
	candidate.Automation.WorkingDirectory = controls.workingDirectory.Text()
	if candidate.Automation.Timeout, err = parseDuration(controls.automationTimeout.Text(), "Automation timeout"); err != nil {
		return config.Config{}, err
	}
	candidate.Automation.CaptureOutput = controls.captureOutput.Checked()

	if controls.startWithWindows != nil {
		candidate.Integration.StartWithWindows = controls.startWithWindows.Checked()
	}
	if !controls.controllerMode {
		candidate.Integration.LocalAPIEnabled = controls.localAPIEnabled.Checked()
		candidate.Integration.LocalAPIListen = controls.localAPIListen.Text()
		candidate.Integration.LocalAPIToken = controls.localAPIToken.Text()
	}

	if err := candidate.Normalize(); err != nil {
		return config.Config{}, err
	}
	return candidate, nil
}

func updateSettingsEnabled(controls *settingsControls) {
	if controls == nil {
		return
	}
	if controls.webSocket != nil {
		enabled := controls.webSocket.Checked()
		setWidgetsEnabled(enabled, controls.reconnectMin, controls.reconnectMax)
	}
	if controls.magneticSnap != nil && controls.snapDistance != nil {
		controls.snapDistance.SetEnabled(controls.magneticSnap.Checked())
	}
	if controls.notificationsEnabled != nil {
		enabled := controls.notificationsEnabled.Checked()
		setWidgetsEnabled(
			enabled,
			controls.delivery,
			controls.stateChanges,
			controls.initialSync,
			controls.customMessage,
		)
	}
	if controls.soundSource != nil {
		source, err := valueAt(
			soundSourceValues,
			controls.soundSource.CurrentIndex(),
			"Sound source",
		)
		custom := err == nil && source == config.SoundSourceCustom
		setWidgetsEnabled(custom, controls.soundName)
		if controls.soundBrowse != nil {
			controls.soundBrowse.SetEnabled(
				custom && controls.soundImportAvailable,
			)
		}
	}
	if controls.automationEnabled != nil {
		enabled := controls.automationEnabled.Checked()
		setWidgetsEnabled(
			enabled,
			controls.onScript,
			controls.offScript,
			controls.messageScript,
			controls.lockScript,
			controls.unlockScript,
			controls.suspendScript,
			controls.resumeScript,
			controls.networkScript,
			controls.workingDirectory,
			controls.automationTimeout,
			controls.captureOutput,
			controls.onScriptBrowse,
			controls.offScriptBrowse,
			controls.messageBrowse,
			controls.lockBrowse,
			controls.unlockBrowse,
			controls.suspendBrowse,
			controls.resumeBrowse,
			controls.networkBrowse,
			controls.workingDirBrowse,
		)
	}
	if controls.localAPIEnabled != nil {
		enabled := controls.localAPIEnabled.Checked()
		if !enabled && controls.revealToken != nil && controls.revealToken.Checked() {
			controls.revealToken.SetChecked(false)
			if controls.localAPIToken != nil {
				controls.localAPIToken.SetPasswordMode(true)
			}
		}
		setWidgetsEnabled(
			enabled,
			controls.localAPIListen,
			controls.localAPIToken,
			controls.revealToken,
			controls.copyToken,
			controls.generateToken,
		)
	}
}

func setWidgetsEnabled(enabled bool, widgets ...walk.Widget) {
	for _, widget := range widgets {
		if widget != nil {
			widget.SetEnabled(enabled)
		}
	}
}

func chooseCustomSound(
	owner walk.Form,
	controls *settingsControls,
	actions SettingsActions,
) {
	if controls == nil || actions.ImportSound == nil {
		return
	}
	localizer := dialogLocalizerForForm(owner)
	dialog := walk.FileDialog{
		Title: dialogText(
			localizer,
			"Import PCController sound effect",
		),
		Filter: dialogText(
			localizer,
			"PCM Wave audio (*.wav)|*.wav|All files (*.*)|*.*",
		),
		FilterIndex: 1,
	}
	if controls.controllerMode {
		dialog.Title = dialogText(localizer, "Import PCController sound effect")
	}
	accepted, err := dialog.ShowOpen(owner)
	if err != nil {
		showValidationError(owner, "Could not select custom sound", err)
		return
	}
	if !accepted {
		return
	}
	id, name, err := actions.runImportSound(dialog.FilePath)
	if err != nil {
		showValidationError(owner, "Could not import custom sound", err)
		return
	}
	controls.soundFile = id
	if controls.soundName != nil {
		_ = controls.soundName.SetText(name)
	}
	if controls.soundSource != nil {
		_ = controls.soundSource.SetCurrentIndex(
			indexOfValue(soundSourceValues, config.SoundSourceCustom),
		)
	}
	if controls.sound != nil {
		controls.sound.SetChecked(true)
	}
	updateSettingsEnabled(controls)
	if controls.soundChanged != nil {
		controls.soundChanged()
	}
}

func chooseScript(owner walk.Form, title string, target *walk.LineEdit) {
	if target == nil {
		return
	}
	localizer := dialogLocalizerForForm(owner)
	dialog := walk.FileDialog{
		Title:    dialogText(localizer, title),
		FilePath: target.Text(),
		Filter: dialogText(
			localizer,
			"Supported scripts (*.ps1;*.cmd;*.bat;*.exe;*.com)|*.ps1;*.cmd;*.bat;*.exe;*.com|All files (*.*)|*.*",
		),
		FilterIndex: 1,
	}
	accepted, err := dialog.ShowOpen(owner)
	if err != nil {
		showValidationError(owner, "Could not select script", err)
		return
	}
	if accepted {
		_ = target.SetText(dialog.FilePath)
	}
}

func chooseFolder(owner walk.Form, target *walk.LineEdit) {
	if target == nil {
		return
	}
	initial := target.Text()
	if initial != "" {
		initial = filepath.Clean(initial)
	}
	localizer := dialogLocalizerForForm(owner)
	dialog := walk.FileDialog{
		Title: dialogText(
			localizer,
			"Select automation working directory",
		),
		InitialDirPath: initial,
	}
	accepted, err := dialog.ShowBrowseFolder(owner)
	if err != nil {
		showValidationError(owner, "Could not select folder", err)
		return
	}
	if accepted {
		_ = target.SetText(dialog.FilePath)
	}
}
