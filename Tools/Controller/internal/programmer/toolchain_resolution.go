package programmer

//go:generate node generate-toolchain-policy.mjs

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/netpolicy"
)

const (
	ToolchainPolicyFormat = "pccontroller-toolchain-policy/v1"
	ToolchainLockFormat   = "pccontroller-toolchain-lock/v1"
)

// ToolchainConstraint selects the newest stable release that is not older than
// MinimumVersion. Exact versions and hashes belong in the generated lock file.
type ToolchainConstraint struct {
	Channel        string `json:"channel"`
	MinimumVersion string `json:"minimum_version"`
}

// ToolchainCLIAssetPolicy maps an operating-system target to a release asset.
type ToolchainCLIAssetPolicy struct {
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	Suffix  string `json:"suffix"`
	Archive string `json:"archive"`
}

// ToolchainCLIPolicy describes the upstream release channel for the dependency
// CLI without freezing the project to one release forever.
type ToolchainCLIPolicy struct {
	Dependency string                    `json:"dependency"`
	Repository string                    `json:"repository"`
	ReleaseAPI string                    `json:"release_api"`
	Constraint ToolchainConstraint       `json:"constraint"`
	Assets     []ToolchainCLIAssetPolicy `json:"assets"`
}

// ToolchainCorePolicy describes the selected board core and its package index.
type ToolchainCorePolicy struct {
	ID         string              `json:"id"`
	IndexURL   string              `json:"index_url"`
	Constraint ToolchainConstraint `json:"constraint"`
}

// ToolchainLibraryPolicy identifies one required library release channel.
type ToolchainLibraryPolicy struct {
	Name       string              `json:"name"`
	Constraint ToolchainConstraint `json:"constraint"`
}

// ToolchainBootloaderPolicy tracks stable Urboot tags and a non-deployed canary
// ref independently from the historical MiniCore stock-reproduction fixture.
type ToolchainBootloaderPolicy struct {
	Repository string              `json:"repository"`
	TagPrefix  string              `json:"tag_prefix"`
	TagsAPI    string              `json:"tags_api"`
	CommitsAPI string              `json:"commits_api"`
	CanaryRef  string              `json:"canary_ref"`
	Constraint ToolchainConstraint `json:"constraint"`
}

// ToolchainGoPolicy tracks the current stable Go toolchain. go.mod and go.sum
// remain the authoritative exact module graph.
type ToolchainGoPolicy struct {
	VersionURL string              `json:"version_url"`
	Constraint ToolchainConstraint `json:"constraint"`
}

// ToolchainTargetPolicy is the board geometry shared by compile planning,
// artifact validation, manifests, and the public Node command surfaces.
type ToolchainTargetPolicy struct {
	MCU                   string `json:"mcu"`
	ClockHz               uint32 `json:"clock_hz"`
	Bootloader            string `json:"bootloader"`
	Baud                  int    `json:"baud"`
	ApplicationLimitBytes uint32 `json:"application_limit_bytes"`
	FlashBytes            uint32 `json:"flash_bytes"`
	EEPROMBytes           uint32 `json:"eeprom_bytes"`
}

// ToolchainPolicy is the latest-compatible, source-controlled dependency
// policy. Resolution produces an exact, hash-bearing ToolchainLock.
type ToolchainPolicy struct {
	Format       string                    `json:"format"`
	Name         string                    `json:"name"`
	FQBN         string                    `json:"fqbn"`
	Target       ToolchainTargetPolicy     `json:"target"`
	LibraryIndex string                    `json:"library_index"`
	CLI          ToolchainCLIPolicy        `json:"cli"`
	Core         ToolchainCorePolicy       `json:"core"`
	Libraries    []ToolchainLibraryPolicy  `json:"libraries"`
	Bootloader   ToolchainBootloaderPolicy `json:"bootloader"`
	Go           ToolchainGoPolicy         `json:"go"`
}

// ResolvedSource records an immutable upstream artifact and its published hash.
type ResolvedSource struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// ResolvedToolchainLibrary adds exact source provenance to a library version.
type ResolvedToolchainLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ResolvedSource
}

