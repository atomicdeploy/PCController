package aptmirror

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProbeStatus string

const (
	ProbeVerified  ProbeStatus = "verified"
	ProbeTransient ProbeStatus = "transient"
	ProbeMissing   ProbeStatus = "missing"
	ProbeUnsafe    ProbeStatus = "unsafe"
)

type ProbeResult struct {
	Status      ProbeStatus `json:"status"`
	Publication time.Time   `json:"publication,omitempty"`
	ValidUntil  time.Time   `json:"valid_until,omitempty"`
	Detail      string      `json:"detail,omitempty"`
}

type Prober interface {
	Probe(context.Context, Config, Candidate, string) ProbeResult
}

type GoodState struct {
	Publication int64 `json:"publication_epoch"`
	LastSuccess int64 `json:"last_success_epoch"`
	ValidUntil  int64 `json:"valid_until_epoch"`
	Exact       bool  `json:"formerly_exact"`
}

type State struct {
	Format     string               `json:"format"`
	UpdatedAt  int64                `json:"updated_at_epoch"`
	References map[string]int64     `json:"official_references"`
	Good       map[string]GoodState `json:"domestic_last_good"`
}

type Route struct {
	CandidateID string `json:"candidate_id"`
	Suite       string `json:"suite"`
	Priority    int    `json:"priority"`
}

type CandidateReport struct {
	CandidateID string      `json:"candidate_id"`
	Suite       string      `json:"suite"`
	Status      ProbeStatus `json:"status"`
	Publication int64       `json:"publication_epoch,omitempty"`
	Priority    int         `json:"priority,omitempty"`
	Detail      string      `json:"detail,omitempty"`
}

type RefreshOptions struct {
	Config Config
	Apply  bool
	Now    time.Time
	Prober Prober
	Output io.Writer
}

type RefreshReport struct {
	Applied             bool              `json:"applied"`
	MirrorListPath      string            `json:"mirror_list_path"`
	StatePath           string            `json:"state_path"`
	OfficialReachable   map[string]bool   `json:"official_reachable"`
	Routes              []Route           `json:"routes"`
	Candidates          []CandidateReport `json:"candidates"`
	GeneratedMirrorList string            `json:"generated_mirror_list"`
}

