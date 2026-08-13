package hostui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultInstanceLeaseSeconds = 45
	MaximumInstanceLeaseSeconds = 300
)

var (
	instanceIDPattern     = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,180}$`)
	instanceValuePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	instancePagePattern   = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,96}$`)
	instanceSecretPattern = regexp.MustCompile(
		`(?i)(authorization|cookie|password|passwd|secret|session|token|api.?key)`,
	)
)

// AppInstance is one live UI or automation surface attached to the primary
// host. Values are deliberately small, presentation-only facts; credentials
// and arbitrary configuration blobs do not belong in presence state.
type AppInstance struct {
	ID           string            `json:"id"`
	Surface      string            `json:"surface"`
	Page         string            `json:"page,omitempty"`
	State        string            `json:"state,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
	Self         *InstanceSelf     `json:"self,omitempty"`
	RegisteredAt time.Time         `json:"registered_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	ExpiresAt    time.Time         `json:"expires_at,omitempty"`
	LeaseSeconds int               `json:"lease_seconds,omitempty"`
}

// InstanceSelf is the bounded, non-secret identity an instance publishes
// about its own process or browser runtime. It deliberately excludes the raw
// process environment because API keys and credentials frequently live there.
type InstanceSelf struct {
	Kind             string            `json:"kind"`
	ProcessID        int               `json:"pid,omitempty"`
	ParentProcessID  int               `json:"parent_pid,omitempty"`
	ImagePath        string            `json:"image_path,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	StartedAt        time.Time         `json:"started_at,omitempty"`
	Vars             map[string]string `json:"vars,omitempty"`
}

var safeProcessVariableNames = [...]string{
	"COMPUTERNAME", "USERNAME", "USERDOMAIN", "OS", "COMSPEC",
	"PROCESSOR_ARCHITECTURE", "PROCESSOR_IDENTIFIER", "NUMBER_OF_PROCESSORS",
	"TERM", "TERM_PROGRAM", "COLORTERM", "WT_PROFILE_ID", "LANG", "LC_ALL",
}

// CurrentProcessSelf returns authoritative identity for the process hosting
// the coordinator. Only an explicit, non-secret environment allowlist is
// exposed; PATH, arguments, tokens, and arbitrary environment values are not.
func CurrentProcessSelf(startedAt time.Time) InstanceSelf {
	imagePath, _ := os.Executable()
	if absolute, err := filepath.Abs(imagePath); err == nil {
		imagePath = absolute
	}
	workingDirectory, _ := os.Getwd()
	variables := make(map[string]string)
	for _, name := range safeProcessVariableNames {
		if value, ok := os.LookupEnv(name); ok && value != "" && len(value) <= 1024 &&
			!strings.ContainsAny(value, "\x00\r\n") {
			variables[strings.ToLower(name)] = value
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return InstanceSelf{
		Kind: "process", ProcessID: os.Getpid(), ParentProcessID: os.Getppid(),
		ImagePath: imagePath, WorkingDirectory: workingDirectory,
		StartedAt: startedAt.UTC(), Vars: variables,
	}
}

type InstanceChange struct {
	Kind     string      `json:"kind"`
	Instance AppInstance `json:"instance"`
}

type InstanceRegistry struct {
	mu        sync.Mutex
	instances map[string]AppInstance
	now       func() time.Time
	observer  func(InstanceChange)
}

func NewInstanceRegistry() *InstanceRegistry {
	return &InstanceRegistry{
		instances: make(map[string]AppInstance),
		now:       time.Now,
	}
}

// TargetsInstance applies the shared target convention used by app actions
// and board-originated navigation events. Empty and * are broadcasts; a
// surface name targets every instance of that UI type; an ID targets one.
func TargetsInstance(target, id, surface string) bool {
	target = strings.TrimSpace(target)
	return target == "" || target == "*" ||
		strings.EqualFold(target, strings.TrimSpace(id)) ||
		strings.EqualFold(target, strings.TrimSpace(surface))
}

func (registry *InstanceRegistry) SetObserver(observer func(InstanceChange)) {
	registry.mu.Lock()
	registry.observer = observer
	registry.mu.Unlock()
}

func (registry *InstanceRegistry) Upsert(value AppInstance) (AppInstance, error) {
	value.ID = strings.TrimSpace(value.ID)
	value.Surface = strings.ToLower(strings.TrimSpace(value.Surface))
	value.Page = strings.ToLower(strings.TrimSpace(value.Page))
	value.State = strings.ToLower(strings.TrimSpace(value.State))
	if !instanceIDPattern.MatchString(value.ID) {
		return AppInstance{}, errors.New("instance id must be 1..180 letters, digits, dot, underscore, colon, or dash")
	}
	if !instanceValuePattern.MatchString(value.Surface) {
		return AppInstance{}, errors.New("instance surface is invalid")
	}
	if !instancePagePattern.MatchString(value.Page) {
		return AppInstance{}, errors.New("instance page is invalid")
	}
	if value.State != "" && value.State != "active" && value.State != "hidden" &&
		value.State != "background" && value.State != "leaving" {
		return AppInstance{}, errors.New("instance state must be active, hidden, background, or leaving")
	}
	if value.LeaseSeconds < 0 || value.LeaseSeconds > MaximumInstanceLeaseSeconds {
		return AppInstance{}, errors.New("instance lease_seconds must be 0..300")
	}
	if len(value.Values) > 32 {
		return AppInstance{}, errors.New("instance values may contain at most 32 entries")
	}
	cleanValues := make(map[string]string, len(value.Values))
	for key, raw := range value.Values {
		key = strings.ToLower(strings.TrimSpace(key))
		if !instanceValuePattern.MatchString(key) || instanceSecretPattern.MatchString(key) {
			return AppInstance{}, errors.New("instance value key is invalid or credential-like")
		}
		if len(raw) > 1024 || strings.ContainsAny(raw, "\x00\r\n") {
			return AppInstance{}, errors.New("instance value is too long or contains controls")
		}
		if key == ActionCapabilitiesKey {
			capabilities, capabilityErr := ActionCapabilities(strings.Split(raw, ",")...)
			if capabilityErr != nil {
				return AppInstance{}, capabilityErr
			}
			raw = capabilities
		}
		cleanValues[key] = raw
	}
	value.Values = cleanValues
	if value.Self != nil {
		cleanSelf, cleanErr := normalizeInstanceSelf(*value.Self)
		if cleanErr != nil {
			return AppInstance{}, cleanErr
		}
		value.Self = &cleanSelf
	}

	registry.mu.Lock()
	now := registry.now().UTC()
	previous, exists := registry.instances[value.ID]
	if exists {
		value.RegisteredAt = previous.RegisteredAt
	} else {
		value.RegisteredAt = now
	}
	value.UpdatedAt = now
	if value.LeaseSeconds > 0 {
		value.ExpiresAt = now.Add(time.Duration(value.LeaseSeconds) * time.Second)
	} else {
		value.ExpiresAt = time.Time{}
	}
	if value.State == "leaving" {
		delete(registry.instances, value.ID)
	} else {
		registry.instances[value.ID] = cloneAppInstance(value)
	}
	observer := registry.observer
	registry.mu.Unlock()
	if observer != nil {
		kind := "updated"
		if !exists {
			kind = "joined"
		}
		if value.State == "leaving" {
			kind = "left"
		}
		observer(InstanceChange{Kind: kind, Instance: cloneAppInstance(value)})
	}
	return cloneAppInstance(value), nil
}

func (registry *InstanceRegistry) Get(id string) (AppInstance, bool) {
	registry.mu.Lock()
	registry.pruneLocked()
	value, ok := registry.instances[strings.TrimSpace(id)]
	registry.mu.Unlock()
	return cloneAppInstance(value), ok
}

func (registry *InstanceRegistry) List() []AppInstance {
	registry.mu.Lock()
	registry.pruneLocked()
	result := make([]AppInstance, 0, len(registry.instances))
	for _, value := range registry.instances {
		result = append(result, cloneAppInstance(value))
	}
	registry.mu.Unlock()
	sort.Slice(result, func(left, right int) bool {
		if result[left].Surface == result[right].Surface {
			return result[left].ID < result[right].ID
		}
		return result[left].Surface < result[right].Surface
	})
	return result
}

func (registry *InstanceRegistry) Remove(id string) bool {
	id = strings.TrimSpace(id)
	registry.mu.Lock()
	value, ok := registry.instances[id]
	if ok {
		delete(registry.instances, id)
	}
	observer := registry.observer
	registry.mu.Unlock()
	if ok && observer != nil {
		value.State = "leaving"
		value.UpdatedAt = registry.now().UTC()
		observer(InstanceChange{Kind: "left", Instance: cloneAppInstance(value)})
	}
	return ok
}

func (registry *InstanceRegistry) pruneLocked() {
	now := registry.now()
	for id, value := range registry.instances {
		if !value.ExpiresAt.IsZero() && !now.Before(value.ExpiresAt) {
			delete(registry.instances, id)
		}
	}
}

func cloneAppInstance(value AppInstance) AppInstance {
	if value.Values != nil {
		values := make(map[string]string, len(value.Values))
		for key, item := range value.Values {
			values[key] = item
		}
		value.Values = values
	}
	if value.Self != nil {
		self := *value.Self
		if self.Vars != nil {
			variables := make(map[string]string, len(self.Vars))
			for key, item := range self.Vars {
				variables[key] = item
			}
			self.Vars = variables
		}
		value.Self = &self
	}
	return value
}

func normalizeInstanceSelf(value InstanceSelf) (InstanceSelf, error) {
	value.Kind = strings.ToLower(strings.TrimSpace(value.Kind))
	if !instanceValuePattern.MatchString(value.Kind) {
		return InstanceSelf{}, errors.New("instance self kind is invalid")
	}
	if value.ProcessID < 0 || value.ParentProcessID < 0 {
		return InstanceSelf{}, errors.New("instance self process IDs must not be negative")
	}
	for _, item := range []string{value.ImagePath, value.WorkingDirectory} {
		if len(item) > 4096 || strings.ContainsAny(item, "\x00\r\n") {
			return InstanceSelf{}, errors.New("instance self path is too long or contains controls")
		}
	}
	if len(value.Vars) > 32 {
		return InstanceSelf{}, errors.New("instance self vars may contain at most 32 entries")
	}
	cleanVariables := make(map[string]string, len(value.Vars))
	for key, raw := range value.Vars {
		key = strings.ToLower(strings.TrimSpace(key))
		if !instanceValuePattern.MatchString(key) || instanceSecretPattern.MatchString(key) {
			return InstanceSelf{}, errors.New("instance self var key is invalid or credential-like")
		}
		if len(raw) > 1024 || strings.ContainsAny(raw, "\x00\r\n") {
			return InstanceSelf{}, errors.New("instance self var is too long or contains controls")
		}
		cleanVariables[key] = raw
	}
	value.Vars = cleanVariables
	return value, nil
}
