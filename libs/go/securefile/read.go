// Package securefile читает bounded read-only files внутри точной mount
// boundary без раскрытия пути или содержимого в ошибках.
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
	errInvalidBoundary = errors.New("secure file boundary is invalid")
	errUnavailable     = errors.New("secure file is unavailable")
	errUnsafe          = errors.New("secure file is unsafe")
)

// Read читает файл, разрешая symlink только внутри его исходного каталога.
func Read(path string, maximumBytes int64) ([]byte, error) {
	return ReadWithin(filepath.Dir(path), path, maximumBytes)
}

// ReadProjectedServiceAccountToken читает Kubernetes projected token, у
// которого kubelet оставляет root owner-write для атомарной ротации, а
// non-root workload получает только group-read.
func ReadProjectedServiceAccountToken(path string, maximumBytes int64) ([]byte, error) {
	return readWithin(filepath.Dir(path), path, maximumBytes, true)
}

// ReadWithin читает файл, разрешая symlink только внутри root.
func ReadWithin(root, path string, maximumBytes int64) ([]byte, error) {
	return readWithin(root, path, maximumBytes, false)
}

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
		!isSafeMode(info, allowProjectedToken, os.Geteuid()) ||
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

func isSafeMode(info os.FileInfo, allowProjectedToken bool, effectiveUID int) bool {
	if IsReadOnlyMode(info.Mode()) {
		return true
	}
	if !allowProjectedToken || info.Mode().Perm() != 0o640 || effectiveUID == 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
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
