package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/portowner"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/shell"
)

type Model struct {
	runtime *control.Runtime
	engine  *shell.Engine
	remote  *RemoteBackend

	remoteSnapshot         control.Snapshot
	remoteSnapshotPending  bool
	remoteSnapshotError    string
	remoteStatusReceivedAt time.Time
	remoteStatusSequence   uint64
	remoteLEDSequence      uint64
	remoteLiveConnected    bool
	remoteLiveSeen         bool
	remoteClockOffset      time.Duration
	remoteEventsClosed     bool
	remoteLiveClosed       bool

	width  int
	height int
	ready  bool

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	page       Page
	cursor     int
	pageOffset int
	logs       []string
	timeline   []timelineEntry
	samples    []measurementSample
	lastSample time.Time

	historyPos               int
	historyBuf               string
	completion               []string
	completionIndex          int
	terminalVisible          bool
	terminalHidden           bool
	renameTarget             string
	renameTerminalWasVisible bool
	settingEditor            *settingEditor
	displayEditor            *displayEditor
	eventsExpanded           bool

	connectPending           bool
	connectRetryAt           time.Time
	connectRetryDelay        time.Duration
	rebootPending            bool
	statusPending            bool
	uiConfig                 func() appconfig.UI
	saveUI                   func(appconfig.UI) error
	applyTUIConsole          func(appconfig.TUIConsole) error
	uiValue                  appconfig.UI
	hostIntegrations         func() appconfig.Integrations
	saveHostIntegrations     func(appconfig.Integrations) error
	buzzerRuntime            func() appconfig.BuzzerRuntimeStatus
	hostIntegrationValue     appconfig.Integrations
	rfConfig                 func() appconfig.RFConfig
	saveRF                   func(appconfig.RFConfig) error
	rfValue                  appconfig.RFConfig
	rfFetch                  func(context.Context) ([]native.RFEntry, error)
	rfApplyOrder             func(context.Context, []native.RFEntry) error
	rfReplaceSupport         func() control.RFReplaceSupport
	rfProbeReplace           func(context.Context) (control.RFReplaceSupport, error)
	rfEntries                []native.RFEntry
	rfOriginal               []native.RFEntry
	rfStaged                 []native.RFEntry
	rfPending                bool
	rfLastRefresh            time.Time
	rfError                  string
	rfStageDirty             bool
	rfReview                 bool
	rfActionPicker           bool
	rfActionQuery            string
	rfActionCursor           int
	rfCategoryPicker         bool
	rfCategoryCursor         int
	rfEditMode               string
	rfCategoryDraft          string
	rfGuideActive            bool
	rfGuideStep              int
	rfGuidePhase             string
	rfGuideCandidate         *native.RFEntry
	rfGuideCandidateCaptured bool
	rfGuideCaptures          [4]*native.RFEntry
	rfGuideAwaitID           int
	rfGuideMappingID         int
	rfGuideRemoveArmed       bool
	rfGuideClearArmed        bool
	rfGuideTransmitArmed     bool
	prefs                    Preferences
	preview                  *control.Snapshot
	pwmValues                [16]uint16
	havePWMValues            bool
	pwmDragChannel           int
	pwmDragValue             uint16
	pwmDragSet               bool
	pwmPending               bool
	lastPWMRefresh           time.Time
	portPicker               bool
	portLoading              bool
	portCandidates           []ports.Info
	portCursor               int
	portError                string
	portOwner                *portowner.Owner
	portOwnerActions         portowner.Actions
	ownerTerminateArmedUntil time.Time
	frontPanel               func() FrontPanelState
	frontPanelPending        bool
	frontPanelLastRefresh    time.Time
	frontPanelKey            func(key int, phase string) error
	mirrorLCD                func(line1, line2 string) error
	lcdMirror                bool
	previewPanel             FrontPanelState
	frontOverlay1            string
	frontOverlay2            string
	frontOverlayUntil        time.Time
	frontOverlayNeedsRestore bool
	integrations             func() hostui.IntegrationStatus
	networkDiscovery         func(context.Context) ([]discovery.Instance, error)
	openNetwork              func(string) error
	networkDevices           []discovery.Instance
	networkDiscoveryPending  bool
	networkDiscoveryError    string
	notifier                 hostui.Notifier
	appActions               <-chan hostui.AppAction
	instanceID               string
	navigationSync           bool
	setNavigationSync        func(bool)
	navigationIdentity       func() (string, uint64)
	navigationGroup          string
	navigationCursor         hostui.NavigationCursor
	reportPage               func(string) error
	reportTerminal           func(page, title string) error
	reportTerminalAsync      func(page, title string)
	commitNavigation         func(page string)
	suppressNavigationCommit bool
	writeOSC                 func(string) error
	ackAppAction             func(hostui.ActionAck) error
	actionReceipts           map[string]hostui.ActionAck
	actionReceiptOrder       []string
	terminalTitleOverride    string
	terminalTitleDirty       bool
	update                   updatePresentation
	hostMenus                *hostmenu.Manager
	pushHostPanel            func(hostmenu.Snapshot) error
	releaseHostPanel         func() error
	hostPanelRevision        uint64
	hostPanelLastPush        time.Time
	hostPanelCaptured        bool
	hostPanelPending         bool
	menuPages                []menuPage
	menuCatalogSource        string
	menuCatalogHash          uint32
	menuCatalogLoaded        bool
	menuCatalogPending       bool
	menuCatalogLastAttempt   time.Time
	menuLayout               control.MenuLayout
	menuLayoutOriginal       control.MenuLayout
	menuLayoutStaged         control.MenuLayout
	menuLayoutDirty          bool
	menuLayoutSearch         string
	menuLayoutSearchEditing  bool
	menuLayoutSort           string
	menuLayoutError          string
	macroSearch              string
	macroSearchEditing       bool
	macroDeleteArmed         bool
	macroDeleteReference     string
	previewMacros            []appconfig.Macro
	previewMacroState        control.MacroState
	previewMacroRecording    control.MacroRecordingState

	welcome              bool
	welcomeFrame         int
	welcomeStarted       time.Time
	welcomeDeadline      time.Time
	welcomeReadyAt       time.Time
	welcomePhase         string
	welcomeError         string
	welcomeSawBusy       bool
	welcomeMelodyStarted bool
	welcomeMelodyPending bool
	welcomeCanContinue   bool
	welcomeMelody        func(context.Context) error
	markWelcomed         func()
	debug                bool
	notice               string
	noticeUntil          time.Time
}

type tickMsg time.Time
type welcomeTickMsg time.Time
type welcomeMelodyResultMsg struct{ err error }
type runtimeEventMsg control.Event
type controlEventClosedMsg struct{}
type commandResultMsg struct {
	line   string
	output string
	err    error
}
type connectResultMsg struct{ err error }
type ownerActionResultMsg struct {
	action string
	err    error
}
type statusResultMsg struct {
	status native.Status
	err    error
}
type menuCatalogResultMsg struct {
	catalog control.MenuCatalog
	err     error
}
type frontPanelResultMsg struct{ err error }
type resetResultMsg struct{ err error }
type remoteSnapshotResultMsg struct {
	snapshot       control.Snapshot
	err            error
	receivedAt     time.Time
	statusSequence uint64
	ledSequence    uint64
}
type remoteLiveUpdateMsg RemoteLiveUpdate
type remoteLiveClosedMsg struct{}
type portsResultMsg struct {
	values []ports.Info
	err    error
}
type notificationResultMsg struct{ err error }
type appActionMsg hostui.AppAction
type appActionClosedMsg struct{}
type rfEntriesResultMsg struct {
	entries []native.RFEntry
	err     error
}
type rfOrderResultMsg struct {
	entries    []native.RFEntry
	rolledBack bool
	err        error
}
type rfProbeResultMsg struct {
	support control.RFReplaceSupport
	err     error
}
type hostMenuResultMsg struct {
	snapshot hostmenu.Snapshot
	err      error
}
type hostPanelResultMsg struct {
	revision uint64
	released bool
	err      error
}
type networkDiscoveryResultMsg struct {
	instances []discovery.Instance
	err       error
}
type networkConnectResultMsg struct {
	name string
	err  error
}

func New(
	runtime *control.Runtime,
	engine *shell.Engine,
	configProvider ...func() appconfig.UI,
) Model {
	options := Options{DisableWelcome: true}
	if len(configProvider) != 0 {
		options.UIConfig = configProvider[0]
	}
	return NewWithOptions(runtime, engine, options)
}

// NewApplication enables the persisted one-time setup animation. Tests and
// embedders can continue using New for deterministic, animation-free startup.
func NewApplication(
	runtime *control.Runtime,
	engine *shell.Engine,
	configProvider func() appconfig.UI,
	saveConfig ...func(appconfig.UI) error,
) Model {
	var save func(appconfig.UI) error
	if len(saveConfig) != 0 {
		save = saveConfig[0]
	}
	return NewApplicationWithOptions(runtime, engine, Options{
		UIConfig: configProvider,
		SaveUI:   save,
	})
}

// NewApplicationWithOptions applies the application-only defaults (persisted
// first-run setup and LCD prompt mirroring) while retaining every injectable
// integration in Options. The host entrypoint can therefore publish truthful
// live service status and desktop notifications without coupling this package
// to the host's complete configuration schema.
func NewApplicationWithOptions(
	runtime *control.Runtime,
	engine *shell.Engine,
	options Options,
) Model {
	provider := options.UIConfig
	if provider == nil {
		value := appconfig.Defaults().UI
		provider = func() appconfig.UI { return value }
		options.UIConfig = provider
	}
	value := provider()
	if options.MirrorLCD == nil {
		if options.Remote == nil {
			options.MirrorLCD = func(line1, line2 string) error {
				runtime.LCDPresenter().MirrorPrompt(line1, line2)
				return nil
			}
		}
	}
	if !options.DisableWelcome {
		options.ForceWelcome = options.ForceWelcome || !value.SetupComplete
	}
	if options.MarkWelcomed == nil {
		save := options.SaveUI
		options.MarkWelcomed = func() {
			if save == nil {
				return
			}
			latest := provider()
			latest.SetupComplete = true
			_ = save(latest)
		}
	}
	return NewWithOptions(runtime, engine, options)
}