// ResolvedBootloader identifies the exact stable Urboot source commit selected
// by policy. SourceCommit is the immutable identity; TreeHash aids auditing.
type ResolvedBootloader struct {
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	Version      string `json:"version"`
	SourceCommit string `json:"source_commit"`
	TreeHash     string `json:"tree_hash,omitempty"`
}

// ResolvedGo records the stable Go release plus hashes of the exact module lock
// files when resolution occurs inside the host module.
type ResolvedGo struct {
	Version     string `json:"version"`
	GoModSHA256 string `json:"go_mod_sha256,omitempty"`
	GoSumSHA256 string `json:"go_sum_sha256,omitempty"`
}

// ToolchainLock is generated from ToolchainPolicy. It is suitable for exact
// bootstrap, reproducible rollback, and dependency-review diffs.
type ToolchainLock struct {
	Format     string                     `json:"format"`
	PolicyName string                     `json:"policy_name"`
	ResolvedAt string                     `json:"resolved_at_utc"`
	Firmware   ToolchainProfile           `json:"firmware"`
	CoreSource ResolvedSource             `json:"core_source"`
	Libraries  []ResolvedToolchainLibrary `json:"libraries"`
	Bootloader ResolvedBootloader         `json:"bootloader"`
	Go         ResolvedGo                 `json:"go"`
}

// ToolchainCanary reports moving upstream candidates without selecting or
// installing them. CI may compile/test these observations separately.
type ToolchainCanary struct {
	CLIRelease       string `json:"cli_release,omitempty"`
	BootloaderRef    string `json:"bootloader_ref,omitempty"`
	BootloaderCommit string `json:"bootloader_commit,omitempty"`
	BootloaderTree   string `json:"bootloader_tree,omitempty"`
}

// ToolchainResolution contains the exact stable lock and optional canary data.
type ToolchainResolution struct {
	Lock   ToolchainLock   `json:"lock"`
	Canary ToolchainCanary `json:"canary,omitempty"`
}

// ToolchainChange is a deterministic, human-readable lock comparison entry.
type ToolchainChange struct {
	Area     string `json:"area"`
	Name     string `json:"name"`
	Current  string `json:"current,omitempty"`
	Resolved string `json:"resolved"`
}

// ToolchainResolveOptions controls network and filesystem inputs. DirectRetry
// retries failed HTTP reads without proxy variables but never mutates os.Environ.
type ToolchainResolveOptions struct {
	Environment   []string
	DirectRetry   bool
	IncludeCanary bool
	HTTPClient    *http.Client
	Now           func() time.Time
	ModuleDir     string
}

func DefaultToolchainPolicy() ToolchainPolicy {
	var policy ToolchainPolicy
	if err := json.Unmarshal([]byte(generatedToolchainPolicyJSON), &policy); err != nil {
		panic(fmt.Sprintf("decode generated toolchain policy: %v", err))
	}
	if err := policy.Validate(); err != nil {
		panic(fmt.Sprintf("validate generated toolchain policy: %v", err))
	}
	return policy
}

// DefaultFQBN returns the board target generated from toolchain-profile.json.
// Keeping this as a function prevents another authored compile-time definition.
func DefaultFQBN() string {
	return DefaultToolchainPolicy().FQBN
}

// DefaultBoardTarget returns the board geometry generated from the canonical
// toolchain policy. Callers must not author a second target definition.
func DefaultBoardTarget() ToolchainTargetPolicy {
	return DefaultToolchainPolicy().Target
}

func LoadToolchainPolicy(path string) (ToolchainPolicy, error) {
	if strings.TrimSpace(path) == "" {
		policy := DefaultToolchainPolicy()
		return policy, policy.Validate()
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolchainPolicy{}, fmt.Errorf("read toolchain policy: %w", err)
	}
	var policy ToolchainPolicy
	if err := json.Unmarshal(content, &policy); err != nil {
		return ToolchainPolicy{}, fmt.Errorf("decode toolchain policy: %w", err)
	}
	return policy, policy.Validate()
}

