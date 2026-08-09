//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type capturedLinuxProvisionCall struct {
	command     linuxHostProvisionCommand
	environment []string
}

func installLinuxProvisionTestHooks(t *testing.T, reusable, member bool) *[]capturedLinuxProvisionCall {
	t.Helper()
	originalCurrentEUID := linuxHostProvisionCurrentEUID
	originalLookupUser := linuxHostProvisionLookupUser
	originalLookupGroup := linuxHostProvisionLookupGroup
	originalUserGroups := linuxHostProvisionUserGroups
	originalLookPath := linuxHostProvisionLookPath
	originalStat := linuxHostProvisionStat
	originalExecutable := linuxHostProvisionExecutable
	originalPathIdentity := linuxHostProvisionPathIdentity
	originalRun := linuxHostProvisionRun
	t.Cleanup(func() {
		linuxHostProvisionCurrentEUID = originalCurrentEUID
		linuxHostProvisionLookupUser = originalLookupUser
		linuxHostProvisionLookupGroup = originalLookupGroup
		linuxHostProvisionUserGroups = originalUserGroups
		linuxHostProvisionLookPath = originalLookPath
		linuxHostProvisionStat = originalStat
		linuxHostProvisionExecutable = originalExecutable
		linuxHostProvisionPathIdentity = originalPathIdentity
		linuxHostProvisionRun = originalRun
	})
	linuxHostProvisionCurrentEUID = func() int { return 0 }
	linuxHostProvisionLookupUser = func(name string) (*user.User, error) {
		if name != "alice" {
			return nil, user.UnknownUserError(name)
		}
		return &user.User{Uid: "1000", Gid: "1000", Username: "alice", HomeDir: "/home/alice"}, nil
	}
	linuxHostProvisionLookupGroup = func(name string) (*user.Group, error) {
		if name != "dialout" {
			return nil, user.UnknownGroupError(name)
		}
		return &user.Group{Gid: "20", Name: "dialout"}, nil
	}
	linuxHostProvisionUserGroups = func(*user.User) ([]string, error) {
		groups := []string{"1000"}
		if member {
			groups = append(groups, "20")
		}
		return groups, nil
	}
	paths := map[string]linuxPathIdentity{
		"/home/alice": {Exists: true, UID: 1000, IsDir: true},
	}
	if reusable {
		for _, path := range []string{
			"/home/alice/.local",
			"/home/alice/.local/share",
			"/home/alice/.local/share/pccontroller",
			"/home/alice/.local/share/pccontroller/tools",
			"/home/alice/.local/share/pccontroller/tools/toolchain",
		} {
			paths[path] = linuxPathIdentity{Exists: true, UID: 1000, IsDir: true}
		}
		paths["/home/alice/.local/share/pccontroller/tools/toolchain/firmware-cli.yaml"] = linuxPathIdentity{
			Exists: true, UID: 1000,
		}
	}
	linuxHostProvisionPathIdentity = func(path string) (linuxPathIdentity, error) {
		return paths[filepath.Clean(path)], nil
	}
	commands := map[string]string{
		"apt-get": "/usr/bin/apt-get", "runuser": "/usr/sbin/runuser",
		"usermod": "/usr/sbin/usermod", "upx": "/usr/local/bin/upx",
		"npm": "/usr/bin/npm", "ln": "/usr/bin/ln",
	}
	linuxHostProvisionLookPath = func(name string) (string, error) {
		if path := commands[name]; path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
	linuxHostProvisionExecutable = func() (string, error) { return "/opt/pccontroller/controller", nil }
	var calls []capturedLinuxProvisionCall
	linuxHostProvisionRun = func(
		_ context.Context,
		command linuxHostProvisionCommand,
		environment []string,
		_ io.Writer,
	) error {
		calls = append(calls, capturedLinuxProvisionCall{
			command: linuxHostProvisionCommand{
				Name: command.Name, Args: append([]string(nil), command.Args...), Dir: command.Dir,
			},
			environment: append([]string(nil), environment...),
		})
		return nil
	}
	return &calls
}

func TestLinuxHostProvisionDryRunChecksPackagesWithoutMutationOrSecretLeak(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, false, false)
	var output bytes.Buffer
	report, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", DirectRetry: true,
		Environment: []string{
			"PATH=/untrusted", "HTTPS_PROXY=http://name:top-secret@proxy.invalid:8080",
			"NO_PROXY=localhost", "PRIVATE_TOKEN=must-not-reach-child",
		},
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || !report.PackageAvailabilityChecked || report.ToolchainStateBefore != "fresh" {
		t.Fatalf("dry-run report has wrong state: %+v", report)
	}
	if len(*calls) != 1 || !containsArgument((*calls)[0].command.Args, "--simulate") {
		t.Fatalf("dry-run executed something other than the read-only package simulation: %+v", *calls)
	}
	environment := strings.Join((*calls)[0].environment, "\n")
	if !strings.Contains(environment, "top-secret") || strings.Contains(environment, "PRIVATE_TOKEN") {
		t.Fatalf("filtered child environment omitted proxy or leaked unrelated root state: %s", environment)
	}
	text := output.String()
	if strings.Contains(text, "top-secret") || strings.Contains(text, "must-not-reach-child") {
		t.Fatalf("dry-run output leaked an environment value: %s", text)
	}
	for _, step := range report.Steps {
		if step.Mutating && !step.Planned {
			t.Fatalf("dry-run mutating step was not plan-only: %+v", step)
		}
	}
	if !strings.Contains(text, "no host, account, or PCController state was changed") {
		t.Fatalf("dry-run did not state its safety boundary: %s", text)
	}
}

