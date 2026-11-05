//go:build windows

// Package selfinstall implements RayanLamp's explicit, per-user file
// installation lifecycle. Ordinary portable startup never calls this package.
package selfinstall

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/comserver"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/shellintegration"
	"rayanlamp.local/app/internal/winfx"
)

const (
	installDirectoryName = "RayanLamp"
	executableName       = "RayanLamp.exe"
	stagePrefix          = "RayanLamp.install."
	backupPrefix         = "RayanLamp.backup."
	helperPrefix         = "RayanLamp.cleanup."
	helperSuffix         = ".exe"

	defaultOperationTimeout = 30 * time.Second
	cleanupWaitTimeout      = 2 * time.Minute
	fileRetryTimeout        = 10 * time.Second
)

var (
	ErrAlreadyInstalled = errors.New(
		"RayanLamp is already installed; pass --force to update it",
	)
	ErrNotInstalled = errors.New("RayanLamp is not installed for this user")
	ErrUnsafePath   = errors.New("selfinstall: refusing an unexpected path")
)

// InstallOptions selects explicitly requested install-time integrations.
// All integrations default to false; installing the file alone writes no
// registry or shell state.
type InstallOptions struct {
	WithStartup bool
	WithShell   bool
	WithCOM     bool
	Launch      bool
	Force       bool
}

// InstallResult describes the completed file operation. Warning is non-nil
// only for a useful partial result, such as a policy-blocked Jump List or a
// requested post-install launch that Windows rejected.
type InstallResult struct {
	Executable string
	Installed  bool
	Updated    bool
	Warning    error
}

// UninstallOptions controls optional removal of per-user configuration and
// logs. Integrations and installed program files are always cleaned when they
// are still owned by the exact installed executable.
type UninstallOptions struct {
	RemoveUserData bool
}

// UninstallResult reports that asynchronous cleanup has been safely scheduled.
// Warning reports foreign integration ownership that was deliberately
// preserved.
type UninstallResult struct {
	Executable string
	Warning    error
}

// CleanupOptions is accepted only by the private detached cleanup-helper mode.
type CleanupOptions struct {
	ParentPID      uint32
	InstallPath    string
	RemoveUserData bool
}

// Status is a read-only view of the fixed per-user installation location.
type Status struct {
	InstallDirectory    string `json:"install_directory"`
	InstalledExecutable string `json:"installed_executable"`
	Installed           bool   `json:"installed"`
	Incomplete          bool   `json:"incomplete"`
	CurrentExecutable   string `json:"current_executable"`
	CurrentIsInstalled  bool   `json:"current_is_installed"`
}

type layout struct {
	localAppData string
	programs     string
	directory    string
	executable   string
	userData     string
}

type dependencies struct {
	localAppData      func() (string, error)
	roamingAppData    func() (string, error)
	tempDirectory     func() (string, error)
	currentExecutable func() (string, error)
	stopPrimary       func(context.Context) error
	primaryRunning    func(context.Context) bool
	startupOwned      func(string) (bool, error)
	setStartup        func(bool, string) error
	shellOwned        func(string) (bool, error)
	registerShell     func(string) error
	unregisterShell   func(string) error
	comOwned          func(string) (bool, error)
	comServerOwned    func(string) (bool, error)
	registerCOM       func(string) error
	unregisterCOM     func(string) error
	launch            func(string) error
	launchHelper      func(string, CleanupOptions) error
	processID         func() int
}

func systemDependencies() dependencies {
	return dependencies{
		localAppData: func() (string, error) {
			return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
		},
		roamingAppData: func() (string, error) {
			return windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
		},
		tempDirectory:     windowsTempDirectory,
		currentExecutable: os.Executable,
		stopPrimary:       stopPrimary,
		primaryRunning:    ipc.IsRunning,
		startupOwned:      winfx.IsStartWithWindowsFor,
		setStartup:        winfx.SetStartWithWindows,
		shellOwned:        shellintegration.IsRegistered,
		registerShell:     shellintegration.RegisterShell,
		unregisterShell:   shellintegration.UnregisterShell,
		comOwned:          comserver.HasOwnedRegistration,
		comServerOwned:    comserver.HasOwnedServerRegistration,
		registerCOM:       comserver.Register,
		unregisterCOM:     comserver.UnregisterExecutable,
		launch: func(path string) error {
			command := exec.Command(path)
			if err := command.Start(); err != nil {
				return err
			}
			return command.Process.Release()
		},
		launchHelper: launchCleanupHelper,
		processID:    os.Getpid,
	}
}

