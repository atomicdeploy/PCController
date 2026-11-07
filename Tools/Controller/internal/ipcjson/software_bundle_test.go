package ipcjson

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoftwareBundleConfinesPathsAndVerifiesBytes(t *testing.T) {
	directory := t.TempDir()
	t.Cleanup(func() {
		bundleCache.Lock()
		defer bundleCache.Unlock()
		for key, image := range bundleCache.files {
			if strings.HasPrefix(key, directory+"\x00") {
				image.file.Close()
				delete(bundleCache.files, key)
			}
		}
	})
	bytes := []byte("software bundle fixture")
	hash := sha256.Sum256(bytes)
	digest := hex.EncodeToString(hash[:])
	if err := os.WriteFile(filepath.Join(directory, "candidate.exe"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	catalog := func(path string) []byte {
		value, _ := json.Marshal(map[string]any{"format": "digitalogic-software-bundle", "assets": []any{map[string]any{"id": "candidate", "path": path, "manifest": map[string]any{"service": "pealayer", "sha256": digest, "size": len(bytes)}}}})
		return value
	}
	os.WriteFile(filepath.Join(directory, "software-bundle.json"), catalog("candidate.exe"), 0600)
	list, err := softwareBundleList(directory)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(list)
	if strings.Contains(string(public), `"path"`) {
		t.Fatal("private bundle path exposed")
	}
	value, err := softwareBundleReadChunk(directory, softwareBundleRead{AssetID: "candidate", SHA256: digest, Length: 8})
	if err != nil {
		t.Fatal(err)
	}
	chunk := value.(map[string]any)
	decoded, _ := base64.StdEncoding.DecodeString(chunk["data"].(string))
	if string(decoded) != string(bytes[:8]) {
		t.Fatal("incorrect bundle bytes")
	}
	if _, err = softwareBundleReadChunk(directory, softwareBundleRead{AssetID: "candidate", SHA256: strings.Repeat("a", 64), Length: 1}); err == nil {
		t.Fatal("wrong identity accepted")
	}
	os.WriteFile(filepath.Join(directory, "software-bundle.json"), catalog("../candidate.exe"), 0600)
	if _, err = softwareBundleList(directory); err == nil {
		t.Fatal("bundle traversal accepted")
	}
}
