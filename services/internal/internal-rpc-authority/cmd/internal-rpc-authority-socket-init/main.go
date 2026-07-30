package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	socketRoot = "/run/mattercodex/internal-rpc-authority"
	socketUID  = 29000
	socketGID  = 29000
)

func main() {
	if err := prepareSocketRoot(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "internal-rpc-authority socket init failed: %v\n", err)
		os.Exit(1)
	}
}

func prepareSocketRoot() error {
	if os.Getuid() != socketUID || os.Getgid() != socketGID {
		return errors.New("socket init process uid/gid mismatch")
	}
	parent := filepath.Dir(socketRoot)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect socket parent: %w", err)
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("socket parent is not a real directory")
	}
	info, err := os.Lstat(socketRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(socketRoot, 0o770|os.ModeSticky); err != nil {
			return fmt.Errorf("create socket root: %w", err)
		}
	case err != nil:
		return fmt.Errorf("inspect socket root: %w", err)
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return errors.New("socket root is not a real directory")
	}
	if err := os.Chmod(socketRoot, 0o770|os.ModeSticky); err != nil {
		return fmt.Errorf("set socket root mode: %w", err)
	}
	if err := validateRoot(); err != nil {
		return err
	}
	for _, name := range []string{"issuer.sock", "verifier.sock"} {
		if err := removeStale(filepath.Join(socketRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

func validateRoot() error {
	info, err := os.Lstat(socketRoot)
	if err != nil {
		return fmt.Errorf("inspect prepared socket root: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok ||
		stat.Uid != socketUID ||
		stat.Gid != socketGID ||
		info.Mode().Perm() != 0o770 ||
		info.Mode()&os.ModeSticky == 0 {
		return errors.New("prepared socket root ownership or mode mismatch")
	}
	return nil
}

func removeStale(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect stale socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("socket path contains a symlink or non-socket")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}
