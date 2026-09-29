//go:build linux

package aptmirror

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestUnattendedUpgradeShimAcceptsReviewedNobleAndResoluteAffectedShape(t *testing.T) {
	moduleRoot := t.TempDir()
	writeFakeAPTModule(t, moduleRoot, "affected")
	output, err := runFakeUnattendedUpgradeSelfTest(t, moduleRoot)
	if err != nil {
		t.Fatalf("compatible fake python3-apt rejected: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "validated 2 PackageFiles") {
		t.Fatalf("self-test did not cover every fake PackageFile: %s", output)
	}
}

func TestUnattendedUpgradeShimPassesThroughReviewedFixedShape(t *testing.T) {
	moduleRoot := t.TempDir()
	writeFakeAPTModule(t, moduleRoot, "fixed")
	output, err := runFakeUnattendedUpgradeSelfTest(t, moduleRoot)
	if err != nil || !strings.Contains(string(output), "passthrough; upstream Origin has no find_index call") {
		t.Fatalf("fixed constructor did not pass through: err=%v output=%s", err, output)
	}
}

func TestUnattendedUpgradeShimRejectsUnknownAffectedShape(t *testing.T) {
	moduleRoot := t.TempDir()
	writeFakeAPTModule(t, moduleRoot, "unknown-affected")
	output, err := runFakeUnattendedUpgradeSelfTest(t, moduleRoot)
	if err == nil || !strings.Contains(string(output), "unknown affected apt.package.Origin.__init__") {
		t.Fatalf("unknown affected constructor was not rejected: err=%v output=%s", err, output)
	}
}

func TestUnattendedUpgradeShimMatchesLiveAPT(t *testing.T) {
	if os.Getenv("PC_CONTROLLER_LIVE_APT_FIXTURE") != "1" {
		t.Skip("set PC_CONTROLLER_LIVE_APT_FIXTURE=1 on a reviewed Ubuntu host")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "unattended-upgrade")
	if err := os.WriteFile(shim, UnattendedUpgradeShim(), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-I", shim, "--pccontroller-self-test")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live python3-apt compatibility failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "PackageFiles") {
		t.Fatalf("live self-test returned no PackageFile count: %s", output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestUnattendedUpgradeArtifactsAreManagedAndDryRunStaysReadOnly(t *testing.T) {
	config := mirrorTestConfig(t)
	if err := os.MkdirAll(filepath.Dir(config.Paths.CanonicalSource), 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	results := probeResults(config,
		ProbeResult{Status: ProbeVerified, Publication: now},
		ProbeResult{Status: ProbeVerified, Publication: now},
	)
	originalSelfTest := runUnattendedUpgradeSelfTest
	t.Cleanup(func() { runUnattendedUpgradeSelfTest = originalSelfTest })
	runUnattendedUpgradeSelfTest = func(context.Context, string) error {
		t.Fatal("dry-run executed the unattended-upgrade self-test")
		return nil
	}
	report, err := Install(context.Background(), InstallOptions{
		Config: config, Now: now, Prober: &tableProber{results: results}, ExecutableSource: executable,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config.Paths.UnattendedShim, config.Paths.UnattendedDropIn, config.Paths.APTDailyDropIn} {
		if !slices.Contains(report.ManagedFiles, path) {
			t.Fatalf("transactional managed paths omitted %s: %q", path, report.ManagedFiles)
		}
	}
	content, err := managedMirrorFiles(config, []byte("controller"), nil, sourcePlan{})
	if err != nil {
		t.Fatal(err)
	}
	if managedFileMode(config, config.Paths.UnattendedShim) != 0o755 {
		t.Fatal("unattended-upgrade shim is not executable")
	}
	if !strings.Contains(string(content[config.Paths.UnattendedDropIn]), "/opt/pccontroller/libexec") ||
		!strings.Contains(string(content[config.Paths.UnattendedDropIn]), "EnvironmentFile=-"+config.Paths.ProxyEnvironment) ||
		!strings.Contains(string(content[config.Paths.APTDailyDropIn]), "EnvironmentFile=-"+config.Paths.ProxyEnvironment) ||
		!strings.HasPrefix(string(content[config.Paths.UnattendedShim]), "#!/usr/bin/python3 -I") {
		t.Fatal("managed unattended-upgrade artifacts do not select the isolated root-owned shim")
	}
}

func runFakeUnattendedUpgradeSelfTest(t *testing.T, moduleRoot string) ([]byte, error) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the isolated compatibility fixture")
	}
	realProgram := filepath.Join(t.TempDir(), "distro-unattended-upgrade")
	if err := os.WriteFile(realProgram, []byte("# fixture\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "unattended-upgrade")
	if err := os.WriteFile(shim, unattendedUpgradeShim(realProgram, moduleRoot, os.Geteuid()), 0o755); err != nil {
		t.Fatal(err)
	}
	return exec.Command(python, "-I", shim, "--pccontroller-self-test").CombinedOutput()
}

func writeFakeAPTModule(t *testing.T, root, shape string) {
	t.Helper()
	packageDirectory := filepath.Join(root, "apt")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	aptPkg := `class PackageFile:
    def __init__(self, identifier, filename, trusted):
        self.id = identifier
        self.filename = filename
        self.archive = "resolute-security"
        self.component = "main"
        self.label = "Ubuntu"
        self.origin = "Ubuntu"
        self.codename = "resolute"
        self.site = "mirror.example"
        self.not_automatic = False
        self.trusted = trusted
`
	aptInit := `import apt_pkg
from . import package

class _Index:
    def __init__(self, trusted):
        self.is_trusted = trusted

class _List:
    def find_index(self, packagefile):
        return _Index(packagefile.trusted)

class _PCache:
    def __init__(self):
        self.file_list = [
            apt_pkg.PackageFile(1, "/var/lib/apt/lists/one", True),
            apt_pkg.PackageFile(2, "/var/lib/apt/lists/two", False),
        ]

class Cache:
    def __init__(self):
        self._list = _List()
        self._cache = _PCache()
`
	constructor := unattendedUpgradeAffectedOriginConstructor
	switch shape {
	case "affected":
	case "unknown-affected":
		constructor = strings.Replace(constructor, "# check the trust", "# changed trust behavior", 1)
	case "fixed":
		constructor = strings.Replace(constructor,
			`        # check the trust
        indexfile = pkg._pcache._list.find_index(packagefile)
        if indexfile and indexfile.is_trusted:
            self.trusted = True
        else:
            self.trusted = False
`, `        # New upstream implementation no longer searches every index target.
        self.trusted = packagefile.trusted
`, 1)
	default:
		t.Fatalf("unknown fake python3-apt shape %q", shape)
	}
	packageSource := "import apt_pkg\n\nclass Package:\n    pass\n\nclass Origin:\n" + constructor
	for path, content := range map[string]string{
		filepath.Join(root, "apt_pkg.py"):              aptPkg,
		filepath.Join(packageDirectory, "__init__.py"): aptInit,
		filepath.Join(packageDirectory, "package.py"):  packageSource,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
