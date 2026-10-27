package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// persistentInput records a validated original and whether its untrusted working copy is absent.
// It carries no capability and is used only while the caller holds the stopped workspace lock.
// persistentInput хранит проверенный оригинал и отсутствие его недоверенной рабочей копии.
// Это не capability; запись используется только под lock остановленной рабочей области.
type persistentInput struct {
	relative string
	artifact runtimecontract.RunnerInputArtifact
	missing  bool
}

// preparePersistentWorkingTree keeps source files and edited copies, refreshing only the guide.
// Originals are revalidated even when copies exist; any unsafe entry refuses without deleting work.
// preparePersistentWorkingTree сохраняет исходники и изменённые копии, обновляя только справочник.
// Оригиналы проверяются и при существующей копии; unsafe entry даёт отказ без удаления работы.
func preparePersistentWorkingTree(ctx context.Context, input model.Input, root *os.Root, directory *os.File, sourceUID uint32) error {
	if input.WorkspaceStorage.ValidateForInput(input) != nil ||
		validatePersistentTree(ctx, int(directory.Fd()), int(directory.Fd()), "", sourceUID, input.WorkspacePolicy, new(workspaceAccessUsage)) != nil {
		return errWorkingDirectory
	}
	bytes, _, entries, err := workspacepolicy.WritableUsageUnderLock(ctx, input.WorkspaceRoot, input.WorkspacePolicy)
	if err != nil {
		return errWorkingDirectory
	}
	guide := materializedWorkspaceGuide(input)
	additionalBytes, additionalEntries := int64(len(guide)), int64(1)
	previousGuideBytes, previousGuideEntries := int64(0), int64(0)
	if info, err := root.Lstat(workspaceGuideName); err == nil {
		if !info.Mode().IsRegular() {
			return errWorkingDirectory
		}
		additionalBytes -= info.Size()
		additionalEntries = 0
		previousGuideBytes, previousGuideEntries = info.Size(), 1
	} else if !errors.Is(err, os.ErrNotExist) {
		return errWorkingDirectory
	}
	inputs := make([]persistentInput, 0, len(input.InputArtifacts))
	newDirectories := make(map[string]struct{})
	for _, artifact := range input.InputArtifacts {
		if artifact.Scope != runtimecontract.AttachmentScopeInput {
			continue
		}
		canonical, err := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
		if err != nil || !strings.HasPrefix(canonical, "/workspace/input/") {
			return errWorkingDirectory
		}
		relative := strings.TrimPrefix(canonical, "/workspace/")
		if validatePersistentOriginal(input.WorkspaceRoot, relative, artifact, sourceUID) != nil {
			return errWorkingDirectory
		}
		info, err := root.Lstat(relative)
		missing := errors.Is(err, os.ErrNotExist)
		if err != nil && !missing || !missing && !info.Mode().IsRegular() {
			return errWorkingDirectory
		}
		if missing {
			additionalBytes += artifact.SizeBytes
			additionalEntries++
			for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
				info, err := root.Lstat(parent)
				if errors.Is(err, os.ErrNotExist) {
					newDirectories[parent] = struct{}{}
				} else if err != nil || !info.IsDir() {
					return errWorkingDirectory
				}
			}
		}
		inputs = append(inputs, persistentInput{relative: relative, artifact: artifact, missing: missing})
	}
	additionalEntries += int64(len(newDirectories))
	// Резерв включает временный новый guide до его atomic rename поверх старого.
	if ctx.Err() != nil || additionalBytes+previousGuideBytes > input.WorkspacePolicy.MaximumWritableBytes-bytes ||
		additionalEntries+previousGuideEntries > input.WorkspacePolicy.MaximumFileCount-entries {
		return errWorkingDirectory
	}
	for _, item := range inputs {
		if !item.missing {
			continue
		}
		file, info, err := openProtectedFile(input.WorkspaceRoot, filepath.Join(input.WorkspaceRoot, item.relative))
		if err != nil {
			return errWorkingDirectory
		}
		err = copyWorkingInput(root, item.relative, file, info, item.artifact, sourceUID)
		closeErr := file.Close()
		if err != nil || closeErr != nil || ctx.Err() != nil {
			return errWorkingDirectory
		}
	}
	return replacePersistentGuide(root, directory, guide)
}

