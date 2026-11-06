package commands

import (
	"context"
	"errors"
	"testing"

	"pccontroller.local/desktop/internal/comserver"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/shellintegration"
)

func TestRegistrationStatusIsReadOnlyAndCurrentExecutableSpecific(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	var mutationCalls int
	dispatcher.registration = &registrationController{actions: registrationActions{
		executable: func() (string, error) {
			return `C:\Portable\PCControllerDesktop.exe`, nil
		},
		shellRegistered: func(executable string) (bool, error) {
			return executable == `C:\Portable\PCControllerDesktop.exe`, nil
		},
		comRegistered: func(executable string) (bool, error) {
			return false, nil
		},
		registerShell: func(string) error {
			mutationCalls++
			return nil
		},
		unregisterShell: func(string) error {
			mutationCalls++
			return nil
		},
		registerCOM: func(string) error {
			mutationCalls++
			return nil
		},
		unregisterCOM: func(string) error {
			mutationCalls++
			return nil
		},
	}}

	result, err := dispatcher.Handler(
		context.Background(),
		request(t, "integration.registration.status", nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := result.(RegistrationStatus)
	if !ok {
		t.Fatalf("status type = %T", result)
	}
	if status.Executable != `C:\Portable\PCControllerDesktop.exe` ||
		!status.ShellRegistered || status.COMRegistered {
		t.Fatalf("registration status = %+v", status)
	}
	if mutationCalls != 0 {
		t.Fatalf("read-only status invoked %d mutators", mutationCalls)
	}
}

func TestRegistrationCOMOwnershipConflictIsCoded(t *testing.T) {
	err := registrationCommandError(
		comserver.ErrRegistrationOwnedByAnotherExecutable,
	)
	assertCommandCode(t, err, "conflict")
}

func TestRegistrationActionsReturnFreshStatus(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	var shellRegistered, comRegistered bool
	dispatcher.registration = &registrationController{actions: registrationActions{
		executable: func() (string, error) {
			return `C:\Portable\PCControllerDesktop.exe`, nil
		},
		shellRegistered: func(string) (bool, error) {
			return shellRegistered, nil
		},
		comRegistered: func(string) (bool, error) {
			return comRegistered, nil
		},
		registerShell: func(string) error {
			shellRegistered = true
			return nil
		},
		unregisterShell: func(string) error {
			shellRegistered = false
			return nil
		},
		registerCOM: func(string) error {
			comRegistered = true
			return nil
		},
		unregisterCOM: func(string) error {
			comRegistered = false
			return nil
		},
	}}

	for _, test := range []struct {
		method    string
		wantShell bool
		wantCOM   bool
	}{
		{"integration.shell.register", true, false},
		{"integration.com.register", true, true},
		{"integration.shell.unregister", false, true},
		{"integration.com.unregister", false, false},
	} {
		result, err := dispatcher.Handler(
			context.Background(),
			request(t, test.method, nil),
		)
		if err != nil {
			t.Fatalf("%s: %v", test.method, err)
		}
		status := result.(RegistrationStatus)
		if status.ShellRegistered != test.wantShell ||
			status.COMRegistered != test.wantCOM {
			t.Fatalf("%s status = %+v", test.method, status)
		}
	}
}

func TestRegistrationPartialFailureIsCodedAndRetainsState(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	shellRegistered := false
	partialCause := errors.New("Jump Lists disabled by policy")
	dispatcher.registration = &registrationController{actions: registrationActions{
		executable: func() (string, error) {
			return `C:\Portable\PCControllerDesktop.exe`, nil
		},
		shellRegistered: func(string) (bool, error) {
			return shellRegistered, nil
		},
		comRegistered: func(string) (bool, error) { return false, nil },
		registerShell: func(string) error {
			shellRegistered = true
			return &shellintegration.PartialError{
				Completed: "URI registered",
				Failed:    "Jump List publication",
				Err:       partialCause,
			}
		},
	}}

	_, err := dispatcher.Handler(
		context.Background(),
		request(t, "integration.shell.register", nil),
	)
	assertCommandCode(t, err, "partial")
	result, err := dispatcher.Handler(
		context.Background(),
		request(t, "integration.registration.status", nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.(RegistrationStatus).ShellRegistered {
		t.Fatal("partial Jump List failure did not retain observable URI ownership")
	}
}

func TestRegistrationCommandsRejectParameters(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	for _, method := range []string{
		"integration.registration.status",
		"integration.shell.register",
		"integration.shell.unregister",
		"integration.com.register",
		"integration.com.unregister",
	} {
		_, err := dispatcher.Handler(
			context.Background(),
			ipc.Request{
				ProtocolVersion: ipc.ProtocolVersion,
				Method:          method,
				Params:          []byte(`{"unexpected":true}`),
			},
		)
		assertCommandCode(t, err, "invalid_argument")
	}
}
