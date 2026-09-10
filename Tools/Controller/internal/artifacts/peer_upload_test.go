package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPeerUploadRequiresOrderedChunksAndRevalidatesArtifact(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	data := []byte(validIntelHEX)
	digest := sha256.Sum256(data)
	begin, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "peer.hex", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendPeerUpload(PeerUploadChunkRequest{
		TransferID: begin.TransferID, Offset: 1, Data: data[:8],
	}); err == nil || !strings.Contains(err.Error(), "expected 0") {
		t.Fatalf("out-of-order chunk err=%v", err)
	}
	split := len(data) / 2
	first, err := service.AppendPeerUpload(PeerUploadChunkRequest{
		TransferID: begin.TransferID, Data: data[:split],
	})
	if err != nil || first.NextOffset != int64(split) {
		t.Fatalf("first chunk=%#v err=%v", first, err)
	}
	if _, err := service.AppendPeerUpload(PeerUploadChunkRequest{
		TransferID: begin.TransferID, Offset: first.NextOffset, Data: data[split:],
	}); err != nil {
		t.Fatal(err)
	}
	result, err := service.FinishPeerUpload(PeerUploadFinishRequest{TransferID: begin.TransferID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact == nil || result.Artifact.SHA256 != hex.EncodeToString(digest[:]) || result.Artifact.Source != "authenticated-peer" {
		t.Fatalf("artifact=%#v", result.Artifact)
	}
	entries, err := os.ReadDir(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".peer-artifact-") {
			t.Fatalf("peer temporary file survived commit: %s", entry.Name())
		}
	}
}

func TestPeerUploadAbortRemovesPartialFile(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	digest := sha256.Sum256([]byte("expected"))
	begin, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "peer.hex", SHA256: hex.EncodeToString(digest[:]), Bytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AbortPeerUpload(PeerUploadFinishRequest{TransferID: begin.TransferID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendPeerUpload(PeerUploadChunkRequest{TransferID: begin.TransferID, Data: []byte("x")}); !os.IsNotExist(err) {
		t.Fatalf("append after abort err=%v", err)
	}
}

func TestPeerUploadExpiryIsEnforcedWithoutAnotherBegin(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	digest := sha256.Sum256([]byte("expected"))
	begin, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "peer.hex", SHA256: hex.EncodeToString(digest[:]), Bytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	path := service.peerUploads[begin.TransferID].path
	service.peerUploads[begin.TransferID].updated = time.Now().Add(-peerUploadTTL - time.Second)
	service.mu.Unlock()
	if _, err := service.AppendPeerUpload(PeerUploadChunkRequest{
		TransferID: begin.TransferID, Data: []byte("x"),
	}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired append err=%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired staging file survived: %v", err)
	}
}

func TestNewServiceRemovesCrashOrphanedPeerUpload(t *testing.T) {
	store := newTestStore(t)
	orphan := filepath.Join(store.Root(), ".peer-artifact-crash.upload")
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("crash orphan survived service startup: %v", err)
	}
}

func TestPeerUploadReservationsBoundDeclaredDiskUse(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	digest := strings.Repeat("a", 64)
	for index := 0; index < 2; index++ {
		if _, err := service.BeginPeerUpload(PeerUploadBeginRequest{
			Kind: KindHostExecutable, Name: "controller", SHA256: digest,
			Bytes: maxBytes(KindHostExecutable),
		}); err != nil {
			t.Fatalf("reservation %d: %v", index, err)
		}
	}
	if _, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "third.hex", SHA256: digest, Bytes: 1,
	}); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("over-capacity begin err=%v", err)
	}
}

