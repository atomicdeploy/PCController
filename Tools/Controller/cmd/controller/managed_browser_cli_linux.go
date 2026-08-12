//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/managedbrowser"
	"pccontroller.local/controller/internal/runtimeinstall"
)

type managedBrowserSession interface {
	Wait(context.Context) error
	Close(context.Context) error
}

func runToolchainRuntimeWindowOpen(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	flags := flag.NewFlagSet("toolchain runtime-window-open", flag.ContinueOnError)
	flags.SetOutput(stderr)
	browser := flags.String("browser", "", "root-owned Google Chrome or Chromium executable")
	target := flags.String("url", "", "clean loopback PCController WebUI URL")
	profile := flags.String("profile", "", "dedicated Chrome application profile directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*browser) == "" || strings.TrimSpace(*target) == "" || strings.TrimSpace(*profile) == "" {
		return errors.New("usage: controller toolchain runtime-window-open --browser FILE --url http://127.0.0.1:8787/ --profile DIR")
	}
	launcher, err := runtimeinstall.ValidateBrowser(*browser)
	if err != nil {
		return err
	}
	nativeBrowser, err := runtimeinstall.BrowserMainExecutable(launcher)
	if err != nil {
		return err
	}
	runtimeConfig := store.CurrentRuntime()
	if err = validateManagedTarget(*target, runtimeConfig.IPC.Listen); err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	session, err := managedbrowser.Start(ctx, managedbrowser.Options{
		Executable: nativeBrowser,
		URL:        *target,
		ProfileDir: *profile,
		Token:      runtimeConfig.IPC.AuthToken,
		Stdout:     stdout,
		Stderr:     stderr,
	})
	if err != nil {
		return err
	}
	if err = announceManagedBrowserReady(session, notifyServiceReady); err != nil {
		return err
	}
	return session.Wait(ctx)
}

func announceManagedBrowserReady(session managedBrowserSession, notify func() error) error {
	if err := notify(); err != nil {
		closeContext, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		return errors.Join(err, session.Close(closeContext))
	}
	return nil
}

func validateManagedTarget(target, configuredListen string) error {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Scheme != "http" || parsed.Host == "" || parsed.Port() == "" || parsed.User != nil || parsed.RawQuery != "" {
		return errors.New("managed browser target must be a clean absolute loopback HTTP URL")
	}
	address := net.ParseIP(strings.Trim(parsed.Hostname(), "[]"))
	if address == nil || !address.IsLoopback() {
		return errors.New("managed browser target must use a literal loopback address")
	}
	_, configuredPort, err := net.SplitHostPort(strings.TrimSpace(configuredListen))
	if err != nil || configuredPort == "" || configuredPort != parsed.Port() {
		return errors.New("managed browser target port does not match the configured IPC listener")
	}
	return nil
}

func notifyServiceReady() error {
	name := strings.TrimSpace(os.Getenv("NOTIFY_SOCKET"))
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "@") {
		name = "\x00" + strings.TrimPrefix(name, "@")
	} else if !filepath.IsAbs(name) {
		return errors.New("systemd notification socket is not absolute")
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("connect systemd notification socket: %w", err)
	}
	defer connection.Close()
	if _, err = connection.Write([]byte("READY=1\nSTATUS=Managed PCController browser authenticated\n")); err != nil {
		return fmt.Errorf("notify systemd of managed browser readiness: %w", err)
	}
	return nil
}

func openAuthenticatedBrowser(value string, store *appconfig.Store) error {
	if store == nil || strings.TrimSpace(store.CurrentRuntime().IPC.AuthToken) == "" {
		return openBrowser(value)
	}
	if err := validateManagedTarget(value, store.CurrentRuntime().IPC.Listen); err != nil {
		// Non-loopback browser navigation remains an explicit/manual token path.
		return openBrowser(value)
	}
	browser, err := resolveRuntimeBrowser("")
	if err != nil {
		return err
	}
	browser, err = runtimeinstall.ValidateBrowser(browser)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	profile, err := managedBrowserProfileDirectory()
	if err != nil {
		return err
	}
	command := exec.Command(
		executable,
		"--config", store.Path(),
		"toolchain", "runtime-window-open",
		"--browser", browser,
		"--url", value,
		"--profile", profile,
	)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.Env = environmentWithoutSecret(os.Environ(), store.CurrentRuntime().IPC.AuthToken)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func environmentWithoutSecret(environment []string, secret string) []string {
	secret = strings.TrimSpace(secret)
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		_, value, found := strings.Cut(entry, "=")
		if found && secret != "" && strings.Contains(value, secret) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func managedBrowserProfileDirectory() (string, error) {
	data := strings.TrimSpace(os.Getenv("PCCONTROLLER_DATA_DIR"))
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share", "pccontroller")
	}
	absolute, err := filepath.Abs(data)
	if err != nil {
		return "", err
	}
	return filepath.Join(absolute, "chrome-profile"), nil
}
