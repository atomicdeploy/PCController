package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"pccontroller.local/desktop/assets"
	"pccontroller.local/desktop/internal/buildinfo"
)

func TestEmbeddedEditionMatchesNativeEdition(t *testing.T) {
	t.Parallel()
	edition, developerTools := BuildEdition()
	if edition != buildinfo.Edition ||
		developerTools != buildinfo.DeveloperTools {
		t.Fatalf(
			"embedded edition = %s/%v, native = %s/%v",
			edition,
			developerTools,
			buildinfo.Edition,
			buildinfo.DeveloperTools,
		)
	}
}

func TestHandlerServesIndexAndHistoryFallback(t *testing.T) {
	t.Parallel()
	handler := Handler()
	for _, requestPath := range []string{
		"/app/",
		"/app/settings",
		"/app/diagnostics/",
	} {
		requestPath := requestPath
		t.Run(requestPath, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			request.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%q", response.Code, response.Body)
			}
			if !strings.Contains(response.Body.String(), `id="app"`) {
				t.Fatalf("response did not contain SPA root")
			}
			if got := response.Header().Get("Content-Security-Policy"); got != appCSP {
				t.Fatalf("CSP = %q", got)
			}
		})
	}
}

func TestHandlerDoesNotFallbackForAPIsOrAssetTypos(t *testing.T) {
	t.Parallel()
	handler := Handler()
	tests := []struct {
		path   string
		accept string
	}{
		{path: "/status", accept: "text/html"},
		{path: "/app/assets/missing.js", accept: "text/html"},
		{path: "/app/settings", accept: "application/json"},
		{path: "/app//settings", accept: "text/html"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Accept", test.accept)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", test.path, response.Code)
		}
	}

	traversal := httptest.NewRequest(http.MethodGet, "/app/", nil)
	traversal.URL.Path = "/app/../status"
	traversal.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, traversal)
	if response.Code != http.StatusNotFound {
		t.Errorf("traversal status = %d, want 404", response.Code)
	}
}

func TestHandlerServesFingerprintWithImmutableCacheAndHead(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(content, "assets")
	if err != nil {
		t.Fatal(err)
	}
	var asset string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") {
			asset = "/app/assets/" + entry.Name()
			break
		}
	}
	if asset == "" {
		t.Fatal("compiled JavaScript asset not found")
	}

	handler := Handler()
	get := httptest.NewRequest(http.MethodGet, asset, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Errorf("Content-Type = %q", got)
	}
	tag := response.Header().Get("ETag")
	if tag == "" {
		t.Fatal("ETag is missing")
	}

	head := httptest.NewRequest(http.MethodHead, asset, nil)
	headResponse := httptest.NewRecorder()
	handler.ServeHTTP(headResponse, head)
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%d", headResponse.Code, headResponse.Body.Len())
	}

	cached := httptest.NewRequest(http.MethodGet, asset, nil)
	cached.Header.Set("If-None-Match", tag)
	cachedResponse := httptest.NewRecorder()
	handler.ServeHTTP(cachedResponse, cached)
	if cachedResponse.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d", cachedResponse.Code)
	}
}

func TestHandlerStreamsImmutableAssetsAndCachesETag(t *testing.T) {
	t.Parallel()
	const name = "index.html"
	want, err := fs.ReadFile(content, name)
	if err != nil {
		t.Fatal(err)
	}
	tracked := &trackingAssetFS{FS: content}
	handler := staticHandler{
		files:  tracked,
		source: newImmutableAssetSource(tracked),
	}

	request := httptest.NewRequest(http.MethodGet, "/app/"+name, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf(
			"streamed asset = status %d, exact=%v",
			response.Code,
			bytes.Equal(response.Body.Bytes(), want),
		)
	}
	sum := sha256.Sum256(want)
	wantTag := `"` + hex.EncodeToString(sum[:12]) + `"`
	if got := response.Header().Get("ETag"); got != wantTag {
		t.Fatalf("ETag = %q, want %q", got, wantTag)
	}
	readFileCalls, streamedBytes := tracked.counts()
	if readFileCalls != 0 {
		t.Fatalf("runtime asset serving called ReadFile %d times", readFileCalls)
	}
	if streamedBytes < int64(len(want))*2 {
		t.Fatalf(
			"first request did not hash and stream source: read=%d size=%d",
			streamedBytes,
			len(want),
		)
	}

	conditional := httptest.NewRequest(http.MethodGet, "/app/"+name, nil)
	conditional.Header.Set("If-None-Match", wantTag)
	cached := httptest.NewRecorder()
	handler.ServeHTTP(cached, conditional)
	if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
		t.Fatalf(
			"conditional response = status %d body %d",
			cached.Code,
			cached.Body.Len(),
		)
	}
	afterReadFileCalls, afterStreamedBytes := tracked.counts()
	if afterReadFileCalls != 0 {
		t.Fatalf(
			"conditional request called ReadFile %d times",
			afterReadFileCalls,
		)
	}
	if afterStreamedBytes != streamedBytes {
		t.Fatalf(
			"cached ETag reread asset bytes: before=%d after=%d",
			streamedBytes,
			afterStreamedBytes,
		)
	}
}

