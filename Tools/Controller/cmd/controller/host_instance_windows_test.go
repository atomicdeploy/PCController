//go:build windows

package main

import (
	"testing"

	"pccontroller.local/controller/internal/productidentity"
)

func TestProductionHostMutexIsMachineWideAcrossUserKeys(t *testing.T) {
	first := windowsHostInstanceMutexName(productidentity.StableAppID + ".Host.user-a")
	second := windowsHostInstanceMutexName(productidentity.StableAppID + ".Host.user-b")
	if first != second || first != productidentity.StableAppID+".Host.Machine" {
		t.Fatalf("production mutexes differ: %q %q", first, second)
	}
	if got := windowsHostInstanceMutexName("test-isolated-lock"); got != "test-isolated-lock" {
		t.Fatalf("custom/test mutex changed: %q", got)
	}
}
