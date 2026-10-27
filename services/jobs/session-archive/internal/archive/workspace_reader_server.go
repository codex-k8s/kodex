package archive

import (
	"context"
	"errors"
	"net"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// workspaceReaderSocketName is the single published socket entry inside the task-private directory.
	// workspaceReaderSocketName — единственный published socket entry внутри task-private directory.
	workspaceReaderSocketName = "reader.sock"
	// workspaceReaderPreparingSocketName remains hidden until exact permissions and ownership have been checked.
	// workspaceReaderPreparingSocketName скрыт до проверки exact permissions и ownership.
	workspaceReaderPreparingSocketName = ".reader-preparing.sock"
)

// ServeWorkspaceReader owns a fixed UID10002 read-only peer without API tokens, source writes or storage credentials.
// Its only accepted connection has the fixed main UID and matching task/root; cancellation joins all socket work.
// ServeWorkspaceReader владеет fixed UID10002 read-only peer без API tokens/source writes/storage credentials.
// Единственный connection требует fixed main UID и matching task/root; cancel завершает socket work.
func ServeWorkspaceReader(ctx context.Context, task model.Task) error {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != int(workspaceReaderProviderUID) {
		return errWorkspaceReaderInvalid
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errWorkspaceReaderInvalid
	}
	digest, err := workspaceReaderTaskDigest(task)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	rootFD, err := OpenWorkspaceSnapshotRoot(task)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	defer unix.Close(rootFD)
	parentFD, err := unix.Open(WorkspaceReaderSocketDirectory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	defer unix.Close(parentFD)
	var parent unix.Stat_t
	if unix.Fstat(parentFD, &parent) != nil || parent.Uid != workspaceReaderRoleUID || parent.Gid != workspaceReaderGroup ||
		parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&0o7777 != 0o2770 {
		return errWorkspaceReaderInvalid
	}
	var old unix.Stat_t
	if err := unix.Fstatat(parentFD, workspaceReaderSocketName, &old, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return errWorkspaceReaderInvalid
	}
	preparing := WorkspaceReaderSocketDirectory + "/" + workspaceReaderPreparingSocketName
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: preparing, Net: "unixpacket"})
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	listener.SetUnlinkOnClose(false)
	currentName := workspaceReaderPreparingSocketName
	var socket unix.Stat_t
	if unix.Fstatat(parentFD, currentName, &socket, unix.AT_SYMLINK_NOFOLLOW) != nil {
		_ = listener.Close()
		return errWorkspaceReaderInvalid
	}
	defer func() {
		_ = listener.Close()
		removeWorkspaceReaderSocket(parentFD, currentName, socket)
	}()
	if unix.Fchmodat(parentFD, currentName, 0o660, 0) != nil || unix.Fstatat(parentFD, currentName, &socket, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		socket.Mode&unix.S_IFMT != unix.S_IFSOCK || socket.Uid != workspaceReaderProviderUID || socket.Gid != workspaceReaderGroup ||
		socket.Mode&0o7777 != 0o660 || socket.Nlink != 1 ||
		unix.Renameat2(parentFD, currentName, parentFD, workspaceReaderSocketName, unix.RENAME_NOREPLACE) != nil {
		return errWorkspaceReaderInvalid
	}
	currentName = workspaceReaderSocketName
	stop := watchWorkspaceReaderCancel(ctx, listener)
	defer stop()
	connection, err := listener.AcceptUnix()
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	return serveWorkspaceReaderPeer(ctx, connection, rootFD, digest, workspaceReaderRoleUID, workspaceReaderProviderUID,
		workspaceReaderGroup, task.Workspace.Policy.MaximumFileCount)
}

// removeWorkspaceReaderSocket unlinks only the still-identical locally created socket after its peer has stopped.
// Existing or replaced socket entries are preserved; source files and model generations are never touched.
// removeWorkspaceReaderSocket удаляет только unchanged locally created socket после остановки peer.
// Existing/replaced socket entries сохраняются; source files/model generations не затрагиваются.
func removeWorkspaceReaderSocket(parentFD int, name string, expected unix.Stat_t) {
	var current unix.Stat_t
	if (name == workspaceReaderSocketName || name == workspaceReaderPreparingSocketName) &&
		unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW) == nil && current.Mode&unix.S_IFMT == unix.S_IFSOCK &&
		current.Dev == expected.Dev && current.Ino == expected.Ino && current.Uid == workspaceReaderProviderUID && current.Nlink == 1 {
		_ = unix.Unlinkat(parentFD, name, 0)
	}
}
