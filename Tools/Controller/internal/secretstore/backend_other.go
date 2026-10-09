//go:build !windows

package secretstore

func newPlatformBackend(string) Backend { return unavailableBackend{} }
