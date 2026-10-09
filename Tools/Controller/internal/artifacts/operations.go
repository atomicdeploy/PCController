package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const operationJournalSchema = 1

type operationJournal struct {
	Schema      int          `json:"schema"`
	Status      UpdateStatus `json:"status"`
	Scope       string       `json:"scope,omitempty"`
	Fingerprint string       `json:"fingerprint,omitempty"`
}

type idempotencyRecord struct {
	OperationID string
	Fingerprint string
}

func (service *Service) reserveOperation(
	kind, digest, detail, idempotencyKey, fingerprint string,
	method ProgrammingMethod,
) (UpdateStatus, bool, error) {
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return UpdateStatus{}, false, err
	}
	scope := kind
	lookup := ""
	if key != "" {
		lookup = scope + ":" + key
	}
	now := time.Now().UTC()
	service.mu.Lock()
	if lookup != "" {
		if prior, ok := service.idempotency[lookup]; ok {
			status, found := service.operations[prior.OperationID]
			if !found {
				service.mu.Unlock()
				return UpdateStatus{}, false, errors.New("idempotency journal references a missing operation")
			}
			if prior.Fingerprint != fingerprint {
				service.mu.Unlock()
				return UpdateStatus{}, false, errors.New("idempotency key was already used for a different request")
			}
			service.mu.Unlock()
			return status, true, nil
		}
	}
	status := UpdateStatus{
		ID: newOperationID(), Kind: kind, State: "queued", ProgressPercent: 0,
		StartedAt: now, UpdatedAt: now, ArtifactSHA256: strings.ToLower(strings.TrimSpace(digest)),
		Detail: detail, IdempotencyKey: key, ProgrammingMethod: method,
		BootloaderOutcome: BootloaderNotAttempted,
	}
	if len(service.order) >= maxOperations {
		oldest := service.order[0]
		delete(service.operations, oldest)
		service.order = append([]string(nil), service.order[1:]...)
		for candidate, record := range service.idempotency {
			if record.OperationID == oldest {
				delete(service.idempotency, candidate)
			}
		}
		_ = os.Remove(service.operationJournalPath(oldest))
	}
	service.operations[status.ID] = status
	service.order = append(service.order, status.ID)
	if lookup != "" {
		service.idempotency[lookup] = idempotencyRecord{OperationID: status.ID, Fingerprint: fingerprint}
	}
	service.operationMeta[status.ID] = operationJournal{
		Schema: operationJournalSchema, Status: status, Scope: scope, Fingerprint: fingerprint,
	}
	service.mu.Unlock()
	if err := service.persistOperation(status.ID); err != nil {
		return UpdateStatus{}, false, fmt.Errorf("persist queued operation: %w", err)
	}
	service.publishStatus(status)
	return status, false, nil
}

func (service *Service) loadOperationJournals() error {
	directory := service.operationJournalDirectory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var values []operationJournal
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			return readErr
		}
		var journal operationJournal
		if decodeErr := strictJSON(content, &journal); decodeErr != nil {
			return fmt.Errorf("decode operation journal %q: %w", entry.Name(), decodeErr)
		}
		if journal.Schema != operationJournalSchema || journal.Status.ID == "" ||
			entry.Name() != journal.Status.ID+".json" {
			return fmt.Errorf("operation journal %q has an invalid identity", entry.Name())
		}
		if !terminalOperationState(journal.Status.State) {
			journal.Status.State = "failed"
			journal.Status.ErrorCode = "host_restarted"
			journal.Status.Detail = "operation was interrupted by host restart; no hardware write was replayed"
			journal.Status.UpdatedAt = time.Now().UTC()
		}
		values = append(values, journal)
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].Status.StartedAt.Before(values[right].Status.StartedAt)
	})
	if len(values) > maxOperations {
		values = values[len(values)-maxOperations:]
	}
	for _, journal := range values {
		status := journal.Status
		service.operations[status.ID] = status
		service.order = append(service.order, status.ID)
		service.operationMeta[status.ID] = journal
		if status.IdempotencyKey != "" {
			lookup := journal.Scope + ":" + status.IdempotencyKey
			service.idempotency[lookup] = idempotencyRecord{
				OperationID: status.ID, Fingerprint: journal.Fingerprint,
			}
		}
		if err := service.persistOperation(status.ID); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) persistOperation(id string) error {
	service.mu.RLock()
	status, ok := service.operations[id]
	journal := service.operationMeta[id]
	service.mu.RUnlock()
	if !ok {
		return os.ErrNotExist
	}
	journal.Schema = operationJournalSchema
	journal.Status = status
	return writeJSONAtomic(service.operationJournalPath(id), journal)
}

func (service *Service) operationJournalDirectory() string {
	return filepath.Join(service.store.Root(), "operations")
}

func (service *Service) operationJournalPath(id string) string {
	return filepath.Join(service.operationJournalDirectory(), id+".json")
}

func terminalOperationState(value string) bool {
	switch value {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func requestFingerprint(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 128 {
		return "", errors.New("idempotency key exceeds 128 characters")
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._:-", character) {
			continue
		}
		return "", errors.New("idempotency key may contain only letters, digits, dot, underscore, colon, and dash")
	}
	return value, nil
}
