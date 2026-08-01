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
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/shell"
)

type Model struct {
	runtime *control.Runtime
	engine  *shell.Engine

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

	historyPos      int
	historyBuf      string
	completion      []string
	completionIndex int

	connectPending           bool
	statusPending            bool
	uiConfig                 func() appconfig.UI
	saveUI                   func(appconfig.UI) error
	uiValue                  appconfig.UI
	hostIntegrations         func() appconfig.Integrations
	saveHostIntegrations     func(appconfig.Integrations) error
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
	prefs                    Preferences
	preview                  *control.Snapshot
	pwmValues                [16]uint16
	havePWMValues            bool
	pwmPending               bool
	lastPWMRefresh           time.Time
	portPicker               bool
	portLoading              bool
	portCandidates           []ports.Info
	portCursor               int
	portError                string
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
	notifier                 hostui.Notifier
	appActions               <-chan hostui.AppAction
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
type commandResultMsg struct {
	line   string
	output string
	err    error
}
type connectResultMsg struct{ err error }
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
		options.MirrorLCD = func(line1, line2 string) error {
			runtime.LCDPresenter().MirrorPrompt(line1, line2)
			return nil
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
			}
			return values[action], nil
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
	model := Model{
		runtime: runtime, engine: engine, input: input, spinner: progress,
		page: PageDashboard, historyPos: -1, uiConfig: options.UIConfig,
		saveUI: options.SaveUI, uiValue: uiValue,
		hostIntegrations:     options.HostIntegrations,
		saveHostIntegrations: options.SaveIntegrations,
		hostIntegrationValue: hostIntegrationValue,
		rfConfig:             options.RFConfig, saveRF: options.SaveRF, rfValue: rfValue,
		rfFetch: options.RFFetch, rfApplyOrder: options.RFApplyOrder,
		rfReplaceSupport: options.RFReplaceSupport, rfProbeReplace: options.RFProbeReplace,
		frontPanel: options.FrontPanel, frontPanelKey: options.FrontPanelKey,
		mirrorLCD: options.MirrorLCD, lcdMirror: uiValue.MirrorPromptToLCD,
		integrations: options.Integrations, notifier: options.Notifier,
		appActions: options.AppActions,
		hostMenus:  options.HostMenus, pushHostPanel: options.PushHostPanel,
		releaseHostPanel: options.ReleaseHostPanel,
		prefs:            prefs, preview: options.Preview, welcome: welcome,
		welcomeStarted: welcomeStarted, welcomeDeadline: welcomeStarted.Add(30 * time.Second),
		welcomePhase: "Waiting for USB and application HELLO", welcomeMelody: options.WelcomeMelody,
		markWelcomed: marker, debug: debug,
		logs: []string{
			productidentity.ServiceName(prefs.AppTitle, "command console ready"),
			"Use the tabs or type help. UI controls use the same validated command paths as CLI and IPC.",
		},
	}
	capabilities := uint32(0)
	if options.Preview != nil {
		capabilities = options.Preview.Hello.Capabilities
	}
	model.menuPages = menuPagesForCapabilities(capabilities)
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
		model.recordSample(options.Preview.Status, options.Preview.StatusUpdated)
	}
	return model
}

