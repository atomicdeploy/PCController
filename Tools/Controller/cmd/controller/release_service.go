package main

import (
	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/releaseplane"
)

func newReleaseHostService(
	client *controllerapi.Client,
	artifactService *artifacts.Service,
) (*releaseplane.Service, error) {
	var events releaseplane.EventSink
	if client != nil {
		events = func(kind, text string, metadata map[string]string) {
			client.EmitHostActionEvent(kind, text, "release-discovery", "artifact.stage", metadata)
		}
	}
	return releaseplane.NewService(nil, artifactService, events)
}
