package installer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/ownedstorage"
	"pccontroller.local/controller/internal/productidentity"
)

const testSourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeDesktop struct {
	ensure    []DesktopTarget
	remove    []DesktopTarget
	ensureErr error
	removeErr error
}

func (desktop *fakeDesktop) Ensure(_ context.Context, target DesktopTarget) error {
	desktop.ensure = append(desktop.ensure, target)
	return desktop.ensureErr
}

func (desktop *fakeDesktop) RemoveOwned(_ context.Context, target DesktopTarget) error {
	desktop.remove = append(desktop.remove, target)
	return desktop.removeErr
}

func TestPackageInventoryBindsHostExecutableAndResources(t *testing.T) {
	packageRoot, manifest := writeTestPackage(t, "1.2.3", "first")
	if manifest.Format != packageManifestFormat || manifest.Target.Platform != "windows" || manifest.Target.Architecture != "amd64" {
		t.Fatalf("manifest identity=%#v", manifest)
	}
	if manifest.ExecutablePath != "controller.exe" || len(manifest.Files) != 3 {
		t.Fatalf("manifest files=%#v", manifest.Files)
	}
	verified, err := VerifyPackage(packageRoot, manifest.RootSHA256, ManifestOptions{Platform: "windows", Architecture: "amd64"})
	if err != nil || !reflect.DeepEqual(verified, manifest) {
		t.Fatalf("verified=%#v err=%v", verified, err)
	}
	if _, err := VerifyPackage(packageRoot, strings.Repeat("b", 64), ManifestOptions{Platform: "windows", Architecture: "amd64"}); err == nil {
		t.Fatal("mismatched externally supplied inventory digest was accepted")
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "controller.exe"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPackage(packageRoot, manifest.RootSHA256, ManifestOptions{Platform: "windows", Architecture: "amd64"}); err == nil {
		t.Fatal("tampered executable was accepted")
	}
}

func TestPackageInventoryAcceptsPackedRuntimeIdentity(t *testing.T) {
	const version = "1.2.3-packed"
	packageRoot, manifest := writeTestPackage(t, version, "packed")
	executable, err := os.ReadFile(filepath.Join(packageRoot, "controller.exe"))
	if err != nil {
		t.Fatal(err)
	}
	// Model the observable layout of an UPX-packed controller: runtime linker
	// identity is not available as plaintext, while the Win32 version resource
	// remains inspectable without executing the package.
	for label, value := range map[string]string{
		"source hash": testSourceSHA,
		"version":     version,
		"build time":  manifest.BuildTime,
	} {
		if bytes.Contains(executable, []byte(value)) {
			t.Fatalf("packed-like executable unexpectedly exposes plaintext %s", label)
		}
	}
	if !bytes.Contains(executable, utf16Bytes(version)) {
		t.Fatal("packed-like executable lost its Win32 version resource")
	}
	if _, err := VerifyPackage(packageRoot, manifest.RootSHA256, ManifestOptions{
		Platform: "windows", Architecture: "amd64",
	}); err != nil {
		t.Fatalf("packed-like package verification failed: %v", err)
	}
}

func TestInstallUpdateAndRepairAreAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageOne, manifestOne := writeTestPackage(t, "1.0.0", "one")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)

	first, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageOne,
		ExpectedPackageSHA256: manifestOne.RootSHA256, ConfigureDesktop: true,
	})
	if err != nil || !first.Changed || !first.Healthy || !first.DesktopManaged {
		t.Fatalf("first install=%#v err=%v", first, err)
	}
	if len(desktop.ensure) != 1 || !samePath(desktop.ensure[0].Executable, first.Executable) {
		t.Fatalf("desktop activation=%#v", desktop.ensure)
	}

	again, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageOne,
		ExpectedPackageSHA256: manifestOne.RootSHA256,
	})
	if err != nil || again.Changed || !again.Healthy || !again.DesktopManaged {
		t.Fatalf("idempotent install=%#v err=%v", again, err)
	}
	if len(desktop.ensure) != 2 {
		t.Fatalf("desktop integration was not idempotently repaired: %#v", desktop.ensure)
	}

	packageTwo, manifestTwo := writeTestPackage(t, "1.1.0", "two")
	updated, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageTwo,
		ExpectedPackageSHA256: manifestTwo.RootSHA256,
	})
	if err != nil || !updated.Changed || updated.State.PreviousSHA256 != manifestOne.RootSHA256 {
		t.Fatalf("update=%#v err=%v", updated, err)
	}
	if samePath(updated.Executable, first.Executable) {
		t.Fatal("content-addressed update replaced a mapped executable in place")
	}
	if _, err := os.Stat(filepath.Dir(first.Executable)); err != nil {
		t.Fatalf("rollback package was not retained: %v", err)
	}

	if err := os.WriteFile(updated.Executable, []byte("damaged"), 0o755); err != nil {
		t.Fatal(err)
	}
	repaired, err := service.Repair(ctx, ChangeRequest{
		Root: root, PackageRoot: packageTwo,
		ExpectedPackageSHA256: manifestTwo.RootSHA256,
	})
	if err != nil || !repaired.Changed || !repaired.Healthy || samePath(repaired.Executable, updated.Executable) {
		t.Fatalf("repair=%#v err=%v", repaired, err)
	}
	status, err := service.Status(ctx, root)
	if err != nil || !status.Healthy || status.PackageSHA256 != manifestTwo.RootSHA256 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
		t.Fatalf("committed transaction journal remains: %v", err)
	}
}

