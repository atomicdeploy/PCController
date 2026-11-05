//go:build windows

package ui

import (
	"image"
	"testing"
)

func TestForegroundRecoveryOwnsReusableSizeAndDPISurfaces(t *testing.T) {
	recovery, err := newForegroundRecovery(23, 17, 96)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Matches(23, 17, 96) {
		t.Fatal("new recovery does not match its size and DPI")
	}
	if recovery.Matches(24, 17, 96) ||
		recovery.Matches(23, 18, 96) ||
		recovery.Matches(23, 17, 120) {
		t.Fatal("recovery matched an incompatible size or DPI")
	}
	if recovery.black == recovery.white ||
		recovery.black == recovery.recovered ||
		recovery.white == recovery.recovered {
		t.Fatal("recovery pixel surfaces unexpectedly alias")
	}
	recovery.Dispose()
	if recovery.blackBitmap != nil || recovery.whiteBitmap != nil ||
		recovery.black != nil || recovery.white != nil ||
		recovery.recovered != nil {
		t.Fatal("Dispose retained a recovery resource")
	}
	// Disposal is deliberately idempotent for partial-construction failures
	// and shell shutdown.
	recovery.Dispose()
}

func TestReleaseHiddenRenderResourcesDropsCPUSurfaces(t *testing.T) {
	recovery, err := newForegroundRecovery(11, 9, 96)
	if err != nil {
		t.Fatal(err)
	}
	shell := &Shell{
		foregroundRecovery: recovery,
		glassFrame:         image.NewRGBA(image.Rect(0, 0, 11, 9)),
		hitSurface:         recovery.recovered,
		presentedSurface:   recovery.recovered,
	}
	shell.releaseHiddenRenderResources()
	if shell.foregroundRecovery != nil ||
		shell.glassFrame != nil ||
		shell.hitSurface != nil ||
		shell.presentedSurface != nil {
		t.Fatal("hidden shell retained a render surface")
	}
	if recovery.blackBitmap != nil || recovery.whiteBitmap != nil ||
		recovery.black != nil || recovery.white != nil ||
		recovery.recovered != nil {
		t.Fatal("hidden shell did not dispose its recovery resources")
	}
}
