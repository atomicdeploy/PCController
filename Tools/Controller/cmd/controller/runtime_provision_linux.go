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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/runtimeinstall"
)

const runtimeInstallUsage = "usage: controller toolchain runtime-install --target-user USER [--package DIR] [--virtual-board FILE] [--browser FILE] [--apply|--dry-run] [--json]"

func runToolchainRuntime(action string, args []string, stdout, stderr io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "runtime-install":
		return runToolchainRuntimeInstall(args, stdout, stderr)
	case "runtime-stage":
		return runToolchainRuntimeStage(args, stdout, stderr)
	case "runtime-status":
		return runToolchainRuntimeOperation("status", args, stdout, stderr)
	case "runtime-rollback":
		return runToolchainRuntimeOperation("rollback", args, stdout, stderr)
	case "runtime-uninstall":
		return runToolchainRuntimeOperation("uninstall", args, stdout, stderr)
	case "runtime-user-links":
		return runToolchainRuntimeUserLinks(args, stdout, stderr)
	default:
		return errors.New("usage: controller toolchain runtime-stage|runtime-install|runtime-status|runtime-rollback|runtime-uninstall [flags]")
	}
}

func runToolchainRuntimeStage(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain runtime-stage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	packageDirectory := flags.String("package", "", "canonical Tools/Controller/bin source package")
	virtualBoard := flags.String("virtual-board", "", "canonical VirtualBoard release output")
	apply := flags.Bool("apply", false, "publish the pinned bytes into the trusted root-owned input stage")
	dryRun := flags.Bool("dry-run", false, "validate and report the stage plan without changing the host")
	jsonOutput := flags.Bool("json", false, "emit the machine-readable report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*packageDirectory) == "" || strings.TrimSpace(*virtualBoard) == "" || (*apply && *dryRun) {
		return errors.New("usage: controller toolchain runtime-stage --package DIR --virtual-board FILE [--apply|--dry-run] [--json]")
	}
	if *apply && os.Geteuid() != 0 {
		return errors.New("runtime-stage --apply requires root; no state was changed")
	}
	ctx, cancel := signalContext()
	defer cancel()
	report, stageErr := runtimeinstall.Stage(ctx, runtimeinstall.StageOptions{
		SourcePackage: *packageDirectory, SourceVirtualBoard: *virtualBoard, Apply: *apply,
	})
	if *jsonOutput {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
	} else {
		fmt.Fprintln(stdout, "PCController trusted runtime input stage:", report.StageID)
		fmt.Fprintln(stdout, "package:", report.PackageDirectory)
		fmt.Fprintln(stdout, "virtual-board:", report.VirtualBoard)
	}
	return stageErr
}

func runToolchainRuntimeUserLinks(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain runtime-user-links", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", runtimeinstall.DefaultRoot, "managed runtime root")
	ensure := flags.Bool("ensure", false, "ensure exact target-user unit links")
	remove := flags.Bool("remove", false, "remove exact target-user unit links")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *ensure == *remove {
		return errors.New("usage: controller toolchain runtime-user-links --root DIR (--ensure|--remove)")
	}
	operation := "ensure"
	if *remove {
		operation = "remove"
	}
	created, err := runtimeinstall.ManageCurrentUserLinks(*root, operation)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(map[string]any{"operation": operation, "created": created})
	fmt.Fprintln(stdout, string(encoded))
	return nil
}

func runToolchainRuntimeWindowReady(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("toolchain runtime-window-ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	deadline := flags.Duration("timeout", 45*time.Second, "maximum authenticated readiness wait")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *deadline <= 0 || *deadline > 2*time.Minute {
		return errors.New("usage: controller toolchain runtime-window-ready [--timeout 45s]")
	}
	configurePrimaryIPC(runtimeWindowReadinessConfig(store))
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		var snapshot control.Snapshot
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
		err := callPrimary(callCtx, "controller.snapshot", map[string]any{}, &snapshot)
		callCancel()
		switch {
		case err != nil:
			last = boundedRuntimeText(err.Error())
		case !snapshot.Connected:
			last = "Controller snapshot is not connected"
		case !strings.EqualFold(strings.TrimSpace(snapshot.Port.Name), "tcp://127.0.0.1:8765"):
			last = "Controller snapshot is connected to an unexpected endpoint"
		case snapshot.Hello.Name != "PCController" || snapshot.Hello.BoardKind == 0:
			last = "VirtualBoard HELLO identity is incomplete"
		default:
			fmt.Fprintln(stdout, "PCController runtime is authenticated, connected, and identity-ready")
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("runtime readiness deadline exceeded: %s", last)
		case <-ticker.C:
		}
	}
}

