package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// PrepareWorkspaceRestore creates one private generation only in the trusted fixed-UID initializer's full-PVC mount.
// Before invoking it the controller must prove the exact task grant, observed PVC UID and absence of other consumers.
// Local task syntax and filesystem ownership are not substitutes for that owner authorization or predecessor join.
// PrepareWorkspaceRestore создаёт private generation только в full-PVC mount доверенного fixed-UID initializer.
// До вызова controller доказывает exact task grant, observed PVC UID и отсутствие других consumers.
// Task syntax/filesystem ownership не заменяют owner authorization или predecessor join.
func PrepareWorkspaceRestore(ctx context.Context, task model.Task) error {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 10001 || os.Getegid() != 29000 ||
		task.Workspace == nil || task.Workspace.PVCUID == "" || task.Workspace.ReconcileOnly {
		return errWorkspaceObjectUnconfirmed
	}
	if _, err := workspaceRestoreIntentBytes(task, task.Workspace.PVCUID); err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	// The immutable image may expose /var/run through the standard /run alias; only this fixed trusted prefix is followed.
	// The mount leaf is no-follow, and every data component below its verified descriptor uses strict openat2 resolution.
	// Неизменяемый образ может содержать системную ссылку /var/run на /run; только этот fixed trusted prefix прослеживается.
	// Mount leaf имеет no-follow; все data components под проверенным descriptor используют strict openat2 resolution.
	root, err := unix.Open(runtimecontract.WorkspaceStorageRootMount, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(root)
	var volume unix.Stat_t
	var filesystem unix.Statfs_t
	if unix.Fstat(root, &volume) != nil || volume.Mode&unix.S_IFMT != unix.S_IFDIR || volume.Uid != 0 ||
		volume.Gid != 29000 || volume.Mode&0o070 != 0o070 || unix.Fstatfs(root, &filesystem) != nil || filesystem.Flags&unix.ST_RDONLY != 0 {
		return errWorkspaceObjectUnconfirmed
	}
	expected := task.Workspace.Target.VolumeReady
	if expected == nil {
		return errWorkspaceObjectUnconfirmed
	}
	ready, err := prepareWorkspaceVolumeAt(ctx, root, expected.Preparation,
		securefile.OwnedRecordIdentity{UID: 10001, GID: 29000}, 0, true)
	if err != nil || ready != *expected {
		return errWorkspaceObjectUnconfirmed
	}
	return prepareWorkspaceRestoreGenerationAt(ctx, root, task, task.Workspace.PVCUID, securefile.OwnedRecordIdentity{UID: 10001, GID: 29000})
}

// restoreDirectoryNames reads a bounded independent directory cursor, leaving the caller's descriptor and cursor untouched.
// restoreDirectoryNames читает bounded independent directory cursor, сохраняя исходные descriptor/cursor caller.
func restoreDirectoryNames(ctx context.Context, directory, maximum int) ([]string, error) {
	if ctx == nil || ctx.Err() != nil || maximum < 1 || maximum > 3 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	fd, err := unix.Openat(directory, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	file := os.NewFile(uintptr(fd), "unpublished-generation-directory")
	names, err := file.Readdirnames(maximum)
	closeErr := file.Close()
	if err != nil && !errors.Is(err, io.EOF) || closeErr != nil || ctx.Err() != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	return names, nil
}

// prepareWorkspaceRestoreGenerationAt prepares only the owner-selected new generation and never overwrites or clears uncertain state.
// Matching intent plus an empty unpublished stage may resume initialization; every other existing entry fails closed.
// The trusted production opener fixes IDs and PVC custody. Tests explicitly use their own unprivileged identity.
// prepareWorkspaceRestoreGenerationAt подготавливает только owner-selected generation без перезаписи/очистки uncertain state.
// Matching intent с empty unpublished stage допускает повтор init; остальные existing entries закрыто отклоняются.
// Trusted production opener фиксирует IDs/PVC custody; tests явно используют собственную unprivileged identity.
func prepareWorkspaceRestoreGenerationAt(ctx context.Context, root int, task model.Task, pvcUID string, owner securefile.OwnedRecordIdentity) error {
	intent, err := workspaceRestoreIntentBytes(task, pvcUID)
	if err != nil || ctx == nil || ctx.Err() != nil || uint32(os.Geteuid()) != owner.UID || task.Workspace.ReconcileOnly ||
		task.Workspace.PVCUID != pvcUID {
		return errWorkspaceObjectUnconfirmed
	}
	subpath, err := task.Workspace.Target.GenerationSubPath()
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	current, err := unix.Openat(root, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	defer func() { _ = unix.Close(current) }()
	parts := strings.Split(subpath, "/")
	for _, name := range parts[:len(parts)-1] {
		next, _, err := securefile.OpenOwnedDirectoryAt(ctx, current, name, 0o700, owner, true)
		if err != nil {
			return errWorkspaceObjectUnconfirmed
		}
		previous := current
		current = next
		if unix.Close(previous) != nil {
			return errWorkspaceObjectUnconfirmed
		}
	}
	names, err := restoreDirectoryNames(ctx, current, 3)
	if err != nil || len(names) > 2 {
		return errWorkspaceObjectUnconfirmed
	}
	hasIntent := false
	for _, name := range names {
		if name != workspaceRestoreIntentFile && name != workspaceRestoreStagingTree {
			return errWorkspaceObjectUnconfirmed
		}
		hasIntent = hasIntent || name == workspaceRestoreIntentFile
	}
	previous, err := securefile.ReadOwnedRecordAt(ctx, current, workspaceRestoreIntentFile, runtimecontract.MaximumWorkspaceManifestBytes, owner)
	if errors.Is(err, unix.ENOENT) {
		if len(names) != 0 || securefile.WriteOwnedRecordAt(ctx, current, workspaceRestoreIntentFile, intent,
			runtimecontract.MaximumWorkspaceManifestBytes, owner) != nil {
			return errWorkspaceObjectUnconfirmed
		}
	} else if err != nil || !hasIntent || !bytes.Equal(previous, intent) {
		return errWorkspaceObjectUnconfirmed
	}
	stage, _, err := securefile.OpenOwnedDirectoryAt(ctx, current, workspaceRestoreStagingTree, 0o2770, owner, true)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	names, readErr := restoreDirectoryNames(ctx, stage, 1)
	closeErr := unix.Close(stage)
	if readErr != nil || len(names) != 0 || closeErr != nil || unix.Fsync(current) != nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}
