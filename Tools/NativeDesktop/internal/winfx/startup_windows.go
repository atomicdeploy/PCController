//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	runRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName    = "RayanLamp"
)

var ErrStartWithWindowsOwnedByAnotherCommand = errors.New(
	"winfx: start-with-Windows registration belongs to another command",
)

type StartWithWindowsOwnershipError struct {
	Command string
}

func (e *StartWithWindowsOwnershipError) Error() string {
	if e == nil || strings.TrimSpace(e.Command) == "" {
		return ErrStartWithWindowsOwnedByAnotherCommand.Error()
	}
	return fmt.Sprintf(
		"%s: current command is %q",
		ErrStartWithWindowsOwnedByAnotherCommand,
		e.Command,
	)
}

func (e *StartWithWindowsOwnershipError) Unwrap() error {
	return ErrStartWithWindowsOwnedByAnotherCommand
}

type startWithWindowsAction uint8

const (
	startWithWindowsNoop startWithWindowsAction = iota
	startWithWindowsSet
	startWithWindowsDelete
	startWithWindowsConflict
)

// SetStartWithWindows adds or removes RayanLamp's current-user Run entry. It
// never writes machine-wide state and stores the executable as a quoted REG_SZ.
// Enabling refuses to replace a value owned by another command. Disabling
// removes the value only when it still points at exePath exactly.
func SetStartWithWindows(enabled bool, exePath string) error {
	return setStartWithWindowsEntry(enabled, exePath, runValueName)
}

func setStartWithWindowsEntry(enabled bool, exePath, valueName string, arguments ...string) error {
	if strings.TrimSpace(valueName) == "" || strings.ContainsRune(valueName, 0) {
		return errors.New("winfx: invalid startup entry name")
	}
	command, err := quoteRunCommand(exePath)
	if err != nil {
		return err
	}

	for _, argument := range arguments {
		if strings.ContainsRune(argument, 0) {
			return errors.New("winfx: invalid startup argument")
		}
		command += " " + syscall.EscapeArg(argument)
	}
	const access = registry.QUERY_VALUE | registry.SET_VALUE
	var key registry.Key
	if enabled {
		key, _, err = registry.CreateKey(
			registry.CURRENT_USER,
			runRegistryPath,
			access,
		)
		if err != nil {
			return fmt.Errorf("create/open HKCU Run: %w", err)
		}
	} else {
		key, err = registry.OpenKey(
			registry.CURRENT_USER,
			runRegistryPath,
			access,
		)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open HKCU Run: %w", err)
		}
	}
	defer key.Close()

	current, _, readErr := key.GetStringValue(valueName)
	exists := true
	if errors.Is(readErr, registry.ErrNotExist) {
		exists = false
		current = ""
	} else if readErr != nil {
		return fmt.Errorf("read HKCU Run value: %w", readErr)
	}

	switch decideStartWithWindowsAction(enabled, current, exists, command) {
	case startWithWindowsNoop:
		return nil
	case startWithWindowsSet:
		if err := key.SetStringValue(valueName, command); err != nil {
			return fmt.Errorf("write HKCU Run value: %w", err)
		}
		return nil
	case startWithWindowsDelete:
		if err := key.DeleteValue(valueName); err != nil &&
			!errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("remove HKCU Run value: %w", err)
		}
		return nil
	case startWithWindowsConflict:
		return &StartWithWindowsOwnershipError{Command: current}
	default:
		return errors.New("winfx: unknown start-with-Windows action")
	}
}

func decideStartWithWindowsAction(
	enabled bool,
	current string,
	exists bool,
	expected string,
) startWithWindowsAction {
	if !exists {
		if enabled {
			return startWithWindowsSet
		}
		return startWithWindowsNoop
	}
	if strings.EqualFold(
		strings.TrimSpace(current),
		strings.TrimSpace(expected),
	) {
		if enabled {
			return startWithWindowsNoop
		}
		return startWithWindowsDelete
	}
	if enabled {
		return startWithWindowsConflict
	}
	// A disable request never owns a differently targeted value.
	return startWithWindowsNoop
}

