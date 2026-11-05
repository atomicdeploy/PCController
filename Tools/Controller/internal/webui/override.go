package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"time"
)

// Editable assets must not be masked by the bundled PWA's cache-first worker.
// A user-supplied service-worker.js can explicitly replace this network-only
// worker, taking responsibility for its own cache policy.
const overrideServiceWorker = `self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil((async () => {
  await Promise.all((await caches.keys()).filter(name => name.startsWith('pccontroller-')).map(name => caches.delete(name)));
  await self.clients.claim();
})()));
self.addEventListener('fetch', event => {
  if (event.request.method === 'GET' && new URL(event.request.url).origin === self.location.origin) {
    event.respondWith(fetch(event.request, {cache: 'no-cache'}));
  }
});
`

// serveOverride returns false only when the override path does not exist.
// os.Root confines all opens, including symlink resolution, to the public UI
// directory. No directory listings, runtime configuration or API routes are
// exposed by this overlay.
func (handler *staticHandler) serveOverride(writer http.ResponseWriter, request *http.Request, name string) bool {
	if handler.overrideDirectory == "" {
		return false
	}
	root, err := os.OpenRoot(handler.overrideDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		http.Error(writer, "open Web UI override", http.StatusInternalServerError)
		return true
	}
	defer root.Close()
	file, err := root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		http.NotFound(writer, request)
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(writer, request)
		return true
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		http.Error(writer, "read Web UI override", http.StatusInternalServerError)
		return true
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(writer, "read Web UI override", http.StatusInternalServerError)
		return true
	}
	writer.Header().Set("Content-Type", contentType(name))
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("ETag", `"`+hex.EncodeToString(digest.Sum(nil))+`"`)
	// ETags track bytes even for same-size edits inside a filesystem timestamp
	// tick. Avoid timestamp-only 304 responses for locally editable files.
	http.ServeContent(writer, request, path.Base(name), time.Time{}, file)
	return true
}
