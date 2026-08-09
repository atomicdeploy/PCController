package aptmirror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ConfigFormat = "pccontroller-ubuntu-apt-mirrors/v1"
	StateFormat  = "pccontroller-ubuntu-apt-mirror-state/v1"
)

type CandidateRole string

const (
	RoleDomestic         CandidateRole = "domestic"
	RoleOfficialMain     CandidateRole = "official-main"
	RoleOfficialSecurity CandidateRole = "official-security"
	RoleOfficialBoth     CandidateRole = "official-both"
)

type Candidate struct {
	ID          string        `json:"id"`
	Role        CandidateRole `json:"role"`
	Priority    int           `json:"priority,omitempty"`
	URI         string        `json:"uri"`
	BypassProxy bool          `json:"bypass_proxy,omitempty"`
}

type Paths struct {
	APTRoot          string `json:"apt_root"`
	MirrorList       string `json:"mirror_list"`
	State            string `json:"state"`
	Lock             string `json:"lock"`
	Keyring          string `json:"keyring"`
	CanonicalSource  string `json:"canonical_source"`
	APTResilience    string `json:"apt_resilience"`
	ProxyEnvironment string `json:"proxy_environment"`
	InstalledConfig  string `json:"installed_config"`
	StableExecutable string `json:"stable_executable"`
	Service          string `json:"service"`
	Timer            string `json:"timer"`
	BackupRoot       string `json:"backup_root"`
}

type Config struct {
	Format                   string      `json:"format"`
	Codename                 string      `json:"codename"`
	Architecture             string      `json:"architecture"`
	Components               []string    `json:"components"`
	MaxLagSeconds            int64       `json:"max_lag_seconds"`
	LastGoodTTLSeconds       int64       `json:"last_good_ttl_seconds"`
	FirstRunMovingAgeSeconds int64       `json:"first_run_moving_age_seconds"`
	FirstRunBaseAgeSeconds   int64       `json:"first_run_base_age_seconds"`
	ConnectTimeoutSeconds    int         `json:"connect_timeout_seconds"`
	TransferTimeoutSeconds   int         `json:"transfer_timeout_seconds"`
	RefreshTimeoutSeconds    int         `json:"refresh_timeout_seconds"`
	FutureToleranceSeconds   int64       `json:"future_tolerance_seconds"`
	Candidates               []Candidate `json:"candidates"`
	Paths                    Paths       `json:"paths"`
}

func DomesticFirstConfig(codename, architecture string) Config {
	return Config{
		Format: ConfigFormat, Codename: codename, Architecture: architecture,
		Components:    []string{"main", "restricted", "universe", "multiverse"},
		MaxLagSeconds: 8 * 60 * 60, LastGoodTTLSeconds: 24 * 60 * 60,
		FirstRunMovingAgeSeconds: 48 * 60 * 60,
		// The immutable release pocket is intentionally older than updates and
		// security. Six months remains bounded while allowing a supported Ubuntu
		// release to bootstrap during official-source censorship.
		FirstRunBaseAgeSeconds: 180 * 24 * 60 * 60,
		ConnectTimeoutSeconds:  5, TransferTimeoutSeconds: 30, RefreshTimeoutSeconds: 4 * 60,
		FutureToleranceSeconds: 10 * 60,
		Candidates: []Candidate{
			{ID: "ir-archive", Role: RoleDomestic, URI: "http://ir.archive.ubuntu.com/ubuntu/", BypassProxy: true},
			{ID: "liara", Role: RoleDomestic, URI: "http://linux-mirror.liara.ir/repository/ubuntu/", BypassProxy: true},
			{ID: "sindad", Role: RoleDomestic, URI: "https://ir.ubuntu.sindad.cloud/ubuntu/", BypassProxy: true},
			{ID: "abrha", Role: RoleDomestic, URI: "https://repo.abrha.net/ubuntu/", BypassProxy: true},
			{ID: "arvan", Role: RoleDomestic, URI: "https://mirror.arvancloud.ir/ubuntu/", BypassProxy: true},
			{ID: "ubuntu-cloud", Role: RoleOfficialMain, Priority: 850, URI: "http://nova.clouds.archive.ubuntu.com/ubuntu/"},
			{ID: "ubuntu-archive", Role: RoleOfficialBoth, Priority: 900, URI: "http://archive.ubuntu.com/ubuntu/"},
			{ID: "ubuntu-security", Role: RoleOfficialSecurity, Priority: 900, URI: "http://security.ubuntu.com/ubuntu/"},
		},
		Paths: DefaultPaths(),
	}
}