// InstallPath returns the authoritative per-user installation directory and
// executable path without changing the filesystem or registry.
func InstallPath() (directory, executable string, err error) {
	deps := systemDependencies()
	paths, err := resolveLayout(deps)
	if err != nil {
		return "", "", err
	}
	return paths.directory, paths.executable, nil
}

// CurrentStatus reports portable-versus-installed state without registration
// writes or other mutations.
func CurrentStatus() (Status, error) {
	return statusWithDependencies(systemDependencies())
}

// Install copies or updates the current executable at the fixed LocalAppData
// destination, then performs only the integrations explicitly requested.
func Install(ctx context.Context, options InstallOptions) (InstallResult, error) {
	return installWithDependencies(ctx, options, systemDependencies())
}

// Uninstall removes exact-owned integrations and schedules safe removal of the
// fixed installed directory. User configuration is preserved by default.
func Uninstall(
	ctx context.Context,
	options UninstallOptions,
) (UninstallResult, error) {
	return uninstallWithDependencies(ctx, options, systemDependencies())
}

// RunCleanupHelper executes the private, detached cleanup phase.
func RunCleanupHelper(
	ctx context.Context,
	options CleanupOptions,
) error {
	return runCleanupHelperWithDependencies(
		ctx,
		options,
		systemDependencies(),
	)
}

func statusWithDependencies(deps dependencies) (Status, error) {
	paths, err := resolveLayout(deps)
	if err != nil {
		return Status{}, err
	}
	current, err := resolvedExecutable(deps.currentExecutable)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		InstallDirectory:    paths.directory,
		InstalledExecutable: paths.executable,
		CurrentExecutable:   current,
		CurrentIsInstalled:  samePath(current, paths.executable),
	}
	directoryInfo, directoryErr := os.Lstat(paths.directory)
	switch {
	case errors.Is(directoryErr, os.ErrNotExist):
		return status, nil
	case directoryErr != nil:
		return Status{}, fmt.Errorf(
			"inspect installation directory: %w",
			directoryErr,
		)
	case !directoryInfo.IsDir() || isReparsePoint(directoryInfo):
		status.Incomplete = true
		return status, nil
	}
	if err := validateInstallDirectoryContents(paths); err != nil {
		status.Incomplete = true
		return status, nil
	}
	executableInfo, executableErr := os.Lstat(paths.executable)
	switch {
	case errors.Is(executableErr, os.ErrNotExist):
		status.Incomplete = true
	case executableErr != nil:
		return Status{}, fmt.Errorf(
			"inspect installed executable: %w",
			executableErr,
		)
	case executableInfo.Mode().IsRegular() && !isReparsePoint(executableInfo):
		status.Installed = true
	default:
		status.Incomplete = true
	}
	return status, nil
}

