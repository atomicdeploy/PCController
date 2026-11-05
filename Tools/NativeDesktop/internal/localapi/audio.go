package localapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const audioFilenameHeader = "X-PCControllerDesktop-Filename"

// AudioAsset is one resolved PCM-WAV cue. Data must remain immutable for the
// duration of the callback; the API does not retain it after the response.
type AudioAsset struct {
	Data       []byte
	Name       string
	SHA256     string
	Source     string
	ModifiedAt time.Time
}

// ImportedAudio identifies one content-addressed custom WAV without exposing
// its storage path to browser configuration or IPC.
type ImportedAudio struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AudioAssetFunc resolves a semantic cue such as on, off, message, or error.
type AudioAssetFunc func(cue string) (AudioAsset, error)

// AudioImportFunc validates and stores one custom WAV.
type AudioImportFunc func(name string, data []byte) (ImportedAudio, error)

func (a *api) uiAudioAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if a.audioAsset == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "audio assets are unavailable")
		return
	}

	cue := strings.TrimPrefix(r.URL.Path, "/ui/audio/")
	if strings.Contains(cue, "/") || !validAudioCue(cue) {
		writeError(w, http.StatusNotFound, "not_found", "audio cue not found")
		return
	}
	asset, err := a.audioAsset(cue)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "audio_unavailable", err.Error())
		return
	}
	if len(asset.Data) == 0 || int64(len(asset.Data)) > maxAudioBytes {
		writeError(w, http.StatusServiceUnavailable, "audio_unavailable", "resolved audio asset is invalid")
		return
	}

	sum := strings.ToLower(strings.TrimSpace(asset.SHA256))
	if len(sum) != sha256.Size*2 {
		digest := sha256.Sum256(asset.Data)
		sum = hex.EncodeToString(digest[:])
	}
	tag := `"` + sum + `"`
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", tag)
	w.Header().Set("Accept-Ranges", "bytes")
	if name := safeAudioDownloadName(asset.Name); name != "" {
		if value := mime.FormatMediaType(
			"inline",
			map[string]string{"filename": name},
		); value != "" {
			w.Header().Set("Content-Disposition", value)
		}
	}
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(
		w,
		r,
		safeAudioDownloadName(asset.Name),
		asset.ModifiedAt,
		bytes.NewReader(asset.Data),
	)
}

func (a *api) uiAudioImport(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if a.audioImport == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "custom audio import is unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "audio/wav" && mediaType != "audio/x-wav") {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "custom audio must be a PCM WAV file")
		return
	}
	if r.ContentLength > maxAudioBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "custom audio exceeds 4 MiB")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAudioBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "could not read custom audio")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_body", "custom audio is empty")
		return
	}
	if int64(len(data)) > maxAudioBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "custom audio exceeds 4 MiB")
		return
	}

	// Keep optimistic-concurrency validation, content-addressed import, and
	// the resulting config patch in one browser transaction. A stale tab must
	// not install an orphan asset before learning that its revision lost.
	a.uiConfigMu.Lock()
	defer a.uiConfigMu.Unlock()
	if a.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "browser configuration is unavailable")
		return
	}
	if !a.requireUIConfigRevision(w, r) {
		return
	}

	imported, err := a.audioImport(r.Header.Get(audioFilenameHeader), data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_audio", err.Error())
		return
	}
	if !validAudioAssetID(imported.ID) {
		writeError(w, http.StatusInternalServerError, "internal_error", "audio importer returned an invalid asset identity")
		return
	}
	imported.Name = safeAudioDisplayName(imported.Name)
	if imported.Name == "" {
		imported.Name = "Custom sound.wav"
	}
	patch, err := json.Marshal(map[string]any{
		"notifications": map[string]any{
			"sound":        true,
			"sound_source": "custom",
			"sound_file":   strings.ToLower(imported.ID),
			"sound_name":   imported.Name,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not encode audio configuration")
		return
	}
	// Import is an atomic convenience mutation: serialize it with ordinary
	// browser configuration patches and return the same redacted config shape.
	a.writeBrowserConfigPatch(w, r, patch)
}

func validAudioCue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "off", "message", "error":
		return true
	default:
		return false
	}
}

func validAudioAssetID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func safeAudioDownloadName(value string) string {
	value = safeAudioDisplayName(value)
	if value == "" {
		return "pccontroller-desktop-sfx.wav"
	}
	if !strings.EqualFold(filepath.Ext(value), ".wav") {
		value += ".wav"
	}
	return value
}

func safeAudioDisplayName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(filepath.Base(value))
	if value == "." || value == string(filepath.Separator) {
		return ""
	}
	runes := []rune(value)
	if len(runes) > 128 {
		value = string(runes[:128])
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0 {
		return ""
	}
	return value
}
