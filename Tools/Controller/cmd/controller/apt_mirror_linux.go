//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pccontroller.local/controller/internal/aptmirror"
)

var (
	linuxAPTMirrorReadFile       = os.ReadFile
	linuxAPTMirrorInstall        = aptmirror.Install
	linuxAPTMirrorRefresh        = aptmirror.Refresh
	linuxAPTMirrorLoadConfig     = aptmirror.LoadConfig
	linuxAPTMirrorLoadCandidates = aptmirror.LoadCandidateOverrides
	linuxAPTMirrorArchitectures  = linuxDebianArchitectures
	linuxAPTMirrorAdoptionLock   = aptmirror.AcquireAdoptionLock
	linuxAPTMirrorPackageLocks   = aptmirror.AcquirePackageManagerLocks
)

func provisionLinuxUbuntuMirrors(
	ctx context.Context,
	options linuxHostProvisionOptions,
	executable string,
	output io.Writer,
	report *linuxHostProvisionReport,
) (aptmirror.InstallReport, error) {
	codename, err := linuxUbuntuCodename()
	if err != nil {
		return aptmirror.InstallReport{}, err
	}
	architectures, err := linuxAPTMirrorArchitectures()
	if err != nil {
		return aptmirror.InstallReport{}, err
	}
	config := aptmirror.DomesticFirstConfig(codename, architectures...)
	if options.MirrorCandidatesPath != "" {
		candidates, loadErr := linuxAPTMirrorLoadCandidates(options.MirrorCandidatesPath)
		if loadErr != nil {
			return aptmirror.InstallReport{}, loadErr
		}
		config.Candidates = candidates
		if validateErr := config.Validate(); validateErr != nil {
			return aptmirror.InstallReport{}, validateErr
		}
	}
	step := linuxHostProvisionStep{
		Name:     "install signed domestic-first Ubuntu mirror topology",
		Mutating: true,
		Detail:   "single mirror+file source; signed suite/index health; atomic last-good state; official fallback retained",
	}
	if !options.Apply {
		step.Planned = true
	}
	environment := linuxProvisionEnvironment(options.Environment)
	var systemd mirrorSystemdState
	var releaseAdoptionLock func()
	var releasePackageLocks func()
	if options.Apply {
		releaseAdoptionLock, err = linuxAPTMirrorAdoptionLock()
		if err != nil {
			return aptmirror.InstallReport{}, fmt.Errorf("serialize Ubuntu mirror adoption: %w", err)
		}
		defer releaseAdoptionLock()
		systemd, releasePackageLocks, err = prepareMirrorAdoption(ctx, environment)
		if err != nil {
			return aptmirror.InstallReport{}, err
		}
		defer releasePackageLocks()
	}
	fmt.Fprintln(output, "\n▶", step.Name)
	mirrorReport, err := linuxAPTMirrorInstall(ctx, aptmirror.InstallOptions{
		Config: config, Apply: options.Apply, ExecutableSource: executable,
		ProxyEnvironment: environment, Output: output,
	})
	if err != nil {
		provisionErr := fmt.Errorf("Ubuntu domestic-first mirror profile: %w", err)
		if options.Apply {
			if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, systemd); restoreErr != nil {
				provisionErr = errors.Join(provisionErr, restoreErr)
			}
		}
		report.Steps = append(report.Steps, step)
		return mirrorReport, provisionErr
	}
	step.Succeeded = !options.Apply || mirrorReport.Applied
	report.Steps = append(report.Steps, step)
	if !options.Apply {
		return mirrorReport, nil
	}
	commandStep := linuxHostProvisionStep{Name: "enable signed APT mirror health timer", Mutating: true}
	if err := activateMirrorSystemd(ctx, environment, output, systemd, &mirrorReport); err != nil {
		report.Steps = append(report.Steps, commandStep)
		return mirrorReport, err
	}
	commandStep.Succeeded = true
	report.Steps = append(report.Steps, commandStep)
	return mirrorReport, nil
}