// StartWithWindowsCommand returns the registered command, or an empty string
// when the RayanLamp value does not exist.
func StartWithWindowsCommand() (string, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		runRegistryPath,
		registry.QUERY_VALUE,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open HKCU Run: %w", err)
	}
	defer key.Close()
	command, _, err := key.GetStringValue(runValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read HKCU Run value: %w", err)
	}
	return strings.TrimSpace(command), nil
}

// IsStartWithWindows reports whether a non-empty RayanLamp Run entry exists.
func IsStartWithWindows() (bool, error) {
	command, err := StartWithWindowsCommand()
	return command != "", err
}

// IsStartWithWindowsFor additionally verifies that the entry points at
// exePath exactly (case-insensitively, as Windows paths are normally treated).
func IsStartWithWindowsFor(exePath string) (bool, error) {
	want, err := quoteRunCommand(exePath)
	if err != nil {
		return false, err
	}
	got, err := StartWithWindowsCommand()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(got, want), nil
}

// ReconcileStartWithWindowsSetting returns the ownership observed in the Run
// key without changing it. If the read fails, configured is retained and the
// error is returned so startup can continue without inventing new state.
func ReconcileStartWithWindowsSetting(
	executable string,
	configured bool,
) (bool, error) {
	return reconcileStartWithWindowsSetting(
		executable,
		configured,
		IsStartWithWindowsFor,
	)
}

func reconcileStartWithWindowsSetting(
	executable string,
	configured bool,
	probe func(string) (bool, error),
) (bool, error) {
	if probe == nil {
		return configured, errors.New("winfx: nil start-with-Windows probe")
	}
	registered, err := probe(executable)
	if err != nil {
		return configured, err
	}
	return registered, nil
}

// StartWithWindowsPersistence remembers the last persisted integration setting.
// Construction is deliberately read/write-free. ApplySaved performs a registry
// operation only after a newly saved value differs from the initial or last
// saved value.
type StartWithWindowsPersistence struct {
	mu         sync.Mutex
	executable string
	persisted  bool
	apply      func(bool, string) error
}

func NewStartWithWindowsPersistence(
	executable string,
	initial bool,
) (*StartWithWindowsPersistence, error) {
	return newStartWithWindowsPersistence(
		executable,
		initial,
		SetStartWithWindows,
	)
}

// NewNamedStartWithWindowsPersistence reuses the donor's ownership checks for
// another application identity. Construction never changes the registry.
func NewNamedStartWithWindowsPersistence(valueName, executable string, initial bool, arguments ...string) (*StartWithWindowsPersistence, error) {
	return newStartWithWindowsPersistence(executable, initial, func(enabled bool, exe string) error {
		return setStartWithWindowsEntry(enabled, exe, valueName, arguments...)
	})
}

func newStartWithWindowsPersistence(
	executable string,
	initial bool,
	apply func(bool, string) error,
) (*StartWithWindowsPersistence, error) {
	if apply == nil {
		return nil, errors.New("winfx: nil start-with-Windows persistence function")
	}
	if _, err := quoteRunCommand(executable); err != nil {
		return nil, err
	}
	return &StartWithWindowsPersistence{
		executable: executable,
		persisted:  initial,
		apply:      apply,
	}, nil
}

func (p *StartWithWindowsPersistence) ApplySaved(enabled bool) error {
	if p == nil {
		return errors.New("winfx: nil start-with-Windows persistence")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if enabled == p.persisted {
		return nil
	}
	// The configuration is already durably saved when this method is called.
	// Advance the baseline even if the external registry operation fails, so an
	// unrelated later config save cannot silently retry a non-requested change.
	p.persisted = enabled
	return p.apply(enabled, p.executable)
}
