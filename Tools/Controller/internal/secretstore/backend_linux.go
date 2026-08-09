//go:build linux

package secretstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type linuxSecretBackend struct {
	namespace string
	toolPath  string
	run       func([]byte, string, ...string) ([]byte, error)
}

var linuxSecretLookPath = exec.LookPath

var linuxSecretRun = func(input []byte, name string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdin = bytes.NewReader(input)
	return command.Output()
}

func newPlatformBackend(namespace string) Backend {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "pccontroller"
	}
	path, err := linuxSecretLookPath("secret-tool")
	if err != nil {
		return unavailableBackend{}
	}
	return &linuxSecretBackend{namespace: namespace, toolPath: path, run: linuxSecretRun}
}

func (backend *linuxSecretBackend) Status() Status {
	available := backend != nil && backend.toolPath != "" && backend.run != nil
	return Status{Provider: "libsecret", Available: available, Durable: available, Scope: "current-user"}
}

func (backend *linuxSecretBackend) Get(name string) (string, error) {
	if !backend.available() {
		return "", ErrUnavailable
	}
	arguments := append([]string{"lookup"}, backend.attributes(name)...)
	output, err := backend.run(nil, backend.toolPath, arguments...)
	if err != nil {
		if commandReportsMissingSecret(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("libsecret lookup failed: %w", redactedCommandError(err))
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r")
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

func (backend *linuxSecretBackend) Set(name, value string) error {
	if !backend.available() {
		return ErrUnavailable
	}
	arguments := append([]string{"store", "--label=PCController credential"}, backend.attributes(name)...)
	if _, err := backend.run([]byte(value+"\n"), backend.toolPath, arguments...); err != nil {
		return fmt.Errorf("libsecret store failed: %w", redactedCommandError(err))
	}
	return nil
}

func (backend *linuxSecretBackend) Delete(name string) error {
	if !backend.available() {
		return ErrUnavailable
	}
	arguments := append([]string{"clear"}, backend.attributes(name)...)
	if _, err := backend.run(nil, backend.toolPath, arguments...); err != nil {
		if commandReportsMissingSecret(err) {
			return ErrNotFound
		}
		return fmt.Errorf("libsecret delete failed: %w", redactedCommandError(err))
	}
	return nil
}

func (backend *linuxSecretBackend) available() bool {
	return backend != nil && backend.toolPath != "" && backend.run != nil
}

func (backend *linuxSecretBackend) attributes(name string) []string {
	return []string{"application", backend.namespace, "key", name}
}

type exitCoder interface{ ExitCode() int }

func commandExitCode(err error) int {
	var coded exitCoder
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return -1
}

func commandReportsMissingSecret(err error) bool {
	if commandExitCode(err) != 1 {
		return false
	}
	var exitError *exec.ExitError
	return !errors.As(err, &exitError) || len(bytes.TrimSpace(exitError.Stderr)) == 0
}

// redactedCommandError deliberately discards stderr and command arguments.
// Secret values are sent over stdin and must never become diagnostics.
func redactedCommandError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New(err.Error())
}
