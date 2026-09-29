//go:build !linux

package aptmirror

import (
	"io"
	"os"
)

func readBoundedSourceFile(path string, maximum int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, errSourceFileSymlink
	}
	if !before.Mode().IsRegular() {
		return nil, errSourceFileNonRegular
	}
	if before.Size() > maximum {
		return nil, errSourceFileTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errSourceFileNonRegular
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, errSourceFileTooLarge
	}
	return content, nil
}