func TestMatchingInstallPersistsNewDesktopIntegration(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageRoot, manifest := writeTestPackage(t, "1.0.0", "one")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)
	first, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageRoot,
		ExpectedPackageSHA256: manifest.RootSHA256,
	})
	if err != nil || !first.Changed || first.DesktopManaged {
		t.Fatalf("first install=%#v err=%v", first, err)
	}
	enabled, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageRoot,
		ExpectedPackageSHA256: manifest.RootSHA256, ConfigureDesktop: true,
	})
	if err != nil || !enabled.Changed || !enabled.DesktopManaged ||
		enabled.State == nil || !enabled.State.DesktopManaged {
		t.Fatalf("desktop enable=%#v err=%v", enabled, err)
	}
	status, err := service.Status(ctx, root)
	if err != nil || !status.Healthy || !status.DesktopManaged ||
		status.State == nil || status.State.DisplayName != productidentity.DefaultTitle {
		t.Fatalf("persisted desktop status=%#v err=%v", status, err)
	}
}

func TestDesktopFailureRetainsJournalAndRollsForwardOnRetry(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageOne, _ := writeTestPackage(t, "1.0.0", "one")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageOne, ConfigureDesktop: true}); err != nil {
		t.Fatal(err)
	}
	packageTwo, manifestTwo := writeTestPackage(t, "2.0.0", "two")
	desktop.ensureErr = errors.New("native registration failed")
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageTwo}); err == nil {
		t.Fatal("desktop activation failure was ignored")
	}
	if len(desktop.remove) != 1 {
		t.Fatalf("prior desktop activation was not cleaned up: %#v", desktop.remove)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
		t.Fatalf("failed desktop activation did not retain its journal: %v", err)
	}
	if _, err := service.Status(ctx, root); err == nil {
		t.Fatal("status ignored the incomplete desktop transition")
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
		t.Fatalf("failed recovery removed its retry journal: %v", err)
	}
	desktop.ensureErr = nil
	status, err := service.Status(ctx, root)
	if err != nil || !status.Healthy || status.PackageSHA256 != manifestTwo.RootSHA256 {
		t.Fatalf("roll-forward status=%#v err=%v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
		t.Fatalf("successful recovery retained its journal: %v", err)
	}
	if len(desktop.remove) != 3 || len(desktop.ensure) != 4 {
		t.Fatalf("desktop transition was not retried idempotently: %#v", desktop)
	}
}

