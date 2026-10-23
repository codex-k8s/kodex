package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

const workspaceAccessBudget = 2 * time.Second

var errProviderWorkspaceAccess = errors.New("provider workspace directory access is invalid")

// Только writer после stop/join делает собственные рабочие каталоги доступными
// для metadata/quota обхода runner. Private file bodies не открываются.
func normalizeProviderWorkspaceAccess(ctx context.Context, input model.Input) error {
	if os.Geteuid() != providerWriterUID {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemOwner)
	}
	if input.WorkspaceRoot != runtimecontract.RuntimeWorkspaceRoot || input.WorkspacePolicy.Validate() != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemPolicy)
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceAccessBudget)
	defer cancel()
	lock, err := workspacepolicy.Lock(ctx, input.WorkspaceRoot)
	if err != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemLock)
	}
	defer lock.Close()
	fd, err := openWorkspaceAccessDirectory(int(lock.Fd()), "work")
	if err != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemDirectory)
	}
	directory := os.NewFile(uintptr(fd), "runtime-working-directory")
	defer directory.Close()
	if err := normalizeWorkspaceAccess(ctx, directory, input.WorkspacePolicy, 10001, providerWriterUID, 29000); err != nil {
		return err
	}
	var opened, current unix.Stat_t
	if unix.Fstat(fd, &opened) != nil || unix.Fstatat(int(lock.Fd()), "work", &current, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameWorkspaceAccessStat(opened, current) {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMetadataChanged)
	}
	return nil
}

// Проверяемые UID/GID назначает только production callsite, не input агента.
func normalizeWorkspaceAccess(ctx context.Context, directory *os.File, policy runtimecontract.RuntimeWorkspacePolicy, runnerUID, providerUID, group uint32) error {
	if ctx.Err() != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemContext)
	}
	if policy.Validate() != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemPolicy)
	}
	var root unix.Stat_t
	if unix.Fstat(int(directory.Fd()), &root) != nil || root.Mode&unix.S_IFMT != unix.S_IFDIR || root.Uid != runnerUID || root.Gid != group || root.Mode&0o7777 != 0o2770 {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemDirectoryIdentity)
	}
	usage := workspaceAccessUsage{entries: 1}
	return normalizeWorkspaceAccessTree(ctx, directory, int(directory.Fd()), "", policy, runnerUID, providerUID, group, &usage)
}

type workspaceAccessUsage struct{ entries, bytes int64 }

func normalizeWorkspaceAccessTree(ctx context.Context, directory *os.File, workFD int, relative string, policy runtimecontract.RuntimeWorkspacePolicy, runnerUID, providerUID, group uint32, usage *workspaceAccessUsage) error {
	if ctx.Err() != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemContext)
	}
	if usage.entries > policy.MaximumFileCount {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemEntryCount)
	}
	entries, err := directory.ReadDir(int(policy.MaximumFileCount-usage.entries) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemIO)
	}
	if int64(len(entries)) > policy.MaximumFileCount-usage.entries {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemEntryCount)
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemContext)
		}
		usage.entries++
		if usage.entries > policy.MaximumFileCount {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemEntryCount)
		}
		if !sameWorkspaceAccessMount(int(directory.Fd()), entry.Name()) {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMount)
		}
		name := entry.Name()
		var before unix.Stat_t
		if unix.Fstatat(int(directory.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW) != nil {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemIO)
		}
		if before.Uid != runnerUID && before.Uid != providerUID {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemOwner)
		}
		switch before.Mode & unix.S_IFMT {
		case unix.S_IFREG:
			if before.Nlink != 1 {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemLinkCount)
			}
			if before.Size < 0 {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemFileSize)
			}
			if before.Size > policy.MaximumWritableBytes-usage.bytes {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemByteQuota)
			}
			usage.bytes += before.Size
			var after unix.Stat_t
			if unix.Fstatat(int(directory.Fd()), name, &after, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameWorkspaceAccessStat(before, after) {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMetadataChanged)
			}
		case unix.S_IFLNK:
			if before.Nlink != 1 {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemLinkCount)
			}
			if err := workspacepolicy.CheckWorkLink(workFD, path.Join(relative, name)); err != nil {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemFileType)
			}
			if before.Size < 0 || before.Size > policy.MaximumWritableBytes-usage.bytes {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemByteQuota)
			}
			usage.bytes += before.Size
			var after unix.Stat_t
			if unix.Fstatat(int(directory.Fd()), name, &after, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameWorkspaceAccessStat(before, after) {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMetadataChanged)
			}
		case unix.S_IFDIR:
			fd, err := openWorkspaceAccessDirectory(int(directory.Fd()), name)
			if err != nil {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemEntryOpen)
			}
			child := os.NewFile(uintptr(fd), "runtime-working-descendant")
			err = normalizeWorkspaceAccessChild(ctx, directory, child, name, before, workFD, path.Join(relative, name), policy, runnerUID, providerUID, group, usage)
			closeErr := child.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemIO)
			}
		default:
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemFileType)
		}
	}
	return nil
}

func normalizeWorkspaceAccessChild(ctx context.Context, parent, child *os.File, name string, before unix.Stat_t, workFD int, relative string, policy runtimecontract.RuntimeWorkspacePolicy, runnerUID, providerUID, group uint32, usage *workspaceAccessUsage) error {
	var opened unix.Stat_t
	if unix.Fstat(int(child.Fd()), &opened) != nil || !sameWorkspaceAccessStat(before, opened) || opened.Mode&unix.S_IFMT != unix.S_IFDIR {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMetadataChanged)
	}
	if opened.Uid == runnerUID && opened.Uid != providerUID && (opened.Gid != group || opened.Mode&0o050 != 0o050) {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemDirectoryIdentity)
	}
	if err := normalizeWorkspaceAccessTree(ctx, child, workFD, relative, policy, runnerUID, providerUID, group, usage); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemContext)
	}
	mode := opened.Mode & 0o7777
	if opened.Uid == providerUID {
		mode |= 0o050
		if opened.Gid != group && unix.Fchown(int(child.Fd()), -1, int(group)) != nil || unix.Fchmod(int(child.Fd()), mode) != nil {
			return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemPermissions)
		}
	}
	var after, current unix.Stat_t
	if unix.Fstat(int(child.Fd()), &after) != nil || unix.Fstatat(int(parent.Fd()), name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		after.Dev != opened.Dev || after.Ino != opened.Ino || after.Uid != opened.Uid || after.Nlink != opened.Nlink || after.Mtim != opened.Mtim ||
		after.Gid != group || after.Mode&0o7777 != mode || !sameWorkspaceAccessStat(after, current) {
		return providerFilesystemDenied(errProviderWorkspaceAccess, filesystemMetadataChanged)
	}
	return nil
}

func openWorkspaceAccessDirectory(parent int, name string) (int, error) {
	return unix.Openat2(parent, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
	})
}

func sameWorkspaceAccessStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Mode == right.Mode && left.Nlink == right.Nlink && left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func sameWorkspaceAccessMount(parent int, name string) bool {
	var root, child unix.Statx_t
	return unix.Statx(parent, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &root) == nil &&
		unix.Statx(parent, name, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &child) == nil &&
		root.Mask&unix.STATX_MNT_ID != 0 && child.Mask&unix.STATX_MNT_ID != 0 && root.Mnt_id == child.Mnt_id
}
