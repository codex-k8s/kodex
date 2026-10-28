package workspace

import (
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// ReadBoundWriterAcknowledgement requires the role-only read-only file subPath and exact input tuple.
// Local metadata alone does not grant writes; the first initializer obtained these bytes through owner BIND.
// ReadBoundWriterAcknowledgement требует role-only read-only file subPath и exact input tuple.
// Local metadata не выдаёт write grant: first initializer получил эти bytes через owner BIND.
func ReadBoundWriterAcknowledgement(input runtimecontract.RunnerInput) (runtimecontract.WorkspaceWriterAcknowledgement, error) {
	var empty runtimecontract.WorkspaceWriterAcknowledgement
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	fd, err := unix.Open(runtimecontract.WorkspaceWriterAcknowledgementPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return empty, denied
	}
	file := os.NewFile(uintptr(fd), "bound-workspace-owner-ack")
	defer file.Close()
	var info unix.Stat_t
	var filesystem unix.Statfs_t
	if unix.Fstat(fd, &info) != nil || unix.Fstatfs(fd, &filesystem) != nil || filesystem.Flags&unix.ST_RDONLY == 0 ||
		info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != 10001 || info.Gid != 29000 || info.Nlink != 1 ||
		info.Mode&0o7777 != 0o440 || info.Size < 1 || info.Size > runtimecontract.MaximumWorkspaceQuiescenceBytes {
		return empty, denied
	}
	raw, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumWorkspaceQuiescenceBytes+1))
	if err != nil || int64(len(raw)) != info.Size {
		return empty, denied
	}
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(raw)
	if err != nil || !ack.Matches(input) {
		return empty, denied
	}
	return ack, nil
}
