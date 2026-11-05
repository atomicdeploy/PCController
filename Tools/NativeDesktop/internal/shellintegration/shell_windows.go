//go:build windows

package shellintegration

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// RegisterShell installs the URI association first and then publishes the
// static Jump List task set. A Jump List policy/API failure leaves the useful
// URI registration in place and is returned as a PartialError.
func RegisterShell(executable string) error {
	return registerShellWithStore(
		currentUserRegistryStore{},
		executable,
		installJumpList,
	)
}

func registerShellWithStore(
	store associationStore,
	executable string,
	install func(string) error,
) error {
	if install == nil {
		return errors.New("shellintegration: nil Jump List installer")
	}
	if err := registerWithStore(store, executable); err != nil {
		return err
	}
	if err := install(executable); err != nil {
		return &PartialError{
			Completed: "the rayan-lamp URI protocol was registered",
			Failed:    "Jump List task installation",
			Err:       err,
		}
	}
	return nil
}

func RegisterCurrentShell() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("shellintegration: locate current executable: %w", err)
	}
	return RegisterShell(executable)
}

// UnregisterShell removes shell state only when executable owns the URI
// association. Jump List and legacy toast metadata failures do not prevent the
// owned URI registration from being removed.
func UnregisterShell(executable string) error {
	return unregisterShellWithStore(
		currentUserRegistryStore{},
		executable,
		deleteJumpList,
	)
}

func unregisterShellWithStore(
	store associationStore,
	executable string,
	deleteTasks func() error,
) error {
	if deleteTasks == nil {
		return errors.New("shellintegration: nil Jump List remover")
	}
	owned, err := isRegisteredWithStore(store, executable)
	if err != nil {
		return err
	}
	if !owned {
		// Preserve a foreign association, but keep an absent association
		// idempotent. Unregister performs that distinction without mutation.
		return unregisterWithStore(store, executable)
	}
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		return err
	}
	// The explicit AppUserModelID makes the Jump List and legacy toast
	// metadata shared across every copy of RayanLamp. Re-check the URI's exact
	// owner immediately before touching that shared state: another copy may
	// have taken ownership after the initial idempotence/foreign-owner probe.
	current, commandExists, err := store.GetString(protocolOpenPath, "")
	if err != nil {
		return fmt.Errorf(
			"shellintegration: recheck protocol owner before shared cleanup: %w",
			err,
		)
	}
	if !commandExists || !registrationOwnedBy(current, spec.command) {
		return &OwnershipError{RegisteredCommand: current}
	}

	var jumpErr, metadataErr, protocolErr error
	jumpErr = deleteTasks()
	metadataErr = cleanupLegacyMetadataWithStore(
		store,
		spec.executable,
	)
	protocolErr = unregisterWithStore(store, spec.executable)
	joined := errors.Join(jumpErr, metadataErr, protocolErr)
	if joined == nil {
		return nil
	}

	completed := make([]string, 0, 3)
	failed := make([]string, 0, 3)
	if jumpErr == nil {
		completed = append(completed, "Jump List tasks were removed")
	} else {
		failed = append(failed, "Jump List removal")
	}
	if metadataErr == nil {
		completed = append(completed, "owned legacy shell metadata was cleaned")
	} else {
		failed = append(failed, "legacy shell metadata cleanup")
	}
	if protocolErr == nil {
		completed = append(completed, "the rayan-lamp URI protocol was removed")
	} else {
		failed = append(failed, "URI protocol removal")
	}
	if len(completed) == 0 {
		return fmt.Errorf(
			"shellintegration: shell integration removal failed: %w",
			joined,
		)
	}
	return &PartialError{
		Completed: strings.Join(completed, "; "),
		Failed:    strings.Join(failed, " and "),
		Err:       joined,
	}
}

func UnregisterCurrentShell() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("shellintegration: locate current executable: %w", err)
	}
	return UnregisterShell(executable)
}

// cleanupLegacyMetadataWithStore removes metadata written by older
// wintoast-based builds only when its activator is absent/stale or points at
// executable. Foreign activators and unrelated AppUserModelID metadata are
// preserved.
func cleanupLegacyMetadataWithStore(
	store associationStore,
	executable string,
) error {
	if store == nil {
		return errors.New("shellintegration: nil association store")
	}
	customActivator, appMetadataExists, err := store.GetString(
		appUserModelRegistryPath,
		appUserModelActivator,
	)
	if err != nil {
		return fmt.Errorf("shellintegration: read AppUserModelID metadata: %w", err)
	}
	activationCommand, activationExists, err := store.GetString(
		legacyToastServerPath,
		"",
	)
	if err != nil {
		return fmt.Errorf("shellintegration: read legacy toast activator: %w", err)
	}
	activationOwned := activationExists &&
		activationOwnedBy(activationCommand, executable)
	appUsesLegacyActivator := appMetadataExists &&
		strings.EqualFold(
			strings.TrimSpace(customActivator),
			legacyToastActivatorID,
		)

	var joined error
	if activationOwned {
		if err := store.DeleteTree(legacyToastActivatorPath); err != nil {
			joined = errors.Join(
				joined,
				fmt.Errorf("remove owned toast activator: %w", err),
			)
		}
	}
	// When the URI is owned by this executable, matching metadata whose
	// activator is absent is stale. If the activator points elsewhere, both
	// pieces are preserved for that other executable.
	if appUsesLegacyActivator && (!activationExists || activationOwned) {
		if err := store.DeleteTree(appUserModelRegistryPath); err != nil {
			joined = errors.Join(
				joined,
				fmt.Errorf("remove owned AppUserModelID metadata: %w", err),
			)
		}
	}
	return joined
}
