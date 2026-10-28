package workspace

import (
	"bytes"
	"context"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"golang.org/x/sys/unix"
)

// VerifyPreparedVolume reads the accepted sealed root before the first BIND and before any
// private write. It never creates metadata, normalizes permissions or accepts a replacement PVC.
// VerifyPreparedVolume читает accepted sealed root до первого BIND и до private write.
// Metadata не создаются, permissions не исправляются, replacement PVC не принимается.
func VerifyPreparedVolume(ctx context.Context, input runtimecontract.RunnerInput) error {
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 10001 || os.Getegid() != 29000 ||
		input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		return denied
	}
	root, err := unix.Open(BoundSessionWorkspaceMount, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	defer unix.Close(root)
	return verifyPreparedVolumeAt(ctx, root, input.WorkspaceStorage.VolumeReady, securefile.OwnedRecordIdentity{UID: 10001, GID: 29000}, 0)
}

// verifyPreparedVolumeAt verifies the already-authorized root descriptor and exact immutable
// record using read-only operations. Tests supply only their private directory's process IDs.
// verifyPreparedVolumeAt проверяет already-authorized root descriptor и exact immutable
// record только чтением. Tests задают process IDs лишь своего private directory.
func verifyPreparedVolumeAt(ctx context.Context, root int, ready *runtimecontract.WorkspaceVolumeReady, owner securefile.OwnedRecordIdentity, rootUID uint32) error {
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	if ctx == nil || ctx.Err() != nil || root < 0 || ready == nil || owner.UID != uint32(os.Geteuid()) {
		return denied
	}
	wanted, err := runtimecontract.EncodeWorkspaceVolumeReady(*ready)
	var before unix.Stat_t
	if err != nil || unix.Fstat(root, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Mode&0o7777 != 0o2777 ||
		before.Uid != rootUID || before.Gid != owner.GID || uint64(before.Dev) != ready.Device || before.Ino != ready.Inode {
		return denied
	}
	metadata, _, err := securefile.OpenOwnedDirectoryAt(ctx, root, runtimecontract.WorkspaceVolumeMetadataDirectory, 0o700, owner, false)
	if err != nil {
		return denied
	}
	defer unix.Close(metadata)
	raw, err := securefile.ReadOwnedRecordAt(ctx, metadata, runtimecontract.WorkspaceVolumeReadyFile, runtimecontract.MaximumWorkspaceVolumeReadyBytes, owner)
	var after unix.Stat_t
	if err != nil || !bytes.Equal(raw, wanted) || unix.Fstat(root, &after) != nil ||
		after.Dev != before.Dev || after.Ino != before.Ino || after.Mode != before.Mode || after.Uid != before.Uid || after.Gid != before.Gid {
		return denied
	}
	return nil
}
