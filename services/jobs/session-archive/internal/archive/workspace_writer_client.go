package archive

import (
	"context"
	"io"
	"net"
	"os"
	"sync"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// workspaceFDWriter serializes one authenticated provider connection and owns only a duplicate unpublished tree descriptor.
// It accepts no arbitrary command or UID and cannot grant write credit to a reconciliation-only task.
// workspaceFDWriter последовательно обслуживает provider connection и владеет только копией unpublished tree descriptor.
// Он не принимает arbitrary command/UID и не выдаёт write credit reconciliation-only task.
type workspaceFDWriter struct {
	mu         sync.Mutex
	ctx        context.Context
	connection *net.UnixConn
	root       int
	request    workspaceWriterRequest
	identity   workspaceRestoreIdentity
	stop       func()
	pending    *workspaceRemoteRestoreFile
	closed     bool
	failed     bool
}

// workspaceRemoteRestoreFile bounds local data writes before explicit provider-side inode and durability completion.
// workspaceRemoteRestoreFile ограничивает local data writes до explicit provider-side inode/durability completion.
type workspaceRemoteRestoreFile struct {
	writer     *workspaceFDWriter
	file       *os.File
	size, used int64
	closed     bool
}

// newWorkspaceFDWriterPeer validates both actual kernel peer identity and exact task/root before returning any writer.
// The caller supplies trusted identities and the owned connection; failure closes that connection but not rootFD.
// newWorkspaceFDWriterPeer проверяет kernel peer identity и exact task/root до выдачи writer.
// Caller передаёт trusted identities и owned connection; отказ закрывает connection, но не rootFD.
func newWorkspaceFDWriterPeer(ctx context.Context, connection *net.UnixConn, rootFD int, task model.Task, identity workspaceRestoreIdentity) (*workspaceFDWriter, error) {
	if connection == nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = connection.Close()
		}
	}()
	if ctx == nil || ctx.Err() != nil || identity.actor != identity.role || uint32(os.Geteuid()) != identity.role {
		return nil, errWorkspaceObjectUnconfirmed
	}
	deadline, bounded := ctx.Deadline()
	digest, err := workspaceWriterTaskDigest(task)
	peer, peerErr := workspaceReaderPeerUID(connection)
	var root unix.Stat_t
	var filesystem unix.Statfs_t
	if err != nil || peerErr != nil || peer != identity.provider || !bounded || connection.SetDeadline(deadline) != nil ||
		unix.Fstat(rootFD, &root) != nil || root.Uid != identity.role || root.Gid != identity.group ||
		root.Mode&unix.S_IFMT != unix.S_IFDIR || root.Mode&0o7777 != 0o2770 || unix.Fstatfs(rootFD, &filesystem) != nil || filesystem.Flags&unix.ST_RDONLY != 0 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	copyFD, err := unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	stop := watchWorkspaceReaderCancel(ctx, connection)
	request := workspaceWriterRequest{taskSHA256: digest, device: uint64(root.Dev), inode: root.Ino, operation: workspaceWriterHello}
	if sendWorkspaceWriterRequest(connection, request) != nil || receiveWorkspaceReaderHello(connection) != nil {
		stop()
		_ = unix.Close(copyFD)
		return nil, errWorkspaceObjectUnconfirmed
	}
	accepted = true
	return &workspaceFDWriter{ctx: ctx, connection: connection, root: copyFD, request: request, identity: identity, stop: stop}, nil
}

// usable closes the entire attempt on cancellation or any prior uncertain command, never retrying a create or completion.
// usable закрывает attempt при отмене или uncertain command без повторного create/completion.
func (writer *workspaceFDWriter) usable(ctx context.Context) bool {
	return writer != nil && !writer.closed && !writer.failed && ctx != nil && ctx.Err() == nil && writer.ctx.Err() == nil
}

// fail poisons the connection while keeping every destination byte for authoritative reconciliation or repair.
// fail закрывает connection и сохраняет destination bytes для authoritative reconciliation/repair.
func (writer *workspaceFDWriter) fail() error {
	writer.failed = true
	_ = writer.connection.Close()
	return errWorkspaceObjectUnconfirmed
}

// abort closes an uncommitted connection without sending a successful FINISH and releases all owned resources.
// abort закрывает uncommitted connection без successful FINISH и освобождает owned resources.
func (writer *workspaceFDWriter) abort() {
	if writer == nil {
		return
	}
	writer.mu.Lock()
	if !writer.closed {
		_ = writer.fail()
	}
	writer.mu.Unlock()
	_ = writer.Close()
}