func runToolchainMirrorInstall(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain mirror-install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apply := flags.Bool("apply", false, "install the stable Controller runtime, adopt Ubuntu sources, and enable the health timer")
	dryRun := flags.Bool("dry-run", false, "probe and inventory without changing the host; this is the default")
	candidatesPath := flags.String("mirror-candidates", "", "reviewed JSON array overriding mirror candidates")
	jsonOutput := flags.Bool("json", false, "emit a secret-free JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*apply && *dryRun) {
		return errors.New("usage: controller toolchain mirror-install [--apply|--dry-run] [--mirror-candidates FILE] [--json]")
	}
	if *apply && linuxHostProvisionCurrentEUID() != 0 {
		return errors.New("APT mirror install --apply requires root")
	}
	codename, err := linuxUbuntuCodename()
	if err != nil {
		return err
	}
	architectures, err := linuxAPTMirrorArchitectures()
	if err != nil {
		return err
	}
	config := aptmirror.DomesticFirstConfig(codename, architectures...)
	if strings.TrimSpace(*candidatesPath) != "" {
		candidates, loadErr := linuxAPTMirrorLoadCandidates(strings.TrimSpace(*candidatesPath))
		if loadErr != nil {
			return loadErr
		}
		config.Candidates = candidates
		if err := config.Validate(); err != nil {
			return err
		}
	}
	executable, err := linuxHostProvisionExecutable()
	if err != nil {
		return fmt.Errorf("locate current Controller executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	environment := linuxProvisionEnvironment(os.Environ())
	ctx, cancel := signalContext()
	defer cancel()
	var systemd mirrorSystemdState
	var releaseAdoptionLock func()
	var releasePackageLocks func()
	if *apply {
		releaseAdoptionLock, err = linuxAPTMirrorAdoptionLock()
		if err != nil {
			return fmt.Errorf("serialize Ubuntu mirror adoption: %w", err)
		}
		defer releaseAdoptionLock()
		systemd, releasePackageLocks, err = prepareMirrorAdoption(ctx, environment)
		if err != nil {
			return err
		}
		defer releasePackageLocks()
	}
	installOutput := stdout
	if *jsonOutput {
		installOutput = io.Discard
	}
	report, installErr := linuxAPTMirrorInstall(ctx, aptmirror.InstallOptions{
		Config: config, Apply: *apply, ExecutableSource: executable,
		ProxyEnvironment: environment, Output: installOutput,
	})
	if installErr == nil && *apply {
		installErr = activateMirrorSystemd(ctx, environment, installOutput, systemd, &report)
	} else if installErr != nil && *apply {
		if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, systemd); restoreErr != nil {
			installErr = errors.Join(installErr, restoreErr)
		}
	}
	if *jsonOutput {
		encoded, encodeErr := json.MarshalIndent(report, "", "  ")
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Fprintln(stdout, string(encoded))
	}
	return installErr
}

type mirrorSystemdState struct {
	Path                    string
	Enabled                 bool
	Active                  bool
	ServiceLoaded           bool
	ServiceWasRunning       bool
	LegacyEnabled           bool
	LegacyActive            bool
	LegacyServiceLoaded     bool
	LegacyServiceWasRunning bool
	APTDailyTimerActive     bool
	APTUpgradeTimerActive   bool
}

func inspectMirrorSystemd(ctx context.Context, environment []string) (mirrorSystemdState, error) {
	path, err := linuxHostProvisionLookPath("systemctl")
	if err != nil {
		return mirrorSystemdState{}, errors.New("installing the APT mirror timer requires trusted systemd systemctl; no files were changed")
	}
	state := mirrorSystemdState{Path: path}
	state.Enabled = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-enabled", "--quiet", "pccontroller-apt-mirror-health.timer"}}, environment, io.Discard) == nil
	state.Active = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-active", "--quiet", "pccontroller-apt-mirror-health.timer"}}, environment, io.Discard) == nil
	state.LegacyEnabled = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-enabled", "--quiet", "apt-mirror-health.timer"}}, environment, io.Discard) == nil
	state.LegacyActive = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-active", "--quiet", "apt-mirror-health.timer"}}, environment, io.Discard) == nil
	state.APTDailyTimerActive = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-active", "--quiet", "apt-daily.timer"}}, environment, io.Discard) == nil
	state.APTUpgradeTimerActive = linuxHostProvisionRun(ctx, linuxHostProvisionCommand{Name: path, Args: []string{"is-active", "--quiet", "apt-daily-upgrade.timer"}}, environment, io.Discard) == nil
	state.ServiceLoaded, state.ServiceWasRunning, err = inspectMirrorServiceState(ctx, environment, path, "pccontroller-apt-mirror-health.service")
	if err != nil {
		return mirrorSystemdState{}, err
	}
	state.LegacyServiceLoaded, state.LegacyServiceWasRunning, err = inspectMirrorServiceState(ctx, environment, path, "apt-mirror-health.service")
	if err != nil {
		return mirrorSystemdState{}, err
	}
	return state, nil
}

func inspectMirrorServiceState(ctx context.Context, environment []string, systemctl, unit string) (bool, bool, error) {
	var output strings.Builder
	command := linuxHostProvisionCommand{Name: systemctl, Args: []string{
		"show", "--property=LoadState", "--property=ActiveState", "--value", unit,
	}}
	if err := linuxHostProvisionRun(ctx, command, environment, &output); err != nil {
		return false, false, fmt.Errorf("inspect APT mirror service %s: %w", unit, err)
	}
	values := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(values) != 2 {
		return false, false, fmt.Errorf("inspect APT mirror service %s: unexpected systemd state", unit)
	}
	loadState := strings.TrimSpace(values[0])
	activeState := strings.TrimSpace(values[1])
	loaded := loadState != "" && loadState != "not-found"
	running := activeState != "" && activeState != "inactive" && activeState != "failed"
	return loaded, running, nil
}

func prepareMirrorAdoption(ctx context.Context, environment []string) (mirrorSystemdState, func(), error) {
	state, err := inspectMirrorSystemd(ctx, environment)
	if err != nil {
		return mirrorSystemdState{}, nil, err
	}
	if err := quiesceMirrorSystemd(ctx, environment, state); err != nil {
		return mirrorSystemdState{}, nil, err
	}
	barrierFailure := func(barrierErr error) (mirrorSystemdState, func(), error) {
		wrapped := fmt.Errorf("APT/dpkg quiescence barrier: %w; no APT source files were changed", barrierErr)
		if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, state); restoreErr != nil {
			wrapped = errors.Join(wrapped, restoreErr)
		}
		return mirrorSystemdState{}, nil, wrapped
	}
	if err := ensurePackageManagerServicesIdle(ctx, environment, state.Path); err != nil {
		return barrierFailure(err)
	}
	release, err := linuxAPTMirrorPackageLocks()
	if err != nil {
		return barrierFailure(err)
	}
	// Close the inspect/acquire race. Once this second check succeeds, the
	// retained record locks prevent a new package transaction until adoption,
	// daemon-reload and timer restoration are complete.
	if err := ensurePackageManagerServicesIdle(ctx, environment, state.Path); err != nil {
		release()
		return barrierFailure(err)
	}
	return state, release, nil
}

