//go:build !windows && !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package pathguard

import "io/fs"

func trustedSystemSymlink(string, fs.FileInfo) (bool, error) { return false, nil }
