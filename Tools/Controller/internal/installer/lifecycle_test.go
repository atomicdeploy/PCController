package installer

import (
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

	"pccontroller.local/controller/internal/productidentity"
)

const testSourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeDesktop struct {
	ensure []DesktopTarget
	remove []DesktopTarget
	err    error
}

func (desktop *fakeDesktop) Ensure(_ context.Context, target DesktopTarget) error {
	desktop.ensure = append(desktop.ensure, target)
	return desktop.err
}

func (desktop *fakeDesktop) RemoveOwned(_ context.Context, target DesktopTarget) error {
	desktop.remove = append(desktop.remove, target)
	return desktop.err
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

func TestDesktopFailureRollsBackDurableActivation(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "installation")
	packageOne, manifestOne := writeTestPackage(t, "1.0.0", "one")
	desktop := &fakeDesktop{}
	service := testService(t, desktop)
	first, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageOne, ConfigureDesktop: true})
	if err != nil {
		t.Fatal(err)
	}
	packageTwo, _ := writeTestPackage(t, "2.0.0", "two")
	desktop.err = errors.New("native registration failed")
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageTwo}); err == nil {
		t.Fatal("desktop activation failure was ignored")
	}
	if len(desktop.remove) != 1 {
		t.Fatalf("failed desktop activation was not cleaned up: %#v", desktop.remove)
	}
	desktop.err = nil
	status, err := service.Status(ctx, root)
	if err != nil || !status.Healthy || status.PackageSHA256 != manifestOne.RootSHA256 || !samePath(status.Executable, first.Executable) {
		t.Fatalf("rollback status=%#v err=%v", status, err)
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
	if err != nil || installed.State == nil {
		t.Fatalf("install=%#v err=%v", installed, err)
	}
	desktop.ensure = nil
	journal := transactionJournal{
		Format: transactionFormat, ID: "state-committed", Operation: "install",
		Phase: "slot-ready", NewSlot: installed.State.ActiveSlot,
		NewSHA256: installed.State.ActiveSHA256, UpdatedAt: time.Now().UTC(),
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
	data := filepath.Join(t.TempDir(), "data", productidentity.ConfigDirectory)
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "keep.json"), []byte("important"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := service.Uninstall(ctx, UninstallRequest{Root: root, PurgePaths: []string{data}})
	if err != nil || !removed.Changed || !removed.DataPreserved {
		t.Fatalf("uninstall=%#v err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(data, "keep.json")); err != nil {
		t.Fatalf("default uninstall removed user data: %v", err)
	}

	root = filepath.Join(t.TempDir(), "installation")
	if _, err := service.Install(ctx, ChangeRequest{Root: root, PackageRoot: packageRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Uninstall(ctx, UninstallRequest{Root: root, PurgeData: true, PurgeConfirmation: "yes", PurgePaths: []string{data}}); err == nil {
		t.Fatal("weak purge confirmation was accepted")
	}
	purged, err := service.Uninstall(ctx, UninstallRequest{
		Root: root, PurgeData: true, PurgeConfirmation: PurgeConfirmation,
		PurgePaths: []string{data},
	})
	if err != nil || purged.DataPreserved || !reflect.DeepEqual(purged.PurgedPaths, []string{data}) {
		t.Fatalf("purge=%#v err=%v", purged, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("confirmed data purge did not remove the exact directory: %v", err)
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
	executable := minimalResourcePE(version, testSourceSHA, buildTime, marker)
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

func minimalResourcePE(version, sourceSHA, buildTime, marker string) []byte {
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
		[]byte(sourceSHA), []byte(version), []byte(buildTime), []byte(marker),
	} {
		copy(resource[offset:], value)
		offset += len(value) + 8
	}
	return content
}