func Refresh(ctx context.Context, options RefreshOptions) (RefreshReport, error) {
	config := options.Config
	if err := config.Validate(); err != nil {
		return RefreshReport{}, err
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	output := options.Output
	if output == nil {
		output = io.Discard
	}
	var unlock func()
	if options.Apply {
		if err := ctx.Err(); err != nil {
			return RefreshReport{}, fmt.Errorf("APT mirror refresh canceled before lock; no files changed: %w", err)
		}
		var err error
		unlock, err = acquireLock(config.Paths.Lock)
		if err != nil {
			return RefreshReport{}, err
		}
		defer unlock()
	}
	state, err := loadState(config.Paths.State)
	if err != nil {
		return RefreshReport{}, err
	}
	prober := options.Prober
	if prober == nil {
		prober = NewHTTPProber(config)
	}
	probeContext, cancelProbes := context.WithTimeout(ctx, config.refreshTimeout())
	defer cancelProbes()

	type keyedProbe struct {
		candidate Candidate
		suite     string
		result    ProbeResult
	}
	var probes []keyedProbe
	for _, candidate := range config.Candidates {
		for _, suite := range config.Suites() {
			if !candidateHasSuite(candidate, suite, config) {
				continue
			}
			probes = append(probes, keyedProbe{candidate: candidate, suite: suite})
		}
	}
	// Network health is independent per candidate and suite. Bound concurrency
	// so a cut-off official network cannot serialize dozens of timeout windows.
	semaphore := make(chan struct{}, 8)
	var wait sync.WaitGroup
	for index := range probes {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				probes[index].result = ProbeResult{Status: ProbeTransient, Detail: "transport"}
				return
			}
			result := prober.Probe(probeContext, config, probes[index].candidate, probes[index].suite)
			if result.Status == "" {
				result = ProbeResult{Status: ProbeUnsafe, Detail: "invalid-prober-result"}
			}
			probes[index].result = result
		}(index)
	}
	wait.Wait()

	references := cloneInt64Map(state.References)
	officialReachable := make(map[string]bool)
	for _, probe := range probes {
		if probe.candidate.Role == RoleDomestic || probe.result.Status != ProbeVerified {
			continue
		}
		epoch := probe.result.Publication.Unix()
		if epoch > references[probe.suite] {
			references[probe.suite] = epoch
		}
		officialReachable[probe.suite] = true
	}

	good := cloneGoodMap(state.Good)
	priorGood := cloneGoodMap(state.Good)
	priority := make(map[string]int)
	var candidateReports []CandidateReport
	for _, probe := range probes {
		if probe.candidate.Role != RoleDomestic {
			candidateReports = append(candidateReports, CandidateReport{
				CandidateID: probe.candidate.ID, Suite: probe.suite,
				Status: probe.result.Status, Publication: unixOrZero(probe.result.Publication),
				Detail: safeProbeDetail(probe.result.Detail),
			})
			continue
		}
		key := stateKey(probe.candidate.ID, probe.suite)
		switch probe.result.Status {
		case ProbeVerified:
			epoch := probe.result.Publication.Unix()
			selected := domesticPriority(config, probe.suite, epoch, references[probe.suite], officialReachable[probe.suite], now)
			priority[key] = selected
			good[key] = GoodState{
				Publication: epoch, LastSuccess: now.Unix(),
				ValidUntil: effectiveValidUntil(config, probe.suite, probe.result).Unix(),
				Exact:      selected == 10,
			}
		case ProbeTransient:
			previous, ok := priorGood[key]
			age := now.Unix() - previous.LastSuccess
			if ok && age >= 0 && age <= config.LastGoodTTLSeconds &&
				now.Unix() < goodStateValidUntil(config, probe.suite, previous) {
				if previous.Exact && !officialReachable[probe.suite] {
					priority[key] = 10
				} else {
					priority[key] = 950
				}
			} else {
				delete(good, key)
			}
		case ProbeMissing, ProbeUnsafe:
			delete(good, key)
		default:
			delete(good, key)
		}
		candidateReports = append(candidateReports, CandidateReport{
			CandidateID: probe.candidate.ID, Suite: probe.suite,
			Status: probe.result.Status, Publication: unixOrZero(probe.result.Publication),
			Priority: priority[key], Detail: safeProbeDetail(probe.result.Detail),
		})
	}

	var routes []Route
	for _, candidate := range config.Candidates {
		for _, suite := range config.Suites() {
			if !candidateHasSuite(candidate, suite, config) {
				continue
			}
			if candidate.Role == RoleDomestic {
				if selected := priority[stateKey(candidate.ID, suite)]; selected != 0 {
					routes = append(routes, Route{CandidateID: candidate.ID, Suite: suite, Priority: selected})
				}
			} else {
				routes = append(routes, Route{CandidateID: candidate.ID, Suite: suite, Priority: candidate.Priority})
			}
		}
	}
	generated, err := renderMirrorList(config, routes)
	if err != nil {
		return RefreshReport{}, err
	}
	report := RefreshReport{
		Applied: false, MirrorListPath: config.Paths.MirrorList,
		StatePath: config.Paths.State, OfficialReachable: officialReachable,
		Routes: routes, Candidates: candidateReports, GeneratedMirrorList: string(generated),
	}
	if !options.Apply {
		fmt.Fprintln(output, "APT mirror refresh dry-run: signed probes completed; no files changed.")
		return report, nil
	}
	if err := probeContext.Err(); err != nil {
		return report, fmt.Errorf("APT mirror refresh canceled before apply; last-good output preserved: %w", err)
	}
	state = State{
		Format: StateFormat, UpdatedAt: now.Unix(), References: references, Good: good,
	}
	encodedState, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return report, err
	}
	encodedState = append(encodedState, '\n')
	priorState, err := captureSnapshots([]string{config.Paths.State})
	if err != nil {
		return report, fmt.Errorf("snapshot APT mirror state before refresh: %w", err)
	}
	if err := atomicWrite(config.Paths.State, encodedState, 0o600); err != nil {
		return report, fmt.Errorf("atomically install APT mirror state: %w", err)
	}
	if err := atomicWrite(config.Paths.MirrorList, generated, 0o644); err != nil {
		if rollbackErr := restoreSnapshots(priorState); rollbackErr != nil {
			return report, fmt.Errorf("atomically install APT mirror list: %w; state rollback failed: %v", err, rollbackErr)
		}
		return report, fmt.Errorf("atomically install APT mirror list: %w; prior state restored", err)
	}
	report.Applied = true
	fmt.Fprintln(output, "APT mirror refresh applied atomically.")
	return report, nil
}

func effectiveValidUntil(config Config, suite string, result ProbeResult) time.Time {
	maximumAge := config.FirstRunMovingAgeSeconds
	if suite == config.Codename {
		maximumAge = config.FirstRunBaseAgeSeconds
	}
	derived := result.Publication.Add(time.Duration(maximumAge) * time.Second)
	if !result.ValidUntil.IsZero() && result.ValidUntil.Before(derived) {
		return result.ValidUntil
	}
	return derived
}