// NewPreview creates an injected board view that never opens or scans a serial
// port. It is intended for safe visual QA, screenshots, and UI development.
func NewPreview(engine *shell.Engine, snapshot control.Snapshot, welcome bool) Model {
	runtime := control.New(control.Options{})
	ui := appconfig.Defaults().UI
	menus := hostmenu.New(appconfig.DefaultHostMenus(), hostmenu.Callbacks{
		Read: func(_ context.Context, action string) (string, error) {
			values := map[string]string{
				"host.status": "PC online", "device.status": "Connected",
				"host.ip": "192.168.1.42", "api.status": "IPC + WS ready",
				"host.macro.selection": "1", "host.macro.selected": "1 output-demo 5st",
				"host.macro.playback": "2/5 1.3s", "host.macro.recording": "Idle",
			}
			return values[action], nil
		},
		Write: func(_ context.Context, action, value string) (string, error) {
			return "Preview " + action + "=" + value, nil
		},
		Execute: func(_ context.Context, action string) (string, error) {
			return "Preview " + action, nil
		},
	})
	if snapshot.Connected {
		_ = menus.Open("")
		_, _ = menus.Refresh(context.Background())
	}
	return NewWithOptions(runtime, engine, Options{
		UIConfig: func() appconfig.UI { return ui },
		Preview:  &snapshot, ForceWelcome: welcome, DisableWelcome: !welcome,
		HostMenus:    menus,
		RFFetch:      func(context.Context) ([]native.RFEntry, error) { return previewRFEntries(), nil },
		RFApplyOrder: func(context.Context, []native.RFEntry) error { return nil },
		RFReplaceSupport: func() control.RFReplaceSupport {
			return control.RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by preview HELLO"}
		},
	})
}

func NewWithOptions(runtime *control.Runtime, engine *shell.Engine, options Options) Model {
	input := textinput.New()
	input.Prompt = "❯ "
	input.Placeholder = "Type a command · Tab/→ completes"
	input.CharLimit = 512
	input.Width = 80
	input.Focus()

	progress := spinner.New()
	progress.Spinner = spinner.MiniDot
	progress.Style = lipgloss.NewStyle().Foreground(colorAccent)

	prefs := defaultPreferences()
	uiValue := appconfig.UI{}
	if options.UIConfig != nil {
		uiValue = options.UIConfig()
		prefs = preferencesFromUI(uiValue)
	}
	rfValue := appconfig.DefaultRFConfig()
	if options.RFConfig != nil {
		rfValue = options.RFConfig()
	}
	hostIntegrationValue := appconfig.Defaults().Integrations
	if options.HostIntegrations != nil {
		hostIntegrationValue = options.HostIntegrations()
	}
	welcome := options.ForceWelcome && !options.DisableWelcome
	welcomeStarted := time.Now()
	marker := options.MarkWelcomed
	if marker == nil {
		marker = func() {}
	}
	debug := options.Debug || strings.EqualFold(os.Getenv("PCCONTROLLER_DEBUG"), "1") ||
		strings.EqualFold(os.Getenv("PCCONTROLLER_DEBUG"), "true")
	ownerActions := options.PortOwnerActions
	if ownerActions == nil {
		ownerActions = portowner.DefaultActions()
	}
	model := Model{
		runtime: runtime, engine: engine, remote: options.Remote, input: input, spinner: progress,
		page: PageDashboard, historyPos: -1, completionIndex: -1, uiConfig: options.UIConfig,
		saveUI: options.SaveUI, applyTUIConsole: options.ApplyTUIConsole, uiValue: uiValue,
		hostIntegrations:     options.HostIntegrations,
		saveHostIntegrations: options.SaveIntegrations,
		buzzerRuntime:        options.BuzzerRuntime,
		hostIntegrationValue: hostIntegrationValue,
		rfConfig:             options.RFConfig, saveRF: options.SaveRF, rfValue: rfValue,
		rfFetch: options.RFFetch, rfApplyOrder: options.RFApplyOrder,
		rfReplaceSupport: options.RFReplaceSupport, rfProbeReplace: options.RFProbeReplace,
		rfGuideAwaitID: -1, rfGuideMappingID: -1,
		frontPanel: options.FrontPanel, frontPanelKey: options.FrontPanelKey,
		mirrorLCD: options.MirrorLCD, lcdMirror: uiValue.MirrorPromptToLCD,
		integrations: options.Integrations, notifier: options.Notifier,
		networkDiscovery: options.NetworkDiscovery, openNetwork: options.OpenNetwork,
		appActions: options.AppActions, instanceID: options.InstanceID,
		navigationSync:     options.NavigationSync,
		setNavigationSync:  options.SetNavigationSync,
		navigationIdentity: options.NavigationIdentity,
		navigationGroup:    strings.ToLower(strings.TrimSpace(options.NavigationGroup)),
		reportPage:         options.ReportPage, reportTerminal: options.ReportTerminal,
		reportTerminalAsync: options.ReportTerminalAsync,
		commitNavigation:    options.CommitNavigation,
		writeOSC:            options.WriteOSC, ackAppAction: options.AckAppAction,
		actionReceipts: make(map[string]hostui.ActionAck),
		hostMenus:      options.HostMenus, pushHostPanel: options.PushHostPanel,
		releaseHostPanel: options.ReleaseHostPanel,
		prefs:            prefs, preview: options.Preview, welcome: welcome,
		pwmDragChannel:   -1,
		portOwnerActions: ownerActions,
		welcomeStarted:   welcomeStarted, welcomeDeadline: welcomeStarted.Add(30 * time.Second),
		welcomePhase: "Waiting for USB and application HELLO", welcomeMelody: options.WelcomeMelody,
		markWelcomed: marker, debug: debug,
		logs: nil,
	}
	if model.navigationGroup == "" {
		model.navigationGroup = hostui.DefaultNavigationGroup
	}
	if options.Remote != nil {
		model.remoteSnapshot = options.Remote.InitialSnapshot
		model.remoteSnapshotPending = options.Remote.Snapshot != nil
		model.remoteSnapshot.ConnectionState = strings.TrimSpace(model.remoteSnapshot.ConnectionState)
		if model.remoteSnapshot.ConnectionState == "" {
			model.remoteSnapshot.ConnectionState = "remote IPC"
		}
		if endpoint := strings.TrimSpace(options.Remote.Endpoint); endpoint != "" {
			model.logs = append(model.logs, "remote IPC attached: "+endpoint)
		}
	}
	capabilities := uint32(0)
	if options.Preview != nil {
		capabilities = options.Preview.Hello.Capabilities
	}
	model.menuPages = menuPagesForCapabilities(capabilities)
	model.reportInstance()
	model.menuCatalogSource = "host generation fallback"
	menuInfo := control.MenuPagesForCapabilities(capabilities)
	model.menuLayout, _ = control.DefaultMenuLayout(menuInfo)
	if capabilities&native.CapabilityMenuLayout != 0 {
		model.menuLayout.Supported = true
		model.menuLayout.Persistent = true
		model.menuLayout.Source = "preview board EEPROM MENU_LAYOUT"
	}
	model.menuLayoutOriginal = cloneMenuLayout(model.menuLayout)
	model.menuLayoutStaged = cloneMenuLayout(model.menuLayout)
	model.menuLayoutSort = "rank"
	if options.Preview != nil {
		model.rfEntries = previewRFEntries()
		model.resetRFStage(model.rfEntries)
		model.seedPreviewPWM()
		model.previewPanel = FrontPanelState{
			Segments: "StAt", Brightness: 5, Blink: false,
			RawSegments: [4]byte{
				segA | segC | segD | segF | segG,
				segD | segE | segF | segG,
				segA | segB | segC | segE | segF | segG,
				segD | segE | segF | segG,
			},
			HasRawSegments: true,
			LCDLine1:       truncateText(prefs.AppTitle, 16), LCDLine2: "Door OPEN · R5",
			LCDBacklight: true, MenuID: options.Preview.Status.MenuPage,
			MenuName:    model.menuPageByID(options.Preview.Status.MenuPage).Name,
			Submode:     model.programModeName(options.Preview.Status.ProgramMode),
			PressedKeys: options.Preview.Status.ActiveKeys, InputSource: "RF + physical",
			Exact: true,
		}
		if !options.Preview.Connected {
			model.previewPanel.LCDLine1 = "PC offline"
			model.previewPanel.LCDLine2 = "Connect USB toPC"
		}
		model.recordSample(*options.Preview)
	}
	return model
}

