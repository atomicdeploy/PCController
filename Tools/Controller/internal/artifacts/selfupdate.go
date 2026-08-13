package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	selfUpdateJournalSchema  = 1
	selfUpdateHelperCommand  = "__self-update-helper"
	healthPathEnvironment    = "CONTROLLER_SELF_UPDATE_HEALTH_PATH"
	healthTokenEnvironment   = "CONTROLLER_SELF_UPDATE_HEALTH_TOKEN"
	healthJournalEnvironment = "CONTROLLER_SELF_UPDATE_JOURNAL"
	defaultHealthTimeout     = 30 * time.Second
)

// SelfUpdatePlan records every recoverable file in the deferred transaction.
type SelfUpdatePlan struct {
	CurrentPath string `json:"current_path"`
	StagedPath  string `json:"staged_path"`
	BackupPath  string `json:"backup_path"`
	HelperPath  string `json:"helper_path"`
	JournalPath string `json:"journal_path"`
	HealthPath  string `json:"health_path"`
	SHA256      string `json:"sha256"`
}

type selfUpdateJournal struct {
	Schema            int       `json:"schema"`
	State             string    `json:"state"`
	UpdatedAt         time.Time `json:"updated_at"`
	ParentPID         int       `json:"parent_pid"`
	ChildPID          int       `json:"child_pid,omitempty"`
	CurrentPath       string    `json:"current_path"`
	StagedPath        string    `json:"staged_path"`
	BackupPath        string    `json:"backup_path"`
	HelperPath        string    `json:"helper_path"`
	JournalPath       string    `json:"journal_path"`
	HealthPath        string    `json:"health_path"`
	HealthToken       string    `json:"health_token"`
	HealthTimeoutMS   int64     `json:"health_timeout_ms"`
	CurrentSHA256     string    `json:"current_sha256"`
	ReplacementSHA256 string    `json:"replacement_sha256"`
	Arguments         []string  `json:"arguments"`
	WorkingDirectory  string    `json:"working_directory"`
	Failure           string    `json:"failure,omitempty"`
}

type HelperLauncher interface {
	Launch(context.Context, string, string) error
}

type HelperLauncherFunc func(context.Context, string, string) error

func (function HelperLauncherFunc) Launch(ctx context.Context, path, journal string) error {
	return function(ctx, path, journal)
}

// PrepareSelfUpdate validates both executable headers, preserves the exact
// argv/config selection and working directory, and starts an external copy of
// the current host as a health-checking transaction coordinator.
func PrepareSelfUpdate(
	ctx context.Context,
	currentPath, artifactPath, expectedSHA256 string,
	launcher HelperLauncher,
) (SelfUpdatePlan, error) {
	return PrepareSelfUpdateWithOptions(ctx, SelfUpdateOptions{
		CurrentPath: currentPath, ArtifactPath: artifactPath,
		ExpectedSHA256: expectedSHA256, Arguments: os.Args[1:], Launcher: launcher,
	})
}

type SelfUpdateOptions struct {
	CurrentPath      string
	ArtifactPath     string
	ExpectedSHA256   string
	Arguments        []string
	WorkingDirectory string
	HealthTimeout    time.Duration
	Launcher         HelperLauncher
}