func TestRecoveryCompletesDesktopActivationAfterStateCommit(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageRoot, _ := writeTestPackage(t, "1.0.0", "one")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)
	installed, err := service.Install(ctx, ChangeRequest{
		Root: root, PackageRoot: packageRoot, ConfigureDesktop: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.State == nil {
		t.Fatalf("install=%#v err=%v", installed, err)
	}
	desktop.ensure = nil
	desired := *installed.State
	journal := transactionJournal{
		Format: transactionFormat, ID: "state-committed", Operation: "install",
		Phase: "slot-ready", NewSlot: installed.State.ActiveSlot,
		NewSHA256: installed.State.ActiveSHA256, DesiredState: &desired, UpdatedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(ctx, root)
	if err != nil || !status.Healthy || len(desktop.ensure) != 1 {
		t.Fatalf("recovered status=%#v desktop=%#v err=%v", status, desktop.ensure, err)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
		t.Fatalf("recovered transaction journal remains: %v", err)
	}
}

func TestRecoveryVerifiesSlotBeforeDesktopActivation(t *testing.T) {
	for _, phase := range []string{"slot-ready", "activated"} {
		t.Run(phase, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "installation")
			packageRoot, _ := writeTestPackage(t, "1.0.0", phase)
			desktop := &fakeDesktop{}
			service := testService(t, desktop)
			installed, err := service.Install(context.Background(), ChangeRequest{
				Root: root, PackageRoot: packageRoot, ConfigureDesktop: true,
			})
			if err != nil || installed.State == nil {
				t.Fatalf("install=%#v err=%v", installed, err)
			}
			desktop.ensure = nil
			desired := *installed.State
			journal := transactionJournal{
				Format: transactionFormat, ID: "corrupt-recovery", Operation: "install",
				Phase: phase, NewSlot: installed.State.ActiveSlot,
				NewSHA256: installed.State.ActiveSHA256, DesiredState: &desired, UpdatedAt: time.Now().UTC(),
			}
			if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(installed.Executable, []byte("corrupt"), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Status(context.Background(), root); err == nil {
				t.Fatal("corrupted recovered slot was accepted")
			}
			if len(desktop.ensure) != 0 {
				t.Fatalf("desktop was activated before slot verification: %#v", desktop.ensure)
			}
			if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
				t.Fatalf("failed recovery journal was not retained: %v", err)
			}
		})
	}
}

func TestPresentationJournalRecoversEnableAndRenameCrashBoundaries(t *testing.T) {
	tests := []struct {
		name             string
		initiallyManaged bool
		previousName     string
		desiredName      string
		stateCommitted   bool
		wantRemove       int
	}{
		{name: "initial-enable-before-state", previousName: "Enabled", desiredName: "Enabled", wantRemove: 0},
		{name: "initial-enable-after-state", previousName: "Enabled", desiredName: "Enabled", stateCommitted: true, wantRemove: 0},
		{name: "rename-before-state", initiallyManaged: true, previousName: "Old Name", desiredName: "New Name", wantRemove: 1},
		{name: "rename-after-state", initiallyManaged: true, previousName: "Old Name", desiredName: "New Name", stateCommitted: true, wantRemove: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "installation")
			packageRoot, _ := writeTestPackage(t, "1.0.0", test.name)
			desktop := &fakeDesktop{}
			service := testService(t, desktop)
			service.DisplayName = test.previousName
			installed, err := service.Install(context.Background(), ChangeRequest{
				Root: root, PackageRoot: packageRoot, ConfigureDesktop: test.initiallyManaged,
			})
			if err != nil || installed.State == nil {
				t.Fatalf("install=%#v err=%v", installed, err)
			}
			previous := *installed.State
			desired := previous
			desired.DisplayName = test.desiredName
			desired.DesktopManaged = true
			desired.UpdatedAt = previous.UpdatedAt.Add(time.Second)
			if test.stateCommitted {
				if err := writeJSONAtomic(filepath.Join(root, installationStateName), desired, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			journal := transactionJournal{
				Format: transactionFormat, ID: test.name, Operation: "install", Phase: "presentation",
				NewSlot: desired.ActiveSlot, NewSHA256: desired.ActiveSHA256,
				PreviousState: &previous, DesiredState: &desired, UpdatedAt: time.Now().UTC(),
			}
			if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
				t.Fatal(err)
			}
			desktop.ensure, desktop.remove = nil, nil
			status, err := service.Status(context.Background(), root)
			if err != nil || !status.Healthy || status.State == nil || *status.State != desired {
				t.Fatalf("recovered status=%#v err=%v", status, err)
			}
			if len(desktop.remove) != test.wantRemove || len(desktop.ensure) != 1 ||
				desktop.ensure[0].DisplayName != test.desiredName {
				t.Fatalf("desktop reconciliation=%#v", desktop)
			}
			if test.wantRemove == 1 && desktop.remove[0].DisplayName != test.previousName {
				t.Fatalf("prior desktop identity was not selected for cleanup: %#v", desktop.remove)
			}
			if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
				t.Fatalf("recovered presentation journal remains: %v", err)
			}
		})
	}
}

