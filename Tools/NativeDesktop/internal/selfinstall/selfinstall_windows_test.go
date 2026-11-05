//go:build windows

package selfinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/comserver"
	"pccontroller.local/desktop/internal/shellintegration"
)

type installFixture struct {
	deps dependencies

	local      string
	roaming    string
	temporary  string
	source     string
	paths      layout
	stopCalls  int
	startCalls []bool
	shellAdds  int
	shellDrops int
	comAdds    int
	comDrops   int
	launches   []string
	helperPath string
	cleanup    CleanupOptions

	startupOwned bool
	shellOwned   bool
	comOwned     bool
	comOwnedErr  error
	shellAddErr  error
	shellDropErr error
	comAddErr    error
	comDropErr   error
}

func newInstallFixture(t *testing.T) *installFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &installFixture{
		local:     filepath.Join(root, "local"),
		roaming:   filepath.Join(root, "roaming"),
		temporary: filepath.Join(root, "temp"),
		source:    filepath.Join(root, "portable", executableName),
	}
	for _, directory := range []string{
		filepath.Dir(fixture.source),
		fixture.local,
		fixture.roaming,
		fixture.temporary,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		fixture.source,
		[]byte("portable PCControllerDesktop build"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	fixture.deps = dependencies{
		localAppData: func() (string, error) {
			return fixture.local, nil
		},
		roamingAppData: func() (string, error) {
			return fixture.roaming, nil
		},
		tempDirectory: func() (string, error) {
			return fixture.temporary, nil
		},
		currentExecutable: func() (string, error) {
			return fixture.source, nil
		},
		stopPrimary: func(context.Context) error {
			fixture.stopCalls++
			return nil
		},
		primaryRunning: func(context.Context) bool { return false },
		startupOwned: func(string) (bool, error) {
			return fixture.startupOwned, nil
		},
		setStartup: func(enabled bool, _ string) error {
			fixture.startCalls = append(fixture.startCalls, enabled)
			return nil
		},
		shellOwned: func(string) (bool, error) {
			return fixture.shellOwned, nil
		},
		registerShell: func(string) error {
			fixture.shellAdds++
			return fixture.shellAddErr
		},
		unregisterShell: func(string) error {
			fixture.shellDrops++
			return fixture.shellDropErr
		},
		comOwned: func(string) (bool, error) {
			return fixture.comOwned, fixture.comOwnedErr
		},
		comServerOwned: func(string) (bool, error) {
			return fixture.comOwned, fixture.comOwnedErr
		},
		registerCOM: func(string) error {
			fixture.comAdds++
			return fixture.comAddErr
		},
		unregisterCOM: func(string) error {
			fixture.comDrops++
			return fixture.comDropErr
		},
		launch: func(path string) error {
			fixture.launches = append(fixture.launches, path)
			return nil
		},
		launchHelper: func(path string, options CleanupOptions) error {
			fixture.helperPath = path
			fixture.cleanup = options
			return nil
		},
		processID: func() int { return 4242 },
	}
	paths, err := resolveLayout(fixture.deps)
	if err != nil {
		t.Fatal(err)
	}
	fixture.paths = paths
	return fixture
}

func (f *installFixture) createInstalled(t *testing.T, contents string) {
	t.Helper()
	if err := os.MkdirAll(f.paths.directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		f.paths.executable,
		[]byte(contents),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
}

func TestInstallDefaultsToFileOnly(t *testing.T) {
	fixture := newInstallFixture(t)
	result, err := installWithDependencies(
		context.Background(),
		InstallOptions{},
		fixture.deps,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Updated || result.Warning != nil {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(fixture.paths.executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "portable PCControllerDesktop build" {
		t.Fatalf("installed bytes = %q", got)
	}
	if fixture.stopCalls != 1 ||
		len(fixture.startCalls) != 0 ||
		fixture.shellAdds != 0 ||
		fixture.comAdds != 0 ||
		fixture.shellDrops != 0 ||
		fixture.comDrops != 0 {
		t.Fatalf("portable-default calls = %+v", fixture)
	}
	entries, err := os.ReadDir(fixture.paths.programs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != installDirectoryName {
		t.Fatalf("Programs entries = %v", entries)
	}
}

func TestInstallRollbackRestoresPreviousExecutableAndFreshState(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "previous installed build")
	fixture.comAddErr = errors.New("COM registration failed")

	_, err := installWithDependencies(
		context.Background(),
		InstallOptions{
			Force:       true,
			WithStartup: true,
			WithShell:   true,
			WithCOM:     true,
		},
		fixture.deps,
	)
	if err == nil || !strings.Contains(err.Error(), "COM registration failed") {
		t.Fatalf("install error = %v", err)
	}
	got, readErr := os.ReadFile(fixture.paths.executable)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "previous installed build" {
		t.Fatalf("restored executable = %q", got)
	}
	if !reflect.DeepEqual(fixture.startCalls, []bool{true, false}) {
		t.Fatalf("startup calls = %v", fixture.startCalls)
	}
	if fixture.shellAdds != 1 || fixture.shellDrops != 1 {
		t.Fatalf(
			"shell add/drop = %d/%d",
			fixture.shellAdds,
			fixture.shellDrops,
		)
	}
	if fixture.comAdds != 1 || fixture.comDrops != 1 {
		t.Fatalf(
			"COM add/drop = %d/%d",
			fixture.comAdds,
			fixture.comDrops,
		)
	}
	entries, readDirErr := os.ReadDir(fixture.paths.programs)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	if len(entries) != 1 || entries[0].Name() != installDirectoryName {
		t.Fatalf("rollback left managed siblings: %v", entries)
	}
}

func TestInstallRollbackPreservesPreexistingCOM(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "previous installed build")
	fixture.comOwned = true
	fixture.comAddErr = errors.New("COM repair failed")

	_, err := installWithDependencies(
		context.Background(),
		InstallOptions{Force: true, WithCOM: true},
		fixture.deps,
	)
	if err == nil {
		t.Fatal("COM failure was accepted")
	}
	if fixture.comDrops != 0 {
		t.Fatalf("preexisting COM was unregistered %d times", fixture.comDrops)
	}
}

func TestUnexpectedInstallContentsFailClosedAndReportIncomplete(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	if err := os.WriteFile(
		filepath.Join(fixture.paths.directory, "foreign.dat"),
		[]byte("do not delete"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, err := installWithDependencies(
		context.Background(),
		InstallOptions{Force: true},
		fixture.deps,
	)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("install error = %v, want ErrUnsafePath", err)
	}
	if fixture.stopCalls != 0 {
		t.Fatalf("shutdown called before path validation %d times", fixture.stopCalls)
	}
	status, err := statusWithDependencies(fixture.deps)
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || !status.Incomplete {
		t.Fatalf("status = %+v", status)
	}
	if _, err := os.Stat(
		filepath.Join(fixture.paths.directory, "foreign.dat"),
	); err != nil {
		t.Fatalf("foreign file was changed: %v", err)
	}
}

func TestUninstallPreservesForeignIntegrationsAndSchedulesCleanup(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	fixture.shellDropErr = &shellintegration.OwnershipError{
		RegisteredCommand: `"C:\Other\PCControllerDesktop.exe" /uri "%1"`,
	}
	fixture.comDropErr = fmtOwnershipError()

	result, err := uninstallWithDependencies(
		context.Background(),
		UninstallOptions{},
		fixture.deps,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Warning == nil ||
		!errors.Is(result.Warning, shellintegration.ErrRegistrationOwned) ||
		!errors.Is(
			result.Warning,
			comserver.ErrRegistrationOwnedByAnotherExecutable,
		) {
		t.Fatalf("warning = %v", result.Warning)
	}
	if !reflect.DeepEqual(fixture.startCalls, []bool{false}) {
		t.Fatalf("startup cleanup calls = %v", fixture.startCalls)
	}
	if fixture.helperPath == "" ||
		fixture.cleanup.ParentPID != 4242 ||
		!samePath(fixture.cleanup.InstallPath, fixture.paths.directory) {
		t.Fatalf(
			"cleanup helper = %q %+v",
			fixture.helperPath,
			fixture.cleanup,
		)
	}
	if _, err := os.Stat(fixture.paths.executable); err != nil {
		t.Fatalf("files were removed before helper handoff: %v", err)
	}
}

func TestUninstallContinuesAfterPartialShellCleanupWhenURIIsGone(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	fixture.shellOwned = false
	fixture.shellDropErr = &shellintegration.PartialError{
		Completed: "the URI was removed",
		Failed:    "Jump List cleanup",
		Err:       errors.New("policy rejected Jump List cleanup"),
	}

	result, err := uninstallWithDependencies(
		context.Background(),
		UninstallOptions{},
		fixture.deps,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !shellintegration.IsPartial(result.Warning) {
		t.Fatalf("warning = %v, want partial shell warning", result.Warning)
	}
	if fixture.helperPath == "" {
		t.Fatal("cleanup helper was not scheduled")
	}
}

func TestUninstallStopsAfterPartialShellCleanupWhenURIStillOwned(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	fixture.shellOwned = true
	fixture.shellDropErr = &shellintegration.PartialError{
		Completed: "Jump List tasks were removed",
		Failed:    "URI cleanup",
		Err:       errors.New("registry deletion failed"),
	}

	_, err := uninstallWithDependencies(
		context.Background(),
		UninstallOptions{},
		fixture.deps,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "owned URI registration remains") {
		t.Fatalf("uninstall error = %v", err)
	}
	if fixture.helperPath != "" {
		t.Fatalf("cleanup helper was incorrectly scheduled: %s", fixture.helperPath)
	}
	if len(fixture.startCalls) != 0 || fixture.comDrops != 0 {
		t.Fatalf(
			"later cleanup ran after URI failure: startup=%v COM=%d",
			fixture.startCalls,
			fixture.comDrops,
		)
	}
}

func TestUninstallStopsWhenStartupStillOwnsTarget(t *testing.T) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	fixture.startupOwned = true

	_, err := uninstallWithDependencies(
		context.Background(),
		UninstallOptions{},
		fixture.deps,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"start-with-Windows registration remains",
		) {
		t.Fatalf("uninstall error = %v", err)
	}
	if fixture.helperPath != "" || fixture.comDrops != 0 {
		t.Fatal("cleanup continued after startup ownership remained")
	}
}

func TestUninstallStopsWhenForeignCOMConflictAlsoLeavesOwnedView(
	t *testing.T,
) {
	fixture := newInstallFixture(t)
	fixture.createInstalled(t, "installed")
	fixture.comDropErr = fmtOwnershipError()
	fixture.comOwned = true
	fixture.comOwnedErr = fmtOwnershipError()

	_, err := uninstallWithDependencies(
		context.Background(),
		UninstallOptions{},
		fixture.deps,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "owned COM registry view remains") {
		t.Fatalf("uninstall error = %v", err)
	}
	if fixture.helperPath != "" {
		t.Fatal("cleanup helper was scheduled with an owned COM view remaining")
	}
}

func fmtOwnershipError() error {
	return errors.Join(
		comserver.ErrRegistrationOwnedByAnotherExecutable,
		errors.New("foreign COM owner"),
	)
}

func TestCleanupHelperCommandUsesTemporaryWorkingDirectory(t *testing.T) {
	fixture := newInstallFixture(t)
	helper := filepath.Join(
		fixture.temporary,
		helperPrefix+"command-test"+helperSuffix,
	)
	command := cleanupHelperCommand(helper, CleanupOptions{
		ParentPID:      123,
		InstallPath:    fixture.paths.directory,
		RemoveUserData: true,
	})
	if !samePath(command.Dir, fixture.temporary) {
		t.Fatalf("helper working directory = %q", command.Dir)
	}
	joined := strings.Join(command.Args[1:], "\x00")
	for _, want := range []string{
		"/cleanup-helper",
		"/quiet",
		"--parent-pid",
		"123",
		"--install-path",
		fixture.paths.directory,
		"--remove-user-data",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("helper arguments %q omit %q", command.Args, want)
		}
	}
}

func TestCleanupRequestValidationRejectsUnexpectedTargets(t *testing.T) {
	fixture := newInstallFixture(t)
	helper := filepath.Join(
		fixture.temporary,
		helperPrefix+"0123456789abcdef"+helperSuffix,
	)
	if err := os.WriteFile(helper, []byte("helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	valid := CleanupOptions{
		ParentPID:   111,
		InstallPath: fixture.paths.directory,
	}
	if err := validateCleanupRequest(
		valid,
		fixture.paths,
		helper,
		fixture.temporary,
		222,
	); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name    string
		options CleanupOptions
		helper  string
		pid     uint32
	}{
		{
			name: "wrong target",
			options: CleanupOptions{
				ParentPID:   111,
				InstallPath: filepath.Join(fixture.local, "Programs", "Other"),
			},
			helper: helper,
			pid:    222,
		},
		{
			name:    "own PID",
			options: valid,
			helper:  helper,
			pid:     111,
		},
		{
			name:    "wrong helper name",
			options: valid,
			helper:  filepath.Join(fixture.temporary, "other.exe"),
			pid:     222,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateCleanupRequest(
				test.options,
				fixture.paths,
				test.helper,
				fixture.temporary,
				test.pid,
			); err == nil {
				t.Fatal("unsafe cleanup request was accepted")
			}
		})
	}
}

func TestManagedBackupWithUnexpectedEntryIsNeverRemoved(t *testing.T) {
	fixture := newInstallFixture(t)
	backup := filepath.Join(
		fixture.paths.programs,
		backupPrefix+"known-test-id",
	)
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		executableName: "old executable",
		"foreign.txt":  "preserve",
	} {
		if err := os.WriteFile(
			filepath.Join(backup, name),
			[]byte(contents),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	err := removeManagedInstallDirectory(
		backup,
		fixture.paths.programs,
		backupPrefix,
	)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("remove error = %v, want ErrUnsafePath", err)
	}
	if _, err := os.Stat(filepath.Join(backup, "foreign.txt")); err != nil {
		t.Fatalf("foreign backup entry was removed: %v", err)
	}
}
