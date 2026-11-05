package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"pccontroller.local/desktop/assets"
)

const (
	appPrefix = "/app/"
	appCSP    = "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; " +
		"form-action 'none'; object-src 'none'; script-src 'self'; " +
		"style-src 'self'; img-src 'self' data:; font-src 'self'; " +
		"media-src 'self' blob:; " +
		"manifest-src 'self'; worker-src 'self'; " +
		"connect-src 'self'"
)

var (
	distributionAssets = newImmutableAssetSource(content)
	canonicalAssets    = newImmutableAssetSource(assets.Files)
)

type staticHandler struct {
	files  fs.FS
	source *immutableAssetSource
}

// immutableAssetSource owns only compact, once-computed ETags. Embedded file
// bytes remain in the executable's read-only image and are streamed through a
// seekable embed file; they are never copied into a long-lived Go cache.
type immutableAssetSource struct {
	files fs.FS
	etags sync.Map
}

type assetETag struct {
	once  sync.Once
	value string
	err   error
}

func newImmutableAssetSource(files fs.FS) *immutableAssetSource {
	return &immutableAssetSource{files: files}
}

// Handler returns an HTTP handler that accepts complete /app/... paths.
// Authentication and /app/session remain the responsibility of the parent
// local API mux.
func Handler() http.Handler {
	return staticHandler{
		files:  content,
		source: distributionAssets,
	}
}

func (handler staticHandler) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	setBrowserHeaders(response.Header())

	if request.URL.Path == "/app" {
		if request.Method != http.MethodGet &&
			request.Method != http.MethodHead {
			methodNotAllowed(response)
			return
		}
		http.Redirect(response, request, appPrefix, http.StatusPermanentRedirect)
		return
	}
	if !strings.HasPrefix(request.URL.Path, appPrefix) {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodGet &&
		request.Method != http.MethodHead {
		methodNotAllowed(response)
		return
	}

	relative := strings.TrimPrefix(request.URL.Path, appPrefix)
	if unsafeRelativePath(relative) {
		http.NotFound(response, request)
		return
	}
	if cue, ok := sharedSoundCue(relative); ok {
		handler.serveSharedSound(response, request, cue)
		return
	}
	if relative == "shared/icons/app.png" {
		handler.serveCanonicalAsset(
			response,
			request,
			"icons/app.png",
			"image/png",
			"app.png",
		)
		return
	}
	if relative == "shared/fonts/Vazirmatn-Regular.ttf" {
		handler.serveCanonicalAsset(
			response,
			request,
			"fonts/Vazirmatn-Regular.ttf",
			"font/ttf",
			"Vazirmatn-Regular.ttf",
		)
		return
	}
	if relative == "" {
		handler.serveFile(response, request, "index.html")
		return
	}
	if info, err := fs.Stat(handler.files, relative); err == nil &&
		!info.IsDir() {
		handler.serveFile(response, request, relative)
		return
	}

	// A missing path with an extension is a static-asset typo, never a SPA
	// route. The same rule keeps API-like JSON requests from receiving HTML.
	if path.Ext(relative) != "" ||
		!acceptsHTML(request.Header.Get("Accept")) {
		http.NotFound(response, request)
		return
	}
	handler.serveFile(response, request, "index.html")
}

func (handler staticHandler) serveSharedSound(
	response http.ResponseWriter,
	request *http.Request,
	cue string,
) {
	handler.serveCanonicalAsset(
		response,
		request,
		"sfx/"+cue+".wav",
		"audio/wav",
		cue+".wav",
	)
}

func (handler staticHandler) serveCanonicalAsset(
	response http.ResponseWriter,
	request *http.Request,
	name string,
	contentType string,
	downloadName string,
) {
	serveImmutableAsset(
		response,
		request,
		canonicalAssets,
		name,
		downloadName,
		contentType,
		"public, max-age=86400",
	)
}

func sharedSoundCue(relative string) (string, bool) {
	const prefix = "shared/sfx/"
	if !strings.HasPrefix(relative, prefix) {
		return "", false
	}
	cue := strings.TrimSuffix(strings.TrimPrefix(relative, prefix), ".wav")
	if relative != prefix+cue+".wav" {
		return "", false
	}
	switch cue {
	case "on", "off", "message", "error":
		return cue, true
	default:
		return "", false
	}
}

