package artifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const maxJSONBody = 64 << 10

// Handler exposes only artifact-domain semantics. The IPC server wraps it with
// its loopback/authentication/remote-capability policy before dispatch.
func (service *Service) Handler() http.Handler {
	return http.HandlerFunc(service.serveHTTP)
}

func (service *Service) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimSuffix(request.URL.Path, "/")
	switch {
	case path == "/api/v1/artifacts/manifest" && request.Method == http.MethodGet:
		manifest, err := service.Manifest()
		writeHTTPResult(writer, manifest, err)
	case path == "/api/v1/artifacts" && request.Method == http.MethodGet:
		var kind *Kind
		if raw := strings.TrimSpace(request.URL.Query().Get("kind")); raw != "" {
			parsed, err := ParseKind(raw)
			if err != nil {
				writeHTTPError(writer, http.StatusBadRequest, err)
				return
			}
			kind = &parsed
		}
		list, err := service.List(kind)
		writeHTTPResult(writer, list, err)
	case path == "/api/v1/artifacts/upload" && request.Method == http.MethodPost:
		service.serveUpload(writer, request)
	case path == "/api/v1/artifacts/fetch" && request.Method == http.MethodPost:
		var value FetchRequest
		if !decodeHTTPJSON(writer, request, &value) {
			return
		}
		applyIdempotencyHeader(request, &value.IdempotencyKey)
		result, err := service.StartFetch(value)
		writeHTTPAccepted(writer, result, err)
	case path == "/api/v1/artifacts/capture" && request.Method == http.MethodPost:
		var value CaptureRequest
		if !decodeHTTPJSON(writer, request, &value) {
			return
		}
		applyIdempotencyHeader(request, &value.IdempotencyKey)
		result, err := service.StartCapture(value)
		writeHTTPAccepted(writer, result, err)
	case path == "/api/v1/artifacts/current/flash" && (request.Method == http.MethodGet || request.Method == http.MethodHead):
		descriptor, err := service.store.Current(KindFlashBackup)
		if err != nil {
			writeHTTPError(writer, http.StatusInternalServerError, err)
			return
		}
		if descriptor == nil || !descriptor.VerifiedReadback {
			writeHTTPError(writer, http.StatusNotFound, errors.New("no verified current flash readback; request an explicit capture first"))
			return
		}
		service.serveDownload(writer, request, descriptor.Kind, descriptor.SHA256)
	case path == "/api/v1/artifacts/current/eeprom" && (request.Method == http.MethodGet || request.Method == http.MethodHead):
		descriptor, err := service.store.Current(KindEEPROM)
		if err != nil {
			writeHTTPError(writer, http.StatusInternalServerError, err)
			return
		}
		if descriptor == nil || !descriptor.VerifiedReadback {
			writeHTTPError(writer, http.StatusNotFound, errors.New("no verified current EEPROM readback; request an explicit capture first"))
			return
		}
		service.serveDownload(writer, request, descriptor.Kind, descriptor.SHA256)
	case strings.HasPrefix(path, "/api/v1/artifacts/") && (request.Method == http.MethodGet || request.Method == http.MethodHead):
		parts := strings.Split(strings.TrimPrefix(path, "/api/v1/artifacts/"), "/")
		if len(parts) != 2 {
			writeHTTPError(writer, http.StatusNotFound, os.ErrNotExist)
			return
		}
		kind, err := ParseKind(parts[0])
		if err != nil {
			writeHTTPError(writer, http.StatusBadRequest, err)
			return
		}
		service.serveDownload(writer, request, kind, parts[1])
	case path == "/api/v1/updates/firmware" && request.Method == http.MethodPost:
		service.serveUpdate(writer, request, service.StartFirmwareUpdate)
	case path == "/api/v1/restores/flash" && request.Method == http.MethodPost:
		service.serveUpdate(writer, request, service.StartFlashRestore)
	case path == "/api/v1/updates/eeprom" && request.Method == http.MethodPost:
		service.serveUpdate(writer, request, service.StartEEPROMUpdate)
	case path == "/api/v1/updates/host" && request.Method == http.MethodPost:
		service.serveUpdate(writer, request, service.StartHostUpdate)
	case strings.HasPrefix(path, "/api/v1/updates/status") && request.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "/api/v1/updates/status")
		id = strings.TrimPrefix(id, "/")
		status, err := service.Status(id)
		writeHTTPResult(writer, status, err)
	default:
		writer.Header().Set("Allow", allowedMethods(path))
		writeHTTPError(writer, http.StatusNotFound, errors.New("artifact endpoint not found"))
	}
}

