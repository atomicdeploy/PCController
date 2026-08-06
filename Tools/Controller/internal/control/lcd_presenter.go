package control

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/native"
)

type LCDPresentationOptions struct {
	Enabled      bool          `json:"enabled"`
	Debounce     time.Duration `json:"debounce"`
	PriorityHold time.Duration `json:"priority_hold"`
}

type LCDPresentationState struct {
	Enabled        bool      `json:"enabled"`
	PromptLine1    string    `json:"prompt_line1"`
	PromptLine2    string    `json:"prompt_line2"`
	ActiveLine1    string    `json:"active_line1"`
	ActiveLine2    string    `json:"active_line2"`
	PriorityKind   string    `json:"priority_kind,omitempty"`
	PriorityUntil  time.Time `json:"priority_until,omitempty"`
	FirmwareMirror bool      `json:"firmware_mirror_confirmed"`
	FirmwareLine1  string    `json:"firmware_line1,omitempty"`
	FirmwareLine2  string    `json:"firmware_line2,omitempty"`
	Physical       bool      `json:"physical_available"`
	Address        byte      `json:"physical_address,omitempty"`
	PhysicalLine1  string    `json:"physical_line1,omitempty"`
	PhysicalLine2  string    `json:"physical_line2,omitempty"`
	LastError      string    `json:"physical_error,omitempty"`
}

type LCDPresenter struct {
	runtime *Runtime

	mu             sync.RWMutex
	sendToken      chan struct{}
	options        LCDPresentationOptions
	prompt         [2]string
	active         [2]string
	firmwareLines  [2]string
	firmwareDevice string
	priorityKind   string
	priorityRank   int
	priorityUntil  time.Time
	version        uint64
	debounceTimer  *time.Timer
	priorityTimer  *time.Timer
	runOnce        sync.Once
	physical       *hostOwnedLCD
	physicalError  string
}

func NewLCDPresenter(runtime *Runtime) *LCDPresenter {
	presenter := &LCDPresenter{
		runtime: runtime, sendToken: make(chan struct{}, 1),
		options: LCDPresentationOptions{
			Debounce: 120 * time.Millisecond, PriorityHold: 2 * time.Second,
		},
	}
	presenter.physical = newHostOwnedLCD(func(
		ctx context.Context,
		address, leaseSeconds byte,
		write []byte,
		readLength byte,
	) (native.I2CTransferResult, error) {
		return TransferI2C(ctx, runtime, address, leaseSeconds, write, readLength)
	})
	return presenter
}