func TestLinuxHostProvisionApplyFreshPathUsesControllerAsTargetUser(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, false, false)
	report, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", Apply: true, Locked: true,
		LockPath:    "/srv/projects/PCController/Tools/Controller/toolchain-lock.json",
		DirectRetry: false, Environment: []string{"HTTP_PROXY=http://proxy.invalid"},
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !report.SerialAccessChanged || !report.ReloginRequired || report.ToolchainStateBefore != "fresh" {
		t.Fatalf("fresh provision report is incomplete: %+v", report)
	}
	if report.UPXPath != "/usr/local/bin/upx" {
		t.Fatalf("global UPX path=%q", report.UPXPath)
	}
	if len(*calls) != 6 {
		t.Fatalf("fresh provision commands=%d want apt update/install, npm/UPX verify, usermod, Controller bootstrap: %+v", len(*calls), *calls)
	}
	if got := (*calls)[0].command; got.Name != "/usr/bin/apt-get" || !containsArgument(got.Args, "update") {
		t.Fatalf("first command did not refresh APT: %+v", got)
	}
	install := (*calls)[1].command
	for _, wanted := range []string{
		"brightnessctl", "libsecret-tools", "libnotify-bin", "ddcutil", "desktop-file-utils",
		"xdg-utils", "ca-certificates", "build-essential", "pkg-config", "cmake", "ninja-build",
		"git", "curl", "ripgrep", "upx-ucl", "golang-go", "nodejs",
	} {
		if !containsArgument(install.Args, wanted) {
			t.Errorf("native package install omitted %q: %q", wanted, install.Args)
		}
	}
	if got := (*calls)[2].command; got.Name != "/usr/bin/npm" || !reflect.DeepEqual(got.Args, []string{"--version"}) {
		t.Fatalf("npm was not verified through its selected provider command: %+v", got)
	}
	if got := (*calls)[3].command; got.Name != "/usr/local/bin/upx" || !reflect.DeepEqual(got.Args, []string{"--version"}) {
		t.Fatalf("UPX was not verified through its global command: %+v", got)
	}
	if got := (*calls)[4].command; got.Name != "/usr/sbin/usermod" || !reflect.DeepEqual(got.Args, []string{"--append", "--groups", "dialout", "alice"}) {
		t.Fatalf("serial membership command is not tightly scoped: %+v", got)
	}
	bootstrap := (*calls)[5].command
	if bootstrap.Name != "/usr/sbin/runuser" {
		t.Fatalf("toolchain did not cross the target-user boundary: %+v", bootstrap)
	}
	if bootstrap.Dir != "/home/alice" {
		t.Fatalf("target-user bootstrap retained a privileged caller working directory: %+v", bootstrap)
	}
	wantPrefix := []string{
		"--user", "alice", "--", "/opt/pccontroller/controller", "toolchain", "bootstrap",
		"--install-dir", "/home/alice/.local/share/pccontroller/tools/toolchain",
		"--direct-retry=false", "--locked", "--lock",
		"/srv/projects/PCController/Tools/Controller/toolchain-lock.json",
	}
	if !reflect.DeepEqual(bootstrap.Args, wantPrefix) {
		t.Fatalf("target-user bootstrap args=%q want %q", bootstrap.Args, wantPrefix)
	}
	for _, call := range *calls {
		if strings.Contains(strings.ToLower(call.command.Name), "arduino") {
			t.Fatalf("host provisioner invoked dependency backend directly: %+v", call.command)
		}
	}
}

func TestLinuxHostProvisionReusesTargetOwnedToolchainAndSerialMembership(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, true, true)
	report, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", Apply: true, DirectRetry: true,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if report.ToolchainStateBefore != "reusable-target-owned" || report.SerialAccessChanged || report.ReloginRequired {
		t.Fatalf("reuse report has wrong ownership/membership semantics: %+v", report)
	}
	for _, call := range *calls {
		if call.command.Name == "/usr/sbin/usermod" {
			t.Fatalf("reuse path modified an existing serial membership: %+v", call.command)
		}
	}
	last := (*calls)[len(*calls)-1].command
	if last.Name != "/usr/sbin/runuser" || !containsArgument(last.Args, "/home/alice/.local/share/pccontroller/tools/toolchain") {
		t.Fatalf("reuse path did not reconcile the target-owned toolchain through Controller: %+v", last)
	}
}

func TestLinuxHostProvisionApplyRequiresRootBeforeCommands(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, false, false)
	linuxHostProvisionCurrentEUID = func() int { return 1000 }
	_, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", Apply: true,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires root") || len(*calls) != 0 {
		t.Fatalf("unprivileged apply err=%v calls=%+v", err, *calls)
	}
}