func PrepareSelfUpdateWithOptions(ctx context.Context, options SelfUpdateOptions) (SelfUpdatePlan, error) {
	if options.Launcher == nil {
		options.Launcher = platformHelperLauncher{}
	}
	currentPath, err := filepath.Abs(strings.TrimSpace(options.CurrentPath))
	if err != nil {
		return SelfUpdatePlan{}, err
	}
	artifactPath, err := filepath.Abs(strings.TrimSpace(options.ArtifactPath))
	if err != nil {
		return SelfUpdatePlan{}, err
	}
	if currentPath == artifactPath {
		return SelfUpdatePlan{}, errors.New("self-update artifact must not be the running executable")
	}
	expected, err := normalizeSHA256(options.ExpectedSHA256)
	if err != nil {
		return SelfUpdatePlan{}, err
	}
	currentIdentity, err := InspectHostExecutable(currentPath)
	if err != nil {
		return SelfUpdatePlan{}, fmt.Errorf("inspect running executable: %w", err)
	}
	replacementIdentity, err := InspectHostExecutable(artifactPath)
	if err != nil {
		return SelfUpdatePlan{}, fmt.Errorf("inspect replacement executable: %w", err)
	}
	if currentIdentity.Platform() != replacementIdentity.Platform() {
		return SelfUpdatePlan{}, fmt.Errorf(
			"replacement platform %q does not match running host %q",
			replacementIdentity.Platform(), currentIdentity.Platform(),
		)
	}
	if err := verifyFileDigest(artifactPath, expected); err != nil {
		return SelfUpdatePlan{}, fmt.Errorf("verify self-update artifact: %w", err)
	}
	currentDigest, err := fileDigest(currentPath)
	if err != nil {
		return SelfUpdatePlan{}, fmt.Errorf("hash running executable: %w", err)
	}
	workingDirectory := strings.TrimSpace(options.WorkingDirectory)
	if workingDirectory == "" {
		workingDirectory, err = os.Getwd()
		if err != nil {
			return SelfUpdatePlan{}, fmt.Errorf("resolve self-update working directory: %w", err)
		}
	}
	workingDirectory, err = filepath.Abs(workingDirectory)
	if err != nil {
		return SelfUpdatePlan{}, err
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = defaultHealthTimeout
	}
	if options.HealthTimeout < time.Second || options.HealthTimeout > 5*time.Minute {
		return SelfUpdatePlan{}, errors.New("self-update health timeout must be between 1 second and 5 minutes")
	}
	stem := "." + filepath.Base(currentPath) + ".update-" + expected[:12]
	directory := filepath.Dir(currentPath)
	plan := SelfUpdatePlan{
		CurrentPath: currentPath,
		StagedPath:  filepath.Join(directory, stem+".new"),
		BackupPath:  filepath.Join(directory, stem+".old"),
		HelperPath:  filepath.Join(directory, stem+platformHelperExtension()),
		JournalPath: filepath.Join(directory, stem+".journal.json"),
		HealthPath:  filepath.Join(directory, stem+".healthy"),
		SHA256:      expected,
	}
	for _, stale := range []string{plan.HealthPath, plan.BackupPath} {
		_ = os.Remove(stale)
	}
	if err := copyVerifiedArtifact(artifactPath, plan.StagedPath, expected); err != nil {
		return SelfUpdatePlan{}, err
	}
	if err := copyVerifiedArtifact(currentPath, plan.HelperPath, currentDigest); err != nil {
		_ = os.Remove(plan.StagedPath)
		return SelfUpdatePlan{}, fmt.Errorf("stage self-update coordinator: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		return SelfUpdatePlan{}, err
	}
	journal := selfUpdateJournal{
		Schema: selfUpdateJournalSchema, State: "prepared", UpdatedAt: time.Now().UTC(),
		ParentPID: os.Getpid(), CurrentPath: plan.CurrentPath, StagedPath: plan.StagedPath,
		BackupPath: plan.BackupPath, HelperPath: plan.HelperPath, JournalPath: plan.JournalPath,
		HealthPath: plan.HealthPath, HealthToken: token,
		HealthTimeoutMS: options.HealthTimeout.Milliseconds(),
		CurrentSHA256:   currentDigest, ReplacementSHA256: expected,
		Arguments: append([]string(nil), options.Arguments...), WorkingDirectory: workingDirectory,
	}
	if err := writeJSONAtomic(plan.JournalPath, journal); err != nil {
		_ = os.Remove(plan.HelperPath)
		_ = os.Remove(plan.StagedPath)
		return SelfUpdatePlan{}, fmt.Errorf("write self-update journal: %w", err)
	}
	if err := options.Launcher.Launch(ctx, plan.HelperPath, plan.JournalPath); err != nil {
		_ = os.Remove(plan.HelperPath)
		_ = os.Remove(plan.StagedPath)
		return SelfUpdatePlan{}, fmt.Errorf("launch deferred self-update helper: %w", err)
	}
	return plan, nil
}

// IsSelfUpdateHelperInvocation identifies the private coordinator mode before
// normal configuration, IPC, serial, or UI initialization can occur.
func IsSelfUpdateHelperInvocation(args []string) bool {
	return len(args) == 2 && args[0] == selfUpdateHelperCommand && strings.TrimSpace(args[1]) != ""
}

// RunSelfUpdateHelper performs atomic replacement, starts the candidate with
// the preserved argv/cwd, and rolls back if it exits or misses health timeout.
func RunSelfUpdateHelper(ctx context.Context, journalPath string) error {
	journal, err := loadSelfUpdateJournal(journalPath)
	if err != nil {
		return err
	}
	if err := validateSelfUpdateJournal(journal); err != nil {
		return err
	}
	if err := waitForProcessExit(ctx, journal.ParentPID, 3*time.Minute); err != nil {
		return updateJournalFailure(&journal, "parent-wait-failed", err)
	}
	if err := verifyFileDigest(journal.CurrentPath, journal.CurrentSHA256); err != nil {
		return updateJournalFailure(&journal, "current-verification-failed", err)
	}
	if err := verifyFileDigest(journal.StagedPath, journal.ReplacementSHA256); err != nil {
		return updateJournalFailure(&journal, "replacement-verification-failed", err)
	}
	journal.State = "replacing"
	journal.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(journal.JournalPath, journal); err != nil {
		return err
	}
	if err := copyVerifiedArtifact(journal.CurrentPath, journal.BackupPath, journal.CurrentSHA256); err != nil {
		return updateJournalFailure(&journal, "backup-failed", err)
	}
	if err := replaceExecutable(journal.StagedPath, journal.CurrentPath); err != nil {
		return updateJournalFailure(&journal, "replacement-failed", err)
	}
	if err := verifyFileDigest(journal.CurrentPath, journal.ReplacementSHA256); err != nil {
		_ = rollbackSelfUpdate(&journal)
		return updateJournalFailure(&journal, "replacement-verification-failed", err)
	}
	child := exec.Command(journal.CurrentPath, journal.Arguments...)
	child.Dir = journal.WorkingDirectory
	child.Env = selfUpdateEnvironment(os.Environ(), journal)
	inheritSelfUpdateIO(child)
	if err := platformStartReplacementProcess(child); err != nil {
		_ = rollbackSelfUpdate(&journal)
		return updateJournalFailure(&journal, "candidate-start-failed", err)
	}
	journal.ChildPID = child.Process.Pid
	journal.State = "awaiting-health"
	journal.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(journal.JournalPath, journal); err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		_ = rollbackSelfUpdate(&journal)
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	healthTimeout := time.Duration(journal.HealthTimeoutMS) * time.Millisecond
	if err := waitForSelfUpdateHealth(ctx, journal.HealthPath, journal.HealthToken, healthTimeout, exited); err != nil {
		_ = child.Process.Kill()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
		if rollbackErr := rollbackSelfUpdate(&journal); rollbackErr != nil {
			return updateJournalFailure(&journal, "rollback-failed", errors.Join(err, rollbackErr))
		}
		journal.State = "rolled-back"
		journal.Failure = err.Error()
		journal.UpdatedAt = time.Now().UTC()
		_ = writeJSONAtomic(journal.JournalPath, journal)
		if restartErr := launchRestoredHost(journal); restartErr != nil {
			return fmt.Errorf("candidate unhealthy (%v); rollback succeeded but restart failed: %w", err, restartErr)
		}
		return nil
	}
	journal.State = "committed"
	journal.Failure = ""
	journal.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(journal.JournalPath, journal); err != nil {
		return err
	}
	_ = os.Remove(journal.BackupPath)
	_ = os.Remove(journal.HealthPath)
	return nil
}

// ScheduleSelfUpdateHealthAcknowledgement only runs in a replacement process.
// A short survival delay prevents a process that immediately fails its config
// or startup path from acknowledging health.
func ScheduleSelfUpdateHealthAcknowledgement(delay time.Duration) {
	path := strings.TrimSpace(os.Getenv(healthPathEnvironment))
	token := strings.TrimSpace(os.Getenv(healthTokenEnvironment))
	if delay <= 0 {
		delay = 750 * time.Millisecond
	}
	if path == "" || token == "" {
		go func() {
			time.Sleep(delay)
			_ = recoverOrphanedSelfUpdates()
		}()
		return
	}
	journalPath := strings.TrimSpace(os.Getenv(healthJournalEnvironment))
	go func() {
		time.Sleep(delay)
		if journalPath != "" {
			if journal, err := loadSelfUpdateJournal(journalPath); err == nil {
				journal.State = "candidate-healthy"
				journal.UpdatedAt = time.Now().UTC()
				_ = writeJSONAtomic(journal.JournalPath, journal)
			}
		}
		_ = writeHealthAcknowledgement(path, token)
	}()
}

// recoverOrphanedSelfUpdates resolves journals left behind if the coordinator
// itself was interrupted. Atomic replacement guarantees the current path is
// either the old or candidate image; a surviving process proves which one won.
func recoverOrphanedSelfUpdates() error {
	current, err := os.Executable()
	if err != nil {
		return err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return err
	}
	pattern := filepath.Join(filepath.Dir(current), "."+filepath.Base(current)+".update-*.journal.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	currentHash, err := fileDigest(current)
	if err != nil {
		return err
	}
	for _, path := range paths {
		journal, loadErr := loadSelfUpdateJournal(path)
		if loadErr != nil || journal.CurrentPath != current || selfUpdateTerminal(journal.State) {
			continue
		}
		switch currentHash {
		case journal.ReplacementSHA256:
			journal.State = "committed-recovered"
			journal.Failure = "coordinator interruption recovered after candidate survived startup"
			_ = os.Remove(journal.BackupPath)
			_ = os.Remove(journal.HealthPath)
		case journal.CurrentSHA256:
			journal.State = "rolled-back-recovered"
			journal.Failure = "coordinator interruption recovered with original executable intact"
			_ = os.Remove(journal.StagedPath)
			_ = os.Remove(journal.HealthPath)
		default:
			continue
		}
		journal.UpdatedAt = time.Now().UTC()
		_ = writeJSONAtomic(journal.JournalPath, journal)
	}
	return nil
}

func selfUpdateTerminal(state string) bool {
	switch state {
	case "committed", "rolled-back", "committed-recovered", "rolled-back-recovered":
		return true
	default:
		return false
	}
}

func waitForSelfUpdateHealth(ctx context.Context, path, token string, timeout time.Duration, exited <-chan error) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			if err == nil {
				return errors.New("replacement exited before health acknowledgement")
			}
			return fmt.Errorf("replacement exited before health acknowledgement: %w", err)
		case <-timer.C:
			return errors.New("replacement health acknowledgement timed out")
		case <-ticker.C:
			if healthAcknowledged(path, token) {
				return nil
			}
		}
	}
}