func (model Model) Init() tea.Cmd {
	commands := []tea.Cmd{tick(model.statusInterval()), tea.SetWindowTitle(model.terminalTitle())}
	if model.appActions != nil {
		commands = append(commands, waitAppAction(model.appActions))
	}
	if model.welcome {
		commands = append(commands, welcomeTick())
	}
	if model.preview == nil {
		if model.remote != nil {
			if model.remote.SetLiveInterval != nil {
				model.remote.SetLiveInterval(model.remoteLiveInterval())
			}
			if model.remote.Events != nil {
				commands = append(commands, waitControlEvent(model.remote.Events))
			}
			if model.remote.Live != nil {
				commands = append(commands, waitRemoteLiveUpdate(model.remote.Live))
			}
			if model.remote.Snapshot != nil {
				model.remoteSnapshotPending = true
				commands = append(commands, refreshRemoteSnapshot(
					model.remote.Snapshot, model.remoteStatusSequence, model.remoteLEDSequence,
				))
			}
		} else {
			commands = append(commands, waitControlEvent(model.runtime.Events()))
		}
	}
	return tea.Batch(commands...)
}

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	inputBefore := model.input.Value()

	switch message := message.(type) {
	case appActionMsg:
		action := hostui.AppAction(message)
		if !hostui.TargetsInstance(action.Target, model.instanceID, "tui") {
			if model.appActions != nil {
				commands = append(commands, waitAppAction(model.appActions))
			}
			break
		}
		if receipt, duplicate := model.actionReceipts[action.OperationID]; action.OperationID != "" && duplicate {
			commands = append(commands, acknowledgeAppAction(model.ackAppAction, receipt))
		} else {
			var actionCommands []tea.Cmd
			var quit bool
			model, actionCommands, quit = model.applyAppAction(action)
			commands = append(commands, actionCommands...)
			if quit {
				return model, tea.Quit
			}
		}
		if model.appActions != nil {
			commands = append(commands, waitAppAction(model.appActions))
		}
	case appActionClosedMsg:
		model.appActions = nil

	case tea.WindowSizeMsg:
		model.width = message.Width
		model.height = message.Height
		model.resize()

	case tea.MouseMsg:
		if model.welcome {
			if model.welcomeCanContinue && message.Button == tea.MouseButtonLeft && message.Action == tea.MouseActionPress {
				model.finishWelcome()
			}
			return model, nil
		}
		return model.handleMouse(message)

	case tea.KeyMsg:
		if model.welcome {
			if model.welcomeCanContinue && (message.String() == "enter" || message.String() == "esc" || message.String() == " ") {
				model.finishWelcome()
			}
			return model, nil
		}
		updated, command, handled := model.handleKey(message)
		if handled {
			return updated, command
		}

	case tickMsg:
		if model.uiConfig != nil {
			model.syncUIConfig(model.uiConfig())
		}
		if model.hostIntegrations != nil {
			model.hostIntegrationValue = model.hostIntegrations()
		}
		if model.rfConfig != nil {
			model.rfValue = model.rfConfig()
		}
		snapshot := model.snapshot()
		if command := model.syncHostPanelCommand(); command != nil {
			commands = append(commands, command)
		}
		if model.remote != nil && !model.remoteStatusReceivedAt.IsZero() {
			snapshot.StatusUpdated = model.remoteStatusReceivedAt
		}
		model.recordSample(snapshot)
		if model.frontOverlayNeedsRestore && time.Now().After(model.frontOverlayUntil) {
			model.frontOverlayNeedsRestore = false
			model.frontOverlay1, model.frontOverlay2 = "", ""
			if model.lcdMirror && model.mirrorLCD != nil {
				state := model.currentFrontPanel(snapshot)
				commands = append(commands, mirrorLCDCommand(model.mirrorLCD, state.LCDLine1, state.LCDLine2, "restore LCD prompt"))
			}
		}
		if model.remote != nil {
			if !model.remoteSnapshotPending && model.remote.Snapshot != nil {
				model.remoteSnapshotPending = true
				commands = append(commands, refreshRemoteSnapshot(
					model.remote.Snapshot, model.remoteStatusSequence, model.remoteLEDSequence,
				))
			}
			if snapshot.Connected && model.page == PageOutputs && !model.pwmPending &&
				time.Since(model.lastPWMRefresh) >= time.Second {
				model.pwmPending = true
				commands = append(commands, execute(model.engine, "pwm get"))
			}
			if snapshot.Connected && model.page == PageRF && !model.rfPending &&
				time.Since(model.rfLastRefresh) >= 2*time.Second {
				model.rfPending = true
				commands = append(commands, model.fetchRFEntriesCommand())
			}
		} else if model.preview == nil {
			if !snapshot.Connected && !snapshot.Paused && !model.connectPending &&
				snapshot.ConnectionState != "reconnecting" &&
				(model.connectRetryAt.IsZero() || !time.Now().Before(model.connectRetryAt)) {
				model.connectPending = true
				commands = append(commands, connect(model.runtime))
			}
			if snapshot.Connected && model.pageNeedsStatus() && !model.statusPending {
				model.statusPending = true
				commands = append(commands, refreshStatus(model.runtime))
			}
			if snapshot.Connected && !model.menuCatalogPending &&
				(!model.menuCatalogLoaded || model.menuCatalogHash != snapshot.Hello.BuildHash) &&
				time.Since(model.menuCatalogLastAttempt) >= 2*time.Second {
				model.menuCatalogPending = true
				model.menuCatalogLastAttempt = time.Now()
				commands = append(commands, refreshMenuCatalog(model.runtime))
			}
			if snapshot.Connected && model.page == PageOutputs && !model.pwmPending &&
				time.Since(model.lastPWMRefresh) >= time.Second {
				model.pwmPending = true
				commands = append(commands, execute(model.engine, "pwm get"))
			}
			if snapshot.Connected && model.page == PageRF && !model.rfPending &&
				time.Since(model.rfLastRefresh) >= 2*time.Second {
				model.rfPending = true
				commands = append(commands, model.fetchRFEntriesCommand())
			}
			if snapshot.Connected && model.page == PageMenus && !model.frontPanelPending &&
				snapshot.Hello.Capabilities&native.CapabilityFrontPanelSnapshot != 0 &&
				time.Since(model.frontPanelLastRefresh) >= 250*time.Millisecond {
				model.frontPanelPending = true
				commands = append(commands, refreshFrontPanel(model.runtime))
			}
		}
		commands = append(commands, tick(model.statusInterval()))

	case welcomeTickMsg:
		if model.welcome {
			model.welcomeFrame++
			if command := model.advanceWelcome(time.Time(message)); command != nil {
				commands = append(commands, command)
			}
			if model.welcome {
				commands = append(commands, welcomeTick())
			}
		}

	case welcomeMelodyResultMsg:
		model.welcomeMelodyPending = false
		if message.err != nil {
			model.welcomeError = "Host welcome melody failed: " + message.err.Error()
			model.welcomePhase = "Setup could not confirm audio completion"
			model.welcomeCanContinue = true
		} else {
			model.welcomePhase = "Controller and host outputs are ready"
			model.finishWelcome()
		}

	case runtimeEventMsg:
		event := control.Event(message)
		if event.Kind == "client.navigation.session.reset" {
			model.navigationCursor.Reset()
			if model.remote != nil && model.remote.Events != nil && !model.remoteEventsClosed {
				commands = append(commands, waitControlEvent(model.remote.Events))
			}
			break
		}
		if command := model.observeUpdateEvent(event); command != nil {
			commands = append(commands, command)
		}
		if operationID := strings.TrimSpace(event.Metadata["operation_id"]); operationID != "" &&
			isAcknowledgedAppActionKind(event.Kind) &&
			hostui.TargetsInstance(event.Metadata["target_instance"], model.instanceID, "tui") {
			value := event.Metadata["value"]
			if strings.EqualFold(event.Kind, "app.page") {
				value = event.Metadata["page"]
			}
			action := hostui.AppAction{
				Kind: event.Kind, Value: value, Source: event.Source,
				Target: event.Metadata["target_instance"], OperationID: operationID,
				Metadata: event.Metadata, At: event.Time,
			}
			commands = append(commands, func() tea.Msg { return appActionMsg(action) })
		}
		if event.Source == "board" && strings.EqualFold(event.Kind, "app.page") &&
			strings.TrimSpace(event.Metadata["operation_id"]) == "" &&
			hostui.TargetsInstance(event.Metadata["target_instance"], model.instanceID, "tui") {
			if page, ok := pageForName(event.Metadata["page"]); ok {
				model.switchPage(page)
				model.setNotice("Board opened " + pageDefinitions[page].Title)
			}
		}
		if strings.EqualFold(event.Kind, "app.page") &&
			strings.TrimSpace(event.Metadata["operation_id"]) == "" &&
			strings.EqualFold(event.Metadata[hostui.NavigationSyncKey], hostui.NavigationSyncGroupUpdate) &&
			hostui.TargetsInstance(event.Metadata["target_instance"], model.instanceID, "tui") &&
			model.navigationSync {
			action := hostui.AppAction{
				Kind: event.Kind, Value: event.Metadata["page"], Source: event.Source,
				Target: event.Metadata["target_instance"], Metadata: event.Metadata,
			}
			if pageName, accepted := model.acceptNavigationAction(action); accepted {
				if page, ok := pageForName(pageName); ok {
					model.applySynchronizedPage(page)
				}
			}
		}
		if model.remote != nil && strings.EqualFold(event.Kind, "app.page") &&
			strings.TrimSpace(event.Metadata["operation_id"]) == "" &&
			!strings.EqualFold(event.Source, "board") &&
			!hostui.HasCoordinatorNavigationMetadata(event.Metadata) &&
			hostui.TargetsInstance(event.Metadata["target_instance"], model.instanceID, "tui") {
			if page, ok := pageForName(event.Metadata["page"]); ok {
				model.switchPage(page)
			}
		}
		if command := model.observeRFGuidedEvent(event); command != nil {
			commands = append(commands, command)
		}
		if command := model.hostMenuDeviceEventCommand(event); command != nil {
			commands = append(commands, command)
		}
		model.recordTimeline(event)
		if model.setFrontPanelEvent(event) && model.lcdMirror && model.mirrorLCD != nil {
			commands = append(commands, mirrorLCDCommand(model.mirrorLCD, model.frontOverlay1, model.frontOverlay2, "priority LCD event"))
		}
		if model.notifier != nil {
			if notification, ok := hostui.NotificationForImportantEvent(hostui.ImportantEvent{Kind: event.Kind, Message: event.Text}); ok {
				commands = append(commands, notifyImportant(model.notifier, notification))
			}
		}
		// Continuous frames and measurements redraw live previews without
		// flooding the operator transcript. Debug mode explicitly opts back in.
		show := model.debug || control.IsActivityEvent(event)
		if !model.debug && event.Frame.Opcode == native.OpHelloResp {
			show = false
		}
		if show {
			model.appendLog(event.Kind, event.Text)
		}
		if model.remote != nil {
			if model.remote.Events != nil && !model.remoteEventsClosed {
				commands = append(commands, waitControlEvent(model.remote.Events))
			}
		} else {
			commands = append(commands, waitControlEvent(model.runtime.Events()))
		}

	case controlEventClosedMsg:
		// A remote event channel is terminal. Do not subscribe to an already
		// closed channel again: receiving from it would complete immediately
		// and spin Bubble Tea's command loop. Snapshot polling remains active
		// and truthfully reports whether command/snapshot IPC can reconnect.
		if model.remote != nil && !model.remoteEventsClosed {
			model.remoteEventsClosed = true
			model.appendLog("warning", "remote IPC event stream closed; live snapshots continue")
		}

	case remoteLiveUpdateMsg:
		update := RemoteLiveUpdate(message)
		if update.ConnectionChange {
			wasConnected := model.remoteLiveConnected
			model.remoteLiveConnected = update.Connected
			if !update.Connected && update.Error != "" && (wasConnected || !model.remoteLiveSeen) {
				model.appendLog("warning", "remote live stream reconnecting: "+update.Error)
			}
			model.remoteLiveSeen = true
		}
		if update.HaveStatus {
			receivedAt := update.StatusReceivedAt
			if receivedAt.IsZero() {
				receivedAt = time.Now()
			}
			updatedAt := update.StatusUpdated
			if updatedAt.IsZero() {
				updatedAt = receivedAt
			}
			model.remoteStatusReceivedAt = receivedAt
			model.remoteStatusSequence++
			model.remoteClockOffset = updatedAt.Sub(receivedAt)
			model.remoteSnapshot.Status = update.Status
			model.remoteSnapshot.HaveStatus = true
			model.remoteSnapshot.StatusUpdated = updatedAt
			sampleSnapshot := model.remoteSnapshot
			sampleSnapshot.StatusUpdated = receivedAt
			model.recordSample(sampleSnapshot)
		}
		if update.HaveStatusLED {
			// A reconnect may replay the last composed frame. Preserve the current
			// phase/timestamp for identical data so the visual never jumps backward.
			if !model.remoteSnapshot.HaveStatusLED || model.remoteSnapshot.StatusLED != update.StatusLED {
				model.remoteSnapshot.StatusLED = update.StatusLED
				model.remoteSnapshot.HaveStatusLED = true
				model.remoteSnapshot.StatusLEDUpdated = update.StatusLEDUpdated
				if model.remoteSnapshot.StatusLEDUpdated.IsZero() {
					model.remoteSnapshot.StatusLEDUpdated = update.StatusLEDReceivedAt
				}
				model.remoteLEDSequence++
			}
		}
		if model.remote != nil && model.remote.Live != nil && !model.remoteLiveClosed {
			commands = append(commands, waitRemoteLiveUpdate(model.remote.Live))
		}

	case remoteLiveClosedMsg:
		if model.remote != nil && !model.remoteLiveClosed {
			model.remoteLiveClosed = true
			model.appendLog("warning", "remote live stream closed; snapshot convergence continues")
		}

	case remoteSnapshotResultMsg:
		model.remoteSnapshotPending = false
		if message.err != nil {
			model.remoteSnapshotError = message.err.Error()
			model.remoteSnapshot = clearDisconnectedPeerState(model.remoteSnapshot)
			model.remoteSnapshot.ConnectionState = "remote IPC unavailable"
			model.remoteSnapshot.ConnectionReason = message.err.Error()
			model.remoteSnapshot.ConnectionUpdated = time.Now()
			break
		}
		wasUnavailable := model.remoteSnapshotError != ""
		acceptStatus := message.statusSequence == model.remoteStatusSequence
		acceptLED := message.ledSequence == model.remoteLEDSequence
		if acceptStatus {
			model.observeRemoteStatus(message.snapshot, message.receivedAt)
		}
		model.remoteSnapshot = mergeRemoteSnapshot(
			model.remoteSnapshot, message.snapshot, acceptStatus, acceptLED,
		)
		if !model.remoteSnapshot.Connected {
			model.remoteSnapshot = clearDisconnectedPeerState(model.remoteSnapshot)
		}
		model.remoteSnapshotError = ""
		if strings.TrimSpace(model.remoteSnapshot.ConnectionState) == "" {
			model.remoteSnapshot.ConnectionState = "remote IPC"
		}
		if wasUnavailable {
			model.appendLog("info", "remote IPC connection restored")
		}

	case commandResultMsg:
		normalizedLine := strings.ToLower(strings.TrimSpace(message.line))
		if normalizedLine == "reconnect" {
			model.connectPending = false
		}
		if strings.EqualFold(strings.TrimSpace(message.line), "reset app") {
			model.rebootPending = false
		}

		if errors.Is(message.err, shell.ErrExit) {
			if model.preview == nil && model.remote == nil {
				_ = model.runtime.Close()
			}
			return model, tea.Quit
		}
		if message.err == nil && message.output == "\x1b[2J\x1b[H" {
			model.logs = nil
			model.updateViewport()
			break
		}
		if message.line == "pwm get" {
			model.pwmPending = false
			model.lastPWMRefresh = time.Now()
		}
		if menuLayoutMutation(message.line) {
			if message.err != nil {
				model.menuLayoutError = message.err.Error()
			} else {
				model.menuLayout = cloneMenuLayout(model.menuLayoutStaged)
				model.menuLayoutOriginal = cloneMenuLayout(model.menuLayoutStaged)
				model.menuLayoutDirty = false
				model.menuLayoutError = ""
				model.menuCatalogLoaded = false
				if !model.menuCatalogPending {
					if model.remote != nil {
						commands = append(commands, execute(model.engine, "menu list"))
					} else {
						model.menuCatalogPending = true
						model.menuCatalogLastAttempt = time.Now()
						commands = append(commands, refreshMenuCatalog(model.runtime))
					}
				}
			}
		}
		model.consumeStructuredResult(message)
		model.appendResult(message.line, message.output, message.err)
		if model.rfGuideActive {
			switch {
			case strings.HasPrefix(normalizedLine, "rf learn ") && message.err != nil:
				model.rfGuidePhase = "interrupted"
				model.setNotice("Guided RF capture could not start: " + message.err.Error())
			case model.rfGuideMappingID >= 0 && rfGuidedMappingCommandMatches(normalizedLine, model.rfGuideMappingID):
				if message.err != nil {
					model.rfGuidePhase = "identity"
					model.setNotice("RF mapping was not saved: " + message.err.Error())
					model.rfGuideMappingID = -1
				} else {
					model = model.completeRFGuidedMapping()
				}
			case strings.HasPrefix(normalizedLine, "rf remove ") && message.err == nil:
				fields := strings.Fields(normalizedLine)
				if len(fields) == 3 && fields[2] == "all" {
					model.rfGuideCaptures = [4]*native.RFEntry{}
					model.rfGuideCandidate = nil
					model.rfGuidePhase = "idle"
					model.setNotice("All learned RF records were cleared")
				} else if len(fields) == 3 {
					if removed, parseErr := strconv.Atoi(fields[2]); parseErr == nil {
						for index, capture := range model.rfGuideCaptures {
							if capture != nil && int(capture.ID) == removed {
								model.rfGuideCaptures[index] = nil
							}
						}
						if model.rfGuideCandidate != nil && int(model.rfGuideCandidate.ID) == removed {
							model.rfGuideCandidate = nil
							model.rfGuidePhase = "idle"
						}
						model.setNotice(fmt.Sprintf("RF entry %d removed; inventory refresh requested", removed))
					}
				}
			}
			model.clearRFGuideArms()
		}
		if message.err == nil && model.preview == nil && model.remote == nil && outputCommandNeedsReadback(message.line) {
			if !model.statusPending {
				model.statusPending = true
				commands = append(commands, refreshStatus(model.runtime))
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(message.line)), "pwm ") &&
				!strings.EqualFold(strings.TrimSpace(message.line), "pwm get") && !model.pwmPending {
				model.pwmPending = true
				commands = append(commands, execute(model.engine, "pwm get"))
			}
		}
		if message.err == nil && (strings.HasPrefix(normalizedLine, "rf map ") || strings.HasPrefix(normalizedLine, "rf remove ")) && model.preview == nil && !model.rfPending {
			model.rfPending = true
			commands = append(commands, model.fetchRFEntriesCommand())
		}

	case terminalOSCResultMsg:
		if message.err != nil {
			model.appendLog("warn", message.kind+": "+message.err.Error())
		} else if message.ack == nil {
			model.setNotice(message.kind + " emitted")
		}
		if message.ack != nil {
			ack := *message.ack
			if message.err != nil {
				ack.State = hostui.ActionStateRejected
				ack.Reason = "terminal_output_unavailable"
			}
			model.rememberActionReceipt(ack)
			commands = append(commands, acknowledgeAppAction(model.ackAppAction, ack))
		}

	case appActionAckResultMsg:
		if message.err != nil {
			model.appendLog("warn", "app action acknowledgement failed: "+message.err.Error())
		}

	case connectResultMsg:
		model.connectPending = false
		if message.err != nil {
			if model.connectRetryDelay <= 0 {
				model.connectRetryDelay = time.Second
			} else {
				model.connectRetryDelay = min(model.connectRetryDelay*2, 30*time.Second)
			}
			model.connectRetryAt = time.Now().Add(model.connectRetryDelay)
			model.portOwner = nil
			var busy *portowner.BusyError
			if errors.As(message.err, &busy) && busy.Owner != nil {
				owner := *busy.Owner
				model.portOwner = &owner
				model.ownerTerminateArmedUntil = time.Time{}
			}
			model.appendLog("warn", "auto-connect: "+message.err.Error())
		} else {
			model.connectRetryAt = time.Time{}
			model.connectRetryDelay = 0
			model.portOwner = nil
			model.ownerTerminateArmedUntil = time.Time{}
			model.setNotice("Port opened and application protocol authenticated")
		}

	case ownerActionResultMsg:
		if message.err != nil {
			model.appendLog("error", message.action+": "+message.err.Error())
			model.setNotice("Owner action failed: " + message.err.Error())
		} else {
			model.appendLog("info", message.action+": completed")
			model.setNotice(message.action + " completed")
			if message.action == "Terminate serial owner" {
				model.portOwner = nil
			}
		}

	case statusResultMsg:
		model.statusPending = false
		if message.err != nil {
			model.appendLog("warn", "status: "+message.err.Error())
		} else {
			snapshot := model.snapshot()
			snapshot.Status = message.status
			snapshot.HaveStatus = true
			snapshot.StatusUpdated = time.Now()
			model.recordSample(snapshot)
		}

	case menuCatalogResultMsg:
		model.menuCatalogPending = false
		if message.err != nil {
			model.menuPages = menuPagesForCapabilities(model.snapshot().Hello.Capabilities)
			model.menuCatalogSource = "host generation fallback · live query failed"
			model.appendLog("warn", "menu catalog: "+message.err.Error())
		} else {
			model.applyMenuCatalog(message.catalog)
		}

	case frontPanelResultMsg:
		model.frontPanelPending = false
		model.frontPanelLastRefresh = time.Now()
		if message.err != nil {
			model.appendLog("warn", "front-panel snapshot: "+message.err.Error())
		}

	case resetResultMsg:
		model.appendResult("reset lines", "DTR/RTS reset pulse sent", message.err)

	case portsResultMsg:
		model.portLoading = false
		model.portCandidates = append([]ports.Info(nil), message.values...)
		model.portError = ""
		if message.err != nil {
			model.portError = message.err.Error()
		}
		if model.portCursor >= len(model.portCandidates) {
			model.portCursor = 0
		}

	case notificationResultMsg:
		if message.err != nil {
			model.appendLog("warn", "desktop notification: "+message.err.Error())
		}

	case rfEntriesResultMsg:
		model.rfPending = false
		model.rfLastRefresh = time.Now()
		if message.err != nil {
			model.rfError = message.err.Error()
			model.appendLog("warn", "RF list: "+message.err.Error())
		} else {
			model.rfError = ""
			model.rfEntries = sortedRFEntries(message.entries)
			if !model.rfStageDirty {
				model.resetRFStage(model.rfEntries)
			}
			model.setNotice(fmt.Sprintf("Loaded %d learned RF codes", len(model.rfEntries)))
			model.resolveRFGuidedCandidate(model.rfEntries)
		}

	case rfOrderResultMsg:
		model.rfPending = false
		model.rfLastRefresh = time.Now()
		if message.err != nil {
			if message.rolledBack {
				model.resetRFStage(message.entries)
				model.appendLog("error", "RF reorder readback failed; original order was restored: "+message.err.Error())
			} else {
				model.appendLog("error", "RF reorder failed: "+message.err.Error())
			}
		} else {
			model.rfEntries = sortedRFEntries(message.entries)
			model.resetRFStage(model.rfEntries)
			model.setNotice("RF order applied and verified by device readback")
		}

	case rfProbeResultMsg:
		model.rfPending = false
		if message.err != nil {
			model.appendLog("error", "safe RF replace probe: "+message.err.Error())
		} else if message.support.Supported {
			model.setNotice("RF record replacement confirmed; press G again to apply the reviewed snapshot")
		} else {
			model.setNotice("Apply remains disabled: " + message.support.Reason)
		}

	case hostMenuResultMsg:
		if message.err != nil {
			model.appendLog("warn", "host menu: "+message.err.Error())
		} else if message.snapshot.Active {
			model.setNotice(fmt.Sprintf("Host menu %s · %s", message.snapshot.MenuTitle, message.snapshot.ItemTitle))
		} else {
			model.setNotice(message.snapshot.Status)
		}
		if command := model.syncHostPanelCommand(); command != nil {
			commands = append(commands, command)
		}

	case hostPanelResultMsg:
		model.hostPanelPending = false
		if message.err != nil {
			model.appendLog("warn", "host front-panel sync: "+message.err.Error())
		} else if message.released {
			model.hostPanelCaptured = false
			model.hostPanelRevision = 0
			model.hostPanelLastPush = time.Time{}
		} else {
			model.hostPanelCaptured = true
			model.hostPanelRevision = message.revision
			model.hostPanelLastPush = time.Now()
		}

	case networkDiscoveryResultMsg:
		model.networkDiscoveryPending = false
		if message.err != nil {
			model.networkDiscoveryError = message.err.Error()
			model.appendLog("warn", "network discovery: "+message.err.Error())
		} else {
			model.networkDiscoveryError = ""
			model.networkDevices = append([]discovery.Instance(nil), message.instances...)
			for index, row := range model.appSettingRows() {
				if strings.HasPrefix(row.Key, "network.device.") {
					model.cursor = index
					break
				}
			}
			model.setNotice(fmt.Sprintf("Discovered %d merged PCController host(s)", len(message.instances)))
		}

	case networkConnectResultMsg:
		if message.err != nil {
			model.appendLog("error", "open network host: "+message.err.Error())
			model.setNotice("Network host was not opened: " + message.err.Error())
		} else {
			model.setNotice("Opened network host " + message.name)
		}

	case spinner.TickMsg:
		if !model.spinnerActive() {
			break
		}
		var command tea.Cmd
		model.spinner, command = model.spinner.Update(message)
		if command != nil {
			commands = append(commands, command)
		}
	}

	if model.terminalIsVisible() {
		var inputCommand tea.Cmd
		model.input, inputCommand = model.input.Update(message)
		commands = append(commands, inputCommand)
		if inputBefore != model.input.Value() {
			model.completion = nil
			model.completionIndex = -1
		}
		if inputBefore != model.input.Value() && model.lcdMirror && model.mirrorLCD != nil {
			state := model.currentFrontPanel(model.snapshot())
			commands = append(commands, mirrorLCDCommand(model.mirrorLCD, state.LCDLine1, state.LCDLine2, "mirror LCD prompt"))
		}
	}
	if model.terminalTitleDirty {
		model.terminalTitleDirty = false
		commands = append(commands, tea.SetWindowTitle(model.terminalTitle()))
		model.reportInstance()
	}
	if len(commands) == 0 {
		return model, nil
	}
	return model, tea.Batch(commands...)
}