func (model Model) Init() tea.Cmd {
	commands := []tea.Cmd{model.spinner.Tick, tick(model.statusInterval())}
	if model.appActions != nil {
		commands = append(commands, waitAppAction(model.appActions))
	}
	if model.welcome {
		commands = append(commands, welcomeTick())
	}
	if model.preview == nil {
		commands = append(commands, waitRuntimeEvent(model.runtime))
	}
	return tea.Batch(commands...)
}

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	inputBefore := model.input.Value()

	switch message := message.(type) {
	case appActionMsg:
		action := hostui.AppAction(message)
		switch action.Kind {
		case "app.page":
			if page, ok := pageForName(action.Value); ok {
				model.switchPage(page)
				model.setNotice("Opened " + pageDefinitions[page].Title)
			} else {
				model.appendLog("warn", "unknown app page: "+action.Value)
			}
		case "app.quit":
			return model, tea.Quit
		case "app.port.open":
			commands = append(commands, execute(model.engine, "port open"))
		case "app.port.close":
			commands = append(commands, execute(model.engine, "port close"))
		case "command":
			commands = append(commands, execute(model.engine, action.Value))
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
		model.recordSample(snapshot.Status, snapshot.StatusUpdated)
		if model.frontOverlayNeedsRestore && time.Now().After(model.frontOverlayUntil) {
			model.frontOverlayNeedsRestore = false
			model.frontOverlay1, model.frontOverlay2 = "", ""
			if model.lcdMirror && model.mirrorLCD != nil {
				state := model.currentFrontPanel(snapshot)
				commands = append(commands, mirrorLCDCommand(model.mirrorLCD, state.LCDLine1, state.LCDLine2, "restore LCD prompt"))
			}
		}
		if model.preview == nil {
			if !snapshot.Connected && !snapshot.Paused && !model.connectPending {
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
		// Telemetry updates redraw cards without flooding the transcript. HELLO
		// payload bytes are diagnostic-only and stay hidden unless debug is on.
		show := event.Kind != "telemetry"
		if !model.debug && event.Frame.Opcode == native.OpHelloResp {
			show = false
		}
		if show {
			model.appendLog(event.Kind, event.Text)
		}
		commands = append(commands, waitRuntimeEvent(model.runtime))

	case commandResultMsg:
		if errors.Is(message.err, shell.ErrExit) {
			if model.preview == nil {
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
					model.menuCatalogPending = true
					model.menuCatalogLastAttempt = time.Now()
					commands = append(commands, refreshMenuCatalog(model.runtime))
				}
			}
		}
		model.consumeStructuredResult(message)
		model.appendResult(message.line, message.output, message.err)
		if message.err == nil && model.preview == nil && outputCommandNeedsReadback(message.line) {
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
		if message.err == nil && strings.HasPrefix(strings.ToLower(message.line), "rf map ") && model.preview == nil && !model.rfPending {
			model.rfPending = true
			commands = append(commands, model.fetchRFEntriesCommand())
		}

	case connectResultMsg:
		model.connectPending = false
		if message.err != nil {
			model.appendLog("warn", "auto-connect: "+message.err.Error())
		} else {
			model.setNotice("Port opened and application protocol authenticated")
		}

	case statusResultMsg:
		model.statusPending = false
		if message.err != nil {
			model.appendLog("warn", "status: "+message.err.Error())
		} else {
			model.recordSample(message.status, time.Now())
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

	case spinner.TickMsg:
		var command tea.Cmd
		model.spinner, command = model.spinner.Update(message)
		commands = append(commands, command)
	}

	var inputCommand tea.Cmd
	model.input, inputCommand = model.input.Update(message)
	commands = append(commands, inputCommand)
	if inputBefore != model.input.Value() && model.lcdMirror && model.mirrorLCD != nil {
		state := model.currentFrontPanel(model.snapshot())
		commands = append(commands, mirrorLCDCommand(model.mirrorLCD, state.LCDLine1, state.LCDLine2, "mirror LCD prompt"))
	}
	return model, tea.Batch(commands...)
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
	completion := model.completionView()
	commandBar := model.commandBar()
	footer := model.footer()
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		actions,
		tabs,
		content,
		completion,
		commandBar,
		footer,
	)
	return lipgloss.NewStyle().MaxWidth(model.width).Render(view)
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
	if model.preview != nil {
		return *model.preview
	}
	return model.runtime.Snapshot()
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
		model.welcomePhase = "Legacy buzzer telemetry · applying safe ready grace"
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
	model.uiValue = value
	model.prefs = preferencesFromUI(value)
	model.lcdMirror = value.MirrorPromptToLCD
}

func (model *Model) recordSample(status native.Status, at time.Time) {
	if at.IsZero() || at.Equal(model.lastSample) {
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
	if event.Kind == "telemetry" || event.Text == "" {
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
	detail := "authenticated device discovery"
	if model.preview != nil {
		status = "PREVIEW"
		style = warnStyle.Copy().Bold(true)
		detail = "injected board · serial disabled"
	} else if snapshot.Connected {
		status = "CONNECTED"
		style = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
		if snapshot.Hello.IdentitySchema == native.IdentitySchema {
			detail = fmt.Sprintf(
				"%s · %s build %08X · %s",
				snapshot.Port.Name, snapshot.Hello.Name, snapshot.Hello.BuildHash,
				snapshot.Hello.BuildStamp,
			)
		} else if snapshot.Hello.IdentitySchema == native.IdentitySchemaLegacy {
			detail = fmt.Sprintf(
				"%s · %s build %08X · %s %s",
				snapshot.Port.Name, snapshot.Hello.Name, snapshot.Hello.BuildHash,
				strings.TrimSpace(snapshot.Hello.BuildDate), strings.TrimSpace(snapshot.Hello.BuildTime),
			)
		} else {
			detail = snapshot.Port.Name + " · " + snapshot.Hello.Name
		}
	} else if snapshot.Paused {
		status = "CLOSED"
		detail = "auto-reconnect paused"
	} else if snapshot.ConnectionState == "reconnecting" {
		status = model.spinner.View() + " RECONNECTING"
		style = warnStyle
		detail = strings.TrimSpace(snapshot.Port.Name + " · " + snapshot.ConnectionReason)
	} else if model.connectPending {
		status = model.spinner.View() + " SCANNING"
		style = warnStyle
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

func (model Model) actionBar(snapshot control.Snapshot) string {
	open := buttonGoodStyle.Render("O Open")
	portsButton := buttonStyle.Render("P Ports ▼")
	closeButton := buttonStyle.Render("X Close")
	reset := buttonBadStyle.Render("R Safe Reset")
	refresh := buttonStyle.Render("↻ Refresh")
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
	}
	controls := lipgloss.JoinHorizontal(lipgloss.Center, portsButton, " ", open, " ", closeButton, " ", reset, " ", refresh)
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
	values := model.completion
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
	left := "←/→ tabs or value  ↑/↓ navigate  Enter activate  Tab/→ complete  Ctrl+C quit  Mouse enabled"
	if model.notice != "" && time.Now().Before(model.noticeUntil) {
		left = model.notice
	}
	return labelStyle.Render(left)
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
	if !model.pageNeedsStatus() {
		if model.uiValue.IdleStatusIntervalMS >= 100 {
			return time.Duration(model.uiValue.IdleStatusIntervalMS) * time.Millisecond
		}
		// Keep lightweight UI/reconnect housekeeping without polling STATUS.
		return time.Second
	}
	interval := model.prefs.PollInterval
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	if model.snapshot().Status.DoorOpen && interval > 125*time.Millisecond && model.pageNeedsStatus() {
		return 125 * time.Millisecond
	}
	return interval
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
	if message.err != nil || !strings.HasPrefix(message.output, "PWM mode=") {
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

func waitRuntimeEvent(runtime *control.Runtime) tea.Cmd {
	return func() tea.Msg { return runtimeEventMsg(<-runtime.Events()) }
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
