package installer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

const (
	ownerMarkerName         = "installation-owner.json"
	installationStateName   = "installation-state.json"
	transactionName         = ".installation-transaction.json"
	lockName                = ".installation.lock"
	packagesDirectory       = "packages"
	stagingDirectory        = ".staging"
	ownerMarkerFormat       = "pccontroller-installation-owner/v1"
	installationStateFormat = "pccontroller-installation-state/v1"
	transactionFormat       = "pccontroller-installation-transaction/v1"
	// PurgeConfirmation is deliberately long and distinct from uninstall.
	// Both --purge-data and this exact value are required.
	PurgeConfirmation = "PURGE-PC-CONTROLLER-USER-DATA"
)

var (
	ErrUnsupportedPlatform       = errors.New("unsupported platform")
	ErrOwnershipMismatch         = errors.New("installation root is not owned by this user and product")
	ErrDesktopAdapterUnavailable = errors.New("native desktop integration adapter is unavailable")
	ErrExternalCleanupRequired   = errors.New("uninstall must continue from a helper outside the installation root")
)

type DesktopTarget struct {
	AppID       string
	DisplayName string
	Executable  string
}

// DesktopIntegrator deliberately isolates lifecycle storage from the native
// URI/AUMID/shortcut adapter. Implementations must be direct platform APIs;
// the installer never shells through PowerShell.
type DesktopIntegrator interface {
	Ensure(context.Context, DesktopTarget) error
	RemoveOwned(context.Context, DesktopTarget) error
}

type ownerMarker struct {
	Format       string    `json:"format"`
	ProductAppID string    `json:"product_app_id"`
	OwnerID      string    `json:"owner_id"`
	InstallRoot  string    `json:"install_root"`
	CreatedAt    time.Time `json:"created_at"`
}

type InstallationState struct {
	Format         string    `json:"format"`
	ProductAppID   string    `json:"product_app_id"`
	OwnerID        string    `json:"owner_id"`
	ActiveSlot     string    `json:"active_slot"`
	ActiveSHA256   string    `json:"active_sha256"`
	PreviousSlot   string    `json:"previous_slot,omitempty"`
	PreviousSHA256 string    `json:"previous_sha256,omitempty"`
	Version        string    `json:"version"`
	SourceSHA256   string    `json:"source_sha256"`
	Executable     string    `json:"executable"`
	DisplayName    string    `json:"display_name"`
	DesktopManaged bool      `json:"desktop_managed"`
	InstalledAt    time.Time `json:"installed_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type transactionJournal struct {
	Format        string             `json:"format"`
	ID            string             `json:"id"`
	Operation     string             `json:"operation"`
	Phase         string             `json:"phase"`
	Stage         string             `json:"stage,omitempty"`
	NewSlot       string             `json:"new_slot,omitempty"`
	NewSHA256     string             `json:"new_sha256,omitempty"`
	PreviousState *InstallationState `json:"previous_state,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type Service struct {
	Platform          string
	Architecture      string
	OwnerID           string
	CurrentExecutable string
	DisplayName       string
	Desktop           DesktopIntegrator
	Now               func() time.Time
	VerifyExecutable  packageVerifier
}

type ChangeRequest struct {
	Root                  string
	PackageRoot           string
	ExpectedPackageSHA256 string
	ConfigureDesktop      bool
}

type UninstallRequest struct {
	Root              string
	PurgeData         bool
	PurgeConfirmation string
	PurgePaths        []string
}

