//go:build linux

package runtimeinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"pccontroller.local/controller/internal/programmer"
)

const runtimeOwnerMarker = "pccontroller-linux-runtime-root/v1\n"

var runtimeUnitNames = []string{
	"pccontroller-virtual-board.service",
	"pccontroller-controller.service",
	"pccontroller-window.service",
}

var runtimeCurrentUserHome = os.UserHomeDir

func Install(ctx context.Context, options InstallOptions) (Report, error) {
	root, err := runtimeRoot(options.Root)
	report := newReport("install", options.Apply, root)
	if err != nil {
		return report, err
	}
	options.Root = root
	if err := validateTarget(options.TargetUser, options.TargetUID, options.TargetHome); err != nil {
		return report, err
	}
	validated, err := ValidatePackage(ctx, options.HostPackage, options.VirtualBoard)
	if err != nil {
		return report, err
	}
	defer validated.Close()
	report.PackageValidated = true
	browser, err := ValidateBrowser(options.Browser)
	if err != nil {
		return report, err
	}
	report.Browser = browser
	curl, err := trustedRegularExecutable(defaultString(options.Curl, "/usr/bin/curl"), "curl")
	if err != nil {
		return report, err
	}
	options.Curl = curl
	profileDigest := sha256Bytes([]byte(strings.Join([]string{
		options.TargetUser, strconv.FormatUint(uint64(options.TargetUID), 10),
		filepath.Clean(options.TargetHome), browser,
	}, "\n")))
	releaseID := validated.Controller.SHA256[:12] + "-" + validated.VirtualBoard.SHA256[:12] + "-" + profileDigest[:12]
	report.ReleaseID = releaseID
	report.TargetUser = options.TargetUser
	report.Actions = append(report.Actions,
		"validate canonical host manifest, target, size, hashes, and bounded executable smokes",
		"publish immutable release "+releaseID+" under "+filepath.Join(root, "releases"),
		"point stable root-owned executable and manifest paths at the release",
		"link three root-owned units into "+options.TargetUser+"'s graphical user session",
	)
	if !options.Apply {
		report.UserManager = userManagerAvailability(options.TargetUID)
		report.Warnings = append(report.Warnings, "executable smokes are deferred until --apply; they run as the target user only after pinned bytes enter a root-owned stage")
		return report, nil
	}
	if root == DefaultRoot && os.Geteuid() != 0 {
		return report, errors.New("runtime installation under /opt requires root; no state was changed")
	}
	lock, err := acquireRuntimeLock(root)
	if err != nil {
		return report, err
	}
	defer lock.Close()
	if err := ensureRuntimeRoot(root); err != nil {
		return report, err
	}
	release, created, _, err := publishRelease(ctx, options, validated, browser, releaseID)
	if err != nil {
		return report, err
	}
	oldCurrent, currentErr := readManagedReleaseLink(root, "current")
	if currentErr != nil {
		if created {
			_ = os.RemoveAll(release)
		}
		return report, currentErr
	}
	oldPrevious, previousErr := readManagedReleaseLink(root, "previous")
	if previousErr != nil {
		if created {
			_ = os.RemoveAll(release)
		}
		return report, previousErr
	}
	if oldCurrent != "" {
		report.PreviousReleaseID = filepath.Base(oldCurrent)
		currentManifest, loadErr := loadRuntimeManifest(filepath.Join(root, oldCurrent, "runtime-manifest.json"))
		if loadErr != nil {
			if created {
				_ = os.RemoveAll(release)
			}
			return report, loadErr
		}
		if currentManifest.TargetUser != options.TargetUser || currentManifest.TargetUID != options.TargetUID ||
			filepath.Clean(currentManifest.TargetHome) != filepath.Clean(options.TargetHome) {
			if created {
				_ = os.RemoveAll(release)
			}
			return report, errors.New("runtime target account differs from the installed profile; uninstall the existing runtime explicitly before changing accounts")
		}
	}
	stableCreated, err := ensureStableLinks(root)
	if err != nil {
		if created {
			_ = os.RemoveAll(release)
		}
		return report, err
	}
	rollbackPublication := func() error {
		var cleanup []error
		if err := restoreManagedReleaseLink(root, "current", oldCurrent); err != nil {
			cleanup = append(cleanup, fmt.Errorf("restore current pointer: %w", err))
		}
		if err := restoreManagedReleaseLink(root, "previous", oldPrevious); err != nil {
			cleanup = append(cleanup, fmt.Errorf("restore previous pointer: %w", err))
		}
		if len(cleanup) != 0 {
			return errors.Join(cleanup...)
		}
		for _, path := range stableCreated {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				cleanup = append(cleanup, fmt.Errorf("remove stable link %s: %w", path, err))
			}
		}
		if created {
			if err := os.RemoveAll(release); err != nil {
				cleanup = append(cleanup, fmt.Errorf("remove failed release: %w", err))
			}
		}
		return errors.Join(cleanup...)
	}
	if oldCurrent != "" && oldCurrent != filepath.Join("releases", releaseID) {
		if err := setManagedReleaseLink(root, "previous", oldCurrent); err != nil {
			return report, errors.Join(fmt.Errorf("publish previous release pointer: %w", err), rollbackPublication())
		}
	}
	if err := setManagedReleaseLink(root, "current", filepath.Join("releases", releaseID)); err != nil {
		return report, errors.Join(fmt.Errorf("publish current release pointer: %w", err), rollbackPublication())
	}
	if err := manageTargetUserLinks(ctx, options, "ensure"); err != nil {
		return report, errors.Join(err, rollbackPublication())
	}
	activation, err := activateUserRuntime(ctx, options, "restart")
	if err != nil {
		if oldCurrent == "" {
			_, stopErr := activateUserRuntime(ctx, options, "stop")
			if stopErr != nil {
				return report, errors.Join(
					fmt.Errorf("activate target-user runtime: %w", err),
					fmt.Errorf("stop partially started first-install services before rollback: %w", stopErr),
				)
			}
			if removeErr := manageTargetUserLinks(ctx, options, "remove"); removeErr != nil {
				return report, errors.Join(
					fmt.Errorf("activate target-user runtime: %w", err),
					fmt.Errorf("remove first-install user links after services stopped: %w", removeErr),
				)
			}
			_, reloadErr := activateUserRuntime(ctx, options, "reload")
			if reloadErr != nil {
				return report, errors.Join(
					fmt.Errorf("activate target-user runtime: %w", err),
					fmt.Errorf("reload target-user manager after stopping services and removing links: %w", reloadErr),
				)
			}
			rollbackErr := rollbackPublication()
			return report, errors.Join(
				fmt.Errorf("activate target-user runtime; first-install services stopped and publication rolled back: %w", err),
				rollbackErr,
			)
		}
		rollbackErr := rollbackPublication()
		currentAfterRollback, currentReadErr := readManagedReleaseLink(root, "current")
		var reactivateErr error
		if currentReadErr != nil {
			reactivateErr = fmt.Errorf("read current pointer before old-release reactivation: %w", currentReadErr)
		} else if currentAfterRollback != oldCurrent {
			reactivateErr = fmt.Errorf("current pointer is %q after rollback, want %q; old release was not restarted", currentAfterRollback, oldCurrent)
		} else {
			_, reactivateErr = activateUserRuntime(ctx, options, "restart")
		}
		if reactivateErr != nil {
			reactivateErr = fmt.Errorf("reactivate previous release after rollback: %w", reactivateErr)
		}
		return report, errors.Join(
			fmt.Errorf("activate target-user runtime; publication rolled back: %w", err),
			rollbackErr,
			reactivateErr,
		)
	}
	report.UserManager = activation
	report.RuntimeReady = activation == "activated-ready"
	report.ExecutableSmokesPassed = true
	report.StableLinksReady = true
	report.UserUnitsReady = true
	report.Installed = true
	report.ArtifactsVerified = true
	return report, nil
}