func (presenter *LCDPresenter) acquireSend(ctx context.Context) error {
	select {
	case presenter.sendToken <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (presenter *LCDPresenter) releaseSend() {
	<-presenter.sendToken
}

func (presenter *LCDPresenter) Run(ctx context.Context) {
	if presenter.runtime == nil {
		return
	}
	afterID := presenter.runtime.LatestEventID()
	// Configure commonly runs before USB authentication. This initial redraw
	// closes the race where connect was published before this goroutine started.
	presenter.redraw()
	for {
		event, err := presenter.runtime.WaitEvent(ctx, afterID, "")
		if err != nil {
			return
		}
		afterID = event.ID
		presenter.ObserveEvent(event)
	}
}

func (presenter *LCDPresenter) Configure(options LCDPresentationOptions) error {
	if options.Debounce == 0 {
		options.Debounce = 120 * time.Millisecond
	}
	if options.PriorityHold == 0 {
		options.PriorityHold = 2 * time.Second
	}
	if options.Debounce < 20*time.Millisecond || options.Debounce > 5*time.Second {
		return fmt.Errorf("LCD prompt debounce must be 20ms..5s")
	}
	if options.PriorityHold < 250*time.Millisecond || options.PriorityHold > time.Minute {
		return fmt.Errorf("LCD priority hold must be 250ms..1m")
	}
	presenter.mu.Lock()
	presenter.options = options
	presenter.version++
	version := presenter.version
	if presenter.debounceTimer != nil {
		presenter.debounceTimer.Stop()
	}
	if presenter.priorityTimer != nil {
		presenter.priorityTimer.Stop()
	}
	presenter.priorityKind = ""
	presenter.priorityRank = 0
	presenter.priorityUntil = time.Time{}
	presenter.active = presenter.prompt
	presenter.firmwareLines = [2]string{}
	presenter.firmwareDevice = ""
	prompt := presenter.prompt
	presenter.mu.Unlock()
	if options.Enabled {
		presenter.runOnce.Do(func() {
			go presenter.Run(context.Background())
		})
		presenter.send(version, prompt[0], prompt[1])
	} else if presenter.physical != nil {
		presenter.physical.reset()
		presenter.ReportPhysicalError("HOST-controlled LCD", nil)
	}
	return nil
}

func (presenter *LCDPresenter) State() LCDPresentationState {
	presenter.mu.RLock()
	defer presenter.mu.RUnlock()
	state := LCDPresentationState{
		Enabled:     presenter.options.Enabled,
		PromptLine1: presenter.prompt[0], PromptLine2: presenter.prompt[1],
		ActiveLine1: presenter.active[0], ActiveLine2: presenter.active[1],
		PriorityKind:   presenter.priorityKind,
		PriorityUntil:  presenter.priorityUntil,
		FirmwareMirror: presenter.firmwareDevice != "",
		FirmwareLine1:  presenter.firmwareLines[0],
		FirmwareLine2:  presenter.firmwareLines[1],
	}
	if presenter.physical != nil {
		physical := presenter.physical.state()
		state.Physical = physical.Available
		state.Address = physical.Address
		state.PhysicalLine1 = physical.Lines[0]
		state.PhysicalLine2 = physical.Lines[1]
		state.LastError = physical.LastError
	}
	return state
}

// ReportPhysicalError publishes only state changes. A missing optional LCD is
// kept visible in LCDPresentationState while background probes stay quiet.
func (presenter *LCDPresenter) ReportPhysicalError(scope string, err error) bool {
	message := ""
	if err != nil {
		message = err.Error()
	}
	presenter.mu.Lock()
	if message == presenter.physicalError {
		presenter.mu.Unlock()
		return false
	}
	presenter.physicalError = message
	presenter.mu.Unlock()
	if message != "" && presenter.runtime != nil {
		presenter.runtime.PublishHostEvent("lcd.error", scope+": "+message)
	}
	return true
}

// RescanPhysical forgets the cached backpack address and immediately probes
// the two supported PCF8574 addresses using the current prompt contents.
func (presenter *LCDPresenter) RescanPhysical(ctx context.Context) (byte, error) {
	if presenter.runtime == nil || presenter.physical == nil {
		return 0, fmt.Errorf("HOST-controlled LCD renderer is unavailable")
	}
	snapshot := presenter.runtime.Snapshot()
	if !snapshot.Connected {
		return 0, fmt.Errorf("device is not connected")
	}
	if snapshot.Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
		return snapshot.Status.LCDAddress, fmt.Errorf("connected firmware owns LCD rendering; host I2C renderer is not active")
	}
	presenter.mu.RLock()
	prompt := presenter.prompt
	presenter.mu.RUnlock()
	presenter.physical.reset()
	if err := presenter.physical.render(ctx, lcdDeviceKey(snapshot), prompt[0], prompt[1]); err != nil {
		return 0, err
	}
	return presenter.physical.state().Address, nil
}

func (presenter *LCDPresenter) MirrorPrompt(line1, line2 string) {
	line1, line2 = lcdLine(line1), lcdLine(line2)
	presenter.mu.Lock()
	presenter.prompt = [2]string{line1, line2}
	if !presenter.options.Enabled {
		presenter.mu.Unlock()
		return
	}
	now := time.Now()
	if !presenter.priorityUntil.IsZero() && now.Before(presenter.priorityUntil) {
		// A prompt edit must not invalidate the active priority timer. The newest
		// prompt is read when that timer restores normal presentation.
		presenter.mu.Unlock()
		return
	}
	if presenter.priorityKind != "" {
		presenter.priorityKind = ""
		presenter.priorityRank = 0
		presenter.priorityUntil = time.Time{}
		if presenter.priorityTimer != nil {
			presenter.priorityTimer.Stop()
		}
	}
	presenter.version++
	version := presenter.version
	presenter.active = presenter.prompt
	if presenter.debounceTimer != nil {
		presenter.debounceTimer.Stop()
	}
	delay := presenter.options.Debounce
	presenter.debounceTimer = time.AfterFunc(delay, func() {
		presenter.send(version, line1, line2)
	})
	presenter.mu.Unlock()
}

func (presenter *LCDPresenter) ShowPriority(
	kind, line1, line2 string,
	hold time.Duration,
) bool {
	rank := lcdPriorityRank(kind)
	if rank == 0 {
		return false
	}
	line1, line2 = lcdLine(line1), lcdLine(line2)
	presenter.mu.Lock()
	if !presenter.options.Enabled {
		presenter.mu.Unlock()
		return false
	}
	now := time.Now()
	if now.Before(presenter.priorityUntil) && rank < presenter.priorityRank {
		presenter.mu.Unlock()
		return false
	}
	if hold <= 0 {
		hold = presenter.options.PriorityHold
	}
	presenter.version++
	version := presenter.version
	presenter.priorityKind = strings.ToLower(strings.TrimSpace(kind))
	presenter.priorityRank = rank
	presenter.priorityUntil = now.Add(hold)
	presenter.active = [2]string{line1, line2}
	if presenter.debounceTimer != nil {
		presenter.debounceTimer.Stop()
	}
	if presenter.priorityTimer != nil {
		presenter.priorityTimer.Stop()
	}
	presenter.priorityTimer = time.AfterFunc(hold, func() {
		presenter.restorePrompt(version)
	})
	presenter.mu.Unlock()
	presenter.send(version, line1, line2)
	return true
}

func (presenter *LCDPresenter) ObserveEvent(event Event) {
	if event.Kind == "connection" {
		switch event.Lifecycle {
		case "disconnect", "reconnecting":
			presenter.clearConnectionState()
		case "connect", "reconnected":
			presenter.clearConnectionState()
			presenter.redraw()
		}
		return
	}
	switch event.Kind {
	case "rx":
		if event.Frame.Opcode == native.OpStatus || event.Frame.Opcode == native.OpFrontPanel {
			presenter.ensurePhysicalHome()
		}
	case "error":
		presenter.ShowPriority("error", "ERROR", event.Text, 0)
	case "door":
		presenter.ShowPriority("door", strings.ToUpper(event.Text), "", 0)
	case "relay", "motion", "macro":
		presenter.ShowPriority(event.Kind, strings.ToUpper(event.Kind), event.Text, 0)
	case "rf.receive", "rf.learn", "rf.learn.ended":
		presenter.ShowPriority("rf", "RF CONTROL", event.Text, 0)
	case "bluetooth":
		presenter.ShowPriority("bluetooth", "BT AUDIO", event.Text, 0)
	case "operational", "operation.state", "program.state":
		// Idle/Running is an explicit HOST-owned state. It is never inferred from
		// the enclosure input; the host state manager publishes this event.
		state := strings.TrimSpace(event.State)
		if state == "" {
			state = event.Text
		}
		if event.Reason != "" {
			state += " - " + event.Reason
		}
		presenter.ShowPriority("operational", "OPERATION", strings.ToUpper(state), 0)
	case "warning":
		presenter.ShowPriority("warning", "WARNING", event.Text, 0)
	}
}

func (presenter *LCDPresenter) ensurePhysicalHome() {
	presenter.mu.RLock()
	enabled := presenter.options.Enabled
	presenter.mu.RUnlock()
	if !enabled || presenter.runtime == nil || presenter.physical == nil {
		return
	}
	snapshot := presenter.runtime.Snapshot()
	if !snapshot.Connected || snapshot.Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := presenter.acquireSend(ctx); err != nil {
		return
	}
	defer presenter.releaseSend()
	if err := presenter.physical.ensureHome(ctx, lcdDeviceKey(snapshot)); err != nil {
		presenter.runtime.PublishHostEvent("lcd.error", "HOST-controlled LCD home: "+err.Error())
	}
}

func (presenter *LCDPresenter) restorePrompt(version uint64) {
	presenter.mu.Lock()
	if presenter.version != version {
		presenter.mu.Unlock()
		return
	}
	presenter.priorityKind = ""
	presenter.priorityRank = 0
	presenter.priorityUntil = time.Time{}
	presenter.version++
	nextVersion := presenter.version
	prompt := presenter.prompt
	presenter.active = prompt
	presenter.mu.Unlock()
	presenter.send(nextVersion, prompt[0], prompt[1])
}

func (presenter *LCDPresenter) clearConnectionState() {
	presenter.mu.Lock()
	presenter.firmwareLines = [2]string{}
	presenter.firmwareDevice = ""
	presenter.physicalError = ""
	presenter.mu.Unlock()
	if presenter.physical != nil {
		presenter.physical.reset()
	}
}

func (presenter *LCDPresenter) redraw() {
	presenter.mu.Lock()
	if presenter.priorityKind != "" && !time.Now().Before(presenter.priorityUntil) {
		presenter.priorityKind = ""
		presenter.priorityRank = 0
		presenter.priorityUntil = time.Time{}
		presenter.version++
		presenter.active = presenter.prompt
	}
	version := presenter.version
	lines := presenter.active
	presenter.mu.Unlock()
	presenter.send(version, lines[0], lines[1])
}

func (presenter *LCDPresenter) send(version uint64, line1, line2 string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := presenter.acquireSend(ctx); err != nil {
		return
	}
	defer presenter.releaseSend()
	presenter.mu.RLock()
	enabled := presenter.options.Enabled && version == presenter.version
	presenter.mu.RUnlock()
	if !enabled || presenter.runtime == nil || !presenter.runtime.Snapshot().Connected {
		return
	}
	payload, err := native.DisplayTextPayload(
		native.DisplayLCD,
		0,
		lcdLine(line1)+lcdLine(line2),
	)
	if err != nil {
		presenter.runtime.PublishHostEvent("lcd.error", "LCD presentation: "+err.Error())
		return
	}
	line1, line2 = lcdLine(line1), lcdLine(line2)
	err = presenter.runtime.Command(ctx, native.OpDisplayText, payload)
	if err != nil {
		// Keep transport failures out of the generic error priority path: feeding
		// them back into the presenter would recursively retry an unavailable LCD.
		presenter.runtime.PublishHostEvent("lcd.error", "LCD presentation: "+err.Error())
		return
	}
	snapshot := presenter.runtime.Snapshot()
	device := lcdDeviceKey(snapshot)
	presenter.mu.Lock()
	current := presenter.options.Enabled && version == presenter.version && snapshot.Connected
	if current {
		presenter.firmwareLines = [2]string{line1, line2}
		presenter.firmwareDevice = device
	}
	presenter.mu.Unlock()
	if !current {
		return
	}
	if snapshot.Hello.Capabilities&native.CapabilityI2CTransfer != 0 && presenter.physical != nil {
		err = presenter.physical.render(ctx, device, line1, line2)
		presenter.ReportPhysicalError("HOST-controlled LCD", err)
	}
}

// RenderPhysical writes lines already mirrored through another display opcode
// (for example a captured host menu) to the cap16 HOST-controlled backpack.
func (presenter *LCDPresenter) RenderPhysical(ctx context.Context, line1, line2 string) error {
	if presenter.runtime == nil || presenter.physical == nil {
		return fmt.Errorf("HOST-controlled LCD renderer is unavailable")
	}
	presenter.mu.RLock()
	enabled := presenter.options.Enabled
	presenter.mu.RUnlock()
	if !enabled {
		return nil
	}
	if err := presenter.acquireSend(ctx); err != nil {
		return err
	}
	defer presenter.releaseSend()
	snapshot := presenter.runtime.Snapshot()
	if !snapshot.Connected || snapshot.Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
		return fmt.Errorf("connected firmware does not expose the HOST-controlled LCD transport")
	}
	return presenter.physical.render(ctx, lcdDeviceKey(snapshot), line1, line2)
}

