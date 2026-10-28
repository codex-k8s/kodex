package archive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net"
	"os"
	"sync"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// WorkspaceReaderSocketDirectory is the task-private socket subpath inside the existing bounded tmp volume.
	// WorkspaceReaderSocketDirectory — task-private socket subpath внутри прежнего bounded tmp volume.
	WorkspaceReaderSocketDirectory = "/var/run/kodex/work-reader"
	// WorkspaceReaderSocketPath is fixed by the producer and never selected by source files or task paths.
	// WorkspaceReaderSocketPath задаёт producer, не source files или task paths.
	WorkspaceReaderSocketPath = WorkspaceReaderSocketDirectory + "/reader.sock"
	// WorkspaceSnapshotRoot is the sole read-only work mount given to either snapshot consumer.
	// WorkspaceSnapshotRoot — единственный read-only work mount обоих snapshot consumers.
	WorkspaceSnapshotRoot = "/workspace/work"
	// workspaceReaderRoleUID pins the ordinary role-owned work identity and main archive consumer.
	// workspaceReaderRoleUID закрепляет ordinary role-owned work identity и main archive consumer.
	workspaceReaderRoleUID uint32 = 10001
	// workspaceReaderProviderUID pins the isolated provider-owned regular-file reader.
	// workspaceReaderProviderUID закрепляет isolated provider-owned regular-file reader.
	workspaceReaderProviderUID uint32 = 10002
	// workspaceReaderGroup pins only the existing shared work group; it grants no API or domain permission.
	// workspaceReaderGroup закрепляет existing shared work group, не API/domain permission.
	workspaceReaderGroup uint32 = 29000
)

// WorkspaceFDReader owns one sequential peer connection bound to the same immutable task and mounted root.
// A caller must close it before releasing Job resources; it cannot select UID, socket or another generation.
// WorkspaceFDReader владеет sequential peer connection для immutable task и mounted root.
// Caller закрывает его до освобождения Job resources; UID/socket/другая generation не выбираются caller.
type WorkspaceFDReader struct {
	connection *net.UnixConn
	request    workspaceReaderRequest
	root       unix.Stat_t
	ctx        context.Context
	stop       func()
	mu         sync.Mutex
	once       sync.Once
}

// OpenWorkspaceSnapshotRoot requires the fixed read-only mounted tree and original trusted generation ownership.
// The controller separately verifies the actual PVC UID, task, image and exact subpath before starting this process.
// OpenWorkspaceSnapshotRoot требует fixed read-only mounted tree и trusted generation ownership.
// Controller отдельно проверяет actual PVC UID, task/image/exact subpath до запуска процесса.
func OpenWorkspaceSnapshotRoot(task model.Task) (int, error) {
	uid := uint32(os.Geteuid())
	if uid != workspaceReaderRoleUID && uid != workspaceReaderProviderUID || task.Validate() != nil || task.Workspace == nil || task.Kind != "SNAPSHOT" {
		return -1, errWorkspaceReaderInvalid
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, WorkspaceSnapshotRoot, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return -1, errWorkspaceReaderInvalid
	}
	var info unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &info) != nil || info.Uid != workspaceReaderRoleUID || info.Gid != workspaceReaderGroup || info.Mode&unix.S_IFMT != unix.S_IFDIR ||
		info.Mode&0o7777 != 0o2770 || unix.Fstatfs(fd, &fs) != nil || fs.Flags&unix.ST_RDONLY == 0 {
		_ = unix.Close(fd)
		return -1, errWorkspaceReaderInvalid
	}
	return fd, nil
}

// workspaceReaderTaskDigest commits every canonical frozen task field without exporting its private body.
// workspaceReaderTaskDigest закрепляет все canonical frozen task fields без выдачи private body.
func workspaceReaderTaskDigest(task model.Task) ([32]byte, error) {
	if task.Validate() != nil || task.Workspace == nil || task.Kind != "SNAPSHOT" {
		return [32]byte{}, errWorkspaceReaderInvalid
	}
	raw, err := json.Marshal(task)
	if err != nil || len(raw) > model.MaximumTaskBytes {
		return [32]byte{}, errWorkspaceReaderInvalid
	}
	return sha256.Sum256(raw), nil
}

