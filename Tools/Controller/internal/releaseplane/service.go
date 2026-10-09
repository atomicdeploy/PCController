package releaseplane

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
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

const maxStageOperations = 64

// EventSink publishes release-discovery progress into the host event stream.
type EventSink func(kind, text string, metadata map[string]string)

// Service discovers and stages verified release artifacts without programming
// connected hardware.
type Service struct {
	client     *Client
	artifacts  *artifacts.Service
	events     EventSink
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	wg         sync.WaitGroup
	closed     bool
	operations map[string]StageStatus
	order      []string
	idempotent map[string]string
	signatures map[string]string
}

// NewService constructs a release staging service around the shared artifact store.
func NewService(client *Client, artifactService *artifacts.Service, events EventSink) (*Service, error) {
	if artifactService == nil {
		return nil, errors.New("release discovery requires the artifact service")
	}
	if client == nil {
		client = NewClient(nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		client: client, artifacts: artifactService, events: events, ctx: ctx, cancel: cancel,
		operations: make(map[string]StageStatus), idempotent: make(map[string]string), signatures: make(map[string]string),
	}, nil
}

// Close cancels staging work and waits for active operations to finish.
func (service *Service) Close() error {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return nil
	}
	service.closed = true
	service.cancel()
	service.mu.Unlock()
	service.wg.Wait()
	return nil
}

