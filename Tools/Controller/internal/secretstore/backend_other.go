//go:build !windows && !linux

package secretstore

func newPlatformBackend(string) Backend { return unavailableBackend{} }
