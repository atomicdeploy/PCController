//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func DefaultNativeEndpoint(appID string) (Endpoint, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return Endpoint{}, errors.New("application identity is required")
	}
	runtimeRoot := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeRoot == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil {
			return Endpoint{}, err
		}
		runtimeRoot = filepath.Join(configRoot, "pccontroller-runtime")
	} else {
		runtimeRoot = filepath.Join(runtimeRoot, "pccontroller")
	}
	digest := sha256.Sum256([]byte(appID + "\x00" + fmt.Sprint(os.Geteuid())))
	return Endpoint{
		Transport: TransportUnix,
		Address: filepath.Join(
			runtimeRoot,
			"pccontroller-"+hex.EncodeToString(digest[:8])+".sock",
		),
		Scope:    "local",
		Priority: 10,
	}, nil
}

func dialNativeEndpoint(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Transport != TransportUnix {
		return nil, errors.New("Windows named pipes are unavailable on this platform")
	}
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, "unix", endpoint.Address)
}

func listenNativeEndpoint(endpoint Endpoint, options ListenOptions) (net.Listener, error) {
	if endpoint.Transport != TransportUnix {
		return nil, errors.New("Windows named pipes are unavailable on this platform")
	}
	path, err := filepath.Abs(strings.TrimSpace(endpoint.Address))
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create RPC socket directory: %w", err)
	}
	if err := validateSocketDirectory(parent); err != nil {
		return nil, err
	}
	var ownership *os.File
	if options.RecoverStaleNative {
		ownership, err = acquireSocketOwnership(path + ".lock")
		if err != nil {
			return nil, err
		}
	}
	releaseOwnership := func() {
		if ownership != nil {
			_ = syscall.Flock(int(ownership.Fd()), syscall.LOCK_UN)
			_ = ownership.Close()
		}
	}
	if err := prepareSocketPath(path, options.RecoverStaleNative); err != nil {
		releaseOwnership()
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		releaseOwnership()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		releaseOwnership()
		return nil, fmt.Errorf("secure RPC socket: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		releaseOwnership()
		return nil, err
	}
	return &ownedUnixListener{
		UnixListener: listener, path: path, identity: info, ownership: ownership,
	}, nil
}

func acquireSocketOwnership(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open RPC ownership lock: %w", err)
	}
	closeWithError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeWithError(err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return closeWithError(err)
	}
	stat, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || !opened.Mode().IsRegular() || opened.Mode()&os.ModeSymlink != 0 ||
		current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
		return closeWithError(errors.New("RPC ownership lock is not a stable regular file"))
	}
	if int(stat.Uid) != os.Geteuid() || opened.Mode().Perm()&0o077 != 0 {
		return closeWithError(errors.New("RPC ownership lock must be private and owned by the current user"))
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return closeWithError(fmt.Errorf("RPC native endpoint is already owned: %w", err))
	}
	return file, nil
}

func validateSocketDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("RPC socket parent is not a real directory")
	}
	if int(stat.Uid) != os.Geteuid() {
		return errors.New("RPC socket parent is not owned by the current user")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("RPC socket parent permissions must exclude group and other users")
	}
	return nil
}

func prepareSocketPath(path string, recoverStale bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("RPC socket path exists and is not a socket")
	}
	probe, probeErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if probeErr == nil {
		_ = probe.Close()
		return errors.New("RPC socket is already live")
	}
	if !recoverStale {
		return fmt.Errorf("RPC socket is unresponsive; stale recovery requires the ownership lock: %w", probeErr)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("recheck stale RPC socket: %w", err)
	}
	if !os.SameFile(info, current) {
		return errors.New("RPC socket identity changed during stale recovery")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale RPC socket: %w", err)
	}
	return nil
}

type ownedUnixListener struct {
	*net.UnixListener
	path      string
	identity  os.FileInfo
	ownership *os.File
	closeOnce sync.Once
	closeErr  error
}

func (listener *ownedUnixListener) Close() error {
	if listener == nil || listener.UnixListener == nil {
		return nil
	}
	listener.closeOnce.Do(func() {
		listener.closeErr = listener.UnixListener.Close()
		info, statErr := os.Lstat(listener.path)
		if statErr == nil && os.SameFile(listener.identity, info) {
			listener.closeErr = errors.Join(listener.closeErr, os.Remove(listener.path))
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			listener.closeErr = errors.Join(listener.closeErr, statErr)
		}
		if listener.ownership != nil {
			listener.closeErr = errors.Join(
				listener.closeErr,
				syscall.Flock(int(listener.ownership.Fd()), syscall.LOCK_UN),
				listener.ownership.Close(),
			)
		}
	})
	return listener.closeErr
}