func Status(ctx context.Context, options OperationOptions) (Report, error) {
	root, err := runtimeRoot(options.Root)
	report := newReport("status", false, root)
	if err != nil {
		return report, err
	}
	if err := validateOwnedRuntimeRoot(root); errors.Is(err, os.ErrNotExist) {
		return report, nil
	} else if err != nil {
		return report, err
	}
	manifest, release, err := loadCurrentManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	report.Installed = true
	report.ReleaseID = manifest.ReleaseID
	report.TargetUser = manifest.TargetUser
	report.Browser = manifest.Browser
	report.ExecutableSmokesPassed = manifest.ExecutableSmokesPassed
	report.UserManager = userManagerAvailability(manifest.TargetUID)
	previous, err := readManagedReleaseLink(root, "previous")
	if err != nil {
		return report, err
	}
	if previous != "" {
		report.PreviousReleaseID = filepath.Base(previous)
	}
	if err := verifyRelease(release, manifest); err != nil {
		return report, err
	}
	report.ArtifactsVerified = true
	ready, err := stableLinksReady(root)
	if err != nil {
		return report, err
	}
	report.StableLinksReady = ready
	userReady, err := userRuntimeLinksReady(root, manifest)
	if err != nil {
		return report, err
	}
	report.UserUnitsReady = userReady
	_ = ctx
	return report, nil
}

func Rollback(ctx context.Context, options OperationOptions) (Report, error) {
	root, err := runtimeRoot(options.Root)
	report := newReport("rollback", options.Apply, root)
	if err != nil {
		return report, err
	}
	if options.Apply {
		if root == DefaultRoot && os.Geteuid() != 0 {
			return report, errors.New("runtime rollback under /opt requires root; no state was changed")
		}
		lock, lockErr := acquireRuntimeLock(root)
		if lockErr != nil {
			return report, lockErr
		}
		defer lock.Close()
	}
	if err := validateOwnedRuntimeRoot(root); err != nil {
		return report, err
	}
	currentManifest, currentRelease, err := loadCurrentManifest(root)
	if err != nil {
		return report, err
	}
	previousTarget, err := readManagedReleaseLink(root, "previous")
	if err != nil {
		return report, err
	}
	if previousTarget == "" {
		return report, errors.New("no previous PCController runtime release is available")
	}
	previousRelease := filepath.Join(root, previousTarget)
	previousManifest, err := loadRuntimeManifest(filepath.Join(previousRelease, "runtime-manifest.json"))
	if err != nil {
		return report, fmt.Errorf("validate previous runtime manifest: %w", err)
	}
	if previousManifest.ReleaseID != filepath.Base(previousTarget) {
		return report, errors.New("previous runtime pointer does not match its release manifest")
	}
	if err := verifyRelease(previousRelease, previousManifest); err != nil {
		return report, fmt.Errorf("validate previous runtime release: %w", err)
	}
	report.Installed = true
	report.ReleaseID = previousManifest.ReleaseID
	report.PreviousReleaseID = currentManifest.ReleaseID
	report.TargetUser = previousManifest.TargetUser
	report.Browser = previousManifest.Browser
	report.ArtifactsVerified = true
	report.Actions = append(report.Actions,
		"atomically repoint current to "+previousManifest.ReleaseID,
		"retain "+currentManifest.ReleaseID+" as the next rollback target",
		"reload and restart the target-user runtime when its manager is available",
	)
	if !options.Apply {
		report.UserManager = userManagerAvailability(previousManifest.TargetUID)
		return report, nil
	}
	currentTarget, err := filepath.Rel(root, currentRelease)
	if err != nil {
		return report, err
	}
	if err := setManagedReleaseLink(root, "current", previousTarget); err != nil {
		return report, err
	}
	if err := setManagedReleaseLink(root, "previous", currentTarget); err != nil {
		return report, errors.Join(err, setManagedReleaseLink(root, "current", currentTarget))
	}
	activationOptions := InstallOptions{
		Root: root, TargetUser: previousManifest.TargetUser, TargetUID: previousManifest.TargetUID,
		TargetHome: previousManifest.TargetHome,
		Runuser:    options.Runuser, Systemctl: options.Systemctl, Curl: options.Curl, Runner: options.Runner,
	}
	activation, err := activateUserRuntime(ctx, activationOptions, "restart")
	if err != nil {
		restoreCurrentErr := setManagedReleaseLink(root, "current", currentTarget)
		restorePreviousErr := setManagedReleaseLink(root, "previous", previousTarget)
		oldActivationOptions := InstallOptions{
			Root: root, TargetUser: currentManifest.TargetUser, TargetUID: currentManifest.TargetUID,
			TargetHome: currentManifest.TargetHome,
			Runuser:    options.Runuser, Systemctl: options.Systemctl, Curl: options.Curl, Runner: options.Runner,
		}
		currentAfterRestore, currentReadErr := readManagedReleaseLink(root, "current")
		var reactivateErr error
		if currentReadErr != nil {
			reactivateErr = fmt.Errorf("read current pointer before original-release reactivation: %w", currentReadErr)
		} else if currentAfterRestore != currentTarget {
			reactivateErr = fmt.Errorf("current pointer is %q after failed rollback, want %q; original release was not restarted", currentAfterRestore, currentTarget)
		} else {
			_, reactivateErr = activateUserRuntime(ctx, oldActivationOptions, "restart")
		}
		return report, errors.Join(
			fmt.Errorf("activate rollback release: %w", err),
			restoreCurrentErr,
			restorePreviousErr,
			func() error {
				if reactivateErr != nil {
					return fmt.Errorf("reactivate original release after failed rollback: %w", reactivateErr)
				}
				return nil
			}(),
		)
	}
	report.UserManager = activation
	report.RuntimeReady = activation == "activated-ready"
	report.StableLinksReady = true
	report.UserUnitsReady = true
	return report, nil
}