func writeHealthAcknowledgement(path, token string) error {
	return writeJSONAtomic(path, map[string]string{"token": token})
}

func healthAcknowledged(path, token string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var value map[string]string
	return json.Unmarshal(content, &value) == nil && value["token"] == token
}

func loadSelfUpdateJournal(path string) (selfUpdateJournal, error) {
	path, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return selfUpdateJournal{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return selfUpdateJournal{}, err
	}
	var journal selfUpdateJournal
	if err := strictJSON(content, &journal); err != nil {
		return selfUpdateJournal{}, fmt.Errorf("decode self-update journal: %w", err)
	}
	if journal.JournalPath == "" {
		journal.JournalPath = path
	}
	return journal, nil
}

func validateSelfUpdateJournal(journal selfUpdateJournal) error {
	if journal.Schema != selfUpdateJournalSchema || journal.State != "prepared" {
		return errors.New("self-update journal is not a prepared transaction")
	}
	paths := []string{journal.CurrentPath, journal.StagedPath, journal.BackupPath,
		journal.HelperPath, journal.JournalPath, journal.HealthPath, journal.WorkingDirectory}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			return errors.New("self-update journal contains a non-absolute path")
		}
	}
	base := filepath.Dir(journal.CurrentPath)
	for _, path := range paths[1:6] {
		relative, err := filepath.Rel(base, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("self-update journal path escapes the executable directory")
		}
	}
	if _, err := normalizeSHA256(journal.CurrentSHA256); err != nil {
		return err
	}
	if _, err := normalizeSHA256(journal.ReplacementSHA256); err != nil {
		return err
	}
	if journal.HealthTimeoutMS < 1000 || journal.HealthTimeoutMS > int64((5*time.Minute)/time.Millisecond) {
		return errors.New("self-update journal health timeout is invalid")
	}
	return nil
}