func TestHandlerSeekableStreamingPreservesByteRanges(t *testing.T) {
	t.Parallel()
	const name = "index.html"
	want, err := fs.ReadFile(content, name)
	if err != nil {
		t.Fatal(err)
	}
	tracked := &trackingAssetFS{FS: content}
	handler := staticHandler{
		files:  tracked,
		source: newImmutableAssetSource(tracked),
	}
	request := httptest.NewRequest(http.MethodGet, "/app/"+name, nil)
	request.Header.Set("Range", "bytes=7-31")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d", response.Code)
	}
	if got, expected := response.Body.Bytes(), want[7:32]; !bytes.Equal(got, expected) {
		t.Fatalf("range body exact = false: got %q want %q", got, expected)
	}
	if got := response.Header().Get("Content-Range"); got !=
		"bytes 7-31/"+strconv.Itoa(len(want)) {
		t.Fatalf("Content-Range = %q", got)
	}
}

func TestEmbeddedContentTypesAndFingerprints(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"index.html":            "text/html; charset=utf-8",
		"app.js":                "text/javascript; charset=utf-8",
		"app.css":               "text/css; charset=utf-8",
		"manifest.json":         "application/json; charset=utf-8",
		"app.webmanifest":       "application/manifest+json; charset=utf-8",
		"icon.svg":              "image/svg+xml",
		"icon.png":              "image/png",
		"icon.ico":              "image/x-icon",
		"sound.wav":             "audio/wav",
		"Vazirmatn-Regular.ttf": "font/ttf",
		"unknown.data":          "",
	} {
		if got := embeddedContentType(name); got != want {
			t.Errorf("content type for %s = %q, want %q", name, got, want)
		}
	}

	for name, want := range map[string]bool{
		"assets/index-Dou94rvi.js":                   true,
		"assets/workbox-window.prod.es5-Bd17z0YL.js": true,
		"assets/name-a_b-cD3fGhiJ.css":               true,
		"assets/name-short.js":                       false,
		"assets/name-abcdefgh.bad-extension!":        false,
		"assets/no-fingerprint.js":                   true,
		"assets/nohash.js":                           false,
		"assets/-abcdefgh.js":                        false,
		"assets/-name-abcdefgh.js":                   true,
		"assets/name-abcdefgh.js/another-plain.css":  false,
	} {
		if got := isFingerprintedName(name); got != want {
			t.Errorf("isFingerprintedName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFingerprintParserMatchesPreviousCachePolicy(t *testing.T) {
	t.Parallel()
	previous := regexp.MustCompile(
		`(?:^|/)[^/]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`,
	)
	err := fs.WalkDir(
		content,
		".",
		func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if got, want := isFingerprintedName(name),
				previous.MatchString(name); got != want {
				t.Errorf(
					"fingerprint policy for %q = %v, previous policy %v",
					name,
					got,
					want,
				)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

type trackingAssetFS struct {
	fs.FS

	mu            sync.Mutex
	readFileCalls int
	streamedBytes int64
}

func (tracked *trackingAssetFS) Open(name string) (fs.File, error) {
	file, err := tracked.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &trackingAssetFile{
		File:    file,
		tracked: tracked,
	}, nil
}

func (tracked *trackingAssetFS) ReadFile(name string) ([]byte, error) {
	tracked.mu.Lock()
	tracked.readFileCalls++
	tracked.mu.Unlock()
	return fs.ReadFile(tracked.FS, name)
}

func (tracked *trackingAssetFS) counts() (readFileCalls int, streamedBytes int64) {
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	return tracked.readFileCalls, tracked.streamedBytes
}

type trackingAssetFile struct {
	fs.File
	tracked *trackingAssetFS
}

func (file *trackingAssetFile) Read(buffer []byte) (int, error) {
	count, err := file.File.Read(buffer)
	file.tracked.mu.Lock()
	file.tracked.streamedBytes += int64(count)
	file.tracked.mu.Unlock()
	return count, err
}

func (file *trackingAssetFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := file.File.(interface {
		Seek(int64, int) (int64, error)
	})
	if !ok {
		return 0, fs.ErrInvalid
	}
	return seeker.Seek(offset, whence)
}

func TestHandlerCSPCompatibleIndexAndServiceWorker(t *testing.T) {
	t.Parallel()
	index, err := fs.ReadFile(content, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	scriptTags := regexp.MustCompile(`<script\b[^>]*>`).FindAllString(string(index), -1)
	if len(scriptTags) == 0 {
		t.Fatal("index contains no script entry")
	}
	for _, tag := range scriptTags {
		if !strings.Contains(tag, "src=") {
			t.Fatalf("inline script tag violates CSP: %q", tag)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/app/sw.js", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("service worker status = %d", response.Code)
	}
	if got := response.Header().Get("Service-Worker-Allowed"); got != "/app/" {
		t.Fatalf("Service-Worker-Allowed = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("service worker Cache-Control = %q", got)
	}
}

func TestHandlerServesCanonicalSharedSounds(t *testing.T) {
	t.Parallel()
	handler := Handler()
	for _, cue := range []string{"on", "off", "message", "error"} {
		request := httptest.NewRequest(
			http.MethodGet,
			"/app/shared/sfx/"+cue+".wav",
			nil,
		)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%q", cue, response.Code, response.Body)
		}
		if response.Header().Get("Content-Type") != "audio/wav" {
			t.Fatalf("%s Content-Type = %q", cue, response.Header().Get("Content-Type"))
		}
		if response.Body.Len() < 12 ||
			response.Body.String()[:4] != "RIFF" ||
			response.Body.String()[8:12] != "WAVE" {
			t.Fatalf("%s did not return a WAV", cue)
		}
	}

	missing := httptest.NewRequest(
		http.MethodGet,
		"/app/shared/sfx/unknown.wav",
		nil,
	)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown cue status = %d", missingResponse.Code)
	}
}

func TestHandlerReusesCanonicalProductIcon(t *testing.T) {
	t.Parallel()
	want, err := assets.Files.ReadFile("icons/app.png")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/app/shared/icons/app.png",
		nil,
	)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/png" ||
		!bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf(
			"canonical icon = status %d, type %q, exact=%v",
			response.Code,
			response.Header().Get("Content-Type"),
			bytes.Equal(response.Body.Bytes(), want),
		)
	}
}

func TestHandlerReusesCanonicalPersianFont(t *testing.T) {
	t.Parallel()
	want, err := assets.Files.ReadFile("fonts/Vazirmatn-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/app/shared/fonts/Vazirmatn-Regular.ttf",
		nil,
	)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "font/ttf" ||
		!bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf(
			"canonical Persian font = status %d, type %q, exact=%v",
			response.Code,
			response.Header().Get("Content-Type"),
			bytes.Equal(response.Body.Bytes(), want),
		)
	}
}

func TestDistributionDoesNotDuplicateCanonicalAssets(t *testing.T) {
	t.Parallel()
	if _, err := fs.ReadFile(content, "icons/app-512.png"); err == nil {
		t.Fatal("Web distribution duplicated the canonical 512px product icon")
	}
	err := fs.WalkDir(content, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(path.Ext(name), ".wav") {
			t.Errorf("Web distribution duplicated shared WAV %s", name)
		}
		if !entry.IsDir() && strings.EqualFold(path.Ext(name), ".ttf") {
			t.Errorf("Web distribution duplicated shared font %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fs.ReadFile(content, "manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte(`"src":"shared/icons/app.png"`)) {
		t.Fatalf("manifest does not reference the canonical product icon: %s", manifest)
	}
	serviceWorker, err := fs.ReadFile(content, "sw.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []string{
		"manifest.webmanifest",
		"icons/app-192.png",
		"icons/app-maskable-512.png",
		"shared/icons/app.png",
		"shared/fonts/Vazirmatn-Regular.ttf",
		"shared/sfx/on.wav",
		"shared/sfx/off.wav",
		"shared/sfx/message.wav",
		"shared/sfx/error.wav",
	} {
		if count := bytes.Count(serviceWorker, []byte(asset)); count != 1 {
			t.Errorf(
				"service worker references %s %d times, want exactly once",
				asset,
				count,
			)
		}
	}
}

func TestHandlerPreservesParentContentSecurityPolicy(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/app/", nil)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	const parentPolicy = "default-src 'none'; connect-src 'self' ws://127.0.0.1:47831"
	response.Header().Set("Content-Security-Policy", parentPolicy)
	Handler().ServeHTTP(response, request)
	if got := response.Header().Get("Content-Security-Policy"); got != parentPolicy {
		t.Fatalf("CSP = %q, want parent policy", got)
	}
}

func TestHandlerRedirectAndMethodBoundary(t *testing.T) {
	t.Parallel()
	get := httptest.NewRequest(http.MethodGet, "/app", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, get)
	if response.Code != http.StatusPermanentRedirect ||
		response.Header().Get("Location") != "/app/" {
		t.Fatalf("redirect status=%d location=%q",
			response.Code, response.Header().Get("Location"))
	}

	post := httptest.NewRequest(http.MethodPost, "/app/settings", strings.NewReader("{}"))
	postResponse := httptest.NewRecorder()
	Handler().ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", postResponse.Code)
	}
	if got := postResponse.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q", got)
	}
}