func Uninstall(ctx context.Context, options OperationOptions) (Report, error) {
	root, err := runtimeRoot(options.Root)
	report := newReport("uninstall", options.Apply, root)
	if err != nil {
		return report, err
	}
	if options.Apply {
		if root == DefaultRoot && os.Geteuid() != 0 {
			return report, errors.New("runtime uninstall under /opt requires root; no state was changed")
		}
		lock, lockErr := acquireRuntimeLock(root)
		if lockErr != nil {
			return report, lockErr
		}
		defer lock.Close()
	}
	if err := validateOwnedRuntimeRoot(root); errors.Is(err, os.ErrNotExist) {
		return report, nil
	} else if err != nil {
		return report, err
	}
	manifest, _, err := loadCurrentManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	report.Installed = true
	report.ReleaseID = manifest.ReleaseID
	report.TargetUser = manifest.TargetUser
	report.Actions = append(report.Actions,
		"stop the three target-user runtime services when the user manager is available",
		"remove only exact PCController-managed user unit links",
		"remove the root-owned runtime releases and stable links",
		"preserve PCController config, VirtualBoard EEPROM, and Chrome profile under the target home",
	)
	if !options.Apply {
		report.UserManager = userManagerAvailability(manifest.TargetUID)
		return report, nil
	}
	activationOptions := InstallOptions{
		Root: root, TargetUser: manifest.TargetUser, TargetUID: manifest.TargetUID,
		TargetHome: manifest.TargetHome,
		Runuser:    options.Runuser, Systemctl: options.Systemctl, Curl: options.Curl, Runner: options.Runner,
	}
	activation, stopErr := activateUserRuntime(ctx, activationOptions, "stop")
	if stopErr != nil {
		return report, fmt.Errorf("stop target-user runtime before uninstall: %w", stopErr)
	}
	report.UserManager = activation
	if err := manageTargetUserLinks(ctx, activationOptions, "remove"); err != nil {
		return report, err
	}
	if _, err := activateUserRuntime(ctx, activationOptions, "reload"); err != nil {
		return report, fmt.Errorf("reload target-user manager after unit removal: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return report, fmt.Errorf("remove managed runtime root: %w", err)
	}
	report.Installed = false
	report.UserUnitsReady = false
	report.StableLinksReady = false
	return report, nil
}

func newReport(operation string, apply bool, root string) Report {
	return Report{
		Platform: "linux", Operation: operation, Applied: apply, Root: root,
		UserDataPreserved: true, PrivilegedDaemonInstalled: false,
	}
}

func runtimeRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = DefaultRoot
	}
	return cleanAbsolutePath(value, "runtime root")
}

func validateTarget(username string, uid uint32, home string) error {
	if strings.TrimSpace(username) == "" || username != strings.TrimSpace(username) || strings.ContainsAny(username, "/\\\x00\r\n") {
		return errors.New("target user has an unsafe account name")
	}
	if uid == 0 {
		return errors.New("runtime target must be an existing non-root account")
	}
	home, err := cleanAbsolutePath(home, "target home")
	if err != nil {
		return err
	}
	info, err := os.Lstat(home)
	if err != nil {
		return fmt.Errorf("inspect target home: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("target home must be a real directory, not a symlink")
	}
	owner, err := fileUID(info)
	if err != nil {
		return err
	}
	if owner != uid {
		return fmt.Errorf("target home is owned by uid %d, not target uid %d", owner, uid)
	}
	return nil
}

func validateBrowser(path string) (string, error) {
	resolved, err := trustedBrowserExecutable(path, "Chrome/Chromium executable")
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(resolved, "\r\n\x00%") {
		return "", errors.New("Chrome/Chromium executable has an unsafe path")
	}
	return resolved, nil
}

// ValidateBrowser pins and validates both the launcher and the native process
// executable before runtime-install is allowed to stage or publish anything.
func ValidateBrowser(path string) (string, error) {
	resolved, err := validateBrowser(path)
	if err != nil {
		return "", err
	}
	if _, err := browserMainExecutable(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func browserMainExecutable(launcher string) (string, error) {
	launcher, err := trustedBrowserExecutable(launcher, "Chrome/Chromium launcher")
	if err != nil {
		return "", err
	}
	file, err := os.Open(launcher)
	if err != nil {
		return "", err
	}
	prefix := make([]byte, 2)
	_, readErr := io.ReadFull(file, prefix)
	_ = file.Close()
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", readErr
	}
	if string(prefix) != "#!" {
		return launcher, nil
	}
	if filepath.Clean(launcher) == "/opt/google/chrome/google-chrome" {
		return trustedBrowserExecutable("/opt/google/chrome/chrome", "Google Chrome MainPID executable")
	}
	return "", fmt.Errorf("browser launcher %s is a script; pass the native browser executable so MainPID identity can be verified", launcher)
}

func ensureRuntimeRoot(root string) error {
	if err := ensureManagedDirectory(root, 0o755, true); err != nil {
		return err
	}
	for _, directory := range []string{filepath.Join(root, "releases"), filepath.Join(root, "bin")} {
		if err := ensureManagedDirectory(directory, 0o755, true); err != nil {
			return err
		}
	}
	marker := filepath.Join(root, ".pccontroller-runtime-root")
	if content, err := readBoundedRegular(marker, 256); err == nil {
		if string(content) != runtimeOwnerMarker {
			return errors.New("runtime root ownership marker is not recognized")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(marker, []byte(runtimeOwnerMarker), 0o644)
}

func acquireRuntimeLock(root string) (*os.File, error) {
	parent := filepath.Dir(root)
	if err := ensureDirectory(parent, 0o755, true); err != nil {
		return nil, fmt.Errorf("prepare runtime lock directory: %w", err)
	}
	path := filepath.Join(parent, ".pccontroller-runtime.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open runtime operation lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if os.Geteuid() == 0 {
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("inspect runtime operation lock: %w", statErr)
		}
		if trustErr := validateTrustedInfo(info, false, false); trustErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("trust runtime operation lock: %w", trustErr)
		}
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire runtime operation lock: %w", err)
	}
	return file, nil
}

func validateOwnedRuntimeRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed runtime root is not a real directory")
	}
	if root == DefaultRoot {
		owner, err := fileUID(info)
		if err != nil || owner != 0 {
			return errors.New("managed runtime root is not root-owned")
		}
	}
	for _, name := range []string{"releases", "bin"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed runtime %s path is not a real directory", name)
		}
		if root == DefaultRoot {
			owner, err := fileUID(info)
			if err != nil || owner != 0 {
				return fmt.Errorf("managed runtime %s path is not root-owned", name)
			}
		}
	}
	content, err := readBoundedRegular(filepath.Join(root, ".pccontroller-runtime-root"), 256)
	if err != nil || string(content) != runtimeOwnerMarker {
		return errors.New("refusing runtime removal without the exact Controller ownership marker")
	}
	return nil
}

