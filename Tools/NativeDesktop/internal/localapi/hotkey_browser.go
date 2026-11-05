package localapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"pccontroller.local/desktop/internal/winfx"
)

const (
	maxHotkeyRequestBytes = int64(4 << 10)
	maxHotkeyChordRunes   = 128
)

type hotkeyCaptureRequest struct {
	LeaseID string `json:"lease_id,omitempty"`
}

type hotkeyProbeRequest struct {
	LeaseID string `json:"lease_id"`
	Chord   string `json:"chord"`
}

type hotkeyCaptureResult struct {
	LeaseID   string    `json:"lease_id"`
	ExpiresAt time.Time `json:"expires_at"`
	LeaseTTL  int64     `json:"lease_ttl_ms"`
}

type hotkeyReleaseResult struct {
	Released bool `json:"released"`
}

type hotkeyProbeResult struct {
	LeaseID   string    `json:"lease_id"`
	ExpiresAt time.Time `json:"expires_at"`
	LeaseTTL  int64     `json:"lease_ttl_ms"`
	Chord     string    `json:"chord"`
	Available bool      `json:"available"`
	Message   string    `json:"message,omitempty"`
}

func (a *api) uiHotkeyCapture(w http.ResponseWriter, r *http.Request) {
	if a.hotkeyCapture == nil || !a.hotkeyCapture.captureAvailable() {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"unavailable",
			"hotkey capture is unavailable",
		)
		return
	}

	switch r.Method {
	case http.MethodPost:
		var params hotkeyCaptureRequest
		if !decodeHotkeyRequest(w, r, &params) {
			return
		}
		var (
			lease hotkeyCaptureLease
			err   error
		)
		if params.LeaseID == "" {
			lease, err = a.hotkeyCapture.begin(r.Context())
		} else {
			if !validRandomToken(params.LeaseID) {
				writeError(
					w,
					http.StatusBadRequest,
					"invalid_lease",
					"lease_id is invalid",
				)
				return
			}
			lease, err = a.hotkeyCapture.renew(r.Context(), params.LeaseID)
		}
		if err != nil {
			writeHotkeyCaptureError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response{
			OK:     true,
			Result: captureLeaseResult(lease),
		})

	case http.MethodDelete:
		var params hotkeyCaptureRequest
		if !decodeHotkeyRequest(w, r, &params) {
			return
		}
		if !validRandomToken(params.LeaseID) {
			writeError(
				w,
				http.StatusBadRequest,
				"invalid_lease",
				"lease_id is required and must be a valid opaque lease id",
			)
			return
		}
		if err := a.hotkeyCapture.release(r.Context(), params.LeaseID); err != nil {
			writeHotkeyCaptureError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response{
			OK:     true,
			Result: hotkeyReleaseResult{Released: true},
		})

	default:
		methodNotAllowed(w, http.MethodPost+", "+http.MethodDelete)
	}
}

func (a *api) uiHotkeyProbe(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if a.hotkeyCapture == nil || !a.hotkeyCapture.probeAvailable() {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"unavailable",
			"hotkey probing is unavailable",
		)
		return
	}

	var params hotkeyProbeRequest
	if !decodeHotkeyRequest(w, r, &params) {
		return
	}
	if !validRandomToken(params.LeaseID) {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_lease",
			"lease_id is required and must be a valid opaque lease id",
		)
		return
	}
	if !utf8.ValidString(params.Chord) ||
		utf8.RuneCountInString(params.Chord) > maxHotkeyChordRunes {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_hotkey",
			"chord must be valid UTF-8 and no longer than 128 characters",
		)
		return
	}
	parsed, err := winfx.ParseHotkey(params.Chord)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_hotkey",
			"chord is not a supported keyboard shortcut",
		)
		return
	}
	if parsed.Key == 0x7B { // VK_F12 is reserved for the debugger.
		writeError(
			w,
			http.StatusBadRequest,
			"reserved_hotkey",
			"F12 is reserved by Windows and cannot be used as a global hotkey",
		)
		return
	}
	chord := parsed.String()
	outcome, err := a.hotkeyCapture.probeChord(
		r.Context(),
		params.LeaseID,
		chord,
	)
	if err != nil {
		writeHotkeyCaptureError(w, err)
		return
	}

	result := hotkeyProbeResult{
		LeaseID:   outcome.Lease.ID,
		ExpiresAt: outcome.Lease.ExpiresAt,
		LeaseTTL:  hotkeyCaptureLeaseTTL.Milliseconds(),
		Chord:     chord,
		Available: outcome.ProbeError == nil,
	}
	if outcome.ProbeError != nil {
		result.Message = "Windows or another application already owns this shortcut."
	}
	writeJSON(w, http.StatusOK, response{OK: true, Result: result})
}

func captureLeaseResult(lease hotkeyCaptureLease) hotkeyCaptureResult {
	return hotkeyCaptureResult{
		LeaseID:   lease.ID,
		ExpiresAt: lease.ExpiresAt,
		LeaseTTL:  hotkeyCaptureLeaseTTL.Milliseconds(),
	}
}

func decodeHotkeyRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if !requireJSONContentType(w, r) {
		return false
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxHotkeyRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(
				w,
				http.StatusRequestEntityTooLarge,
				"request_too_large",
				"request body exceeds 4 KiB",
			)
			return false
		}
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_json",
			"unable to read request body",
		)
		return false
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' ||
		trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_json",
			"request body must contain one JSON object",
		)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		message := "request body contains unsupported or invalid fields"
		if strings.Contains(err.Error(), "unknown field") {
			message = "request body contains an unknown field"
		}
		writeError(w, http.StatusBadRequest, "invalid_json", message)
		return false
	}
	return true
}

func writeHotkeyCaptureError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errHotkeyLeaseNotFound):
		writeError(
			w,
			http.StatusNotFound,
			"lease_not_found",
			"hotkey capture lease is missing or expired",
		)
	case errors.Is(err, errHotkeyLeaseLimit):
		writeError(
			w,
			http.StatusTooManyRequests,
			"too_many_leases",
			"too many hotkey capture leases are active",
		)
	case errors.Is(err, errHotkeyCaptureUnavailable),
		errors.Is(err, errHotkeyProbeUnavailable),
		errors.Is(err, errHotkeyCaptureClosed),
		errors.Is(err, errHotkeyCaptureTransition):
		writeError(
			w,
			http.StatusServiceUnavailable,
			"unavailable",
			"hotkey capture is temporarily unavailable",
		)
	default:
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"unable to manage hotkey capture lease",
		)
	}
}