func ensurePackageManagerServicesIdle(ctx context.Context, environment []string, systemctl string) error {
	var busy []string
	for _, unit := range []string{"apt-daily.service", "apt-daily-upgrade.service"} {
		_, running, err := inspectMirrorServiceState(ctx, environment, systemctl, unit)
		if err != nil {
			return err
		}
		if running {
			busy = append(busy, unit)
		}
	}
	if len(busy) != 0 {
		return fmt.Errorf("package service still active: %s", strings.Join(busy, ", "))
	}
	return nil
}

func quiesceMirrorSystemd(ctx context.Context, environment []string, state mirrorSystemdState) error {
	var commands [][]string
	if state.APTDailyTimerActive {
		commands = append(commands, []string{"stop", "apt-daily.timer"})
	}
	if state.APTUpgradeTimerActive {
		commands = append(commands, []string{"stop", "apt-daily-upgrade.timer"})
	}
	if state.Enabled || state.Active {
		commands = append(commands, []string{"disable", "--now", "pccontroller-apt-mirror-health.timer"})
	}
	if state.LegacyEnabled || state.LegacyActive {
		commands = append(commands, []string{"disable", "--now", "apt-mirror-health.timer"})
	}
	if state.ServiceLoaded {
		commands = append(commands, []string{"stop", "pccontroller-apt-mirror-health.service"})
	}
	if state.LegacyServiceLoaded {
		commands = append(commands, []string{"stop", "apt-mirror-health.service"})
	}
	for _, args := range commands {
		command := linuxHostProvisionCommand{Name: state.Path, Args: args}
		if err := linuxHostProvisionRun(ctx, command, environment, io.Discard); err != nil {
			quiesceErr := fmt.Errorf("quiesce APT mirror unit %s before Go-owned adoption: %w", args[len(args)-1], err)
			if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, state); restoreErr != nil {
				return errors.Join(quiesceErr, restoreErr)
			}
			return quiesceErr
		}
	}
	return nil
}

