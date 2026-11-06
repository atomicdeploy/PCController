//go:build windows

package shellintegration

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

type associationStore interface {
	KeyExists(path string) (bool, error)
	GetString(path, name string) (value string, exists bool, err error)
	SetString(path, name, value string) error
	DeleteTree(path string) error
}

type currentUserRegistryStore struct{}

// Register creates the optional per-user pccontroller-desktop URI association. It does
// not replace an association owned by a different executable.
func Register(executable string) error {
	return registerWithStore(currentUserRegistryStore{}, executable)
}

// RegisterCurrentExecutable registers the current process image. Registration
// is never performed by ordinary portable startup.
func RegisterCurrentExecutable() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("shellintegration: locate current executable: %w", err)
	}
	return Register(executable)
}

// Unregister removes the URI association only when its open command still
// points to executable. A relocated or separately installed copy is preserved.
func Unregister(executable string) error {
	return unregisterWithStore(currentUserRegistryStore{}, executable)
}

// UnregisterCurrentExecutable removes the current process image's association
// when, and only when, it still owns it.
func UnregisterCurrentExecutable() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("shellintegration: locate current executable: %w", err)
	}
	return Unregister(executable)
}

// IsRegistered reports whether executable currently owns the optional URI
// association. It is read-only and treats a missing or differently owned
// association as false.
func IsRegistered(executable string) (bool, error) {
	return isRegisteredWithStore(currentUserRegistryStore{}, executable)
}

// IsCurrentExecutableRegistered is the read-only portable-mode gate used by
// integrations that must not create registry state during ordinary startup.
func IsCurrentExecutableRegistered() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("shellintegration: locate current executable: %w", err)
	}
	return IsRegistered(executable)
}

func isRegisteredWithStore(
	store associationStore,
	executable string,
) (bool, error) {
	if store == nil {
		return false, errors.New("shellintegration: nil association store")
	}
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		return false, err
	}
	current, exists, err := store.GetString(protocolOpenPath, "")
	if err != nil {
		return false, fmt.Errorf("shellintegration: read protocol owner: %w", err)
	}
	return exists && registrationOwnedBy(current, spec.command), nil
}

func registerWithStore(store associationStore, executable string) error {
	if store == nil {
		return errors.New("shellintegration: nil association store")
	}
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		return err
	}
	rootExisted, err := store.KeyExists(protocolRootPath)
	if err != nil {
		return fmt.Errorf("shellintegration: inspect protocol registration: %w", err)
	}
	current, commandExists, err := store.GetString(protocolOpenPath, "")
	if err != nil {
		return fmt.Errorf("shellintegration: read protocol owner: %w", err)
	}
	if rootExisted && (!commandExists || !registrationOwnedBy(current, spec.command)) {
		return &OwnershipError{RegisteredCommand: current}
	}

	for _, value := range spec.values {
		if err := store.SetString(value.path, value.name, value.value); err != nil {
			if !rootExisted {
				// This process created the incomplete tree and has not
				// displaced another owner, so best-effort cleanup is safe.
				_ = store.DeleteTree(protocolRootPath)
			}
			return fmt.Errorf(
				"shellintegration: write protocol registration %s: %w",
				value.path,
				err,
			)
		}
	}
	return nil
}

func unregisterWithStore(store associationStore, executable string) error {
	if store == nil {
		return errors.New("shellintegration: nil association store")
	}
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		return err
	}
	exists, err := store.KeyExists(protocolRootPath)
	if err != nil {
		return fmt.Errorf("shellintegration: inspect protocol registration: %w", err)
	}
	if !exists {
		return nil
	}
	current, commandExists, err := store.GetString(protocolOpenPath, "")
	if err != nil {
		return fmt.Errorf("shellintegration: read protocol owner: %w", err)
	}
	if !commandExists || !registrationOwnedBy(current, spec.command) {
		return &OwnershipError{RegisteredCommand: current}
	}

	// Re-read immediately before deletion. Registry associations are not
	// transactional; this narrows the window in which another copy could take
	// ownership while an unregister command is running.
	current, commandExists, err = store.GetString(protocolOpenPath, "")
	if err != nil {
		return fmt.Errorf("shellintegration: recheck protocol owner: %w", err)
	}
	if !commandExists || !registrationOwnedBy(current, spec.command) {
		return &OwnershipError{RegisteredCommand: current}
	}
	if err := store.DeleteTree(protocolRootPath); err != nil {
		return fmt.Errorf("shellintegration: remove protocol registration: %w", err)
	}
	return nil
}

func (currentUserRegistryStore) KeyExists(path string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := key.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func (currentUserRegistryStore) GetString(
	path,
	name string,
) (string, bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		path,
		registry.QUERY_VALUE,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (currentUserRegistryStore) SetString(path, name, value string) error {
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		path,
		registry.SET_VALUE,
	)
	if err != nil {
		return err
	}
	if err := key.SetStringValue(name, value); err != nil {
		_ = key.Close()
		return err
	}
	return key.Close()
}

func (currentUserRegistryStore) DeleteTree(path string) error {
	return deleteRegistryTree(registry.CURRENT_USER, path)
}

func deleteRegistryTree(parent registry.Key, path string) error {
	key, err := registry.OpenKey(parent, path, registry.ALL_ACCESS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	names, readErr := key.ReadSubKeyNames(-1)
	if readErr == nil {
		for _, name := range names {
			if err := deleteRegistryTree(key, name); err != nil {
				_ = key.Close()
				return err
			}
		}
	}
	closeErr := key.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	err = registry.DeleteKey(parent, path)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

var _ associationStore = currentUserRegistryStore{}
