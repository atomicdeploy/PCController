package ipcjson

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

// ErrorCodeOutcomeUncertain means a caller must retry with the same idempotency key.
const ErrorCodeOutcomeUncertain = -32004

const (
	peerHostVerificationTimeout = 60 * time.Second
	peerHostVerificationPoll    = 500 * time.Millisecond
	peerHostProbeTimeout        = 3 * time.Second
)

var peerHostStableVerificationWindow = 12 * time.Second

type peerHostUpdateRequest struct {
	Peer           string `json:"peer"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	Authorized     bool   `json:"authorized"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type peerHostUpdateResult struct {
	Peer             string                 `json:"peer"`
	Artifact         artifacts.Descriptor   `json:"artifact"`
	Operation        artifacts.UpdateStatus `json:"operation"`
	Stage            string                 `json:"stage"`
	TerminalVerified bool                   `json:"terminal_verified"`
}

func (service *Service) updatePeerHost(ctx context.Context, request peerHostUpdateRequest) (result peerHostUpdateResult, err error) {
	peer := strings.TrimSpace(request.Peer)
	if peer == "" {
		return result, errors.New("peer is required")
	}
	if !request.Authorized {
		return result, errors.New("peer host update requires authorized=true")
	}
	idempotencyKey, err := peerHostIdempotencyKey(request.IdempotencyKey)
	if err != nil {
		return result, err
	}
	if service.Artifacts == nil || service.BridgeCall == nil {
		return result, errors.New("artifact service and host bridge manager are required")
	}
	descriptor, file, err := service.Artifacts.Open(artifacts.KindHostExecutable, request.ArtifactSHA256)
	if err != nil {
		return result, fmt.Errorf("open verified host artifact: %w", err)
	}
	defer file.Close()
	operationID := peerHostOperationID(peer, descriptor.SHA256, idempotencyKey)
	service.emitPeerUpdate(operationID, peer, idempotencyKey, descriptor, "queued", 0, "peer host update queued")
	defer func() {
		if err != nil {
			state := "failed"
			detail := err.Error()
			metadata := map[string]string(nil)
			var rpcError *RPCError
			if errors.As(err, &rpcError) && rpcError.Code == ErrorCodeOutcomeUncertain {
				state = "outcome-uncertain"
				detail = rpcError.Message
				metadata = map[string]string{
					"retry_same_idempotency_key": "true",
					"terminal_verified":          "false",
				}
			}
			service.emitPeerUpdate(operationID, peer, idempotencyKey, descriptor, state, 0, detail, metadata)
		}
	}()

	var begin artifacts.PeerUploadBeginResult
	err = service.callPeer(ctx, peer, "controller.artifact.upload.begin", artifacts.PeerUploadBeginRequest{
		Kind: descriptor.Kind, Name: descriptor.Name, SHA256: descriptor.SHA256,
		Bytes: descriptor.Bytes, BuildHash: descriptor.BuildHash,
		BuildTimestamp: descriptor.BuildTimestamp, PackedTimestamp: descriptor.PackedTimestamp,
		Platform: descriptor.Platform, Metadata: descriptor.Metadata,
	}, &begin)
	if err != nil {
		return result, fmt.Errorf("begin peer artifact transfer: %w", err)
	}
	begin.TransferID = strings.TrimSpace(begin.TransferID)
	if begin.TransferID == "" || len(begin.TransferID) > 128 || begin.NextOffset != 0 {
		return result, errors.New("peer returned an invalid artifact transfer identity")
	}
	abort := true
	defer func() {
		if abort {
			abortContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = service.callPeer(abortContext, peer, "controller.artifact.upload.abort", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, nil)
		}
	}()
	chunkBytes := begin.ChunkBytes
	if chunkBytes < 1 || chunkBytes > artifacts.PeerUploadChunkBytes {
		return result, fmt.Errorf("peer selected invalid chunk size %d", chunkBytes)
	}
	buffer := make([]byte, chunkBytes)
	offset, lastPercent := int64(0), -1
	for offset < descriptor.Bytes {
		want := int64(len(buffer))
		if remaining := descriptor.Bytes - offset; remaining < want {
			want = remaining
		}
		count, readErr := io.ReadFull(file, buffer[:want])
		if readErr != nil {
			return result, fmt.Errorf("read verified host artifact at %d: %w", offset, readErr)
		}
		var chunk artifacts.PeerUploadChunkResult
		err = service.callPeer(ctx, peer, "controller.artifact.upload.chunk", artifacts.PeerUploadChunkRequest{
			TransferID: begin.TransferID, Offset: offset, Data: buffer[:count],
		}, &chunk)
		if err != nil {
			return result, fmt.Errorf("transfer host artifact at %d: %w", offset, err)
		}
		expectedOffset := offset + int64(count)
		if chunk.TransferID != begin.TransferID || chunk.NextOffset != expectedOffset ||
			chunk.BytesTotal != descriptor.Bytes {
			return result, fmt.Errorf(
				"peer artifact acknowledgement mismatch: transfer=%q offset=%d total=%d; expected transfer=%q offset=%d total=%d",
				chunk.TransferID, chunk.NextOffset, chunk.BytesTotal,
				begin.TransferID, expectedOffset, descriptor.Bytes,
			)
		}
		offset = expectedOffset
		percent := int(offset * 80 / descriptor.Bytes)
		if percent/10 != lastPercent/10 {
			lastPercent = percent
			service.emitPeerUpdate(operationID, peer, idempotencyKey, descriptor, "transferring", percent, "transferring verified host artifact to peer")
		}
	}
	var uploaded artifacts.OperationResult
	if err = service.callPeer(ctx, peer, "controller.artifact.upload.finish", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, &uploaded); err != nil {
		return result, fmt.Errorf("verify peer artifact: %w", err)
	}
	abort = false
	if err = validatePeerUploadResult(uploaded, descriptor); err != nil {
		return result, err
	}
	service.emitPeerUpdate(operationID, peer, idempotencyKey, descriptor, "artifact-verified", 85, "peer verified the transferred host artifact")
	var update artifacts.OperationResult
	if err = service.callPeer(ctx, peer, "controller.update.host", artifacts.UpdateRequest{
		ArtifactSHA256: descriptor.SHA256, Authorized: true,
		IdempotencyKey: idempotencyKey,
	}, &update); err != nil {
		return result, fmt.Errorf("queue peer host replacement: %w", err)
	}
	stage, stageErr := peerHostAcceptance(update.Operation, descriptor.SHA256, idempotencyKey)
	if stageErr != nil {
		return result, stageErr
	}
	service.emitPeerUpdate(
		operationID, peer, idempotencyKey, descriptor, stage, 90,
		"peer coordinator accepted remote staging; waiting for restart and active SHA acknowledgement",
		map[string]string{
			"remote_operation_id": update.Operation.ID,
			"terminal_verified":   "false",
		},
	)
	if err = service.verifyPeerHostReplacement(ctx, operationID, peer, idempotencyKey, descriptor, update.Operation.ID); err != nil {
		return result, err
	}
	update.Operation.State = "completed"
	update.Operation.ProgressPercent = 100
	update.Operation.Detail = "peer restarted and acknowledged the exact active host executable SHA-256"
	update.Operation.UpdatedAt = time.Now().UTC()
	result = peerHostUpdateResult{
		Peer: peer, Artifact: *uploaded.Artifact, Operation: update.Operation,
		Stage: "completed", TerminalVerified: true,
	}
	return result, nil
}

func (service *Service) verifyPeerHostReplacement(
	ctx context.Context,
	operationID, peer, idempotencyKey string,
	descriptor artifacts.Descriptor,
	remoteOperationID string,
) error {
	verifyContext, cancel := context.WithTimeout(ctx, peerHostVerificationTimeout)
	defer cancel()

	reconnecting := false
	var candidateObservedAt time.Time
	healthChecking := false
	for {
		probeContext, probeCancel := context.WithTimeout(verifyContext, peerHostProbeTimeout)
		var manifest artifacts.Manifest
		probeErr := service.callPeer(probeContext, peer, "controller.artifact.manifest", struct{}{}, &manifest)
		probeCancel()
		if probeErr == nil {
			if manifest.Current.Host != nil && strings.EqualFold(manifest.Current.Host.SHA256, descriptor.SHA256) {
				if candidateObservedAt.IsZero() {
					candidateObservedAt = time.Now()
				}
				if !healthChecking && peerHostStableVerificationWindow > 0 {
					healthChecking = true
					service.emitPeerUpdate(
						operationID, peer, idempotencyKey, descriptor, "health-checking", 98,
						"peer candidate is running; waiting for the self-update health commit",
						map[string]string{
							"remote_operation_id": remoteOperationID,
							"active_sha256":       manifest.Current.Host.SHA256,
							"terminal_verified":   "false",
						},
					)
				}
				if peerHostStableVerificationWindow <= 0 || time.Since(candidateObservedAt) >= peerHostStableVerificationWindow {
					service.emitPeerUpdate(
						operationID, peer, idempotencyKey, descriptor, "completed", 100,
						"peer restarted and acknowledged the exact active host executable SHA-256 after health commit",
						map[string]string{
							"remote_operation_id": remoteOperationID,
							"active_sha256":       manifest.Current.Host.SHA256,
							"terminal_verified":   "true",
						},
					)
					return nil
				}
			} else {
				candidateObservedAt = time.Time{}
				healthChecking = false
			}
		} else {
			candidateObservedAt = time.Time{}
			healthChecking = false
			if !reconnecting {
				reconnecting = true
				service.emitPeerUpdate(
					operationID, peer, idempotencyKey, descriptor, "reconnecting", 95,
					"peer process is restarting; waiting for the bridge and active SHA acknowledgement",
					map[string]string{
						"remote_operation_id": remoteOperationID,
						"terminal_verified":   "false",
					},
				)
			}
		}

		select {
		case <-verifyContext.Done():
			return peerOutcomeUncertain("peer did not acknowledge the requested active host executable SHA-256 after restart")
		case <-time.After(peerHostVerificationPoll):
		}
	}
}

func peerHostIdempotencyKey(provided string) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided == "" {
		return "", errors.New("peer host update requires a caller-generated idempotency_key")
	}
	if len(provided) > 128 {
		return "", errors.New("idempotency key exceeds 128 characters")
	}
	for _, character := range provided {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._:-", character) {
			continue
		}
		return "", errors.New("idempotency key contains an unsupported character")
	}
	return provided, nil
}

