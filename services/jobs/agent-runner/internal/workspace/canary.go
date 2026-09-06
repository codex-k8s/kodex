// Package workspace применяет server-owned writable policy к фактическому mount.
package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type ResultProvenance struct {
	Schema                 string `json:"schema"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	Attempt                int32  `json:"attempt"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
}

func (denial *Denial) Error() string { return "workspace readiness denied: " + denial.Reason }

func DenialReason(err error) string {
	var denial *Denial
	if errors.As(err, &denial) {
		return denial.Reason
	}
	return runtimecontract.RuntimeWorkspaceIOError
}

func RunCanary(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy) error {
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || policy.Validate() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	lock, err := Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	for _, candidate := range []string{"/workspace/input/readiness", "/workspace/knowledge/readiness", runtimecontract.RuntimeContextRoot + "/readiness"} {
		access, reason := policy.AccessForPath(candidate)
		if reason != "" || access != runtimecontract.RuntimeWorkspaceReadOnly {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
		}
	}
	usage, files, err := writableUsageContext(ctx, root, policy)
	if err != nil {
		return err
	}
	const initialPayload = "kodex-workspace-create\n"
	const replacementPayload = "kodex-workspace-replace\n"
	if !withinQuota(usage, files, int64(len(initialPayload)+len(replacementPayload)), policy) {
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
	nested := ".readiness-" + stringHex(nonce)
	if err := unix.Mkdirat(directory, nested, 0o700); err != nil {
		return classify(err)
	}
	defer unix.Unlinkat(directory, nested, unix.AT_REMOVEDIR)
	nestedDirectory, err := unix.Openat(directory, nested, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return classify(err)
	}
	defer unix.Close(nestedDirectory)
	const current = "current.txt"
	const temporary = "current.txt.next"
	defer unix.Unlinkat(nestedDirectory, temporary, 0)
	defer unix.Unlinkat(nestedDirectory, current, 0)
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	file, err := unix.Openat(nestedDirectory, current, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, []byte(initialPayload)); err == nil {
		err = unix.Fsync(file)
	}
	closeErr := unix.Close(file)
	if err != nil {
		return classify(err)
	}
	if closeErr != nil {
		return classify(closeErr)
	}
	if err := verifyFile(nestedDirectory, current, initialPayload); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	file, err = unix.Openat(nestedDirectory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, []byte(replacementPayload)); err == nil {
		err = unix.Fsync(file)
	}
	closeErr = unix.Close(file)
	if err != nil || closeErr != nil {
		return classify(errors.Join(err, closeErr))
	}
	if err = unix.Renameat(nestedDirectory, temporary, nestedDirectory, current); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(nestedDirectory); err != nil {
		return classify(err)
	}
	if err := verifyFile(nestedDirectory, current, replacementPayload); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if err = unix.Unlinkat(nestedDirectory, current, 0); err != nil {
		return classify(err)
	}
	if err = unix.Fsync(nestedDirectory); err != nil {
		return classify(err)
	}
	if err = unix.Unlinkat(directory, nested, unix.AT_REMOVEDIR); err != nil {
		return classify(err)
	}
	return nil
}

func verifyFile(directory int, name, expected string) error {
	file, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return classify(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(file, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		_ = unix.Close(file)
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	buffer := make([]byte, len(expected)+1)
	read, readErr := readBounded(file, buffer)
	closeErr := unix.Close(file)
	if readErr != nil || closeErr != nil || read != len(expected) || string(buffer[:read]) != expected {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return nil
}

func PublishResult(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy, provenance ResultProvenance) error {
	if provenance.Schema != "kodex.workspace-write-result.v1" || provenance.RuntimeRevisionRef == "" ||
		provenance.RuntimeRevisionVersion < 1 || !validSHA256(provenance.RuntimeRevisionDigest) ||
		provenance.Attempt < 1 || !validSHA256(provenance.ExecutionBindingDigest) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	raw, err := json.Marshal(provenance)
	if err != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	raw = append(raw, '\n')
	if access, reason := policy.AccessForPath(".kodex/outbox/workspace-write-result.json"); reason != "" || access != runtimecontract.RuntimeWorkspaceWritable {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}
	}
	lock, err := Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	directory, err := openDirectory(root, ".kodex/outbox")
	if err != nil {
		return classify(err)
	}
	defer unix.Close(directory)
	usage, files, err := writableUsage(root, policy)
	if err != nil {
		return err
	}
	if !withinQuota(usage, files, int64(len(raw)), policy) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
	}
	const temporary = ".workspace-write-result.next"
	const committed = "workspace-write-result.json"
	_ = unix.Unlinkat(directory, temporary, 0)
	file, err := unix.Openat(directory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return classify(err)
	}
	if err = writeFull(file, raw); err == nil {
		err = unix.Fsync(file)
	}
	closeErr := unix.Close(file)
	if err != nil || closeErr != nil {
		_ = unix.Unlinkat(directory, temporary, 0)
		return classify(errors.Join(err, closeErr))
	}
	if err = unix.Renameat(directory, temporary, directory, committed); err != nil {
		_ = unix.Unlinkat(directory, temporary, 0)
		return classify(err)
	}
	if err = unix.Fsync(directory); err != nil {
		return classify(err)
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func writeFull(file int, payload []byte) error {
	for len(payload) != 0 {
		written, err := unix.Write(file, payload)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || written <= 0 {
			return errors.New("workspace write is incomplete")
		}
		payload = payload[written:]
	}
	return nil
}

func readBounded(file int, buffer []byte) (int, error) {
	total := 0
	for total < len(buffer) {
		read, err := unix.Read(file, buffer[total:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return total, err
		}
		if read == 0 {
			return total, nil
		}
		total += read
	}
	return total, nil
}

func writableUsage(root string, policy runtimecontract.RuntimeWorkspacePolicy) (int64, int64, error) {
	return writableUsageContext(nil, root, policy)
}

func writableUsageContext(ctx context.Context, root string, policy runtimecontract.RuntimeWorkspacePolicy) (int64, int64, error) {
	var bytes, files, entries int64
	err := filepath.WalkDir(root, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if ctx != nil && ctx.Err() != nil {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
		}
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
		entries++
		if entries > policy.MaximumFileCount {
			return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return classify(err)
			}
			if info.Size() < 0 || info.Size() > policy.MaximumWritableBytes-bytes {
				return &Denial{Reason: runtimecontract.RuntimeWorkspaceQuotaExceeded}
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
