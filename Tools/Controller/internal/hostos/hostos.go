// Package hostos implements explicitly policy-gated host operating-system actions.
package hostos

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Policy struct {
	VirtualKeys VirtualKeyPolicy `json:"virtual_keys"`
	Power       PowerPolicy      `json:"power"`
	Brightness  BrightnessPolicy `json:"brightness"`
}

type VirtualKeyPolicy struct {
	Enabled       bool     `json:"enabled"`
	Allowed       []string `json:"allowed"`
	MinIntervalMS int      `json:"min_interval_ms"`
	HoldMS        int      `json:"hold_ms"`
}

type PowerPolicy struct {
	Enabled             bool     `json:"enabled"`
	Allowed             []string `json:"allowed"`
	RequireConfirmation bool     `json:"require_confirmation"`
	ConfirmationToken   string   `json:"confirmation_token"`
	AllowAutomation     bool     `json:"allow_automation"`
}

// BrightnessPolicy gates DDC/CI writes independently from harmless reads.
// It is PC-side configuration and is never written to the controller EEPROM.
type BrightnessPolicy struct {
	Enabled    bool `json:"enabled"`
	MinPercent int  `json:"min_percent"`
	MaxPercent int  `json:"max_percent"`
}

type VirtualKeyRequest struct {
	Key    string `json:"key"`
	HoldMS int    `json:"hold_ms,omitempty"`
}

type PowerRequest struct {
	Action       string `json:"action"`
	Confirmation string `json:"confirmation,omitempty"`
	Automation   bool   `json:"automation,omitempty"`
}

