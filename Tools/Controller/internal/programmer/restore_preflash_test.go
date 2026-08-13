package programmer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAVRRunner struct {
	mu           sync.Mutex
	calls        []string
	flashHEX     []byte
	eepromHEX    []byte
	failEEPROM   bool
	failMetadata bool
}

func (runner *fakeAVRRunner) Run(
	_ context.Context,
	command Command,
	output io.Writer,
) error {
	joined := strings.Join(command.Args, " ")
	runner.mu.Lock()
	runner.calls = append(runner.calls, joined)
	runner.mu.Unlock()
	if path := commandOutputPath(command, "-Uflash:r:"); path != "" {
		return os.WriteFile(path, runner.flashHEX, 0o600)
	}
	if path := commandOutputPath(command, "-Ueeprom:r:"); path != "" {
		if runner.failEEPROM {
			return errors.New("simulated EEPROM read failure")
		}
		return os.WriteFile(path, runner.eepromHEX, 0o600)
	}
	if strings.Contains(joined, "-xshowall") ||
		(!strings.Contains(joined, "-U") && strings.Contains(joined, "-c")) ||
		(strings.Contains(joined, "-Ulfuse:r:-:h") &&
			strings.Contains(joined, "-Uhfuse:r:-:h") &&
			strings.Contains(joined, "-Uefuse:r:-:h") &&
			strings.Contains(joined, "-Ulock:r:-:h")) {
		if runner.failMetadata {
			return errors.New("simulated metadata failure")
		}
		_, err := io.WriteString(output, "fake AVRDUDE metadata\n")
		return err
	}
	return fmt.Errorf("unexpected fake AVRDUDE command: %s", joined)
}

func commandOutputPath(command Command, prefix string) string {
	for _, argument := range command.Args {
		if strings.HasPrefix(argument, prefix) && strings.HasSuffix(argument, ":i") {
			return strings.TrimSuffix(strings.TrimPrefix(argument, prefix), ":i")
		}
	}
	return ""
}