func activateMirrorSystemd(ctx context.Context, environment []string, output io.Writer, prior mirrorSystemdState, report *aptmirror.InstallReport) error {
	var activationErr error
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "pccontroller-apt-mirror-health.timer"}} {
		command := linuxHostProvisionCommand{Name: prior.Path, Args: args}
		fmt.Fprintln(output, formatLinuxProvisionCommand(command))
		if err := linuxHostProvisionRun(ctx, command, environment, output); err != nil {
			activationErr = fmt.Errorf("activate APT mirror timer: %w", err)
			break
		}
	}
	if activationErr == nil {
		if err := verifyMirrorTimerScheduled(ctx, environment, prior.Path); err != nil {
			activationErr = fmt.Errorf("verify APT mirror timer schedule: %w", err)
		}
	}
	if activationErr == nil {
		if err := restorePackageManagerTimerState(ctx, environment, output, prior); err != nil {
			activationErr = fmt.Errorf("restore APT package timers after source adoption: %w", err)
		}
	}
	if activationErr != nil {
		var rollbackErrors []error
		if rollbackErr := report.Rollback(); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, rollbackErr)
		}
		if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, prior); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, restoreErr)
		}
		return errors.Join(append([]error{activationErr}, rollbackErrors...)...)
	}
	report.Commit()
	return nil
}