func TestLinuxHostProvisionRejectsForeignOwnedCacheInsteadOfChowning(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, false, false)
	linuxHostProvisionPathIdentity = func(path string) (linuxPathIdentity, error) {
		if filepath.Clean(path) == "/home/alice" {
			return linuxPathIdentity{Exists: true, UID: 1000, IsDir: true}, nil
		}
		if filepath.Clean(path) == "/home/alice/.local/share" {
			return linuxPathIdentity{Exists: true, UID: 0, IsDir: true}, nil
		}
		return linuxPathIdentity{}, nil
	}
	_, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", Apply: true,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "instead of chowning") || len(*calls) != 0 {
		t.Fatalf("foreign-owned state err=%v calls=%+v", err, *calls)
	}
}

func TestLinuxHostProvisionIsConfigIndependent(t *testing.T) {
	installLinuxProvisionTestHooks(t, false, false)
	config := filepath.Join(t.TempDir(), "invalid-config.json")
	invalid := []byte(`{"schema":1,"host_menus":{"request_gesture":"invalid"}}`)
	if err := os.WriteFile(config, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"--config", config, "toolchain", "provision-host", "--target-user", "alice", "--dry-run",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("fresh-host plan loaded root runtime config: %v\nstderr: %s", err, stderr.String())
	}
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(after, invalid) {
		t.Fatalf("fresh-host plan changed runtime config: %v\n%s", err, after)
	}
}

func TestLinuxProvisionEnvironmentCanonicalizesCaseInsensitiveProxyValues(t *testing.T) {
	environment := linuxProvisionEnvironment([]string{
		"https_proxy=http://lower.invalid:8080",
		"HTTPS_PROXY=http://upper.invalid:8080",
		"http_proxy=http://lower-http.invalid:8080",
		"PRIVATE_TOKEN=must-not-pass",
	})
	values := make(map[string][]string)
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = append(values[name], value)
		}
	}
	for _, name := range []string{"HTTPS_PROXY", "https_proxy"} {
		if got := values[name]; !reflect.DeepEqual(got, []string{"http://upper.invalid:8080"}) {
			t.Fatalf("%s=%q want deterministic uppercase-precedence value", name, got)
		}
	}
	for _, name := range []string{"HTTP_PROXY", "http_proxy"} {
		if got := values[name]; !reflect.DeepEqual(got, []string{"http://lower-http.invalid:8080"}) {
			t.Fatalf("%s=%q want mirrored lowercase-only value", name, got)
		}
	}
	if strings.Contains(strings.Join(environment, "\n"), "PRIVATE_TOKEN") {
		t.Fatalf("filtered environment leaked unrelated root state: %q", environment)
	}
}

func TestLinuxHostProvisionExpandsAllProxyForAptWithoutLoggingValue(t *testing.T) {
	calls := installLinuxProvisionTestHooks(t, false, false)
	const proxy = "socks5h://proxy-user:top-secret@proxy.invalid:1080"
	var output bytes.Buffer
	report, err := provisionLinuxHost(context.Background(), linuxHostProvisionOptions{
		TargetUser: "alice", Environment: []string{"ALL_PROXY=" + proxy},
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].command.Name != "/usr/bin/apt-get" ||
		!containsArgument((*calls)[0].command.Args, "--simulate") {
		t.Fatalf("expected one read-only apt-get simulation, got %+v", *calls)
	}
	values := make(map[string]string)
	for _, entry := range (*calls)[0].environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	for _, name := range []string{
		"ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy",
	} {
		if values[name] != proxy {
			t.Errorf("apt-get environment %s=%q want ALL_PROXY fallback", name, values[name])
		}
	}
	for _, name := range []string{"ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY"} {
		if !containsArgument(report.ProxyVariables, name) {
			t.Errorf("secret-free proxy variable report omitted %s: %q", name, report.ProxyVariables)
		}
	}
	if strings.Contains(output.String(), "top-secret") || strings.Contains(output.String(), "proxy-user") {
		t.Fatalf("provision output leaked proxy credentials: %s", output.String())
	}
}

func TestTrustedLinuxProvisionExecutableIgnoresCallerPATH(t *testing.T) {
	originalStat := linuxHostProvisionStat
	t.Cleanup(func() { linuxHostProvisionStat = originalStat })
	malicious := filepath.Join(t.TempDir(), "apt-get")
	if err := os.WriteFile(malicious, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(malicious))
	linuxHostProvisionStat = func(path string) (os.FileInfo, error) {
		if path == "/usr/bin/apt-get" {
			return os.Stat(malicious)
		}
		return nil, os.ErrNotExist
	}
	got, err := trustedLinuxHostProvisionExecutable("apt-get")
	if err != nil || got != "/usr/bin/apt-get" {
		t.Fatalf("trusted resolver selected %q, %v; caller PATH=%q", got, err, os.Getenv("PATH"))
	}
	if _, err := trustedLinuxHostProvisionExecutable("../apt-get"); err == nil {
		t.Fatal("trusted resolver accepted a path-bearing privileged command")
	}
}

func containsArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
}