func TestBackupManifestContentAddressingValidationAndRestorePlan(t *testing.T) {
	root := t.TempDir()
	runner := newFakeAVRRunner(t)
	date := uint32((2026-2000)<<9 | 8<<5 | 1)
	clock := uint32(19<<11 | 42<<5 | 58>>1)
	options := fakeBackupOptions(root)
	options.ApplicationIdentitySchema = 4
	options.ApplicationPackedTimestamp = date<<16 | clock
	directory, err := BackupWithRunner(context.Background(), options, io.Discard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "flash.hex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("per-operation duplicate flash still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "eeprom.hex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("per-operation duplicate EEPROM still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "programmer.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("per-operation duplicate metadata still exists: %v", err)
	}
	manifestPath := filepath.Join(directory, "manifest.json")
	validated, err := ValidateBackupManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	flash := validated.Files["flash"]
	if flash.Storage != "content-addressed" || !strings.Contains(flash.Name, flash.SHA256) ||
		validated.Manifest.Reference != "firmware-sha256:"+flash.SHA256 ||
		validated.Manifest.ApplicationTimestamp != "260801194258" ||
		validated.Manifest.ApplicationPackedTimestamp == "" {
		t.Fatalf("unexpected content-addressed manifest: %#v", validated.Manifest)
	}
	plan, err := PlanSafeRestore(manifestPath, RestorePlanOptions{
		Port: "COM18", Components: []RestoreComponent{RestoreFlash, RestoreEEPROM},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Options.Operation != OperationWriteFlash ||
		plan.Steps[1].Options.Operation != OperationWriteEEPROM ||
		!plan.Steps[1].Options.ConfirmEEPROMWrite {
		t.Fatalf("unexpected restore plan: %#v", plan)
	}
	if err := VerifyRestorePlan(plan); err != nil {
		t.Fatal(err)
	}
	usbPlan, err := PlanSafeRestore(manifestPath, RestorePlanOptions{
		Method:     MethodUSBasp,
		Components: []RestoreComponent{RestoreFlash},
	})
	if err != nil || len(usbPlan.Steps) != 1 {
		t.Fatalf("method-selected USBasp restore plan=%#v err=%v", usbPlan, err)
	}
}

func TestValidateBackupAcceptsCurrentAndMigrationPackedIdentitySchemas(t *testing.T) {
	date := uint32((2026-2000)<<9 | 8<<5 | 1)
	clock := uint32(19<<11 | 42<<5 | 58>>1)
	for _, schema := range []byte{3, 4} {
		options := fakeBackupOptions(t.TempDir())
		options.ApplicationIdentitySchema = schema
		options.ApplicationPackedTimestamp = date<<16 | clock
		if err := ValidateBackup(options); err != nil {
			t.Fatalf("schema %d: %v", schema, err)
		}
	}
	for _, schema := range []byte{1, 2} {
		options := fakeBackupOptions(t.TempDir())
		options.ApplicationIdentitySchema = schema
		options.ApplicationPackedTimestamp = date<<16 | clock
		if err := ValidateBackup(options); err == nil {
			t.Fatalf("obsolete schema %d packed timestamp was accepted", schema)
		}
	}
}

func TestBackupReusesContentAddressedFlashAndEEPROMAcrossOperations(t *testing.T) {
	root := t.TempDir()
	runner := newFakeAVRRunner(t)
	first, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("backup operation directories collided")
	}
	flashFiles := 0
	err = filepath.Walk(filepath.Join(root, "firmware"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info.Mode().IsRegular() {
			flashFiles++
		}
		return walkErr
	})
	if err != nil || flashFiles != 1 {
		t.Fatalf("firmware content store files=%d err=%v", flashFiles, err)
	}
	firstBackup, err := ValidateBackupManifest(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondBackup, err := ValidateBackupManifest(filepath.Join(second, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if firstBackup.Files["flash"].Path != secondBackup.Files["flash"].Path {
		t.Fatal("operation manifests do not reference the same content blob")
	}
	eepromFiles := 0
	err = filepath.Walk(filepath.Join(root, "eeprom"), func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info.Mode().IsRegular() {
			eepromFiles++
		}
		return walkErr
	})
	if err != nil || eepromFiles != 1 {
		t.Fatalf("EEPROM content store files=%d err=%v", eepromFiles, err)
	}
	if firstBackup.Files["eeprom"].Path != secondBackup.Files["eeprom"].Path {
		t.Fatal("operation manifests do not reference the same EEPROM content blob")
	}
	metadataFiles := 0
	err = filepath.Walk(filepath.Join(root, "metadata"), func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info.Mode().IsRegular() {
			metadataFiles++
		}
		return walkErr
	})
	if err != nil || metadataFiles != 1 {
		t.Fatalf("metadata content store files=%d err=%v", metadataFiles, err)
	}
	if firstBackup.Files["metadata"].Path != secondBackup.Files["metadata"].Path {
		t.Fatal("operation manifests do not reference the same metadata blob")
	}
}

func TestBackupManifestTamperAndTraversalAreRejected(t *testing.T) {
	root := t.TempDir()
	runner := newFakeAVRRunner(t)
	directory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "manifest.json")
	validated, err := ValidateBackupManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	eeprom := validated.Files["eeprom"].Path
	if err := os.WriteFile(eeprom, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateBackupManifest(manifestPath); err == nil ||
		(!strings.Contains(err.Error(), "size") && !strings.Contains(err.Error(), "SHA-256")) {
		t.Fatalf("tamper was not rejected: %v", err)
	}

	secondDirectory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(filepath.Join(root, "second")),
		io.Discard, newFakeAVRRunner(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	secondManifestPath := filepath.Join(secondDirectory, "manifest.json")
	content, err := os.ReadFile(secondManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest BackupManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Kind == "eeprom" {
			manifest.Files[index].RelativePath = "../../outside.hex"
		}
	}
	content, _ = json.Marshal(manifest)
	if err := os.WriteFile(secondManifestPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateBackupManifest(secondManifestPath); err == nil ||
		!strings.Contains(err.Error(), "escapes") {
		t.Fatalf("path traversal was not rejected: %v", err)
	}
}

func TestAutomaticBackupThenFlashUsesCapabilityAwareBackupPolicy(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "new.hex")
	firmwareImage := &IntelHexImage{data: map[uint32]byte{0: 0x99, 1: 0x88}}
	firmwareHEX, _ := firmwareImage.Canonical()
	if err := os.WriteFile(firmware, firmwareHEX, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := newFakeAVRRunner(t)
	flashed := 0
	result, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup:       fakeBackupOptions(filepath.Join(root, "backups")),
		}, runner,
		func(_ context.Context, path string, _ io.Writer) error {
			flashed++
			if path != firmware {
				t.Fatalf("flash path=%q", path)
			}
			return nil
		}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BackupComplete || !result.Flashed || flashed != 1 ||
		result.BackupReference == "" {
		t.Fatalf("unexpected automatic result=%#v flashed=%d", result, flashed)
	}

	failing := newFakeAVRRunner(t)
	failing.failEEPROM = true
	flashed = 0
	failedResult, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup:       fakeBackupOptions(filepath.Join(root, "failed-backups")),
		}, failing,
		func(context.Context, string, io.Writer) error { flashed++; return nil },
		io.Discard,
	)
	if err != nil || flashed != 1 || !failedResult.Flashed ||
		failedResult.BackupComplete || len(failedResult.Warnings) != 1 {
		t.Fatalf("application-only update result=%#v flashed=%d err=%v", failedResult, flashed, err)
	}

	required := newFakeAVRRunner(t)
	required.failEEPROM = true
	flashed = 0
	requiredResult, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath:          firmware,
			Backup:                fakeBackupOptions(filepath.Join(root, "required-backups")),
			RequireCompleteBackup: true,
		}, required,
		func(context.Context, string, io.Writer) error { flashed++; return nil },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "refusing to flash") || flashed != 0 ||
		requiredResult.BackupComplete {
		t.Fatalf("required backup result=%#v flashed=%d err=%v", requiredResult, flashed, err)
	}
}