type LifecycleResult struct {
	Action         string             `json:"action"`
	Changed        bool               `json:"changed"`
	Healthy        bool               `json:"healthy"`
	Root           string             `json:"root"`
	Executable     string             `json:"executable,omitempty"`
	PackageSHA256  string             `json:"package_sha256,omitempty"`
	Version        string             `json:"version,omitempty"`
	DesktopManaged bool               `json:"desktop_managed"`
	DataPreserved  bool               `json:"data_preserved"`
	PurgedPaths    []string           `json:"purged_paths,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
	State          *InstallationState `json:"state,omitempty"`
}

func NewService() (*Service, error) {
	owner, err := platformOwnerID()
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	return &Service{
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		OwnerID: owner, CurrentExecutable: executable,
		DisplayName: productidentity.DefaultTitle, Now: time.Now,
	}, nil
}

func DefaultInstallRoot() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("%w: per-user installation is available only on Windows", ErrUnsupportedPlatform)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate per-user local application data: %w", err)
	}
	return filepath.Join(base, "Programs", productidentity.ConfigDirectory), nil
}

// RunningFromRoot reports whether uninstall needs the external native helper.
func (service *Service) RunningFromRoot(root string) (bool, error) {
	resolved, err := safeInstallRoot(root)
	if err != nil {
		return false, err
	}
	current, err := filepath.Abs(service.CurrentExecutable)
	if err != nil {
		return false, err
	}
	return pathWithin(resolved, current), nil
}

func (service *Service) Install(ctx context.Context, request ChangeRequest) (LifecycleResult, error) {
	return service.activate(ctx, "install", request, false)
}

func (service *Service) Repair(ctx context.Context, request ChangeRequest) (LifecycleResult, error) {
	return service.activate(ctx, "repair", request, true)
}

func (service *Service) Status(ctx context.Context, root string) (LifecycleResult, error) {
	if err := service.validate(); err != nil {
		return LifecycleResult{}, err
	}
	resolved, err := safeInstallRoot(root)
	if err != nil {
		return LifecycleResult{}, err
	}
	result := LifecycleResult{Action: "status", Root: resolved, DataPreserved: true}
	if _, err := os.Lstat(resolved); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	if err := service.checkOwnership(resolved, false); err != nil {
		return result, err
	}
	lock, err := acquireLifecycleLock(ctx, filepath.Join(resolved, lockName))
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err := service.recover(ctx, resolved); err != nil {
		return result, err
	}
	state, exists, err := loadState(resolved)
	if err != nil || !exists {
		return result, err
	}
	if state.OwnerID != service.OwnerID {
		return result, ErrOwnershipMismatch
	}
	result.State = &state
	result.DesktopManaged = state.DesktopManaged
	result.PackageSHA256 = state.ActiveSHA256
	result.Version = state.Version
	result.Executable = filepath.Join(resolved, filepath.FromSlash(state.Executable))
	if err := service.verifySlot(resolved, state.ActiveSlot, state.ActiveSHA256); err != nil {
		result.Warnings = append(result.Warnings, err.Error())
		return result, nil
	}
	result.Healthy = true
	return result, nil
}

func (service *Service) activate(ctx context.Context, operation string, request ChangeRequest, repair bool) (LifecycleResult, error) {
	if err := service.validate(); err != nil {
		return LifecycleResult{}, err
	}
	root, err := safeInstallRoot(request.Root)
	if err != nil {
		return LifecycleResult{}, err
	}
	result := LifecycleResult{Action: operation, Root: root, DataPreserved: true}
	manifest, err := VerifyPackage(request.PackageRoot, request.ExpectedPackageSHA256, ManifestOptions{
		Platform: service.Platform, Architecture: service.Architecture,
		VerifyExecutable: service.VerifyExecutable,
	})
	if err != nil {
		return result, err
	}
	packageRoot, err := secureDirectory(request.PackageRoot)
	if err != nil {
		return result, err
	}
	result.PackageSHA256, result.Version = manifest.RootSHA256, manifest.Version
	if err := service.checkOwnership(root, true); err != nil {
		return result, err
	}
	lock, err := acquireLifecycleLock(ctx, filepath.Join(root, lockName))
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err := service.checkOwnership(root, false); err != nil {
		return result, err
	}
	if err := service.recover(ctx, root); err != nil {
		return result, err
	}
	previous, exists, err := loadState(root)
	if err != nil {
		return result, err
	}
	if exists && previous.OwnerID != service.OwnerID {
		return result, ErrOwnershipMismatch
	}
	if repair && !exists {
		return result, errors.New("repair requires an existing owned installation; use install first")
	}
	desktopManaged := request.ConfigureDesktop
	if exists && previous.DesktopManaged {
		desktopManaged = true
	}
	if desktopManaged && service.Desktop == nil {
		return result, ErrDesktopAdapterUnavailable
	}
	if exists && strings.EqualFold(previous.ActiveSHA256, manifest.RootSHA256) {
		if verifyErr := service.verifySlot(root, previous.ActiveSlot, previous.ActiveSHA256); verifyErr == nil {
			if desktopManaged {
				if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, previous)); err != nil {
					return result, fmt.Errorf("repair native desktop integration: %w", err)
				}
			}
			if previous.DesktopManaged != desktopManaged || strings.TrimSpace(previous.DisplayName) == "" {
				previous.DesktopManaged = desktopManaged
				if strings.TrimSpace(previous.DisplayName) == "" {
					previous.DisplayName = service.DisplayName
				}
				previous.UpdatedAt = service.now()
				if err := writeJSONAtomic(filepath.Join(root, installationStateName), previous, 0o600); err != nil {
					return result, fmt.Errorf("persist desktop integration state: %w", err)
				}
				result.Changed = true
			}
			result.Healthy, result.DesktopManaged, result.Executable = true, desktopManaged, filepath.Join(root, filepath.FromSlash(previous.Executable))
			result.State = &previous
			return result, nil
		} else if !repair {
			result.Warnings = append(result.Warnings, "matching installed package was damaged and has been replaced from the verified source")
		}
	}

	id, err := transactionID()
	if err != nil {
		return result, err
	}
	stageRelative := filepath.ToSlash(filepath.Join(stagingDirectory, id))
	journal := transactionJournal{
		Format: transactionFormat, ID: id, Operation: operation, Phase: "staging",
		Stage: stageRelative, NewSHA256: manifest.RootSHA256, UpdatedAt: service.now(),
	}
	if exists {
		copy := previous
		journal.PreviousState = &copy
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	stage := filepath.Join(root, filepath.FromSlash(stageRelative))
	if err := service.stagePackage(packageRoot, stage, manifest); err != nil {
		_ = os.RemoveAll(stage)
		_ = os.Remove(filepath.Join(root, transactionName))
		return result, err
	}

	slotRelative := filepath.ToSlash(filepath.Join(packagesDirectory, manifest.RootSHA256))
	slot := filepath.Join(root, filepath.FromSlash(slotRelative))
	if err := os.MkdirAll(filepath.Dir(slot), 0o700); err != nil {
		return result, err
	}
	if _, statErr := os.Lstat(slot); statErr == nil {
		if verifyErr := service.verifySlot(root, slotRelative, manifest.RootSHA256); verifyErr == nil {
			_ = os.RemoveAll(stage)
		} else {
			slotRelative = filepath.ToSlash(filepath.Join(packagesDirectory, manifest.RootSHA256+"-repair-"+id))
			slot = filepath.Join(root, filepath.FromSlash(slotRelative))
			if err := os.Rename(stage, slot); err != nil {
				return result, fmt.Errorf("publish repaired package slot: %w", err)
			}
		}
	} else if os.IsNotExist(statErr) {
		if err := os.Rename(stage, slot); err != nil {
			return result, fmt.Errorf("publish package slot: %w", err)
		}
	} else {
		return result, statErr
	}
	journal.Phase, journal.NewSlot, journal.UpdatedAt = "slot-ready", slotRelative, service.now()
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	if err := service.verifySlot(root, slotRelative, manifest.RootSHA256); err != nil {
		return result, fmt.Errorf("verify published package slot: %w", err)
	}

	now := service.now()
	next := InstallationState{
		Format: installationStateFormat, ProductAppID: productidentity.StableAppID,
		OwnerID: service.OwnerID, ActiveSlot: slotRelative, ActiveSHA256: manifest.RootSHA256,
		Version: manifest.Version, SourceSHA256: manifest.SourceSHA256,
		Executable:     filepath.ToSlash(filepath.Join(slotRelative, filepath.FromSlash(manifest.ExecutablePath))),
		DisplayName:    service.DisplayName,
		DesktopManaged: desktopManaged, InstalledAt: now, UpdatedAt: now,
	}
	if exists {
		next.InstalledAt = previous.InstalledAt
		if previous.DesktopManaged && strings.TrimSpace(previous.DisplayName) != "" {
			next.DisplayName = previous.DisplayName
		}
		if previous.ActiveSlot != slotRelative {
			next.PreviousSlot, next.PreviousSHA256 = previous.ActiveSlot, previous.ActiveSHA256
		} else {
			next.PreviousSlot, next.PreviousSHA256 = previous.PreviousSlot, previous.PreviousSHA256
		}
	}
	if err := writeJSONAtomic(filepath.Join(root, installationStateName), next, 0o600); err != nil {
		return result, err
	}
	journal.Phase, journal.UpdatedAt = "activated", service.now()
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	if desktopManaged {
		if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, next)); err != nil {
			rollbackErr := service.rollbackActivation(ctx, root, journal)
			return result, errors.Join(fmt.Errorf("activate native desktop integration: %w", err), rollbackErr)
		}
	}
	if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("commit installation transaction: %w", err)
	}
	result.Changed, result.Healthy, result.DesktopManaged = true, true, desktopManaged
	result.Executable = filepath.Join(root, filepath.FromSlash(next.Executable))
	result.State = &next
	result.Warnings = append(result.Warnings, prunePackageSlots(root, next)...)
	return result, nil
}

func (service *Service) Uninstall(ctx context.Context, request UninstallRequest) (LifecycleResult, error) {
	if err := service.validate(); err != nil {
		return LifecycleResult{}, err
	}
	root, err := safeInstallRoot(request.Root)
	if err != nil {
		return LifecycleResult{}, err
	}
	result := LifecycleResult{Action: "uninstall", Root: root, DataPreserved: !request.PurgeData}
	if request.PurgeData {
		if request.PurgeConfirmation != PurgeConfirmation {
			return result, fmt.Errorf("data purge requires the exact separate confirmation %q", PurgeConfirmation)
		}
		for _, path := range request.PurgePaths {
			if _, err := safePurgePath(path, root); err != nil {
				return result, err
			}
		}
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		if request.PurgeData {
			if err := purgePaths(request.PurgePaths, root, &result); err != nil {
				return result, err
			}
		}
		return result, nil
	} else if err != nil {
		return result, err
	}
	if err := service.checkOwnership(root, false); err != nil {
		return result, err
	}
	lock, err := acquireLifecycleLock(ctx, filepath.Join(root, lockName))
	if err != nil {
		return result, err
	}
	locked := true
	defer func() {
		if locked {
			_ = lock.Close()
		}
	}()
	if err := service.recover(ctx, root); err != nil {
		return result, err
	}
	state, exists, err := loadState(root)
	if err != nil {
		return result, err
	}
	if exists && state.OwnerID != service.OwnerID {
		return result, ErrOwnershipMismatch
	}
	if exists && state.DesktopManaged {
		if service.Desktop == nil {
			return result, ErrDesktopAdapterUnavailable
		}
		if err := service.Desktop.RemoveOwned(ctx, service.desktopTarget(root, state)); err != nil {
			return result, fmt.Errorf("remove owned native desktop integration: %w", err)
		}
	}
	journal := transactionJournal{
		Format: transactionFormat, ID: "uninstall", Operation: "uninstall",
		Phase: "uninstalling", UpdatedAt: service.now(),
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	if err := lock.Close(); err != nil {
		return result, err
	}
	locked = false
	if pathWithin(root, service.CurrentExecutable) {
		return result, ErrExternalCleanupRequired
	}
	tombstone := root + ".remove-" + fmt.Sprint(service.now().UnixNano())
	if err := os.Rename(root, tombstone); err != nil {
		return result, fmt.Errorf("detach installation for removal: %w", err)
	}
	result.Changed = true
	if err := os.RemoveAll(tombstone); err != nil {
		result.Warnings = append(result.Warnings, "detached installation remains queued for cleanup: "+tombstone)
	}
	if request.PurgeData {
		if err := purgePaths(request.PurgePaths, root, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (service *Service) validate() error {
	if service.Platform == "" {
		service.Platform = runtime.GOOS
	}
	if service.Architecture == "" {
		service.Architecture = runtime.GOARCH
	}
	if service.Platform != "windows" {
		return fmt.Errorf("%w: per-user installation is available only on Windows", ErrUnsupportedPlatform)
	}
	if strings.TrimSpace(service.OwnerID) == "" {
		return errors.New("installation owner identity is unavailable")
	}
	if strings.TrimSpace(service.DisplayName) == "" {
		service.DisplayName = productidentity.DefaultTitle
	}
	if service.Now == nil {
		service.Now = time.Now
	}
	return nil
}

func (service *Service) now() time.Time { return service.Now().UTC() }

func (service *Service) desktopTarget(root string, state InstallationState) DesktopTarget {
	displayName := strings.TrimSpace(state.DisplayName)
	if displayName == "" {
		displayName = service.DisplayName
	}
	return DesktopTarget{
		AppID: productidentity.StableAppID, DisplayName: displayName,
		Executable: filepath.Join(root, filepath.FromSlash(state.Executable)),
	}
}

func (service *Service) checkOwnership(root string, create bool) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) && create {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("installation root must be a real directory, not a link")
	}
	path := filepath.Join(root, ownerMarkerName)
	content, err := readBoundedRegularFile(path, 64<<10)
	if os.IsNotExist(err) && create {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if entry.Name() != lockName {
				return fmt.Errorf("%w: non-empty root has no ownership marker", ErrOwnershipMismatch)
			}
		}
		marker := ownerMarker{
			Format: ownerMarkerFormat, ProductAppID: productidentity.StableAppID,
			OwnerID: service.OwnerID, InstallRoot: root, CreatedAt: service.now(),
		}
		return writeJSONAtomic(path, marker, 0o600)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return ErrOwnershipMismatch
		}
		return err
	}
	var marker ownerMarker
	if err := decodeStrictJSON(content, &marker); err != nil {
		return fmt.Errorf("%w: invalid ownership marker: %v", ErrOwnershipMismatch, err)
	}
	if marker.Format != ownerMarkerFormat || marker.ProductAppID != productidentity.StableAppID || marker.OwnerID != service.OwnerID || !samePath(marker.InstallRoot, root) {
		return ErrOwnershipMismatch
	}
	return nil
}

func (service *Service) stagePackage(source, stage string, manifest PackageManifest) error {
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	for _, entry := range manifest.Files {
		sourcePath, err := inventoryEntryPath(source, entry.Path)
		if err != nil {
			return err
		}
		targetPath, err := inventoryEntryPath(stage, entry.Path)
		if err != nil {
			return err
		}
		if err := copyVerifiedFile(sourcePath, targetPath, entry); err != nil {
			return fmt.Errorf("stage %s: %w", entry.Path, err)
		}
	}
	manifestContent, err := readBoundedRegularFile(filepath.Join(source, PackageManifestName), maximumManifestBytes)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(stage, PackageManifestName), manifestContent, 0o644); err != nil {
		return err
	}
	_, err = VerifyPackage(stage, manifest.RootSHA256, ManifestOptions{
		Platform: service.Platform, Architecture: service.Architecture,
		VerifyExecutable: service.VerifyExecutable,
	})
	return err
}

func (service *Service) verifySlot(root, relative, digest string) error {
	path, err := inventoryEntryPath(root, relative)
	if err != nil {
		return err
	}
	manifest, err := VerifyPackage(path, digest, ManifestOptions{
		Platform: service.Platform, Architecture: service.Architecture,
		VerifyExecutable: service.VerifyExecutable,
	})
	if err != nil {
		return err
	}
	if !strings.EqualFold(manifest.RootSHA256, digest) {
		return errors.New("installed slot identity differs from state")
	}
	return nil
}

func (service *Service) recover(ctx context.Context, root string) error {
	content, err := readBoundedRegularFile(filepath.Join(root, transactionName), 256<<10)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal transactionJournal
	if err := decodeStrictJSON(content, &journal); err != nil || journal.Format != transactionFormat {
		return fmt.Errorf("installation recovery journal is invalid: %w", err)
	}
	switch journal.Phase {
	case "staging":
		if journal.Stage != "" {
			stage, pathErr := inventoryEntryPath(root, journal.Stage)
			if pathErr != nil {
				return pathErr
			}
			_ = os.RemoveAll(stage)
		}
	case "slot-ready":
		state, exists, stateErr := loadState(root)
		if stateErr != nil {
			return stateErr
		}
		activated := exists && state.ActiveSlot == journal.NewSlot &&
			strings.EqualFold(state.ActiveSHA256, journal.NewSHA256)
		if !activated {
			if journal.NewSlot != "" {
				slot, pathErr := inventoryEntryPath(root, journal.NewSlot)
				if pathErr != nil {
					return pathErr
				}
				_ = os.RemoveAll(slot)
			}
		} else {
			if err := service.verifySlot(root, state.ActiveSlot, state.ActiveSHA256); err != nil {
				return fmt.Errorf("recover published installation slot: %w", err)
			}
			if state.DesktopManaged {
				if service.Desktop == nil {
					return ErrDesktopAdapterUnavailable
				}
				if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, state)); err != nil {
					return fmt.Errorf("recover native desktop integration: %w", err)
				}
			}
		}
	case "activated":
		state, exists, stateErr := loadState(root)
		if stateErr != nil || !exists || state.ActiveSlot != journal.NewSlot || !strings.EqualFold(state.ActiveSHA256, journal.NewSHA256) {
			return errors.New("activated installation transaction does not match durable state")
		}
		if state.DesktopManaged {
			if service.Desktop == nil {
				return ErrDesktopAdapterUnavailable
			}
			if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, state)); err != nil {
				return err
			}
		}
	case "uninstalling":
		return errors.New("an interrupted uninstall must be retried from an external package copy")
	default:
		return fmt.Errorf("installation recovery journal phase %q is unsupported", journal.Phase)
	}
	if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (service *Service) rollbackActivation(ctx context.Context, root string, journal transactionJournal) error {
	var result error
	activated, activatedExists, activatedErr := loadState(root)
	if activatedErr != nil {
		result = errors.Join(result, activatedErr)
	} else if activatedExists && activated.DesktopManaged && service.Desktop != nil {
		result = errors.Join(
			result,
			service.Desktop.RemoveOwned(ctx, service.desktopTarget(root, activated)),
		)
	}
	if journal.PreviousState == nil {
		if err := os.Remove(filepath.Join(root, installationStateName)); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	} else {
		if err := writeJSONAtomic(filepath.Join(root, installationStateName), *journal.PreviousState, 0o600); err != nil {
			result = errors.Join(result, err)
		} else if journal.PreviousState.DesktopManaged && service.Desktop != nil {
			result = errors.Join(result, service.Desktop.Ensure(ctx, service.desktopTarget(root, *journal.PreviousState)))
		}
	}
	if journal.NewSlot != "" && (journal.PreviousState == nil || journal.PreviousState.ActiveSlot != journal.NewSlot) {
		if slot, err := inventoryEntryPath(root, journal.NewSlot); err == nil {
			_ = os.RemoveAll(slot)
		}
	}
	if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !os.IsNotExist(err) {
		result = errors.Join(result, err)
	}
	return result
}

func loadState(root string) (InstallationState, bool, error) {
	content, err := readBoundedRegularFile(filepath.Join(root, installationStateName), 256<<10)
	if os.IsNotExist(err) {
		return InstallationState{}, false, nil
	}
	if err != nil {
		return InstallationState{}, false, err
	}
	var state InstallationState
	if err := decodeStrictJSON(content, &state); err != nil {
		return InstallationState{}, false, err
	}
	if state.Format != installationStateFormat || state.ProductAppID != productidentity.StableAppID || strings.TrimSpace(state.OwnerID) == "" {
		return InstallationState{}, false, errors.New("installation state identity is invalid")
	}
	if strings.TrimSpace(state.DisplayName) == "" {
		return InstallationState{}, false, errors.New("installation display name is missing")
	}
	if _, err := normalizeInventoryPath(state.ActiveSlot); err != nil {
		return InstallationState{}, false, err
	}
	if _, err := normalizeInventoryPath(state.Executable); err != nil {
		return InstallationState{}, false, err
	}
	activePrefix := strings.TrimSuffix(filepath.ToSlash(state.ActiveSlot), "/") + "/"
	if !strings.HasPrefix(strings.ToLower(filepath.ToSlash(state.Executable)), strings.ToLower(activePrefix)) {
		return InstallationState{}, false, errors.New("installation executable is outside the active package slot")
	}
	if _, err := normalizeDigest(state.ActiveSHA256); err != nil {
		return InstallationState{}, false, err
	}
	return state, true, nil
}

func copyVerifiedFile(source, target string, entry PackageFile) error {
	input, info, err := openRegularFile(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if info.Size() != entry.Bytes {
		return errors.New("source file type or size differs from inventory")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".install-copy-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), input)
	if copyErr != nil {
		return copyErr
	}
	if written != entry.Bytes || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), entry.SHA256) {
		return errors.New("source changed while copying")
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, target); err != nil {
		return err
	}
	cleanup = false
	return verifyRegularFile(target, entry.Bytes, entry.SHA256)
}

func transactionID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func prunePackageSlots(root string, state InstallationState) []string {
	directory := filepath.Join(root, packagesDirectory)
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []string{"unable to inspect superseded packages: " + err.Error()}
	}
	keep := map[string]bool{
		filepath.Base(filepath.FromSlash(state.ActiveSlot)): true,
	}
	if state.PreviousSlot != "" {
		keep[filepath.Base(filepath.FromSlash(state.PreviousSlot))] = true
	}
	var warnings []string
	for _, entry := range entries {
		if keep[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			warnings = append(warnings, "superseded package remains for later cleanup: "+entry.Name())
		}
	}
	sort.Strings(warnings)
	return warnings
}

func purgePaths(paths []string, installRoot string, result *LifecycleResult) error {
	for _, value := range paths {
		path, err := safePurgePath(value, installRoot)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("purge user data %s: %w", path, err)
		}
		result.PurgedPaths = append(result.PurgedPaths, path)
	}
	return nil
}

func safePurgePath(value, installRoot string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(value) == "" {
		return "", errors.New("purge path must be an absolute product data directory")
	}
	path = filepath.Clean(path)
	if samePath(path, filepath.VolumeName(path)+string(os.PathSeparator)) || samePath(path, installRoot) || pathWithin(path, installRoot) || pathWithin(installRoot, path) {
		return "", errors.New("purge path overlaps the installation root or a filesystem root")
	}
	if !strings.EqualFold(filepath.Base(path), productidentity.ConfigDirectory) {
		return "", fmt.Errorf("purge path %s is not the product data directory", path)
	}
	return path, nil
}

func safeInstallRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		var err error
		value, err = DefaultInstallRoot()
		if err != nil {
			return "", err
		}
	}
	root, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	volumeRoot := filepath.VolumeName(root) + string(os.PathSeparator)
	if samePath(root, volumeRoot) || filepath.Dir(root) == root {
		return "", errors.New("installation root must not be a filesystem root")
	}
	return root, nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeFileAtomic(path, content, mode)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".install-state-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
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
	if err := replaceFile(temporaryPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