type ActionResult struct {
	Action string    `json:"action"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// BrightnessStatus is the primary display's DDC/CI brightness normalized to
// 0..100 while retaining the monitor's native range for diagnostics.
type BrightnessStatus struct {
	Percent    int    `json:"percent"`
	RawMinimum uint32 `json:"raw_minimum"`
	RawCurrent uint32 `json:"raw_current"`
	RawMaximum uint32 `json:"raw_maximum"`
	Display    string `json:"display,omitempty"`
}

// BrightnessResult is suitable for the CLI, event bus, IPC, and API layers.
type BrightnessResult struct {
	Status  BrightnessStatus `json:"status"`
	Changed bool             `json:"changed"`
	At      time.Time        `json:"at"`
}

// BrightnessBackend makes platform brightness access injectable without
// weakening the production policy gate. Tests use a deterministic fake while
// Windows uses the DDC/CI physical-monitor APIs.
type BrightnessBackend interface {
	Brightness(context.Context) (BrightnessStatus, error)
	SetBrightness(context.Context, int) (BrightnessStatus, error)
}

type NetworkAddress struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Loopback  bool   `json:"loopback"`
}

type SystemStatus struct {
	Hostname         string           `json:"hostname"`
	OperatingSystem  string           `json:"operating_system"`
	Architecture     string           `json:"architecture"`
	LogicalCPUs      int              `json:"logical_cpus"`
	ProcessID        int              `json:"process_id"`
	HostUptimeMS     uint64           `json:"host_uptime_ms,omitempty"`
	NetworkAddresses []NetworkAddress `json:"network_addresses"`
	DiscoverySource  string           `json:"serial_discovery_source,omitempty"`
	CollectedAt      time.Time        `json:"collected_at"`
}

func DefaultPolicy() Policy {
	return Policy{
		VirtualKeys: VirtualKeyPolicy{
			Allowed:       []string{"UP", "DOWN", "LEFT", "RIGHT", "F13"},
			MinIntervalMS: 120,
			HoldMS:        30,
		},
		Power: PowerPolicy{
			Allowed:             []string{"lock", "sleep", "hibernate"},
			RequireConfirmation: true,
			ConfirmationToken:   "CONFIRM",
		},
		Brightness: BrightnessPolicy{MinPercent: 0, MaxPercent: 100},
	}
}

func ClonePolicy(value Policy) Policy {
	value.VirtualKeys.Allowed = append([]string(nil), value.VirtualKeys.Allowed...)
	value.Power.Allowed = append([]string(nil), value.Power.Allowed...)
	return value
}

func ValidatePolicy(value Policy) error {
	if value.VirtualKeys.MinIntervalMS < 50 || value.VirtualKeys.MinIntervalMS > 10_000 {
		return errors.New("os_actions.virtual_keys.min_interval_ms must be 50..10000")
	}
	if value.VirtualKeys.HoldMS < 10 || value.VirtualKeys.HoldMS > 1_000 {
		return errors.New("os_actions.virtual_keys.hold_ms must be 10..1000")
	}
	if len(value.VirtualKeys.Allowed) == 0 || len(value.VirtualKeys.Allowed) > 64 {
		return errors.New("os_actions.virtual_keys.allowed must contain 1..64 keys")
	}
	seenKeys := make(map[uint16]bool)
	for index, key := range value.VirtualKeys.Allowed {
		resolved, err := ResolveVirtualKey(key)
		if err != nil {
			return fmt.Errorf("os_actions.virtual_keys.allowed[%d]: %w", index, err)
		}
		if seenKeys[resolved.Code] {
			return fmt.Errorf("os_actions.virtual_keys.allowed[%d] duplicates VK 0x%02X", index, resolved.Code)
		}
		seenKeys[resolved.Code] = true
	}
	if len(value.Power.Allowed) == 0 || len(value.Power.Allowed) > 6 {
		return errors.New("os_actions.power.allowed must contain 1..6 actions")
	}
	seenPower := make(map[string]bool)
	for index, action := range value.Power.Allowed {
		normalized, err := NormalizePowerAction(action)
		if err != nil {
			return fmt.Errorf("os_actions.power.allowed[%d]: %w", index, err)
		}
		if seenPower[normalized] {
			return fmt.Errorf("os_actions.power.allowed[%d] duplicates %s", index, normalized)
		}
		seenPower[normalized] = true
	}
	if value.Power.RequireConfirmation && strings.TrimSpace(value.Power.ConfirmationToken) == "" {
		return errors.New("os_actions.power.confirmation_token is required")
	}
	if len(value.Power.ConfirmationToken) > 64 {
		return errors.New("os_actions.power.confirmation_token must be at most 64 characters")
	}
	if value.Brightness.MinPercent < 0 || value.Brightness.MinPercent > 100 ||
		value.Brightness.MaxPercent < 0 || value.Brightness.MaxPercent > 100 ||
		value.Brightness.MinPercent > value.Brightness.MaxPercent {
		return errors.New("os_actions.brightness range must satisfy 0 <= min_percent <= max_percent <= 100")
	}
	return nil
}

type ResolvedVirtualKey struct {
	Name string `json:"name"`
	Code uint16 `json:"code"`
}

var namedVirtualKeys = buildVirtualKeyMap()

func buildVirtualKeyMap() map[string]uint16 {
	values := map[string]uint16{
		"BACKSPACE": 0x08, "TAB": 0x09, "ENTER": 0x0D, "RETURN": 0x0D,
		"ESC": 0x1B, "ESCAPE": 0x1B, "SPACE": 0x20,
		"PAGEUP": 0x21, "PAGEDOWN": 0x22, "END": 0x23, "HOME": 0x24,
		"LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
		"INSERT": 0x2D, "DELETE": 0x2E,
		"VOLUMEMUTE": 0xAD, "VOLUMEDOWN": 0xAE, "VOLUMEUP": 0xAF,
		"NEXTTRACK": 0xB0, "PREVTRACK": 0xB1, "PLAYPAUSE": 0xB3,
	}
	for code := uint16('0'); code <= uint16('9'); code++ {
		values[string(rune(code))] = code
	}
	for code := uint16('A'); code <= uint16('Z'); code++ {
		values[string(rune(code))] = code
	}
	for index := uint16(1); index <= 24; index++ {
		values[fmt.Sprintf("F%d", index)] = 0x6F + index
	}
	return values
}

func normalizeKeyName(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "VK_")
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
}

func ResolveVirtualKey(value string) (ResolvedVirtualKey, error) {
	normalized := normalizeKeyName(value)
	if normalized == "" {
		return ResolvedVirtualKey{}, errors.New("virtual key is empty")
	}
	if code, ok := namedVirtualKeys[normalized]; ok {
		if virtualKeyDenied(code) {
			return ResolvedVirtualKey{}, fmt.Errorf("virtual key %s is reserved", normalized)
		}
		return ResolvedVirtualKey{Name: canonicalVirtualKeyName(code), Code: code}, nil
	}
	numeric := normalized
	base := 10
	if strings.HasPrefix(numeric, "0X") {
		numeric, base = numeric[2:], 16
	}
	parsed, err := strconv.ParseUint(numeric, base, 8)
	if err != nil || parsed == 0 || parsed == 0xFF {
		return ResolvedVirtualKey{}, fmt.Errorf("unknown virtual key %q", value)
	}
	code := uint16(parsed)
	if virtualKeyDenied(code) {
		return ResolvedVirtualKey{}, fmt.Errorf("virtual key 0x%02X is reserved", code)
	}
	return ResolvedVirtualKey{Name: canonicalVirtualKeyName(code), Code: code}, nil
}

func virtualKeyDenied(code uint16) bool {
	switch code {
	case 0x10, 0x11, 0x12, 0x5B, 0x5C, 0xE5, 0xE7:
		return true
	default:
		return false
	}
}

func canonicalVirtualKeyName(code uint16) string {
	preferred := []string{
		"UP", "DOWN", "LEFT", "RIGHT", "ENTER", "ESCAPE", "SPACE", "TAB",
		"BACKSPACE", "HOME", "END", "PAGEUP", "PAGEDOWN", "INSERT", "DELETE",
		"VOLUMEUP", "VOLUMEDOWN", "VOLUMEMUTE", "PLAYPAUSE", "NEXTTRACK", "PREVTRACK",
	}
	for _, name := range preferred {
		if namedVirtualKeys[name] == code {
			return name
		}
	}
	if code >= 0x70 && code <= 0x87 {
		return fmt.Sprintf("F%d", code-0x6F)
	}
	if (code >= '0' && code <= '9') || (code >= 'A' && code <= 'Z') {
		return string(rune(code))
	}
	return fmt.Sprintf("VK_0x%02X", code)
}

func virtualKeyAllowed(policy VirtualKeyPolicy, code uint16) bool {
	for _, value := range policy.Allowed {
		resolved, err := ResolveVirtualKey(value)
		if err == nil && resolved.Code == code {
			return true
		}
	}
	return false
}

func NormalizePowerAction(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sleep", "suspend":
		return "sleep", nil
	case "hibernate":
		return "hibernate", nil
	case "shutdown", "poweroff", "power-off":
		return "shutdown", nil
	case "restart", "reboot":
		return "restart", nil
	case "logoff", "log-out", "logout":
		return "logoff", nil
	case "lock", "lock-workstation", "lockworkstation":
		return "lock", nil
	default:
		return "", fmt.Errorf("unknown power action %q", value)
	}
}

type Executor struct {
	mu                sync.Mutex
	lastDown          time.Time
	pressed           map[uint16]bool
	brightnessBackend BrightnessBackend
}

type nativeBrightnessBackend struct{}

func (nativeBrightnessBackend) Brightness(ctx context.Context) (BrightnessStatus, error) {
	return platformMonitorBrightness(ctx)
}

func (nativeBrightnessBackend) SetBrightness(ctx context.Context, percent int) (BrightnessStatus, error) {
	return platformSetMonitorBrightness(ctx, percent)
}

// NewExecutor supplies an injectable brightness backend. A nil backend keeps
// the native platform implementation, as does the backward-compatible zero
// value Executor.
func NewExecutor(brightness BrightnessBackend) *Executor {
	return &Executor{brightnessBackend: brightness}
}

var DefaultExecutor = NewExecutor(nil)

func (executor *Executor) brightness() BrightnessBackend {
	if executor.brightnessBackend != nil {
		return executor.brightnessBackend
	}
	return nativeBrightnessBackend{}
}

func (executor *Executor) PressVirtualKey(
	ctx context.Context,
	policy VirtualKeyPolicy,
	request VirtualKeyRequest,
) (ActionResult, error) {
	if !policy.Enabled {
		return ActionResult{}, errors.New("virtual-key emission is disabled by host policy")
	}
	resolved, err := ResolveVirtualKey(request.Key)
	if err != nil {
		return ActionResult{}, err
	}
	if !virtualKeyAllowed(policy, resolved.Code) {
		return ActionResult{}, fmt.Errorf("virtual key %s is not in the configured allowlist", resolved.Name)
	}
	hold := request.HoldMS
	if hold == 0 {
		hold = policy.HoldMS
	}
	if hold < 10 || hold > 1_000 {
		return ActionResult{}, errors.New("virtual-key hold_ms must be 10..1000")
	}
	minimum := time.Duration(policy.MinIntervalMS) * time.Millisecond
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if !executor.lastDown.IsZero() && time.Since(executor.lastDown) < minimum {
		return ActionResult{}, fmt.Errorf("virtual-key rate limit is %dms", policy.MinIntervalMS)
	}
	select {
	case <-ctx.Done():
		return ActionResult{}, ctx.Err()
	default:
	}
	if err := platformKeyDown(resolved.Code); err != nil {
		return ActionResult{}, err
	}
	if executor.pressed == nil {
		executor.pressed = make(map[uint16]bool)
	}
	executor.pressed[resolved.Code] = true
	executor.lastDown = time.Now()
	timer := time.NewTimer(time.Duration(hold) * time.Millisecond)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}
	releaseErr := platformKeyUp(resolved.Code)
	if releaseErr != nil {
		return ActionResult{}, fmt.Errorf("release virtual key %s: %w", resolved.Name, releaseErr)
	}
	delete(executor.pressed, resolved.Code)
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{
		Action: "virtual-key", At: time.Now().UTC(),
		Detail: fmt.Sprintf("%s (0x%02X) pressed for %dms", resolved.Name, resolved.Code, hold),
	}, nil
}

// ReleaseAll defensively releases any key retained after a platform failure.
func (executor *Executor) ReleaseAll() error {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	var failures []error
	for code := range executor.pressed {
		if err := platformKeyUp(code); err != nil {
			failures = append(failures, fmt.Errorf("release VK 0x%02X: %w", code, err))
			continue
		}
		delete(executor.pressed, code)
	}
	return errors.Join(failures...)
}

func (executor *Executor) Power(
	ctx context.Context,
	policy PowerPolicy,
	request PowerRequest,
) (ActionResult, error) {
	if !policy.Enabled {
		return ActionResult{}, errors.New("power actions are disabled by host policy")
	}
	action, err := NormalizePowerAction(request.Action)
	if err != nil {
		return ActionResult{}, err
	}
	allowed := false
	for _, candidate := range policy.Allowed {
		normalized, normalizeErr := NormalizePowerAction(candidate)
		if normalizeErr == nil && normalized == action {
			allowed = true
			break
		}
	}
	if !allowed {
		return ActionResult{}, fmt.Errorf("power action %s is not in the configured allowlist", action)
	}
	if request.Automation && !policy.AllowAutomation {
		return ActionResult{}, errors.New("automated power actions are disabled by host policy")
	}
	if policy.RequireConfirmation && subtle.ConstantTimeCompare(
		[]byte(request.Confirmation), []byte(policy.ConfirmationToken),
	) != 1 {
		return ActionResult{}, errors.New("power action confirmation did not match policy")
	}
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	if err := platformPowerAction(ctx, action); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{
		Action: "power." + action, At: time.Now().UTC(),
		Detail: action + " request accepted by the operating system",
	}, nil
}

// MonitorBrightness reads the primary physical monitor without requiring the
// write policy to be enabled. Unsupported internal panels or display drivers
// return a descriptive platform error.
func (executor *Executor) MonitorBrightness(ctx context.Context) (BrightnessResult, error) {
	if err := ctx.Err(); err != nil {
		return BrightnessResult{}, err
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	status, err := executor.brightness().Brightness(ctx)
	if err != nil {
		return BrightnessResult{}, err
	}
	if status.Percent < 0 || status.Percent > 100 {
		return BrightnessResult{}, fmt.Errorf("platform returned invalid monitor brightness %d", status.Percent)
	}
	return BrightnessResult{Status: status, At: time.Now().UTC()}, nil
}

// SetMonitorBrightness applies a normalized value only when the watched host
// policy explicitly enables it and the requested value is inside its range.
func (executor *Executor) SetMonitorBrightness(
	ctx context.Context,
	policy BrightnessPolicy,
	percent int,
) (BrightnessResult, error) {
	if !policy.Enabled {
		return BrightnessResult{}, errors.New("monitor-brightness writes are disabled by host policy")
	}
	if percent < policy.MinPercent || percent > policy.MaxPercent {
		return BrightnessResult{}, fmt.Errorf(
			"monitor brightness %d is outside the configured %d..%d range",
			percent, policy.MinPercent, policy.MaxPercent,
		)
	}
	if err := ctx.Err(); err != nil {
		return BrightnessResult{}, err
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	status, err := executor.brightness().SetBrightness(ctx, percent)
	if err != nil {
		return BrightnessResult{}, err
	}
	if status.Percent < 0 || status.Percent > 100 {
		return BrightnessResult{}, fmt.Errorf("platform returned invalid monitor brightness %d", status.Percent)
	}
	return BrightnessResult{Status: status, Changed: true, At: time.Now().UTC()}, nil
}

func Status(discoverySource string) (SystemStatus, error) {
	hostname, _ := os.Hostname()
	interfaces, err := net.Interfaces()
	if err != nil {
		return SystemStatus{}, fmt.Errorf("enumerate network interfaces: %w", err)
	}
	addresses := make([]NetworkAddress, 0)
	for _, networkInterface := range interfaces {
		values, addressErr := networkInterface.Addrs()
		if addressErr != nil {
			continue
		}
		for _, value := range values {
			address := value.String()
			ipText := strings.SplitN(address, "/", 2)[0]
			ip := net.ParseIP(strings.TrimSuffix(ipText, "%"+networkInterface.Name))
			addresses = append(addresses, NetworkAddress{
				Interface: networkInterface.Name, Address: address,
				Loopback: ip != nil && ip.IsLoopback(),
			})
		}
	}
	sort.Slice(addresses, func(left, right int) bool {
		if addresses[left].Loopback != addresses[right].Loopback {
			return !addresses[left].Loopback
		}
		if addresses[left].Interface != addresses[right].Interface {
			return addresses[left].Interface < addresses[right].Interface
		}
		return addresses[left].Address < addresses[right].Address
	})
	return SystemStatus{
		Hostname: hostname, OperatingSystem: runtime.GOOS, Architecture: runtime.GOARCH,
		LogicalCPUs: runtime.NumCPU(), ProcessID: os.Getpid(), HostUptimeMS: platformUptimeMS(),
		NetworkAddresses: addresses, DiscoverySource: discoverySource, CollectedAt: time.Now().UTC(),
	}, nil
}
