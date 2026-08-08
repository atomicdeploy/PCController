// Package webui serves the browser application embedded in the controller
// executable. The handler is deliberately independent from IPC so it can be
// mounted behind the existing REST/WebSocket mux without owning a listener.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var defaultReservedPrefixes = []string{
	"/api",
	"/healthz",
	"/ipc",
	"/socket.io",
}

//go:embed all:dist
var embeddedFiles embed.FS

var embeddedHandler = mustEmbeddedHandler()

type assetMetadata struct {
	etag    string
	modTime time.Time
	size    int64
}

type staticHandler struct {
	files            fs.FS
	assets           map[string]assetMetadata
	reservedPrefixes []string
}

// Handler returns a handler for the embedded production bundle. With no extra
// prefixes it reuses a process-wide handler. Passing the configured WebSocket
// path creates an otherwise identical handler that also reserves that route.
// Handlers are safe for concurrent use and do not create a network listener.
func Handler(additionalReservedPrefixes ...string) http.Handler {
	if len(additionalReservedPrefixes) == 0 {
		return embeddedHandler
	}
	return mustEmbeddedHandler(additionalReservedPrefixes...)
}

// NewHandler creates a handler over an immutable filesystem rooted at the web
// distribution directory. Additional prefixes are combined with the built-in
// API/IPC reservations and are useful for a configured WebSocket path.
func NewHandler(files fs.FS, additionalReservedPrefixes ...string) (http.Handler, error) {
	if files == nil {
		return nil, errors.New("web UI filesystem is nil")
	}
	assets, err := indexAssets(files)
	if err != nil {
		return nil, err
	}
	if _, ok := assets["index.html"]; !ok {
		return nil, errors.New("web UI filesystem does not contain index.html")
	}
	return &staticHandler{
		files:            files,
		assets:           assets,
		reservedPrefixes: normalizeReservedPrefixes(additionalReservedPrefixes),
	}, nil
}

func mustEmbeddedHandler(additionalReservedPrefixes ...string) http.Handler {
	root, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		panic("open embedded web UI: " + err.Error())
	}
	handler, err := NewHandler(root, additionalReservedPrefixes...)
	if err != nil {
		panic("index embedded web UI: " + err.Error())
	}
	return handler
}

