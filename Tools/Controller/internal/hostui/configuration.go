package hostui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenConfiguration accepts only the host's resolved configuration path, never
// an arbitrary browser-supplied path or command. Success means launch accepted.
func OpenConfiguration(file, target string) error {
	if !ConfigurationDesktopAvailable() {
		return errors.New("no interactive desktop session is available on the controller host")
	}
	var command *exec.Cmd
	switch target {
	case "file":
		if _, err := os.Stat(file); err != nil {
			return err
		}
	case "folder":
		file = filepath.Dir(file)
	default:
		return errors.New("configuration target must be file or folder")
	}
	switch runtime.GOOS {
	case "windows":
		if target == "file" {
			command = exec.Command("notepad.exe", file)
		} else {
			command = exec.Command("explorer.exe", file)
		}
	case "darwin":
		if target == "file" {
			command = exec.Command("open", "-t", file)
		} else {
			command = exec.Command("open", file)
		}
	default:
		command = exec.Command("xdg-open", file)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
