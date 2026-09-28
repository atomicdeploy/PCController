package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopPredecessorRequiresActiveAndVerifiedPreviousOwnedSlot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "installation")
	service := testService(t, &fakeDesktop{})
	install := func(version string) LifecycleResult {
		t.Helper()
		packageRoot, manifest := writeTestPackage(t, version, version)
		result, err := service.Install(context.Background(), ChangeRequest{
			Root: root, PackageRoot: packageRoot, ExpectedPackageSHA256: manifest.RootSHA256, ConfigureDesktop: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	install("1.0.0")
	install("1.1.0")
	current := install("1.2.0")
	previousExecutable := filepath.Join(root, filepath.FromSlash(current.State.PreviousSlot), "controller.exe")
	owned, err := service.OwnsDesktopPredecessor(current.Executable, previousExecutable)
	if err != nil || !owned {
		t.Fatalf("valid predecessor owned=%t err=%v", owned, err)
	}
	for _, pair := range [][2]string{
		{previousExecutable, current.Executable}, // A rollback process cannot retarget the canonical link.
		{current.Executable, filepath.Join(t.TempDir(), "packages", "foreign", "controller.exe")},
		{current.Executable, filepath.Join(filepath.Dir(previousExecutable), "not-controller.exe")},
	} {
		owned, err := service.OwnsDesktopPredecessor(pair[0], pair[1])
		if err != nil || owned {
			t.Fatalf("foreign candidate accepted %v: %t %v", pair, owned, err)
		}
	}
	otherOwner := *service
	otherOwner.OwnerID = "another-owner"
	if owned, err := otherOwner.OwnsDesktopPredecessor(current.Executable, previousExecutable); err == nil || owned {
		t.Fatalf("foreign owner accepted: %t %v", owned, err)
	}
	if err := os.WriteFile(previousExecutable, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if owned, err := service.OwnsDesktopPredecessor(current.Executable, previousExecutable); err == nil || owned {
		t.Fatalf("tampered previous package accepted: %t %v", owned, err)
	}
}
