package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type ownedUnixListener struct {
	*net.UnixListener
	path string
}

func listenUnix(config Config) (*ownedUnixListener, error) {
	if err := validateSocketRoot(filepath.Dir(config.SocketPath)); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(config.SocketPath); err != nil {
		return nil, err
	}
	temporaryPath := fmt.Sprintf("%s.starting-%d", config.SocketPath, os.Getpid())
	if err := removeKnownTemporarySocket(temporaryPath); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: temporaryPath,
		Net:  "unix",
	})
	if err != nil {
		return nil, fmt.Errorf("listen on temporary authority UDS: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	cleanup := func() {
		_ = listener.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := os.Chmod(temporaryPath, config.SocketMode); err != nil {
		cleanup()
		return nil, fmt.Errorf("set authority UDS mode: %w", err)
	}
	if err := validateSocketIdentity(
		temporaryPath,
		config.ExpectedProcessUID,
		config.ExpectedProcessGID,
		config.SocketMode,
	); err != nil {
		cleanup()
		return nil, err
	}
	if err := os.Rename(temporaryPath, config.SocketPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("publish authority UDS atomically: %w", err)
	}
	return &ownedUnixListener{UnixListener: listener, path: config.SocketPath}, nil
}

func (listener *ownedUnixListener) Close() error {
	closeErr := listener.UnixListener.Close()
	removeErr := os.Remove(listener.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return errors.Join(closeErr, fmt.Errorf("remove authority UDS: %w", removeErr))
	}
	return closeErr
}

func validateSocketRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect authority UDS root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("authority UDS root is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		stat.Uid != 29000 ||
		stat.Gid != 29000 ||
		info.Mode().Perm() != 0o770 ||
		info.Mode()&os.ModeSticky == 0 {
		return errors.New("authority UDS root ownership or mode mismatch")
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing authority UDS: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("existing authority UDS path is a symlink or non-socket")
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("authority UDS is already active")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale authority UDS: %w", err)
	}
	return nil
}

func removeKnownTemporarySocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect temporary authority UDS: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("temporary authority UDS path is unsafe")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove temporary authority UDS: %w", err)
	}
	return nil
}

func validateSocketIdentity(
	path string,
	expectedUID uint32,
	expectedGID uint32,
	expectedMode os.FileMode,
) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect created authority UDS: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 ||
		stat.Uid != expectedUID ||
		stat.Gid != expectedGID ||
		info.Mode().Perm() != expectedMode.Perm() {
		return errors.New("created authority UDS ownership, type or mode mismatch")
	}
	return nil
}
