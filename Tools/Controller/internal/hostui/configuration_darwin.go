package hostui

import (
	"os"
	"syscall"
)

// loginwindow assigns /dev/console to the active GUI user. Root/loginwindow,
// another user's session and a headless host are not this process's desktop.
// Do not launch subprocess probes during periodic UI snapshot collection.
func ConfigurationDesktopAvailable() bool {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && configurationConsoleUserMatches(stat.Uid, os.Geteuid())
}

func configurationConsoleUserMatches(owner uint32, user int) bool {
	return user > 0 && uint32(user) == owner
}