func installWithDependencies(
	ctx context.Context,
	options InstallOptions,
	deps dependencies,
) (result InstallResult, returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDependencies(deps); err != nil {
		return InstallResult{}, err
	}
	paths, err := resolveLayout(deps)
	if err != nil {
		return InstallResult{}, err
	}
	source, err := resolvedExecutable(deps.currentExecutable)
	if err != nil {
		return InstallResult{}, err
	}
	if err := validateRegularFile(source, "source executable"); err != nil {
		return InstallResult{}, err
	}
	result.Executable = paths.executable

	currentIsInstalled := samePath(source, paths.executable)
	existing, err := inspectExistingInstallation(paths)
	if err != nil {
		return InstallResult{}, err
	}
	if currentIsInstalled && !existing {
		return InstallResult{}, fmt.Errorf(
			"%w: the current executable has the install path but the installation is incomplete",
			ErrUnsafePath,
		)
	}
	if existing && !currentIsInstalled && !options.Force {
		return InstallResult{}, ErrAlreadyInstalled
	}
	if !currentIsInstalled && pathWithin(source, paths.directory) {
		return InstallResult{}, fmt.Errorf(
			"%w: source executable is inside the managed install directory",
			ErrUnsafePath,
		)
	}

	var (
		stageDirectory  string
		backupDirectory string
		swapped         bool
		hadExisting     = existing
	)
	if !currentIsInstalled {
		if err := os.MkdirAll(paths.programs, 0o700); err != nil {
			return InstallResult{}, fmt.Errorf(
				"create per-user Programs directory: %w",
				err,
			)
		}
		stageDirectory, err = os.MkdirTemp(paths.programs, stagePrefix)
		if err != nil {
			return InstallResult{}, fmt.Errorf(
				"create install staging directory: %w",
				err,
			)
		}
		defer func() {
			if stageDirectory != "" {
				_ = removeManagedSibling(
					stageDirectory,
					paths.programs,
					stagePrefix,
				)
			}
		}()
		stagedExecutable := filepath.Join(stageDirectory, executableName)
		if err := copyFileVerified(source, stagedExecutable); err != nil {
			return InstallResult{}, err
		}

		operationCtx, cancelOperation := operationContext(ctx)
		err = deps.stopPrimary(operationCtx)
		cancelOperation()
		if err != nil {
			return InstallResult{}, fmt.Errorf(
				"stop running RayanLamp before installation: %w",
				err,
			)
		}
		if hadExisting {
			// Recheck after shutdown and immediately before the destructive
			// boundary. A replaced/reparse-point directory is never moved.
			if _, err := inspectExistingInstallation(paths); err != nil {
				return InstallResult{}, err
			}
			backupDirectory, err = reserveSiblingPath(
				paths.programs,
				backupPrefix,
			)
			if err != nil {
				return InstallResult{}, err
			}
			if err := os.Rename(paths.directory, backupDirectory); err != nil {
				return InstallResult{}, fmt.Errorf(
					"move current installation to rollback directory: %w",
					err,
				)
			}
		}
		if err := os.Rename(stageDirectory, paths.directory); err != nil {
			if hadExisting {
				restoreErr := os.Rename(backupDirectory, paths.directory)
				return InstallResult{}, errors.Join(
					fmt.Errorf("activate staged installation: %w", err),
					restoreErr,
				)
			}
			return InstallResult{}, fmt.Errorf(
				"activate staged installation: %w",
				err,
			)
		}
		stageDirectory = ""
		swapped = true
		result.Installed = !hadExisting
		result.Updated = hadExisting
	} else {
		result.Installed = true
	}

	var startupCreated, shellCreated bool
	rollback := func(cause error) error {
		var cleanupErr error
		if shellCreated {
			cleanupErr = errors.Join(
				cleanupErr,
				deps.unregisterShell(paths.executable),
			)
		}
		if startupCreated {
			cleanupErr = errors.Join(
				cleanupErr,
				deps.setStartup(false, paths.executable),
			)
		}
		if swapped {
			cleanupErr = errors.Join(
				cleanupErr,
				removeExactInstallDirectory(paths),
			)
			if hadExisting {
				if validationErr := validateManagedInstallDirectory(
					backupDirectory,
					paths.programs,
					backupPrefix,
				); validationErr != nil {
					cleanupErr = errors.Join(cleanupErr, validationErr)
				} else {
					restoreErr := os.Rename(
						backupDirectory,
						paths.directory,
					)
					cleanupErr = errors.Join(cleanupErr, restoreErr)
					if restoreErr == nil {
						backupDirectory = ""
					}
				}
			}
		}
		return errors.Join(cause, cleanupErr)
	}

	if options.WithStartup {
		owned, probeErr := deps.startupOwned(paths.executable)
		if probeErr != nil {
			return InstallResult{}, rollback(fmt.Errorf(
				"inspect start-with-Windows registration: %w",
				probeErr,
			))
		}
		if err := deps.setStartup(true, paths.executable); err != nil {
			return InstallResult{}, rollback(err)
		}
		startupCreated = !owned
	}

	var warning error
	if options.WithShell {
		owned, probeErr := deps.shellOwned(paths.executable)
		if probeErr != nil {
			return InstallResult{}, rollback(fmt.Errorf(
				"inspect shell integration: %w",
				probeErr,
			))
		}
		registerErr := deps.registerShell(paths.executable)
		switch {
		case registerErr == nil:
			shellCreated = !owned
		case shellintegration.IsPartial(registerErr):
			shellCreated = !owned
			warning = errors.Join(warning, registerErr)
		default:
			return InstallResult{}, rollback(registerErr)
		}
	}

	// COM is deliberately last among transactional integrations. Register()
	// cleans its own newly created partial keys on failure, while a successful
	// final COM operation needs no speculative unregister that could remove a
	// pre-existing owned registration.
	if options.WithCOM {
		owned, probeErr := deps.comOwned(paths.executable)
		if probeErr != nil {
			return InstallResult{}, rollback(fmt.Errorf(
				"inspect COM registration: %w",
				probeErr,
			))
		}
		if err := deps.registerCOM(paths.executable); err != nil {
			if !owned {
				err = errors.Join(
					err,
					deps.unregisterCOM(paths.executable),
				)
			}
			return InstallResult{}, rollback(err)
		}
	}

	if backupDirectory != "" {
		if err := removeManagedInstallDirectory(
			backupDirectory,
			paths.programs,
			backupPrefix,
		); err != nil {
			warning = errors.Join(
				warning,
				fmt.Errorf("remove rollback directory: %w", err),
			)
		} else {
			backupDirectory = ""
		}
	}
	if options.Launch {
		if err := deps.launch(paths.executable); err != nil {
			// The installation and integrations are already committed. A
			// launch failure is useful to report but is not a reason to tear
			// down a valid installation.
			warning = errors.Join(
				warning,
				fmt.Errorf("launch installed RayanLamp: %w", err),
			)
		}
	}
	result.Warning = warning
	return result, nil
}