// NewWorkspaceFDReader completes the exact peer/task/root startup barrier before any snapshot stream or PUT.
// Socket readiness retries are local read-only scheduling inside the existing caller deadline, never effect retries.
// NewWorkspaceFDReader завершает exact peer/task/root startup barrier до snapshot stream или PUT.
// Socket readiness retries — local read-only scheduling внутри caller deadline, не effect retries.
func NewWorkspaceFDReader(ctx context.Context, task model.Task, rootFD int) (*WorkspaceFDReader, error) {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != int(workspaceReaderRoleUID) || rootFD < 0 {
		return nil, errWorkspaceReaderInvalid
	}
	digest, err := workspaceReaderTaskDigest(task)
	var root unix.Stat_t
	var fs unix.Statfs_t
	if err != nil || unix.Fstat(rootFD, &root) != nil || root.Uid != workspaceReaderRoleUID || root.Gid != workspaceReaderGroup ||
		root.Mode&unix.S_IFMT != unix.S_IFDIR || root.Mode&0o7777 != 0o2770 || unix.Fstatfs(rootFD, &fs) != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return nil, errWorkspaceReaderInvalid
	}
	connection, err := dialWorkspaceReader(ctx)
	if err != nil {
		return nil, errWorkspaceReaderInvalid
	}
	stop := watchWorkspaceReaderCancel(ctx, connection)
	request := workspaceReaderRequest{taskSHA256: digest, device: uint64(root.Dev), inode: root.Ino}
	if workspaceReaderHello(connection, request, workspaceReaderProviderUID) != nil {
		_ = connection.Close()
		stop()
		return nil, errWorkspaceReaderInvalid
	}
	return &WorkspaceFDReader{connection: connection, request: request, root: root, ctx: ctx, stop: stop}, nil
}

// dialWorkspaceReader accepts only the task-private socket owned by the fixed provider UID and work group.
// Unknown permissions or peer drift fail once; only absent/not-yet-listening startup is retried until cancellation.
// dialWorkspaceReader принимает только task-private socket fixed provider UID/work group.
// Unknown permissions/peer drift закрываются сразу; только absent/startup ожидание повторяется до отмены.
func dialWorkspaceReader(ctx context.Context) (*net.UnixConn, error) {
	connection, err := dialWorkspacePeer(ctx, WorkspaceReaderSocketDirectory, false, workspaceReaderRoleUID, workspaceReaderProviderUID, workspaceReaderGroup)
	if err != nil {
		return nil, errWorkspaceReaderInvalid
	}
	return connection, nil
}