// PrepareDisconnect presents the fixed offline fallback while UART/I2C is
// still available. Abrupt cable loss cannot perform this host write; cap16
// firmware may instead reveal the preloaded hidden page, while older firmware
// leaves its last-confirmed physical contents unverified.
func (presenter *LCDPresenter) PrepareDisconnect(ctx context.Context) error {
	if presenter.runtime == nil {
		return fmt.Errorf("LCD runtime is unavailable")
	}
	presenter.mu.RLock()
	enabled := presenter.options.Enabled
	presenter.mu.RUnlock()
	if !enabled {
		return nil
	}
	const line1 = lcdOfflineLine1
	const line2 = lcdOfflineLine2
	if err := presenter.acquireSend(ctx); err != nil {
		return err
	}
	defer presenter.releaseSend()
	snapshot := presenter.runtime.Snapshot()
	if !snapshot.Connected {
		return fmt.Errorf("cannot present LCD offline fallback after disconnect")
	}
	payload, err := native.DisplayTextPayload(
		native.DisplayLCD, 0, lcdLine(line1)+lcdLine(line2),
	)
	if err != nil {
		return err
	}
	if err = presenter.runtime.Command(ctx, native.OpDisplayText, payload); err != nil {
		return err
	}
	if snapshot.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if presenter.physical == nil {
			return fmt.Errorf("HOST-controlled LCD renderer is unavailable")
		}
		if err = presenter.physical.render(ctx, lcdDeviceKey(snapshot), line1, line2); err != nil {
			return err
		}
	}
	presenter.mu.Lock()
	presenter.firmwareLines = [2]string{lcdLine(line1), lcdLine(line2)}
	presenter.firmwareDevice = lcdDeviceKey(snapshot)
	presenter.mu.Unlock()
	return nil
}

func lcdDeviceKey(snapshot Snapshot) string {
	return fmt.Sprintf(
		"%s|%s|%08X|%d",
		snapshot.Port.Name,
		snapshot.Port.InstanceID,
		snapshot.Hello.BuildHash,
		snapshot.ConnectionUpdated.UnixNano(),
	)
}

func lcdLine(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if builder.Len() >= 16 {
			break
		}
		if character < 0x20 || character > 0x7E {
			character = '?'
		}
		builder.WriteRune(character)
	}
	for builder.Len() < 16 {
		builder.WriteByte(' ')
	}
	return builder.String()
}

func lcdPriorityRank(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "error", "hot":
		return 100
	case "warning":
		return 90
	case "motion":
		return 80
	case "door", "relay", "rf", "macro":
		return 60
	case "operational", "operation.state", "program.state", "bluetooth":
		return 40
	default:
		return 0
	}
}