func uninstallWithDependencies(
	ctx context.Context,
	options UninstallOptions,
	deps dependencies,
) (UninstallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDependencies(deps); err != nil {
		return UninstallResult{}, err
	}
	paths, err := resolveLayout(deps)
	if err != nil {
		return UninstallResult{}, err
	}
	result := UninstallResult{Executable: paths.executable}
	existing, err := inspectExistingInstallation(paths)
	if err != nil {
		return result, err
	}
	if !existing {
		return result, ErrNotInstalled
	}
	operationCtx, cancelOperation := operationContext(ctx)
	err = deps.stopPrimary(operationCtx)
	cancelOperation()
	if err != nil {
		return result, fmt.Errorf(
			"stop running RayanLamp before uninstall: %w",
			err,
		)
	}
	// Recheck after coordinating shutdown so an installation transferred to a
	// junction or incomplete directory cannot be removed.
	if _, err := inspectExistingInstallation(paths); err != nil {
		return result, err
	}

	var warning error
	if err := deps.unregisterShell(paths.executable); err != nil {
		switch {
		case errors.Is(err, shellintegration.ErrRegistrationOwned):
			warning = errors.Join(warning, err)
		case shellintegration.IsPartial(err):
			owned, probeErr := deps.shellOwned(paths.executable)
			if probeErr != nil {
				return result, errors.Join(
					err,
					fmt.Errorf(
						"recheck shell integration after partial cleanup: %w",
						probeErr,
					),
				)
			}
			if owned {
				return result, errors.Join(
					err,
					errors.New(
						"owned URI registration remains after partial cleanup",
					),
				)
			}
			warning = errors.Join(warning, err)
		default:
			return result, fmt.Errorf(
				"remove owned shell integration: %w",
				err,
			)
		}
	}
	if err := deps.setStartup(false, paths.executable); err != nil {
		return result, fmt.Errorf(
			"remove owned start-with-Windows registration: %w",
			err,
		)
	}
	startupStillOwned, err := deps.startupOwned(paths.executable)
	if err != nil {
		return result, fmt.Errorf(
			"recheck start-with-Windows cleanup: %w",
			err,
		)
	}
	if startupStillOwned {
		return result, errors.New(
			"owned start-with-Windows registration remains after cleanup",
		)
	}
	if err := deps.unregisterCOM(paths.executable); err != nil {
		if errors.Is(
			err,
			comserver.ErrRegistrationOwnedByAnotherExecutable,
		) {
			owned, probeErr := deps.comServerOwned(paths.executable)
			if owned {
				return result, errors.Join(
					err,
					probeErr,
					errors.New(
						"an owned COM registry view remains after cleanup",
					),
				)
			}
			if probeErr != nil && !errors.Is(
				probeErr,
				comserver.ErrRegistrationOwnedByAnotherExecutable,
			) {
				return result, errors.Join(
					err,
					fmt.Errorf(
						"recheck COM cleanup: %w",
						probeErr,
					),
				)
			}
			warning = errors.Join(warning, err, probeErr)
		} else {
			return result, fmt.Errorf(
				"remove owned COM registration: %w",
				err,
			)
		}
	} else {
		comStillOwned, probeErr := deps.comServerOwned(paths.executable)
		switch {
		case errors.Is(
			probeErr,
			comserver.ErrRegistrationOwnedByAnotherExecutable,
		):
			warning = errors.Join(warning, probeErr)
		case probeErr != nil:
			return result, fmt.Errorf(
				"recheck COM cleanup: %w",
				probeErr,
			)
		case comStillOwned:
			return result, errors.New(
				"owned COM registration remains after cleanup",
			)
		}
	}

	current, err := resolvedExecutable(deps.currentExecutable)
	if err != nil {
		return result, err
	}
	if err := validateRegularFile(current, "cleanup-helper source"); err != nil {
		return result, err
	}
	tempDirectory, err := resolvedDirectory(deps.tempDirectory)
	if err != nil {
		return result, fmt.Errorf("resolve temporary directory: %w", err)
	}
	if err := os.MkdirAll(tempDirectory, 0o700); err != nil {
		return result, fmt.Errorf("create temporary directory: %w", err)
	}
	helper, err := reserveHelperPath(tempDirectory)
	if err != nil {
		return result, err
	}
	if err := copyFileVerified(current, helper); err != nil {
		return result, err
	}
	cleanup := CleanupOptions{
		ParentPID:      uint32(deps.processID()),
		InstallPath:    paths.directory,
		RemoveUserData: options.RemoveUserData,
	}
	if err := deps.launchHelper(helper, cleanup); err != nil {
		_ = os.Remove(helper)
		return result, fmt.Errorf("launch detached cleanup helper: %w", err)
	}
	result.Warning = warning
	return result, nil
}