func publishRelease(
	ctx context.Context,
	options InstallOptions,
	validated ValidatedPackage,
	browser, releaseID string,
) (string, bool, RuntimeManifest, error) {
	releases := filepath.Join(options.Root, "releases")
	destination := filepath.Join(releases, releaseID)
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", false, RuntimeManifest{}, errors.New("existing release path is not a real directory")
		}
		manifest, loadErr := loadRuntimeManifest(filepath.Join(destination, "runtime-manifest.json"))
		if loadErr != nil {
			return "", false, RuntimeManifest{}, loadErr
		}
		if manifest.ReleaseID != releaseID {
			return "", false, RuntimeManifest{}, errors.New("existing release identity does not match its directory")
		}
		if verifyErr := verifyRelease(destination, manifest); verifyErr != nil {
			return "", false, RuntimeManifest{}, verifyErr
		}
		return destination, false, manifest, nil
	} else if !os.IsNotExist(err) {
		return "", false, RuntimeManifest{}, err
	}
	stage, err := os.MkdirTemp(releases, ".runtime-stage-")
	if err != nil {
		return "", false, RuntimeManifest{}, err
	}
	stageOwned := true
	defer func() {
		if stageOwned {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o755); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	for _, directory := range []string{filepath.Join(stage, "bin"), filepath.Join(stage, "systemd"), filepath.Join(stage, "systemd", "user")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return "", false, RuntimeManifest{}, err
		}
		if err := os.Chmod(directory, 0o755); err != nil {
			return "", false, RuntimeManifest{}, err
		}
	}
	controllerDestination := filepath.Join(stage, "bin", "controller")
	boardDestination := filepath.Join(stage, "bin", "virtual-board")
	if err := copyVerifiedExecutable(validated.controllerFile, validated.Controller, controllerDestination); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	if err := copyVerifiedExecutable(validated.virtualBoardFile, validated.VirtualBoard, boardDestination); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	if err := atomicWrite(filepath.Join(stage, "host-manifest.json"), validated.ManifestBytes, 0o644); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	if err := smokeStagedExecutables(ctx, options, controllerDestination, boardDestination, validated.Host); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	unitContents := runtimeUnits(options.Root, browser, options.Curl)
	files := []FileIdentity{
		{Role: "controller", Path: "bin/controller", Bytes: validated.Controller.Bytes, SHA256: validated.Controller.SHA256},
		{Role: "virtual-board", Path: "bin/virtual-board", Bytes: validated.VirtualBoard.Bytes, SHA256: validated.VirtualBoard.SHA256},
		{Role: "host-manifest", Path: "host-manifest.json", Bytes: int64(len(validated.ManifestBytes)), SHA256: sha256Bytes(validated.ManifestBytes)},
	}
	for _, name := range runtimeUnitNames {
		content := []byte(unitContents[name])
		path := filepath.Join(stage, "systemd", "user", name)
		if err := atomicWrite(path, content, 0o644); err != nil {
			return "", false, RuntimeManifest{}, err
		}
		digest := sha256Bytes(content)
		files = append(files, FileIdentity{Role: "systemd-user-unit", Path: filepath.ToSlash(filepath.Join("systemd", "user", name)), Bytes: int64(len(content)), SHA256: digest})
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	manifest := RuntimeManifest{
		Format: RuntimeManifestFormat, ReleaseID: releaseID, GeneratedUTC: now.Format(time.RFC3339),
		Target: "linux/" + runtimeArchitecture(), TargetUser: options.TargetUser, TargetUID: options.TargetUID,
		TargetHome: filepath.Clean(options.TargetHome), HostVersion: validated.Host.Identity.Version,
		HostSourceSHA256: validated.Host.Identity.SourceSHA256, Browser: browser, Files: files,
		ExecutableSmokesPassed:    true,
		UserDataPreservedOnRemove: true, PrivilegedDaemonInstalled: false,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", false, RuntimeManifest{}, err
	}
	if err := atomicWrite(filepath.Join(stage, "runtime-manifest.json"), append(encoded, '\n'), 0o644); err != nil {
		return "", false, RuntimeManifest{}, err
	}
	if err := verifyRelease(stage, manifest); err != nil {
		return "", false, RuntimeManifest{}, fmt.Errorf("verify staged runtime release: %w", err)
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", false, RuntimeManifest{}, fmt.Errorf("atomically publish runtime release: %w", err)
	}
	if err := syncDirectory(releases); err != nil {
		_ = os.RemoveAll(destination)
		return "", false, RuntimeManifest{}, fmt.Errorf("durably publish runtime release: %w", err)
	}
	stageOwned = false
	return destination, true, manifest, nil
}

func smokeStagedExecutables(
	ctx context.Context,
	options InstallOptions,
	controller, virtualBoard string,
	host HostManifest,
) error {
	runuser, err := trustedRegularExecutable(defaultString(options.Runuser, "/usr/sbin/runuser"), "runuser")
	if err != nil {
		return err
	}
	runner := options.Runner
	if runner == nil {
		runner = runBoundedCommand
	}
	environment := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	run := func(executable string, arguments ...string) (string, error) {
		smokeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		userArguments := []string{
			"--user", options.TargetUser, "--", "env",
			"HOME=" + filepath.Clean(options.TargetHome),
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			executable,
		}
		userArguments = append(userArguments, arguments...)
		output, runErr := runner(smokeContext, runuser, userArguments, environment)
		if runErr != nil {
			return output, fmt.Errorf("target-user smoke failed: %w: %s", runErr, boundedText(output))
		}
		return output, nil
	}
	controllerOutput, err := run(controller, "version")
	if err != nil {
		return fmt.Errorf("Controller bounded smoke: %w", err)
	}
	if err := validateControllerSmoke(controllerOutput, host); err != nil {
		return fmt.Errorf("Controller bounded smoke: %w", err)
	}
	boardOutput, err := run(virtualBoard, "--help")
	if err != nil {
		return fmt.Errorf("VirtualBoard bounded smoke: %w", err)
	}
	if !strings.Contains(boardOutput, "PCController Virtual Board") || !strings.Contains(boardOutput, "--port") {
		return errors.New("VirtualBoard bounded smoke did not expose the expected native helper identity")
	}
	return nil
}

func runtimeArchitecture() string {
	return runtime.GOARCH
}

func runtimeUnits(root, browser, curl string) map[string]string {
	controller := filepath.Join(root, "bin", "controller")
	board := filepath.Join(root, "bin", "virtual-board")
	return map[string]string{
		"pccontroller-virtual-board.service": `[Unit]
Description=PCController native VirtualBoard
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=simple
ExecStart=` + systemdQuote(board) + ` --bind 127.0.0.1 --port 8765 --eeprom ` + systemdQuote("%h/.local/share/pccontroller/virtual-board/eeprom.bin") + ` --no-stdin --quiet
Restart=on-failure
RestartSec=2s
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=graphical-session.target
`,
		"pccontroller-controller.service": `[Unit]
Description=PCController loopback WebUI host
PartOf=graphical-session.target
After=graphical-session.target pccontroller-virtual-board.service
Requires=pccontroller-virtual-board.service
BindsTo=pccontroller-virtual-board.service

[Service]
Type=simple
Environment=PCCONTROLLER_DATA_DIR=%h/.local/share/pccontroller
Environment=PCCONTROLLER_DESKTOP_EXECUTABLE=` + systemdQuote(controller) + `
ExecStart=` + systemdQuote(controller) + ` web --listen 127.0.0.1:8787 --no-open --no-tray --port tcp://127.0.0.1:8765
Restart=on-failure
RestartSec=2s
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=graphical-session.target
`,
		"pccontroller-window.service": `[Unit]
Description=PCController Chrome application window
PartOf=graphical-session.target
After=graphical-session.target pccontroller-controller.service
Requires=pccontroller-controller.service
BindsTo=pccontroller-controller.service

[Service]
Type=simple
Environment=PCCONTROLLER_DATA_DIR=%h/.local/share/pccontroller
ExecStartPre=` + systemdQuote(curl) + ` --noproxy ` + systemdQuote("*") + ` --fail --silent --show-error --retry 30 --retry-connrefused --retry-delay 1 --max-time 45 http://127.0.0.1:8787/healthz
ExecStartPre=` + systemdQuote(controller) + ` toolchain runtime-window-ready --timeout 45s
ExecStart=` + systemdQuote(browser) + ` --app=http://127.0.0.1:8787/ --user-data-dir=%h/.local/share/pccontroller/chrome-profile --no-first-run --no-default-browser-check
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true

[Install]
WantedBy=graphical-session.target
`,
	}
}