// StartStage validates and asynchronously imports one immutable candidate.
func (service *Service) StartStage(request StageRequest) (StageResult, error) {
	if err := validateCandidate(request.Candidate); err != nil {
		return StageResult{}, err
	}
	if strings.TrimSpace(request.Candidate.ID) == "" {
		request.Candidate.ID = candidateID(request.Candidate)
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if len(request.IdempotencyKey) > 128 {
		return StageResult{}, errors.New("idempotency_key exceeds 128 characters")
	}
	fingerprint, err := stageFingerprint(request.Candidate)
	if err != nil {
		return StageResult{}, err
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return StageResult{}, errors.New("release discovery service is closed")
	}
	if request.IdempotencyKey != "" {
		if id := service.idempotent[request.IdempotencyKey]; id != "" {
			if service.signatures[request.IdempotencyKey] != fingerprint {
				service.mu.Unlock()
				return StageResult{}, errors.New("idempotency_key was already used for a different staging request")
			}
			status := service.operations[id]
			service.mu.Unlock()
			return StageResult{Operation: status}, nil
		}
	}
	now := time.Now().UTC()
	status := StageStatus{
		ID: newStageID(), CandidateID: request.Candidate.ID, Kind: request.Candidate.Kind,
		State: "queued", Detail: "queued verified candidate download",
		StartedAt: now, UpdatedAt: now,
	}
	if request.Candidate.Archive {
		status.BytesTotal = request.Candidate.ArchiveBytes
	} else {
		status.BytesTotal = request.Candidate.Bytes
	}
	if len(service.order) >= maxStageOperations {
		oldest := service.order[0]
		delete(service.operations, oldest)
		service.order = append([]string(nil), service.order[1:]...)
		for key, value := range service.idempotent {
			if value == oldest {
				delete(service.idempotent, key)
				delete(service.signatures, key)
			}
		}
	}
	service.operations[status.ID] = status
	service.order = append(service.order, status.ID)
	if request.IdempotencyKey != "" {
		service.idempotent[request.IdempotencyKey] = status.ID
		service.signatures[request.IdempotencyKey] = fingerprint
	}
	service.wg.Add(1)
	service.mu.Unlock()
	service.publish(status)
	go func() {
		defer service.wg.Done()
		service.runStage(status.ID, request)
	}()
	return StageResult{Operation: status}, nil
}

func stageFingerprint(candidate Candidate) (string, error) {
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return "", fmt.Errorf("encode staging identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (service *Service) runStage(id string, request StageRequest) {
	ctx, cancel := context.WithTimeout(service.ctx, 20*time.Minute)
	defer cancel()
	service.update(id, func(status *StageStatus) {
		status.State = "downloading"
		status.Detail = "downloading candidate through configured proxy policy"
	})
	descriptor, err := service.client.stage(ctx, service.artifacts, request, func(done, total int64, detail string) {
		service.update(id, func(status *StageStatus) {
			status.State, status.BytesDone, status.Detail = "downloading", done, detail
			if total > 0 {
				status.BytesTotal = total
				status.ProgressPercent = int(done * 85 / total)
				if status.ProgressPercent > 85 {
					status.ProgressPercent = 85
				}
			}
		})
	})
	if err != nil {
		service.update(id, func(status *StageStatus) {
			status.State, status.Error, status.Detail = "failed", err.Error(), "candidate download or validation failed"
		})
		return
	}
	service.update(id, func(status *StageStatus) {
		status.State, status.ProgressPercent = "completed", 100
		status.Detail, status.Artifact = "candidate verified and staged; hardware was not opened", &descriptor
		if status.BytesTotal > 0 {
			status.BytesDone = status.BytesTotal
		}
	})
}

// Status returns one staging operation, or the latest when ID is empty.
func (service *Service) Status(id string) (StageStatus, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" && len(service.order) > 0 {
		id = service.order[len(service.order)-1]
	}
	status, ok := service.operations[id]
	if !ok {
		return StageStatus{}, os.ErrNotExist
	}
	return status, nil
}

func (service *Service) update(id string, mutate func(*StageStatus)) {
	service.mu.Lock()
	status, ok := service.operations[id]
	if !ok {
		service.mu.Unlock()
		return
	}
	mutate(&status)
	status.UpdatedAt = time.Now().UTC()
	service.operations[id] = status
	service.mu.Unlock()
	service.publish(status)
}

func (service *Service) publish(status StageStatus) {
	if service.events == nil {
		return
	}
	metadata := map[string]string{
		"operation_id": status.ID, "state": status.State,
		"candidate_id":     status.CandidateID,
		"kind":             string(status.Kind),
		"progress_percent": strconv.Itoa(status.ProgressPercent),
		"bytes_done":       strconv.FormatInt(status.BytesDone, 10),
		"bytes_total":      strconv.FormatInt(status.BytesTotal, 10),
	}
	if status.Artifact != nil {
		metadata["sha256"] = status.Artifact.SHA256
	}
	if status.Error != "" {
		metadata["error"] = status.Error
	}
	service.events("artifact.discovery."+status.State, status.Detail, metadata)
}

// DispatchRPC handles release-discovery methods and reports whether the method
// belonged to this service.
func (service *Service) DispatchRPC(ctx context.Context, method string, params json.RawMessage) (any, bool, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "controller.discovery.github.workflow":
		var request GitHubWorkflowRequest
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.client.DiscoverWorkflow(ctx, request)
		return value, true, err
	case "controller.discovery.github.release":
		var request GitHubReleaseRequest
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.client.DiscoverRelease(ctx, request)
		return value, true, err
	case "controller.discovery.manifest":
		var request ManifestRequest
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.client.DiscoverManifest(ctx, request)
		return value, true, err
	case "controller.discovery.local_manifest":
		value, err := service.LocalManifest()
		return value, true, err
	case "controller.discovery.check":
		var request CheckRequest
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := CheckForUpdate(request)
		return value, true, err
	case "controller.discovery.stage":
		var request StageRequest
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartStage(request)
		return value, true, err
	case "controller.discovery.status":
		var request struct {
			ID string `json:"id,omitempty"`
		}
		if err := decodeRPC(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.Status(request.ID)
		return value, true, err
	default:
		return nil, false, nil
	}
}

func decodeRPC(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("params contain trailing JSON")
		}
		return err
	}
	return nil
}

func newStageID() string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("discovery-%d", time.Now().UnixNano())
	}
	return "discovery-" + hex.EncodeToString(value)
}