func runCleanupHelperWithDependencies(
	ctx context.Context,
	options CleanupOptions,
	deps dependencies,
) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDependencies(deps); err != nil {
		return err
	}
	paths, err := resolveLayout(deps)
	if err != nil {
		return err
	}
	helper, err := resolvedExecutable(deps.currentExecutable)
	if err != nil {
		return err
	}
	tempDirectory, err := resolvedDirectory(deps.tempDirectory)
	if err != nil {
		return err
	}
	if err := validateCleanupHelperIdentity(
		helper,
		tempDirectory,
	); err != nil {
		return err
	}
	// The helper is transient even when target cleanup fails. Mark it for
	// deletion on every validated exit path, and preserve both errors if
	// Windows also rejects self-removal.
	defer func() {
		returnErr = errors.Join(returnErr, unlinkCurrentHelper(helper))
	}()
	if err := os.Chdir(tempDirectory); err != nil {
		return fmt.Errorf(
			"change cleanup helper working directory: %w",
			err,
		)
	}
	if err := validateCleanupRequest(
		options,
		paths,
		helper,
		tempDirectory,
		uint32(deps.processID()),
	); err != nil {
		return err
	}
	if err := waitForProcessExit(
		ctx,
		options.ParentPID,
		cleanupWaitTimeout,
	); err != nil {
		return err
	}
	probeContext, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	running := deps.primaryRunning(probeContext)
	cancel()
	if running {
		return errors.New(
			"selfinstall: cleanup stopped because RayanLamp started again",
		)
	}
	if err := removeExactInstallDirectoryWithRetry(
		paths,
		fileRetryTimeout,
	); err != nil {
		return err
	}
	if options.RemoveUserData {
		if err := removeExactUserDataWithRetry(
			paths,
			fileRetryTimeout,
		); err != nil {
			return err
		}
	}
	return nil
}

func resolveLayout(deps dependencies) (layout, error) {
	localAppData, err := resolvedDirectory(deps.localAppData)
	if err != nil {
		return layout{}, fmt.Errorf("resolve LocalAppData: %w", err)
	}
	roamingAppData, err := resolvedDirectory(deps.roamingAppData)
	if err != nil {
		return layout{}, fmt.Errorf("resolve roaming AppData: %w", err)
	}
	programs := filepath.Join(localAppData, "Programs")
	directory := filepath.Join(programs, installDirectoryName)
	executable := filepath.Join(directory, executableName)
	userData := filepath.Join(roamingAppData, installDirectoryName)
	return layout{
		localAppData: localAppData,
		programs:     programs,
		directory:    directory,
		executable:   executable,
		userData:     userData,
	}, nil
}

