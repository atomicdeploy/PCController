package shellintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	classesRootPath       = `Software\Classes`
	protocolRootPath      = classesRootPath + `\` + Scheme
	protocolIconPath      = protocolRootPath + `\DefaultIcon`
	protocolOpenPath      = protocolRootPath + `\shell\open\command`
	protocolDisplayName   = "URL:PCController Protocol"
	protocolURLValueName  = "URL Protocol"
	protocolArgumentToken = "%1"

	appUserModelRegistryPath = classesRootPath + `\AppUserModelId\` + AppUserModelID
	appUserModelActivator    = "CustomActivator"
	legacyToastActivatorID   = "{66F0C709-6E91-45BE-9013-5A96219E6D3D}"
	legacyToastActivatorPath = classesRootPath + `\CLSID\` +
		legacyToastActivatorID
	legacyToastServerPath = legacyToastActivatorPath + `\LocalServer32`
)

var ErrRegistrationOwned = errors.New(
	"shellintegration: pccontroller-desktop protocol is registered to another executable",
)

type registrationValue struct {
	path  string
	name  string
	value string
}

type registrationSpec struct {
	executable string
	command    string
	values     []registrationValue
}

// RegistrationCommand returns the exact command stored for the optional
// protocol handler. Both the executable and URI argument are quoted.
func RegistrationCommand(executable string) (string, error) {
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		return "", err
	}
	return spec.command, nil
}

func makeRegistrationSpec(executable string) (registrationSpec, error) {
	absolute, err := validatedExecutable(executable)
	if err != nil {
		return registrationSpec{}, err
	}
	command := `"` + absolute + `" /uri "` + protocolArgumentToken + `"`
	icon := `"` + absolute + `",0`
	return registrationSpec{
		executable: absolute,
		command:    command,
		values: []registrationValue{
			{path: protocolRootPath, value: protocolDisplayName},
			{path: protocolRootPath, name: protocolURLValueName, value: ""},
			{path: protocolIconPath, value: icon},
			// Write the ownership-bearing command last. If a new
			// registration fails before this point, the platform registrar
			// can remove the incomplete tree without displacing an owner.
			{path: protocolOpenPath, value: command},
		},
	}, nil
}

func validatedExecutable(executable string) (string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return "", errors.New("shellintegration: executable path is empty")
	}
	if strings.ContainsAny(executable, "\"\x00\r\n") {
		return "", errors.New("shellintegration: executable path contains an unsafe character")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("shellintegration: resolve executable path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("shellintegration: inspect executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("shellintegration: executable path is not a regular file")
	}
	return absolute, nil
}

func registrationOwnedBy(command, expected string) bool {
	return strings.EqualFold(strings.TrimSpace(command), strings.TrimSpace(expected))
}

func activationOwnedBy(command, executable string) bool {
	command = strings.TrimSpace(command)
	if len(command) >= 2 && command[0] == '"' && command[len(command)-1] == '"' {
		command = command[1 : len(command)-1]
	}
	if command == "" || strings.ContainsAny(command, "\"\x00\r\n") {
		return false
	}
	return strings.EqualFold(filepath.Clean(command), filepath.Clean(executable))
}

type OwnershipError struct {
	RegisteredCommand string
}

func (e *OwnershipError) Error() string {
	if e == nil || strings.TrimSpace(e.RegisteredCommand) == "" {
		return ErrRegistrationOwned.Error()
	}
	return fmt.Sprintf(
		"%s: current command is %q",
		ErrRegistrationOwned,
		e.RegisteredCommand,
	)
}

func (e *OwnershipError) Unwrap() error {
	return ErrRegistrationOwned
}
