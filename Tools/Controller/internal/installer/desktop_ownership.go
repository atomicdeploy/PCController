package installer

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OwnsDesktopPredecessor authorizes stale links only between verified package
// slots in the same user-owned installation. It is read-only: unlike Status,
// it neither recovers an installation journal nor takes its lifecycle lock.
// The active executable must match current persisted state; an old running
// process cannot use this to retarget current links back to an earlier build.
func (service *Service) OwnsDesktopPredecessor(executable, candidate string) (bool, error) {
	root, activeSlot, ok := desktopPackageSlot(executable)
	if !ok {
		return false, nil
	}
	otherRoot, previousSlot, ok := desktopPackageSlot(candidate)
	if !ok || !samePath(root, otherRoot) || samePath(activeSlot, previousSlot) {
		return false, nil
	}
	if err := service.checkOwnership(root, false); err != nil {
		return false, err
	}
	state, exists, err := loadState(root)
	if err != nil {
		return false, err
	}
	if !exists || !state.DesktopManaged || state.OwnerID != service.OwnerID ||
		!samePath(executable, filepath.Join(root, filepath.FromSlash(state.Executable))) ||
		!samePath(activeSlot, filepath.Join(root, filepath.FromSlash(state.ActiveSlot))) {
		return false, nil
	}
	if err := service.verifySlot(root, state.ActiveSlot, state.ActiveSHA256); err != nil {
		return false, err
	}
	if state.PreviousSlot == "" || !samePath(previousSlot, filepath.Join(root, filepath.FromSlash(state.PreviousSlot))) {
		return false, nil
	}
	manifest, err := VerifyPackage(previousSlot, state.PreviousSHA256, ManifestOptions{
		Platform: service.Platform, Architecture: service.Architecture,
		VerifyExecutable: service.VerifyExecutable,
	})
	if err != nil {
		return false, fmt.Errorf("verify prior shortcut package: %w", err)
	}
	return samePath(candidate, filepath.Join(previousSlot, filepath.FromSlash(manifest.ExecutablePath))), nil
}

// Current packages keep their executable directly in packages/<digest>/.
// Do not broaden ownership to arbitrary sibling directories or path prefixes.
func desktopPackageSlot(executable string) (root, slot string, ok bool) {
	resolved, err := filepath.Abs(executable)
	if err != nil {
		return "", "", false
	}
	slot = filepath.Dir(resolved)
	packages := filepath.Dir(slot)
	if !strings.EqualFold(filepath.Base(packages), packagesDirectory) {
		return "", "", false
	}
	return filepath.Dir(packages), slot, true
}
