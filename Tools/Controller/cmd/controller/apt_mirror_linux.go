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
	"runtime"
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
	architecture, err := linuxDebianArchitecture()
	if err != nil {
		return aptmirror.InstallReport{}, err
	}
	config := aptmirror.DomesticFirstConfig(codename, architecture)
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
	if options.Apply {
		systemd, err = inspectMirrorSystemd(ctx, environment)
		if err != nil {
			return aptmirror.InstallReport{}, err
		}
		if err := quiesceLegacyMirrorTimer(ctx, environment, systemd); err != nil {
			return aptmirror.InstallReport{}, err
		}
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
	architecture, err := linuxDebianArchitecture()
	if err != nil {
		return err
	}
	config := aptmirror.DomesticFirstConfig(codename, architecture)
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
	if *apply {
		systemd, err = inspectMirrorSystemd(ctx, environment)
		if err != nil {
			return err
		}
		if err := quiesceLegacyMirrorTimer(ctx, environment, systemd); err != nil {
			return err
		}
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
	Path          string
	Enabled       bool
	Active        bool
	LegacyEnabled bool
	LegacyActive  bool
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
	return state, nil
}

func quiesceLegacyMirrorTimer(ctx context.Context, environment []string, state mirrorSystemdState) error {
	if !state.LegacyEnabled && !state.LegacyActive {
		return nil
	}
	command := linuxHostProvisionCommand{Name: state.Path, Args: []string{"disable", "--now", "apt-mirror-health.timer"}}
	if err := linuxHostProvisionRun(ctx, command, environment, io.Discard); err != nil {
		return fmt.Errorf("quiesce legacy apt-mirror-health.timer before Go-owned adoption: %w", err)
	}
	return nil
}

func activateMirrorSystemd(ctx context.Context, environment []string, output io.Writer, prior mirrorSystemdState, report *aptmirror.InstallReport) error {
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "pccontroller-apt-mirror-health.timer"}} {
		command := linuxHostProvisionCommand{Name: prior.Path, Args: args}
		fmt.Fprintln(output, formatLinuxProvisionCommand(command))
		if err := linuxHostProvisionRun(ctx, command, environment, output); err != nil {
			var rollbackErrors []error
			if rollbackErr := report.Rollback(); rollbackErr != nil {
				rollbackErrors = append(rollbackErrors, rollbackErr)
			}
			if restoreErr := restoreMirrorTimerState(context.WithoutCancel(ctx), environment, prior); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, restoreErr)
			}
			return errors.Join(append([]error{fmt.Errorf("activate APT mirror timer: %w", err)}, rollbackErrors...)...)
		}
	}
	report.Commit()
	return nil
}

func restoreMirrorTimerState(ctx context.Context, environment []string, prior mirrorSystemdState) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var failures []error
	commands := [][]string{{"disable", "--now", "pccontroller-apt-mirror-health.timer"}, {"daemon-reload"}}
	if prior.Enabled {
		commands = append(commands, []string{"enable", "pccontroller-apt-mirror-health.timer"})
	}
	if prior.Active {
		commands = append(commands, []string{"start", "pccontroller-apt-mirror-health.timer"})
	}
	if prior.LegacyEnabled {
		commands = append(commands, []string{"enable", "apt-mirror-health.timer"})
	}
	if prior.LegacyActive {
		commands = append(commands, []string{"start", "apt-mirror-health.timer"})
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
	if id != "ubuntu" && !containsArgument(idLike, "ubuntu") {
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

func linuxDebianArchitecture() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64", "riscv64", "s390x":
		return runtime.GOARCH, nil
	case "386":
		return "i386", nil
	case "ppc64le":
		return "ppc64el", nil
	default:
		return "", fmt.Errorf("no reviewed Debian architecture mapping for Controller GOARCH=%s", runtime.GOARCH)
	}
}