func TestPackageRenameRecoveryCleansPriorIdentityAndRetainsFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		phase      string
		failRemove bool
	}{
		{name: "slot-ready", phase: "slot-ready"},
		{name: "activated", phase: "activated"},
		{name: "activated-cleanup-retry", phase: "activated", failRemove: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "installation")
			packageOne, _ := writeTestPackage(t, "1.0.0", "old-"+test.name)
			packageTwo, _ := writeTestPackage(t, "2.0.0", "new-"+test.name)
			desktop := &fakeDesktop{}
			service := testService(t, desktop)
			service.DisplayName = "Old Name"
			installed, err := service.Install(context.Background(), ChangeRequest{
				Root: root, PackageRoot: packageOne, ConfigureDesktop: true,
			})
			if err != nil || installed.State == nil {
				t.Fatalf("install=%#v err=%v", installed, err)
			}
			previous := *installed.State
			service.DisplayName = "New Name"
			updated, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageTwo})
			if err != nil || updated.State == nil {
				t.Fatalf("update=%#v err=%v", updated, err)
			}
			desired := *updated.State
			journal := transactionJournal{
				Format: transactionFormat, ID: test.name, Operation: "install", Phase: test.phase,
				NewSlot: desired.ActiveSlot, NewSHA256: desired.ActiveSHA256,
				PreviousState: &previous, DesiredState: &desired, UpdatedAt: time.Now().UTC(),
			}
			if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
				t.Fatal(err)
			}
			desktop.ensure, desktop.remove = nil, nil
			if test.failRemove {
				desktop.removeErr = errors.New("shortcut cleanup failed")
				if _, err := service.Status(context.Background(), root); err == nil {
					t.Fatal("desktop cleanup failure was ignored")
				}
				if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
					t.Fatalf("failed cleanup removed its retry journal: %v", err)
				}
				desktop.removeErr = nil
			}
			status, err := service.Status(context.Background(), root)
			if err != nil || !status.Healthy || status.State == nil || *status.State != desired {
				t.Fatalf("recovered status=%#v err=%v", status, err)
			}
			if len(desktop.remove) == 0 || desktop.remove[len(desktop.remove)-1].DisplayName != "Old Name" ||
				len(desktop.ensure) != 1 || desktop.ensure[0].DisplayName != "New Name" {
				t.Fatalf("package rename reconciliation=%#v", desktop)
			}
			if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
				t.Fatalf("recovered activation journal remains: %v", err)
			}
		})
	}
}

func TestInterruptedUninstallRollsBackToRetryableState(t *testing.T) {
	for _, phase := range []string{"uninstall-prepared", "uninstalling"} {
		t.Run(phase, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "installation")
			packageRoot, _ := writeTestPackage(t, "1.0.0", phase)
			desktop := &fakeDesktop{}
			service := testService(t, desktop)
			installed, err := service.Install(context.Background(), ChangeRequest{
				Root: root, PackageRoot: packageRoot, ConfigureDesktop: true,
			})
			if err != nil || installed.State == nil {
				t.Fatalf("install=%#v err=%v", installed, err)
			}
			desktop.ensure = nil
			stateCopy := *installed.State
			journal := transactionJournal{
				Format: transactionFormat, ID: "uninstall", Operation: "uninstall",
				Phase: phase, PreviousState: &stateCopy, UpdatedAt: time.Now().UTC(),
			}
			if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
				t.Fatal(err)
			}
			status, err := service.Status(context.Background(), root)
			if err != nil || !status.Healthy || len(desktop.ensure) != 1 {
				t.Fatalf("recovered status=%#v desktop=%#v err=%v", status, desktop.ensure, err)
			}
			if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
				t.Fatalf("retryable recovery left journal: %v", err)
			}
			removed, err := service.Uninstall(context.Background(), UninstallRequest{Root: root})
			if err != nil || !removed.Changed {
				t.Fatalf("retry uninstall=%#v err=%v", removed, err)
			}
		})
	}
}

