package worker

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// errWorkspaceReaderWorker hides task paths and filesystem details at the minimal helper boundary.
// errWorkspaceReaderWorker скрывает task paths и filesystem details на границе минимального helper.
var errWorkspaceReaderWorker = errors.New("session workspace reader failed")

// ReadWorkspace runs only the fixed-UID read-only descriptor peer and never reads storage credentials.
// Its deadline is the existing archive worker budget; the root context is owned by the executable.
// ReadWorkspace запускает только fixed-UID read-only descriptor peer без чтения storage credentials.
// Deadline соответствует прежнему бюджету archive worker; root context принадлежит executable.
func ReadWorkspace(lifecycle context.Context) error {
	timeout := 8 * time.Minute
	if raw := os.Getenv("SESSION_ARCHIVE_WORKER_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Minute || value > 15*time.Minute {
			return errWorkspaceReaderWorker
		}
		timeout = value
	}
	if lifecycle == nil || lifecycle.Err() != nil || os.Geteuid() != 10002 {
		return errWorkspaceReaderWorker
	}
	task, err := model.DecodeFile("/var/run/config/kodex/session-archive/task.json")
	if err != nil || task.Workspace == nil || task.Kind != "SNAPSHOT" {
		return errWorkspaceReaderWorker
	}
	ctx, cancel := context.WithTimeout(lifecycle, timeout)
	defer cancel()
	if archive.ServeWorkspaceReader(ctx, task) != nil {
		return errWorkspaceReaderWorker
	}
	return nil
}

// PrepareWorkspaceReader creates only the new task-private socket directory in the bounded temporary volume.
// It owns no source mount; an existing entry or wrong group fails closed instead of chmodding foreign data.
// PrepareWorkspaceReader создаёт только новый task-private socket directory в bounded temporary volume.
// Source mount отсутствует; существующий entry или чужая группа отклоняются без chmod чужих данных.
func PrepareWorkspaceReader(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 10001 || os.Getegid() != 29000 {
		return errWorkspaceReaderWorker
	}
	parent, err := unix.Open("/tmp", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errWorkspaceReaderWorker
	}
	defer unix.Close(parent)
	if unix.Mkdirat(parent, "workspace-reader", 0o770) != nil {
		return errWorkspaceReaderWorker
	}
	child, err := unix.Openat2(parent, "workspace-reader", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return errWorkspaceReaderWorker
	}
	defer unix.Close(child)
	var info unix.Stat_t
	if unix.Fstat(child, &info) != nil || info.Uid != 10001 || info.Gid != 29000 || info.Mode&unix.S_IFMT != unix.S_IFDIR ||
		unix.Fchmod(child, 0o2770) != nil || unix.Fstat(child, &info) != nil || info.Mode&0o7777 != 0o2770 ||
		unix.Fsync(child) != nil || unix.Fsync(parent) != nil || ctx.Err() != nil {
		return errWorkspaceReaderWorker
	}
	return nil
}