func validateDependencies(deps dependencies) error {
	switch {
	case deps.localAppData == nil:
		return errors.New("selfinstall: nil LocalAppData resolver")
	case deps.roamingAppData == nil:
		return errors.New("selfinstall: nil roaming AppData resolver")
	case deps.tempDirectory == nil:
		return errors.New("selfinstall: nil temporary-directory resolver")
	case deps.currentExecutable == nil:
		return errors.New("selfinstall: nil executable resolver")
	case deps.stopPrimary == nil:
		return errors.New("selfinstall: nil primary shutdown function")
	case deps.primaryRunning == nil:
		return errors.New("selfinstall: nil primary probe")
	case deps.startupOwned == nil || deps.setStartup == nil:
		return errors.New("selfinstall: nil startup integration")
	case deps.shellOwned == nil ||
		deps.registerShell == nil ||
		deps.unregisterShell == nil:
		return errors.New("selfinstall: nil shell integration")
	case deps.comOwned == nil ||
		deps.comServerOwned == nil ||
		deps.registerCOM == nil ||
		deps.unregisterCOM == nil:
		return errors.New("selfinstall: nil COM integration")
	case deps.launch == nil || deps.launchHelper == nil:
		return errors.New("selfinstall: nil process launcher")
	case deps.processID == nil:
		return errors.New("selfinstall: nil process-ID provider")
	default:
		return nil
	}
}