// validatePersistentOriginal proves the current read-only INPUT bytes without trusting an edited copy.
// It opens no writable target and preserves inode/time, owner, link and exact digest checks.
// validatePersistentOriginal доказывает текущие read-only INPUT bytes без доверия изменённой копии.
// Writable target не открывается; сохраняются inode/time, owner, link и exact digest проверки.
func validatePersistentOriginal(workspaceRoot, relative string, artifact runtimecontract.RunnerInputArtifact, sourceUID uint32) error {
	file, before, err := openProtectedFile(workspaceRoot, filepath.Join(workspaceRoot, relative))
	if err != nil {
		return errWorkingDirectory
	}
	defer file.Close()
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != sourceUID || stat.Nlink != 1 || before.Mode().Perm() != 0o440 || before.Size() != artifact.SizeBytes {
		return errWorkingDirectory
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, artifact.SizeBytes+1))
	after, statErr := file.Stat()
	if err != nil || written != artifact.SizeBytes || statErr != nil {
		return errWorkingDirectory
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || !os.SameFile(before, after) || before.Size() != after.Size() || stat.Mtim != afterStat.Mtim || stat.Ctim != afterStat.Ctim ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != artifact.Digest {
		return errWorkingDirectory
	}
	return nil
}

// validatePersistentTree rejects foreign ownership, hardlinks and nested mounts before any writes.
// Contained links use the existing work-root confinement; the walk never follows them for counting.
// validatePersistentTree отвергает чужих owner, hardlinks и nested mounts до любой записи.
// Внутренние ссылки проходят прежнюю work-root проверку; обход не следует им при подсчёте.
func validatePersistentTree(ctx context.Context, parent, workFD int, relative string, sourceUID uint32, policy runtimecontract.RuntimeWorkspacePolicy, usage *workspaceAccessUsage) error {
	if ctx.Err() != nil || usage.entries >= policy.MaximumFileCount {
		return errWorkingDirectory
	}
	usage.entries++
	fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkingDirectory
	}
	directory := os.NewFile(uintptr(fd), "persistent-working-tree")
	defer directory.Close()
	entries, err := directory.ReadDir(int(policy.MaximumFileCount-usage.entries) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return errWorkingDirectory
	}
	for _, entry := range entries {
		if ctx.Err() != nil || usage.entries >= policy.MaximumFileCount || !sameWorkspaceAccessMount(fd, entry.Name()) {
			return errWorkingDirectory
		}
		var info unix.Stat_t
		if unix.Fstatat(fd, entry.Name(), &info, unix.AT_SYMLINK_NOFOLLOW) != nil || info.Uid != sourceUID && info.Uid != providerWriterUID {
			return errWorkingDirectory
		}
		name := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		if info.Mode&unix.S_IFMT == unix.S_IFDIR {
			child, err := openWorkspaceAccessDirectory(fd, entry.Name())
			if err != nil {
				return errWorkingDirectory
			}
			err = validatePersistentTree(ctx, child, workFD, name, sourceUID, policy, usage)
			unix.Close(child)
			if err != nil {
				return err
			}
			continue
		}
		usage.entries++
		if info.Nlink != 1 || info.Size < 0 || info.Size > policy.MaximumWritableBytes-usage.bytes {
			return errWorkingDirectory
		}
		usage.bytes += info.Size
		switch info.Mode & unix.S_IFMT {
		case unix.S_IFREG:
		case unix.S_IFLNK:
			if workspacepolicy.CheckWorkLink(workFD, name) != nil {
				return errWorkingDirectory
			}
		default:
			return errWorkingDirectory
		}
	}
	return nil
}

// replacePersistentGuide exclusively stages and syncs only the platform guide before atomic replace.
// An existing staging name fails closed; it is never removed as untrusted disposable data.
// replacePersistentGuide создаёт и sync только platform guide до atomic replace.
// Существующее staging имя даёт отказ; оно не удаляется как чужие временные данные.
func replacePersistentGuide(root *os.Root, directory *os.File, guide string) error {
	const staged = ".kodex-workspace-guide.next"
	file, err := root.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return errWorkingDirectory
	}
	written, writeErr := io.WriteString(file, guide)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(guide) || syncErr != nil || closeErr != nil || root.Rename(staged, workspaceGuideName) != nil || directory.Sync() != nil {
		return errWorkingDirectory
	}
	return nil
}
