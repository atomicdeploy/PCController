package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
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