func (handler staticHandler) serveFile(
	response http.ResponseWriter,
	request *http.Request,
	name string,
) {
	contentType := embeddedContentType(name)
	if name == "sw.js" {
		response.Header().Set("Service-Worker-Allowed", appPrefix)
	}
	var cacheControl string
	switch {
	case name == "index.html",
		name == "sw.js",
		name == "manifest.webmanifest":
		cacheControl = "no-cache"
	case isFingerprintedName(name):
		cacheControl = "public, max-age=31536000, immutable"
	default:
		cacheControl = "public, max-age=86400"
	}

	serveImmutableAsset(
		response,
		request,
		handler.source,
		name,
		path.Base(name),
		contentType,
		cacheControl,
	)
}

func serveImmutableAsset(
	response http.ResponseWriter,
	request *http.Request,
	source *immutableAssetSource,
	name string,
	downloadName string,
	contentType string,
	cacheControl string,
) {
	tag, err := source.etag(name)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if cacheControl != "" {
		response.Header().Set("Cache-Control", cacheControl)
	}
	response.Header().Set("ETag", tag)
	if request.Header.Get("If-None-Match") == tag {
		response.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := source.files.Open(name)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	defer file.Close()
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(
			response,
			"embedded asset is not seekable",
			http.StatusInternalServerError,
		)
		return
	}
	http.ServeContent(
		response,
		request,
		downloadName,
		time.Time{},
		seeker,
	)
}

func (source *immutableAssetSource) etag(name string) (string, error) {
	if source == nil || source.files == nil {
		return "", fs.ErrNotExist
	}
	value, found := source.etags.Load(name)
	if !found {
		candidate := &assetETag{}
		value, _ = source.etags.LoadOrStore(name, candidate)
	}
	entry := value.(*assetETag)
	entry.once.Do(func() {
		file, err := source.files.Open(name)
		if err != nil {
			entry.err = err
			return
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			entry.err = err
			return
		}
		sum := hash.Sum(nil)
		entry.value = `"` + hex.EncodeToString(sum[:12]) + `"`
	})
	return entry.value, entry.err
}

func embeddedContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".webmanifest":
		return "application/manifest+json; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".wav":
		return "audio/wav"
	case ".ttf":
		return "font/ttf"
	default:
		return ""
	}
}

func isFingerprintedName(name string) bool {
	leaf := path.Base(name)
	dot := strings.LastIndexByte(leaf, '.')
	if dot < 1 || dot == len(leaf)-1 ||
		!allASCIIAlphaNumeric(leaf[dot+1:]) {
		return false
	}
	stem := leaf[:dot]
	for index := strings.IndexByte(stem, '-'); index >= 0; index = nextHyphen(stem, index+1) {
		if index < 1 {
			continue
		}
		hash := stem[index+1:]
		if len(hash) >= 8 && allFingerprintCharacters(hash) {
			return true
		}
	}
	return false
}

func nextHyphen(value string, start int) int {
	if start >= len(value) {
		return -1
	}
	index := strings.IndexByte(value[start:], '-')
	if index < 0 {
		return -1
	}
	return start + index
}

func allASCIIAlphaNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range []byte(value) {
		if !isASCIIAlphaNumeric(character) {
			return false
		}
	}
	return true
}

func allFingerprintCharacters(value string) bool {
	for _, character := range []byte(value) {
		if !isASCIIAlphaNumeric(character) &&
			character != '_' &&
			character != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}

func setBrowserHeaders(header http.Header) {
	// The local API sets an exact-host CSP before delegating here. Retain it;
	// the self-only policy is a defense-in-depth fallback for standalone use.
	if header.Get("Content-Security-Policy") == "" {
		header.Set("Content-Security-Policy", appCSP)
	}
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set(
		"Permissions-Policy",
		"camera=(), geolocation=(), microphone=(), payment=(), usb=()",
	)
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}

func unsafeRelativePath(relative string) bool {
	if relative == "" {
		return false
	}
	if strings.ContainsAny(relative, `\`+"\x00") {
		return true
	}
	comparable := strings.TrimSuffix(relative, "/")
	if comparable == "" {
		return false
	}
	for _, segment := range strings.Split(comparable, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return true
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+comparable), "/")
	return clean != comparable
}

func acceptsHTML(value string) bool {
	for _, part := range strings.Split(value, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if mediaType == "text/html" ||
			mediaType == "application/xhtml+xml" {
			return true
		}
	}
	return false
}

func methodNotAllowed(response http.ResponseWriter) {
	response.Header().Set("Allow", fmt.Sprintf("%s, %s", http.MethodGet, http.MethodHead))
	http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
}
