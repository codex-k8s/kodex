// Package security проверяет локальную границу immutable artifacts.
package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const protectedExecutable = "/usr/local/bin/kodex-agent-runner"

func VerifyInvocation(args []string, mode string) error {
	if len(args) != 2 || args[0] != protectedExecutable || args[1] != mode {
		return errors.New("protected agent-runner invocation is invalid")
	}
	executable, err := os.Executable()
	if err != nil || filepath.Clean(executable) != protectedExecutable {
		return errors.New("resolve protected agent-runner executable")
	}
	return VerifyProtectedRegular(protectedExecutable, true)
}

func VerifyProtectedRegular(path string, executable bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("protected artifact path is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("protected artifact is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || (stat.Gid != 0 && stat.Gid != 29000) || stat.Nlink != 1 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("protected artifact ownership is invalid")
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return errors.New("protected executable mode is invalid")
	}
	return verifyParents(path)
}

func verifyParents(path string) error {
	for current := filepath.Dir(path); current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("protected artifact parent is unsafe")
		}
	}
	return nil
}

func EnsureWorkspaceDirectory(relative string) error {
	return ensureWorkspaceDirectory(relative, 0o700, false)
}

func EnsureSharedWorkspaceDirectory(relative string) error {
	return ensureWorkspaceDirectory(relative, 0o2770, true)
}

func ensureWorkspaceDirectory(relative string, mode uint32, shared bool) error {
	clean := filepath.Clean(relative)
	if clean != relative || filepath.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return errors.New("workspace directory path is invalid")
	}
	current, err := unix.Open("/workspace", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("open workspace directory")
	}
	defer func() { _ = unix.Close(current) }()
	traversed := ""
	for _, part := range strings.Split(clean, string(os.PathSeparator)) {
		traversed = filepath.Join(traversed, part)
		if part == "" || part == "." || part == ".." {
			return errors.New("workspace directory component is invalid")
		}
		if err := unix.Mkdirat(current, part, mode); err != nil && !errors.Is(err, syscall.EEXIST) {
			return errors.New("create workspace directory")
		}
		next, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return errors.New("open workspace directory component")
		}
		var stat unix.Stat_t
		if unix.Fstat(next, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			unix.Close(next)
			return errors.New("workspace directory component is unsafe")
		}
		owned := stat.Uid == uint32(os.Geteuid())
		volumeRoot := shared && isWorkspaceVolumeRoot(traversed, stat)
		if !owned && !volumeRoot {
			unix.Close(next)
			return errors.New("workspace directory component is unsafe")
		}
		if shared {
			if owned && (stat.Gid != 29000 || stat.Mode&0o7777 != mode) && (unix.Fchown(next, -1, 29000) != nil || unix.Fchmod(next, mode) != nil) {
				unix.Close(next)
				return errors.New("protect shared workspace directory")
			}
		} else if !owned || stat.Mode&0o077 != 0 {
			unix.Close(next)
			return errors.New("workspace directory component is unsafe")
		}
		unix.Close(current)
		current = next
	}
	return nil
}

// Kubelet оставляет у emptyDir исходные other bits (по умолчанию 0777).
// Исключение относится только к точным корням томов из controller Pod ABI.
func isWorkspaceVolumeRoot(relative string, stat unix.Stat_t) bool {
	return (relative == "input" || relative == "knowledge" || relative == ".kodex/state") &&
		stat.Uid == 0 && stat.Gid == 29000 && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o070 == 0o070
}
