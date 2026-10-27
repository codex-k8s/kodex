package archive

import (
	"context"
	"io"
	"net"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// watchWorkspaceReaderCancel closes one owned socket on cancellation and joins its only watcher on return.
// The returned stop function has one owner; no database, source descriptor or unrelated resource is closed.
// watchWorkspaceReaderCancel закрывает owned socket при отмене и завершает единственный watcher до возврата.
// Stop function имеет одного owner; database/source descriptor/чужой resource не закрывается.
func watchWorkspaceReaderCancel(ctx context.Context, connection io.Closer) func() {
	stop, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-stop:
		}
	}()
	return func() { close(stop); <-joined }
}

// serveWorkspaceReaderPeer processes only one exact peer/task/root under the caller's existing deadline.
// It owns the connection but not rootFD; a complete handshake precedes all descriptor grants.
// Fixed production IDs are supplied by the worker wrapper, never by a request field.
// serveWorkspaceReaderPeer обслуживает exact peer/task/root в пределах существующего caller deadline.
// Connection принадлежит helper, rootFD — caller; handshake предшествует выдаче descriptors.
// Production IDs задаёт worker wrapper, не request field.
func serveWorkspaceReaderPeer(ctx context.Context, connection *net.UnixConn, rootFD int, taskSHA [32]byte, mainUID, sourceUID, sourceGID uint32, maximumFiles int64) error {
	if ctx == nil || connection == nil || rootFD < 0 || ctx.Err() != nil || maximumFiles < 1 || maximumFiles > runtimecontract.RuntimeWorkspaceMaximumFiles {
		return errWorkspaceReaderInvalid
	}
	defer connection.Close()
	stop := watchWorkspaceReaderCancel(ctx, connection)
	defer stop()
	deadline, bounded := ctx.Deadline()
	if !bounded || connection.SetDeadline(deadline) != nil {
		return errWorkspaceReaderInvalid
	}
	peer, err := workspaceReaderPeerUID(connection)
	var root unix.Stat_t
	if err != nil || peer != mainUID || unix.Fstat(rootFD, &root) != nil || root.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errWorkspaceReaderInvalid
	}
	hello, opened := false, int64(0)
	for {
		payload := make([]byte, workspaceReaderMaximumFrame)
		control := make([]byte, unix.CmsgSpace(4))
		n, controlBytes, flags, _, readErr := connection.ReadMsgUnix(payload, control)
		descriptors, count, valid := workspaceReaderDescriptors(control[:controlBytes])
		for _, fd := range descriptors {
			_ = unix.Close(fd)
		}
		if readErr == io.EOF || n == 0 && readErr == nil {
			if hello && ctx.Err() == nil && valid && count == 0 && len(descriptors) == 0 && controlBytes == 0 {
				return nil
			}
			return errWorkspaceReaderInvalid
		}
		if readErr != nil || ctx.Err() != nil || !valid || count != 0 || len(descriptors) != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			return errWorkspaceReaderInvalid
		}
		request, err := decodeWorkspaceReaderRequest(payload[:n])
		var current unix.Stat_t
		if err != nil || request.taskSHA256 != taskSHA || request.device != uint64(root.Dev) || request.inode != root.Ino ||
			unix.Fstat(rootFD, &current) != nil || !workspaceTreeIdentityEqual(root, current) {
			return errWorkspaceReaderInvalid
		}
		if request.hello {
			if hello {
				return errWorkspaceReaderInvalid
			}
			n, controlBytes, err := connection.WriteMsgUnix([]byte{2}, nil, nil)
			if err != nil || n != 1 || controlBytes != 0 {
				return errWorkspaceReaderInvalid
			}
			hello = true
			continue
		}
		opened++
		if !hello || opened > 2*maximumFiles || sendWorkspaceReaderSource(connection, rootFD, request.path, sourceUID, sourceGID) != nil {
			return errWorkspaceReaderInvalid
		}
	}
}

// sendWorkspaceReaderSource opens only a relative owned regular file beneath the fixed read-only work root.
// Sender closes its original FD after SCM_RIGHTS; the receiver independently checks inode and access flags.
// sendWorkspaceReaderSource открывает relative owned regular file под fixed read-only work root.
// Sender закрывает original FD после SCM_RIGHTS; receiver отдельно проверяет inode/access flags.
func sendWorkspaceReaderSource(connection *net.UnixConn, rootFD int, path string, sourceUID, sourceGID uint32) error {
	if connection == nil || rootFD < 0 || !workspaceStreamPath(path) {
		return errWorkspaceReaderInvalid
	}
	fd, err := unix.Openat2(rootFD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != sourceUID || info.Gid != sourceGID || info.Mode&unix.S_IFMT != unix.S_IFREG ||
		info.Nlink != 1 || info.Mode&0o7000 != 0 {
		return errWorkspaceReaderInvalid
	}
	rights := unix.UnixRights(fd)
	n, controlBytes, err := connection.WriteMsgUnix([]byte{1}, rights, nil)
	if err != nil || n != 1 || controlBytes != len(rights) {
		return errWorkspaceReaderInvalid
	}
	return nil
}

// workspaceReaderHello binds task/root before the main worker may write any object or begin a canonical stream.
// workspaceReaderHello связывает task/root до object write или начала canonical stream у main worker.
func workspaceReaderHello(connection *net.UnixConn, request workspaceReaderRequest, expectedPeer uint32) error {
	peer, err := workspaceReaderPeerUID(connection)
	if err != nil || peer != expectedPeer {
		return errWorkspaceReaderInvalid
	}
	request.path, request.hello = "", true
	raw, err := encodeWorkspaceReaderRequest(request)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	n, controlBytes, err := connection.WriteMsgUnix(raw, nil, nil)
	if err != nil || n != len(raw) || controlBytes != 0 {
		return errWorkspaceReaderInvalid
	}
	return receiveWorkspaceReaderHello(connection)
}

// openWorkspaceReaderSource requests one exact known inode and returns only a read-only owned descriptor.
// The private connection is sequential; callers close the returned file after bounded source consumption.
// openWorkspaceReaderSource запрашивает exact known inode и возвращает только read-only owned descriptor.
// Private connection последовательна; caller закрывает файл после bounded source consumption.
func openWorkspaceReaderSource(connection *net.UnixConn, request workspaceReaderRequest, expected unix.Stat_t, peerUID, sourceUID, sourceGID uint32) (*os.File, error) {
	peer, err := workspaceReaderPeerUID(connection)
	if err != nil || peer != peerUID || expected.Uid != sourceUID || expected.Gid != sourceGID ||
		expected.Mode&unix.S_IFMT != unix.S_IFREG || expected.Nlink != 1 || expected.Mode&0o7000 != 0 {
		return nil, errWorkspaceReaderInvalid
	}
	request.hello = false
	raw, err := encodeWorkspaceReaderRequest(request)
	if err != nil {
		return nil, errWorkspaceReaderInvalid
	}
	n, controlBytes, err := connection.WriteMsgUnix(raw, nil, nil)
	if err != nil || n != len(raw) || controlBytes != 0 {
		return nil, errWorkspaceReaderInvalid
	}
	return receiveWorkspaceReaderFile(connection, expected)
}