func TestAutomaticBackupThenFlashAcceptsExplicitUSBaspMethod(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "new.hex")
	image := &IntelHexImage{data: map[uint32]byte{0: 1}}
	content, _ := image.Canonical()
	if err := os.WriteFile(firmware, content, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := newFakeAVRRunner(t)
	result, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup: Options{
				Method: MethodUSBasp, OutputPath: filepath.Join(root, "backup"),
				Avrdude: "fake-avrdude", AvrdudeConf: "fake-avrdude.conf",
			},
		}, runner, func(context.Context, string, io.Writer) error { return nil }, io.Discard,
	)
	if err != nil || !result.BackupComplete || !result.Flashed {
		t.Fatalf("method-selected USBasp result=%#v err=%v", result, err)
	}
	if len(runner.calls) == 0 {
		t.Fatal("method-selected USBasp did not invoke the programmer runner")
	}
}

func TestAutomaticBackupThenFlashRunsPostBackupGateBeforeFlash(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "new.hex")
	content, _ := (&IntelHexImage{data: map[uint32]byte{0: 1}}).Canonical()
	if err := os.WriteFile(firmware, content, 0o600); err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, 2)
	result, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup:       fakeBackupOptions(filepath.Join(root, "backups")),
			AfterBackup: func(_ context.Context, backup AutomaticPreflashResult, _ io.Writer) error {
				if !backup.BackupComplete || backup.BackupReference == "" {
					t.Fatalf("post-backup gate received incomplete result: %#v", backup)
				}
				order = append(order, "latched")
				return nil
			},
		}, newFakeAVRRunner(t),
		func(context.Context, string, io.Writer) error {
			order = append(order, "flashed")
			return nil
		}, io.Discard,
	)
	if err != nil || !result.Flashed || strings.Join(order, ",") != "latched,flashed" {
		t.Fatalf("post-backup order=%v result=%#v err=%v", order, result, err)
	}

	flashed := false
	_, err = AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup:       fakeBackupOptions(filepath.Join(root, "second-backup")),
			AfterBackup: func(context.Context, AutomaticPreflashResult, io.Writer) error {
				return errors.New("cannot arm latch")
			},
		}, newFakeAVRRunner(t),
		func(context.Context, string, io.Writer) error { flashed = true; return nil }, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "cannot arm latch") || flashed {
		t.Fatalf("failed post-backup gate flashed=%t err=%v", flashed, err)
	}
}

func TestAutomaticBackupThenFlashRejectsFirmwareChangedDuringBackup(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "new.hex")
	initialImage := &IntelHexImage{data: map[uint32]byte{0: 1}}
	initial, _ := initialImage.Canonical()
	if err := os.WriteFile(firmware, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := newFakeAVRRunner(t)
	mutatedImage := &IntelHexImage{data: map[uint32]byte{0: 2}}
	mutated, _ := mutatedImage.Canonical()
	wrapped := CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
		err := runner.Run(ctx, command, output)
		if err == nil && commandOutputPath(command, "-Ueeprom:r:") != "" {
			if writeErr := os.WriteFile(firmware, mutated, 0o600); writeErr != nil {
				return writeErr
			}
		}
		return err
	})
	flashed := false
	_, err := AutomaticBackupThenFlash(
		context.Background(), AutomaticPreflashOptions{
			FirmwarePath: firmware,
			Backup:       fakeBackupOptions(filepath.Join(root, "backups")),
		}, wrapped,
		func(context.Context, string, io.Writer) error { flashed = true; return nil },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "firmware changed") || flashed {
		t.Fatalf("changed firmware guard: flashed=%t err=%v", flashed, err)
	}
}