func (model Model) applyAppAction(action hostui.AppAction) (Model, []tea.Cmd, bool) {
	var commands []tea.Cmd
	acknowledge := func(state, reason string) {
		if action.OperationID == "" {
			return
		}
		ack := hostui.ActionAck{
			OperationID: action.OperationID, InstanceID: model.instanceID,
			State: state, Reason: reason,
		}
		model.rememberActionReceipt(ack)
		commands = append(commands, acknowledgeAppAction(model.ackAppAction, ack))
	}

	switch action.Kind {
	case "app.page":
		coordinatorNavigation := hostui.HasCoordinatorNavigationMetadata(action.Metadata)
		if coordinatorNavigation {
			if !model.navigationSync {
				acknowledge(hostui.ActionStateRejected, "navigation_sync_disabled")
				return model, commands, false
			}
			pageName, accepted := model.acceptNavigationAction(action)
			if !accepted {
				acknowledge(hostui.ActionStateRejected, "stale_navigation")
				return model, commands, false
			}
			action.Value = pageName
		}
		if page, ok := pageForName(action.Value); ok {
			if coordinatorNavigation || action.OperationID != "" {
				// Both coordinator navigation and exact typed app.page deliveries
				// are remote application, not a fresh local group-navigation intent.
				model.applySynchronizedPage(page)
			} else {
				model.switchPage(page)
			}
			if action.OperationID == "" {
				model.setNotice("Opened " + pageDefinitions[page].Title)
			}
			acknowledge(hostui.ActionStateApplied, "")
		} else {
			model.appendLog("warn", "unknown app page: "+action.Value)
			acknowledge(hostui.ActionStateRejected, "unknown_page")
		}
	case "app.quit":
		return model, commands, true
	case "app.title":
		if strings.EqualFold(action.Value, "auto") {
			model.terminalTitleOverride = ""
		} else {
			model.terminalTitleOverride = action.Value
		}
		model.terminalTitleDirty = true
		if action.OperationID == "" {
			model.setNotice("Terminal title updated")
		}
		acknowledge(hostui.ActionStateApplied, "")
	case "app.osc":
		ack := model.pendingActionAck(action)
		commands = append(commands, terminalOSCCommand(model.writeOSC, action.Value, "OSC", ack))
	case "app.progress":
		progress, err := hostui.ParseTerminalProgress(action.Value)
		if err != nil {
			model.appendLog("warn", "terminal progress: "+err.Error())
			acknowledge(hostui.ActionStateRejected, "invalid_progress")
		} else if payload, payloadErr := progress.OSCPayload(); payloadErr != nil {
			model.appendLog("warn", "terminal progress: "+payloadErr.Error())
			acknowledge(hostui.ActionStateRejected, "invalid_progress")
		} else {
			ack := model.pendingActionAck(action)
			commands = append(commands, terminalOSCCommand(model.writeOSC, payload, "terminal progress", ack))
		}
	case "app.port.open":
		commands = append(commands, execute(model.engine, "port open"))
	case "app.port.close":
		commands = append(commands, execute(model.engine, "port close"))
	case "command":
		if strings.EqualFold(strings.TrimSpace(action.Value), "reset app") {
			model.rebootPending = true
			model.setNotice("Rebooting controller…")
		}
		commands = append(commands, execute(model.engine, action.Value))
	default:
		acknowledge(hostui.ActionStateRejected, "unsupported_action")
	}
	return model, commands, false
}

func isAcknowledgedAppActionKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "app.page", "app.title", "app.progress", "app.osc":
		return true
	default:
		return false
	}
}

func (model Model) pendingActionAck(action hostui.AppAction) *hostui.ActionAck {
	if action.OperationID == "" {
		return nil
	}
	return &hostui.ActionAck{
		OperationID: action.OperationID, InstanceID: model.instanceID,
		State: hostui.ActionStateApplied,
	}
}

func (model *Model) rememberActionReceipt(ack hostui.ActionAck) {
	if model == nil || ack.OperationID == "" {
		return
	}
	if model.actionReceipts == nil {
		model.actionReceipts = make(map[string]hostui.ActionAck)
	}
	if _, exists := model.actionReceipts[ack.OperationID]; !exists {
		model.actionReceiptOrder = append(model.actionReceiptOrder, ack.OperationID)
	}
	model.actionReceipts[ack.OperationID] = ack
	for len(model.actionReceiptOrder) > hostui.MaximumActionOperations {
		oldest := model.actionReceiptOrder[0]
		model.actionReceiptOrder = model.actionReceiptOrder[1:]
		delete(model.actionReceipts, oldest)
	}
}

func waitAppAction(actions <-chan hostui.AppAction) tea.Cmd {
	return func() tea.Msg {
		action, ok := <-actions
		if !ok {
			return appActionClosedMsg{}
		}
		return appActionMsg(action)
	}
}

