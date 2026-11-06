package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Staging a deferred replacement is not terminal success. The replacement's
// original service operation remains pending across restart until its private
// transaction coordinator has actually committed (or rolled back).
var ErrHostActivationPending = errors.New("host replacement awaits verified activation")

func pendingHostActivation(status UpdateStatus) bool {
	return status.Kind == "host" && (status.State == "awaiting-restart" || status.State == "awaiting-health" ||
		(status.State == "completed" && status.Stage == "staged"))
}

func hostActivationStatus(status UpdateStatus, current string) UpdateStatus {
	if !pendingHostActivation(status) {
		return status
	}
	next := status
	// Staged completion is not activation, including when the outgoing process
	// already persisted it. Interpret the transaction facts, not a version tag.
	if next.State == "completed" {
		next.State, next.Stage = "awaiting-health", "awaiting-health"
		next.ProgressKnown, next.ProgressPercent = false, 0
		next.Detail = "waiting for verified healthy host replacement"
	}
	digest, err := normalizeSHA256(status.ArtifactSHA256)
	if err != nil {
		return next
	}
	path := filepath.Join(filepath.Dir(current), "."+filepath.Base(current)+".update-"+digest[:12]+".journal.json")
	journal, err := loadSelfUpdateJournal(path)
	if err != nil || journal.CurrentPath != current || journal.ReplacementSHA256 != digest ||
		journal.UpdatedAt.Before(status.StartedAt) || (journal.OperationID != "" && journal.OperationID != status.ID) {
		return next
	}
	switch journal.State {
	case "committed", "committed-recovered":
		if err := verifyFileDigest(current, digest); err != nil {
			return next
		}
		next.State, next.Stage = "completed", "completed"
		next.ProgressKnown, next.ProgressPercent = true, 100
		next.Detail, next.ErrorCode = "healthy host replacement verified", ""
	case "rolled-back", "rolled-back-recovered":
		next.State, next.Stage = "failed", "rolled-back"
		next.ProgressKnown, next.ProgressPercent = false, 0
		next.Detail, next.ErrorCode = "host replacement rolled back: "+journal.Failure, "host_update_rolled_back"
	default:
		if strings.HasSuffix(journal.State, "-failed") {
			next.State, next.Stage = "failed", "activation-failed"
			next.ProgressKnown, next.ProgressPercent = false, 0
			next.Detail, next.ErrorCode = "host replacement failed: "+journal.Failure, "host_activation_failed"
		}
	}
	return next
}

func (service *Service) reconcileHostActivation(status UpdateStatus) UpdateStatus {
	if !pendingHostActivation(status) {
		return status
	}
	// Serialize only rare activation reconciliation, not hardware/runtime work.
	// In particular, a final executable hash must not hold the service state lock.
	service.hostActivationMu.Lock()
	service.mu.RLock()
	status = service.operations[status.ID]
	service.mu.RUnlock()
	if !pendingHostActivation(status) {
		service.hostActivationMu.Unlock()
		return status
	}
	current, err := os.Executable()
	if err != nil {
		service.hostActivationMu.Unlock()
		return status
	}
	next := hostActivationStatus(status, current)
	if next.State == status.State && next.Stage == status.Stage {
		service.hostActivationMu.Unlock()
		return status
	}
	next.UpdatedAt, next.StageStartedAt = time.Now().UTC(), time.Now().UTC()
	service.mu.Lock()
	if err := service.persistTerminalOperationLocked(next.ID, next); err != nil {
		next.State, next.ProgressKnown, next.ProgressPercent = "failed", false, 0
		next.ErrorCode, next.Detail = "operation_journal_failed", "could not persist host activation outcome: "+err.Error()
	}
	service.operations[next.ID] = next
	service.mu.Unlock()
	service.hostActivationMu.Unlock()
	service.publishStatus(next)
	return next
}
