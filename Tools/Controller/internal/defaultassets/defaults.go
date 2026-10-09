// Package defaultassets exposes optional build-time board recovery images.
// Their presence enables the recovery offer; no package API performs a write.
package defaultassets

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"pccontroller.local/controller/internal/programmer"
)

const metadataPath = "assets/default-metadata.json"

//go:embed all:assets
var embedded embed.FS

// Artifact is one verified file embedded in a host release.
type Artifact struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	File           string `json:"file"`
	SHA256         string `json:"sha256"`
	Bytes          int    `json:"bytes"`
	BuildHash      string `json:"build_hash,omitempty"`
	BuildTimestamp string `json:"build_timestamp,omitempty"`
	Data           []byte `json:"-"`
}

// Metadata is generated from the validated firmware build manifest.
type Metadata struct {
	Format       string   `json:"format"`
	GeneratedUTC string   `json:"generated_utc"`
	Firmware     Artifact `json:"firmware"`
	EEPROM       Artifact `json:"eeprom"`
}

// Bundle is enabled only when both current-format recovery images validate.
type Bundle struct {
	Enabled  bool
	Metadata Metadata
	Firmware Artifact
	EEPROM   Artifact
}

// Load returns an unavailable bundle when the build did not package defaults.
// A present but damaged/incomplete bundle is an error, never a silent fallback.
func Load() (Bundle, error) { return loadFS(embedded) }

func loadFS(files fs.FS) (Bundle, error) {
	raw, err := fs.ReadFile(files, metadataPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Bundle{}, nil
	}
	if err != nil {
		return Bundle{}, fmt.Errorf("read embedded default metadata: %w", err)
	}
	var metadata Metadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return Bundle{}, fmt.Errorf("decode embedded default metadata: %w", err)
	}
	if metadata.Format != "controller-embedded-defaults/v1" {
		return Bundle{}, fmt.Errorf("unsupported embedded default metadata format %q", metadata.Format)
	}
	firmware, err := loadArtifact(files, metadata.Firmware, "firmware")
	if err != nil {
		return Bundle{}, err
	}
	eeprom, err := loadArtifact(files, metadata.EEPROM, "eeprom")
	if err != nil {
		return Bundle{}, err
	}
	if err := validateIntelHex(firmware, false); err != nil {
		return Bundle{}, fmt.Errorf("embedded default firmware: %w", err)
	}
	if err := validateIntelHex(eeprom, true); err != nil {
		return Bundle{}, fmt.Errorf("embedded default EEPROM: %w", err)
	}
	return Bundle{Enabled: true, Metadata: metadata, Firmware: firmware, EEPROM: eeprom}, nil
}

func loadArtifact(files fs.FS, artifact Artifact, expectedKind string) (Artifact, error) {
	artifact.Kind = strings.TrimSpace(artifact.Kind)
	artifact.Name = strings.TrimSpace(artifact.Name)
	artifact.File = strings.TrimSpace(artifact.File)
	artifact.SHA256 = strings.ToLower(strings.TrimSpace(artifact.SHA256))
	if artifact.Kind != expectedKind || artifact.Name == "" || artifact.File == "" {
		return Artifact{}, fmt.Errorf("embedded %s metadata is incomplete", expectedKind)
	}
	if path.Base(artifact.File) != artifact.File || strings.Contains(artifact.File, "\\") {
		return Artifact{}, fmt.Errorf("embedded %s file name is unsafe", expectedKind)
	}
	if len(artifact.SHA256) != sha256.Size*2 {
		return Artifact{}, fmt.Errorf("embedded %s SHA-256 has the wrong length", expectedKind)
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return Artifact{}, fmt.Errorf("embedded %s SHA-256 is not hexadecimal", expectedKind)
	}
	content, err := fs.ReadFile(files, "assets/"+artifact.File)
	if err != nil {
		return Artifact{}, fmt.Errorf("read embedded %s: %w", expectedKind, err)
	}
	if len(content) == 0 || artifact.Bytes != len(content) {
		return Artifact{}, fmt.Errorf("embedded %s size mismatch", expectedKind)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return Artifact{}, fmt.Errorf("embedded %s SHA-256 mismatch", expectedKind)
	}
	artifact.Data = content
	return artifact, nil
}

func validateIntelHex(artifact Artifact, eeprom bool) error {
	image, err := programmer.ParseIntelHex(bytes.NewReader(artifact.Data))
	if err != nil {
		return err
	}
	inspection, err := image.Inspect()
	if err != nil {
		return err
	}
	if !inspection.HasData {
		return errors.New("image contains no data")
	}
	if eeprom {
		if inspection.DataBytes != programmer.PCControllerEEPROMBytes ||
			inspection.MinimumAddress != 0 ||
			inspection.MaximumAddress+1 != programmer.PCControllerEEPROMBytes {
			return fmt.Errorf("require a complete %d-byte Intel HEX image", programmer.PCControllerEEPROMBytes)
		}
		return nil
	}
	if inspection.MaximumAddress >= programmer.ATmega328PFlashSize {
		return errors.New("image exceeds ATmega328P flash")
	}
	return nil
}