func runtimeWindowReadinessConfig(store *appconfig.Store) appconfig.Config {
	runtimeConfig := store.CurrentRuntime()
	runtimeConfig.IPC.Listen = "127.0.0.1:8787"
	return runtimeConfig
}

func boundedRuntimeText(value string) string {
	value = strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
	if len(value) > 256 {
		value = value[:256] + "..."
	}
	return value
}

func runToolchainRuntimeInstall(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain runtime-install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	targetUser := flags.String("target-user", "", "existing non-root account that owns the graphical session and PCController data")
	packageDirectory := flags.String("package", "", "canonical host package directory containing controller and host-manifest.json")
	virtualBoard := flags.String("virtual-board", "", "canonical native VirtualBoard executable")
	browser := flags.String("browser", "", "root-owned Google Chrome or Chromium executable")
	apply := flags.Bool("apply", false, "publish and activate the validated runtime; without this flag the command is read-only")
	dryRun := flags.Bool("dry-run", false, "validate every input and print the publication plan without changing the host")
	jsonOutput := flags.Bool("json", false, "emit the machine-readable report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*targetUser) == "" {
		return errors.New(runtimeInstallUsage)
	}
	if *apply && *dryRun {
		return errors.New("--apply and --dry-run are mutually exclusive")
	}
	account, err := linuxHostProvisionLookupUser(strings.TrimSpace(*targetUser))
	if err != nil {
		return fmt.Errorf("resolve runtime target account: %w", err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return errors.New("runtime target must be an existing non-root Linux account")
	}
	resolvedPackage, err := resolveRuntimeHostPackage(*packageDirectory)
	if err != nil {
		return err
	}
	resolvedBoard, err := resolveRuntimeVirtualBoard(*virtualBoard, resolvedPackage)
	if err != nil {
		return err
	}
	resolvedBrowser, err := resolveRuntimeBrowser(*browser)
	if err != nil {
		return err
	}
	runuser, err := trustedLinuxHostProvisionExecutable("runuser")
	if err != nil {
		return err
	}
	systemctl, err := trustedLinuxHostProvisionExecutable("systemctl")
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	stagedID := ""
	if *apply && os.Geteuid() == 0 {
		trusted, trustErr := runtimeinstall.ValidatePackage(ctx, resolvedPackage, resolvedBoard)
		if trustErr == nil {
			_ = trusted.Close()
		} else {
			staged, stageErr := runtimeinstall.Stage(ctx, runtimeinstall.StageOptions{
				SourcePackage: resolvedPackage, SourceVirtualBoard: resolvedBoard, Apply: true,
			})
			if stageErr != nil {
				return fmt.Errorf("stage untrusted canonical runtime inputs: %w", stageErr)
			}
			resolvedPackage = staged.PackageDirectory
			resolvedBoard = staged.VirtualBoard
			stagedID = staged.StageID
		}
	}
	report, operationErr := runtimeinstall.Install(ctx, runtimeinstall.InstallOptions{
		TargetUser: account.Username, TargetUID: uint32(uid), TargetHome: account.HomeDir,
		HostPackage: resolvedPackage, VirtualBoard: resolvedBoard, Browser: resolvedBrowser,
		Runuser: runuser, Systemctl: systemctl, Apply: *apply,
	})
	if stagedID != "" {
		report.Actions = append([]string{"published trusted runtime input stage " + stagedID}, report.Actions...)
	}
	printRuntimeReport(stdout, report, *jsonOutput)
	return operationErr
}

func runToolchainRuntimeOperation(operation string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain runtime-"+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	apply := flags.Bool("apply", false, "apply the operation; without this flag the command is read-only")
	dryRun := flags.Bool("dry-run", false, "print and validate the operation without changing the host")
	jsonOutput := flags.Bool("json", false, "emit the machine-readable report")
	if operation == "status" {
		// Status is always read-only; accepting mutation flags would make scripts
		// appear to request a state transition that can never occur.
		if err := flags.Parse(args); err != nil {
			return err
		}
		if flags.NArg() != 0 || *apply || *dryRun {
			return errors.New("usage: controller toolchain runtime-status [--json]")
		}
	} else {
		if err := flags.Parse(args); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("usage: controller toolchain runtime-%s [--apply|--dry-run] [--json]", operation)
		}
		if *apply && *dryRun {
			return errors.New("--apply and --dry-run are mutually exclusive")
		}
	}
	ctx, cancel := signalContext()
	defer cancel()
	options := runtimeinstall.OperationOptions{Apply: *apply}
	var report runtimeinstall.Report
	var err error
	switch operation {
	case "status":
		report, err = runtimeinstall.Status(ctx, options)
	case "rollback":
		if *apply {
			options.Runuser, err = trustedLinuxHostProvisionExecutable("runuser")
			if err != nil {
				return err
			}
			options.Systemctl, err = trustedLinuxHostProvisionExecutable("systemctl")
			if err != nil {
				return err
			}
		}
		report, err = runtimeinstall.Rollback(ctx, options)
	case "uninstall":
		if *apply {
			options.Runuser, err = trustedLinuxHostProvisionExecutable("runuser")
			if err != nil {
				return err
			}
			options.Systemctl, err = trustedLinuxHostProvisionExecutable("systemctl")
			if err != nil {
				return err
			}
		}
		report, err = runtimeinstall.Uninstall(ctx, options)
	default:
		return fmt.Errorf("unsupported runtime operation %q", operation)
	}
	printRuntimeReport(stdout, report, *jsonOutput)
	return err
}