func verifyMirrorTimerScheduled(ctx context.Context, environment []string, systemctl string) error {
	command := linuxHostProvisionCommand{Name: systemctl, Args: []string{
		"show", "--property=ActiveState", "--property=NextElapseUSecMonotonic",
		"pccontroller-apt-mirror-health.timer",
	}}
	var output strings.Builder
	if err := linuxHostProvisionRun(ctx, command, environment, &output); err != nil {
		return err
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(output.String(), "\n") {
		name, value, found := strings.Cut(line, "=")
		if found {
			properties[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	next := properties["NextElapseUSecMonotonic"]
	if properties["ActiveState"] != "active" || next == "" || next == "0" || strings.EqualFold(next, "infinity") {
		return fmt.Errorf("timer is not active with a finite next monotonic activation (ActiveState=%q, NextElapseUSecMonotonic=%q)",
			properties["ActiveState"], next)
	}
	return nil
}

func restorePackageManagerTimerState(ctx context.Context, environment []string, output io.Writer, prior mirrorSystemdState) error {
	var failures []error
	for _, timer := range []struct {
		unit   string
		active bool
	}{
		{"apt-daily.timer", prior.APTDailyTimerActive},
		{"apt-daily-upgrade.timer", prior.APTUpgradeTimerActive},
	} {
		if !timer.active {
			continue
		}
		command := linuxHostProvisionCommand{Name: prior.Path, Args: []string{"start", timer.unit}}
		fmt.Fprintln(output, formatLinuxProvisionCommand(command))
		if err := linuxHostProvisionRun(ctx, command, environment, output); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func restoreMirrorTimerState(ctx context.Context, environment []string, prior mirrorSystemdState) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var failures []error
	var commands [][]string
	currentEnabled := linuxHostProvisionRun(cleanupContext, linuxHostProvisionCommand{Name: prior.Path, Args: []string{"is-enabled", "--quiet", "pccontroller-apt-mirror-health.timer"}}, environment, io.Discard) == nil
	currentActive := linuxHostProvisionRun(cleanupContext, linuxHostProvisionCommand{Name: prior.Path, Args: []string{"is-active", "--quiet", "pccontroller-apt-mirror-health.timer"}}, environment, io.Discard) == nil
	currentServiceLoaded, _, inspectErr := inspectMirrorServiceState(cleanupContext, environment, prior.Path, "pccontroller-apt-mirror-health.service")
	if inspectErr != nil {
		failures = append(failures, inspectErr)
	}
	if currentEnabled || currentActive {
		commands = append(commands, []string{"disable", "--now", "pccontroller-apt-mirror-health.timer"})
	}
	if currentServiceLoaded {
		commands = append(commands, []string{"stop", "pccontroller-apt-mirror-health.service"})
	}
	commands = append(commands, []string{"daemon-reload"})
	if prior.Enabled {
		commands = append(commands, []string{"enable", "pccontroller-apt-mirror-health.timer"})
	}
	if prior.Active {
		commands = append(commands, []string{"start", "pccontroller-apt-mirror-health.timer"})
	}
	if prior.ServiceWasRunning {
		commands = append(commands, []string{"start", "--no-block", "pccontroller-apt-mirror-health.service"})
	}
	if prior.LegacyEnabled {
		commands = append(commands, []string{"enable", "apt-mirror-health.timer"})
	}
	if prior.LegacyActive {
		commands = append(commands, []string{"start", "apt-mirror-health.timer"})
	}
	if prior.LegacyServiceWasRunning {
		commands = append(commands, []string{"start", "--no-block", "apt-mirror-health.service"})
	}
	if prior.APTDailyTimerActive {
		commands = append(commands, []string{"start", "apt-daily.timer"})
	}
	if prior.APTUpgradeTimerActive {
		commands = append(commands, []string{"start", "apt-daily-upgrade.timer"})
	}
	for _, args := range commands {
		if err := linuxHostProvisionRun(cleanupContext, linuxHostProvisionCommand{Name: prior.Path, Args: args}, environment, io.Discard); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func runToolchainMirrorRefresh(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain mirror-refresh", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", aptmirror.DefaultPaths().InstalledConfig, "root-owned PCController APT mirror config")
	apply := flags.Bool("apply", false, "atomically install the refreshed mirror list and last-good state")
	dryRun := flags.Bool("dry-run", false, "explicit read-only alias; read-only is the default")
	jsonOutput := flags.Bool("json", false, "emit the refresh report as JSON without proxy values")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*apply && *dryRun) {
		return errors.New("usage: controller toolchain mirror-refresh [--config FILE] [--apply|--dry-run] [--json]")
	}
	if *apply {
		if linuxHostProvisionCurrentEUID() != 0 {
			return errors.New("APT mirror refresh --apply requires root")
		}
		identity, err := linuxHostProvisionPathIdentity(*configPath)
		if err != nil {
			return fmt.Errorf("inspect APT mirror config: %w", err)
		}
		if !identity.Exists || identity.Symlink || identity.IsDir || identity.UID != 0 {
			return errors.New("APT mirror refresh --apply requires a root-owned regular config file, not a symlink")
		}
	}
	config, err := linuxAPTMirrorLoadConfig(*configPath)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	refreshOutput := stdout
	if *jsonOutput {
		refreshOutput = io.Discard
	}
	report, err := linuxAPTMirrorRefresh(ctx, aptmirror.RefreshOptions{
		Config: config, Apply: *apply, Output: refreshOutput,
	})
	if *jsonOutput {
		encoded, encodeErr := json.MarshalIndent(report, "", "  ")
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Fprintln(stdout, string(encoded))
	}
	return err
}

func linuxUbuntuCodename() (string, error) {
	content, err := linuxAPTMirrorReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("read Linux release identity: %w", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), "\"")
	}
	id := strings.ToLower(values["ID"])
	idLike := strings.Fields(strings.ToLower(values["ID_LIKE"]))
	if id != "ubuntu" && !slices.Contains(idLike, "ubuntu") {
		return "", fmt.Errorf("domestic-first APT profile supports Ubuntu only, got ID=%q", id)
	}
	codename := values["UBUNTU_CODENAME"]
	if codename == "" {
		codename = values["VERSION_CODENAME"]
	}
	if strings.TrimSpace(codename) == "" {
		return "", errors.New("Ubuntu release identity does not declare a codename")
	}
	return strings.TrimSpace(codename), nil
}

func linuxDebianArchitectures() ([]string, error) {
	dpkg, err := linuxHostProvisionLookPath("dpkg")
	if err != nil {
		return nil, errors.New("APT mirror provisioning requires trusted dpkg architecture discovery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	environment := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	var result []string
	seen := make(map[string]bool)
	for index, argument := range []string{"--print-architecture", "--print-foreign-architectures"} {
		var output strings.Builder
		command := linuxHostProvisionCommand{Name: dpkg, Args: []string{argument}}
		if err := linuxHostProvisionRun(ctx, command, environment, &output); err != nil {
			return nil, fmt.Errorf("discover Debian architectures with dpkg: %w", err)
		}
		values := strings.Fields(output.String())
		if index == 0 && len(values) != 1 {
			return nil, errors.New("dpkg did not report exactly one native Debian architecture")
		}
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("dpkg did not report a Debian architecture")
	}
	return result, nil
}
