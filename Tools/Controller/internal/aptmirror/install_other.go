//go:build !linux

package aptmirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const maximumControllerExecutableBytes = 512 << 20

func readPinnedExecutable(path string, requireTrusted bool) ([]byte, error) {
	if requireTrusted {
		return nil, errors.New("APT mirror profile --apply is supported only on Linux")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumControllerExecutableBytes {
		return nil, errors.New("current Controller executable must be a bounded regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumControllerExecutableBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read pinned Controller executable: %w", err)
	}
	if len(content) > maximumControllerExecutableBytes {
		return nil, errors.New("current Controller executable grew beyond the bounded installer limit")
	}
	return content, nil
}

func selfTestUnattendedUpgradeShim(context.Context, string) error {
	return errors.New("unattended-upgrade compatibility is supported only on Linux")
}

func validateMirrorApply([]string, string) error {
	return errors.New("APT mirror profile --apply is supported only on Linux")
}
