package aptmirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type InstallOptions struct {
	Config           Config
	Apply            bool
	Now              time.Time
	Prober           Prober
	ExecutableSource string
	ProxyEnvironment []string
	Output           io.Writer
}

type InstallReport struct {
	Applied             bool          `json:"applied"`
	BackupDirectory     string        `json:"backup_directory,omitempty"`
	ExecutableSource    string        `json:"executable_source"`
	ExecutableTarget    string        `json:"executable_target"`
	ExecutableSHA256    string        `json:"executable_sha256"`
	ActiveUbuntuSources []string      `json:"active_ubuntu_sources,omitempty"`
	AdoptedTopology     []string      `json:"adopted_topology,omitempty"`
	InactiveInventory   []string      `json:"inactive_source_inventory,omitempty"`
	DisabledSources     []string      `json:"disabled_sources,omitempty"`
	ManagedFiles        []string      `json:"managed_files"`
	Refresh             RefreshReport `json:"refresh"`
	rollback            func() error
}

func (report *InstallReport) Rollback() error {
	if report.rollback == nil {
		return nil
	}
	err := report.rollback()
	report.rollback = nil
	report.Applied = false
	return err
}

func (report *InstallReport) Commit() { report.rollback = nil }

func Install(ctx context.Context, options InstallOptions) (report InstallReport, resultErr error) {
	config := options.Config
	if err := config.Validate(); err != nil {
		return InstallReport{}, err
	}
	output := options.Output
	if output == nil {
		output = io.Discard
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	executable := filepath.Clean(strings.TrimSpace(options.ExecutableSource))
	if executable == "." || !filepath.IsAbs(executable) {
		return InstallReport{}, errors.New("APT mirror profile requires an absolute current Controller executable path")
	}
	executableContent, err := os.ReadFile(executable)
	if err != nil {
		return InstallReport{}, fmt.Errorf("read current Controller executable: %w", err)
	}
	digest := sha256.Sum256(executableContent)
	plan, err := planUbuntuSources(config)
	if err != nil {
		return InstallReport{}, err
	}
	refresh, err := Refresh(ctx, RefreshOptions{
		Config: config, Apply: false, Now: now, Prober: options.Prober, Output: output,
	})
	if err != nil {
		return InstallReport{}, err
	}
	managedContent, err := managedMirrorFiles(config, executableContent, options.ProxyEnvironment, plan)
	if err != nil {
		return InstallReport{}, err
	}
	managedPaths := make([]string, 0, len(managedContent)+2)
	for path := range managedContent {
		managedPaths = append(managedPaths, path)
	}
	managedPaths = append(managedPaths, config.Paths.MirrorList, config.Paths.State)
	managedPaths = uniqueSortedStrings(managedPaths)
	report = InstallReport{
		Applied: false, ExecutableSource: executable,
		ExecutableTarget:    config.Paths.StableExecutable,
		ExecutableSHA256:    hex.EncodeToString(digest[:]),
		ActiveUbuntuSources: append([]string(nil), plan.ActiveUbuntu...),
		AdoptedTopology:     append([]string(nil), plan.ExistingTopology...),
		InactiveInventory:   append([]string(nil), plan.InactiveInventory...),
		ManagedFiles:        managedPaths, Refresh: refresh,
	}
	for path := range plan.Edits {
		report.DisabledSources = append(report.DisabledSources, path)
	}
	sort.Strings(report.DisabledSources)
	if !options.Apply {
		fmt.Fprintln(output, "APT domestic-first install dry-run: topology inventoried and signed health probed; no files changed.")
		return report, nil
	}
	if err := validateMirrorApply(executable, managedPaths, config.Paths.BackupRoot); err != nil {
		return report, err
	}
	snapshots, err := captureSnapshots(managedPaths)
	if err != nil {
		return report, err
	}
	backupDirectory, err := writeBackup(config.Paths.BackupRoot, now, snapshots)
	if err != nil {
		return report, err
	}
	report.BackupDirectory = backupDirectory
	report.rollback = func() error { return restoreSnapshots(snapshots) }
	failed := true
	defer func() {
		if failed {
			if rollbackErr := report.Rollback(); rollbackErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore APT mirror rollback snapshot: %w", rollbackErr))
			}
		}
	}()
	// Install non-source support files first, then generate a verified mirror
	// list/state, and only then switch active APT source stanzas. At no point may
	// APT observe a canonical mirror+file source whose target list is absent.
	for _, path := range uniqueSortedMapKeys(managedContent) {
		_, legacySource := plan.Edits[path]
		if path == config.Paths.CanonicalSource || legacySource {
			continue
		}
		mode := managedFileMode(config, path)
		if err := atomicWrite(path, managedContent[path], mode); err != nil {
			return report, fmt.Errorf("install managed APT mirror file %s: %w", path, err)
		}
	}
	refresh, err = Refresh(ctx, RefreshOptions{
		Config: config, Apply: true, Now: now, Prober: options.Prober, Output: output,
	})
	if err != nil {
		return report, err
	}
	report.Refresh = refresh
	if err := activateManagedSources(config, managedContent, plan, atomicWrite); err != nil {
		return report, err
	}
	report.Applied = true
	failed = false
	fmt.Fprintln(output, "APT domestic-first profile installed with rollback backup:", backupDirectory)
	return report, nil
}

