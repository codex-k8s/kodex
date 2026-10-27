package archive

import (
	"bytes"
	"context"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// PrepareWorkspaceVolume seals only a newly created, owner-authorized empty local-path PVC after its first FSGroup preparation.
// Reconciliation requires a read-only mount and an existing exact record; it never normalizes permissions, creates missing metadata or proves business data empty.
// The caller separately proves the persisted grant, positive PVC UID, trusted Job/image and absence of other consumers before materialization.
// PrepareWorkspaceVolume запечатывает только новый owner-authorized empty local-path PVC после первой подготовки FSGroup.
// Reconciliation требует read-only mount и existing exact record; она не нормализует permissions, не создаёт missing metadata и не доказывает пустоту business data.
// Caller отдельно доказывает persisted grant, positive PVC UID, trusted Job/image и отсутствие других consumers до materialization.
func PrepareWorkspaceVolume(ctx context.Context, preparation runtimecontract.WorkspaceVolumePreparation, reconcileOnly bool) (ready runtimecontract.WorkspaceVolumeReady, resultErr error) {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 10001 || os.Getegid() != 29000 || preparation.Validate() != nil {
		return ready, errWorkspaceObjectUnconfirmed
	}
	root, err := unix.Open(runtimecontract.WorkspaceStorageRootMount, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ready, errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if unix.Close(root) != nil {
			ready, resultErr = runtimecontract.WorkspaceVolumeReady{}, errWorkspaceObjectUnconfirmed
		}
	}()
	var filesystem unix.Statfs_t
	if unix.Fstatfs(root, &filesystem) != nil || (filesystem.Flags&unix.ST_RDONLY != 0) != reconcileOnly {
		return ready, errWorkspaceObjectUnconfirmed
	}
	return prepareWorkspaceVolumeAt(ctx, root, preparation, securefile.OwnedRecordIdentity{UID: 10001, GID: 29000}, 0, reconcileOnly)
}

// workspaceVolumeRoot checks the exact qualified local-path root without chown/chmod; tests supply their private stand-in's owner explicitly.
// Root ownership and mode are preconditions, not evidence of a domain grant or an empty directory.
// workspaceVolumeRoot проверяет exact qualified local-path root без chown/chmod; tests явно передают владельца private stand-in.
// Ownership/mode корня — preconditions, не доказательство domain grant или пустоты каталога.
func workspaceVolumeRoot(ctx context.Context, root int, owner securefile.OwnedRecordIdentity, rootUID uint32) (unix.Stat_t, error) {
	var info unix.Stat_t
	if ctx == nil || ctx.Err() != nil || root < 0 || owner.UID != uint32(os.Geteuid()) ||
		unix.Fstat(root, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o7777 != 0o2777 ||
		info.Uid != rootUID || info.Gid != owner.GID || info.Dev == 0 || info.Ino == 0 {
		return info, errWorkspaceObjectUnconfirmed
	}
	return info, nil
}

// prepareWorkspaceVolumeAt creates only its own private metadata directory on the first empty-root attempt and preserves every uncertain entry.
// Later attempts read a sealed matching record without listing or touching business directories; descriptor ownership remains with the caller.
// prepareWorkspaceVolumeAt создаёт только собственный private metadata directory при первой empty-root attempt и сохраняет все uncertain entries.
// Поздние attempts читают matching sealed record без обхода/изменения business directories; descriptor остаётся у caller.
func prepareWorkspaceVolumeAt(ctx context.Context, root int, preparation runtimecontract.WorkspaceVolumePreparation, owner securefile.OwnedRecordIdentity, rootUID uint32, reconcileOnly bool) (ready runtimecontract.WorkspaceVolumeReady, resultErr error) {
	info, err := workspaceVolumeRoot(ctx, root, owner, rootUID)
	if err != nil || preparation.Validate() != nil {
		return ready, errWorkspaceObjectUnconfirmed
	}
	expected := runtimecontract.WorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema,
		Preparation: preparation, Device: uint64(info.Dev), Inode: info.Ino}
	wanted, err := runtimecontract.EncodeWorkspaceVolumeReady(expected)
	if err != nil {
		return ready, errWorkspaceObjectUnconfirmed
	}
	if !reconcileOnly {
		names, err := restoreDirectoryNames(ctx, root, 1)
		if err != nil || len(names) != 0 {
			return ready, errWorkspaceObjectUnconfirmed
		}
	}
	metadata, created, err := securefile.OpenOwnedDirectoryAt(ctx, root, runtimecontract.WorkspaceVolumeMetadataDirectory, 0o700, owner, !reconcileOnly)
	if err != nil {
		return ready, errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if unix.Close(metadata) != nil {
			ready, resultErr = runtimecontract.WorkspaceVolumeReady{}, errWorkspaceObjectUnconfirmed
		}
	}()
	if !reconcileOnly {
		if !created || securefile.WriteOwnedRecordAt(ctx, metadata, runtimecontract.WorkspaceVolumeReadyFile,
			wanted, runtimecontract.MaximumWorkspaceVolumeReadyBytes, owner) != nil || unix.Fsync(root) != nil {
			return ready, errWorkspaceObjectUnconfirmed
		}
		// No competing consumer may add even an unrelated root entry while the first preparation owns EMPTY custody.
		// Ни один competing consumer не может добавить даже unrelated root entry, пока первая preparation владеет EMPTY custody.
		names, err := restoreDirectoryNames(ctx, root, 2)
		if err != nil || len(names) != 1 || names[0] != runtimecontract.WorkspaceVolumeMetadataDirectory {
			return ready, errWorkspaceObjectUnconfirmed
		}
	}
	raw, err := securefile.ReadOwnedRecordAt(ctx, metadata, runtimecontract.WorkspaceVolumeReadyFile,
		runtimecontract.MaximumWorkspaceVolumeReadyBytes, owner)
	if err != nil || !bytes.Equal(raw, wanted) {
		return ready, errWorkspaceObjectUnconfirmed
	}
	observed, err := runtimecontract.DecodeWorkspaceVolumeReady(raw)
	after, statErr := workspaceVolumeRoot(ctx, root, owner, rootUID)
	if err != nil || statErr != nil || observed != expected || after.Dev != info.Dev || after.Ino != info.Ino {
		return ready, errWorkspaceObjectUnconfirmed
	}
	return observed, nil
}
