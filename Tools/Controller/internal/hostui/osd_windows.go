//go:build windows

package hostui

import (
	"errors"
	"image"
	"image/png"
	"os"
)

func activateOSDAction(uri string) error { return openWindowsURI(uri) }
func loadOSDIcon(name string) (image.Image, error) {
	if name == "success" {
		name = NotificationIconTemperatureCleared
	}
	asset := ToastLogoFileName
	if name != "" && name != "app" {
		asset = toastEventIconFileName(name)
	}
	path, err := resolveToastAsset("", asset)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || isWindowsReparsePoint(info) || info.Size() > 1<<20 {
		return nil, errors.New("OSD icon must be a regular local PNG smaller than 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	config, err := png.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if config.Width > 1024 || config.Height > 1024 {
		return nil, errors.New("OSD icon exceeds 1024 pixels")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, err
	}
	return png.Decode(f)
}