func pageForName(value string) (Page, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	// Web, URI, notification, and global-hotkey actions use the stable product
	// page names below. Keep those names independent from the compact labels in
	// pageDefinitions so every surface routes the same action deterministically.
	aliases := map[string]Page{
		"controls":  PageOutputs,
		"outputs":   PageOutputs,
		"workbench": PageOutputs,
		"updates":   PageProgramming,
		"settings":  PageAppSettings,
	}
	if page, ok := aliases[value]; ok {
		return page, true
	}
	for index, definition := range pageDefinitions {
		if value == strings.ToLower(definition.Key) ||
			value == strings.ToLower(definition.Short) ||
			value == strings.ToLower(definition.Title) {
			return Page(index), true
		}
	}
	return 0, false
}

func (model Model) View() string {
	if !model.ready {
		return "Starting " + model.prefs.AppTitle + "…"
	}
	if model.welcome {
		return model.welcomeView()
	}
	snapshot := model.snapshot()
	header := model.header(snapshot)
	actions := model.actionBar(snapshot)
	tabs := model.tabBar()
	content := model.pageView(snapshot)
	footer := model.footer()
	parts := []string{header, actions, tabs, content}
	if model.terminalIsVisible() {
		parts = append(parts, model.completionView(), model.commandBar())
	}
	parts = append(parts, footer)
	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().MaxWidth(model.width).Render(view)
}

func (model Model) terminalIsVisible() bool {
	return !model.terminalHidden && (model.page == PageConsole || model.terminalVisible)
}

func (model *Model) resize() {
	if model.width < 56 {
		model.width = 56
	}
	if model.height < 18 {
		model.height = 18
	}
	consoleHeight := model.contentHeight() - 4
	if consoleHeight < 3 {
		consoleHeight = 3
	}
	if !model.ready {
		model.viewport = viewport.New(model.width, consoleHeight)
		model.ready = true
	} else {
		model.viewport.Width = model.width
		model.viewport.Height = consoleHeight
	}
	model.input.Width = model.width - 4
	model.updateViewport()
}

func (model Model) snapshot() control.Snapshot {
	if model.remote != nil {
		return model.remoteSnapshot
	}
	if model.preview != nil {
		return *model.preview
	}
	return model.runtime.Snapshot()
}

func (model *Model) observeRemoteStatus(snapshot control.Snapshot, receivedAt time.Time) {
	if snapshot.StatusUpdated.IsZero() {
		model.remoteStatusReceivedAt = time.Time{}
		model.remoteClockOffset = 0
		return
	}
	if !model.remoteStatusReceivedAt.IsZero() &&
		snapshot.StatusUpdated.Equal(model.remoteSnapshot.StatusUpdated) {
		return
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	model.remoteStatusReceivedAt = receivedAt
	// StatusUpdated arrived over JSON and contains only the source wall clock.
	// Keep it untouched in remoteSnapshot, while recording the observed offset
	// separately so freshness can use this process's monotonic time component.
	model.remoteClockOffset = snapshot.StatusUpdated.Sub(receivedAt)
}

func mergeRemoteSnapshot(current, incoming control.Snapshot, allowStatus, allowLED bool) control.Snapshot {
	if !allowStatus {
		incoming.Status = current.Status
		incoming.HaveStatus = current.HaveStatus
		incoming.StatusUpdated = current.StatusUpdated
	}
	// Missing LED state means this primary has not observed a frame yet; it is
	// not an authoritative black/off frame. Preserve the last known composed
	// value. A real off frame has HaveStatusLED=true and six explicit zero/color
	// fields, and therefore still replaces the current value.
	if !allowLED || (current.HaveStatusLED && !incoming.HaveStatusLED) {
		incoming.StatusLED = current.StatusLED
		incoming.HaveStatusLED = current.HaveStatusLED
		incoming.StatusLEDUpdated = current.StatusLEDUpdated
	}
	return incoming
}

func clearDisconnectedPeerState(snapshot control.Snapshot) control.Snapshot {
	snapshot.Connected = false
	snapshot.Hello = native.Hello{}
	snapshot.Status = native.Status{}
	snapshot.Settings = native.Settings{}
	snapshot.HaveStatus = false
	snapshot.HaveSettings = false
	snapshot.StatusUpdated = time.Time{}
	snapshot.FrontPanel = native.FrontPanel{}
	snapshot.HaveFrontPanel = false
	snapshot.FrontPanelUpdated = time.Time{}
	snapshot.StatusLED = native.StatusLEDState{}
	snapshot.HaveStatusLED = false
	snapshot.StatusLEDUpdated = time.Time{}
	snapshot.RFLearning = control.RFLearnState{}
	return snapshot
}

func (model Model) statusFreshnessLabel(snapshot control.Snapshot, now time.Time) string {
	updated := snapshot.StatusUpdated
	if model.remote != nil && !model.remoteStatusReceivedAt.IsZero() {
		updated = model.remoteStatusReceivedAt
	}
	return freshnessLabel(updated, now)
}

func (model Model) remoteClockWarning() string {
	if model.remote == nil || model.remoteStatusReceivedAt.IsZero() {
		return ""
	}
	return remoteClockSkewWarning(model.remoteClockOffset)
}

func (model Model) rfLearnState() control.RFLearnState {
	if model.remote != nil {
		return model.remoteSnapshot.RFLearning
	}
	if model.preview != nil {
		return model.preview.RFLearning
	}
	return model.runtime.RFLearnState()
}

func (model *Model) finishWelcome() {
	if !model.welcome {
		return
	}
	model.welcome = false
	if model.markWelcomed != nil {
		model.markWelcomed()
	}
}

// advanceWelcome is driven by authenticated HELLO, final STATUS, and the
// firmware-advertised buzzer-busy flag. It never advances merely because an
// animation timer elapsed.
func (model *Model) advanceWelcome(now time.Time) tea.Cmd {
	if model.preview != nil {
		// Preview mode has no transport. Simulate the same named handshake phases
		// deterministically so visual QA can inspect every setup state safely.
		switch {
		case model.welcomeFrame < 5:
			model.welcomePhase = "Preview: authenticating application HELLO"
		case model.welcomeFrame < 12:
			model.welcomePhase = "Preview: board welcome melody is busy"
		case model.welcomeFrame < 20:
			model.welcomePhase = "Preview: awaiting host output scheduler"
		default:
			model.welcomePhase = "Preview handshake complete"
			model.finishWelcome()
		}
		return nil
	}
	if !model.welcomeDeadline.IsZero() && now.After(model.welcomeDeadline) {
		model.welcomePhase = "Setup timed out with a bounded wait"
		if model.welcomeError == "" {
			model.welcomeError = "The controller stayed offline, never sent final READY/STATUS, or audio did not complete."
		}
		model.welcomeCanContinue = true
		return nil
	}
	snapshot := model.snapshot()
	if !snapshot.Connected {
		model.welcomePhase = "Waiting for USB and application HELLO"
		return nil
	}
	if !snapshot.Hello.IsPCController() {
		model.welcomePhase = "USB open · authenticating application HELLO"
		return nil
	}
	if !snapshot.HaveStatus {
		model.welcomePhase = "HELLO accepted · waiting for final READY/STATUS"
		return nil
	}
	if model.welcomeReadyAt.IsZero() {
		model.welcomeReadyAt = now
	}
	busy, known := native.BuzzerBusy(snapshot.Hello, snapshot.Status)
	if known && busy {
		model.welcomeSawBusy = true
		model.welcomePhase = "Controller ready · board welcome melody is playing"
		return nil
	}
	if known && model.welcomeSawBusy {
		model.welcomePhase = "Board welcome melody completed"
		model.finishWelcome()
		return nil
	}
	// If READY arrived after the board queue was already idle (or older firmware
	// lacks cap20), play the configured host melody and await the scheduler's
	// completion instead of guessing from elapsed animation frames.
	grace := 500 * time.Millisecond
	if !known {
		grace = 1200 * time.Millisecond
		model.welcomePhase = "Buzzer state unavailable · applying ready grace"
	} else {
		model.welcomePhase = "Controller ready · buzzer idle"
	}
	if now.Sub(model.welcomeReadyAt) < grace || model.welcomeMelodyPending {
		return nil
	}
	if model.welcomeMelodyStarted {
		return nil
	}
	model.welcomeMelodyStarted = true
	if model.welcomeMelody == nil {
		model.welcomeError = "Configured host welcome melody callback is unavailable."
		model.welcomePhase = "Setup audio could not be started"
		model.welcomeCanContinue = true
		return nil
	}
	model.welcomeMelodyPending = true
	model.welcomePhase = "Playing configured host welcome melody"
	play := model.welcomeMelody
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return welcomeMelodyResultMsg{err: play(ctx)}
	}
}

func (model *Model) syncUIConfig(value appconfig.UI) {
	if model.uiValue.TUIConsole != value.TUIConsole && model.applyTUIConsole != nil {
		if err := model.applyTUIConsole(value.TUIConsole); err != nil {
			model.appendLog("warn", "apply local console settings: "+err.Error())
			model.setNotice("Local console settings were not applied: " + err.Error())
		}
	}
	previousTitle := model.prefs.AppTitle
	model.uiValue = value
	model.prefs = preferencesFromUI(value)
	model.lcdMirror = value.MirrorPromptToLCD
	if previousTitle != model.prefs.AppTitle && model.terminalTitleOverride == "" {
		model.terminalTitleDirty = true
	}
}

