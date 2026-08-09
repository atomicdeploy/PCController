//go:build linux

package portowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type linuxEnumerator struct {
	procRoot string
}

const maxLinuxPID = uint32(1<<31 - 1)

func systemEnumerator() Enumerator { return linuxEnumerator{procRoot: "/proc"} }

func isAccessDenied(cause error) bool {
	return looksAccessDenied(cause) || errors.Is(cause, syscall.EBUSY) || errors.Is(cause, syscall.EACCES)
}

func isLocalSerialTarget(value string) bool {
	if isCOMPort(value) {
		return true
	}
	_, err := linuxSerialPath(value)
	return err == nil
}

func linuxSerialPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "tcp://") {
		return "", errors.New("serial device path is empty or remote")
	}
	if !strings.ContainsRune(value, os.PathSeparator) {
		value = filepath.Join("/dev", value)
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	if _, _, err := linuxSerialDirectoryEntry(path); err != nil {
		return "", err
	}
	return path, nil
}

// linuxSerialDirectoryEntry reduces an accepted path to one of the fixed
// Linux serial directories plus a single entry name. The user-provided path is
// used only for equality against names returned by ReadDir; it is never passed
// to a filesystem operation.
func linuxSerialDirectoryEntry(path string) (string, string, error) {
	relative, err := filepath.Rel("/dev", path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("serial-owner lookup is restricted to /dev")
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	if len(parts) == 1 && (strings.HasPrefix(parts[0], "tty") || strings.HasPrefix(parts[0], "rfcomm")) {
		return "/dev", parts[0], nil
	}
	if len(parts) == 3 && parts[0] == "serial" &&
		(parts[1] == "by-id" || parts[1] == "by-path") && parts[2] != "" {
		return filepath.Join("/dev", "serial", parts[1]), parts[2], nil
	}
	return "", "", fmt.Errorf("%s is not a recognized Linux serial-device path", path)
}

func statLinuxSerialTarget(path string) (os.FileInfo, error) {
	directory, name, err := linuxSerialDirectoryEntry(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() != name {
			continue
		}
		info, statErr := os.Stat(filepath.Join(directory, entry.Name()))
		if statErr != nil {
			return nil, statErr
		}
		return info, nil
	}
	return nil, os.ErrNotExist
}

func (enumerator linuxEnumerator) FindOwner(ctx context.Context, port string) (Owner, bool, error) {
	if err := ctx.Err(); err != nil {
		return Owner{}, false, err
	}
	target, err := linuxSerialPath(port)
	if err != nil {
		return Owner{}, false, err
	}
	procRoot := enumerator.procRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	return findLinuxOwner(ctx, procRoot, target)
}

func findLinuxOwner(ctx context.Context, procRoot, target string) (Owner, bool, error) {
	targetInfo, err := statLinuxSerialTarget(target)
	if err != nil {
		return Owner{}, false, fmt.Errorf("inspect serial device %s: %w", target, err)
	}
	return findLinuxOwnerByFileInfo(ctx, procRoot, targetInfo)
}

func findLinuxOwnerByFileInfo(ctx context.Context, procRoot string, targetInfo os.FileInfo) (Owner, bool, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return Owner{}, false, fmt.Errorf("enumerate Linux processes: %w", err)
	}
	var pids []uint32
	for _, entry := range entries {
		pid, parseErr := strconv.ParseUint(entry.Name(), 10, 31)
		if parseErr == nil && pid > 0 && entry.IsDir() {
			pids = append(pids, uint32(pid))
		}
	}
	sort.Slice(pids, func(left, right int) bool { return pids[left] < pids[right] })
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return Owner{}, false, err
		}
		pidText := strconv.FormatUint(uint64(pid), 10)
		fds, readErr := os.ReadDir(filepath.Join(procRoot, pidText, "fd"))
		if readErr != nil {
			continue
		}
		for _, fd := range fds {
			fdInfo, statErr := os.Stat(filepath.Join(procRoot, pidText, "fd", fd.Name()))
			if statErr != nil || !os.SameFile(targetInfo, fdInfo) {
				continue
			}
			return linuxOwner(procRoot, pid), true, nil
		}
	}
	return Owner{}, false, nil
}

func linuxOwner(procRoot string, pid uint32) Owner {
	processDirectory := filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10))
	nameBytes, _ := os.ReadFile(filepath.Join(processDirectory, "comm"))
	name := strings.TrimSpace(strings.ToValidUTF8(string(nameBytes), ""))
	if len(name) > 256 {
		name = name[:256]
	}
	executable, _ := os.Readlink(filepath.Join(processDirectory, "exe"))
	startTime, _ := linuxProcessStartTime(procRoot, pid)
	return Owner{PID: pid, Name: name, Executable: executable, ProcessStartTime: startTime}
}

