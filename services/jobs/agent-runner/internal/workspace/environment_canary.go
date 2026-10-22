package workspace

import (
	"context"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// RunEnvironmentCanary validates normalized policy digests and the root, then locks and probes only declared environment volumes using distinct mount IDs. Empty volume lists succeed without a probe; network/RBAC digests are not proof of live network or RBAC enforcement.
// RunEnvironmentCanary проверяет нормализованные дайджесты policy и root, затем блокирует и пробует лишь объявленные тома с отдельными mount IDs. Пустой список успешен без пробы; дайджесты network/RBAC не доказывают фактическое сетевое или RBAC-ограничение.
func RunEnvironmentCanary(ctx context.Context, root string, policy runtimecontract.RuntimeEnvironmentPolicy) error {
	normalized, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if ctx.Err() != nil || root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || err != nil ||
		policy.ResourcesDigest != normalized.ResourcesDigest || policy.VolumesDigest != normalized.VolumesDigest ||
		policy.NetworkDigest != normalized.NetworkDigest || policy.RBACDigest != normalized.RBACDigest {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if len(normalized.Volumes) == 0 {
		return nil
	}
	lock, err := Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	parent, err := openDirectory(root, ".kodex/volumes")
	if err != nil {
		return classify(err)
	}
	defer unix.Close(parent)
	var parentStat unix.Statx_t
	if err := unix.Statx(parent, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &parentStat); err != nil || parentStat.Mask&unix.STATX_MNT_ID == 0 {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	for _, volume := range normalized.Volumes {
		if err := probeEnvironmentVolume(ctx, parent, parentStat.Mnt_id, volume); err != nil {
			return err
		}
	}
	return nil
}

// probeEnvironmentVolume opens a declared child without following symlinks, requires statx mount identity distinct from the parent and an acceptable writable filesystem, then runs the private-directory canary without listing existing volume files.
// probeEnvironmentVolume открывает объявленный дочерний каталог без symlinks, требует отдельный от родителя mount identity statx и допустимую writable ФС, затем запускает приватную пробу без перечисления существующих файлов тома.
func probeEnvironmentVolume(ctx context.Context, parent int, parentMountID uint64, volume runtimecontract.RuntimeVolume) error {
	if ctx.Err() != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	directory, err := unix.Openat(parent, volume.Name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return classify(err)
	}
	defer unix.Close(directory)
	var stat unix.Statfs_t
	var mount unix.Statx_t
	if err := unix.Fstatfs(directory, &stat); err != nil {
		return classify(err)
	}
	if err := unix.Statx(directory, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &mount); err != nil || mount.Mask&unix.STATX_MNT_ID == 0 || mount.Mnt_id == parentMountID {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	if !validEnvironmentFilesystem(stat, volume) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return runDirectoryCanary(ctx, directory)
}

// validEnvironmentFilesystem rejects read-only or degenerate filesystems, requires bounded tmpfs capacity for memory volumes and non-tmpfs for disk volumes. Disk sizeLimit enforcement belongs to kubelet/admission, not statfs.
// validEnvironmentFilesystem отклоняет read-only и вырожденные ФС, требует ограниченную ёмкость tmpfs для memory-томов и не-tmpfs для disk. Контроль disk sizeLimit принадлежит kubelet/admission, не statfs.
func validEnvironmentFilesystem(stat unix.Statfs_t, volume runtimecontract.RuntimeVolume) bool {
	if stat.Flags&unix.ST_RDONLY != 0 || stat.Bsize <= 0 || stat.Blocks == 0 || volume.SizeMiB < 16 || volume.SizeMiB > 10240 {
		return false
	}
	switch volume.Kind {
	case runtimecontract.RuntimeVolumeEphemeralMemory:
		// tmpfs может быть меньше sizeLimit из-за node/container memory budget.
		maximum := uint64(volume.SizeMiB) << 20
		return stat.Type == unix.TMPFS_MAGIC && uint64(stat.Bsize) <= maximum && stat.Blocks <= maximum/uint64(stat.Bsize)
	case runtimecontract.RuntimeVolumeEphemeralDisk:
		// Disk emptyDir sizeLimit принадлежит kubelet, а не statfs quota.
		// Exact лимит проверяется admission/readback Pod, не этой canary.
		return stat.Type != unix.TMPFS_MAGIC && volume.SizeMiB >= 16 && volume.SizeMiB <= 10240
	default:
		return false
	}
}