func peerHostOperationID(peer, digest, idempotencyKey string) string {
	fingerprint := sha256.Sum256([]byte(
		strings.ToLower(strings.TrimSpace(peer)) + "\x00" +
			strings.ToLower(strings.TrimSpace(digest)) + "\x00" + idempotencyKey,
	))
	return "peer-host-" + hex.EncodeToString(fingerprint[:12])
}

func validatePeerUploadResult(result artifacts.OperationResult, descriptor artifacts.Descriptor) error {
	if result.Artifact == nil || result.Artifact.SHA256 != descriptor.SHA256 ||
		result.Artifact.Kind != artifacts.KindHostExecutable ||
		result.Artifact.Bytes != descriptor.Bytes ||
		!strings.EqualFold(strings.TrimSpace(result.Artifact.Platform), strings.TrimSpace(descriptor.Platform)) {
		return errors.New("peer returned a different artifact identity")
	}
	operation := result.Operation
	if strings.TrimSpace(operation.ID) == "" || operation.Kind != "artifact-upload" ||
		operation.State != "completed" ||
		!strings.EqualFold(operation.ArtifactSHA256, descriptor.SHA256) ||
		operation.BytesDone != descriptor.Bytes || operation.BytesTotal != descriptor.Bytes {
		return errors.New("peer returned an invalid completed artifact-upload operation")
	}
	return nil
}