func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func verifyRelease(directory string, manifest RuntimeManifest) error {
	if err := validateRuntimeManifest(manifest); err != nil {
		return err
	}
	for _, relative := range []string{"", "bin", "systemd", filepath.Join("systemd", "user")} {
		path := filepath.Join(directory, relative)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 {
			return fmt.Errorf("runtime release directory %s is not a real 0755 directory", path)
		}
		if directory == filepath.Join(DefaultRoot, "releases", manifest.ReleaseID) {
			owner, ownerErr := fileUID(info)
			if ownerErr != nil || owner != 0 {
				return fmt.Errorf("runtime release directory %s is not root-owned", path)
			}
		}
	}
	manifestInfo, err := os.Lstat(filepath.Join(directory, "runtime-manifest.json"))
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 || manifestInfo.Mode().Perm() != 0o644 {
		return errors.New("runtime release manifest is not a regular 0644 file")
	}
	for _, identity := range manifest.Files {
		path, err := packageArtifactPath(directory, identity.Path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != identity.Bytes {
			return fmt.Errorf("runtime artifact %s does not match its manifest size/type", identity.Path)
		}
		expectedMode := os.FileMode(0o644)
		if identity.Role == "controller" || identity.Role == "virtual-board" {
			expectedMode = 0o755
		}
		if info.Mode().Perm() != expectedMode {
			return fmt.Errorf("runtime artifact %s mode is %04o, want %04o", identity.Path, info.Mode().Perm(), expectedMode)
		}
		if os.Geteuid() == 0 && strings.HasPrefix(filepath.Clean(directory)+string(filepath.Separator), filepath.Clean(DefaultRoot)+string(filepath.Separator)) {
			owner, err := fileUID(info)
			if err != nil || owner != 0 {
				return fmt.Errorf("runtime artifact %s is not root-owned", identity.Path)
			}
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("runtime artifact %s is group/world writable", identity.Path)
		}
		digest, err := sha256File(path, maximumBinaryBytes)
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, identity.SHA256) {
			return fmt.Errorf("runtime artifact %s SHA-256 mismatch", identity.Path)
		}
	}
	return nil
}

func loadCurrentManifest(root string) (RuntimeManifest, string, error) {
	target, err := readManagedReleaseLink(root, "current")
	if err != nil {
		return RuntimeManifest{}, "", err
	}
	if target == "" {
		return RuntimeManifest{}, "", os.ErrNotExist
	}
	release := filepath.Join(root, target)
	manifest, err := loadRuntimeManifest(filepath.Join(release, "runtime-manifest.json"))
	if err != nil {
		return RuntimeManifest{}, "", err
	}
	if filepath.Base(target) != manifest.ReleaseID {
		return RuntimeManifest{}, "", errors.New("current runtime pointer does not match its release manifest")
	}
	return manifest, release, nil
}

func loadRuntimeManifest(path string) (RuntimeManifest, error) {
	content, err := readBoundedRegular(path, maximumManifestBytes)
	if err != nil {
		return RuntimeManifest{}, err
	}
	var manifest RuntimeManifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return RuntimeManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return RuntimeManifest{}, errors.New("runtime manifest has trailing JSON values")
	}
	if err := validateRuntimeManifest(manifest); err != nil {
		return RuntimeManifest{}, err
	}
	return manifest, nil
}

func validateRuntimeManifest(manifest RuntimeManifest) error {
	if manifest.Format != RuntimeManifestFormat || manifest.ReleaseID == "" || filepath.Base(manifest.ReleaseID) != manifest.ReleaseID {
		return errors.New("runtime manifest has an invalid release identity")
	}
	if manifest.Target != "linux/"+runtimeArchitecture() || manifest.TargetUser == "" || manifest.TargetUID == 0 {
		return errors.New("runtime manifest has an invalid target identity")
	}
	if !filepath.IsAbs(manifest.TargetHome) || !filepath.IsAbs(manifest.Browser) || strings.ContainsAny(manifest.Browser, "\r\n\x00%") {
		return errors.New("runtime manifest has invalid target-home or browser paths")
	}
	if strings.TrimSpace(manifest.HostVersion) == "" || !validSHA256(manifest.HostSourceSHA256) {
		return errors.New("runtime manifest has an incomplete host build identity")
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedUTC); err != nil {
		return errors.New("runtime manifest has an invalid generation time")
	}
	if !manifest.ExecutableSmokesPassed {
		return errors.New("runtime manifest does not attest target-user executable smokes")
	}
	if !manifest.UserDataPreservedOnRemove || manifest.PrivilegedDaemonInstalled {
		return errors.New("runtime manifest violates the user-data/#116 boundary")
	}
	expected := map[string]string{
		"bin/controller":     "controller",
		"bin/virtual-board":  "virtual-board",
		"host-manifest.json": "host-manifest",
	}
	for _, name := range runtimeUnitNames {
		expected[filepath.ToSlash(filepath.Join("systemd", "user", name))] = "systemd-user-unit"
	}
	seen := map[string]bool{}
	for _, identity := range manifest.Files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(identity.Path)))
		role, ok := expected[clean]
		if !ok || role != identity.Role || seen[clean] {
			return fmt.Errorf("runtime manifest has an unexpected, duplicate, or mismatched artifact %q", identity.Path)
		}
		if identity.Bytes <= 0 || !validSHA256(identity.SHA256) {
			return fmt.Errorf("runtime manifest artifact %q has an invalid size or SHA-256", identity.Path)
		}
		seen[clean] = true
	}
	if len(seen) != len(expected) {
		return errors.New("runtime manifest does not contain every required release artifact exactly once")
	}
	return nil
}

func ensureStableLinks(root string) ([]string, error) {
	links := map[string]string{
		filepath.Join(root, "bin", "controller"):    filepath.Join("..", "current", "bin", "controller"),
		filepath.Join(root, "bin", "virtual-board"): filepath.Join("..", "current", "bin", "virtual-board"),
		filepath.Join(root, "manifest.json"):        filepath.Join("current", "runtime-manifest.json"),
	}
	var created []string
	for path, target := range links {
		made, err := ensureExactSymlink(path, target, 0)
		if err != nil {
			removeExactPaths(created)
			return nil, err
		}
		if made {
			created = append(created, path)
		}
	}
	return created, nil
}