func activateManagedSources(
	config Config,
	managedContent map[string][]byte,
	plan sourcePlan,
	write func(string, []byte, os.FileMode) error,
) error {
	canonical, ok := managedContent[config.Paths.CanonicalSource]
	if !ok {
		return errors.New("managed APT topology has no canonical Ubuntu source")
	}
	// The canonical source must become active before any legacy Ubuntu source
	// is disabled. A power loss between atomic renames can therefore leave only
	// a temporary duplicate topology, never a host with no Ubuntu source.
	if err := write(config.Paths.CanonicalSource, canonical, managedFileMode(config, config.Paths.CanonicalSource)); err != nil {
		return fmt.Errorf("activate canonical APT mirror source %s: %w", config.Paths.CanonicalSource, err)
	}
	for _, path := range uniqueSortedMapKeys(plan.Edits) {
		if err := write(path, managedContent[path], managedFileMode(config, path)); err != nil {
			return fmt.Errorf("disable adopted legacy APT source %s: %w", path, err)
		}
	}
	return nil
}

func managedMirrorFiles(config Config, executable []byte, environment []string, plan sourcePlan) (map[string][]byte, error) {
	encodedConfig, err := EncodeConfig(config)
	if err != nil {
		return nil, err
	}
	result := map[string][]byte{
		config.Paths.StableExecutable: executable,
		config.Paths.InstalledConfig:  encodedConfig,
		config.Paths.CanonicalSource:  SourceDeb822(config),
		config.Paths.APTResilience:    APTResilienceConfig(config),
		config.Paths.ProxyEnvironment: proxyEnvironmentFile(environment),
		config.Paths.Service:          SystemdService(config),
		config.Paths.Timer:            SystemdTimer(),
	}
	for path, content := range plan.Edits {
		result[path] = content
	}
	return result, nil
}

func SystemdService(config Config) []byte {
	return []byte(fmt.Sprintf(`[Unit]
Description=PCController signed Ubuntu mirror health refresh
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
TimeoutStartSec=5min
EnvironmentFile=-%s
ExecStart=%s toolchain mirror-refresh --config %s --apply --json
User=root
Group=root
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s %s %s
`, config.Paths.ProxyEnvironment, config.Paths.StableExecutable, config.Paths.InstalledConfig,
		filepath.Dir(config.Paths.MirrorList), filepath.Dir(config.Paths.State), filepath.Dir(config.Paths.Lock)))
}

func managedFileMode(config Config, path string) os.FileMode {
	if path == config.Paths.StableExecutable {
		return 0o755
	}
	if path == config.Paths.ProxyEnvironment {
		return 0o600
	}
	return 0o644
}

