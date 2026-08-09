//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package pathguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// trustedSystemSymlink recognizes only a stable operating-system alias: the
// link, its parent, and its resolved directory target must all be root-owned,
// and the parent must not be writable by group or other users. This permits
// standard aliases such as macOS /var -> /private/var without allowing a user
// to redirect installer or purge operations through a link they control.
func trustedSystemSymlink(path string, info fs.FileInfo) (bool, error) {
	linkStat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || linkStat.Uid != 0 {
		return false, nil
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != 0 || parent.Mode().Perm()&0o022 != 0 {
		return false, nil
	}
	target, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	targetStat, ok := target.Sys().(*syscall.Stat_t)
	return ok && targetStat.Uid == 0 && target.IsDir(), nil
}