func indexAssets(files fs.FS) (map[string]assetMetadata, error) {
	assets := make(map[string]assetMetadata)
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		assets[name] = assetMetadata{
			etag:    `"` + hex.EncodeToString(digest[:]) + `"`,
			modTime: info.ModTime(),
			size:    int64(len(content)),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func (handler *staticHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(writer.Header())
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestPath, ok := safeRequestPath(request)
	if !ok || handler.reserved(requestPath) {
		http.NotFound(writer, request)
		return
	}

	name := strings.TrimPrefix(requestPath, "/")
	if name == "" {
		name = "index.html"
	}
	// Development bundles may omit the generated ICO. Production bundles contain
	// the real multi-resolution icon and take this path directly; SVG is only a
	// compatibility fallback for incomplete development filesystems.
	if name == "favicon.ico" {
		if _, exists := handler.assets[name]; !exists {
			if _, svgExists := handler.assets["favicon.svg"]; !svgExists {
				http.NotFound(writer, request)
				return
			}
			name = "favicon.svg"
		}
	}
	if _, exists := handler.assets[name]; exists {
		handler.serveAsset(writer, request, name)
		return
	}
	// An already-open browser tab can briefly retain an older index document
	// while the self-contained host executable is replaced. Vite fingerprints
	// its entry script and stylesheet, so that stale document would otherwise
	// receive a 404 and render an unstyled, inert shell. Redirect only the two
	// well-known entry assets to the current bundle; chunks remain strict so a
	// genuinely inconsistent package still fails loudly instead of mixing code.
	if replacement, exists := handler.staleEntryReplacement(name); exists {
		writer.Header().Set("Cache-Control", "no-store")
		http.Redirect(writer, request, "/"+replacement, http.StatusTemporaryRedirect)
		return
	}
	if acceptsSPAFallback(requestPath, request.Header.Get("Accept")) {
		handler.serveAsset(writer, request, "index.html")
		return
	}
	http.NotFound(writer, request)
}

func (handler *staticHandler) staleEntryReplacement(name string) (string, bool) {
	if path.Dir(name) != "assets" || !fingerprintedAsset(name) {
		return "", false
	}
	base := path.Base(name)
	prefix := ""
	switch {
	case strings.HasPrefix(base, "app-") && strings.EqualFold(path.Ext(base), ".js"):
		prefix = "app-"
	case strings.HasPrefix(base, "index-") && strings.EqualFold(path.Ext(base), ".css"):
		prefix = "index-"
	default:
		return "", false
	}

	replacement := ""
	for candidate := range handler.assets {
		candidateBase := path.Base(candidate)
		if path.Dir(candidate) != "assets" || !strings.HasPrefix(candidateBase, prefix) ||
			!strings.EqualFold(path.Ext(candidateBase), path.Ext(base)) || !fingerprintedAsset(candidate) {
			continue
		}
		if replacement != "" {
			// Multiple entry candidates indicate a malformed or transitional
			// bundle. Do not guess which executable resource is authoritative.
			return "", false
		}
		replacement = candidate
	}
	return replacement, replacement != ""
}

func (handler *staticHandler) serveAsset(
	writer http.ResponseWriter,
	request *http.Request,
	name string,
) {
	metadata, ok := handler.assets[name]
	if !ok {
		http.NotFound(writer, request)
		return
	}
	file, err := handler.files.Open(name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()

	var content io.ReadSeeker
	if seeker, seekable := file.(io.ReadSeeker); seekable {
		content = seeker
	} else {
		value, readErr := io.ReadAll(file)
		if readErr != nil || int64(len(value)) != metadata.size {
			http.Error(writer, "read embedded asset", http.StatusInternalServerError)
			return
		}
		content = bytes.NewReader(value)
	}

	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("Content-Type", contentType(name))
	writer.Header().Set("ETag", metadata.etag)
	if name != "index.html" && fingerprintedAsset(name) {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		writer.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(writer, request, path.Base(name), metadata.modTime, content)
}

func safeRequestPath(request *http.Request) (string, bool) {
	if request == nil || request.URL == nil {
		return "", false
	}
	escaped := request.URL.EscapedPath()
	if escaped == "" {
		escaped = "/"
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil || decoded == "" || decoded[0] != '/' ||
		strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, '\x00') {
		return "", false
	}
	for _, segment := range strings.Split(decoded, "/") {
		candidate := segment
		for range 2 {
			unescaped, unescapeErr := url.PathUnescape(candidate)
			if unescapeErr != nil {
				return "", false
			}
			candidate = unescaped
		}
		if candidate == "." || candidate == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(decoded)
	if cleaned == "." {
		cleaned = "/"
	}
	name := strings.TrimPrefix(cleaned, "/")
	if name != "" && !fs.ValidPath(name) {
		return "", false
	}
	return cleaned, true
}

func acceptsSPAFallback(requestPath, accept string) bool {
	if requestPath == "/" {
		return true
	}
	if path.Ext(path.Base(requestPath)) != "" {
		return false
	}
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "text/html") ||
		strings.Contains(accept, "application/xhtml+xml")
}

func normalizeReservedPrefixes(additional []string) []string {
	values := append([]string(nil), defaultReservedPrefixes...)
	values = append(values, additional...)
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.Contains(value, "\\") {
			continue
		}
		value = "/" + strings.Trim(value, "/")
		value = path.Clean(value)
		if value == "/" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (handler *staticHandler) reserved(requestPath string) bool {
	for _, prefix := range handler.reservedPrefixes {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".webmanifest":
		return "application/manifest+json"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".wasm":
		return "application/wasm"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	}
	if value := mime.TypeByExtension(path.Ext(name)); value != "" {
		return value
	}
	return "application/octet-stream"
}

func fingerprintedAsset(name string) bool {
	base := path.Base(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	separator := strings.LastIndexAny(stem, "-.")
	if separator < 0 || separator == len(stem)-1 {
		return false
	}
	fingerprint := stem[separator+1:]
	if len(fingerprint) < 8 || len(fingerprint) > 64 {
		return false
	}
	for _, character := range fingerprint {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return false
		}
	}
	return true
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"img-src 'self' data:",
		"font-src 'self'",
		"style-src 'self'",
		"style-src-elem 'self'",
		"style-src-attr 'unsafe-inline'",
		"script-src 'self'",
		"worker-src 'self'",
		"connect-src 'self'",
		"manifest-src 'self'",
	}, "; "))
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=()")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}
