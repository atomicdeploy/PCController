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

	"pccontroller.local/controller/internal/ownedstorage"
	"pccontroller.local/controller/internal/pathguard"
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
	DesiredState  *InstallationState `json:"desired_state,omitempty"`
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
	PurgeConfigFiles  []string
	PurgeDataRoots    []string
	PreviewPurge      bool
}

type PurgeTarget struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
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
	PurgeTargets   []PurgeTarget      `json:"purge_targets,omitempty"`
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
	cleaned, cleanupErr := service.cleanupDetachedInstallation(resolved)
	result.Changed = cleaned
	if cleanupErr != nil {
		return result, cleanupErr
	}
	if _, err := os.Lstat(resolved); errors.Is(err, os.ErrNotExist) {
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
	if _, cleanupErr := service.cleanupDetachedInstallation(root); cleanupErr != nil {
		return result, cleanupErr
	}
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
			next := previous
			next.DisplayName = service.DisplayName
			next.DesktopManaged = desktopManaged
			nameChanged := !strings.EqualFold(strings.TrimSpace(previous.DisplayName), strings.TrimSpace(next.DisplayName))
			stateChanged := previous.DesktopManaged != desktopManaged || nameChanged
			desktopTransition := service.desktopTransitionRequired(root, &previous, next)
			if stateChanged {
				next.UpdatedAt = service.now()
			}
			if desktopTransition {
				id, idErr := transactionID()
				if idErr != nil {
					return result, idErr
				}
				previousCopy, desiredCopy := previous, next
				journal := transactionJournal{
					Format: transactionFormat, ID: id, Operation: operation, Phase: "presentation",
					NewSlot: next.ActiveSlot, NewSHA256: next.ActiveSHA256,
					PreviousState: &previousCopy, DesiredState: &desiredCopy, UpdatedAt: service.now(),
				}
				if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
					return result, err
				}
				if err := service.reconcileDesktopTransition(ctx, root, journal.PreviousState, next); err != nil {
					return result, fmt.Errorf("apply journaled desktop transition: %w", err)
				}
				if err := writeJSONAtomic(filepath.Join(root, installationStateName), next, 0o600); err != nil {
					return result, fmt.Errorf("persist journaled desktop transition: %w", err)
				}
				if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return result, fmt.Errorf("commit desktop transition: %w", err)
				}
			} else {
				if desktopManaged {
					if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, next)); err != nil {
						return result, fmt.Errorf("repair native desktop integration: %w", err)
					}
				}
				if stateChanged {
					if err := writeJSONAtomic(filepath.Join(root, installationStateName), next, 0o600); err != nil {
						return result, fmt.Errorf("persist presentation state: %w", err)
					}
				}
			}
			if stateChanged {
				result.Changed = true
			}
			result.Healthy, result.DesktopManaged, result.Executable = true, desktopManaged, filepath.Join(root, filepath.FromSlash(next.Executable))
			result.State = &next
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
		_ = removeOwnedSubtree(root, stage)
		_ = os.Remove(filepath.Join(root, transactionName))
		return result, err
	}

	slotRelative := filepath.ToSlash(filepath.Join(packagesDirectory, manifest.RootSHA256))
	slot := filepath.Join(root, filepath.FromSlash(slotRelative))
	if err := pathguard.MkdirAll(filepath.Dir(slot), 0o700); err != nil {
		return result, err
	}
	if _, statErr := os.Lstat(slot); statErr == nil {
		if verifyErr := service.verifySlot(root, slotRelative, manifest.RootSHA256); verifyErr == nil {
			if err := removeOwnedSubtree(root, stage); err != nil {
				return result, err
			}
		} else {
			slotRelative = filepath.ToSlash(filepath.Join(packagesDirectory, manifest.RootSHA256+"-repair-"+id))
			slot = filepath.Join(root, filepath.FromSlash(slotRelative))
			if err := publishDirectory(stage, slot); err != nil {
				return result, fmt.Errorf("publish repaired package slot: %w", err)
			}
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if err := publishDirectory(stage, slot); err != nil {
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
		if previous.ActiveSlot != slotRelative {
			next.PreviousSlot, next.PreviousSHA256 = previous.ActiveSlot, previous.ActiveSHA256
		} else {
			next.PreviousSlot, next.PreviousSHA256 = previous.PreviousSlot, previous.PreviousSHA256
		}
	}
	desiredCopy := next
	journal.DesiredState, journal.UpdatedAt = &desiredCopy, service.now()
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, fmt.Errorf("persist desired installation state: %w", err)
	}
	if err := writeJSONAtomic(filepath.Join(root, installationStateName), next, 0o600); err != nil {
		return result, err
	}
	journal.Phase, journal.UpdatedAt = "activated", service.now()
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	if err := service.reconcileDesktopTransition(ctx, root, journal.PreviousState, next); err != nil {
		return result, fmt.Errorf("activate journaled desktop integration: %w", err)
	}
	if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !errors.Is(err, os.ErrNotExist) {
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
		if !request.PreviewPurge && request.PurgeConfirmation != PurgeConfirmation {
			return result, fmt.Errorf("data purge requires the exact separate confirmation %q", PurgeConfirmation)
		}
		result.PurgeTargets, err = preparePurgeTargets(request, root, service.OwnerID)
		if err != nil {
			return result, err
		}
		if request.PreviewPurge {
			return result, nil
		}
	}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		cleaned, cleanupErr := service.cleanupDetachedInstallation(root)
		result.Changed = cleaned
		if cleanupErr != nil {
			return result, cleanupErr
		}
		if request.PurgeData {
			if err := executePurgeTargets(result.PurgeTargets, root, service.OwnerID, &result); err != nil {
				return result, err
			}
		}
		return result, nil
	} else if err != nil {
		return result, err
	}
	if cleaned, cleanupErr := service.cleanupDetachedInstallation(root); cleanupErr != nil {
		return result, cleanupErr
	} else if cleaned {
		result.Warnings = append(result.Warnings, "completed cleanup of a previously detached installation")
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
	journal := transactionJournal{
		Format: transactionFormat, ID: "uninstall", Operation: "uninstall",
		Phase: "uninstall-prepared", UpdatedAt: service.now(),
	}
	if exists {
		copy := state
		journal.PreviousState = &copy
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		return result, err
	}
	if exists && state.DesktopManaged {
		if service.Desktop == nil {
			return result, ErrDesktopAdapterUnavailable
		}
		if err := service.Desktop.RemoveOwned(ctx, service.desktopTarget(root, state)); err != nil {
			return result, fmt.Errorf("remove owned native desktop integration: %w", err)
		}
	}
	journal.Phase, journal.UpdatedAt = "uninstalling", service.now()
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
	if err := pathguard.ValidateTree(root); err != nil {
		return result, fmt.Errorf("validate installation before detaching: %w", err)
	}
	tombstone := removalTombstone(root)
	if err := pathguard.ValidateComponents(tombstone, true); err != nil {
		return result, err
	}
	if err := os.Rename(root, tombstone); err != nil {
		return result, fmt.Errorf("detach installation for removal: %w", err)
	}
	if err := pathguard.ValidateTree(tombstone); err != nil {
		return result, fmt.Errorf("validate detached installation: %w", err)
	}
	if err := service.checkDetachedOwnership(tombstone, root); err != nil {
		return result, fmt.Errorf("validate detached installation ownership: %w", err)
	}
	result.Changed = true
	if err := removeTreeSecure(tombstone); err != nil {
		result.Warnings = append(result.Warnings, "detached installation remains queued for cleanup: "+tombstone)
	}
	if request.PurgeData {
		if err := executePurgeTargets(result.PurgeTargets, root, service.OwnerID, &result); err != nil {
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

func removalTombstone(root string) string { return root + ".removing" }

func (service *Service) cleanupDetachedInstallation(root string) (bool, error) {
	tombstone := removalTombstone(root)
	if err := pathguard.ValidateComponents(tombstone, true); err != nil {
		return false, err
	}
	if _, err := os.Lstat(tombstone); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := service.checkDetachedOwnership(tombstone, root); err != nil {
		return false, fmt.Errorf("refuse foreign detached installation cleanup: %w", err)
	}
	if err := removeTreeSecure(tombstone); err != nil {
		return false, err
	}
	return true, nil
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

func (service *Service) desktopTransitionRequired(root string, previous *InstallationState, desired InstallationState) bool {
	if previous == nil {
		return desired.DesktopManaged
	}
	if previous.DesktopManaged != desired.DesktopManaged {
		return true
	}
	if !desired.DesktopManaged {
		return false
	}
	priorTarget := service.desktopTarget(root, *previous)
	desiredTarget := service.desktopTarget(root, desired)
	return !strings.EqualFold(strings.TrimSpace(priorTarget.DisplayName), strings.TrimSpace(desiredTarget.DisplayName)) ||
		!samePath(priorTarget.Executable, desiredTarget.Executable)
}

// reconcileDesktopTransition is deliberately idempotent. The transaction
// journal remains durable until prior owned artifacts have been removed, the
// desired identity has been ensured, and the matching installation state has
// been persisted. Re-running after any crash boundary is therefore safe.
func (service *Service) reconcileDesktopTransition(
	ctx context.Context,
	root string,
	previous *InstallationState,
	desired InstallationState,
) error {
	if (previous != nil && previous.DesktopManaged) || desired.DesktopManaged {
		if service.Desktop == nil {
			return ErrDesktopAdapterUnavailable
		}
	}
	if previous != nil && previous.DesktopManaged && service.desktopTransitionRequired(root, previous, desired) {
		if err := service.Desktop.RemoveOwned(ctx, service.desktopTarget(root, *previous)); err != nil {
			return fmt.Errorf("remove prior native desktop identity: %w", err)
		}
	}
	if desired.DesktopManaged {
		if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, desired)); err != nil {
			return fmt.Errorf("ensure desired native desktop identity: %w", err)
		}
	}
	return nil
}

func (service *Service) checkOwnership(root string, create bool) error {
	if err := pathguard.ValidateComponents(root, create); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := pathguard.MkdirAll(root, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("installation root must be a real directory, not a link")
	}
	if err := pathguard.ValidateComponents(root, false); err != nil {
		return err
	}
	path := filepath.Join(root, ownerMarkerName)
	content, err := readBoundedRegularFile(path, 64<<10)
	if errors.Is(err, os.ErrNotExist) && create {
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
		if errors.Is(err, os.ErrNotExist) {
			return ErrOwnershipMismatch
		}
		return err
	}
	return service.validateOwnershipMarker(content, root)
}

func (service *Service) checkDetachedOwnership(location, originalRoot string) error {
	if err := pathguard.ValidateComponents(location, false); err != nil {
		return err
	}
	info, err := os.Lstat(location)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrOwnershipMismatch
	}
	content, err := readBoundedRegularFile(filepath.Join(location, ownerMarkerName), 64<<10)
	if err != nil {
		return err
	}
	return service.validateOwnershipMarker(content, originalRoot)
}

func (service *Service) validateOwnershipMarker(content []byte, expectedRoot string) error {
	var marker ownerMarker
	if err := decodeStrictJSON(content, &marker); err != nil {
		return fmt.Errorf("%w: invalid ownership marker: %v", ErrOwnershipMismatch, err)
	}
	if marker.Format != ownerMarkerFormat || marker.ProductAppID != productidentity.StableAppID || marker.OwnerID != service.OwnerID || !samePath(marker.InstallRoot, expectedRoot) {
		return ErrOwnershipMismatch
	}
	return nil
}

func (service *Service) stagePackage(source, stage string, manifest PackageManifest) error {
	if err := removeTreeSecure(stage); err != nil {
		return err
	}
	if err := pathguard.MkdirAll(stage, 0o700); err != nil {
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
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal transactionJournal
	if err := decodeStrictJSON(content, &journal); err != nil || journal.Format != transactionFormat {
		return fmt.Errorf("installation recovery journal is invalid: %w", err)
	}
	for _, candidate := range []struct {
		name  string
		state *InstallationState
	}{
		{name: "previous", state: journal.PreviousState},
		{name: "desired", state: journal.DesiredState},
	} {
		if candidate.state == nil {
			continue
		}
		if err := validateInstallationState(*candidate.state); err != nil {
			return fmt.Errorf("installation recovery journal %s state is invalid: %w", candidate.name, err)
		}
		if candidate.state.OwnerID != service.OwnerID {
			return ErrOwnershipMismatch
		}
	}
	if journal.DesiredState != nil &&
		(journal.DesiredState.ActiveSlot != journal.NewSlot ||
			!strings.EqualFold(journal.DesiredState.ActiveSHA256, journal.NewSHA256)) {
		return errors.New("installation recovery journal desired state differs from its slot identity")
	}
	switch journal.Phase {
	case "staging":
		if journal.Stage != "" {
			stage, pathErr := inventoryEntryPath(root, journal.Stage)
			if pathErr != nil {
				return pathErr
			}
			if err := removeOwnedSubtree(root, stage); err != nil {
				return err
			}
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
				if err := removeOwnedSubtree(root, slot); err != nil {
					return err
				}
			}
		} else {
			if err := service.verifySlot(root, state.ActiveSlot, state.ActiveSHA256); err != nil {
				return fmt.Errorf("recover published installation slot: %w", err)
			}
			if journal.DesiredState == nil || state != *journal.DesiredState {
				return errors.New("published installation transaction does not match its desired state")
			}
			if err := service.reconcileDesktopTransition(ctx, root, journal.PreviousState, state); err != nil {
				return fmt.Errorf("recover native desktop transition: %w", err)
			}
		}
	case "activated":
		state, exists, stateErr := loadState(root)
		if stateErr != nil || !exists || state.ActiveSlot != journal.NewSlot || !strings.EqualFold(state.ActiveSHA256, journal.NewSHA256) {
			return errors.New("activated installation transaction does not match durable state")
		}
		if journal.DesiredState == nil || state != *journal.DesiredState {
			return errors.New("activated installation transaction does not match its desired state")
		}
		if err := service.verifySlot(root, state.ActiveSlot, state.ActiveSHA256); err != nil {
			return fmt.Errorf("recover activated installation slot: %w", err)
		}
		if err := service.reconcileDesktopTransition(ctx, root, journal.PreviousState, state); err != nil {
			return fmt.Errorf("recover activated desktop transition: %w", err)
		}
	case "presentation":
		if journal.PreviousState == nil || journal.DesiredState == nil {
			return errors.New("desktop presentation transaction is incomplete")
		}
		state, exists, stateErr := loadState(root)
		if stateErr != nil || !exists {
			return errors.New("desktop presentation transaction has no durable installation state")
		}
		if state != *journal.PreviousState && state != *journal.DesiredState {
			return errors.New("desktop presentation transaction differs from durable state")
		}
		if err := service.verifySlot(root, journal.DesiredState.ActiveSlot, journal.DesiredState.ActiveSHA256); err != nil {
			return fmt.Errorf("recover desktop presentation installation slot: %w", err)
		}
		if err := service.reconcileDesktopTransition(ctx, root, journal.PreviousState, *journal.DesiredState); err != nil {
			return fmt.Errorf("recover desktop presentation transition: %w", err)
		}
		if err := writeJSONAtomic(filepath.Join(root, installationStateName), *journal.DesiredState, 0o600); err != nil {
			return fmt.Errorf("persist recovered desktop presentation state: %w", err)
		}
	case "uninstall-prepared", "uninstalling":
		state, exists, stateErr := loadState(root)
		if stateErr != nil {
			return stateErr
		}
		if exists {
			if state.OwnerID != service.OwnerID {
				return ErrOwnershipMismatch
			}
			if err := service.verifySlot(root, state.ActiveSlot, state.ActiveSHA256); err != nil {
				return fmt.Errorf("recover interrupted uninstall installation slot: %w", err)
			}
			if state.DesktopManaged {
				if service.Desktop == nil {
					return ErrDesktopAdapterUnavailable
				}
				if err := service.Desktop.Ensure(ctx, service.desktopTarget(root, state)); err != nil {
					return fmt.Errorf("restore desktop integration after interrupted uninstall: %w", err)
				}
			}
		}
	default:
		return fmt.Errorf("installation recovery journal phase %q is unsupported", journal.Phase)
	}
	if err := os.Remove(filepath.Join(root, transactionName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func loadState(root string) (InstallationState, bool, error) {
	content, err := readBoundedRegularFile(filepath.Join(root, installationStateName), 256<<10)
	if errors.Is(err, os.ErrNotExist) {
		return InstallationState{}, false, nil
	}
	if err != nil {
		return InstallationState{}, false, err
	}
	var state InstallationState
	if err := decodeStrictJSON(content, &state); err != nil {
		return InstallationState{}, false, err
	}
	if err := validateInstallationState(state); err != nil {
		return InstallationState{}, false, err
	}
	return state, true, nil
}

func validateInstallationState(state InstallationState) error {
	if state.Format != installationStateFormat || state.ProductAppID != productidentity.StableAppID || strings.TrimSpace(state.OwnerID) == "" {
		return errors.New("installation state identity is invalid")
	}
	if strings.TrimSpace(state.DisplayName) == "" {
		return errors.New("installation display name is missing")
	}
	if _, err := normalizeInventoryPath(state.ActiveSlot); err != nil {
		return err
	}
	if _, err := normalizeInventoryPath(state.Executable); err != nil {
		return err
	}
	activePrefix := strings.TrimSuffix(filepath.ToSlash(state.ActiveSlot), "/") + "/"
	if !strings.HasPrefix(strings.ToLower(filepath.ToSlash(state.Executable)), strings.ToLower(activePrefix)) {
		return errors.New("installation executable is outside the active package slot")
	}
	if _, err := normalizeDigest(state.ActiveSHA256); err != nil {
		return err
	}
	if (state.PreviousSlot == "") != (state.PreviousSHA256 == "") {
		return errors.New("installation rollback slot identity is incomplete")
	}
	if state.PreviousSlot != "" {
		if _, err := normalizeInventoryPath(state.PreviousSlot); err != nil {
			return err
		}
		if _, err := normalizeDigest(state.PreviousSHA256); err != nil {
			return err
		}
	}
	return nil
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
	if err := pathguard.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := pathguard.ValidateComponents(target, true); err != nil {
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
	if err := pathguard.ValidateComponents(target, false); err != nil {
		return err
	}
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
	if err := pathguard.ValidateComponents(directory, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return []string{"unable to trust superseded package directory: " + err.Error()}
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
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
		candidate := filepath.Join(directory, entry.Name())
		if err := removeOwnedSubtree(root, candidate); err != nil {
			warnings = append(warnings, "superseded package remains for later cleanup: "+entry.Name())
		}
	}
	sort.Strings(warnings)
	return warnings
}

func preparePurgeTargets(request UninstallRequest, installRoot, ownerID string) ([]PurgeTarget, error) {
	dataTargets := make([]PurgeTarget, 0, len(request.PurgeDataRoots))
	for _, value := range request.PurgeDataRoots {
		target, err := validatePurgeDataRoot(value, installRoot, ownerID)
		if err != nil {
			return nil, err
		}
		duplicate := false
		for index := 0; index < len(dataTargets); index++ {
			existing := dataTargets[index]
			if samePath(existing.Path, target.Path) || pathWithin(existing.Path, target.Path) {
				duplicate = true
				break
			}
			if pathWithin(target.Path, existing.Path) {
				dataTargets = append(dataTargets[:index], dataTargets[index+1:]...)
				index--
			}
		}
		if !duplicate {
			dataTargets = append(dataTargets, target)
		}
	}
	sort.Slice(dataTargets, func(left, right int) bool {
		return strings.ToLower(dataTargets[left].Path) < strings.ToLower(dataTargets[right].Path)
	})

	configTargets := make([]PurgeTarget, 0, len(request.PurgeConfigFiles))
	for _, value := range request.PurgeConfigFiles {
		target, err := validatePurgeConfigFile(value, installRoot)
		if err != nil {
			return nil, err
		}
		covered := false
		for _, data := range dataTargets {
			if pathWithin(data.Path, target.Path) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		for _, existing := range configTargets {
			if samePath(existing.Path, target.Path) {
				covered = true
				break
			}
		}
		if !covered {
			configTargets = append(configTargets, target)
		}
	}
	sort.Slice(configTargets, func(left, right int) bool {
		return strings.ToLower(configTargets[left].Path) < strings.ToLower(configTargets[right].Path)
	})
	return append(configTargets, dataTargets...), nil
}

func validatePurgeConfigFile(value, installRoot string) (PurgeTarget, error) {
	path, err := pathguard.CleanAbsolute(value)
	if err != nil {
		return PurgeTarget{}, fmt.Errorf("purge configuration file: %w", err)
	}
	if samePath(path, installRoot) || pathWithin(installRoot, path) {
		return PurgeTarget{}, errors.New("purge configuration file overlaps the installation root")
	}
	if err := pathguard.ValidateComponents(path, true); err != nil {
		return PurgeTarget{}, fmt.Errorf("purge configuration file %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return PurgeTarget{Kind: "config-file", Path: path}, nil
	}
	if err != nil {
		return PurgeTarget{}, err
	}
	if !info.Mode().IsRegular() {
		return PurgeTarget{}, errors.New("purge configuration target is not a regular file")
	}
	return PurgeTarget{Kind: "config-file", Path: path, Exists: true}, nil
}

func validatePurgeDataRoot(value, installRoot, ownerID string) (PurgeTarget, error) {
	path, err := pathguard.CleanAbsolute(value)
	if err != nil {
		return PurgeTarget{}, fmt.Errorf("purge data root: %w", err)
	}
	volumeRoot := filepath.VolumeName(path) + string(os.PathSeparator)
	if samePath(path, volumeRoot) || filepath.Dir(path) == path || samePath(path, installRoot) ||
		pathWithin(path, installRoot) || pathWithin(installRoot, path) {
		return PurgeTarget{}, errors.New("purge data root overlaps the installation root or a filesystem root")
	}
	if err := pathguard.ValidateComponents(path, true); err != nil {
		return PurgeTarget{}, fmt.Errorf("purge data root %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return PurgeTarget{Kind: "data-root", Path: path}, nil
	}
	if err != nil {
		return PurgeTarget{}, err
	}
	if !info.IsDir() {
		return PurgeTarget{}, errors.New("purge data root is not a directory")
	}
	if err := pathguard.ValidateTree(path); err != nil {
		return PurgeTarget{}, fmt.Errorf("purge data root %s: %w", path, err)
	}
	if err := ownedstorage.VerifyFor(path, ownerID); err != nil {
		return PurgeTarget{}, fmt.Errorf("purge data root %s: %w", path, err)
	}
	return PurgeTarget{Kind: "data-root", Path: path, Exists: true}, nil
}

func executePurgeTargets(targets []PurgeTarget, installRoot, ownerID string, result *LifecycleResult) error {
	for _, target := range targets {
		switch target.Kind {
		case "config-file":
			validated, err := validatePurgeConfigFile(target.Path, installRoot)
			if err != nil {
				return err
			}
			if !validated.Exists {
				continue
			}
			if err := os.Remove(validated.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("purge configuration file %s: %w", validated.Path, err)
			}
			result.PurgedPaths = append(result.PurgedPaths, validated.Path)
		case "data-root":
			validated, err := validatePurgeDataRoot(target.Path, installRoot, ownerID)
			if err != nil {
				return err
			}
			if !validated.Exists {
				continue
			}
			if err := removeTreeSecure(validated.Path); err != nil {
				return fmt.Errorf("purge user data %s: %w", validated.Path, err)
			}
			result.PurgedPaths = append(result.PurgedPaths, validated.Path)
		default:
			return fmt.Errorf("unsupported purge target kind %q", target.Kind)
		}
	}
	return nil
}

func safeInstallRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		var err error
		value, err = DefaultInstallRoot()
		if err != nil {
			return "", err
		}
	}
	root, err := pathguard.CleanAbsolute(value)
	if err != nil {
		return "", err
	}
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
	if err := pathguard.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := pathguard.ValidateComponents(path, true); err != nil {
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
	return pathguard.ValidateComponents(path, false)
}

func removeOwnedSubtree(root, target string) error {
	root, err := pathguard.CleanAbsolute(root)
	if err != nil {
		return err
	}
	target, err = pathguard.CleanAbsolute(target)
	if err != nil {
		return err
	}
	if samePath(root, target) || !pathWithin(root, target) {
		return errors.New("recursive removal target is outside the owned root")
	}
	if err := pathguard.ValidateComponents(root, false); err != nil {
		return err
	}
	return removeTreeSecure(target)
}

func removeTreeSecure(path string) error {
	if err := pathguard.ValidateComponents(path, true); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := pathguard.ValidateTree(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func publishDirectory(source, target string) error {
	if err := pathguard.ValidateTree(source); err != nil {
		return err
	}
	if err := pathguard.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := pathguard.ValidateComponents(target, true); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	if err := pathguard.ValidateComponents(target, false); err != nil {
		return err
	}
	return pathguard.ValidateTree(target)
}