func goodStateValidUntil(config Config, suite string, state GoodState) int64 {
	if state.ValidUntil > 0 {
		return state.ValidUntil
	}
	maximumAge := config.FirstRunMovingAgeSeconds
	if suite == config.Codename {
		maximumAge = config.FirstRunBaseAgeSeconds
	}
	return state.Publication + maximumAge
}

func domesticPriority(config Config, suite string, publication, reference int64, officialReachable bool, now time.Time) int {
	threshold := int64(0)
	if reference > config.MaxLagSeconds {
		threshold = reference - config.MaxLagSeconds
	}
	if reference > 0 && publication >= threshold {
		return 10
	}
	if officialReachable {
		return 950
	}
	maximumAge := config.FirstRunMovingAgeSeconds
	if suite == config.Codename {
		maximumAge = config.FirstRunBaseAgeSeconds
	}
	age := now.Unix() - publication
	if reference == 0 && age >= 0 && age <= maximumAge {
		return 20
	}
	return 950
}

func renderMirrorList(config Config, routes []Route) ([]byte, error) {
	byIDPriority := make(map[string][]string)
	for _, route := range routes {
		key := fmt.Sprintf("%s\x00%d", route.CandidateID, route.Priority)
		byIDPriority[key] = append(byIDPriority[key], route.Suite)
	}
	var buffer bytes.Buffer
	buffer.WriteString("# Generated by PCController; DO NOT EDIT.\n")
	buffer.WriteString("# Domestic current=10, censorship-safe first-run=20, official fallback=850/900, stale/transient rescue=950.\n")
	priorities := []int{10, 20, 850, 900, 950}
	for _, wantedPriority := range priorities {
		for _, candidate := range config.Candidates {
			key := fmt.Sprintf("%s\x00%d", candidate.ID, wantedPriority)
			suites := byIDPriority[key]
			if len(suites) == 0 {
				continue
			}
			buffer.WriteString(candidate.URI)
			fmt.Fprintf(&buffer, "\tpriority:%d", wantedPriority)
			for _, suite := range config.Suites() {
				if containsString(suites, suite) {
					fmt.Fprintf(&buffer, " suite:%s", suite)
				}
			}
			buffer.WriteByte('\n')
		}
	}
	content := buffer.Bytes()
	for _, suite := range config.Suites() {
		if !bytes.Contains(content, []byte("suite:"+suite)) {
			return nil, fmt.Errorf("generated mirror list has no route for %s", suite)
		}
	}
	return append([]byte(nil), content...), nil
}

func loadState(path string) (State, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Format: StateFormat, References: map[string]int64{}, Good: map[string]GoodState{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read APT mirror state: %w", err)
	}
	var state State
	if err := json.Unmarshal(content, &state); err != nil || state.Format != StateFormat {
		return State{}, errors.New("APT mirror state is corrupt or has an unsupported format; last-good output was preserved")
	}
	if state.References == nil {
		state.References = make(map[string]int64)
	}
	if state.Good == nil {
		state.Good = make(map[string]GoodState)
	}
	return state, nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) (resultErr error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".pccontroller-apt-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func stateKey(candidate, suite string) string { return candidate + "|" + suite }

func cloneInt64Map(input map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneGoodMap(input map[string]GoodState) map[string]GoodState {
	result := make(map[string]GoodState, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func safeProbeDetail(value string) string {
	switch value {
	case "transport", "missing", "signature", "identity", "expired", "future", "index-reference", "content-hash", "invalid-prober-result":
		return value
	default:
		return ""
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func SortedRoutes(routes []Route) []Route {
	result := append([]Route(nil), routes...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		if result[i].CandidateID != result[j].CandidateID {
			return result[i].CandidateID < result[j].CandidateID
		}
		return result[i].Suite < result[j].Suite
	})
	return result
}

func SourceDeb822(config Config) []byte {
	return []byte(fmt.Sprintf(
		"Types: deb\nURIs: mirror+file:%s\nSuites: %s\nComponents: %s\nArchitectures: %s\nSigned-By: %s\n",
		config.Paths.MirrorList, strings.Join(config.Suites(), " "),
		strings.Join(config.Components, " "), config.Architecture, config.Paths.Keyring,
	))
}

func APTResilienceConfig() []byte {
	return []byte("Acquire::Queue-Mode \"host\";\nAcquire::Retries \"1\";\nAcquire::http::Timeout \"15\";\nAcquire::https::Timeout \"15\";\nAcquire::http::Dl-Limit \"0\";\n")
}
