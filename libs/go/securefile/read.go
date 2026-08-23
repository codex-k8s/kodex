// Package securefile читает bounded read-only files внутри точной mount
// boundary без раскрытия пути или содержимого в ошибках.
package securefile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	errInvalidBoundary = errors.New("secure file boundary is invalid")
	errUnavailable     = errors.New("secure file is unavailable")
	errUnsafe          = errors.New("secure file is unsafe")
)

// Read читает файл, разрешая symlink только внутри его исходного каталога.
func Read(path string, maximumBytes int64) ([]byte, error) {
	return ReadWithin(filepath.Dir(path), path, maximumBytes)
}

// ReadWithin читает файл, разрешая symlink только внутри root.
func ReadWithin(root, path string, maximumBytes int64) ([]byte, error) {
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
	if err != nil || !info.Mode().IsRegular() || !IsReadOnlyMode(info.Mode()) ||
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

// IsReadOnlyMode возвращает true только для утвержденных exact permissions.
func IsReadOnlyMode(mode os.FileMode) bool {
	switch mode.Perm() {
	case 0o400, 0o440, 0o444:
		return true
	default:
		return false
	}
}
