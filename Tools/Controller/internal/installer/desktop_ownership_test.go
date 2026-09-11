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
	oldest := install("1.0.0")
	previous := install("1.1.0")
	current := install("1.2.0")
	owned, err := service.OwnsDesktopPredecessor(current.Executable, previous.Executable)
	if err != nil || !owned {
		t.Fatalf("valid predecessor owned=%t err=%v", owned, err)
	}
	for _, pair := range [][2]string{
		{previous.Executable, current.Executable}, // Stale process cannot downgrade a current link.
		{current.Executable, oldest.Executable},   // Older retained history is not implicit authorization.
		{current.Executable, filepath.Join(t.TempDir(), "packages", "foreign", "controller.exe")},
		{current.Executable, filepath.Join(filepath.Dir(previous.Executable), "not-controller.exe")},
	} {
		owned, err := service.OwnsDesktopPredecessor(pair[0], pair[1])
		if err != nil || owned {
			t.Fatalf("foreign candidate accepted %v: %t %v", pair, owned, err)
		}
	}
	otherOwner := *service
	otherOwner.OwnerID = "another-owner"
	if owned, err := otherOwner.OwnsDesktopPredecessor(current.Executable, previous.Executable); err == nil || owned {
		t.Fatalf("foreign owner accepted: %t %v", owned, err)
	}
	if err := os.WriteFile(previous.Executable, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if owned, err := service.OwnsDesktopPredecessor(current.Executable, previous.Executable); err == nil || owned {
		t.Fatalf("tampered previous package accepted: %t %v", owned, err)
	}
}