func (policy ToolchainPolicy) Validate() error {
	if policy.Format != ToolchainPolicyFormat {
		return fmt.Errorf("unsupported toolchain policy format %q", policy.Format)
	}
	if strings.TrimSpace(policy.Name) == "" || strings.TrimSpace(policy.FQBN) == "" {
		return errors.New("toolchain policy requires name and FQBN")
	}
	if strings.TrimSpace(policy.Target.MCU) == "" ||
		strings.TrimSpace(policy.Target.Bootloader) == "" ||
		policy.Target.ClockHz == 0 || policy.Target.Baud <= 0 ||
		policy.Target.ApplicationLimitBytes == 0 || policy.Target.FlashBytes == 0 ||
		policy.Target.EEPROMBytes == 0 ||
		policy.Target.ApplicationLimitBytes >= policy.Target.FlashBytes {
		return errors.New("toolchain policy requires a valid board target and memory geometry")
	}
	if policy.CLI.Dependency == "" || policy.CLI.Repository == "" || policy.CLI.ReleaseAPI == "" || len(policy.CLI.Assets) == 0 {
		return errors.New("toolchain policy requires CLI repository, release API, and assets")
	}
	if policy.Core.ID == "" || policy.Core.IndexURL == "" || policy.LibraryIndex == "" {
		return errors.New("toolchain policy requires core and library indexes")
	}
	if policy.Bootloader.Repository == "" || policy.Bootloader.TagsAPI == "" || policy.Bootloader.CommitsAPI == "" || policy.Go.VersionURL == "" {
		return errors.New("toolchain policy requires bootloader and Go sources")
	}
	constraints := []ToolchainConstraint{policy.CLI.Constraint, policy.Core.Constraint, policy.Bootloader.Constraint, policy.Go.Constraint}
	for _, library := range policy.Libraries {
		if strings.TrimSpace(library.Name) == "" {
			return errors.New("toolchain library policy requires a name")
		}
		constraints = append(constraints, library.Constraint)
	}
	for _, constraint := range constraints {
		if constraint.Channel != "stable" || strings.TrimSpace(constraint.MinimumVersion) == "" {
			return errors.New("toolchain constraints currently require stable channel and a minimum version")
		}
		if _, ok := parseStableVersion(constraint.MinimumVersion); !ok {
			return fmt.Errorf("invalid stable minimum version %q", constraint.MinimumVersion)
		}
	}
	for _, asset := range policy.CLI.Assets {
		if asset.GOOS == "" || asset.GOARCH == "" || asset.Suffix == "" || (asset.Archive != "zip" && asset.Archive != "tar.gz") {
			return errors.New("toolchain CLI asset policy is incomplete")
		}
	}
	return nil
}

func LoadToolchainLock(path string) (ToolchainLock, error) {
	if strings.TrimSpace(path) == "" {
		var lock ToolchainLock
		if err := json.Unmarshal([]byte(generatedToolchainLockJSON), &lock); err != nil {
			return ToolchainLock{}, fmt.Errorf("decode embedded toolchain lock: %w", err)
		}
		return lock, lock.Validate()
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolchainLock{}, fmt.Errorf("read toolchain lock: %w", err)
	}
	var lock ToolchainLock
	if err := json.Unmarshal(content, &lock); err != nil {
		return ToolchainLock{}, fmt.Errorf("decode toolchain lock: %w", err)
	}
	return lock, lock.Validate()
}

func (lock ToolchainLock) Validate() error {
	if lock.Format != ToolchainLockFormat || lock.PolicyName == "" || lock.ResolvedAt == "" {
		return errors.New("toolchain lock format, policy name, or resolution time is missing")
	}
	if _, err := time.Parse(time.RFC3339, lock.ResolvedAt); err != nil {
		return fmt.Errorf("invalid toolchain lock resolution time: %w", err)
	}
	if err := lock.Firmware.Validate(); err != nil {
		return err
	}
	if lock.Bootloader.Tag == "" || lock.Bootloader.SourceCommit == "" || lock.Go.Version == "" {
		return errors.New("toolchain lock requires bootloader and Go identities")
	}
	return nil
}

