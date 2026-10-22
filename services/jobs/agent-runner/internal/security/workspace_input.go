package security

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// errWorkspaceInputUnsafe is the shared fixed failure for unsafe input-tree traversal, file type, ownership or descriptor operations without revealing a rejected path.
// errWorkspaceInputUnsafe задаёт общий фиксированный отказ для небезопасного обхода входного дерева, типа файла, владельца или операции дескриптора без раскрытия отклонённого пути.
var errWorkspaceInputUnsafe = errors.New("workspace input tree is unsafe")

// ProtectWorkspaceInputTree opens a clean absolute parent and protects only its input or knowledge subtree through child descriptors.
// Only recognized volume-root metadata under exact /workspace keeps its original mode; descendants are changed by the recursive helper.
// This changes permissions, not mount readOnly state, and does not read document contents.
// ProtectWorkspaceInputTree открывает нормализованного абсолютного родителя и защищает только его поддерево input либо knowledge через дочерние дескрипторы.
// Только распознанные метаданные корня тома под точным /workspace сохраняют исходный режим; потомков изменяет рекурсивная функция.
// Изменяются права доступа, а не состояние монтирования readOnly; содержимое документов не читается.
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

// protectInputEntry opens one child without following symlinks or blocking on special files, validates type/ownership and recursively processes directories.
// Regular files must have one hardlink; eligible descendants become mode 0440 or setgid 2750, preserving only the exact volume-root exception.
// It closes its descriptor on return; a late failure may leave earlier descendants already protected.
// protectInputEntry открывает потомка без следования ссылкам и блокировки на специальных файлах, проверяет тип и владельца, рекурсивно обрабатывает каталоги.
// Обычные файлы требуют одну жёсткую ссылку; допустимые потомки получают режим 0440 либо setgid 2750, сохраняя только точное исключение корня тома.
// Дескриптор закрывается при возврате; поздняя ошибка может оставить ранее обработанных потомков уже защищёнными.
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