func proxyEnvironmentFile(environment []string) []byte {
	type selectedValue struct {
		value          string
		exactUppercase bool
	}
	values := make(map[string]selectedValue)
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		upper := strings.ToUpper(name)
		if !found || strings.ContainsAny(value, "\r\n\x00") {
			continue
		}
		switch upper {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY":
			candidate := selectedValue{value: value, exactUppercase: name == upper}
			if existing, ok := values[upper]; !ok || (!existing.exactUppercase && candidate.exactUppercase) {
				values[upper] = candidate
			}
		}
	}
	if fallback, ok := values["ALL_PROXY"]; ok {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY"} {
			if _, exists := values[name]; !exists {
				values[name] = fallback
			}
		}
	}
	var names []string
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var output strings.Builder
	output.WriteString("# Root-readable proxy environment captured by PCController; values are never logged.\n")
	for _, name := range names {
		value := strings.ReplaceAll(values[name].value, "\\", "\\\\")
		value = strings.ReplaceAll(value, "\"", "\\\"")
		fmt.Fprintf(&output, "%s=\"%s\"\n", name, value)
	}
	return []byte(output.String())
}

func SystemdTimer() []byte {
	return []byte(`[Unit]
Description=Refresh PCController Ubuntu mirror health

[Timer]
OnBootSec=2min
OnUnitActiveSec=2h
RandomizedDelaySec=2min
Persistent=true
Unit=pccontroller-apt-mirror-health.service

[Install]
WantedBy=timers.target
`)
}

type fileSnapshot struct {
	Path    string      `json:"path"`
	Exists  bool        `json:"exists"`
	Mode    os.FileMode `json:"mode"`
	SHA256  string      `json:"sha256,omitempty"`
	Content []byte      `json:"-"`
}

func captureSnapshots(paths []string) ([]fileSnapshot, error) {
	result := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, fileSnapshot{Path: path})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("refusing non-regular managed path %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		digest := sha256.Sum256(content)
		result = append(result, fileSnapshot{
			Path: path, Exists: true, Mode: info.Mode().Perm(),
			SHA256: hex.EncodeToString(digest[:]), Content: content,
		})
	}
	return result, nil
}

func writeBackup(root string, now time.Time, snapshots []fileSnapshot) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create APT mirror backup root: %w", err)
	}
	directory, err := os.MkdirTemp(root, "pccontroller-apt-mirrors-"+now.Format("20060102T150405Z")+"-")
	if err != nil {
		return "", fmt.Errorf("create APT mirror backup: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", err
	}
	type manifestEntry struct {
		Path   string      `json:"path"`
		Exists bool        `json:"exists"`
		Mode   os.FileMode `json:"mode,omitempty"`
		SHA256 string      `json:"sha256,omitempty"`
		File   string      `json:"file,omitempty"`
	}
	manifest := struct {
		Format  string          `json:"format"`
		Created string          `json:"created_utc"`
		Files   []manifestEntry `json:"files"`
	}{Format: "pccontroller-apt-mirror-backup/v1", Created: now.Format(time.RFC3339)}
	for index, snapshot := range snapshots {
		entry := manifestEntry{
			Path: snapshot.Path, Exists: snapshot.Exists,
			Mode: snapshot.Mode, SHA256: snapshot.SHA256,
		}
		if snapshot.Exists {
			entry.File = fmt.Sprintf("%03d-%s", index, filepath.Base(snapshot.Path))
			if err := atomicWrite(filepath.Join(directory, entry.File), snapshot.Content, 0o600); err != nil {
				return "", err
			}
		}
		manifest.Files = append(manifest.Files, entry)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(directory, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	return directory, nil
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var restored []error
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		if !snapshot.Exists {
			if err := os.Remove(snapshot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				restored = append(restored, err)
			}
			continue
		}
		if err := atomicWrite(snapshot.Path, snapshot.Content, snapshot.Mode); err != nil {
			restored = append(restored, err)
		}
	}
	return errors.Join(restored...)
}

func uniqueSortedMapKeys(values map[string][]byte) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueSortedStrings(values []string) []string {
	sort.Strings(values)
	return uniqueStrings(values)
}