func TestDetachedUninstallTombstoneIsRecovered(t *testing.T) {
	root := filepath.Join(t.TempDir(), "installation")
	packageRoot, _ := writeTestPackage(t, "1.0.0", "tombstone")
	service := testService(t, nil)
	if _, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		Format: transactionFormat, ID: "uninstall", Operation: "uninstall",
		Phase: "uninstalling", UpdatedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	tombstone := removalTombstone(root)
	if err := os.Rename(root, tombstone); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), root)
	if err != nil || !status.Changed || status.Healthy {
		t.Fatalf("detached recovery status=%#v err=%v", status, err)
	}
	if _, err := os.Stat(tombstone); !os.IsNotExist(err) {
		t.Fatalf("detached tombstone remains: %v", err)
	}
}

func TestCurrentDisplayNameWinsEnableUpdateRetryAndCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "installation")
	packageOne, _ := writeTestPackage(t, "1.0.0", "name-one")
	packageTwo, _ := writeTestPackage(t, "2.0.0", "name-two")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)
	service.DisplayName = "Old Name"
	first, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageOne})
	if err != nil || first.State == nil {
		t.Fatal(err)
	}
	service.DisplayName = "Current Name"
	enabled, err := service.Install(context.Background(), ChangeRequest{
		Root: root, PackageRoot: packageOne, ConfigureDesktop: true,
	})
	if err != nil || enabled.State.DisplayName != "Current Name" ||
		len(desktop.ensure) != 1 || desktop.ensure[0].DisplayName != "Current Name" {
		t.Fatalf("enable=%#v desktop=%#v err=%v", enabled, desktop.ensure, err)
	}
	service.DisplayName = "Updated Name"
	updated, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageTwo})
	if err != nil || updated.State.DisplayName != "Updated Name" ||
		len(desktop.remove) == 0 || desktop.remove[len(desktop.remove)-1].DisplayName != "Current Name" ||
		desktop.ensure[len(desktop.ensure)-1].DisplayName != "Updated Name" {
		t.Fatalf("update=%#v desktop=%#v err=%v", updated, desktop, err)
	}

	service.DisplayName = "Failed Rename"
	desktop.ensureErr = errors.New("registration failed")
	if _, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageTwo}); err == nil {
		t.Fatal("rename registration failure was ignored")
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
		t.Fatalf("failed rename did not retain its recovery journal: %v", err)
	}
	desktop.ensureErr = nil
	status, err := service.Status(context.Background(), root)
	if err != nil || status.State == nil || status.State.DisplayName != "Failed Rename" {
		t.Fatalf("rename retry status=%#v err=%v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); !os.IsNotExist(err) {
		t.Fatalf("successful rename retry retained its journal: %v", err)
	}
	desktop.remove = nil
	removed, err := service.Uninstall(context.Background(), UninstallRequest{Root: root})
	if err != nil || !removed.Changed || len(desktop.remove) != 1 || desktop.remove[0].DisplayName != "Failed Rename" {
		t.Fatalf("renamed cleanup=%#v desktop=%#v err=%v", removed, desktop.remove, err)
	}
}

func TestLifecycleLockHonorsContextDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockName)
	first, err := acquireLifecycleLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := acquireLifecycleLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended lock error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("contended lock exceeded bounded cancellation: %s", elapsed)
	}
}

func TestOwnershipChecksAndInterruptedStagingRecovery(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageRoot, _ := writeTestPackage(t, "1.0.0", "one")
	owner := testService(t, nil)
	if _, err := owner.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
		t.Fatal(err)
	}
	foreign := testService(t, nil)
	foreign.OwnerID = "S-1-5-21-foreign"
	if _, err := foreign.Status(ctx, root); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("foreign owner status error=%v", err)
	}

	stage := filepath.Join(root, stagingDirectory, "abandoned")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		Format: transactionFormat, ID: "abandoned", Operation: "repair",
		Phase: "staging", Stage: filepath.ToSlash(filepath.Join(stagingDirectory, "abandoned")), UpdatedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := owner.Status(ctx, root)
	if err != nil || !status.Healthy {
		t.Fatalf("recovered status=%#v err=%v", status, err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("abandoned stage was not removed: %v", err)
	}
}