func resolveRuntimeHostPackage(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		path, err := filepath.Abs(strings.TrimSpace(explicit))
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current Controller executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve current Controller executable identity: %w", err)
	}
	directory := filepath.Dir(resolved)
	if _, err := os.Lstat(filepath.Join(directory, "host-manifest.json")); err != nil {
		return "", errors.New("canonical host package was not found beside this executable; pass --package DIR")
	}
	return directory, nil
}

func resolveRuntimeVirtualBoard(explicit, packageDirectory string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		path, err := filepath.Abs(strings.TrimSpace(explicit))
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	candidates := []string{filepath.Join(packageDirectory, "virtual-board")}
	if root := findProjectRoot(); root != "." {
		candidates = append(candidates, filepath.Join(root, "Tools", "VirtualBoard", ".build", "release", "bin", "virtual_board"))
	}
	for _, candidate := range candidates {
		if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", errors.New("canonical VirtualBoard executable was not found; pass --virtual-board FILE")
}

func resolveRuntimeBrowser(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		path, err := filepath.Abs(strings.TrimSpace(explicit))
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("Google Chrome or Chromium was not found; install one or pass --browser FILE")
}

func printRuntimeReport(output io.Writer, report runtimeinstall.Report, jsonOutput bool) {
	if jsonOutput {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(output, string(encoded))
		return
	}
	state := "not installed"
	if report.Installed {
		state = "installed"
	}
	mode := "dry-run"
	if report.Applied {
		mode = "applied"
	}
	fmt.Fprintf(output, "PCController Linux runtime %s: %s (%s)\n", report.Operation, state, mode)
	if report.ReleaseID != "" {
		fmt.Fprintln(output, "release:", report.ReleaseID)
	}
	if report.PreviousReleaseID != "" {
		fmt.Fprintln(output, "rollback release:", report.PreviousReleaseID)
	}
	if report.UserManager != "" {
		fmt.Fprintln(output, "graphical user manager:", report.UserManager)
	}
	for _, action := range report.Actions {
		fmt.Fprintln(output, "-", action)
	}
}