func (model *Model) recordSample(snapshot control.Snapshot) {
	status, at := snapshot.Status, snapshot.StatusUpdated
	if at.IsZero() || at.Equal(model.lastSample) ||
		(!model.lastSample.IsZero() && at.Sub(model.lastSample) < 100*time.Millisecond) {
		return
	}
	model.lastSample = at
	if status.PWMChannel < byte(len(model.pwmValues)) {
		model.pwmValues[status.PWMChannel] = status.PWMValue
	}
	model.samples = append(model.samples, measurementSample{
		At: at, SupplyMV: status.SupplyMV, BusMV: status.BusMV,
		CurrentMA: status.CurrentMA, PowerMW: status.PowerMW,
		TLEDCenti: status.TLEDCenti, TBTCenti: status.TBTCenti,
		HaveSupply:  snapshot.Connected && snapshot.HaveStatus && status.INA219Available && validVoltageReading(status.SupplyMV),
		HaveBus:     snapshot.Connected && snapshot.HaveStatus && status.INA219Available && validVoltageReading(status.BusMV),
		HaveCurrent: snapshot.Connected && snapshot.HaveStatus && status.INA219Available && validCurrentReading(status.CurrentMA),
		HavePower:   snapshot.Connected && snapshot.HaveStatus && status.INA219Available && validPowerReading(status.PowerMW),
		HaveTLED:    snapshot.Connected && snapshot.HaveStatus && status.TLEDAvailable && validTemperatureReading(status.TLEDCenti),
		HaveTBT: snapshot.Connected && snapshot.HaveStatus &&
			snapshot.Hello.Capabilities&native.CapabilityBluetoothAudio != 0 &&
			status.TBTAvailable && validTemperatureReading(status.TBTCenti),
	})
	cutoff := at.Add(-model.prefs.HistoryWindow)
	first := 0
	for first < len(model.samples) && model.samples[first].At.Before(cutoff) {
		first++
	}
	if first > 0 {
		model.samples = append([]measurementSample(nil), model.samples[first:]...)
	}
	// A hard ceiling protects long-running 100 ms sessions even if a malformed
	// config requests an excessive history window.
	if len(model.samples) > 1_000_000 {
		model.samples = append([]measurementSample(nil), model.samples[len(model.samples)-1_000_000:]...)
	}
}

func (model *Model) recordTimeline(event control.Event) {
	if (!model.debug && !control.IsActivityEvent(event)) || event.Text == "" {
		return
	}
	important := event.Kind == "door" || event.Kind == "bluetooth" ||
		strings.HasPrefix(event.Kind, "rf") || event.Kind == "reset" ||
		event.Kind == "error" || event.Kind == "connection"
	model.timeline = append(model.timeline, timelineEntry{
		At: event.Time, Kind: event.Kind, Text: event.Text, Important: important,
	})
	if len(model.timeline) > model.eventLogLimit() {
		model.timeline = append([]timelineEntry(nil), model.timeline[len(model.timeline)-model.eventLogLimit():]...)
	}
}

func (model *Model) setFrontPanelEvent(event control.Event) bool {
	kind := strings.ToLower(event.Kind)
	highPriority := event.Kind == "error" || event.Kind == "door" ||
		strings.Contains(kind, "hot") || strings.Contains(kind, "motion") ||
		strings.Contains(kind, "relay") || strings.HasPrefix(kind, "rf")
	if !highPriority || event.Text == "" {
		return false
	}
	model.frontOverlay1 = strings.ToUpper(event.Kind)
	model.frontOverlay2 = event.Text
	hold := time.Duration(model.uiValue.LCDPriorityHoldMS) * time.Millisecond
	if hold < 250*time.Millisecond {
		hold = 2 * time.Second
	}
	model.frontOverlayUntil = time.Now().Add(hold)
	model.frontOverlayNeedsRestore = true
	return true
}