// HTTPRequiresProgramming tells the outer IPC policy whether this route can
// mutate stored or hardware state.
func HTTPRequiresProgramming(request *http.Request) bool {
	return request != nil && request.Method != http.MethodGet && request.Method != http.MethodHead
}

func (service *Service) serveUpload(writer http.ResponseWriter, request *http.Request) {
	kind, err := ParseKind(request.URL.Query().Get("kind"))
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return
	}
	expectedBytes, err := optionalInt64(request.URL.Query().Get("bytes"))
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return
	}
	packed, err := optionalUint32(request.URL.Query().Get("packed_timestamp"))
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxBytes(kind)+(1<<20))
	input := io.Reader(request.Body)
	name := request.URL.Query().Get("name")
	if strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "multipart/") {
		multipartReader, readerErr := request.MultipartReader()
		if readerErr != nil {
			writeHTTPError(writer, http.StatusBadRequest, readerErr)
			return
		}
		part, readerErr := nextUploadPart(multipartReader)
		if readerErr != nil {
			writeHTTPError(writer, http.StatusBadRequest, readerErr)
			return
		}
		defer part.Close()
		input = part
		if strings.TrimSpace(name) == "" {
			name = part.FileName()
		}
	}
	result, err := service.UploadOperation(input, PutOptions{
		Kind: kind, Name: name, Source: "browser-upload",
		ExpectedSHA256: firstNonEmpty(request.URL.Query().Get("sha256"), request.Header.Get("X-Checksum-SHA256")),
		ExpectedBytes:  expectedBytes, BuildHash: request.URL.Query().Get("build_hash"),
		BuildTimestamp: request.URL.Query().Get("build_timestamp"), PackedTimestamp: packed,
		Platform: request.URL.Query().Get("platform"),
	})
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return
	}
	writeHTTPJSON(writer, http.StatusCreated, result)
}

func (service *Service) serveDownload(writer http.ResponseWriter, request *http.Request, kind Kind, digest string) {
	descriptor, file, err := service.Open(kind, digest)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeHTTPError(writer, status, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeHTTPError(writer, http.StatusInternalServerError, err)
		return
	}
	writer.Header().Set("Content-Type", descriptor.MediaType)
	writer.Header().Set("ETag", `"sha256:`+descriptor.SHA256+`"`)
	writer.Header().Set("X-Checksum-SHA256", descriptor.SHA256)
	writer.Header().Set("X-Artifact-Kind", string(descriptor.Kind))
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": descriptor.Name}))
	http.ServeContent(writer, request, descriptor.Name, info.ModTime(), file)
}

func (service *Service) serveUpdate(
	writer http.ResponseWriter,
	request *http.Request,
	start func(UpdateRequest) (OperationResult, error),
) {
	var value UpdateRequest
	if !decodeHTTPJSON(writer, request, &value) {
		return
	}
	applyIdempotencyHeader(request, &value.IdempotencyKey)
	result, err := start(value)
	writeHTTPAccepted(writer, result, err)
}

func applyIdempotencyHeader(request *http.Request, destination *string) {
	if destination != nil && strings.TrimSpace(*destination) == "" {
		*destination = strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	}
}

func nextUploadPart(reader interface {
	NextPart() (*multipart.Part, error)
}) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FormName() == "file" || part.FileName() != "" {
			return part, nil
		}
		_ = part.Close()
	}
}

func decodeHTTPJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxJSONBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request contains trailing JSON")
		}
		writeHTTPError(writer, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeHTTPAccepted(writer http.ResponseWriter, value any, err error) {
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, err)
		return
	}
	writeHTTPJSON(writer, http.StatusAccepted, value)
}

func writeHTTPResult(writer http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeHTTPError(writer, status, err)
		return
	}
	writeHTTPJSON(writer, http.StatusOK, value)
}

func writeHTTPError(writer http.ResponseWriter, status int, err error) {
	writeHTTPJSON(writer, status, map[string]string{"error": err.Error()})
}

func writeHTTPJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func optionalInt64(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid byte count %q", value)
	}
	return parsed, nil
}

func optionalUint32(value string) (uint32, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	base := 10
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		base = 16
		value = value[2:]
	}
	parsed, err := strconv.ParseUint(value, base, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid packed timestamp %q", value)
	}
	return uint32(parsed), nil
}

func allowedMethods(path string) string {
	if strings.Contains(path, "/upload") || strings.Contains(path, "/fetch") ||
		strings.Contains(path, "/capture") || strings.Contains(path, "/updates/") ||
		strings.Contains(path, "/restores/") {
		return http.MethodPost
	}
	return http.MethodGet + ", " + http.MethodHead
}