func rollbackSelfUpdate(journal *selfUpdateJournal) error {
	if err := verifyFileDigest(journal.BackupPath, journal.CurrentSHA256); err != nil {
		return fmt.Errorf("verify rollback executable: %w", err)
	}
	if err := replaceExecutable(journal.BackupPath, journal.CurrentPath); err != nil {
		return fmt.Errorf("restore previous executable: %w", err)
	}
	return verifyFileDigest(journal.CurrentPath, journal.CurrentSHA256)
}

func launchRestoredHost(journal selfUpdateJournal) error {
	command := exec.Command(journal.CurrentPath, journal.Arguments...)
	command.Dir = journal.WorkingDirectory
	command.Env = withoutSelfUpdateEnvironment(os.Environ())
	inheritSelfUpdateIO(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// inheritSelfUpdateIO preserves the coordinator's visible terminal or service
// log streams while the external helper replaces and restarts it. Leaving the
// exec.Cmd streams nil connects the candidate to null devices, which makes an
// interactive TUI exit immediately and leaves its terminal blank after an
// otherwise valid bridge-initiated update.
func inheritSelfUpdateIO(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
}

func selfUpdateEnvironment(environment []string, journal selfUpdateJournal) []string {
	result := withoutSelfUpdateEnvironment(environment)
	return append(result,
		healthPathEnvironment+"="+journal.HealthPath,
		healthTokenEnvironment+"="+journal.HealthToken,
		healthJournalEnvironment+"="+journal.JournalPath,
	)
}

func withoutSelfUpdateEnvironment(environment []string) []string {
	prefixes := []string{healthPathEnvironment + "=", healthTokenEnvironment + "=", healthJournalEnvironment + "="}
	result := make([]string, 0, len(environment))
	for _, value := range environment {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(strings.ToUpper(value), prefix) {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, value)
		}
	}
	return result
}

func updateJournalFailure(journal *selfUpdateJournal, state string, cause error) error {
	journal.State = state
	journal.Failure = cause.Error()
	journal.UpdatedAt = time.Now().UTC()
	_ = writeJSONAtomic(journal.JournalPath, *journal)
	return cause
}

func copyVerifiedArtifact(source, destination, expected string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".self-update-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return err
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), input); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expected {
		return fmt.Errorf("staged self-update SHA-256 mismatch: expected %s, received %s", expected, actual)
	}
	_ = os.Remove(destination)
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("publish staged self-update: %w", err)
	}
	return nil
}

func verifyFileDigest(path, expected string) error {
	actual, err := fileDigest(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA-256 mismatch: expected %s, received %s", expected, actual)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func randomToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