func TestUninstallPreservesDataUnlessSeparatelyConfirmed(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageRoot, _ := writeTestPackage(t, "1.0.0", "one")
	service := testService(t, nil)
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "custom", "controller-data")
	if err := ownedstorage.EnsureFor(data, service.OwnerID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "keep.json"), []byte("important"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join(t.TempDir(), productidentity.ConfigDirectory)
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(repository, "config.json")
	sibling := filepath.Join(repository, "source.go")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("package source"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := service.Uninstall(ctx, UninstallRequest{
		Root: root, PurgeConfigFiles: []string{config}, PurgeDataRoots: []string{data},
	})
	if err != nil || !removed.Changed || !removed.DataPreserved {
		t.Fatalf("uninstall=%#v err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(data, "keep.json")); err != nil {
		t.Fatalf("default uninstall removed user data: %v", err)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("default uninstall removed configuration: %v", err)
	}

	root = filepath.Join(t.TempDir(), "installation")
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Uninstall(ctx, UninstallRequest{
		Root: root, PurgeData: true, PurgeConfirmation: "yes",
		PurgeConfigFiles: []string{config}, PurgeDataRoots: []string{data},
	}); err == nil {
		t.Fatal("weak purge confirmation was accepted")
	}
	preview, err := service.Uninstall(ctx, UninstallRequest{
		Root: root, PurgeData: true, PreviewPurge: true,
		PurgeConfigFiles: []string{config, config}, PurgeDataRoots: []string{data, data},
	})
	if err != nil || preview.Changed || len(preview.PurgeTargets) != 2 {
		t.Fatalf("purge preview=%#v err=%v", preview, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("purge preview changed installation: %v", err)
	}
	purged, err := service.Uninstall(ctx, UninstallRequest{
		Root: root, PurgeData: true, PurgeConfirmation: PurgeConfirmation,
		PurgeConfigFiles: []string{config}, PurgeDataRoots: []string{data},
	})
	if err != nil || purged.DataPreserved || !reflect.DeepEqual(
		purged.PurgedPaths, []string{config, data},
	) {
		t.Fatalf("purge=%#v err=%v", purged, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("confirmed data purge did not remove the exact directory: %v", err)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("confirmed purge did not remove the exact config file: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("confirmed purge removed a repository sibling: %v", err)
	}
}

func TestPurgeRejectsUnmarkedRootsAndAllowsAbsentOverrides(t *testing.T) {
	service := testService(t, nil)
	installRoot := filepath.Join(t.TempDir(), "installation")
	unmarked := filepath.Join(t.TempDir(), "arbitrary-data")
	if err := os.MkdirAll(unmarked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmarked, "foreign.txt"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Uninstall(context.Background(), UninstallRequest{
		Root: installRoot, PurgeData: true, PreviewPurge: true,
		PurgeDataRoots: []string{unmarked},
	}); !errors.Is(err, ownedstorage.ErrNotOwned) {
		t.Fatalf("unmarked root error=%v", err)
	}
	absent := filepath.Join(t.TempDir(), "custom", "missing-data")
	preview, err := service.Uninstall(context.Background(), UninstallRequest{
		Root: installRoot, PurgeData: true, PreviewPurge: true,
		PurgeDataRoots: []string{absent},
	})
	if err != nil || len(preview.PurgeTargets) != 1 || preview.PurgeTargets[0].Exists {
		t.Fatalf("absent override preview=%#v err=%v", preview, err)
	}
	owned := filepath.Join(t.TempDir(), "owned-data")
	if err := ownedstorage.EnsureFor(owned, service.OwnerID); err != nil {
		t.Fatal(err)
	}
	coveredConfig := filepath.Join(owned, "config.json")
	if err := os.WriteFile(coveredConfig, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlap, err := service.Uninstall(context.Background(), UninstallRequest{
		Root: installRoot, PurgeData: true, PreviewPurge: true,
		PurgeConfigFiles: []string{coveredConfig, coveredConfig},
		PurgeDataRoots:   []string{owned, owned},
	})
	if err != nil || len(overlap.PurgeTargets) != 1 || overlap.PurgeTargets[0].Kind != "data-root" {
		t.Fatalf("overlap purge preview=%#v err=%v", overlap, err)
	}
}

func TestUnsupportedPlatformAndForeignRootAreExplicit(t *testing.T) {
	service := testService(t, nil)
	service.Platform = "linux"
	if _, err := service.Status(context.Background(), filepath.Join(t.TempDir(), "installation")); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform error=%v", err)
	}
	foreignRoot := filepath.Join(t.TempDir(), "installation")
	if err := os.MkdirAll(foreignRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignRoot, "foreign.txt"), []byte("owned elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	packageRoot, _ := writeTestPackage(t, "1.0.0", "one")
	service.Platform = "windows"
	if _, err := service.Install(context.Background(), ChangeRequest{Root: foreignRoot, PackageRoot: packageRoot}); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("foreign root error=%v", err)
	}
}

func testService(t *testing.T, desktop DesktopIntegrator) *Service {
	t.Helper()
	return &Service{
		Platform: "windows", Architecture: "amd64", OwnerID: "S-1-5-21-test",
		CurrentExecutable: filepath.Join(t.TempDir(), "release", "controller.exe"),
		DisplayName:       productidentity.DefaultTitle, Desktop: desktop,
		Now: time.Now,
	}
}

func writeTestPackage(t *testing.T, version, marker string) (string, PackageManifest) {
	t.Helper()
	root := t.TempDir()
	buildTime := "2026-08-02T12:34:56Z"
	executable := minimalResourcePE(version, marker)
	executablePath := filepath.Join(root, "controller.exe")
	if err := os.WriteFile(executablePath, executable, 0o755); err != nil {
		t.Fatal(err)
	}
	executableSHA, executableBytes, err := digestFile(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	host := map[string]any{
		"format": hostManifestFormat,
		"target": map[string]any{"platform": "windows", "architecture": "amd64"},
		"identity": map[string]any{
			"version": version, "appName": productidentity.DefaultTitle,
			"tagline": "test package", "sourceSHA256": testSourceSHA,
			"sourceFiles": 1, "buildTime": buildTime,
		},
		"validation": map[string]any{"windowsResources": "verified", "webUI": map[string]any{"status": "passed"}},
		"artifacts":  []map[string]any{{"path": "controller.exe", "bytes": executableBytes, "sha256": executableSHA}},
	}
	content, err := json.MarshalIndent(host, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "host-manifest.json"), append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "licenses"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "licenses", "NOTICE.txt"), []byte("test notice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := GeneratePackageManifest(root, filepath.Join(root, PackageManifestName), ManifestOptions{Platform: "windows", Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	return root, manifest
}

func minimalResourcePE(version, marker string) []byte {
	const peOffset = 64
	const optionalBytes = 240
	const sectionTable = peOffset + 24 + optionalBytes
	const resourceOffset = 512
	content := make([]byte, 2048)
	content[0], content[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(content[0x3c:0x40], peOffset)
	copy(content[peOffset:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(content[peOffset+4:peOffset+6], 0x8664)
	binary.LittleEndian.PutUint16(content[peOffset+6:peOffset+8], 1)
	binary.LittleEndian.PutUint16(content[peOffset+20:peOffset+22], optionalBytes)
	binary.LittleEndian.PutUint16(content[peOffset+22:peOffset+24], 0x0002)
	binary.LittleEndian.PutUint16(content[peOffset+24:peOffset+26], 0x020b)
	header := content[sectionTable : sectionTable+40]
	copy(header[:8], ".rsrc")
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(content)-resourceOffset))
	binary.LittleEndian.PutUint32(header[20:24], resourceOffset)
	resource := content[resourceOffset:]
	offset := 0
	for _, value := range [][]byte{
		utf16Bytes(productidentity.DefaultTitle), utf16Bytes(version), utf16Bytes("controller.exe"),
		[]byte(marker),
	} {
		copy(resource[offset:], value)
		offset += len(value) + 8
	}
	return content
}