func peerHostAcceptance(status artifacts.UpdateStatus, digest, idempotencyKey string) (string, error) {
	if strings.TrimSpace(status.ID) == "" || status.Kind != "host" ||
		!strings.EqualFold(status.ArtifactSHA256, digest) ||
		status.IdempotencyKey != idempotencyKey {
		return "", peerOutcomeUncertain("peer returned an invalid host staging operation")
	}
	switch status.State {
	case "queued":
		return "remote-queued", nil
	case "staging", "staged", "completed":
		return "remote-staged", nil
	case "failed", "cancelled":
		return "", fmt.Errorf("peer host staging is %s: %s", status.State, status.Detail)
	default:
		return "", peerOutcomeUncertain(fmt.Sprintf("peer returned unsupported host staging state %q", status.State))
	}
}

func peerOutcomeUncertain(detail string) *RPCError {
	detail = strings.TrimSpace(detail)
	message := "peer outcome uncertain; retry with the same idempotency key"
	if detail != "" {
		message += ": " + detail
	}
	return &RPCError{Code: ErrorCodeOutcomeUncertain, Message: message}
}

func (service *Service) callPeer(ctx context.Context, peer, method string, params, target any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	response, err := service.BridgeCall(ctx, peer, Request{JSONRPC: Version, Method: method, Params: encoded})
	if err != nil {
		return peerOutcomeUncertain(err.Error())
	}
	if response.JSONRPC != Version || (response.Result == nil) == (response.Error == nil) {
		return peerOutcomeUncertain("invalid peer JSON-RPC response envelope")
	}
	if response.Error != nil {
		return response.Error
	}
	if target == nil {
		return nil
	}
	encoded, err = json.Marshal(response.Result)
	if err != nil {
		return peerOutcomeUncertain(err.Error())
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return peerOutcomeUncertain(err.Error())
	}
	return nil
}

func (service *Service) emitPeerUpdate(
	operationID, peer, idempotencyKey string,
	descriptor artifacts.Descriptor,
	state string,
	percent int,
	detail string,
	extra ...map[string]string,
) {
	if service.Client == nil {
		return
	}
	metadata := map[string]string{
		"operation_id": operationID, "peer": peer, "kind": "host",
		"idempotency_key": idempotencyKey,
		"state":           state, "stage": state,
		"progress_known": "true", "progress_percent": strconv.Itoa(percent),
		"sha256": descriptor.SHA256, "bytes_total": strconv.FormatInt(descriptor.Bytes, 10),
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if len(extra) != 0 {
		for key, value := range extra[0] {
			metadata[key] = value
		}
	}
	service.Client.EmitHostActionEvent("peer-update."+state, detail, "bridge", "peer-host-update", metadata)
}