// command sends one sequential descriptor-free operation and requires its exact acknowledgement under request cancellation.
// Its caller holds the mutex and separately checks whether a file completion is currently permitted.
// command отправляет sequential descriptor-free operation и требует exact ACK с учётом request cancellation.
// Caller держит mutex и отдельно проверяет допустимость file completion.
func (writer *workspaceFDWriter) command(ctx context.Context, operation byte, name string, mode uint32, size int64) error {
	if !writer.usable(ctx) {
		return writer.fail()
	}
	request := writer.request
	request.operation, request.path, request.mode, request.size = operation, name, mode, size
	stop := watchWorkspaceReaderCancel(ctx, writer.connection)
	defer stop()
	if sendWorkspaceWriterRequest(writer.connection, request) != nil || receiveWorkspaceReaderHello(writer.connection) != nil || !writer.usable(ctx) {
		return writer.fail()
	}
	return nil
}

// Directory creates one provider-owned temporary directory through the dedicated authenticated protocol.
// Directory создаёт provider-owned temporary directory через отдельный authenticated protocol.
func (writer *workspaceFDWriter) Directory(ctx context.Context, name string) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.pending != nil {
		return writer.fail()
	}
	return writer.command(ctx, workspaceWriterDirectory, name, 0, 0)
}

// File obtains exactly one independently verified write-only provider descriptor and retains bounded local byte accounting.
// File получает independently verified write-only provider descriptor и сохраняет bounded local byte accounting.
func (writer *workspaceFDWriter) File(ctx context.Context, name string, mode uint32, size int64) (io.WriteCloser, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.usable(ctx) || writer.pending != nil {
		return nil, writer.fail()
	}
	request := writer.request
	request.operation, request.path, request.mode, request.size = workspaceWriterFile, name, mode, size
	stop := watchWorkspaceReaderCancel(ctx, writer.connection)
	defer stop()
	if sendWorkspaceWriterRequest(writer.connection, request) != nil {
		return nil, writer.fail()
	}
	file, err := receiveWorkspaceWriterFile(writer.connection, writer.root, name, writer.identity)
	if err != nil {
		return nil, writer.fail()
	}
	if !writer.usable(ctx) {
		_ = file.Close()
		return nil, writer.fail()
	}
	pending := &workspaceRemoteRestoreFile{writer: writer, file: file, size: size}
	writer.pending = pending
	return pending, nil
}

// SealDirectory applies the original provider directory mode only after its descendant files have closed.
// SealDirectory возвращает исходный provider directory mode после закрытия descendant files.
func (writer *workspaceFDWriter) SealDirectory(ctx context.Context, name string, mode uint32) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.pending != nil {
		return writer.fail()
	}
	return writer.command(ctx, workspaceWriterSeal, name, mode, 0)
}

// Write never acknowledges bytes beyond the assigned file size and poisons the whole restore on a partial write.
// Write не подтверждает bytes сверх assigned file size и закрывает restore при partial write.
func (file *workspaceRemoteRestoreFile) Write(raw []byte) (int, error) {
	writer := file.writer
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if file.closed || !writer.usable(writer.ctx) || writer.pending != file || int64(len(raw)) > file.size-file.used {
		return 0, writer.fail()
	}
	n, err := file.file.Write(raw)
	file.used += int64(n)
	if err != nil || n != len(raw) {
		return n, writer.fail()
	}
	return n, nil
}

// Close closes the local data descriptor before requesting exact provider-side completion; an uncertain ACK is never resent.
// Close закрывает local data descriptor до provider-side completion; uncertain ACK никогда не отправляется повторно.
func (file *workspaceRemoteRestoreFile) Close() error {
	writer := file.writer
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if file.closed {
		return errWorkspaceObjectUnconfirmed
	}
	file.closed = true
	valid := writer.usable(writer.ctx) && writer.pending == file && file.used == file.size && file.file.Sync() == nil
	closeErr := file.file.Close()
	if !valid || closeErr != nil || writer.command(writer.ctx, workspaceWriterComplete, "", 0, 0) != nil {
		return writer.fail()
	}
	writer.pending = nil
	return nil
}

// Close finishes the dedicated provider adapter only when every file has completed, then joins watchers and releases owned descriptors.
// Failed or pending work closes the connection without a successful finish; rootFD supplied by the caller remains open.
// Close завершает provider adapter после всех files, затем завершает watchers и освобождает owned descriptors.
// Failed/pending work закрывает connection без successful finish; исходный rootFD caller остаётся открыт.
func (writer *workspaceFDWriter) Close() error {
	if writer == nil {
		return nil
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		if writer.failed {
			return errWorkspaceObjectUnconfirmed
		}
		return nil
	}
	valid := writer.usable(writer.ctx) && writer.pending == nil && writer.command(writer.ctx, workspaceWriterFinish, "", 0, 0) == nil &&
		receiveWorkspaceWriterJoin(writer.connection) == nil && writer.usable(writer.ctx)
	writer.closed = true
	if writer.pending != nil && !writer.pending.closed {
		writer.pending.closed = true
		_ = writer.pending.file.Close()
	}
	connectionErr := writer.connection.Close()
	rootErr := unix.Close(writer.root)
	writer.stop()
	if !valid || connectionErr != nil || rootErr != nil {
		writer.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}