func (model Model) header(snapshot control.Snapshot) string {
	status := "DISCONNECTED"
	style := errorStyle
	detail := "Enter or click to reconnect · background retry armed"
	if model.preview != nil {
		status = "PREVIEW"
		style = warnStyle.Copy().Bold(true)
		detail = "injected board · serial disabled"
	} else if snapshot.Connected {
		status = "CONNECTED"
		style = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
		if snapshot.Hello.IdentitySchema == native.IdentitySchemaCompact {
			detail = fmt.Sprintf(
				"%s · %s build %08X · %s",
				snapshot.Port.Name, snapshot.Hello.Name, snapshot.Hello.BuildHash,
				snapshot.Hello.BuildStamp,
			)
		} else {
			detail = snapshot.Port.Name + " · " + snapshot.Hello.Name
		}
	} else if snapshot.Paused {
		status = "CLOSED"
		detail = "auto-reconnect paused"
	} else if model.connectPending {
		status = model.spinnerView() + " CONNECTING"
		style = warnStyle
		detail = strings.TrimSpace(snapshot.Port.Name + " · " + snapshot.ConnectionReason)
		if detail == "" {
			detail = "authenticated device discovery"
		}
	}
	left := titleStyle.Render("◆ " + model.prefs.AppTitle)
	statusRendered := style.Render(status)
	maxDetail := model.width - lipgloss.Width(left) - lipgloss.Width(statusRendered) - 4
	if maxDetail < 0 {
		maxDetail = 0
	}
	detail = truncateText(detail, maxDetail)
	right := statusRendered
	if detail != "" {
		right += "  " + labelStyle.Render(detail)
	}
	gap := model.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (model Model) connectionCanReconnect(snapshot control.Snapshot) bool {
	return model.preview == nil && !snapshot.Connected && !snapshot.Paused && !model.connectPending
}

type actionBarItem struct {
	label  string
	action string
	style  lipgloss.Style
}

func (item actionBarItem) render() string {
	return item.style.Render(item.label)
}

func (model Model) actionBarItems(snapshot control.Snapshot) []actionBarItem {
	items := []actionBarItem{{label: "P Ports ▼", action: "ports", style: buttonStyle}}
	if model.uiValue.SeparatePortButtons {
		items = append(items,
			actionBarItem{label: "O Open", action: "open", style: buttonGoodStyle},
			actionBarItem{label: "X Close", action: "close", style: buttonStyle},
		)
	} else if snapshot.Connected {
		items = append(items, actionBarItem{label: "X Close", action: "close", style: buttonStyle})
	} else {
		items = append(items, actionBarItem{label: "O Open", action: "open", style: buttonGoodStyle})
	}
	rebootLabel := "R Reboot"
	rebootAction := "reboot"
	if model.rebootPending {
		rebootLabel = model.spinnerView() + " Rebooting"
		rebootAction = ""
	}
	items = append(items,
		actionBarItem{label: rebootLabel, action: rebootAction, style: buttonStyle},
		actionBarItem{label: "↻ Refresh", action: "refresh", style: buttonStyle},
	)
	if model.portOwner != nil && model.width >= 150 {
		items = append(items,
			actionBarItem{label: "^F Owner", action: "owner", style: buttonStyle},
			actionBarItem{label: "^W Ask Close", action: "owner-close", style: buttonBadStyle},
			actionBarItem{label: "^T Terminate", action: "owner-terminate", style: buttonBadStyle},
		)
	}
	return items
}

func (model Model) actionBar(snapshot control.Snapshot) string {
	items := model.actionBarItems(snapshot)
	buttons := make([]string, 0, len(items))
	for _, item := range items {
		buttons = append(buttons, item.render())
	}
	connectionText := "No port owned"
	connectionStyle := labelStyle
	if snapshot.Connected {
		connectionText = snapshot.Port.Name
		if snapshot.Port.FriendlyName != "" && snapshot.Port.FriendlyName != snapshot.Port.Name {
			connectionText += " · " + snapshot.Port.FriendlyName
		}
		if snapshot.Port.VID != "" || snapshot.Port.PID != "" {
			connectionText += fmt.Sprintf(" · %s:%s", snapshot.Port.VID, snapshot.Port.PID)
		}
		connectionStyle = valueStyle
	} else if snapshot.Paused {
		connectionText = "Closed by user"
		connectionStyle = warnStyle
	} else if model.portOwner != nil {
		connectionText = "BUSY · " + model.portOwner.Detail()
		connectionStyle = errorStyle
	}
	controls := lipgloss.JoinHorizontal(lipgloss.Center, intersperseStrings(buttons, " ")...)
	available := model.width - lipgloss.Width(controls) - 3
	connectionText = truncateText(connectionText, available)
	return lipgloss.JoinHorizontal(lipgloss.Center, controls, "   ", connectionStyle.Render(connectionText))
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func (model Model) tabBar() string {
	var rows []string
	var row string
	for page, definition := range pageDefinitions {
		label := definition.Key + " " + definition.Short
		style := inactiveTabStyle
		if Page(page) == model.page {
			style = activeTabStyle
		}
		rendered := style.Render(label)
		separator := " "
		if row != "" && lipgloss.Width(row)+lipgloss.Width(separator)+lipgloss.Width(rendered) > model.width {
			rows = append(rows, row)
			row = rendered
		} else if row == "" {
			row = rendered
		} else {
			row += separator + rendered
		}
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (model Model) commandBar() string {
	return cardStyle.Copy().Width(model.width - 2).Render(model.input.View())
}

func (model Model) completionView() string {
	if len(model.completion) == 0 {
		return ""
	}
	values := append([]string(nil), model.completion...)
	if len(values) > 6 {
		values = values[:6]
	}
	for index := range values {
		if index == model.completionIndex {
			values[index] = selectedStyle.Render(values[index])
		} else {
			values[index] = labelStyle.Render(values[index])
		}
	}
	return "  " + strings.Join(values, "  ")
}

func (model Model) footer() string {
	left := "←/→ tabs or value  ↑/↓ navigate  Enter activate  ~ terminal  Ctrl+C quit  Mouse enabled"
	if model.terminalIsVisible() {
		left = "Terminal visible · ~ hide  Tab/→ complete  Ctrl+C quit  Mouse enabled"
	}
	if model.portOwner != nil {
		left = "Serial busy · Ctrl+F show owner · Ctrl+W ask close · Ctrl+T twice to terminate · primary controller protected"
	}
	if model.notice != "" && time.Now().Before(model.noticeUntil) {
		left = model.notice
	}
	return labelStyle.Render(left)
}

func intersperseStrings(values []string, separator string) []string {
	if len(values) < 2 {
		return values
	}
	result := make([]string, 0, len(values)*2-1)
	for index, value := range values {
		if index != 0 {
			result = append(result, separator)
		}
		result = append(result, value)
	}
	return result
}

func (model *Model) setNotice(value string) {
	model.notice = value
	model.noticeUntil = time.Now().Add(4 * time.Second)
}

func (model Model) pageNeedsStatus() bool {
	switch model.page {
	case PageDashboard, PageOutputs, PageMenus, PageBoardSettings, PageEvents:
		return true
	default:
		return false
	}
}

func listPorts() tea.Cmd {
	return func() tea.Msg {
		values, err := ports.List()
		return portsResultMsg{values: values, err: err}
	}
}

func mirrorLCDCommand(callback func(string, string) error, line1, line2, label string) tea.Cmd {
	return func() tea.Msg {
		return commandResultMsg{line: label, err: callback(padCells(line1, 16), padCells(line2, 16))}
	}
}

func notifyImportant(notifier hostui.Notifier, notification hostui.Notification) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return notificationResultMsg{err: notifier.Notify(ctx, notification)}
	}
}

func (model Model) statusInterval() time.Duration {
	var interval time.Duration
	if !model.pageNeedsStatus() {
		if model.uiValue.IdleStatusIntervalMS >= 100 {
			interval = time.Duration(model.uiValue.IdleStatusIntervalMS) * time.Millisecond
		} else {
			// Keep lightweight UI/reconnect housekeeping without polling STATUS.
			interval = time.Second
		}
	} else {
		interval = model.prefs.PollInterval
		if interval < 100*time.Millisecond {
			interval = 100 * time.Millisecond
		}
		if model.snapshot().Status.DoorOpen && interval > 125*time.Millisecond {
			interval = 125 * time.Millisecond
		}
	}
	// Remote activity events remain push-driven. The snapshot poll is only a
	// convergence/backstop path, so rendering and making an authenticated RPC
	// eight times per second adds load without improving control latency.
	if model.remote != nil && interval < time.Second {
		interval = time.Second
	}
	return interval
}

func (model Model) spinnerActive() bool {
	return model.connectPending || model.rebootPending || model.portLoading ||
		model.remoteSnapshotPending || model.statusPending || model.pwmPending ||
		model.rfPending || model.frontPanelPending || model.networkDiscoveryPending ||
		model.hostPanelPending || model.menuCatalogPending
}

const (
	remoteActiveLiveInterval = 50 * time.Millisecond
	remoteIdleLiveInterval   = time.Second
)

// remoteLiveInterval keeps active board pages at the authenticated status
// stream's supported 20 Hz ceiling. Non-board pages retain a one-second
// convergence sample; state frames such as the status light remain push-driven.
func (model Model) remoteLiveInterval() time.Duration {
	if model.pageNeedsStatus() {
		return remoteActiveLiveInterval
	}
	return remoteIdleLiveInterval
}

// spinnerView advances progress glyphs from wall time instead of running a
// permanent Bubble Tea spinner command. Any real UI event (including the
// bounded status tick) redraws an active operation; an idle connected TUI no
// longer performs a full Lip Gloss render at the spinner's frame rate.
func (model Model) spinnerView() string {
	frames := model.spinner.Spinner.Frames
	if len(frames) == 0 {
		return ""
	}
	interval := model.spinner.Spinner.FPS
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	frame := int(time.Now().UnixNano()/int64(interval)) % len(frames)
	return model.spinner.Style.Render(frames[frame])
}

func (model Model) eventLogLimit() int {
	if model.prefs.EventLogLimit >= 50 {
		return model.prefs.EventLogLimit
	}
	return 500
}

func (model *Model) appendResult(line, output string, err error) {
	if output != "" {
		trimmedOutput := strings.TrimSpace(output)
		helpOutput := trimmedOutput == "Commands:" || strings.HasPrefix(trimmedOutput, "Commands:\n")
		for _, outputLine := range strings.Split(trimmedOutput, "\n") {
			kind := "rx"
			if helpOutput {
				kind = "help"
			}
			model.appendLog(kind, outputLine)
		}
	}
	if err != nil {
		model.appendLog("error", line+": "+err.Error())
		model.setNotice("Command failed: " + err.Error())
	} else if line != "" {
		model.setNotice("Completed: " + line)
	}
}

func (model *Model) appendLog(kind, value string) {
	if value == "" {
		return
	}
	prefix := time.Now().Format("15:04:05.000") + " "
	switch kind {
	case "rx":
		value = labelStyle.Render(prefix+"RX  ") + rxStyle.Render(value)
	case "tx":
		value = labelStyle.Render(prefix+"TX  ") + txStyle.Render(value)
	case "error":
		value = errorStyle.Render(prefix+"ERR ") + value
	case "warn":
		value = warnStyle.Render(prefix+"WRN ") + value
	case "help":
		value = model.styleHelpLine(prefix, value)
	default:
		value = labelStyle.Render(prefix) + value
	}
	model.logs = append(model.logs, value)
	if len(model.logs) > model.eventLogLimit() {
		model.logs = append([]string(nil), model.logs[len(model.logs)-model.eventLogLimit():]...)
	}
	model.updateViewport()
}

func (model Model) styleHelpLine(prefix, line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "Commands:" {
		return labelStyle.Render(prefix) + titleStyle.Render("Command reference")
	}
	if strings.HasSuffix(trimmed, ":") {
		return labelStyle.Render(prefix) + warnStyle.Copy().Bold(true).Render(strings.TrimSuffix(trimmed, ":"))
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	command := fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, command))
	return labelStyle.Render(prefix+"  ") + txStyle.Copy().Bold(true).Render(command) + " " + labelStyle.Render(rest)
}

func (model *Model) updateViewport() {
	if !model.ready {
		return
	}
	atBottom := model.viewport.AtBottom()
	model.viewport.SetContent(strings.Join(model.logs, "\n"))
	if atBottom {
		model.viewport.GotoBottom()
	}
}

func (model *Model) historyMove(delta int) {
	history := model.engine.History()
	if len(history) == 0 {
		return
	}
	if model.historyPos == -1 {
		model.historyBuf = model.input.Value()
		model.historyPos = len(history)
	}
	model.historyPos += delta
	if model.historyPos < 0 {
		model.historyPos = 0
	}
	if model.historyPos >= len(history) {
		model.historyPos = -1
		model.input.SetValue(model.historyBuf)
	} else {
		model.input.SetValue(history[model.historyPos])
	}
	model.input.CursorEnd()
	model.updateInputPlaceholder()
}

func (model *Model) updateInputPlaceholder() {
	history := model.engine.History()
	if len(history) == 0 {
		model.input.Placeholder = "Type a command · Tab/→ completes"
		return
	}
	model.input.Placeholder = history[len(history)-1]
}

func (model *Model) consumeStructuredResult(message commandResultMsg) {
	if message.err != nil || !strings.HasPrefix(message.output, "PWM available=") {
		return
	}
	start := strings.Index(message.output, "values=[")
	end := strings.LastIndex(message.output, "]")
	if start < 0 || end <= start+8 {
		return
	}
	parts := strings.Fields(message.output[start+8 : end])
	if len(parts) != len(model.pwmValues) {
		return
	}
	for index, part := range parts {
		value, err := strconv.ParseUint(part, 10, 16)
		if err != nil || value > 4095 {
			return
		}
		model.pwmValues[index] = uint16(value)
	}
	model.havePWMValues = true
}

func outputCommandNeedsReadback(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range []string{"relay ", "pwm ", "macro ", "rf map ", "automation ", "menu "} {
		if strings.HasPrefix(line, prefix) {
			return line != "pwm get" && line != "relay status" &&
				line != "menu list" && line != "menu current" && line != "menu layout"
		}
	}
	return false
}

func menuLayoutMutation(line string) bool {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	if len(words) < 2 || words[0] != "menu" {
		return false
	}
	if words[1] == "show" || words[1] == "hide" || words[1] == "move" || words[1] == "order" {
		return true
	}
	return len(words) >= 3 && words[1] == "layout" && (words[2] == "set" || words[2] == "reset")
}

func (model *Model) seedPreviewPWM() {
	for index := range model.pwmValues {
		model.pwmValues[index] = uint16(index * 257 % 4096)
	}
	model.havePWMValues = true
}

func tick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(value time.Time) tea.Msg { return tickMsg(value) })
}

func welcomeTick() tea.Cmd {
	return tea.Tick(75*time.Millisecond, func(value time.Time) tea.Msg { return welcomeTickMsg(value) })
}

func waitControlEvent(events <-chan control.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return controlEventClosedMsg{}
		}
		return runtimeEventMsg(event)
	}
}

func waitRemoteLiveUpdate(updates <-chan RemoteLiveUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return remoteLiveClosedMsg{}
		}
		return remoteLiveUpdateMsg(update)
	}
}

func refreshRemoteSnapshot(
	fetch func(context.Context) (control.Snapshot, error),
	statusSequence, ledSequence uint64,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		snapshot, err := fetch(ctx)
		return remoteSnapshotResultMsg{
			snapshot: snapshot, err: err, receivedAt: time.Now(),
			statusSequence: statusSequence, ledSequence: ledSequence,
		}
	}
}

func connect(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return connectResultMsg{err: runtime.EnsureConnected(ctx)}
	}
}

func refreshStatus(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		status, err := runtime.RefreshStatus(ctx)
		return statusResultMsg{status: status, err: err}
	}
}

func refreshMenuCatalog(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		catalog, err := control.QueryMenuCatalog(ctx, runtime)
		return menuCatalogResultMsg{catalog: catalog, err: err}
	}
}

func refreshFrontPanel(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		_, err := runtime.RefreshFrontPanel(ctx)
		return frontPanelResultMsg{err: err}
	}
}

func resetLines(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return resetResultMsg{err: runtime.PulseReset(ctx)}
	}
}

func execute(engine *shell.Engine, line string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		output, err := engine.Execute(ctx, line)
		return commandResultMsg{line: line, output: output, err: err}
	}
}