func stableLinksReady(root string) (bool, error) {
	expected := map[string]string{
		filepath.Join(root, "bin", "controller"):    filepath.Join("..", "current", "bin", "controller"),
		filepath.Join(root, "bin", "virtual-board"): filepath.Join("..", "current", "bin", "virtual-board"),
		filepath.Join(root, "manifest.json"):        filepath.Join("current", "runtime-manifest.json"),
	}
	for path, target := range expected {
		actual, err := os.Readlink(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual != target {
			return false, nil
		}
	}
	return true, nil
}

func readManagedReleaseLink(root, name string) (string, error) {
	path := filepath.Join(root, name)
	target, err := os.Readlink(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("managed release pointer %s is not a symlink: %w", name, err)
	}
	clean := filepath.Clean(target)
	if filepath.IsAbs(clean) || filepath.Dir(clean) != "releases" || filepath.Base(clean) == "." || filepath.Base(clean) == ".." {
		return "", fmt.Errorf("managed release pointer %s has unsafe target %q", name, target)
	}
	info, err := os.Lstat(filepath.Join(root, clean))
	if err != nil {
		return "", fmt.Errorf("managed release pointer %s target is unavailable: %w", name, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("managed release pointer %s target is not a real directory", name)
	}
	return clean, nil
}

func setManagedReleaseLink(root, name, target string) error {
	clean := filepath.Clean(target)
	if filepath.IsAbs(clean) || filepath.Dir(clean) != "releases" {
		return fmt.Errorf("unsafe release pointer target %q", target)
	}
	return replaceSymlink(filepath.Join(root, name), clean, 0)
}

func restoreManagedReleaseLink(root, name, target string) error {
	path := filepath.Join(root, name)
	if target == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return setManagedReleaseLink(root, name, target)
}

// ManageCurrentUserLinks is the deliberately unprivileged half of runtime
// installation. The root installer invokes the published Controller through
// runuser, so all traversal and writes below happen with the target UID and a
// user-controlled ancestor can never redirect a root write or chown.
func ManageCurrentUserLinks(root, operation string) ([]string, error) {
	root, err := runtimeRoot(root)
	if err != nil {
		return nil, err
	}
	manifest, _, err := loadCurrentManifest(root)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 || uint32(os.Geteuid()) != manifest.TargetUID {
		return nil, errors.New("runtime user-link helper must run as the installed non-root target UID")
	}
	home, err := runtimeCurrentUserHome()
	if err != nil || filepath.Clean(home) != filepath.Clean(manifest.TargetHome) {
		return nil, errors.New("runtime user-link helper HOME differs from the installed target home")
	}
	dataPaths, err := programmer.HostDataPathsFor(filepath.Join(manifest.TargetHome, ".local", "share", "pccontroller"))
	if err != nil {
		return nil, err
	}
	if err := programmer.EnsureHostDataPaths(dataPaths); err != nil {
		return nil, err
	}
	unitDirectory := filepath.Join(manifest.TargetHome, ".config", "systemd", "user")
	wantsDirectory := filepath.Join(unitDirectory, "graphical-session.target.wants")
	stateDirectory := filepath.Join(manifest.TargetHome, ".local", "share", "pccontroller", "virtual-board")
	if operation == "remove" {
		return nil, removeCurrentUserRuntimeLinks(root, unitDirectory, wantsDirectory)
	}
	if operation != "ensure" {
		return nil, fmt.Errorf("unsupported runtime user-link operation %q", operation)
	}
	for _, directory := range []string{unitDirectory, wantsDirectory, stateDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, err
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("target runtime path %s is not a real directory", directory)
		}
	}
	var created []string
	for _, name := range runtimeUnitNames {
		unitPath := filepath.Join(unitDirectory, name)
		unitTarget := filepath.Join(root, "current", "systemd", "user", name)
		made, err := ensureExactSymlink(unitPath, unitTarget, 0)
		if err != nil {
			removeExactPaths(created)
			return nil, err
		}
		if made {
			created = append(created, unitPath)
		}
		wantPath := filepath.Join(wantsDirectory, name)
		made, err = ensureExactSymlink(wantPath, filepath.Join("..", name), 0)
		if err != nil {
			removeExactPaths(created)
			return nil, err
		}
		if made {
			created = append(created, wantPath)
		}
	}
	return created, nil
}

func userRuntimeLinksReady(root string, manifest RuntimeManifest) (bool, error) {
	unitDirectory := filepath.Join(manifest.TargetHome, ".config", "systemd", "user")
	wantsDirectory := filepath.Join(unitDirectory, "graphical-session.target.wants")
	for _, name := range runtimeUnitNames {
		expected := map[string]string{
			filepath.Join(unitDirectory, name):  filepath.Join(root, "current", "systemd", "user", name),
			filepath.Join(wantsDirectory, name): filepath.Join("..", name),
		}
		for path, target := range expected {
			actual, err := os.Readlink(path)
			if os.IsNotExist(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if actual != target {
				return false, nil
			}
		}
	}
	return true, nil
}

func removeCurrentUserRuntimeLinks(root, unitDirectory, wantsDirectory string) error {
	for _, name := range runtimeUnitNames {
		expected := map[string]string{
			filepath.Join(unitDirectory, name):  filepath.Join(root, "current", "systemd", "user", name),
			filepath.Join(wantsDirectory, name): filepath.Join("..", name),
		}
		for path, target := range expected {
			actual, err := os.Readlink(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("refusing to remove non-symlink user unit path %s", path)
			}
			if actual != target {
				return fmt.Errorf("refusing to remove user unit link %s with foreign target %q", path, actual)
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func manageTargetUserLinks(ctx context.Context, options InstallOptions, operation string) error {
	if os.Geteuid() != 0 && uint32(os.Geteuid()) == options.TargetUID {
		_, err := ManageCurrentUserLinks(options.Root, operation)
		return err
	}
	runuser, err := trustedRegularExecutable(defaultString(options.Runuser, "/usr/sbin/runuser"), "runuser")
	if err != nil {
		return err
	}
	controller, err := managedRuntimeExecutable(filepath.Join(options.Root, "bin", "controller"), "published Controller")
	if err != nil {
		return err
	}
	runner := options.Runner
	if runner == nil {
		runner = runBoundedCommand
	}
	arguments := []string{
		"--user", options.TargetUser, "--", "env",
		"HOME=" + filepath.Clean(options.TargetHome),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		controller, "toolchain", "runtime-user-links", "--root", options.Root, "--" + operation,
	}
	output, err := runner(ctx, runuser, arguments, []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"})
	if err != nil {
		return fmt.Errorf("target-user runtime links %s: %w: %s", operation, err, boundedText(output))
	}
	return nil
}

func activateUserRuntime(ctx context.Context, options InstallOptions, action string) (string, error) {
	availability := userManagerAvailability(options.TargetUID)
	if availability == "deferred-no-user-manager" {
		return availability, nil
	}
	runuser, err := trustedRegularExecutable(defaultString(options.Runuser, "/usr/sbin/runuser"), "runuser")
	if err != nil {
		return "", err
	}
	systemctl, err := trustedRegularExecutable(defaultString(options.Systemctl, "/usr/bin/systemctl"), "systemctl")
	if err != nil {
		return "", err
	}
	runner := options.Runner
	if runner == nil {
		runner = runBoundedCommand
	}
	runtimeDirectory := "/run/user/" + strconv.FormatUint(uint64(options.TargetUID), 10)
	environment := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	run := func(arguments ...string) (string, error) {
		userArguments := []string{
			"--user", options.TargetUser, "--", "env",
			"XDG_RUNTIME_DIR=" + runtimeDirectory,
			"DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(runtimeDirectory, "bus"),
			systemctl, "--user",
		}
		userArguments = append(userArguments, arguments...)
		output, err := runner(ctx, runuser, userArguments, environment)
		if err != nil {
			return output, fmt.Errorf("%s: %w: %s", strings.Join(arguments, " "), err, boundedText(output))
		}
		return output, nil
	}
	if _, err := run("daemon-reload"); err != nil {
		return "", err
	}
	switch action {
	case "restart":
		active, activeErr := run("is-active", "graphical-session.target")
		if activeErr != nil || strings.TrimSpace(active) != "active" {
			return "deferred-no-active-graphical-session", nil
		}
		environmentOutput, environmentErr := run("show-environment")
		if environmentErr != nil || !graphicalEnvironmentPresent(environmentOutput) {
			return "deferred-no-graphical-environment", nil
		}
		if _, err := run("stop", "pccontroller-window.service"); err != nil {
			return "", err
		}
		manifest, release, err := loadCurrentManifest(options.Root)
		if err != nil {
			return "", err
		}
		if _, err := run("restart", "pccontroller-virtual-board.service"); err != nil {
			return "", err
		}
		boardPID, err := runtimeVerifyUserUnit(ctx, run, "pccontroller-virtual-board.service", filepath.Join(release, "bin", "virtual-board"))
		if err != nil {
			return "", err
		}
		if err := runtimeWaitForPIDListener(ctx, boardPID, "127.0.0.1:8765"); err != nil {
			return "", err
		}
		if err := reverifyUnitPID(ctx, run, "pccontroller-virtual-board.service", filepath.Join(release, "bin", "virtual-board"), boardPID); err != nil {
			return "", err
		}
		if _, err := run("restart", "pccontroller-controller.service"); err != nil {
			return "", err
		}
		controllerPID, err := runtimeVerifyUserUnit(ctx, run, "pccontroller-controller.service", filepath.Join(release, "bin", "controller"))
		if err != nil {
			return "", err
		}
		if err := runtimeWaitForHealth(ctx); err != nil {
			return "", err
		}
		if err := runPublishedReadiness(ctx, options, runner, runuser); err != nil {
			return "", err
		}
		if err := reverifyUnitPID(ctx, run, "pccontroller-virtual-board.service", filepath.Join(release, "bin", "virtual-board"), boardPID); err != nil {
			return "", err
		}
		if err := runtimeWaitForPIDListener(ctx, boardPID, "127.0.0.1:8765"); err != nil {
			return "", err
		}
		if err := reverifyUnitPID(ctx, run, "pccontroller-controller.service", filepath.Join(release, "bin", "controller"), controllerPID); err != nil {
			return "", err
		}
		if err := runtimeWaitForPIDListener(ctx, controllerPID, "127.0.0.1:8787"); err != nil {
			return "", err
		}
		if err := reverifyUnitPID(ctx, run, "pccontroller-controller.service", filepath.Join(release, "bin", "controller"), controllerPID); err != nil {
			return "", err
		}
		browserProcess, err := browserMainExecutable(manifest.Browser)
		if err != nil {
			return "", err
		}
		if _, err := run("restart", "pccontroller-window.service"); err != nil {
			return "", err
		}
		if _, err := runtimeVerifyUserUnit(ctx, run, "pccontroller-window.service", browserProcess); err != nil {
			return "", err
		}
	case "stop":
		if _, err := run(append([]string{"stop"}, reverseStrings(runtimeUnitNames)...)...); err != nil {
			return "", err
		}
	case "reload":
		return "reloaded", nil
	default:
		return "", fmt.Errorf("unsupported user runtime activation %q", action)
	}
	if action == "restart" {
		return "activated-ready", nil
	}
	return "stopped", nil
}

type userSystemctlRunner func(arguments ...string) (string, error)

var runtimeVerifyUserUnit = verifyUserUnit
var runtimePIDOwnsTCPListener = pidOwnsTCPListener
var runtimeWaitForPIDListener = waitForPIDListener
var runtimeWaitForHealth = waitForRuntimeHealth
var runtimeReadProcessExecutable = os.Readlink
var runtimeUserUnitReadyTimeout = 30 * time.Second
var runtimeUserUnitPollInterval = 250 * time.Millisecond

func graphicalEnvironmentPresent(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "DISPLAY=") && strings.TrimSpace(strings.TrimPrefix(line, "DISPLAY=")) != "" {
			return true
		}
		if strings.HasPrefix(line, "WAYLAND_DISPLAY=") && strings.TrimSpace(strings.TrimPrefix(line, "WAYLAND_DISPLAY=")) != "" {
			return true
		}
	}
	return false
}

func verifyUserUnit(ctx context.Context, run userSystemctlRunner, unit, expectedExecutable string) (int, error) {
	deadline := time.NewTimer(runtimeUserUnitReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(runtimeUserUnitPollInterval)
	defer ticker.Stop()
	expected := ""
	if expectedExecutable != "" {
		resolved, err := filepath.EvalSymlinks(expectedExecutable)
		if err != nil {
			return 0, fmt.Errorf("resolve expected executable for unit %s: %w", unit, err)
		}
		expected = filepath.Clean(resolved)
	}
	var last string
	stablePID := 0
	stableSamples := 0
	for {
		output, err := run("show", "--property=ActiveState", "--property=MainPID", "--value", unit)
		if err == nil {
			state, pid := parseUserUnitShow(output)
			if state == "active" && pid > 0 {
				actual := ""
				var readErr error
				if expected != "" {
					actual, readErr = runtimeReadProcessExecutable(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
				}
				if readErr == nil && (expected == "" || filepath.Clean(actual) == expected) {
					if stablePID == pid {
						stableSamples++
					} else {
						stablePID = pid
						stableSamples = 1
					}
					if stableSamples >= 2 {
						return pid, nil
					}
					last = fmt.Sprintf("MainPID %d has the expected executable but is awaiting a stable sample", pid)
				} else {
					stablePID = 0
					stableSamples = 0
					switch {
					case readErr != nil:
						last = fmt.Sprintf("read MainPID %d executable: %v", pid, readErr)
					case filepath.Base(filepath.Clean(actual)) == "systemd-executor":
						last = fmt.Sprintf("MainPID %d is still transitioning through %q", pid, actual)
					default:
						last = fmt.Sprintf("MainPID %d executable is %q, want %q", pid, actual, expected)
					}
				}
			} else {
				stablePID = 0
				stableSamples = 0
				last = fmt.Sprintf("unit state is %q with MainPID %d", state, pid)
			}
		}
		if err != nil {
			stablePID = 0
			stableSamples = 0
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline.C:
			return 0, fmt.Errorf("unit %s did not become active with the published executable: %s", unit, last)
		case <-ticker.C:
		}
	}
}

func parseUserUnitShow(output string) (string, int) {
	state := ""
	pid := 0
	for _, field := range strings.Fields(output) {
		value := field
		if strings.HasPrefix(field, "ActiveState=") {
			value = strings.TrimPrefix(field, "ActiveState=")
		}
		switch value {
		case "active", "activating", "deactivating", "failed", "inactive", "reloading", "maintenance":
			state = value
			continue
		}
		if strings.HasPrefix(field, "MainPID=") {
			value = strings.TrimPrefix(field, "MainPID=")
		}
		if parsed, err := strconv.ParseInt(value, 10, 32); err == nil && parsed >= 0 {
			pid = int(parsed)
		}
	}
	return state, pid
}

func reverifyUnitPID(ctx context.Context, run userSystemctlRunner, unit, executable string, expectedPID int) error {
	actualPID, err := runtimeVerifyUserUnit(ctx, run, unit, executable)
	if err != nil {
		return err
	}
	if actualPID != expectedPID {
		return fmt.Errorf("unit %s MainPID changed from %d to %d during readiness verification", unit, expectedPID, actualPID)
	}
	return nil
}

func waitForPIDListener(ctx context.Context, pid int, address string) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		if err := runtimePIDOwnsTCPListener(pid, address); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("MainPID %d did not own listener %s: %w", pid, address, last)
		case <-ticker.C:
		}
	}
}

func pidOwnsTCPListener(pid int, address string) error {
	content, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return fmt.Errorf("inspect loopback listener: %w", err)
	}
	inodes, err := parseTCPListenerInodes(content, address)
	if err != nil {
		return err
	}
	if len(inodes) == 0 {
		return fmt.Errorf("no listening %s socket exists", address)
	}
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return fmt.Errorf("inspect MainPID file descriptors: %w", err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if err == nil && strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") && inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] {
			return nil
		}
	}
	return fmt.Errorf("listener %s is not owned by MainPID %d", address, pid)
}

func parseTCPListenerInodes(content []byte, address string) (map[string]bool, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, fmt.Errorf("unsupported loopback listener address %q", address)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("invalid loopback listener port %q", portText)
	}
	localAddress := fmt.Sprintf("0100007F:%04X", port)
	inodes := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 9 && fields[1] == localAddress && fields[3] == "0A" {
			inodes[fields[9]] = true
		}
	}
	return inodes, nil
}

