// Package api embeds the generated, offline-capable API explorer and contracts.
package api

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed *.json *.html assets THIRD_PARTY_NOTICES.md
var files embed.FS

// Handler serves only embedded regular assets, never a directory or filesystem override.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/docs/")
		if name == "" {
			name = "reference.html"
		}
		if path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(files, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		data, err := files.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Scalar injects its local styles. All network calls are blocked: this is
		// a browse/copy reference, not a way around hardware admission or CORS.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; worker-src 'none'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		if kind := mime.TypeByExtension(path.Ext(name)); kind != "" {
			w.Header().Set("Content-Type", kind)
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
