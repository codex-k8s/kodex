// Package security checks local artifact metadata and workspace permission boundaries.
// Пакет security проверяет локальные метаданные артефактов и границы прав рабочей области.
package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// protectedExecutable fixes the only accepted runner entrypoint path for protected execution modes; it is not a content hash or signature pin.
// protectedExecutable задаёт единственный допустимый путь точки входа runner для защищённых режимов; хешем содержимого или привязкой подписи не является.
const protectedExecutable = "/usr/local/bin/kodex-agent-runner"

// VerifyInvocation requires the exact two-argument protected entrypoint/mode invocation, confirms the running executable path and checks protected-file metadata.
// It does not hash executable content or authorize an execution lease.
// VerifyInvocation требует точный вызов защищённой точки входа с режимом в двух аргументах, подтверждает путь исполняемого файла и проверяет метаданные защищённого файла.
// Содержимое исполняемого файла не хешируется, аренда выполнения не авторизуется.
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

// VerifyProtectedRegular requires a clean absolute regular file, root UID, root or runtime group, one hardlink and no group/other writes; executable mode needs some execute bit.
// Also checks ancestor directory modes; Lstat checks do not pin an open descriptor or establish content provenance.
// VerifyProtectedRegular требует нормализованный абсолютный обычный файл, UID root, группу root либо выполнения, одну жёсткую ссылку и отсутствие записи группе и остальным; исполняемый режим требует хотя бы один бит выполнения.
// Также проверяет режимы родительских каталогов; проверки Lstat не закрепляют открытый дескриптор и не устанавливают происхождение содержимого.
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

// verifyParents walks ancestors below the filesystem root, rejecting symlinks, non-directories and group/other-writable modes.
// It does not check ancestor ownership or make the pathname walk atomic.
// verifyParents обходит родителей ниже корня файловой системы, отклоняя ссылки, не-каталоги и режимы с записью группе либо остальным.
// Владельцы родителей не проверяются, обход имён путей не становится атомарным.
func verifyParents(path string) error {
	for current := filepath.Dir(path); current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("protected artifact parent is unsafe")
		}
	}
	return nil
}

// EnsureWorkspaceDirectory creates or validates an effective-user-owned private relative directory chain beneath /workspace using the descriptor-based helper.
// EnsureWorkspaceDirectory создаёт либо проверяет частную цепочку относительных каталогов под /workspace с владельцем, равным действующему пользователю, через дескрипторную функцию.
func EnsureWorkspaceDirectory(relative string) error {
	return ensureWorkspaceDirectory(relative, 0o700, false)
}

// EnsureSharedWorkspaceDirectory creates or normalizes a runtime-group setgid directory chain beneath /workspace, permitting only the exact root-volume metadata exception.
// EnsureSharedWorkspaceDirectory создаёт либо нормализует цепочку каталогов с setgid и группой выполнения под /workspace, допуская только точное исключение метаданных корня тома.
func EnsureSharedWorkspaceDirectory(relative string) error {
	return ensureWorkspaceDirectory(relative, 0o2770, true)
}

// ensureWorkspaceDirectory walks a clean relative directory chain from an O_NOFOLLOW /workspace descriptor, using mkdirat/openat/fstat instead of following path components.
// Private directories must be owned without group/other access; shared owned directories are normalized to group 29000 and the requested mode.
// Only recognized root-volume metadata may bypass current-UID ownership; opened descriptors are closed on success and failure.
// ensureWorkspaceDirectory обходит нормализованную относительную цепочку от дескриптора /workspace с O_NOFOLLOW через mkdirat, openat и fstat без следования компонентам-ссылкам.
// Частные каталоги требуют собственного владельца без доступа группе и остальным; собственные общие каталоги нормализуются к группе 29000 и заданному режиму.
// Только распознанные метаданные корня тома допускают исключение текущего UID; открытые дескрипторы закрываются при успехе и ошибке.
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

// isWorkspaceVolumeRoot recognizes only input, knowledge and .kodex/state with root UID, runtime group 29000, directory type and full group access.
// Other permission bits are tolerated for kubelet-created volume roots; this predicate checks metadata, not actual mount provenance.
// isWorkspaceVolumeRoot распознаёт только input, knowledge и .kodex/state с UID root, группой выполнения 29000, типом каталога и полным групповым доступом.
// Прочие биты доступа допускаются для корней томов kubelet; предикат проверяет метаданные, а не фактическое происхождение монтирования.
func isWorkspaceVolumeRoot(relative string, stat unix.Stat_t) bool {
	return (relative == "input" || relative == "knowledge" || relative == ".kodex/state") &&
		stat.Uid == 0 && stat.Gid == 29000 && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o070 == 0o070
}
