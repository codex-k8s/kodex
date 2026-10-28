package workspace

import (
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// OpenBoundWorkDirectory permits exactly the owner-bound work mount while descendants retain NO_XDEV.
// The read-only inode proof is local evidence, never a substitute for owner BIND or Pod/PVC readback.
// OpenBoundWorkDirectory допускает exact owner-bound work mount, сохраняя NO_XDEV для descendants.
// Read-only inode proof — local evidence, не замена owner BIND или Pod/PVC readback.
func OpenBoundWorkDirectory(rootFD int, input runtimecontract.RunnerInput) (int, error) {
	return openBoundWorkDirectory(rootFD, input, runtimecontract.WorkspaceMountProofPath, 10001, 29000)
}

// openBoundWorkDirectory confines the one mount exception to literal work and validates protected metadata.
// Its proof path and IDs are trusted production/test parameters, never values from model requests.
// openBoundWorkDirectory ограничивает mount exception literal work и проверяет protected metadata.
// Proof path и IDs — trusted production/test parameters, не поля model requests.
func openBoundWorkDirectory(rootFD int, input runtimecontract.RunnerInput, proofPath string, runnerUID, group uint32) (int, error) {
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	if rootFD < 0 || input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		return -1, denied
	}
	proofFD, err := unix.Open(proofPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, denied
	}
	proofFile := os.NewFile(uintptr(proofFD), "runtime-workspace-mount-proof")
	defer proofFile.Close()
	var proofInfo unix.Stat_t
	var proofFS unix.Statfs_t
	if unix.Fstat(proofFD, &proofInfo) != nil || proofInfo.Mode&unix.S_IFMT != unix.S_IFREG || proofInfo.Nlink != 1 ||
		proofInfo.Uid != runnerUID || proofInfo.Gid != group || proofInfo.Mode&0o7777 != 0o440 ||
		proofInfo.Size < 1 || proofInfo.Size > runtimecontract.MaximumWorkspaceMountProofBytes ||
		unix.Fstatfs(proofFD, &proofFS) != nil || proofFS.Flags&unix.ST_RDONLY == 0 {
		return -1, denied
	}
	raw, err := io.ReadAll(io.LimitReader(proofFile, runtimecontract.MaximumWorkspaceMountProofBytes+1))
	if err != nil || int64(len(raw)) != proofInfo.Size {
		return -1, denied
	}
	proof, err := runtimecontract.DecodeWorkspaceMountProof(raw)
	if err != nil {
		return -1, denied
	}
	// Только этот фиксированный переход разрешает crossing; source path не задаёт caller.
	fd, err := unix.Openat2(rootFD, "work", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return -1, denied
	}
	var info unix.Stat_t
	var rootMount, workMount unix.Statx_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != runnerUID ||
		info.Gid != group || info.Mode&0o7777 != 0o2770 || !proof.Matches(input, uint64(info.Dev), info.Ino) ||
		unix.Statx(rootFD, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &rootMount) != nil ||
		unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &workMount) != nil ||
		rootMount.Mask&unix.STATX_MNT_ID == 0 || workMount.Mask&unix.STATX_MNT_ID == 0 || rootMount.Mnt_id == workMount.Mnt_id {
		unix.Close(fd)
		return -1, denied
	}
	return fd, nil
}
