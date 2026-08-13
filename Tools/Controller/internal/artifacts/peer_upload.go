package artifacts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	PeerUploadChunkBytes       = 384 << 10
	peerUploadTTL              = 15 * time.Minute
	peerUploadCleanupInterval  = time.Minute
	peerUploadMaximumTransfers = 8
	peerUploadMaximumReserved  = 512 << 20
)

type peerUpload struct {
	file      *os.File
	path      string
	options   PutOptions
	received  int64
	updated   time.Time
	finishing bool
}

func (service *Service) BeginPeerUpload(request PeerUploadBeginRequest) (PeerUploadBeginResult, error) {
	if service == nil || service.store == nil {
		return PeerUploadBeginResult{}, errors.New("artifact service is unavailable")
	}
	if !ValidKind(request.Kind) {
		return PeerUploadBeginResult{}, fmt.Errorf("unsupported artifact kind %q", request.Kind)
	}
	if request.Bytes < 1 || request.Bytes > maxBytes(request.Kind) {
		return PeerUploadBeginResult{}, fmt.Errorf("%s peer upload size must be 1..%d bytes", request.Kind, maxBytes(request.Kind))
	}
	digest, err := normalizeSHA256(request.SHA256)
	if err != nil {
		return PeerUploadBeginResult{}, err
	}
	if _, err := safeArtifactName(request.Name, request.Kind); err != nil {
		return PeerUploadBeginResult{}, err
	}
	if _, err := validateDescriptorMetadata(request.Metadata); err != nil {
		return PeerUploadBeginResult{}, err
	}
	now := time.Now()
	service.cleanupExpiredPeerUploads(now)
	if err := service.reservePeerUpload(request.Bytes); err != nil {
		return PeerUploadBeginResult{}, err
	}
	reservationActive := true
	defer func() {
		if reservationActive {
			service.releasePeerUploadReservation(request.Bytes)
		}
	}()
	createTemp := service.peerUploadCreateTemp
	if createTemp == nil {
		createTemp = os.CreateTemp
	}
	file, err := createTemp(service.store.Root(), ".peer-artifact-*.upload")
	if err != nil {
		return PeerUploadBeginResult{}, fmt.Errorf("create peer artifact staging file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return PeerUploadBeginResult{}, err
	}
	id := newOperationID()
	upload := &peerUpload{file: file, path: file.Name(), updated: now, options: PutOptions{
		Kind: request.Kind, Name: request.Name, Source: "authenticated-peer",
		ExpectedSHA256: digest, ExpectedBytes: request.Bytes,
		BuildHash: request.BuildHash, BuildTimestamp: request.BuildTimestamp,
		PackedTimestamp: request.PackedTimestamp, Platform: request.Platform,
		Metadata: request.Metadata,
	}}
	service.mu.Lock()
	if service.peerUploadsClosed {
		service.mu.Unlock()
		_ = file.Close()
		_ = os.Remove(file.Name())
		return PeerUploadBeginResult{}, errors.New("artifact service is closed")
	}
	service.peerUploads[id] = upload
	service.peerUploadPending--
	service.peerUploadPendingBytes -= request.Bytes
	reservationActive = false
	service.mu.Unlock()
	service.peerUploadOps.Done()
	return PeerUploadBeginResult{TransferID: id, ChunkBytes: PeerUploadChunkBytes}, nil
}

// reservePeerUpload accounts for a transfer before it allocates a file or file
// descriptor. Close flips peerUploadsClosed under the same lock before waiting
// for peerUploadOps, so a reserved Begin either commits before Close or rolls
// its allocation back while Close waits.
func (service *Service) reservePeerUpload(bytes int64) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.peerUploadsClosed {
		return errors.New("artifact service is closed")
	}
	reserved := service.peerUploadPendingBytes
	for _, existing := range service.peerUploads {
		reserved += existing.options.ExpectedBytes
	}
	if len(service.peerUploads)+service.peerUploadPending >= peerUploadMaximumTransfers ||
		reserved+bytes > peerUploadMaximumReserved {
		return errors.New("peer artifact transfer capacity is exhausted")
	}
	service.peerUploadPending++
	service.peerUploadPendingBytes += bytes
	service.peerUploadOps.Add(1)
	return nil
}

func (service *Service) releasePeerUploadReservation(bytes int64) {
	service.mu.Lock()
	service.peerUploadPending--
	service.peerUploadPendingBytes -= bytes
	service.mu.Unlock()
	service.peerUploadOps.Done()
}

