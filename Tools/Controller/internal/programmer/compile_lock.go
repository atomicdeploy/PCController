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
	"time"
)

const compileLockRetryInterval = 100 * time.Millisecond

// compileExecutionLock serializes every controller-owned firmware compile that
// shares the managed Arduino cache. The operating system releases the lock if
// its process exits, so an interrupted build cannot strand a stale mutex.
type compileExecutionLock struct {
	file *os.File
	path string
}

// compileLockOwner makes lock contention actionable without participating in
// ownership; the kernel lock, rather than this diagnostic record, is decisive.
type compileLockOwner struct {
	PID        int    `json:"pid"`
	Hostname   string `json:"hostname,omitempty"`
	AcquiredAt string `json:"acquired_at"`
	SourceHash string `json:"source_hash"`
	SourceRoot string `json:"source_root"`
}

// acquireCompileExecutionLock waits interruptibly for the cache-wide compiler
// lease. It must be held from before staging through final manifest creation.
func acquireCompileExecutionLock(
	ctx context.Context,
	identity CompileIdentity,
	output io.Writer,
) (*compileExecutionLock, error) {
	if ctx == nil {
		return nil, errors.New("firmware compile lock requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for firmware compile lock: %w", err)
	}
	cacheRoot := filepath.Dir(filepath.Dir(identity.SketchPath))
	if strings.TrimSpace(cacheRoot) == "" || cacheRoot == "." {
		return nil, errors.New("firmware compile lock requires a managed cache path")
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create firmware compile lock directory %s: %w", cacheRoot, err)
	}
	lockPath := filepath.Join(cacheRoot, ".compile.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open firmware compile lock %s: %w", lockPath, err)
	}

	waitingReported := false
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("wait for firmware compile lock %s: %w", lockPath, err)
		}
		locked, lockErr := tryLockCompileFile(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire firmware compile lock %s: %w", lockPath, lockErr)
		}
		if locked {
			previous, hadPrevious := readCompileLockOwner(lockPath)
			owner := compileLockOwner{
				PID: os.Getpid(), AcquiredAt: time.Now().UTC().Format(time.RFC3339Nano),
				SourceHash: fmt.Sprintf("%08X", identity.SourceHash),
				SourceRoot: identity.SourceRoot,
			}
			owner.Hostname, _ = os.Hostname()
			if err := writeCompileLockOwner(file, owner); err != nil {
				_ = unlockCompileFile(file)
				_ = file.Close()
				return nil, fmt.Errorf("record firmware compile lock owner: %w", err)
			}
			if output != nil {
				switch {
				case waitingReported:
					fmt.Fprintf(output, "Firmware compile lock acquired for source %08X.\n", identity.SourceHash)
				case hadPrevious:
					fmt.Fprintf(
						output,
						"Recovered abandoned firmware compile lock from %s; source %08X now owns it.\n",
						formatCompileLockOwner(previous),
						identity.SourceHash,
					)
				}
			}
			return &compileExecutionLock{file: file, path: lockPath}, nil
		}

		if !waitingReported {
			waitingReported = true
			if output != nil {
				if owner, ok := readCompileLockOwner(lockPath); ok {
					fmt.Fprintf(
						output,
						"Another firmware compile is active (%s); waiting for exclusive staging ownership.\n",
						formatCompileLockOwner(owner),
					)
				} else {
					fmt.Fprintln(output, "Another firmware compile is active; waiting for exclusive staging ownership.")
				}
			}
		}

		timer := time.NewTimer(compileLockRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, fmt.Errorf("wait for firmware compile lock %s: %w", lockPath, ctx.Err())
		case <-timer.C:
		}
	}
}

// Release clears diagnostic ownership before dropping the kernel lease. The
// empty lock file is intentionally reusable and never represents ownership.
func (lock *compileExecutionLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	var failures []error
	if err := lock.file.Truncate(0); err != nil {
		failures = append(failures, fmt.Errorf("clear firmware compile lock %s: %w", lock.path, err))
	}
	if err := lock.file.Sync(); err != nil {
		failures = append(failures, fmt.Errorf("sync firmware compile lock %s: %w", lock.path, err))
	}
	if err := unlockCompileFile(lock.file); err != nil {
		failures = append(failures, fmt.Errorf("release firmware compile lock %s: %w", lock.path, err))
	}
	if err := lock.file.Close(); err != nil {
		failures = append(failures, fmt.Errorf("close firmware compile lock %s: %w", lock.path, err))
	}
	lock.file = nil
	return errors.Join(failures...)
}

func writeCompileLockOwner(file *os.File, owner compileLockOwner) error {
	encoded, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		return err
	}
	return file.Sync()
}

func readCompileLockOwner(path string) (compileLockOwner, bool) {
	content, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(content))) == 0 {
		return compileLockOwner{}, false
	}
	var owner compileLockOwner
	if err := json.Unmarshal(content, &owner); err != nil || owner.PID <= 0 {
		return compileLockOwner{}, false
	}
	return owner, true
}

func formatCompileLockOwner(owner compileLockOwner) string {
	host := strings.TrimSpace(owner.Hostname)
	if host == "" {
		host = "unknown host"
	}
	description := fmt.Sprintf("PID %d on %s", owner.PID, host)
	if owner.SourceHash != "" {
		description += ", source " + owner.SourceHash
	}
	if owner.AcquiredAt != "" {
		description += ", acquired " + owner.AcquiredAt
	}
	return description
}
