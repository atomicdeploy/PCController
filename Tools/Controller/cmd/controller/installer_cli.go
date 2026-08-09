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
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/installer"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/programmer"
)

func runPackageLifecycle(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !strings.EqualFold(args[0], "inventory") {
		return errors.New("usage: controller package inventory --directory DIR [--output FILE]")
	}
	flags := flag.NewFlagSet("package inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "built host package directory")
	output := flags.String("output", "", "inventory output path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*directory) == "" {
		return errors.New("usage: controller package inventory --directory DIR [--output FILE]")
	}
	resolvedOutput := strings.TrimSpace(*output)
	if resolvedOutput == "" {
		resolvedOutput = filepath.Join(*directory, installer.PackageManifestName)
	}
	manifest, err := installer.GeneratePackageManifest(*directory, resolvedOutput, installer.ManifestOptions{})
	if err != nil {
		return err
	}
	return writeLifecycleJSON(stdout, manifest)
}

func runInstallLifecycle(
	action string,
	args []string,
	stdout, stderr io.Writer,
	displayName string,
) error {
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "per-user installation directory")
	packageRoot := flags.String("package", "", "verified extracted host package directory")
	expected := flags.String("expected-package-sha256", "", "expected installation inventory SHA-256")
	desktop := flags.Bool("desktop", false, "activate URI/AUMID/shortcut through the native adapter")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: controller %s [--package DIR] [--root DIR] [--expected-package-sha256 SHA256] [--desktop]", action)
	}
	service, err := newInstallerService(displayName)
	if err != nil {
		return err
	}
	resolvedPackage := strings.TrimSpace(*packageRoot)
	if resolvedPackage == "" {
		resolvedPackage, err = runningPackageDirectory()
		if err != nil {
			return err
		}
	}
	request := installer.ChangeRequest{
		Root: *root, PackageRoot: resolvedPackage,
		ExpectedPackageSHA256: *expected, ConfigureDesktop: *desktop,
	}
	ctx := context.Background()
	var result installer.LifecycleResult
	if action == "repair" {
		result, err = service.Repair(ctx, request)
	} else {
		result, err = service.Install(ctx, request)
	}
	if err != nil {
		return err
	}
	return writeLifecycleJSON(stdout, result)
}

func runInstallationStatus(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !strings.EqualFold(args[0], "status") {
		return errors.New("usage: controller installation status [--root DIR]")
	}
	flags := flag.NewFlagSet("installation status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "per-user installation directory")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller installation status [--root DIR]")
	}
	service, err := newInstallerService("")
	if err != nil {
		return err
	}
	result, err := service.Status(context.Background(), *root)
	if err != nil {
		return err
	}
	return writeLifecycleJSON(stdout, result)
}

func runUninstallLifecycle(
	args []string,
	stdout, stderr io.Writer,
	configPath, displayName string,
) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "per-user installation directory")
	purgeData := flags.Bool("purge-data", false, "also remove host configuration, backups, tools, logs, and state")
	confirmation := flags.String("confirm-purge", "", "exact separate data-purge confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller uninstall [--root DIR] [--purge-data --confirm-purge TOKEN]")
	}
	service, err := newInstallerService(displayName)
	if err != nil {
		return err
	}
	request := installer.UninstallRequest{
		Root: *root, PurgeData: *purgeData, PurgeConfirmation: *confirmation,
	}
	if *purgeData {
		request.PurgePaths, err = lifecyclePurgePaths(configPath)
		if err != nil {
			return err
		}
	}
	running, err := service.RunningFromRoot(request.Root)
	if err != nil {
		return err
	}
	if running {
		plan, err := service.PrepareExternalUninstall(context.Background(), request)
		if err != nil {
			return err
		}
		return writeLifecycleJSON(stdout, map[string]any{
			"action": "uninstall", "scheduled": true,
			"helper":         plan.HelperPath,
			"data_preserved": !request.PurgeData,
		})
	}
	result, err := service.Uninstall(context.Background(), request)
	if err != nil {
		return err
	}
	return writeLifecycleJSON(stdout, result)
}

type installerDesktopAdapter struct{}

func (installerDesktopAdapter) Ensure(
	_ context.Context,
	target installer.DesktopTarget,
) error {
	status, err := hostui.EnsureDesktopIntegration(hostui.DesktopIntegrationOptions{
		AppID: target.AppID, DisplayName: target.DisplayName,
		Executable: target.Executable,
	})
	if err != nil {
		return err
	}
	if !status.Supported || !status.ProtocolReady || !status.ShortcutReady {
		return errors.New("native desktop integration did not become ready")
	}
	return nil
}

func (installerDesktopAdapter) RemoveOwned(
	_ context.Context,
	target installer.DesktopTarget,
) error {
	status, err := hostui.RemoveDesktopIntegration(hostui.DesktopIntegrationOptions{
		AppID: target.AppID, DisplayName: target.DisplayName,
		Executable: target.Executable,
	})
	if err != nil {
		return err
	}
	if !status.Supported {
		return errors.New("native desktop integration is unavailable")
	}
	return nil
}

func newInstallerService(displayName string) (*installer.Service, error) {
	service, err := installer.NewService()
	if err != nil {
		return nil, err
	}
	service.DisplayName = productidentity.Title(displayName)
	service.Desktop = installerDesktopAdapter{}
	return service, nil
}

func runningPackageDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	return filepath.Dir(executable), nil
}

func lifecyclePurgePaths(configPath string) ([]string, error) {
	resolvedConfig, err := appconfig.ResolvePath(configPath)
	if err != nil {
		return nil, err
	}
	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return nil, err
	}
	values := []string{filepath.Dir(resolvedConfig), paths.DataDir}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(filepath.Clean(value))
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func writeLifecycleJSON(output io.Writer, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(content))
	return err
}
