// Package portowner diagnoses and safely interacts with a process holding a serial device.
package portowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Window describes the best top-level window associated with a process.
type Window struct {
	Handle  uintptr `json:"handle,omitempty"`
	Title   string  `json:"title,omitempty"`
	Class   string  `json:"class,omitempty"`
	Visible bool    `json:"visible,omitempty"`
}

// Owner is the best available identity for a process holding the serial handle.
type Owner struct {
	PID              uint32 `json:"pid"`
	Name             string `json:"name,omitempty"`
	Executable       string `json:"executable,omitempty"`
	ProcessStartTime uint64 `json:"process_start_time_100ns,omitempty"`
	Window           Window `json:"window,omitempty"`
}

// Label returns a concise human-readable process identity.
func (owner Owner) Label() string {
	name := strings.TrimSpace(owner.Name)
	if name == "" && owner.Executable != "" {
		name = filepath.Base(owner.Executable)
	}
	if name == "" {
		name = "unknown process"
	}
	if owner.PID != 0 {
		return fmt.Sprintf("%s (PID %d)", name, owner.PID)
	}
	return name
}

// Detail returns process, executable, and window information when available.
func (owner Owner) Detail() string {
	parts := []string{owner.Label()}
	if owner.Executable != "" {
		parts = append(parts, owner.Executable)
	}
	if owner.Window.Title != "" {
		window := fmt.Sprintf("window %q", owner.Window.Title)
		if owner.Window.Class != "" {
			window += " [" + owner.Window.Class + "]"
		}
		parts = append(parts, window)
	}
	return strings.Join(parts, "; ")
}

// Enumerator resolves the process holding an exclusive serial-device handle.
type Enumerator interface {
	FindOwner(context.Context, string) (Owner, bool, error)
}

// Actions are intentionally operator-driven owner-window/process operations.
type Actions interface {
	BringToForeground(context.Context, Owner) error
	RequestGracefulClose(context.Context, Owner) error
	Terminate(context.Context, Owner, string) error
	TerminateConfirmation(Owner) string
}

// BusyError retains the original serial-open error and optional owner diagnostics.
type BusyError struct {
	Port            string
	Cause           error
	Owner           *Owner
	DiagnosticError error
}

func (failure *BusyError) Error() string {
	base := fmt.Sprintf("open %s: %v", failure.Port, failure.Cause)
	if failure.Owner != nil {
		return base + "; serial owner: " + failure.Owner.Detail()
	}
	if failure.DiagnosticError != nil {
		return base + "; serial owner could not be resolved: " + failure.DiagnosticError.Error()
	}
	return base + "; serial owner could not be resolved"
}

func (failure *BusyError) Unwrap() error { return failure.Cause }

// EnrichOpenError adds owner diagnostics only for a local COM access/busy failure.
func EnrichOpenError(ctx context.Context, port string, cause error) error {
	return EnrichOpenErrorWith(ctx, port, cause, systemEnumerator())
}

// EnrichOpenErrorWith is the deterministic/injectable form used by tests and adapters.
func EnrichOpenErrorWith(ctx context.Context, port string, cause error, enumerator Enumerator) error {
	if cause == nil {
		return nil
	}
	if !isCOMPort(port) || !isAccessDenied(cause) {
		return fmt.Errorf("open %s: %w", port, cause)
	}
	failure := &BusyError{Port: port, Cause: cause}
	if enumerator == nil {
		return failure
	}
	owner, found, err := enumerator.FindOwner(ctx, port)
	if err != nil {
		failure.DiagnosticError = err
	} else if found {
		failure.Owner = &owner
	}
	return failure
}

func isCOMPort(value string) bool {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), `\\.\`))
	if !strings.HasPrefix(value, "COM") || len(value) == 3 {
		return false
	}
	for _, character := range value[3:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func looksAccessDenied(cause error) bool {
	if errors.Is(cause, os.ErrPermission) {
		return true
	}
	text := strings.ToLower(cause.Error())
	return strings.Contains(text, "access is denied") ||
		strings.Contains(text, "permission denied") ||
		strings.Contains(text, "serial port busy") ||
		strings.Contains(text, "sharing violation")
}

func terminationConfirmation(owner Owner) string {
	return fmt.Sprintf("TERMINATE %d", owner.PID)
}

func validateTermination(owner Owner, confirmation string, currentPID uint32, currentExecutable string) error {
	if owner.PID == 0 {
		return errors.New("cannot terminate an owner without a PID")
	}
	if confirmation != terminationConfirmation(owner) {
		return fmt.Errorf("explicit confirmation required: %s", terminationConfirmation(owner))
	}
	if owner.PID == currentPID {
		return errors.New("refusing to terminate the current controller process")
	}
	if sameExecutable(owner, currentExecutable) {
		return errors.New("refusing to terminate a controller process; use its graceful quit or primary IPC command")
	}
	return nil
}

func sameExecutable(owner Owner, current string) bool {
	current = strings.TrimSpace(current)
	if current == "" {
		current, _ = os.Executable()
	}
	if owner.Executable != "" && current != "" {
		left, leftErr := filepath.Abs(owner.Executable)
		right, rightErr := filepath.Abs(current)
		if leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right)) {
			return true
		}
	}
	return owner.Name != "" && current != "" && strings.EqualFold(owner.Name, filepath.Base(current))
}
