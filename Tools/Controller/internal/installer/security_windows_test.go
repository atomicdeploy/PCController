//go:build windows

package installer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/ownedstorage"
)

func TestWindowsPackageAndInstallRejectIntermediateJunctions(t *testing.T) {
	packageRoot, manifest := writeTestPackage(t, "1.0.0", "junction-source")
	external := t.TempDir()
	notice, err := os.ReadFile(filepath.Join(packageRoot, "licenses", "NOTICE.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "NOTICE.txt"), notice, 0o600); err != nil {
		t.Fatal(err)
	}
	licenses := filepath.Join(packageRoot, "licenses")
	if err := os.RemoveAll(licenses); err != nil {
		t.Fatal(err)
	}
	createJunction(t, licenses, external)
	if _, err := secureDirectory(licenses); err == nil {
		t.Fatal("package junction itself was accepted as a secure directory")
	}
	if _, err := VerifyPackage(packageRoot, manifest.RootSHA256, ManifestOptions{
		Platform: "windows", Architecture: "amd64",
	}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse") {
		t.Fatalf("package junction error=%v", err)
	}

	cleanPackage, _ := writeTestPackage(t, "1.0.0", "junction-root")
	realParent := t.TempDir()
	linkParent := filepath.Join(t.TempDir(), "linked-parent")
	createJunction(t, linkParent, realParent)
	service := testService(t, nil)
	if _, err := service.Install(context.Background(), ChangeRequest{
		Root: filepath.Join(linkParent, "installation"), PackageRoot: cleanPackage,
	}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse") {
		t.Fatalf("install-root junction error=%v", err)
	}
}

func TestWindowsRecoveryPruneAndPurgeRejectJunctions(t *testing.T) {
	t.Run("staging", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "installation")
		packageRoot, _ := writeTestPackage(t, "1.0.0", "staging-junction")
		service := testService(t, nil)
		if _, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
			t.Fatal(err)
		}
		staging := filepath.Join(root, stagingDirectory)
		if err := os.RemoveAll(staging); err != nil {
			t.Fatal(err)
		}
		external := t.TempDir()
		sentinel := filepath.Join(external, "sentinel.txt")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		createJunction(t, staging, external)
		journal := transactionJournal{
			Format: transactionFormat, ID: "junction", Operation: "repair", Phase: "staging",
			Stage: filepath.ToSlash(filepath.Join(stagingDirectory, "abandoned")), UpdatedAt: time.Now().UTC(),
		}
		if err := writeJSONAtomic(filepath.Join(root, transactionName), journal, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Status(context.Background(), root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse") {
			t.Fatalf("staging junction error=%v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("recovery traversed staging junction: %v", err)
		}
	})

	t.Run("packages-prune", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "installation")
		packageRoot, _ := writeTestPackage(t, "1.0.0", "package-junction")
		service := testService(t, nil)
		installed, err := service.Install(context.Background(), ChangeRequest{Root: root, PackageRoot: packageRoot})
		if err != nil || installed.State == nil {
			t.Fatal(err)
		}
		external := t.TempDir()
		sentinel := filepath.Join(external, "sentinel.txt")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		createJunction(t, filepath.Join(root, packagesDirectory, "foreign"), external)
		warnings := prunePackageSlots(root, *installed.State)
		if len(warnings) == 0 {
			t.Fatal("junction package slot was silently pruned")
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("prune traversed package junction: %v", err)
		}
	})

	t.Run("purge", func(t *testing.T) {
		service := testService(t, nil)
		data := filepath.Join(t.TempDir(), "custom-data")
		if err := ownedstorage.EnsureFor(data, service.OwnerID); err != nil {
			t.Fatal(err)
		}
		external := t.TempDir()
		sentinel := filepath.Join(external, "sentinel.txt")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		createJunction(t, filepath.Join(data, "linked"), external)
		if _, err := service.Uninstall(context.Background(), UninstallRequest{
			Root: filepath.Join(t.TempDir(), "absent-install"), PurgeData: true,
			PreviewPurge: true, PurgeDataRoots: []string{data},
		}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse") {
			t.Fatalf("purge junction error=%v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("purge traversed data junction: %v", err)
		}
	})
}

func TestWindowsInventoryRejectsNamespaceAliases(t *testing.T) {
	for _, value := range []string{"CON", "folder/file:stream", "trailing.", "folder /file"} {
		if _, err := normalizeInventoryPath(value); err == nil {
			t.Errorf("namespace alias %q was accepted", value)
		}
	}
}

func createJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("Windows junction creation unavailable: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}
