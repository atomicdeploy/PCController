//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"pccontroller.local/controller/internal/aptmirror"
	"pccontroller.local/controller/internal/netpolicy"
	"pccontroller.local/controller/internal/programmer"
)

// linuxHostNativePackages is deliberately a reviewed, finite profile. It
// contains the source-build tools and the command-line adapters Controller
// actually invokes on Linux. It does not install a permissive udev rule or a
// second, unmanaged firmware toolchain.
var linuxHostNativePackages = []string{
	"brightnessctl",
	"build-essential",
	"ca-certificates",
	"cmake",
	"curl",
	"ddcutil",
	"desktop-file-utils",
	"git",
	"golang-go",
	"libnotify-bin",
	"libsecret-tools",
	"ninja-build",
	"nodejs",
	"pkg-config",
	"ripgrep",
	"gpgv",
	"ubuntu-keyring",
	"upx-ucl",
	"xdg-utils",
}

type linuxHostProvisionOptions struct {
	TargetUser           string
	Apply                bool
	Locked               bool
	PolicyPath           string
	LockPath             string
	DirectRetry          bool
	UbuntuMirrors        string
	MirrorCandidatesPath string
	Environment          []string
}

type linuxHostProvisionStep struct {
	Name      string `json:"name"`
	Command   string `json:"command,omitempty"`
	Mutating  bool   `json:"mutating"`
	Planned   bool   `json:"planned,omitempty"`
	Succeeded bool   `json:"succeeded,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type linuxHostProvisionReport struct {
	Platform                   string                   `json:"platform"`
	Applied                    bool                     `json:"applied"`
	TargetUser                 string                   `json:"target_user"`
	TargetUID                  string                   `json:"target_uid"`
	TargetHome                 string                   `json:"target_home"`
	DataDir                    string                   `json:"data_dir"`
	ToolchainDir               string                   `json:"toolchain_dir"`
	ToolchainStateBefore       string                   `json:"toolchain_state_before"`
	PackageManager             string                   `json:"package_manager"`
	NativePackages             []string                 `json:"native_packages"`
	PackageAvailabilityChecked bool                     `json:"package_availability_checked"`
	SerialGroup                string                   `json:"serial_group"`
	SerialAccessChanged        bool                     `json:"serial_access_changed"`
	ReloginRequired            bool                     `json:"relogin_required"`
	NPMPath                    string                   `json:"npm_path,omitempty"`
	UPXPath                    string                   `json:"upx_path,omitempty"`
	ProxyVariables             []string                 `json:"proxy_variables,omitempty"`
	UbuntuMirrors              *aptmirror.InstallReport `json:"ubuntu_mirrors,omitempty"`
	Steps                      []linuxHostProvisionStep `json:"steps"`
}

type linuxHostProvisionCommand struct {
	Name string
	Args []string
	Dir  string
}

type linuxPathIdentity struct {
	Exists  bool
	UID     uint64
	IsDir   bool
	Symlink bool
}

var (
	linuxHostProvisionCurrentEUID = os.Geteuid
	linuxHostProvisionLookupUser  = user.Lookup
	linuxHostProvisionLookupGroup = user.LookupGroup
	linuxHostProvisionUserGroups  = func(account *user.User) ([]string, error) {
		return account.GroupIds()
	}
	linuxHostProvisionLookPath     = trustedLinuxHostProvisionExecutable
	linuxHostProvisionExecutable   = os.Executable
	linuxHostProvisionStat         = os.Stat
	linuxHostProvisionPathIdentity = func(path string) (linuxPathIdentity, error) {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return linuxPathIdentity{}, nil
		}
		if err != nil {
			return linuxPathIdentity{}, err
		}
		identity := linuxPathIdentity{
			Exists: true, IsDir: info.IsDir(), Symlink: info.Mode()&os.ModeSymlink != 0,
		}
		if status, ok := info.Sys().(*syscall.Stat_t); ok {
			identity.UID = uint64(status.Uid)
		} else {
			return linuxPathIdentity{}, fmt.Errorf("inspect owner of %s: unsupported file metadata", path)
		}
		return identity, nil
	}
	linuxHostProvisionRun = func(
		ctx context.Context,
		command linuxHostProvisionCommand,
		environment []string,
		output io.Writer,
	) error {
		process := exec.CommandContext(ctx, command.Name, command.Args...)
		process.Env = append([]string(nil), environment...)
		process.Dir = command.Dir
		process.Stdout = output
		process.Stderr = output
		return process.Run()
	}
)

var linuxHostProvisionTrustedDirectories = []string{
	"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin",
}

func runToolchainHostProvision(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain provision-host", flag.ContinueOnError)
	flags.SetOutput(stderr)
	targetUser := flags.String("target-user", "", "existing non-root interactive or service account that will own PCController state")
	apply := flags.Bool("apply", false, "execute the reviewed plan as root; without this flag the command is read-only")
	dryRun := flags.Bool("dry-run", false, "validate package availability and print the plan without changing the host")
	policyPath := flags.String("policy", "", "latest-compatible toolchain policy JSON passed to the target-user bootstrap")
	lockPath := flags.String("lock", "", "exact resolved toolchain lock JSON passed to the target-user bootstrap")
	locked := flags.Bool("locked", false, "bootstrap the existing exact lock as the target user")
	directRetry := flags.Bool("direct-retry", true, "retry failed target-user network steps once without proxy variables")
	ubuntuMirrors := flags.String("ubuntu-mirrors", "", "opt-in Ubuntu mirror profile: domestic-first")
	mirrorCandidates := flags.String("mirror-candidates", "", "reviewed JSON array overriding mirror candidates (paths/policy remain Controller-owned)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	const usage = "usage: controller toolchain provision-host --target-user USER [--apply|--dry-run] [--ubuntu-mirrors=domestic-first] [--mirror-candidates FILE] [--locked --lock FILE] [--policy FILE] [--direct-retry=false]"
	if flags.NArg() != 0 {
		return errors.New(usage)
	}
	if strings.TrimSpace(*targetUser) == "" {
		return errors.New(usage)
	}
	if *apply && *dryRun {
		return errors.New("--apply and --dry-run are mutually exclusive")
	}
	if *locked && strings.TrimSpace(*policyPath) != "" {
		return errors.New("--policy cannot be combined with --locked; pass the exact --lock instead")
	}
	if !*locked && strings.TrimSpace(*lockPath) != "" {
		return errors.New("--lock requires --locked")
	}
	if *ubuntuMirrors != "" && !strings.EqualFold(strings.TrimSpace(*ubuntuMirrors), "domestic-first") {
		return errors.New("--ubuntu-mirrors supports only the reviewed domestic-first profile")
	}
	if strings.TrimSpace(*mirrorCandidates) != "" && strings.TrimSpace(*ubuntuMirrors) == "" {
		return errors.New("--mirror-candidates requires --ubuntu-mirrors=domestic-first")
	}

	resolvedPolicy := ""
	resolvedLock := ""
	if *locked {
		resolvedLock = defaultToolchainMetadataPath(*lockPath, "toolchain-lock.json")
		if strings.TrimSpace(resolvedLock) == "" {
			return errors.New("exact toolchain lock could not be resolved; pass --lock FILE")
		}
		var err error
		resolvedLock, err = filepath.Abs(resolvedLock)
		if err != nil {
			return fmt.Errorf("resolve exact toolchain lock: %w", err)
		}
	} else {
		resolvedPolicy = defaultToolchainMetadataPath(*policyPath, "toolchain-profile.json")
		if resolvedPolicy != "" {
			var err error
			resolvedPolicy, err = filepath.Abs(resolvedPolicy)
			if err != nil {
				return fmt.Errorf("resolve toolchain policy: %w", err)
			}
		}
	}

	ctx, cancel := signalContext()
	defer cancel()
	report, err := provisionLinuxHost(ctx, linuxHostProvisionOptions{
		TargetUser: strings.TrimSpace(*targetUser), Apply: *apply,
		Locked: *locked, PolicyPath: resolvedPolicy, LockPath: resolvedLock,
		DirectRetry: *directRetry, UbuntuMirrors: strings.ToLower(strings.TrimSpace(*ubuntuMirrors)),
		MirrorCandidatesPath: strings.TrimSpace(*mirrorCandidates), Environment: os.Environ(),
	}, stdout)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if len(encoded) != 0 {
		fmt.Fprintln(stdout, string(encoded))
	}
	return err
}

func provisionLinuxHost(
	ctx context.Context,
	options linuxHostProvisionOptions,
	output io.Writer,
) (linuxHostProvisionReport, error) {
	if output == nil {
		output = io.Discard
	}
	report := linuxHostProvisionReport{
		Platform: "linux", Applied: options.Apply,
		NativePackages: append([]string(nil), linuxHostNativePackages...),
	}
	if options.Apply && linuxHostProvisionCurrentEUID() != 0 {
		return report, errors.New("Linux host provisioning with --apply requires root; rerun Controller through the system privilege boundary")
	}
	account, err := linuxHostProvisionLookupUser(options.TargetUser)
	if err != nil {
		return report, fmt.Errorf("resolve target account %q: %w", options.TargetUser, err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return report, errors.New("target account must be an existing non-root Linux user")
	}
	home := filepath.Clean(strings.TrimSpace(account.HomeDir))
	if home == "." || home == string(filepath.Separator) || !filepath.IsAbs(home) {
		return report, fmt.Errorf("target account %q has an unsafe home directory %q", account.Username, account.HomeDir)
	}
	report.TargetUser = account.Username
	report.TargetUID = account.Uid
	report.TargetHome = home

	paths, err := programmer.HostDataPathsFor(filepath.Join(home, ".local", "share", "pccontroller"))
	if err != nil {
		return report, err
	}
	report.DataDir = paths.DataDir
	report.ToolchainDir = paths.ToolchainDir
	if err := validateTargetOwnedPath(paths.DataDir, home, uid); err != nil {
		return report, err
	}
	configuration := filepath.Join(paths.ToolchainDir, "firmware-cli.yaml")
	configurationIdentity, err := linuxHostProvisionPathIdentity(configuration)
	if err != nil {
		return report, fmt.Errorf("inspect reusable target-user toolchain: %w", err)
	}
	if configurationIdentity.Exists {
		if configurationIdentity.Symlink || configurationIdentity.IsDir || configurationIdentity.UID != uid {
			return report, fmt.Errorf("refusing unsafe reusable toolchain configuration %s: it must be a target-owned regular file", configuration)
		}
		report.ToolchainStateBefore = "reusable-target-owned"
	} else {
		report.ToolchainStateBefore = "fresh"
	}

	aptGet, err := linuxHostProvisionLookPath("apt-get")
	if err != nil {
		return report, errors.New("Linux host provisioning currently supports Debian/Ubuntu apt-get; no supported package manager was found")
	}
	report.PackageManager = aptGet
	runuser, err := linuxHostProvisionLookPath("runuser")
	if err != nil {
		return report, errors.New("Linux target-user bootstrap requires util-linux runuser")
	}
	executable, err := linuxHostProvisionExecutable()
	if err != nil {
		return report, fmt.Errorf("locate current Controller executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return report, fmt.Errorf("resolve current Controller executable: %w", err)
	}
	if options.UbuntuMirrors == "domestic-first" {
		mirrorReport, mirrorErr := provisionLinuxUbuntuMirrors(ctx, options, executable, output, &report)
		report.UbuntuMirrors = &mirrorReport
		if mirrorErr != nil {
			return report, mirrorErr
		}
	}

	group, alreadyMember, err := linuxSerialGroup(account)
	if err != nil {
		return report, err
	}
	report.SerialGroup = group.Name
	environment := linuxProvisionEnvironment(options.Environment)
	report.ProxyVariables = linuxProvisionProxyNames(environment)
	packageEnvironment := append(append([]string(nil), environment...), "DEBIAN_FRONTEND=noninteractive")

	mode := "dry-run"
	if options.Apply {
		mode = "apply"
	}
	fmt.Fprintf(output, "Linux fresh-host provision %s for %s (uid %s)\n", mode, account.Username, account.Uid)
	fmt.Fprintln(output, "Target-owned PCController data:", paths.DataDir)
	if len(report.ProxyVariables) == 0 {
		fmt.Fprintln(output, "Proxy environment: none configured")
	} else {
		fmt.Fprintln(output, "Proxy environment inherited by name only:", strings.Join(report.ProxyVariables, ", "))
	}

	if !options.Apply {
		check := linuxHostProvisionCommand{
			Name: aptGet,
			Args: append([]string{
				"-o", "DPkg::Lock::Timeout=120", "--simulate", "install", "--no-install-recommends",
			}, linuxHostNativePackages...),
		}
		step := linuxHostProvisionStep{
			Name: "validate native package profile", Command: formatLinuxProvisionCommand(check),
			Mutating: false,
		}
		fmt.Fprintln(output, "\n▶", step.Name, "(read-only)")
		fmt.Fprintln(output, step.Command)
		if err := linuxHostProvisionRun(ctx, check, packageEnvironment, output); err != nil {
			report.Steps = append(report.Steps, step)
			return report, fmt.Errorf("native package profile is unavailable from the currently configured APT sources: %w", err)
		}
		step.Succeeded = true
		report.PackageAvailabilityChecked = true
		report.Steps = append(report.Steps, step)
	}

	packageCommands := []struct {
		name    string
		command linuxHostProvisionCommand
	}{
		{
			name: "refresh native package indexes",
			command: linuxHostProvisionCommand{
				Name: aptGet, Args: []string{"-o", "DPkg::Lock::Timeout=120", "update"},
			},
		},
		{
			name: "install PCController native package profile",
			command: linuxHostProvisionCommand{
				Name: aptGet,
				Args: append([]string{
					"-o", "DPkg::Lock::Timeout=120", "install", "--yes", "--no-install-recommends",
				}, linuxHostNativePackages...),
			},
		},
	}
	for _, configured := range packageCommands {
		step := linuxHostProvisionStep{
			Name: configured.name, Command: formatLinuxProvisionCommand(configured.command), Mutating: true,
		}
		fmt.Fprintln(output, "\n▶", step.Name)
		fmt.Fprintln(output, step.Command)
		if !options.Apply {
			step.Planned = true
			fmt.Fprintln(output, "  dry-run: not executed")
			report.Steps = append(report.Steps, step)
			continue
		}
		if err := linuxHostProvisionRun(ctx, configured.command, packageEnvironment, output); err != nil {
			report.Steps = append(report.Steps, step)
			return report, fmt.Errorf("%s: %w", step.Name, err)
		}
		step.Succeeded = true
		report.Steps = append(report.Steps, step)
	}
	if !options.Apply {
		report.Steps = append(report.Steps, linuxHostProvisionStep{
			Name: "verify npm supplied by selected Node.js package", Mutating: true, Planned: true,
			Detail: "use npm supplied by nodejs when available; otherwise install the distribution npm package through APT after Node.js selection",
		})
		fmt.Fprintln(output, "\n▶ verify npm supplied by selected Node.js package")
		fmt.Fprintln(output, "  dry-run: npm will be verified after Node.js selection and installed through APT only if the selected provider does not include it")
		report.Steps = append(report.Steps, linuxHostProvisionStep{
			Name: "verify globally discoverable UPX", Mutating: true, Planned: true,
			Detail: "use an existing upx command or publish the distribution upx-ucl executable as /usr/local/bin/upx without overwriting a foreign path",
		})
		fmt.Fprintln(output, "\n▶ verify globally discoverable UPX")
		fmt.Fprintln(output, "  dry-run: verify upx, or safely link the reviewed upx-ucl package command at /usr/local/bin/upx")
	} else {
		npmPath, npmErr := ensureLinuxNPM(ctx, aptGet, packageEnvironment, environment, output, &report)
		if npmErr != nil {
			return report, npmErr
		}
		report.NPMPath = npmPath
		upxPath, upxErr := ensureLinuxGlobalUPX(ctx, environment, output, &report)
		if upxErr != nil {
			return report, upxErr
		}
		report.UPXPath = upxPath
	}

	if alreadyMember {
		report.Steps = append(report.Steps, linuxHostProvisionStep{
			Name: "grant serial access", Mutating: true, Skipped: true, Succeeded: true,
			Detail: fmt.Sprintf("%s is already a member of %s", account.Username, group.Name),
		})
		fmt.Fprintf(output, "\n✅ %s already has serial access through %s\n", account.Username, group.Name)
	} else {
		usermod, lookupErr := linuxHostProvisionLookPath("usermod")
		if lookupErr != nil {
			return report, errors.New("serial access requires shadow-utils usermod; the existing serial group was not changed")
		}
		command := linuxHostProvisionCommand{
			Name: usermod, Args: []string{"--append", "--groups", group.Name, account.Username},
		}
		step := linuxHostProvisionStep{
			Name: "grant serial access", Command: formatLinuxProvisionCommand(command), Mutating: true,
		}
		fmt.Fprintln(output, "\n▶", step.Name)
		fmt.Fprintln(output, step.Command)
		if !options.Apply {
			step.Planned = true
			fmt.Fprintln(output, "  dry-run: not executed; no udev rule will be installed")
		} else if err := linuxHostProvisionRun(ctx, command, environment, output); err != nil {
			report.Steps = append(report.Steps, step)
			return report, fmt.Errorf("grant %s membership in existing %s group: %w", account.Username, group.Name, err)
		} else {
			step.Succeeded = true
			report.SerialAccessChanged = true
			report.ReloginRequired = true
		}
		report.Steps = append(report.Steps, step)
	}

	bootstrapArguments := []string{
		"--user", account.Username, "--", executable,
		"toolchain", "bootstrap", "--install-dir", paths.ToolchainDir,
		fmt.Sprintf("--direct-retry=%t", options.DirectRetry),
	}
	if options.Locked {
		bootstrapArguments = append(bootstrapArguments, "--locked", "--lock", options.LockPath)
	} else if options.PolicyPath != "" {
		bootstrapArguments = append(bootstrapArguments, "--policy", options.PolicyPath)
	}
	bootstrap := linuxHostProvisionCommand{Name: runuser, Args: bootstrapArguments, Dir: home}
	step := linuxHostProvisionStep{
		Name:    "bootstrap managed firmware toolchain as target user",
		Command: formatLinuxProvisionCommand(bootstrap), Mutating: true,
		Detail: "target-user working directory: " + home,
	}
	fmt.Fprintln(output, "\n▶", step.Name)
	fmt.Fprintln(output, step.Command)
	if !options.Apply {
		step.Planned = true
		fmt.Fprintln(output, "  dry-run: not executed; Controller remains the only firmware dependency entrypoint")
		report.Steps = append(report.Steps, step)
		fmt.Fprintln(output, "\nDry-run complete; no host, account, or PCController state was changed.")
		return report, nil
	}
	if err := linuxHostProvisionRun(ctx, bootstrap, environment, output); err != nil {
		report.Steps = append(report.Steps, step)
		return report, fmt.Errorf("target-user managed toolchain bootstrap: %w", err)
	}
	step.Succeeded = true
	report.Steps = append(report.Steps, step)
	fmt.Fprintln(output, "\n✅ Linux fresh-host provisioning completed through Controller.")
	if report.ReloginRequired {
		fmt.Fprintln(output, "The target account must start a new login session before applications inherit the serial group.")
	}
	return report, nil
}

func ensureLinuxNPM(
	ctx context.Context,
	aptGet string,
	packageEnvironment, environment []string,
	output io.Writer,
	report *linuxHostProvisionReport,
) (string, error) {
	npmPath, err := linuxHostProvisionLookPath("npm")
	if err != nil {
		install := linuxHostProvisionCommand{
			Name: aptGet,
			Args: []string{
				"-o", "DPkg::Lock::Timeout=120", "install", "--yes", "--no-install-recommends", "npm",
			},
		}
		step := linuxHostProvisionStep{
			Name: "install npm for selected Node.js provider", Command: formatLinuxProvisionCommand(install), Mutating: true,
		}
		fmt.Fprintln(output, "\n▶", step.Name)
		fmt.Fprintln(output, step.Command)
		if runErr := linuxHostProvisionRun(ctx, install, packageEnvironment, output); runErr != nil {
			report.Steps = append(report.Steps, step)
			return "", fmt.Errorf("selected Node.js package did not provide npm and the distribution npm package could not be installed: %w", runErr)
		}
		step.Succeeded = true
		report.Steps = append(report.Steps, step)
		npmPath, err = linuxHostProvisionLookPath("npm")
		if err != nil {
			return "", errors.New("npm package installation completed but npm is not discoverable on PATH")
		}
	}
	verify := linuxHostProvisionCommand{Name: npmPath, Args: []string{"--version"}}
	step := linuxHostProvisionStep{
		Name: "verify npm", Command: formatLinuxProvisionCommand(verify), Mutating: false,
	}
	fmt.Fprintln(output, "\n▶", step.Name)
	fmt.Fprintln(output, step.Command)
	if runErr := linuxHostProvisionRun(ctx, verify, environment, output); runErr != nil {
		report.Steps = append(report.Steps, step)
		return "", fmt.Errorf("verify npm command: %w", runErr)
	}
	step.Succeeded = true
	report.Steps = append(report.Steps, step)
	return npmPath, nil
}

func ensureLinuxGlobalUPX(
	ctx context.Context,
	environment []string,
	output io.Writer,
	report *linuxHostProvisionReport,
) (string, error) {
	upxPath, err := linuxHostProvisionLookPath("upx")
	if err != nil {
		backend, backendErr := linuxHostProvisionLookPath("upx-ucl")
		if backendErr != nil {
			return "", errors.New("upx-ucl was installed but neither upx nor upx-ucl is discoverable on PATH")
		}
		const destination = "/usr/local/bin/upx"
		identity, inspectErr := linuxHostProvisionPathIdentity(destination)
		if inspectErr != nil {
			return "", fmt.Errorf("inspect global UPX command path: %w", inspectErr)
		}
		if identity.Exists {
			return "", fmt.Errorf("refusing to overwrite existing non-discoverable %s while publishing upx-ucl", destination)
		}
		linker, lookupErr := linuxHostProvisionLookPath("ln")
		if lookupErr != nil {
			return "", errors.New("publishing the upx-ucl command requires coreutils ln")
		}
		link := linuxHostProvisionCommand{
			Name: linker, Args: []string{"--symbolic", "--no-target-directory", backend, destination},
		}
		step := linuxHostProvisionStep{
			Name: "publish globally discoverable UPX", Command: formatLinuxProvisionCommand(link), Mutating: true,
		}
		fmt.Fprintln(output, "\n▶", step.Name)
		fmt.Fprintln(output, step.Command)
		if runErr := linuxHostProvisionRun(ctx, link, environment, output); runErr != nil {
			report.Steps = append(report.Steps, step)
			return "", fmt.Errorf("publish %s without overwriting an existing path: %w", destination, runErr)
		}
		step.Succeeded = true
		report.Steps = append(report.Steps, step)
		upxPath = destination
	}
	verify := linuxHostProvisionCommand{Name: upxPath, Args: []string{"--version"}}
	step := linuxHostProvisionStep{
		Name: "verify globally discoverable UPX", Command: formatLinuxProvisionCommand(verify), Mutating: false,
	}
	fmt.Fprintln(output, "\n▶", step.Name)
	fmt.Fprintln(output, step.Command)
	if runErr := linuxHostProvisionRun(ctx, verify, environment, output); runErr != nil {
		report.Steps = append(report.Steps, step)
		return "", fmt.Errorf("verify global UPX command: %w", runErr)
	}
	step.Succeeded = true
	report.Steps = append(report.Steps, step)
	return upxPath, nil
}

func validateTargetOwnedPath(dataDir, home string, uid uint64) error {
	relative, err := filepath.Rel(home, dataDir)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("PCController target data directory must be a child of the target account home")
	}
	current := filepath.Clean(dataDir)
	for {
		identity, inspectErr := linuxHostProvisionPathIdentity(current)
		if inspectErr != nil {
			return fmt.Errorf("inspect target-owned path %s: %w", current, inspectErr)
		}
		if identity.Exists {
			if identity.Symlink {
				return fmt.Errorf("refusing symlink in target-owned PCController path: %s", current)
			}
			if !identity.IsDir {
				return fmt.Errorf("target-owned PCController path component is not a directory: %s", current)
			}
			if identity.UID != uid {
				return fmt.Errorf("refusing root/foreign-owned PCController path %s (uid %d); use a clean target-owned home path instead of chowning a shared cache", current, identity.UID)
			}
		}
		if current == home {
			if !identity.Exists {
				return fmt.Errorf("target account home does not exist: %s", home)
			}
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("target-owned PCController path escaped the target account home")
		}
		current = parent
	}
}

func linuxSerialGroup(account *user.User) (*user.Group, bool, error) {
	groupIDs, err := linuxHostProvisionUserGroups(account)
	if err != nil {
		return nil, false, fmt.Errorf("inspect target account groups: %w", err)
	}
	member := make(map[string]bool, len(groupIDs)+1)
	for _, id := range groupIDs {
		member[id] = true
	}
	member[account.Gid] = true
	for _, candidate := range []string{"dialout", "uucp"} {
		group, lookupErr := linuxHostProvisionLookupGroup(candidate)
		if lookupErr == nil {
			return group, member[group.Gid], nil
		}
	}
	return nil, false, errors.New("no existing supported serial group was found (looked for dialout and uucp); Controller will not create a broad udev rule")
}

func linuxProvisionEnvironment(source []string) []string {
	augmented := netpolicy.WithLocalNetworkNoProxy(append([]string(nil), source...))
	type selectedValue struct {
		value          string
		exactUppercase bool
	}
	selected := make(map[string]selectedValue)
	for _, entry := range augmented {
		name, value, found := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		canonical := strings.ToUpper(name)
		if !found || name == "" || !linuxProvisionEnvironmentAllowed(canonical) {
			continue
		}
		candidate := selectedValue{value: value, exactUppercase: name == canonical}
		if existing, exists := selected[canonical]; exists && (existing.exactUppercase || !candidate.exactUppercase) {
			continue
		}
		selected[canonical] = candidate
	}
	keys := make([]string, 0, len(selected))
	for key := range selected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	for _, key := range keys {
		value := selected[key].value
		result = append(result, key+"="+value)
		if linuxProvisionProxyKey(key) {
			// Some package managers honor only the lowercase spelling. Emit
			// both spellings with one deterministically selected value so a
			// conflicting caller environment cannot be order-dependent.
			result = append(result, strings.ToLower(key)+"="+value)
		}
	}
	return result
}

func linuxProvisionProxyKey(upper string) bool {
	switch upper {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY":
		return true
	default:
		return false
	}
}

func linuxProvisionEnvironmentAllowed(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "LANG" || upper == "LANGUAGE" || upper == "TERM" || upper == "TZ" || strings.HasPrefix(upper, "LC_") {
		return true
	}
	switch upper {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS":
		return true
	default:
		return false
	}
}

func linuxProvisionProxyNames(environment []string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		upper := strings.ToUpper(strings.TrimSpace(name))
		if !found || strings.TrimSpace(value) == "" {
			continue
		}
		switch upper {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY":
			if !seen[upper] {
				seen[upper] = true
				names = append(names, upper)
			}
		}
	}
	sort.Strings(names)
	return names
}

func formatLinuxProvisionCommand(command linuxHostProvisionCommand) string {
	parts := make([]string, 0, len(command.Args)+1)
	parts = append(parts, strconv.Quote(command.Name))
	for _, argument := range command.Args {
		parts = append(parts, strconv.Quote(argument))
	}
	return strings.Join(parts, " ")
}

func trustedLinuxHostProvisionExecutable(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("unsafe privileged executable name %q", name)
	}
	for _, directory := range linuxHostProvisionTrustedDirectories {
		candidate := filepath.Join(directory, name)
		info, err := linuxHostProvisionStat(candidate)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("trusted system executable %q was not found in %s", name, strings.Join(linuxHostProvisionTrustedDirectories, ", "))
}