func TestPeerUploadCapacityIsReservedBeforeTempFileAllocation(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	entered := make(chan string, 64)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	service.peerUploadCreateTemp = func(directory, pattern string) (*os.File, error) {
		file, createErr := os.CreateTemp(directory, pattern)
		if createErr != nil {
			return nil, createErr
		}
		entered <- file.Name()
		<-release
		return file, nil
	}

	type beginResult struct {
		result PeerUploadBeginResult
		err    error
	}
	const attempts = 64
	results := make(chan beginResult, attempts)
	for index := 0; index < attempts; index++ {
		go func() {
			result, beginErr := service.BeginPeerUpload(PeerUploadBeginRequest{
				Kind: KindFirmware, Name: "bounded.hex", SHA256: strings.Repeat("a", 64), Bytes: 1,
			})
			results <- beginResult{result: result, err: beginErr}
		}()
	}

	for index := 0; index < peerUploadMaximumTransfers; index++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d reservations reached temp-file allocation", index)
		}
	}
	rejected := 0
	for rejected < attempts-peerUploadMaximumTransfers {
		select {
		case result := <-results:
			if result.err == nil || !strings.Contains(result.err.Error(), "capacity") {
				t.Fatalf("unreserved begin result=%#v err=%v", result.result, result.err)
			}
			rejected++
		case path := <-entered:
			t.Fatalf("capacity check allocated an extra staging file %s", path)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d excess begins were rejected", rejected, attempts-peerUploadMaximumTransfers)
		}
	}
	paths, err := filepath.Glob(filepath.Join(store.Root(), ".peer-artifact-*.upload"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != peerUploadMaximumTransfers {
		t.Fatalf("allocated staging files=%d; want %d", len(paths), peerUploadMaximumTransfers)
	}

	unblock()
	accepted := 0
	for accepted < peerUploadMaximumTransfers {
		select {
		case result := <-results:
			if result.err != nil || result.result.TransferID == "" {
				t.Fatalf("reserved begin result=%#v err=%v", result.result, result.err)
			}
			accepted++
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d reserved begins completed", accepted)
		}
	}
}

func TestPeerUploadLifecycleRejectsWorkAfterServiceClose(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	digest := strings.Repeat("a", 64)
	if _, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "closed.hex", SHA256: digest, Bytes: 1,
	}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("begin after close err=%v", err)
	}
	entries, err := os.ReadDir(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".peer-artifact-") {
			t.Fatalf("begin after close leaked %s", entry.Name())
		}
	}
}

func TestPeerUploadCleanupDoesNotInterruptFinishingTransfer(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	digest := strings.Repeat("a", 64)
	begin, err := service.BeginPeerUpload(PeerUploadBeginRequest{
		Kind: KindFirmware, Name: "finishing.hex", SHA256: digest, Bytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	upload := service.peerUploads[begin.TransferID]
	upload.finishing = true
	upload.updated = time.Now().Add(-peerUploadTTL - time.Second)
	path := upload.path
	service.mu.Unlock()
	service.cleanupExpiredPeerUploads(time.Now())
	service.mu.RLock()
	retained := service.peerUploads[begin.TransferID] == upload
	service.mu.RUnlock()
	if !retained {
		t.Fatal("cleanup removed a transfer while final verification owned it")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cleanup closed finishing transfer: %v", err)
	}
	service.mu.Lock()
	upload.finishing = false
	service.mu.Unlock()
}

func TestPeerUploadFinishCloseRaceLeavesNoPartialFiles(t *testing.T) {
	for iteration := 0; iteration < 40; iteration++ {
		store := newTestStore(t)
		service, err := NewService(Options{Store: store})
		if err != nil {
			t.Fatal(err)
		}
		data := []byte(validIntelHEX)
		digest := sha256.Sum256(data)
		begin, err := service.BeginPeerUpload(PeerUploadBeginRequest{
			Kind: KindFirmware, Name: "race.hex", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.AppendPeerUpload(PeerUploadChunkRequest{
			TransferID: begin.TransferID, Data: bytes.Clone(data),
		}); err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			close(started)
			_, err := service.FinishPeerUpload(PeerUploadFinishRequest{TransferID: begin.TransferID})
			finished <- err
		}()
		<-started
		service.Close()
		<-finished // A close win may reject; a finish win must be joined before Close returns.
		entries, err := os.ReadDir(store.Root())
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".peer-artifact-") {
				t.Fatalf("iteration %d leaked %s", iteration, entry.Name())
			}
		}
	}
}
