package aptmirror

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type tableProber struct {
	mu      sync.Mutex
	results map[string]ProbeResult
}

func (prober *tableProber) Probe(_ context.Context, _ Config, candidate Candidate, suite string) ProbeResult {
	prober.mu.Lock()
	defer prober.mu.Unlock()
	if result, ok := prober.results[stateKey(candidate.ID, suite)]; ok {
		return result
	}
	return ProbeResult{Status: ProbeTransient, Detail: "transport"}
}

func mirrorTestConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	aptRoot := filepath.Join(root, "etc", "apt")
	config := DomesticFirstConfig("resolute", "amd64")
	config.Components = []string{"main"}
	config.Candidates = []Candidate{
		{ID: "domestic", Role: RoleDomestic, URI: "https://mirror.example/ubuntu/", BypassProxy: true},
		{ID: "official", Role: RoleOfficialBoth, Priority: 900, URI: "https://archive.ubuntu.com/ubuntu/"},
	}
	config.Paths = Paths{
		APTRoot:          aptRoot,
		MirrorList:       filepath.Join(aptRoot, "mirrors", "ubuntu-dynamic.list"),
		State:            filepath.Join(root, "var", "lib", "pccontroller", "state.json"),
		Lock:             filepath.Join(root, "run", "mirror.lock"),
		Keyring:          filepath.Join(root, "keyring.gpg"),
		CanonicalSource:  filepath.Join(aptRoot, "sources.list.d", "ubuntu.sources"),
		APTResilience:    filepath.Join(aptRoot, "apt.conf.d", "80-pccontroller"),
		ProxyEnvironment: filepath.Join(root, "etc", "pccontroller", "proxy.env"),
		InstalledConfig:  filepath.Join(root, "etc", "pccontroller", "mirrors.json"),
		StableExecutable: filepath.Join(root, "opt", "pccontroller", "bin", "controller"),
		Service:          filepath.Join(root, "etc", "systemd", "mirror.service"),
		Timer:            filepath.Join(root, "etc", "systemd", "mirror.timer"),
		BackupRoot:       filepath.Join(root, "backups"),
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	return config
}

func probeResults(config Config, domestic, official ProbeResult) map[string]ProbeResult {
	results := make(map[string]ProbeResult)
	for _, suite := range config.Suites() {
		results[stateKey("domestic", suite)] = domestic
		results[stateKey("official", suite)] = official
	}
	return results
}

func routePriority(report RefreshReport, candidate, suite string) int {
	for _, route := range report.Routes {
		if route.CandidateID == candidate && route.Suite == suite {
			return route.Priority
		}
	}
	return 0
}

func TestRefreshRoutingPriorities(t *testing.T) {
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		domestic     ProbeResult
		official     ProbeResult
		wantedBase   int
		wantedMoving int
	}{
		{
			name:       "official reference makes exact domestic preferred",
			domestic:   ProbeResult{Status: ProbeVerified, Publication: now.Add(-2 * time.Hour), ValidUntil: now.Add(24 * time.Hour)},
			official:   ProbeResult{Status: ProbeVerified, Publication: now.Add(-time.Hour), ValidUntil: now.Add(24 * time.Hour)},
			wantedBase: 10, wantedMoving: 10,
		},
		{
			name:       "official cutoff permits strict-age first run",
			domestic:   ProbeResult{Status: ProbeVerified, Publication: now.Add(-time.Hour), ValidUntil: now.Add(24 * time.Hour)},
			official:   ProbeResult{Status: ProbeTransient, Detail: "transport"},
			wantedBase: 20, wantedMoving: 20,
		},
		{
			name:       "old immutable base stays domestic first during official cutoff",
			domestic:   ProbeResult{Status: ProbeVerified, Publication: now.Add(-200 * 24 * time.Hour)},
			official:   ProbeResult{Status: ProbeTransient, Detail: "transport"},
			wantedBase: 20, wantedMoving: 950,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := mirrorTestConfig(t)
			report, err := Refresh(context.Background(), RefreshOptions{
				Config: config, Now: now,
				Prober: &tableProber{results: probeResults(config, test.domestic, test.official)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := routePriority(report, "domestic", config.Codename); got != test.wantedBase {
				t.Fatalf("base priority=%d want %d", got, test.wantedBase)
			}
			if got := routePriority(report, "domestic", config.Codename+"-updates"); got != test.wantedMoving {
				t.Fatalf("moving priority=%d want %d", got, test.wantedMoving)
			}
			if got := routePriority(report, "official", config.Codename); got != 900 {
				t.Fatalf("official fallback priority=%d", got)
			}
		})
	}
}

func TestRefreshTransientLastGoodDependsOnOfficialAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		official   ProbeResult
		last       int64
		validUntil int64
		wanted     int
	}{
		{"official cut retains formerly exact", ProbeResult{Status: ProbeTransient}, now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix(), 10},
		{"official recovery demotes transient", ProbeResult{Status: ProbeVerified, Publication: now.Add(-time.Hour)}, now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix(), 950},
		{"expired last-good is removed", ProbeResult{Status: ProbeTransient}, now.Add(-time.Hour).Unix(), now.Add(-time.Minute).Unix(), 0},
		{"future success cannot resurrect state", ProbeResult{Status: ProbeTransient}, now.Add(time.Hour).Unix(), now.Add(2 * time.Hour).Unix(), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := mirrorTestConfig(t)
			state := State{Format: StateFormat, References: map[string]int64{}, Good: map[string]GoodState{
				stateKey("domestic", config.Codename): {
					Publication: now.Add(-2 * time.Hour).Unix(), LastSuccess: test.last,
					ValidUntil: test.validUntil, Exact: true,
				},
			}}
			content, _ := json.Marshal(state)
			if err := os.MkdirAll(filepath.Dir(config.Paths.State), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config.Paths.State, content, 0o600); err != nil {
				t.Fatal(err)
			}
			results := probeResults(config, ProbeResult{Status: ProbeTransient}, test.official)
			report, err := Refresh(context.Background(), RefreshOptions{
				Config: config, Now: now, Prober: &tableProber{results: results},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := routePriority(report, "domestic", config.Codename); got != test.wanted {
				t.Fatalf("priority=%d want %d", got, test.wanted)
			}
		})
	}
}