func (service *Service) AppendPeerUpload(request PeerUploadChunkRequest) (PeerUploadChunkResult, error) {
	id := strings.TrimSpace(request.TransferID)
	if id == "" {
		return PeerUploadChunkResult{}, errors.New("transfer_id is required")
	}
	if len(request.Data) == 0 || len(request.Data) > PeerUploadChunkBytes {
		return PeerUploadChunkResult{}, fmt.Errorf("peer upload chunk must be 1..%d bytes", PeerUploadChunkBytes)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.peerUploadsClosed {
		return PeerUploadChunkResult{}, errors.New("artifact service is closed")
	}
	upload := service.peerUploads[id]
	if upload == nil {
		return PeerUploadChunkResult{}, os.ErrNotExist
	}
	if peerUploadExpired(upload, time.Now()) {
		delete(service.peerUploads, id)
		_ = upload.file.Close()
		_ = os.Remove(upload.path)
		return PeerUploadChunkResult{}, errors.New("peer artifact transfer expired")
	}
	if upload.finishing {
		return PeerUploadChunkResult{}, errors.New("peer artifact transfer is finishing")
	}
	if request.Offset != upload.received {
		return PeerUploadChunkResult{}, fmt.Errorf("peer upload offset is %d; expected %d", request.Offset, upload.received)
	}
	if upload.received+int64(len(request.Data)) > upload.options.ExpectedBytes {
		return PeerUploadChunkResult{}, errors.New("peer upload exceeds declared size")
	}
	written, err := upload.file.WriteAt(request.Data, request.Offset)
	if err != nil {
		return PeerUploadChunkResult{}, fmt.Errorf("write peer upload: %w", err)
	}
	if written != len(request.Data) {
		return PeerUploadChunkResult{}, io.ErrShortWrite
	}
	upload.received += int64(written)
	upload.updated = time.Now()
	return PeerUploadChunkResult{TransferID: id, NextOffset: upload.received, BytesTotal: upload.options.ExpectedBytes}, nil
}

func (service *Service) FinishPeerUpload(request PeerUploadFinishRequest) (OperationResult, error) {
	id := strings.TrimSpace(request.TransferID)
	service.mu.Lock()
	upload := service.peerUploads[id]
	if upload == nil {
		service.mu.Unlock()
		return OperationResult{}, os.ErrNotExist
	}
	if service.peerUploadsClosed {
		service.mu.Unlock()
		return OperationResult{}, errors.New("artifact service is closed")
	}
	if upload.finishing {
		service.mu.Unlock()
		return OperationResult{}, errors.New("peer artifact transfer is already finishing")
	}
	if peerUploadExpired(upload, time.Now()) {
		delete(service.peerUploads, id)
		service.mu.Unlock()
		_ = upload.file.Close()
		_ = os.Remove(upload.path)
		return OperationResult{}, errors.New("peer artifact transfer expired")
	}
	if upload.received != upload.options.ExpectedBytes {
		delete(service.peerUploads, id)
		service.mu.Unlock()
		_ = upload.file.Close()
		_ = os.Remove(upload.path)
		return OperationResult{}, fmt.Errorf("peer upload received %d of %d bytes", upload.received, upload.options.ExpectedBytes)
	}
	upload.finishing = true
	service.peerUploadOps.Add(1)
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		if service.peerUploads[id] == upload {
			delete(service.peerUploads, id)
		}
		service.mu.Unlock()
		_ = upload.file.Close()
		_ = os.Remove(upload.path)
		service.peerUploadOps.Done()
	}()
	if err := upload.file.Sync(); err != nil {
		return OperationResult{}, err
	}
	if err := upload.file.Close(); err != nil {
		return OperationResult{}, err
	}
	file, err := os.Open(upload.path)
	if err != nil {
		return OperationResult{}, err
	}
	defer file.Close()
	return service.UploadOperation(file, upload.options)
}

func (service *Service) AbortPeerUpload(request PeerUploadFinishRequest) error {
	id := strings.TrimSpace(request.TransferID)
	service.mu.Lock()
	upload := service.peerUploads[id]
	if upload == nil {
		service.mu.Unlock()
		return nil
	}
	if service.peerUploadsClosed {
		service.mu.Unlock()
		return errors.New("artifact service is closed")
	}
	if upload.finishing {
		service.mu.Unlock()
		return errors.New("peer artifact transfer is finishing")
	}
	delete(service.peerUploads, id)
	service.mu.Unlock()
	return errors.Join(upload.file.Close(), os.Remove(upload.path))
}

func peerUploadExpired(upload *peerUpload, now time.Time) bool {
	return upload == nil || now.Sub(upload.updated) > peerUploadTTL
}

func (service *Service) cleanupExpiredPeerUploads(now time.Time) {
	service.mu.Lock()
	expired := make([]*peerUpload, 0)
	for transferID, upload := range service.peerUploads {
		if upload.finishing || !peerUploadExpired(upload, now) {
			continue
		}
		delete(service.peerUploads, transferID)
		expired = append(expired, upload)
	}
	service.mu.Unlock()
	for _, upload := range expired {
		_ = upload.file.Close()
		_ = os.Remove(upload.path)
	}
}

func (service *Service) peerUploadCleanupLoop() {
	defer service.peerUploadWait.Done()
	ticker := time.NewTicker(peerUploadCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-service.ctx.Done():
			return
		case now := <-ticker.C:
			service.cleanupExpiredPeerUploads(now)
		}
	}
}

func (service *Service) removeOrphanedPeerUploads() error {
	paths, err := filepath.Glob(filepath.Join(service.store.Root(), ".peer-artifact-*.upload"))
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove %s: %w", filepath.Base(path), err))
		}
	}
	return cleanupErr
}