func waitForRuntimeHealth(ctx context.Context) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8787/healthz", nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Controller /healthz did not become ready on 127.0.0.1:8787")
		case <-ticker.C:
		}
	}
}

func runPublishedReadiness(ctx context.Context, options InstallOptions, runner CommandRunner, runuser string) error {
	controller, err := managedRuntimeExecutable(filepath.Join(options.Root, "bin", "controller"), "published Controller")
	if err != nil {
		return err
	}
	arguments := []string{
		"--user", options.TargetUser, "--", "env",
		"HOME=" + filepath.Clean(options.TargetHome),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		controller, "toolchain", "runtime-window-ready", "--timeout", "45s",
	}
	output, err := runner(ctx, runuser, arguments, []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"})
	if err != nil {
		return fmt.Errorf("authenticated Controller/VirtualBoard readiness failed: %w: %s", err, boundedText(output))
	}
	return nil
}

func userManagerAvailability(uid uint32) string {
	if !runtimeUserManagerBusPresent(uid) {
		return "deferred-no-user-manager"
	}
	return "available"
}

var runtimeUserManagerBusPresent = func(uid uint32) bool {
	bus := filepath.Join("/run/user", strconv.FormatUint(uint64(uid), 10), "bus")
	info, err := os.Lstat(bus)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

func runBoundedCommand(ctx context.Context, name string, arguments, environment []string) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	output := &boundedBuffer{limit: maximumSmokeOutput}
	command := exec.CommandContext(commandContext, name, arguments...)
	command.Env = append([]string(nil), environment...)
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		return output.String(), errors.New("runtime command exceeded the 120-second safety deadline")
	}
	return output.String(), err
}