// Open returns only one provider-owned regular-file FD equal to the independently observed tree entry.
// Relative path, root device and generation binding cannot be changed after the startup handshake.
// Open возвращает только provider-owned regular-file FD с independently observed tree identity.
// Relative path/root device/generation binding после startup handshake не меняются.
func (reader *WorkspaceFDReader) Open(path string, expected unix.Stat_t) (*os.File, error) {
	if reader == nil {
		return nil, errWorkspaceReaderInvalid
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.ctx.Err() != nil || expected.Dev != reader.root.Dev {
		return nil, errWorkspaceReaderInvalid
	}
	request := reader.request
	request.path = path
	return openWorkspaceReaderSource(reader.connection, request, expected, workspaceReaderProviderUID, workspaceReaderProviderUID, workspaceReaderGroup)
}

// sendEntryRequest binds a typed read-only lookup to the already authenticated connection, task and root.
// Its caller holds the single connection mutex; no new credentials, grant or root are accepted here.
// sendEntryRequest связывает typed read-only lookup с authenticated connection/task/root.
// Caller держит single connection mutex; новые credentials/grant/root здесь не принимаются.
func (reader *WorkspaceFDReader) sendEntryRequest(name string, kind workspaceReaderOperation) error {
	peer, err := workspaceReaderPeerUID(reader.connection)
	if reader.ctx.Err() != nil || err != nil || peer != workspaceReaderProviderUID {
		return errWorkspaceReaderInvalid
	}
	request := reader.request
	request.path, request.kind, request.hello = name, kind, false
	raw, err := encodeWorkspaceReaderRequest(request)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	n, controls, err := reader.connection.WriteMsgUnix(raw, nil, nil)
	if err != nil || n != len(raw) || controls != 0 {
		return errWorkspaceReaderInvalid
	}
	return nil
}

// OpenDirectory grants only an exact provider-owned read-only directory FD; it never passes through the file-only receiver.
// OpenDirectory выдаёт только exact provider-owned read-only directory FD, не проходя через file-only receiver.
func (reader *WorkspaceFDReader) OpenDirectory(name string, expected unix.Stat_t) (*os.File, error) {
	if reader == nil {
		return nil, errWorkspaceReaderInvalid
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if expected.Dev != reader.root.Dev || expected.Uid != workspaceReaderProviderUID || expected.Gid != workspaceReaderGroup ||
		!workspaceReaderEntryKind(expected, true) || reader.sendEntryRequest(name, workspaceReaderDirectory) != nil {
		return nil, errWorkspaceReaderInvalid
	}
	return receiveWorkspaceReaderDescriptor(reader.connection, expected, true)
}

// Stat obtains bounded kernel metadata for an otherwise inaccessible provider-owned descendant, without granting a descriptor.
// Role-owned, foreign, linked, special and cross-device observations are never accepted as provider metadata.
// Stat получает bounded kernel metadata недоступного provider-owned descendant без выдачи descriptor.
// Role-owned/foreign/linked/special/cross-device observations не принимаются как provider metadata.
func (reader *WorkspaceFDReader) Stat(name string) (unix.Stat_t, error) {
	if reader == nil {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.sendEntryRequest(name, workspaceReaderMetadata) != nil {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	info, err := receiveWorkspaceReaderMetadata(reader.connection)
	if err != nil || info.Dev != reader.root.Dev || info.Uid != workspaceReaderProviderUID || info.Gid != workspaceReaderGroup {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	return info, nil
}

// Close stops the only owned connection and joins its cancellation watcher exactly once.
// Close останавливает owned connection и завершает cancellation watcher ровно один раз.
func (reader *WorkspaceFDReader) Close() error {
	if reader != nil {
		reader.once.Do(func() { _ = reader.connection.Close(); reader.stop() })
	}
	return nil
}

// EncodeWorkspaceSnapshot opens only the fixed frozen mount and joins its fixed-UID reader before returning.
// The returned digest commits the stream, not a stored object or CLEAN artifact; the caller owns publication.
// EncodeWorkspaceSnapshot открывает только fixed frozen mount и завершает fixed-UID reader до возврата.
// Дайджест закрепляет поток, не stored object/CLEAN artifact; публикацией владеет caller.
func EncodeWorkspaceSnapshot(ctx context.Context, destination io.Writer, task model.Task) (string, int64, int64, int64, error) {
	rootFD, err := OpenWorkspaceSnapshotRoot(task)
	if err != nil {
		return "", 0, 0, 0, errWorkspaceReaderInvalid
	}
	defer unix.Close(rootFD)
	reader, err := NewWorkspaceFDReader(ctx, task, rootFD)
	if err != nil {
		return "", 0, 0, 0, errWorkspaceReaderInvalid
	}
	defer reader.Close()
	return scanWorkspaceReaderTree(ctx, rootFD, destination, task.Workspace.Policy,
		workspaceReaderRoleUID, workspaceReaderGroup, workspaceReaderProviderUID, reader)
}
