//go:build windows && !no_native_gui

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestNativeArtifactImportStagesExactBytesWithoutApplyingThem(t *testing.T) {
	data := []byte(":0100000001FE\n:00000001FF\n")
	path := filepath.Join(t.TempDir(), "firmware.hex")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	received := []byte{}
	calls := 0
	p := nativePages{rpc: func(_ context.Context, method string, params, result any) error {
		calls++
		switch method {
		case "controller.artifact.upload.begin":
			*result.(*artifacts.PeerUploadBeginResult) = artifacts.PeerUploadBeginResult{TransferID: "owned", ChunkBytes: 7}
		case "controller.artifact.upload.chunk":
			request := params.(artifacts.PeerUploadChunkRequest)
			if request.Offset != int64(len(received)) {
				t.Fatal("offset drift")
			}
			received = append(received, request.Data...)
			*result.(*artifacts.PeerUploadChunkResult) = artifacts.PeerUploadChunkResult{TransferID: "owned", NextOffset: int64(len(received)), BytesTotal: int64(len(data))}
		case "controller.artifact.upload.finish":
			*result.(*artifacts.OperationResult) = artifacts.OperationResult{Artifact: &artifacts.Descriptor{Kind: artifacts.KindFirmware, SHA256: digest, Bytes: int64(len(data))}}
		default:
			t.Fatalf("import must not apply an update: %s", method)
		}
		return nil
	}}
	artifact, err := p.uploadNativeArtifact(context.Background(), path, artifacts.KindFirmware, digest)
	if err != nil || artifact.SHA256 != digest || !bytes.Equal(data, received) {
		t.Fatalf("transfer mismatch: %+v %v", artifact, err)
	}
	previous := calls
	if _, err := p.uploadNativeArtifact(context.Background(), path, artifacts.KindFirmware, "wrong"); err == nil || calls != previous {
		t.Fatal("checksum rejection contacted the host")
	}
}

func TestNativeArtifactDownloadChecksEveryChunkAndNeverOverwrites(t *testing.T) {
	data := []byte("verified board backup")
	hash := sha256.Sum256(data)
	artifact := artifacts.Descriptor{Kind: artifacts.KindEEPROM, SHA256: hex.EncodeToString(hash[:]), Bytes: int64(len(data))}
	for _, fault := range []string{"", "begin", "offset", "digest", "eof", "bytes", "payload", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			closed, chunks := 0, 0
			p := nativePages{rpc: func(_ context.Context, method string, params, result any) error {
				switch method {
				case "controller.artifact.download.begin":
					v := artifacts.PeerDownloadResult{TransferID: "owned", SHA256: artifact.SHA256, BytesTotal: artifact.Bytes, ChunkBytes: 5}
					if fault == "begin" {
						v.BytesTotal++
					}
					*result.(*artifacts.PeerDownloadResult) = v
				case "controller.artifact.download.chunk":
					chunks++
					if fault == "canceled" {
						return context.Canceled
					}
					r := params.(artifacts.PeerDownloadChunkRequest)
					end := r.Offset + int64(r.Length)
					v := artifacts.PeerDownloadResult{TransferID: "owned", SHA256: artifact.SHA256, Offset: r.Offset, NextOffset: end, BytesTotal: artifact.Bytes, Data: append([]byte(nil), data[r.Offset:end]...), EOF: end == artifact.Bytes}
					switch fault {
					case "offset":
						v.NextOffset++
					case "digest":
						v.SHA256 = "wrong"
					case "eof":
						v.EOF = !v.EOF
					case "bytes":
						v.BytesTotal++
					case "payload":
						v.Data[0] ^= 1
					}
					*result.(*artifacts.PeerDownloadResult) = v
				case "controller.artifact.download.close":
					closed++
				default:
					t.Fatalf("download must not mutate the board: %s", method)
				}
				return nil
			}}
			path := filepath.Join(t.TempDir(), "eeprom.bin")
			err := p.downloadNativeArtifact(context.Background(), artifact, path)
			if closed != 1 {
				t.Fatal("download session leaked")
			}
			if fault != "" {
				if err == nil {
					t.Fatal("corrupt transfer published")
				}
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal("invalid bytes published")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("download changed bytes")
			}
			before := chunks
			if err := p.downloadNativeArtifact(context.Background(), artifact, path); err == nil || chunks != before {
				t.Fatal("existing export was overwritten or contacted transport")
			}
		})
	}
}

func TestNativeArtifactIdentityIncludesKind(t *testing.T) {
	sha := "same-content"
	p := nativePages{rpc: func(_ context.Context, method string, _, result any) error {
		if method == "controller.artifact.list" {
			*result.(*artifacts.List) = artifacts.List{Artifacts: []artifacts.Descriptor{{Kind: artifacts.KindFirmware, SHA256: sha}, {Kind: artifacts.KindFlashBackup, SHA256: sha}}}
			return nil
		}
		if method == "controller.artifact.manifest" {
			return nil
		}
		t.Fatal(method)
		return nil
	}}
	page, err := p.loadNativeArtifacts(context.Background(), control.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.RowValues) != 2 || page.Rows[0][0] == page.Rows[1][0] {
		t.Fatal("equal digest merged distinct artifact kinds")
	}
}