func TestRefreshDryRunAndCorruptStatePreserveHost(t *testing.T) {
	config := mirrorTestConfig(t)
	if err := os.MkdirAll(filepath.Dir(config.Paths.State), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.Paths.MirrorList), 0o755); err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"format":"pccontroller-ubuntu-apt-mirror-state/v1","official_references":{},"domestic_last_good":{}}`)
	mirror := []byte("last-known-good\n")
	_ = os.WriteFile(config.Paths.State, state, 0o600)
	_ = os.WriteFile(config.Paths.MirrorList, mirror, 0o644)
	now := time.Now().UTC()
	results := probeResults(config,
		ProbeResult{Status: ProbeVerified, Publication: now},
		ProbeResult{Status: ProbeVerified, Publication: now},
	)
	if _, err := Refresh(context.Background(), RefreshOptions{Config: config, Now: now, Prober: &tableProber{results: results}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(config.Paths.MirrorList); string(got) != string(mirror) {
		t.Fatal("dry-run changed mirror list")
	}
	if _, err := os.Stat(config.Paths.Lock); !os.IsNotExist(err) {
		t.Fatal("dry-run created a lock")
	}
	_ = os.WriteFile(config.Paths.State, []byte("corrupt"), 0o600)
	if _, err := Refresh(context.Background(), RefreshOptions{Config: config, Apply: true, Now: now, Prober: &tableProber{results: results}}); err == nil {
		t.Fatal("corrupt state was accepted")
	}
	if got, _ := os.ReadFile(config.Paths.MirrorList); string(got) != string(mirror) {
		t.Fatal("corrupt state path changed last-good mirror output")
	}
}

func TestConfigRejectsCredentialsBoundsAndPathCollisions(t *testing.T) {
	config := mirrorTestConfig(t)
	config.Candidates[0].URI = "https://user:secret@mirror.example/ubuntu/"
	if err := config.Validate(); err == nil {
		t.Fatal("credential-bearing mirror URI accepted")
	}
	config = mirrorTestConfig(t)
	config.RefreshTimeoutSeconds = 16 * 60
	if err := config.Validate(); err == nil {
		t.Fatal("unbounded refresh timeout accepted")
	}
	config = mirrorTestConfig(t)
	config.Paths.Timer = config.Paths.Service
	if err := config.Validate(); err == nil {
		t.Fatal("colliding managed paths accepted")
	}
}

func TestRefreshCancellationAndMirrorFailurePreserveState(t *testing.T) {
	config := mirrorTestConfig(t)
	now := time.Now().UTC()
	results := probeResults(config, ProbeResult{Status: ProbeVerified, Publication: now}, ProbeResult{Status: ProbeVerified, Publication: now})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Refresh(canceled, RefreshOptions{Config: config, Apply: true, Now: now, Prober: &tableProber{results: results}}); err == nil {
		t.Fatal("canceled apply succeeded")
	}
	if _, err := os.Stat(config.Paths.Lock); !os.IsNotExist(err) {
		t.Fatal("canceled apply created a lock")
	}
	prior := []byte(`{"format":"pccontroller-ubuntu-apt-mirror-state/v1","official_references":{},"domestic_last_good":{}}`)
	if err := os.MkdirAll(filepath.Dir(config.Paths.State), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Paths.State, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.Paths.MirrorList, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(context.Background(), RefreshOptions{Config: config, Apply: true, Now: now, Prober: &tableProber{results: results}}); err == nil {
		t.Fatal("mirror replace failure succeeded")
	}
	if got, _ := os.ReadFile(config.Paths.State); string(got) != string(prior) {
		t.Fatalf("state was not rolled back: %s", got)
	}
}