func WriteToolchainLock(path string, lock ToolchainLock) error {
	if err := lock.Validate(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode toolchain lock: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := writeFileAtomicReplace(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write toolchain lock: %w", err)
	}
	return nil
}

// UpdateToolchainLock writes only substantive dependency changes. A resolver
// timestamp alone never dirties the repository or creates an empty update PR.
func UpdateToolchainLock(path string, current, resolved ToolchainLock) (bool, error) {
	if len(CompareToolchainLocks(current, resolved)) == 0 {
		return false, nil
	}
	if err := WriteToolchainLock(path, resolved); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveToolchainPolicy resolves stable upstream releases from primary
// registries. It performs no device I/O and does not install dependencies.
func ResolveToolchainPolicy(ctx context.Context, policy ToolchainPolicy, options ToolchainResolveOptions) (ToolchainResolution, error) {
	if err := policy.Validate(); err != nil {
		return ToolchainResolution{}, err
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	environment = netpolicy.WithLocalNetworkNoProxy(environment)
	fetcher := newToolchainFetcher(options.HTTPClient, environment, options.DirectRetry)
	cli, cliCanary, err := resolveCLI(ctx, fetcher, policy.CLI)
	if err != nil {
		return ToolchainResolution{}, err
	}
	coreVersion, coreSource, err := resolveCore(ctx, fetcher, policy.Core)
	if err != nil {
		return ToolchainResolution{}, err
	}
	libraries, err := resolveLibraries(ctx, fetcher, policy.LibraryIndex, policy.Libraries)
	if err != nil {
		return ToolchainResolution{}, err
	}
	bootloader, bootCanary, err := resolveBootloader(ctx, fetcher, policy.Bootloader, options.IncludeCanary)
	if err != nil {
		return ToolchainResolution{}, err
	}
	goVersion, err := resolveGo(ctx, fetcher, policy.Go)
	if err != nil {
		return ToolchainResolution{}, err
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	firmwareLibraries := make([]ToolchainLibrary, 0, len(libraries))
	for _, library := range libraries {
		firmwareLibraries = append(firmwareLibraries, ToolchainLibrary{Name: library.Name, Version: library.Version})
	}
	moduleDir := strings.TrimSpace(options.ModuleDir)
	if moduleDir == "" {
		moduleDir = "."
	}
	resolvedGo := ResolvedGo{Version: goVersion}
	resolvedGo.GoModSHA256, _ = fileSHA256(filepath.Join(moduleDir, "go.mod"))
	resolvedGo.GoSumSHA256, _ = fileSHA256(filepath.Join(moduleDir, "go.sum"))
	lock := ToolchainLock{
		Format: ToolchainLockFormat, PolicyName: policy.Name,
		ResolvedAt: now().UTC().Format(time.RFC3339),
		Firmware: ToolchainProfile{
			Name: policy.Name, FQBN: policy.FQBN,
			PackageIndexes: []string{policy.Core.IndexURL}, CLI: cli,
			CoreID: policy.Core.ID, CoreVersion: coreVersion,
			Libraries: firmwareLibraries,
			ProvidedTools: []string{
				"AVR compiler/programmer tools supplied by the resolved MiniCore package",
				"Urboot/Urclock UART programmer metadata supplied by the resolved MiniCore package",
			},
		},
		CoreSource: coreSource, Libraries: libraries,
		Bootloader: bootloader, Go: resolvedGo,
	}
	if err := lock.Validate(); err != nil {
		return ToolchainResolution{}, fmt.Errorf("resolved toolchain lock is invalid: %w", err)
	}
	result := ToolchainResolution{Lock: lock}
	if options.IncludeCanary {
		result.Canary = ToolchainCanary{
			CLIRelease: cliCanary, BootloaderRef: policy.Bootloader.CanaryRef,
			BootloaderCommit: bootCanary.SourceCommit, BootloaderTree: bootCanary.TreeHash,
		}
	}
	return result, nil
}

func CompareToolchainLocks(current, resolved ToolchainLock) []ToolchainChange {
	var changes []ToolchainChange
	add := func(area, name, before, after string) {
		if before != after {
			changes = append(changes, ToolchainChange{Area: area, Name: name, Current: before, Resolved: after})
		}
	}
	add("firmware", resolved.Firmware.CLI.Dependency, current.Firmware.CLI.Version, resolved.Firmware.CLI.Version)
	add("firmware", "FQBN", current.Firmware.FQBN, resolved.Firmware.FQBN)
	currentAssets := make(map[string]ToolchainAsset)
	for _, asset := range current.Firmware.CLI.Assets {
		currentAssets[asset.GOOS+"/"+asset.GOARCH] = asset
	}
	for _, asset := range resolved.Firmware.CLI.Assets {
		name := asset.GOOS + "/" + asset.GOARCH
		before := currentAssets[name]
		add("cli-artifact", name, before.URL+"@"+before.SHA256, asset.URL+"@"+asset.SHA256)
	}
	add("firmware", resolved.Firmware.CoreID, current.Firmware.CoreVersion, resolved.Firmware.CoreVersion)
	add("core-artifact", resolved.Firmware.CoreID, current.CoreSource.URL+"@"+current.CoreSource.SHA256, resolved.CoreSource.URL+"@"+resolved.CoreSource.SHA256)
	currentLibraries := make(map[string]string)
	currentLibrarySources := make(map[string]ResolvedSource)
	for _, library := range current.Firmware.Libraries {
		currentLibraries[library.Name] = library.Version
	}
	for _, library := range current.Libraries {
		currentLibrarySources[library.Name] = library.ResolvedSource
	}
	for _, library := range resolved.Firmware.Libraries {
		add("library", library.Name, currentLibraries[library.Name], library.Version)
	}
	for _, library := range resolved.Libraries {
		before := currentLibrarySources[library.Name]
		add("library-artifact", library.Name, before.URL+"@"+before.SHA256, library.URL+"@"+library.SHA256)
	}
	add("bootloader", resolved.Bootloader.Repository, current.Bootloader.Tag+"@"+current.Bootloader.SourceCommit, resolved.Bootloader.Tag+"@"+resolved.Bootloader.SourceCommit)
	add("bootloader", "source-tree", current.Bootloader.TreeHash, resolved.Bootloader.TreeHash)
	add("host", "Go", current.Go.Version, resolved.Go.Version)
	add("host-lock", "go.mod", current.Go.GoModSHA256, resolved.Go.GoModSHA256)
	add("host-lock", "go.sum", current.Go.GoSumSHA256, resolved.Go.GoSumSHA256)
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Area == changes[j].Area {
			return changes[i].Name < changes[j].Name
		}
		return changes[i].Area < changes[j].Area
	})
	return changes
}

type toolchainFetcher struct {
	configured *http.Client
	direct     *http.Client
	retry      bool
	token      string
}

func newToolchainFetcher(client *http.Client, environment []string, directRetry bool) toolchainFetcher {
	if client != nil {
		return toolchainFetcher{configured: client, retry: false, token: environmentValue(environment, "GITHUB_TOKEN")}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFromEnvironmentSlice(environment)
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	return toolchainFetcher{
		configured: &http.Client{Transport: transport, Timeout: 2 * time.Minute},
		direct:     &http.Client{Transport: direct, Timeout: 2 * time.Minute},
		retry:      directRetry, token: firstNonEmpty(environmentValue(environment, "GITHUB_TOKEN"), environmentValue(environment, "GH_TOKEN")),
	}
}

func (fetcher toolchainFetcher) get(ctx context.Context, source string, maximum int64) ([]byte, error) {
	read := func(client *http.Client) ([]byte, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "PCController-toolchain-resolver/1")
		request.Header.Set("Accept", "application/vnd.github+json, application/json, text/plain;q=0.9, */*;q=0.1")
		if fetcher.token != "" && strings.Contains(request.URL.Host, "github.com") {
			request.Header.Set("Authorization", "Bearer "+fetcher.token)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned HTTP %s", source, response.Status)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > maximum {
			return nil, fmt.Errorf("%s exceeds %d-byte resolution limit", source, maximum)
		}
		return body, nil
	}
	body, err := read(fetcher.configured)
	if err == nil || !fetcher.retry || fetcher.direct == nil {
		return body, err
	}
	directBody, directErr := read(fetcher.direct)
	if directErr != nil {
		return nil, errors.Join(fmt.Errorf("configured-network request: %w", err), fmt.Errorf("direct request: %w", directErr))
	}
	return directBody, nil
}

func resolveCLI(ctx context.Context, fetcher toolchainFetcher, policy ToolchainCLIPolicy) (ToolchainCLI, string, error) {
	type asset struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	}
	type release struct {
		TagName    string  `json:"tag_name"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []asset `json:"assets"`
	}
	body, err := fetcher.get(ctx, policy.ReleaseAPI, 8<<20)
	if err != nil {
		return ToolchainCLI{}, "", fmt.Errorf("resolve firmware CLI releases: %w", err)
	}
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return ToolchainCLI{}, "", fmt.Errorf("decode firmware CLI releases: %w", err)
	}
	var stable *release
	var prerelease string
	for index := range releases {
		candidate := &releases[index]
		if candidate.Draft {
			continue
		}
		version := strings.TrimPrefix(candidate.TagName, "v")
		if candidate.Prerelease || strings.Contains(version, "-") {
			if prerelease == "" {
				prerelease = candidate.TagName
			}
			continue
		}
		if _, ok := parseStableVersion(version); !ok {
			continue
		}
		if stable == nil || compareStableVersions(version, strings.TrimPrefix(stable.TagName, "v")) > 0 {
			stable = candidate
		}
	}
	if stable == nil {
		return ToolchainCLI{}, prerelease, errors.New("firmware CLI registry has no stable semantic release")
	}
	version := strings.TrimPrefix(stable.TagName, "v")
	if compareStableVersions(version, policy.Constraint.MinimumVersion) < 0 {
		return ToolchainCLI{}, prerelease, fmt.Errorf("latest firmware CLI %s is older than required minimum %s", version, policy.Constraint.MinimumVersion)
	}
	resolved := ToolchainCLI{Dependency: policy.Dependency, Version: version}
	for _, wanted := range policy.Assets {
		var found *asset
		for index := range stable.Assets {
			if strings.HasSuffix(stable.Assets[index].Name, wanted.Suffix) {
				found = &stable.Assets[index]
				break
			}
		}
		if found == nil {
			return ToolchainCLI{}, prerelease, fmt.Errorf("firmware CLI %s lacks %s/%s asset suffix %s", version, wanted.GOOS, wanted.GOARCH, wanted.Suffix)
		}
		digest := strings.TrimPrefix(found.Digest, "sha256:")
		if !validSHA256(digest) {
			return ToolchainCLI{}, prerelease, fmt.Errorf("firmware CLI asset %s lacks a valid published SHA-256 digest", found.Name)
		}
		resolved.Assets = append(resolved.Assets, ToolchainAsset{
			GOOS: wanted.GOOS, GOARCH: wanted.GOARCH, URL: found.URL,
			SHA256: digest, Archive: wanted.Archive,
		})
	}
	return resolved, prerelease, nil
}

func resolveCore(ctx context.Context, fetcher toolchainFetcher, policy ToolchainCorePolicy) (string, ResolvedSource, error) {
	type platform struct {
		Architecture string `json:"architecture"`
		Version      string `json:"version"`
		URL          string `json:"url"`
		Checksum     string `json:"checksum"`
	}
	type packageEntry struct {
		Name      string     `json:"name"`
		Platforms []platform `json:"platforms"`
	}
	var index struct {
		Packages []packageEntry `json:"packages"`
	}
	body, err := fetcher.get(ctx, policy.IndexURL, 32<<20)
	if err != nil {
		return "", ResolvedSource{}, fmt.Errorf("resolve board core: %w", err)
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return "", ResolvedSource{}, fmt.Errorf("decode board core index: %w", err)
	}
	parts := strings.Split(policy.ID, ":")
	if len(parts) != 2 {
		return "", ResolvedSource{}, fmt.Errorf("invalid core ID %q", policy.ID)
	}
	var selected *platform
	for _, packageItem := range index.Packages {
		if packageItem.Name != parts[0] {
			continue
		}
		for index := range packageItem.Platforms {
			candidate := &packageItem.Platforms[index]
			if candidate.Architecture != parts[1] {
				continue
			}
			if _, ok := parseStableVersion(candidate.Version); !ok {
				continue
			}
			if selected == nil || compareStableVersions(candidate.Version, selected.Version) > 0 {
				copy := *candidate
				selected = &copy
			}
		}
	}
	if selected == nil || compareStableVersions(selected.Version, policy.Constraint.MinimumVersion) < 0 {
		return "", ResolvedSource{}, fmt.Errorf("no compatible stable release found for %s", policy.ID)
	}
	digest := strings.TrimPrefix(selected.Checksum, "SHA-256:")
	if !validSHA256(digest) {
		return "", ResolvedSource{}, fmt.Errorf("core %s@%s has invalid SHA-256", policy.ID, selected.Version)
	}
	return selected.Version, ResolvedSource{URL: selected.URL, SHA256: digest}, nil
}

func resolveLibraries(ctx context.Context, fetcher toolchainFetcher, source string, policies []ToolchainLibraryPolicy) ([]ResolvedToolchainLibrary, error) {
	type library struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		URL      string `json:"url"`
		Checksum string `json:"checksum"`
	}
	var index struct {
		Libraries []library `json:"libraries"`
	}
	body, err := fetcher.get(ctx, source, 32<<20)
	if err != nil {
		return nil, fmt.Errorf("resolve library index: %w", err)
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		reader, gzipErr := gzip.NewReader(bytes.NewReader(body))
		if gzipErr != nil {
			return nil, fmt.Errorf("open compressed library index: %w", gzipErr)
		}
		decompressed, readErr := io.ReadAll(io.LimitReader(reader, 128<<20))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(readErr, closeErr)
		}
		body = decompressed
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("decode library index: %w", err)
	}
	resolved := make([]ResolvedToolchainLibrary, 0, len(policies))
	for _, wanted := range policies {
		var selected *library
		for itemIndex := range index.Libraries {
			candidate := &index.Libraries[itemIndex]
			if candidate.Name != wanted.Name {
				continue
			}
			if _, ok := parseStableVersion(candidate.Version); !ok {
				continue
			}
			if selected == nil || compareStableVersions(candidate.Version, selected.Version) > 0 {
				copy := *candidate
				selected = &copy
			}
		}
		if selected == nil || compareStableVersions(selected.Version, wanted.Constraint.MinimumVersion) < 0 {
			return nil, fmt.Errorf("no compatible stable release found for library %s", wanted.Name)
		}
		digest := strings.TrimPrefix(selected.Checksum, "SHA-256:")
		if !validSHA256(digest) {
			return nil, fmt.Errorf("library %s@%s has invalid SHA-256", wanted.Name, selected.Version)
		}
		resolved = append(resolved, ResolvedToolchainLibrary{
			Name: wanted.Name, Version: selected.Version,
			ResolvedSource: ResolvedSource{URL: selected.URL, SHA256: digest},
		})
	}
	return resolved, nil
}

func resolveBootloader(ctx context.Context, fetcher toolchainFetcher, policy ToolchainBootloaderPolicy, includeCanary bool) (ResolvedBootloader, ResolvedBootloader, error) {
	type tag struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	body, err := fetcher.get(ctx, policy.TagsAPI, 4<<20)
	if err != nil {
		return ResolvedBootloader{}, ResolvedBootloader{}, fmt.Errorf("resolve Urboot tags: %w", err)
	}
	var tags []tag
	if err := json.Unmarshal(body, &tags); err != nil {
		return ResolvedBootloader{}, ResolvedBootloader{}, fmt.Errorf("decode Urboot tags: %w", err)
	}
	var selected *tag
	for index := range tags {
		if !strings.HasPrefix(tags[index].Name, policy.TagPrefix) {
			continue
		}
		version := strings.TrimPrefix(tags[index].Name, policy.TagPrefix)
		if _, ok := parseStableVersion(version); !ok {
			continue
		}
		if selected == nil || compareStableVersions(version, strings.TrimPrefix(selected.Name, policy.TagPrefix)) > 0 {
			copy := tags[index]
			selected = &copy
		}
	}
	if selected == nil {
		return ResolvedBootloader{}, ResolvedBootloader{}, errors.New("Urboot registry has no stable prefixed semantic tag")
	}
	version := strings.TrimPrefix(selected.Name, policy.TagPrefix)
	if compareStableVersions(version, policy.Constraint.MinimumVersion) < 0 {
		return ResolvedBootloader{}, ResolvedBootloader{}, fmt.Errorf("latest Urboot %s is older than required minimum %s", version, policy.Constraint.MinimumVersion)
	}
	stable, err := resolveGitHubCommit(ctx, fetcher, policy.CommitsAPI, selected.Name)
	if err != nil {
		return ResolvedBootloader{}, ResolvedBootloader{}, err
	}
	stable.Tag, stable.Version, stable.Repository = selected.Name, version, policy.Repository
	var canary ResolvedBootloader
	if includeCanary && policy.CanaryRef != "" {
		canary, err = resolveGitHubCommit(ctx, fetcher, policy.CommitsAPI, policy.CanaryRef)
		if err != nil {
			return ResolvedBootloader{}, ResolvedBootloader{}, err
		}
		canary.Tag, canary.Repository = policy.CanaryRef, policy.Repository
	}
	return stable, canary, nil
}

func resolveGitHubCommit(ctx context.Context, fetcher toolchainFetcher, commitsAPI, ref string) (ResolvedBootloader, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`).MatchString(ref) {
		return ResolvedBootloader{}, fmt.Errorf("unsafe GitHub ref %q", ref)
	}
	source := strings.TrimRight(commitsAPI, "/") + "/" + url.PathEscape(ref)
	body, err := fetcher.get(ctx, source, 2<<20)
	if err != nil {
		return ResolvedBootloader{}, fmt.Errorf("resolve Urboot commit %s: %w", ref, err)
	}
	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &commit); err != nil {
		return ResolvedBootloader{}, fmt.Errorf("decode Urboot commit %s: %w", ref, err)
	}
	if !validGitHash(commit.SHA) || !validGitHash(commit.Commit.Tree.SHA) {
		return ResolvedBootloader{}, fmt.Errorf("Urboot ref %s returned invalid commit or tree hash", ref)
	}
	return ResolvedBootloader{SourceCommit: commit.SHA, TreeHash: commit.Commit.Tree.SHA}, nil
}

