package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"rayanlamp.local/app/internal/comserver"
	"rayanlamp.local/app/internal/shellintegration"
)

// RegistrationStatus is a read-only snapshot of registrations owned by the
// exact executable hosting the authoritative RayanLamp dispatcher.
type RegistrationStatus struct {
	Executable      string `json:"executable"`
	ShellRegistered bool   `json:"shell_registered"`
	COMRegistered   bool   `json:"com_registered"`
}

const (
	MethodIntegrationRegistrationStatus = "integration.registration.status"
	MethodIntegrationShellRegister      = "integration.shell.register"
	MethodIntegrationShellUnregister    = "integration.shell.unregister"
	MethodIntegrationCOMRegister        = "integration.com.register"
	MethodIntegrationCOMUnregister      = "integration.com.unregister"
)

type registrationActions struct {
	executable      func() (string, error)
	shellRegistered func(string) (bool, error)
	comRegistered   func(string) (bool, error)
	registerShell   func(string) error
	unregisterShell func(string) error
	registerCOM     func(string) error
	unregisterCOM   func(string) error
}

type registrationController struct {
	mu      sync.Mutex
	actions registrationActions
}

func defaultRegistrationController() *registrationController {
	return &registrationController{actions: registrationActions{
		executable:      os.Executable,
		shellRegistered: shellintegration.IsRegistered,
		comRegistered:   comserver.IsRegistered,
		registerShell:   shellintegration.RegisterShell,
		unregisterShell: shellintegration.UnregisterShell,
		registerCOM:     comserver.Register,
		unregisterCOM:   comserver.UnregisterExecutable,
	}}
}

func (controller *registrationController) handle(method string) (any, error) {
	if controller == nil {
		return nil, commandError{
			"unavailable",
			"Windows registration services are unavailable",
		}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()

	switch method {
	case MethodIntegrationRegistrationStatus:
		return controller.status()
	case MethodIntegrationShellRegister:
		if err := controller.withExecutable(controller.actions.registerShell); err != nil {
			return nil, registrationCommandError(err)
		}
	case MethodIntegrationShellUnregister:
		if err := controller.withExecutable(controller.actions.unregisterShell); err != nil {
			return nil, registrationCommandError(err)
		}
	case MethodIntegrationCOMRegister:
		if err := controller.withExecutable(controller.actions.registerCOM); err != nil {
			return nil, registrationCommandError(err)
		}
	case MethodIntegrationCOMUnregister:
		if err := controller.withExecutable(controller.actions.unregisterCOM); err != nil {
			return nil, registrationCommandError(err)
		}
	default:
		return nil, commandError{"not_found", fmt.Sprintf("unknown command %q", method)}
	}
	return controller.status()
}

func (controller *registrationController) withExecutable(
	action func(string) error,
) error {
	if action == nil {
		return errors.New("registration action is unavailable")
	}
	executable, err := controller.currentExecutable()
	if err != nil {
		return err
	}
	return action(executable)
}

func (controller *registrationController) status() (RegistrationStatus, error) {
	executable, err := controller.currentExecutable()
	if err != nil {
		return RegistrationStatus{}, registrationCommandError(err)
	}
	if controller.actions.shellRegistered == nil ||
		controller.actions.comRegistered == nil {
		return RegistrationStatus{}, commandError{
			"unavailable",
			"registration status services are unavailable",
		}
	}
	shellRegistered, shellErr :=
		controller.actions.shellRegistered(executable)
	comRegistered, comErr :=
		controller.actions.comRegistered(executable)
	if err := errors.Join(shellErr, comErr); err != nil {
		return RegistrationStatus{}, registrationCommandError(err)
	}
	return RegistrationStatus{
		Executable:      executable,
		ShellRegistered: shellRegistered,
		COMRegistered:   comRegistered,
	}, nil
}

func (controller *registrationController) currentExecutable() (string, error) {
	if controller.actions.executable == nil {
		return "", errors.New("current executable lookup is unavailable")
	}
	executable, err := controller.actions.executable()
	if err != nil {
		return "", fmt.Errorf("locate current executable: %w", err)
	}
	if executable == "" {
		return "", errors.New("current executable path is empty")
	}
	return filepath.Clean(executable), nil
}

func registrationCommandError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case shellintegration.IsPartial(err):
		return commandError{"partial", err.Error()}
	case errors.Is(err, shellintegration.ErrRegistrationOwned),
		errors.Is(err, comserver.ErrRegistrationOwnedByAnotherExecutable):
		return commandError{"conflict", err.Error()}
	case errors.Is(err, shellintegration.ErrUnsupported),
		errors.Is(err, comserver.ErrUnsupported):
		return commandError{"unavailable", err.Error()}
	default:
		return commandError{"registration_failed", err.Error()}
	}
}
