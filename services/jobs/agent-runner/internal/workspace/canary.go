// Package workspace применяет server-owned writable policy к фактическому mount.
package workspace

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

type Denial struct{ Reason string }

func (denial *Denial) Error() string { return "workspace readiness denied: " + denial.Reason }

func DenialReason(err error) string {
	var denial *Denial
	if errors.As(err, &denial) {
		return denial.Reason
	}
	return runtimecontract.RuntimeWorkspaceIOError
}

func RunCanary(root string, policy runtimecontract.RuntimeWorkspacePolicy) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || policy.Validate() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	for _, candidate := range []string{"/workspace/input/readiness", "/workspace/knowledge/readiness"} {
		access, reason := policy.AccessForPath(candidate)
		if reason != "" || access != runtimecontract.RuntimeWorkspaceReadOnly {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
		}
	}
	usage, files, err := writableUsage(root, policy)
	if err != nil {
		return err
	}
	const payload = "kodex-workspace-readiness\n"
	if !withinQuota(usage, files, int64(len(payload)), policy) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	}
	directory, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return classify(err)
	}
	defer unix.Close(directory)
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	temporary := ".readiness-next-" + stringHex(nonce)
	committed := ".readiness-current-" + stringHex(nonce)
	defer unix.Unlinkat(directory, temporary, 0)
	defer unix.Unlinkat(directory, committed, 0)
	file, err := unix.Openat(directory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if _, err = unix.Write(file, []byte(payload)); err == nil {
		err = unix.Fsync(file)
	}
	closeErr := unix.Close(file)
	if err != nil {
		return classify(err)
	}
	if closeErr != nil {
		return classify(closeErr)
	}
	if err = unix.Renameat(directory, temporary, directory, committed); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(directory); err != nil {
		return classify(err)
	}
	file, err = unix.Openat(directory, committed, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return classify(err)
	}
	buffer := make([]byte, len(payload)+1)
	read, readErr := unix.Read(file, buffer)
	closeErr = unix.Close(file)
	if readErr != nil || closeErr != nil || read != len(payload) || string(buffer[:read]) != payload {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if err = unix.Unlinkat(directory, committed, 0); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(directory); err != nil {
		return classify(err)
	}
	return nil
}

func writableUsage(root string, policy runtimecontract.RuntimeWorkspacePolicy) (int64, int64, error) {
	var bytes, files int64
	err := filepath.WalkDir(root, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return classify(walkErr)
		}
		relative, err := filepath.Rel(root, localPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
		}
		canonical := policy.Root
		if relative != "." {
			canonical += "/" + filepath.ToSlash(relative)
		}
		access, reason := policy.AccessForPath(canonical)
		if reason != "" {
			return &Denial{Reason: reason}
		}
		if access == runtimecontract.RuntimeWorkspaceReadOnly && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return classify(err)
			}
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files, err
}

func withinQuota(currentBytes, currentFiles, additionalBytes int64, policy runtimecontract.RuntimeWorkspacePolicy) bool {
	return currentBytes >= 0 && currentFiles >= 0 && additionalBytes >= 0 &&
		currentBytes <= policy.MaximumWritableBytes-additionalBytes && currentFiles < policy.MaximumFileCount
}

func openDirectory(root, relative string) (int, error) {
	current, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			unix.Close(current)
			return -1, syscall.EINVAL
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

// OpenOutbox возвращает directory handle, разрешённый без следования symlink.
func OpenOutbox(root string) (*os.File, error) {
	descriptor, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return nil, classify(err)
	}
	file := os.NewFile(uintptr(descriptor), "runtime-outbox")
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return file, nil
}

func classify(err error) error {
	switch {
	case errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.EINVAL):
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	default:
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
}

func stringHex(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2], result[index*2+1] = digits[item>>4], digits[item&15]
	}
	return string(result)
}