func DefaultPaths() Paths {
	return Paths{
		APTRoot:          "/etc/apt",
		MirrorList:       "/etc/apt/mirrors/ubuntu-dynamic.list",
		State:            "/var/lib/pccontroller-apt-mirrors/state.json",
		Lock:             "/run/lock/pccontroller-apt-mirrors.lock",
		Keyring:          "/usr/share/keyrings/ubuntu-archive-keyring.gpg",
		CanonicalSource:  "/etc/apt/sources.list.d/ubuntu.sources",
		APTResilience:    "/etc/apt/apt.conf.d/80-pccontroller-mirror-resilience",
		ProxyEnvironment: "/etc/pccontroller/apt-mirror-proxy.env",
		InstalledConfig:  "/etc/pccontroller/apt-mirrors.json",
		StableExecutable: "/opt/pccontroller/bin/controller",
		Service:          "/etc/systemd/system/pccontroller-apt-mirror-health.service",
		Timer:            "/etc/systemd/system/pccontroller-apt-mirror-health.timer",
		BackupRoot:       "/var/backups",
	}
}

func (config Config) Suites() []string {
	return []string{
		config.Codename,
		config.Codename + "-updates",
		config.Codename + "-backports",
		config.Codename + "-security",
	}
}

func (config Config) Validate() error {
	if config.Format != ConfigFormat {
		return fmt.Errorf("unsupported APT mirror config format %q", config.Format)
	}
	identifier := regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	architecture := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	if !identifier.MatchString(config.Codename) || !architecture.MatchString(config.Architecture) {
		return errors.New("APT mirror config requires a safe Ubuntu codename and Debian architecture")
	}
	if len(config.Components) == 0 || len(config.Candidates) == 0 {
		return errors.New("APT mirror config requires components and candidates")
	}
	seenComponent := make(map[string]bool)
	for _, component := range config.Components {
		if !identifier.MatchString(component) || seenComponent[component] {
			return fmt.Errorf("invalid or duplicate Ubuntu component %q", component)
		}
		seenComponent[component] = true
	}
	if config.MaxLagSeconds <= 0 || config.MaxLagSeconds > int64((7*24*time.Hour)/time.Second) ||
		config.LastGoodTTLSeconds <= 0 || config.LastGoodTTLSeconds > int64((7*24*time.Hour)/time.Second) ||
		config.FirstRunMovingAgeSeconds <= 0 || config.FirstRunBaseAgeSeconds <= 0 ||
		config.FirstRunMovingAgeSeconds > int64((7*24*time.Hour)/time.Second) ||
		config.FirstRunBaseAgeSeconds > int64((365*24*time.Hour)/time.Second) ||
		config.ConnectTimeoutSeconds <= 0 || config.ConnectTimeoutSeconds > 120 ||
		config.TransferTimeoutSeconds <= 0 || config.TransferTimeoutSeconds > 300 ||
		config.RefreshTimeoutSeconds <= 0 || config.RefreshTimeoutSeconds > 15*60 ||
		config.FutureToleranceSeconds < 0 || config.FutureToleranceSeconds > int64((24*time.Hour)/time.Second) {
		return errors.New("APT mirror timing policy must use positive bounded durations")
	}
	seenID := make(map[string]bool)
	seenURI := make(map[string]bool)
	for _, candidate := range config.Candidates {
		if candidate.ID != strings.ToLower(candidate.ID) || !identifier.MatchString(candidate.ID) || seenID[candidate.ID] {
			return fmt.Errorf("invalid or duplicate APT mirror candidate ID %q", candidate.ID)
		}
		seenID[candidate.ID] = true
		parsed, err := url.Parse(candidate.URI)
		if err != nil || strings.ContainsAny(candidate.URI, " \t\r\n") ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil ||
			parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
			!strings.HasSuffix(parsed.Path, "/") || seenURI[candidate.URI] {
			return fmt.Errorf("candidate %s requires a unique HTTP(S) archive-root URI ending in slash", candidate.ID)
		}
		seenURI[candidate.URI] = true
		switch candidate.Role {
		case RoleDomestic:
			if candidate.Priority != 0 {
				return fmt.Errorf("domestic candidate %s priority is health-derived", candidate.ID)
			}
		case RoleOfficialMain, RoleOfficialSecurity, RoleOfficialBoth:
			if candidate.Priority != 850 && candidate.Priority != 900 {
				return fmt.Errorf("official candidate %s must use fallback priority 850 or 900", candidate.ID)
			}
		default:
			return fmt.Errorf("candidate %s has unsupported role %q", candidate.ID, candidate.Role)
		}
	}
	for _, suite := range config.Suites() {
		covered := false
		for _, candidate := range config.Candidates {
			if candidate.Role != RoleDomestic && candidateHasSuite(candidate, suite, config) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("no unconditional official fallback covers %s", suite)
		}
	}
	pathValues := map[string]string{
		"APT root":    config.Paths.APTRoot,
		"mirror list": config.Paths.MirrorList, "state": config.Paths.State,
		"lock": config.Paths.Lock, "Ubuntu keyring": config.Paths.Keyring,
		"canonical source":      config.Paths.CanonicalSource,
		"APT resilience config": config.Paths.APTResilience,
		"proxy environment":     config.Paths.ProxyEnvironment,
		"installed config":      config.Paths.InstalledConfig,
		"stable executable":     config.Paths.StableExecutable,
		"service":               config.Paths.Service, "timer": config.Paths.Timer,
		"backup root": config.Paths.BackupRoot,
	}
	seenPath := make(map[string]string)
	for name, path := range pathValues {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return fmt.Errorf("%s path must be absolute", name)
		}
		clean := filepath.Clean(path)
		if prior := seenPath[clean]; prior != "" {
			return fmt.Errorf("%s path collides with %s", name, prior)
		}
		seenPath[clean] = name
	}
	return nil
}

