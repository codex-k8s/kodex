package security

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var errWorkspaceInputUnsafe = errors.New("workspace input tree is unsafe")

// ProtectWorkspaceInputTree защищает потомков через открытые дескрипторы.
// Только точный корень тома kubelet сохраняет исходный mode: рабочие consumers
// получают весь том readOnly по controller Pod ABI.
func ProtectWorkspaceInputTree(root, relative string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || (relative != "input" && relative != "knowledge") {
		return errors.New("workspace input tree is invalid")
	}
	parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkspaceInputUnsafe
	}
	defer unix.Close(parent)
	return protectInputEntry(parent, relative, root == "/workspace", relative)
}

func protectInputEntry(parent int, name string, volumeRoot bool, relative string) error {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkspaceInputUnsafe
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil {
		return errWorkspaceInputUnsafe
	}
	directory := stat.Mode&unix.S_IFMT == unix.S_IFDIR
	exempt := volumeRoot && isWorkspaceVolumeRoot(relative, stat)
	if (relative != "" && !directory) || (!directory && (stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1)) ||
		(stat.Uid != uint32(os.Geteuid()) && !exempt) {
		return errWorkspaceInputUnsafe
	}
	if directory {
		entries, readErr := file.ReadDir(-1)
		if readErr != nil {
			return errWorkspaceInputUnsafe
		}
		for _, entry := range entries {
			if err := protectInputEntry(fd, entry.Name(), false, ""); err != nil {
				return err
			}
		}
	}
	if !exempt {
		mode := uint32(0o440)
		if directory {
			mode = 0o2750
		}
		if unix.Fchmod(fd, mode) != nil {
			return errors.New("protect workspace input tree")
		}
	}
	return nil
}
