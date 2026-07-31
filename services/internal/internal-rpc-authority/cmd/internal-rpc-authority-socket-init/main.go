package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	socketRoot      = "/run/mattercodex/internal-rpc-authority"
	socketUID       = 29000
	socketGID       = 29000
	readinessSource = "/usr/local/bin/internal-rpc-authority-local-readiness"
	readinessTarget = "/run/mattercodex/internal-rpc-authority/local-readiness"
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
	return installReadinessProbe()
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

func installReadinessProbe() error {
	sourceInfo, err := os.Lstat(readinessSource)
	if err != nil ||
		!sourceInfo.Mode().IsRegular() ||
		sourceInfo.Mode()&os.ModeSymlink != 0 ||
		sourceInfo.Size() <= 0 ||
		sourceInfo.Size() > 64<<20 {
		return errors.New("local readiness source binary is unsafe")
	}
	if targetInfo, targetErr := os.Lstat(readinessTarget); targetErr == nil {
		if !targetInfo.Mode().IsRegular() ||
			targetInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("local readiness target is unsafe")
		}
		if err := os.Remove(readinessTarget); err != nil {
			return fmt.Errorf("remove stale local readiness binary: %w", err)
		}
	} else if !errors.Is(targetErr, os.ErrNotExist) {
		return fmt.Errorf("inspect local readiness target: %w", targetErr)
	}
	source, err := os.Open(readinessSource)
	if err != nil {
		return fmt.Errorf("open local readiness source: %w", err)
	}
	defer source.Close()
	temporary := readinessTarget + ".starting"
	target, err := os.OpenFile(
		temporary,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o550,
	)
	if err != nil {
		return fmt.Errorf("create local readiness target: %w", err)
	}
	if _, err := target.ReadFrom(source); err != nil {
		_ = target.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("copy local readiness binary: %w", err)
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("sync local readiness binary: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close local readiness binary: %w", err)
	}
	if err := os.Rename(temporary, readinessTarget); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish local readiness binary: %w", err)
	}
	return nil
}