// linuxProcessStartTime returns Unix time in 100ns units. Linux exposes process
// starts as USER_HZ ticks since boot; /proc uses USER_HZ=100 on supported Go
// Linux targets irrespective of the kernel scheduler frequency.
func linuxProcessStartTime(procRoot string, pid uint32) (uint64, error) {
	stat, err := os.ReadFile(filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10), "stat"))
	if err != nil {
		return 0, err
	}
	closing := strings.LastIndexByte(string(stat), ')')
	if closing < 0 {
		return 0, errors.New("process stat has no command terminator")
	}
	fields := strings.Fields(string(stat[closing+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("process stat has no start-time field")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, err
	}
	bootStat, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return 0, err
	}
	var bootSeconds uint64
	for _, line := range strings.Split(string(bootStat), "\n") {
		if strings.HasPrefix(line, "btime ") {
			bootSeconds, err = strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
			break
		}
	}
	if err != nil || bootSeconds == 0 {
		return 0, errors.New("/proc/stat has no valid boot time")
	}
	return bootSeconds*10_000_000 + ticks*100_000, nil
}

type linuxActions struct {
	procRoot        string
	pidfdOpen       func(int, int) (int, error)
	pidfdSendSignal func(int, unix.Signal, *unix.Siginfo, int) error
	close           func(int) error
}

func DefaultActions() Actions {
	return linuxActions{
		procRoot:        "/proc",
		pidfdOpen:       unix.PidfdOpen,
		pidfdSendSignal: unix.PidfdSendSignal,
		close:           unix.Close,
	}
}

func (linuxActions) BringToForeground(context.Context, Owner) error {
	return errors.New("serial-owner foreground activation is unavailable on Linux: Wayland does not expose a safe PID-to-window activation API")
}

func (actions linuxActions) RequestGracefulClose(ctx context.Context, owner Owner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentExecutable, _ := os.Executable()
	if owner.PID == uint32(os.Getpid()) || sameExecutable(owner, currentExecutable) {
		return errors.New("refusing to close the current controller process")
	}
	return actions.send(ctx, owner, unix.SIGTERM, "request graceful close")
}

func (actions linuxActions) Terminate(ctx context.Context, owner Owner, confirmation string) error {
	currentExecutable, _ := os.Executable()
	if err := validateTermination(owner, confirmation, uint32(os.Getpid()), currentExecutable); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return actions.send(ctx, owner, unix.SIGKILL, "terminate")
}

func (linuxActions) TerminateConfirmation(owner Owner) string {
	return terminationConfirmation(owner)
}

func (actions linuxActions) verify(owner Owner) error {
	if owner.PID == 0 {
		return errors.New("serial owner has no PID")
	}
	if owner.ProcessStartTime == 0 && owner.Executable == "" {
		return errors.New("refusing action: serial owner has no stable start-time or executable identity")
	}
	procRoot := actions.procRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	pid := owner.PID
	if owner.ProcessStartTime != 0 {
		startTime, err := linuxProcessStartTime(procRoot, pid)
		if err != nil {
			return fmt.Errorf("verify %s start time: %w", owner.Label(), err)
		}
		if startTime != owner.ProcessStartTime {
			return fmt.Errorf("refusing action: PID %d no longer identifies the diagnosed serial owner", owner.PID)
		}
	}
	if owner.Executable != "" {
		executable, err := os.Readlink(filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10), "exe"))
		if err != nil {
			return fmt.Errorf("verify %s executable: %w", owner.Label(), err)
		}
		if filepath.Clean(executable) != filepath.Clean(owner.Executable) {
			return fmt.Errorf("refusing action: executable identity for PID %d changed", owner.PID)
		}
	}
	return nil
}

func (actions linuxActions) send(ctx context.Context, owner Owner, signal unix.Signal, operation string) error {
	if owner.PID == 0 {
		return errors.New("serial owner has no PID")
	}
	if owner.PID > maxLinuxPID {
		return fmt.Errorf("serial owner PID %d exceeds the Linux process range", owner.PID)
	}
	open := actions.pidfdOpen
	if open == nil {
		open = unix.PidfdOpen
	}
	closeFD := actions.close
	if closeFD == nil {
		closeFD = unix.Close
	}
	pidfd, err := open(int(owner.PID), 0)
	if err != nil {
		return fmt.Errorf("pin %s with pidfd_open: %w", owner.Label(), err)
	}
	closeWith := func(cause error) error {
		if closeErr := closeFD(pidfd); closeErr != nil {
			return errors.Join(cause, fmt.Errorf("close pidfd for %s: %w", owner.Label(), closeErr))
		}
		return cause
	}

	// Opening the pidfd first pins the exact process. Identity is checked after
	// the pin, and the signal is sent through that descriptor, so PID reuse can
	// never redirect the requested action to a later unrelated process.
	if err := actions.verify(owner); err != nil {
		return closeWith(err)
	}
	if err := ctx.Err(); err != nil {
		return closeWith(err)
	}
	send := actions.pidfdSendSignal
	if send == nil {
		send = unix.PidfdSendSignal
	}
	if err := send(pidfd, signal, nil, 0); err != nil {
		return closeWith(fmt.Errorf("%s %s through pidfd: %w", operation, owner.Label(), err))
	}
	return closeWith(nil)
}
