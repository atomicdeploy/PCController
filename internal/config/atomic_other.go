//go:build !windows

package config

import "os"

func atomicReplace(source, destination string) error {
	return os.Rename(source, destination)
}

func secureFile(path string) error {
	return os.Chmod(path, 0o600)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
