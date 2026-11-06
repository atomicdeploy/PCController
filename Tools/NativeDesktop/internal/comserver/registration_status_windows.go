//go:build windows

package comserver

import (
	"errors"
	"fmt"
	"os"
)

type registryKeyExistsReader func(path string, view uint32) (bool, error)

// IsRegistered reports whether every COM registry view and shared ProgID
// marker required by the current Windows architecture is owned by executable.
// It is strictly read-only; foreign, incomplete, and absent registrations all
// report false without being repaired or removed.
func IsRegistered(executable string) (bool, error) {
	views, err := comRegistryViews()
	if err != nil {
		return false, err
	}
	return isRegisteredWithReaders(
		executable,
		views,
		registryKeyExistsView,
		registeredLocalServerCommand,
		readRegistryStringView,
	)
}

// IsCurrentExecutableRegistered is the read-only portable-mode status probe
// for the process image hosting PCControllerDesktop.
func IsCurrentExecutableRegistered() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("comserver: locate current executable: %w", err)
	}
	return IsRegistered(executable)
}

func isRegisteredWithReaders(
	executable string,
	views []registryView,
	keyExists registryKeyExistsReader,
	localServer localServerCommandReader,
	shared registryStringReader,
) (bool, error) {
	if keyExists == nil || localServer == nil || shared == nil {
		return false, errors.New("comserver: nil registration status reader")
	}
	expected, err := RegistrationCommand(executable)
	if err != nil {
		return false, err
	}
	clsidRoot := classesRoot + `\CLSID\` + CLSID
	for _, view := range views {
		exists, err := keyExists(clsidRoot, view.access)
		if err != nil {
			return false, fmt.Errorf(
				"comserver: inspect %s CLSID registration: %w",
				view.label,
				err,
			)
		}
		if !exists {
			return false, nil
		}
		actual, commandExists, err := localServer(clsidRoot, view.access)
		if err != nil {
			return false, err
		}
		if !commandExists || !sameRegistrationCommand(actual, expected) {
			return false, nil
		}
	}
	for _, path := range sharedRegistrationRoots() {
		exists, err := keyExists(path, 0)
		if err != nil {
			return false, fmt.Errorf(
				"comserver: inspect shared registration %s: %w",
				path,
				err,
			)
		}
		if !exists {
			return false, nil
		}
		if err := validateSharedRegistrationRoot(path, shared); err != nil {
			if errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}
