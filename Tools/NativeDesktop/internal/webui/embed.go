// Package webui serves the compiled, bundled Rayan Lamp SPA/PWA.
package webui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"rayanlamp.local/app/internal/buildinfo"
)

// distribution contains only generated browser artifacts. Frontend source and
// node_modules are never linked into RayanLamp.exe.
//
//go:embed all:dist
var distribution embed.FS

var content = mustSub(distribution, "dist")
var embeddedEdition = mustEmbeddedEdition(content)

type editionMarker struct {
	Edition        string `json:"edition"`
	DeveloperTools bool   `json:"developer_tools"`
}

func mustSub(source fs.FS, directory string) fs.FS {
	sub, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}
	return sub
}

func mustEmbeddedEdition(source fs.FS) editionMarker {
	data, err := fs.ReadFile(source, "build-edition.json")
	if err != nil {
		panic(fmt.Errorf("webui: read embedded build edition: %w", err))
	}
	var marker editionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		panic(fmt.Errorf("webui: parse embedded build edition: %w", err))
	}
	if marker.Edition != buildinfo.Edition ||
		marker.DeveloperTools != buildinfo.DeveloperTools {
		panic(fmt.Errorf(
			"webui: embedded %s edition (developer_tools=%v) does not match Go %s edition (developer_tools=%v); rebuild both through the supported pipeline",
			marker.Edition,
			marker.DeveloperTools,
			buildinfo.Edition,
			buildinfo.DeveloperTools,
		))
	}
	return marker
}

// BuildEdition reports the immutable embedded browser edition. Package
// initialization rejects stale mixed artifacts before an HTTP listener can
// serve them.
func BuildEdition() (string, bool) {
	return embeddedEdition.Edition, embeddedEdition.DeveloperTools
}
