// Package securefile reads bounded regular files inside an explicitly chosen
// mount boundary, checking resolved paths, exact read-only modes and size.
// Its errors contain neither file paths nor file contents.
// RU: Пакет securefile читает ограниченные обычные файлы в заданной границе
// mount, проверяя разрешённые пути, точные read-only режимы и размер.
// Ошибки не содержат путей и содержимого файлов.
package securefile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	// errInvalidBoundary rejects missing paths and nonpositive size limits.
	// RU: errInvalidBoundary отклоняет пустые пути и неположительный лимит размера.
	errInvalidBoundary = errors.New("secure file boundary is invalid")
	// errUnavailable hides filesystem errors that could reveal path details.
	// RU: errUnavailable скрывает ошибки файловой системы, раскрывающие детали пути.
	errUnavailable = errors.New("secure file is unavailable")
	// errUnsafe rejects path escape, file type, mode and content-size violations.
	// RU: errUnsafe отклоняет выход за корень и нарушение типа, режима или размера файла.
	errUnsafe = errors.New("secure file is unsafe")
)

// Read selects the file's original directory as its boundary, so a resolved
// symlink may stay inside that tree but cannot escape the chosen root path.
// RU: Read считает исходный каталог файла границей: разрешённая symlink
// может остаться внутри него, но не выйти за выбранный корневой путь.
func Read(path string, maximumBytes int64) ([]byte, error) {
	return ReadWithin(filepath.Dir(path), path, maximumBytes)
}

// ReadProjectedServiceAccountToken applies the usual file checks and the
// narrow Kubernetes 0640 exception: a non-root process may read a root-owned
// token, or its own token only when the mount rejects writes with EROFS.
// RU: ReadProjectedServiceAccountToken применяет обычные проверки и узкое
// исключение для Kubernetes 0640: non-root процесс может читать root-owned
// токен либо свой токен только если mount отклоняет запись с EROFS.
func ReadProjectedServiceAccountToken(path string, maximumBytes int64) ([]byte, error) {
	return readWithin(filepath.Dir(path), path, maximumBytes, true)
}

// ReadWithin resolves the supplied root and file, rejects paths outside that
// root and reads only bounded regular files with an approved read-only mode.
// RU: ReadWithin разрешает корень и файл, отклоняет путь за пределами корня
// и читает лишь ограниченный обычный файл с допустимым read-only режимом.
func ReadWithin(root, path string, maximumBytes int64) ([]byte, error) {
	return readWithin(root, path, maximumBytes, false)
}

// readWithin performs the shared path, mode, size and exact-byte-count checks.
// The projected-token flag changes only the narrow 0640 ownership/mount rule;
// no credential format is interpreted and no result is cached.
// RU: readWithin выполняет общие проверки пути, режима, размера и точного
// числа байтов. Флаг projected token меняет только узкое правило для 0640,
// владельца и mount; формат credential не разбирается, результат не кэшируется.
func readWithin(root, path string, maximumBytes int64, allowProjectedToken bool) ([]byte, error) {
	if root == "" || path == "" || maximumBytes < 1 {
		return nil, errInvalidBoundary
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, errInvalidBoundary
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, errUnavailable
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, errInvalidBoundary
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return nil, errUnavailable
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) ||
		relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errUnsafe
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, errUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() ||
		!isSafeMode(
			info,
			allowProjectedToken,
			os.Geteuid(),
			projectedTokenMountIsReadOnly(resolvedPath, info, allowProjectedToken),
		) ||
		info.Size() < 1 || info.Size() > maximumBytes {
		return nil, errUnsafe
	}
	value, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil || len(value) < 1 || int64(len(value)) > maximumBytes ||
		int64(len(value)) != info.Size() {
		return nil, errUnavailable
	}
	return value, nil
}

// isSafeMode admits exact read-only modes, or a projected 0640 token for a
// non-root process when ownership and the tested mount state permit it.
// RU: isSafeMode допускает точные read-only режимы либо projected-токен 0640
// для non-root процесса при подходящем владельце и проверенном состоянии mount.
func isSafeMode(
	info os.FileInfo,
	allowProjectedToken bool,
	effectiveUID int,
	readOnlyMount bool,
) bool {
	if IsReadOnlyMode(info.Mode()) {
		return true
	}
	if !allowProjectedToken || info.Mode().Perm() != 0o640 || effectiveUID == 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stat.Uid == 0 || stat.Uid == uint32(effectiveUID) && readOnlyMount
}

// projectedTokenMountIsReadOnly probes only the exceptional 0640 token path;
// it treats EROFS on a write-open attempt as proof of a read-only mount.
// RU: projectedTokenMountIsReadOnly проверяет только исключительный токен 0640;
// EROFS при попытке открыть на запись подтверждает read-only mount.
func projectedTokenMountIsReadOnly(
	path string,
	info os.FileInfo,
	allowProjectedToken bool,
) bool {
	if !allowProjectedToken || info.Mode().Perm() != 0o640 {
		return false
	}
	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = probe.Close()
		return false
	}
	return errors.Is(err, syscall.EROFS)
}

// IsReadOnlyMode accepts only 0400, 0440 and 0444, with owner read access and
// no write or execute bits for any class; other modes fail closed.
// RU: IsReadOnlyMode допускает только 0400, 0440 и 0444: владелец может читать,
// а права записи и выполнения отсутствуют у всех; прочие режимы отклоняются.
func IsReadOnlyMode(mode os.FileMode) bool {
	switch mode.Perm() {
	case 0o400, 0o440, 0o444:
		return true
	default:
		return false
	}
}
