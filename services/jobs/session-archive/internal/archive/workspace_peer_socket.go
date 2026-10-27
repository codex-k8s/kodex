package archive

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// errWorkspacePeerSocket hides task-private socket names and kernel diagnostic details from either protocol boundary.
// errWorkspacePeerSocket скрывает task-private socket names и kernel diagnostics на границе обоих protocols.
var errWorkspacePeerSocket = errors.New("session workspace socket is unconfirmed")

// workspacePeerListener owns one exact locally created IPC socket, not a data root or execution grant.
// workspacePeerListener владеет exact locally created IPC socket, не data root или execution grant.
type workspacePeerListener struct {
	listener *net.UnixListener
	parent   int
	name     string
	socket   unix.Stat_t
	created  bool
	closed   bool
	failed   bool
}

// workspacePeerSocketNames keeps read-only and writable endpoints separate without accepting a wire-selected filename.
// workspacePeerSocketNames разделяет read-only и writable endpoints без wire-selected filename.
func workspacePeerSocketNames(writable bool) (string, string) {
	if writable {
		return "writer.sock", ".writer-preparing.sock"
	}
	return "reader.sock", ".reader-preparing.sock"
}

// newWorkspacePeerListener prepares one no-replace socket below the already authorized task-private IPC directory.
// Production callers fix paths and IDs; filesystem fixtures explicitly supply their own unprivileged process identity.
// newWorkspacePeerListener подготавливает no-replace socket под заранее authorized task-private IPC directory.
// Production callers фиксируют paths/IDs; filesystem fixtures явно передают свою unprivileged identity.
func newWorkspacePeerListener(ctx context.Context, directory string, writable bool, roleUID, providerUID, group uint32) (*workspacePeerListener, error) {
	if ctx == nil || ctx.Err() != nil || uint32(os.Geteuid()) != providerUID || !filepath.IsAbs(directory) ||
		filepath.Clean(directory) != directory || strings.ContainsRune(directory, 0) {
		return nil, errWorkspacePeerSocket
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, errWorkspacePeerSocket
	}
	name, preparing := workspacePeerSocketNames(writable)
	if len(directory)+1+len(preparing) >= len(unix.RawSockaddrUnix{}.Path) {
		return nil, errWorkspacePeerSocket
	}
	parent, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkspacePeerSocket
	}
	value := &workspacePeerListener{parent: parent, name: preparing}
	accepted := false
	defer func() {
		if !accepted {
			_ = value.Close()
		}
	}()
	var info, old unix.Stat_t
	if unix.Fstat(parent, &info) != nil || info.Uid != roleUID || info.Gid != group || info.Mode&0o7777 != 0o2770 ||
		!errors.Is(unix.Fstatat(parent, name, &old, unix.AT_SYMLINK_NOFOLLOW), unix.ENOENT) ||
		!errors.Is(unix.Fstatat(parent, preparing, &old, unix.AT_SYMLINK_NOFOLLOW), unix.ENOENT) {
		return nil, errWorkspacePeerSocket
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: directory + "/" + preparing, Net: "unixpacket"})
	if err != nil {
		return nil, errWorkspacePeerSocket
	}
	value.listener = listener
	listener.SetUnlinkOnClose(false)
	if unix.Fstatat(parent, preparing, &value.socket, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		value.socket.Mode&unix.S_IFMT != unix.S_IFSOCK || value.socket.Uid != providerUID || value.socket.Gid != group ||
		value.socket.Dev != info.Dev || value.socket.Nlink != 1 {
		return nil, errWorkspacePeerSocket
	}
	value.created = true
	if unix.Fchmodat(parent, preparing, 0o660, 0) != nil || unix.Fstatat(parent, preparing, &info, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		info.Dev != value.socket.Dev || info.Ino != value.socket.Ino || info.Uid != providerUID || info.Gid != group ||
		info.Mode&unix.S_IFMT != unix.S_IFSOCK || info.Mode&0o7777 != 0o660 || info.Nlink != 1 ||
		unix.Renameat2(parent, preparing, parent, name, unix.RENAME_NOREPLACE) != nil {
		return nil, errWorkspacePeerSocket
	}
	value.name = name
	if unix.Fstatat(parent, name, &info, unix.AT_SYMLINK_NOFOLLOW) != nil || info.Dev != value.socket.Dev || info.Ino != value.socket.Ino {
		return nil, errWorkspacePeerSocket
	}
	value.socket = info
	accepted = true
	return value, nil
}

// Accept waits for one connection under the caller's bounded lifecycle and joins its cancellation watcher before returning.
// Accept ждёт один connection в bounded caller lifecycle и завершает cancellation watcher до возврата.
func (listener *workspacePeerListener) Accept(ctx context.Context) (*net.UnixConn, error) {
	if listener == nil || listener.closed || listener.listener == nil || ctx == nil || ctx.Err() != nil {
		return nil, errWorkspacePeerSocket
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || listener.listener.SetDeadline(deadline) != nil {
		return nil, errWorkspacePeerSocket
	}
	stop := watchWorkspaceReaderCancel(ctx, listener.listener)
	defer stop()
	connection, err := listener.listener.AcceptUnix()
	if err != nil {
		return nil, errWorkspacePeerSocket
	}
	return connection, nil
}

// Close releases only this listener and its still-identical socket inode, preserving any replaced entry for diagnosis.
// Close освобождает только этот listener и его unchanged socket inode, сохраняя replaced entry для диагностики.
func (listener *workspacePeerListener) Close() error {
	if listener == nil {
		return nil
	}
	if listener.closed {
		if listener.failed {
			return errWorkspacePeerSocket
		}
		return nil
	}
	listener.closed = true
	if listener.listener != nil && listener.listener.Close() != nil {
		listener.failed = true
	}
	if listener.created {
		var current unix.Stat_t
		err := unix.Fstatat(listener.parent, listener.name, &current, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil && current.Mode&unix.S_IFMT == unix.S_IFSOCK && current.Dev == listener.socket.Dev &&
			current.Ino == listener.socket.Ino && current.Uid == listener.socket.Uid && current.Gid == listener.socket.Gid && current.Nlink == 1 {
			if unix.Unlinkat(listener.parent, listener.name, 0) != nil {
				listener.failed = true
			}
		} else if !errors.Is(err, unix.ENOENT) {
			listener.failed = true
		}
	}
	if unix.Close(listener.parent) != nil {
		listener.failed = true
	}
	if listener.failed {
		return errWorkspacePeerSocket
	}
	return nil
}

// dialWorkspacePeer checks a fixed socket's actual inode/mode and kernel peer identity without repeating a protocol effect.
// Only absent or not-yet-listening startup is polled; every other drift closes the attempt immediately.
// dialWorkspacePeer проверяет socket inode/mode и kernel peer identity без повторения protocol effect.
// Повторяется только absent/not-yet-listening startup; остальные drifts немедленно закрывают attempt.
func dialWorkspacePeer(ctx context.Context, directory string, writable bool, roleUID, providerUID, group uint32) (*net.UnixConn, error) {
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsRune(directory, 0) {
		return nil, errWorkspacePeerSocket
	}
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return nil, errWorkspacePeerSocket
	}
	name, _ := workspacePeerSocketNames(writable)
	socketPath := directory + "/" + name
	var parent unix.Stat_t
	if unix.Lstat(directory, &parent) != nil || parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&0o7777 != 0o2770 ||
		parent.Uid != roleUID || parent.Gid != group {
		return nil, errWorkspacePeerSocket
	}
	for ctx.Err() == nil {
		var before unix.Stat_t
		statErr := unix.Lstat(socketPath, &before)
		if statErr == nil {
			if before.Mode&unix.S_IFMT != unix.S_IFSOCK || before.Uid != providerUID || before.Gid != group || before.Mode&0o7777 != 0o660 || before.Nlink != 1 {
				return nil, errWorkspacePeerSocket
			}
			connection, err := (&net.Dialer{}).DialContext(ctx, "unixpacket", socketPath)
			if err == nil {
				peer, ok := connection.(*net.UnixConn)
				var after, currentParent unix.Stat_t
				if !ok || unix.Lstat(socketPath, &after) != nil || before.Dev != after.Dev || before.Ino != after.Ino ||
					after.Uid != before.Uid || after.Gid != before.Gid || after.Mode != before.Mode || after.Nlink != before.Nlink ||
					unix.Lstat(directory, &currentParent) != nil || parent.Dev != currentParent.Dev || parent.Ino != currentParent.Ino ||
					parent.Mode != currentParent.Mode || parent.Uid != currentParent.Uid || parent.Gid != currentParent.Gid || peer.SetDeadline(deadline) != nil {
					_ = connection.Close()
					return nil, errWorkspacePeerSocket
				}
				uid, err := workspaceReaderPeerUID(peer)
				if err != nil || uid != providerUID {
					_ = peer.Close()
					return nil, errWorkspacePeerSocket
				}
				return peer, nil
			}
			if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.ECONNREFUSED) {
				return nil, errWorkspacePeerSocket
			}
		} else if !errors.Is(statErr, unix.ENOENT) {
			return nil, errWorkspacePeerSocket
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
	return nil, errWorkspacePeerSocket
}