func newFakeAVRRunner(t *testing.T) *fakeAVRRunner {
	t.Helper()
	flashImage := &IntelHexImage{data: map[uint32]byte{0: 1, 1: 2, 0x200: 3}}
	eepromData := make(map[uint32]byte, PCControllerEEPROMBytes)
	for address := uint32(0); address < PCControllerEEPROMBytes; address++ {
		eepromData[address] = 0xFF
	}
	eepromData[PCControllerEEPROMBytes-1] = 0xAA
	eepromImage := &IntelHexImage{data: eepromData}
	flashHEX, err := flashImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	eepromHEX, err := eepromImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeAVRRunner{flashHEX: flashHEX, eepromHEX: eepromHEX}
}

func fakeBackupOptions(root string) Options {
	return Options{
		Method: MethodUrclock, Operation: OperationBackup,
		Port: "COM18", OutputPath: root,
		Avrdude: "fake-avrdude", AvrdudeConf: "fake-avrdude.conf",
		MCU: "atmega328p", ApplicationHash: 0x1234ABCD,
	}
}

func TestRestorePlanDetectsPostConfirmationMutation(t *testing.T) {
	root := t.TempDir()
	directory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSafeRestore(filepath.Join(directory, "manifest.json"), RestorePlanOptions{
		Port: "COM18", Components: []RestoreComponent{RestoreEEPROM},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Steps[0].Path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRestorePlan(plan); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("post-confirmation mutation not found: %v", err)
	}
}

func TestBackupManifestTimestampsRemainOperationSpecific(t *testing.T) {
	root := t.TempDir()
	first, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ValidateBackupManifest(filepath.Join(first, "manifest.json"))
	b, _ := ValidateBackupManifest(filepath.Join(second, "manifest.json"))
	if !b.Manifest.CreatedAt.After(a.Manifest.CreatedAt) || a.Manifest.Reference != b.Manifest.Reference {
		t.Fatalf("operation times/reference a=%#v b=%#v", a.Manifest, b.Manifest)
	}
}

func TestBackupRetentionPrunesExpiredButProtectsPrevious(t *testing.T) {
	root := t.TempDir()
	makeBackup := func(created time.Time, expired bool) string {
		directory, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "manifest.json")
		content, _ := os.ReadFile(path)
		var manifest BackupManifest
		_ = json.Unmarshal(content, &manifest)
		manifest.CreatedAt = created
		manifest.CompletedAt = created.Add(time.Second)
		manifest.RetainUntil = created.Add(24 * time.Hour)
		if !expired {
			manifest.RetainUntil = created.Add(365 * 24 * time.Hour)
		}
		content, _ = json.Marshal(manifest)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return directory
	}
	now := time.Now().UTC()
	old := makeBackup(now.Add(-72*time.Hour), true)
	previous := makeBackup(now.Add(-48*time.Hour), true)
	current := makeBackup(now.Add(-time.Hour), false)
	removed, err := PruneExpiredBackups(root, now, current)
	if err != nil || len(removed) > 1 || (len(removed) == 1 && filepath.Clean(removed[0]) != filepath.Clean(old)) {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired backup was retained: %v", err)
	}
	if _, err := os.Stat(previous); err != nil {
		t.Fatalf("previous backup was not protected: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current backup was not protected: %v", err)
	}
}

func TestBackupRetentionPrunesUnreferencedContentButKeepsSharedBlob(t *testing.T) {
	root := t.TempDir()
	first, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
	if err != nil {
		t.Fatal(err)
	}
	firstValidated, err := ValidateBackupManifest(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	shared := firstValidated.Files["flash"].Path
	content, _ := os.ReadFile(filepath.Join(first, "manifest.json"))
	var manifest BackupManifest
	_ = json.Unmarshal(content, &manifest)
	manifest.CreatedAt = time.Now().UTC().Add(-72 * time.Hour)
	manifest.CompletedAt = manifest.CreatedAt.Add(time.Second)
	manifest.RetainUntil = manifest.CreatedAt.Add(time.Hour)
	content, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(first, "manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := BackupWithRunner(context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PruneExpiredBackups(root, time.Now().UTC(), third); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired operation still exists: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("previous operation was lost: %v", err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared content blob referenced by retained operations was lost: %v", err)
	}
}