func candidateHasSuite(candidate Candidate, suite string, config Config) bool {
	security := suite == config.Codename+"-security"
	switch candidate.Role {
	case RoleDomestic, RoleOfficialBoth:
		return true
	case RoleOfficialMain:
		return !security
	case RoleOfficialSecurity:
		return security
	default:
		return false
	}
}

func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read APT mirror config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("decode APT mirror config: %w", err)
	}
	return config, config.Validate()
}

func EncodeConfig(config Config) ([]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// LoadCandidateOverrides reads only the candidate list. Host-controlled paths
// and health policy cannot be replaced through the provision-host CLI input.
func LoadCandidateOverrides(path string) ([]Candidate, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read APT mirror candidates: %w", err)
	}
	var candidates []Candidate
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidates); err != nil {
		return nil, fmt.Errorf("decode APT mirror candidates JSON array: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("APT mirror candidates JSON has trailing content")
	}
	if len(candidates) == 0 {
		return nil, errors.New("APT mirror candidates JSON array is empty")
	}
	return candidates, nil
}

func (config Config) connectTimeout() time.Duration {
	return time.Duration(config.ConnectTimeoutSeconds) * time.Second
}

func (config Config) transferTimeout() time.Duration {
	return time.Duration(config.TransferTimeoutSeconds) * time.Second
}

func (config Config) refreshTimeout() time.Duration {
	return time.Duration(config.RefreshTimeoutSeconds) * time.Second
}
