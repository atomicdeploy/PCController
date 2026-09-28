//go:build !windows && !linux

package main

import (
	"context"

	"pccontroller.local/controller/internal/hostui"
)

type unavailableVisibleSurfacePlatform struct{}

func newVisibleSurfacePlatform() visibleSurfacePlatform {
	return unavailableVisibleSurfacePlatform{}
}

func (unavailableVisibleSurfacePlatform) Start(
	context.Context,
	visibleSurfaceSpec,
) visibleSurfaceStart {
	return visibleSurfaceStart{Reason: "visible application surface launch is unavailable on this platform"}
}

func (unavailableVisibleSurfacePlatform) Focus(
	context.Context,
	visibleSurfaceSpec,
	hostui.AppInstance,
) visibleSurfaceStart {
	return visibleSurfaceStart{Reason: "visible application surface focus is unavailable on this platform"}
}