func resolveGo(ctx context.Context, fetcher toolchainFetcher, policy ToolchainGoPolicy) (string, error) {
	body, err := fetcher.get(ctx, policy.VersionURL, 64<<10)
	if err != nil {
		return "", fmt.Errorf("resolve Go stable release: %w", err)
	}
	line := strings.TrimSpace(strings.SplitN(string(body), "\n", 2)[0])
	version := strings.TrimPrefix(line, "go")
	if _, ok := parseStableVersion(version); !ok || compareStableVersions(version, policy.Constraint.MinimumVersion) < 0 {
		return "", fmt.Errorf("Go registry returned incompatible stable version %q", line)
	}
	return version, nil
}

func parseStableVersion(value string) ([]int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" || strings.ContainsAny(value, "-+") {
		return nil, false
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, false
	}
	result := make([]int, len(parts))
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return nil, false
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return nil, false
		}
		result[index] = parsed
	}
	return result, true
}

func compareStableVersions(left, right string) int {
	a, aOK := parseStableVersion(left)
	b, bOK := parseStableVersion(right)
	if !aOK || !bOK {
		return strings.Compare(left, right)
	}
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		var av, bv int
		if index < len(a) {
			av = a[index]
		}
		if index < len(b) {
			bv = b[index]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validGitHash(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func environmentValue(environment []string, wanted string) string {
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), wanted) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func runtimeTarget() string { return runtime.GOOS + "/" + runtime.GOARCH }
