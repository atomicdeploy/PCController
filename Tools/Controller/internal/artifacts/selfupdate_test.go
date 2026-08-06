package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPrepareSelfUpdateStagesVerifiedArtifactAndUsesInjectedLauncher(t *testing.T) {
	directory := t.TempDir()
	current := filepath.Join(directory, "controller-current.exe")
	replacement := filepath.Join(directory, "controller-new.exe")
	running, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTestExecutable(running, current, nil); err != nil {
		t.Fatal(err)
	}
	trailer := []byte("replacement-build")
	if err := copyTestExecutable(running, replacement, trailer); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	hash := hex.EncodeToString(digest[:])
	var launched, launchedJournal string
	arguments := []string{"web", "--config", filepath.Join(directory, "config.json")}
	plan, err := PrepareSelfUpdateWithOptions(context.Background(), SelfUpdateOptions{
		CurrentPath: current, ArtifactPath: replacement, ExpectedSHA256: hash,
		Arguments: arguments, WorkingDirectory: directory, HealthTimeout: 2 * time.Second,
		Launcher: HelperLauncherFunc(func(_ context.Context, helper, journal string) error {
			launched, launchedJournal = helper, journal
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SHA256 != hash || launched != plan.HelperPath || launchedJournal != plan.JournalPath {
		t.Fatalf("plan=%#v launched=%q journal=%q", plan, launched, launchedJournal)
	}
	if err := verifyFileDigest(plan.StagedPath, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.HelperPath); err != nil {
		t.Fatal(err)
	}
	journal, err := loadSelfUpdateJournal(plan.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != "prepared" || journal.WorkingDirectory != directory ||
		!reflect.DeepEqual(journal.Arguments, arguments) {
		t.Fatalf("journal=%#v", journal)
	}
}

func TestSelfUpdateHealthAcknowledgementUsesUnforgeableToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	if err := writeHealthAcknowledgement(path, "expected-token"); err != nil {
		t.Fatal(err)
	}
	if !healthAcknowledged(path, "expected-token") || healthAcknowledged(path, "other") {
		t.Fatal("health token was not checked exactly")
	}
}

func TestReplaceExecutablePublishesIntoAnExistingCanonicalPath(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "controller.new")
	destination := filepath.Join(directory, "controller.exe")
	if err := os.WriteFile(source, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("current"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "replacement" {
		t.Fatalf("destination content=%q", content)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
}

func copyTestExecutable(source, destination string, trailer []byte) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if _, err := output.Write(trailer); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