func ensureDirectory(path string, mode os.FileMode, requireRoot bool) error {
	info, err := os.Lstat(path)
	created := false
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, mode); err != nil {
			return err
		}
		created = true
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed path %s is not a real directory", path)
	}
	if created {
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		info, err = os.Lstat(path)
		if err != nil {
			return err
		}
	}
	if requireRoot && os.Geteuid() == 0 {
		owner, err := fileUID(info)
		if err != nil {
			return err
		}
		if owner != 0 {
			return fmt.Errorf("managed directory %s is not root-owned", path)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("managed directory %s is group/world writable", path)
		}
	}
	return nil
}

func ensureManagedDirectory(path string, mode os.FileMode, requireRoot bool) error {
	if err := ensureDirectory(path, mode, requireRoot); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("managed directory %s does not have required mode %04o", path, mode.Perm())
	}
	return nil
}

func ensureExactSymlink(path, target string, owner uint32) (bool, error) {
	actual, err := os.Readlink(path)
	if err == nil {
		if actual != target {
			return false, fmt.Errorf("refusing to replace foreign symlink %s -> %s", path, actual)
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("refusing to replace non-symlink path %s", path)
	}
	if err := os.Symlink(target, path); err != nil {
		return false, err
	}
	if owner != 0 {
		if err := os.Lchown(path, int(owner), -1); err != nil {
			_ = os.Remove(path)
			return false, err
		}
	}
	return true, nil
}

func replaceSymlink(path, target string, owner uint32) error {
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+strconv.Itoa(os.Getpid()))
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	if owner != 0 {
		if err := os.Lchown(temporary, int(owner), -1); err != nil {
			return err
		}
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func copyVerifiedExecutable(input *os.File, source FileIdentity, destination string) error {
	if input == nil {
		return fmt.Errorf("pinned %s descriptor is unavailable", source.Role)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if err := output.Chmod(0o755); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	copyErr := func() error {
		written, err := io.Copy(output, io.LimitReader(input, maximumBinaryBytes+1))
		if err != nil {
			return err
		}
		if written != source.Bytes {
			return fmt.Errorf("source %s changed size while being copied", source.Role)
		}
		if err := output.Sync(); err != nil {
			return err
		}
		return output.Close()
	}()
	if copyErr != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return copyErr
	}
	digest, err := sha256File(destination, maximumBinaryBytes)
	if err != nil || !strings.EqualFold(digest, source.SHA256) {
		_ = os.Remove(destination)
		if err != nil {
			return err
		}
		return fmt.Errorf("source %s changed hash while being copied", source.Role)
	}
	return nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) (resultErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pccontroller-runtime-")
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
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sha256Bytes(content []byte) string {
	digest := sha256.Sum256(content)
	return fmt.Sprintf("%x", digest[:])
}

func fileUID(info os.FileInfo) (uint32, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("Linux ownership metadata is unavailable")
	}
	return status.Uid, nil
}

func removeExactPaths(paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		_ = os.Remove(paths[index])
	}
}

func reverseStrings(values []string) []string {
	result := append([]string(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