func resolvedExecutable(
	resolve func() (string, error),
) (string, error) {
	if resolve == nil {
		return "", errors.New("selfinstall: nil executable resolver")
	}
	path, err := resolve()
	if err != nil {
		return "", fmt.Errorf("locate current executable: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsAny(path, "\"\x00\r\n") {
		return "", errors.New("selfinstall: invalid current executable path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func resolvedDirectory(resolve func() (string, error)) (string, error) {
	if resolve == nil {
		return "", errors.New("selfinstall: nil directory resolver")
	}
	path, err := resolve()
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("selfinstall: invalid directory path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func inspectExistingInstallation(paths layout) (bool, error) {
	info, err := os.Lstat(paths.directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect installation directory: %w", err)
	}
	if !info.IsDir() || isReparsePoint(info) {
		return false, fmt.Errorf(
			"%w: installation directory is not a normal directory",
			ErrUnsafePath,
		)
	}
	if err := validateInstallDirectoryContents(paths); err != nil {
		return false, err
	}
	if err := validateRegularFile(
		paths.executable,
		"installed executable",
	); err != nil {
		return false, fmt.Errorf(
			"%w: %v",
			ErrUnsafePath,
			err,
		)
	}
	return true, nil
}

func validateInstallDirectoryContents(paths layout) error {
	entries, err := os.ReadDir(paths.directory)
	if err != nil {
		return fmt.Errorf("read installation directory: %w", err)
	}
	if len(entries) != 1 ||
		!strings.EqualFold(entries[0].Name(), executableName) ||
		entries[0].IsDir() {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return fmt.Errorf(
			"%w: installation directory has unexpected entries %q",
			ErrUnsafePath,
			names,
		)
	}
	return nil
}

func validateRegularFile(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() || isReparsePoint(info) {
		return fmt.Errorf("%s is not a normal regular file", label)
	}
	return nil
}

func isReparsePoint(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func copyFileVerified(source, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source executable: %w", err)
	}
	defer sourceFile.Close()
	sourceHash := sha256.New()
	destinationFile, err := os.OpenFile(
		destination,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o700,
	)
	if err != nil {
		return fmt.Errorf("create staged executable: %w", err)
	}
	_, copyErr := io.Copy(
		destinationFile,
		io.TeeReader(sourceFile, sourceHash),
	)
	syncErr := destinationFile.Sync()
	closeErr := destinationFile.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.Join(copyErr, syncErr, closeErr)
	}
	destinationFile, err = os.Open(destination)
	if err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("reopen staged executable: %w", err)
	}
	destinationHash := sha256.New()
	_, hashErr := io.Copy(destinationHash, destinationFile)
	closeErr = destinationFile.Close()
	if hashErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.Join(hashErr, closeErr)
	}
	if !equalBytes(sourceHash.Sum(nil), destinationHash.Sum(nil)) {
		_ = os.Remove(destination)
		return errors.New(
			"selfinstall: staged executable failed SHA-256 verification",
		)
	}
	return nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func reserveSiblingPath(parent, prefix string) (string, error) {
	directory, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", fmt.Errorf("reserve rollback path: %w", err)
	}
	if err := os.Remove(directory); err != nil {
		return "", fmt.Errorf("release rollback path: %w", err)
	}
	return directory, nil
}

func removeManagedSibling(path, parent, prefix string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	expectedParent, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(absolute), expectedParent) ||
		!strings.HasPrefix(
			strings.ToLower(filepath.Base(absolute)),
			strings.ToLower(prefix),
		) {
		return fmt.Errorf("%w: %s", ErrUnsafePath, absolute)
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if isReparsePoint(info) {
		return fmt.Errorf("%w: managed directory is a reparse point", ErrUnsafePath)
	}
	return os.RemoveAll(absolute)
}

func validateManagedInstallDirectory(path, parent, prefix string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	expectedParent, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(absolute), expectedParent) ||
		!strings.HasPrefix(
			strings.ToLower(filepath.Base(absolute)),
			strings.ToLower(prefix),
		) {
		return fmt.Errorf("%w: %s", ErrUnsafePath, absolute)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !info.IsDir() || isReparsePoint(info) {
		return fmt.Errorf(
			"%w: managed installation is not a normal directory",
			ErrUnsafePath,
		)
	}
	managed := layout{
		programs:   expectedParent,
		directory:  absolute,
		executable: filepath.Join(absolute, executableName),
	}
	if err := validateInstallDirectoryContents(managed); err != nil {
		return err
	}
	return validateRegularFile(
		managed.executable,
		"managed installed executable",
	)
}

func removeManagedInstallDirectory(path, parent, prefix string) error {
	if err := validateManagedInstallDirectory(path, parent, prefix); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func removeExactInstallDirectory(paths layout) error {
	if !samePath(
		paths.directory,
		filepath.Join(paths.localAppData, "Programs", installDirectoryName),
	) {
		return fmt.Errorf("%w: %s", ErrUnsafePath, paths.directory)
	}
	info, err := os.Lstat(paths.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || isReparsePoint(info) {
		return fmt.Errorf(
			"%w: installation directory is not a normal directory",
			ErrUnsafePath,
		)
	}
	if err := validateInstallDirectoryContents(paths); err != nil {
		return err
	}
	return os.RemoveAll(paths.directory)
}

func removeExactInstallDirectoryWithRetry(
	paths layout,
	timeout time.Duration,
) error {
	return retryUntil(timeout, func() error {
		return removeExactInstallDirectory(paths)
	})
}

func removeExactUserDataWithRetry(paths layout, timeout time.Duration) error {
	expected := filepath.Join(filepath.Dir(paths.userData), installDirectoryName)
	if !samePath(paths.userData, expected) {
		return fmt.Errorf("%w: %s", ErrUnsafePath, paths.userData)
	}
	return retryUntil(timeout, func() error {
		info, err := os.Lstat(paths.userData)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || isReparsePoint(info) {
			return fmt.Errorf(
				"%w: user-data directory is not a normal directory",
				ErrUnsafePath,
			)
		}
		return os.RemoveAll(paths.userData)
	})
}

func retryUntil(timeout time.Duration, operation func() error) error {
	if operation == nil {
		return errors.New("selfinstall: nil retry operation")
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		last = operation()
		if last == nil || time.Now().After(deadline) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func reserveHelperPath(tempDirectory string) (string, error) {
	file, err := os.CreateTemp(
		tempDirectory,
		helperPrefix+"*"+helperSuffix,
	)
	if err != nil {
		return "", fmt.Errorf("reserve cleanup-helper path: %w", err)
	}
	path := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(path)
	if closeErr != nil || removeErr != nil {
		return "", errors.Join(closeErr, removeErr)
	}
	return path, nil
}

func validateCleanupRequest(
	options CleanupOptions,
	paths layout,
	helper string,
	tempDirectory string,
	currentPID uint32,
) error {
	if options.ParentPID == 0 || options.ParentPID == currentPID {
		return errors.New("selfinstall: invalid cleanup parent process")
	}
	if !samePath(options.InstallPath, paths.directory) {
		return fmt.Errorf(
			"%w: cleanup target %s",
			ErrUnsafePath,
			options.InstallPath,
		)
	}
	return validateCleanupHelperIdentity(helper, tempDirectory)
}

func validateCleanupHelperIdentity(helper, tempDirectory string) error {
	if !samePath(filepath.Dir(helper), tempDirectory) {
		return fmt.Errorf(
			"%w: cleanup helper is outside the Windows temporary directory",
			ErrUnsafePath,
		)
	}
	name := filepath.Base(helper)
	lowerName := strings.ToLower(name)
	if !strings.HasPrefix(lowerName, strings.ToLower(helperPrefix)) ||
		!strings.HasSuffix(lowerName, helperSuffix) {
		return fmt.Errorf(
			"%w: cleanup helper has an unexpected name",
			ErrUnsafePath,
		)
	}
	return validateRegularFile(helper, "cleanup helper")
}

func launchCleanupHelper(helper string, options CleanupOptions) error {
	command := cleanupHelperCommand(helper, options)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func cleanupHelperCommand(
	helper string,
	options CleanupOptions,
) *exec.Cmd {
	arguments := []string{
		"/cleanup-helper",
		"/quiet",
		"--parent-pid", strconv.FormatUint(uint64(options.ParentPID), 10),
		"--install-path", options.InstallPath,
	}
	if options.RemoveUserData {
		arguments = append(arguments, "--remove-user-data")
	}
	command := exec.Command(helper, arguments...)
	command.Dir = filepath.Dir(helper)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS |
			windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow: true,
	}
	return command
}

func stopPrimary(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	probeContext, cancelProbe := context.WithTimeout(ctx, 500*time.Millisecond)
	running := ipc.IsRunning(probeContext)
	cancelProbe()
	if !running {
		return nil
	}
	request, err := ipc.NewRequest(
		"selfinstall-quit-"+strconv.FormatInt(time.Now().UnixNano(), 10),
		"quit",
		nil,
	)
	if err != nil {
		return err
	}
	response, callErr := ipc.Call(ctx, request)
	if callErr == nil && !response.OK {
		if response.Error == nil {
			return errors.New("running RayanLamp rejected the quit request")
		}
		return fmt.Errorf(
			"running RayanLamp rejected the quit request: %s",
			response.Error.Message,
		)
	}
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	for {
		probeContext, cancelProbe = context.WithTimeout(
			ctx,
			350*time.Millisecond,
		)
		running = ipc.IsRunning(probeContext)
		cancelProbe()
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			if callErr != nil {
				return errors.Join(callErr, ctx.Err())
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func operationContext(
	parent context.Context,
) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return parent, func() {}
	}
	return context.WithTimeout(parent, defaultOperationTimeout)
}

func waitForProcessExit(
	ctx context.Context,
	processID uint32,
	timeout time.Duration,
) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, processID)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open cleanup parent process: %w", err)
	}
	defer windows.CloseHandle(handle)

	deadline := time.Now().Add(timeout)
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New(
				"selfinstall: timed out waiting for the uninstaller to exit",
			)
		}
		wait := remaining
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		result, err := windows.WaitForSingleObject(
			handle,
			uint32(wait/time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("wait for cleanup parent process: %w", err)
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			continue
		default:
			return fmt.Errorf(
				"selfinstall: unexpected process wait result 0x%08X",
				result,
			)
		}
	}
}

func windowsTempDirectory() (string, error) {
	size := uint32(windows.MAX_PATH + 1)
	for {
		buffer := make([]uint16, size)
		written, err := windows.GetTempPath(size, &buffer[0])
		if err != nil {
			return "", err
		}
		if written == 0 {
			return "", errors.New("Windows returned an empty temporary path")
		}
		if written < size {
			return windows.UTF16ToString(buffer[:written]), nil
		}
		size = written + 1
		if size > 32768 {
			return "", errors.New("Windows temporary path is too long")
		}
	}
}

func unlinkCurrentHelper(path string) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|
			windows.FILE_SHARE_WRITE|
			windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("open cleanup helper for self-removal: %w", err)
	}
	const (
		fileDispositionInfoEx     = 21
		fileDispositionFlagDelete = 0x00000001
		fileDispositionFlagPOSIX  = 0x00000002
	)
	flags := uint32(
		fileDispositionFlagDelete |
			fileDispositionFlagPOSIX,
	)
	dispositionErr := windows.SetFileInformationByHandle(
		handle,
		fileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)),
		uint32(unsafe.Sizeof(flags)),
	)
	closeErr := windows.CloseHandle(handle)
	if dispositionErr == nil {
		return closeErr
	}

	// A conservative compatibility fallback may succeed after the explicit
	// handle is closed. Never schedule privileged reboot-time deletion.
	removeErr := os.Remove(path)
	if removeErr == nil {
		return closeErr
	}
	return errors.Join(
		fmt.Errorf(
			"mark cleanup helper for POSIX self-removal: %w",
			dispositionErr,
		),
		closeErr,
		fmt.Errorf("remove cleanup helper: %w", removeErr),
	)
}

func samePath(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return strings.EqualFold(
		filepath.Clean(leftAbsolute),
		filepath.Clean(rightAbsolute),
	)
}

func pathWithin(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative != "." &&
		relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
