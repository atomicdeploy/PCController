//go:build windows

package artifacts

import "golang.org/x/sys/windows"

func setUpdateHidden(path string, hidden bool) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if hidden {
		attributes |= windows.FILE_ATTRIBUTE_HIDDEN
	} else {
		attributes &^= windows.FILE_ATTRIBUTE_HIDDEN
	}
	return windows.SetFileAttributes(name, attributes)
}
