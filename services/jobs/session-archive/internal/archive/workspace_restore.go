package archive

import (
	"context"
	"os"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// WorkspaceWriterSocketDirectory is the dedicated writable peer IPC subpath, separate from the read-only snapshot endpoint.
	// WorkspaceWriterSocketDirectory — отдельный writable peer IPC subpath, не read-only snapshot endpoint.
	WorkspaceWriterSocketDirectory = "/var/run/kodex/work-writer"
	// WorkspaceRestoreRoot mounts exactly the initialized stage or published reconciliation tree, never an entire PVC.
	// WorkspaceRestoreRoot монтирует только initialized stage либо published reconciliation tree, не целый PVC.
	WorkspaceRestoreRoot = "/workspace/work"
	// WorkspaceRestoreGenerationMount exposes private generation metadata only to the trusted main worker.
	// WorkspaceRestoreGenerationMount открывает private generation metadata только trusted main worker.
	WorkspaceRestoreGenerationMount = "/var/run/config/kodex/workspace-restore-generation"
)

// openWorkspaceRestoreMount accepts only one fixed owned mount with mode appropriate to the exact task execution mode.
// It follows the image's immutable OS prefix but not its mount leaf; physical PVC/task custody remains the controller's responsibility.
// openWorkspaceRestoreMount принимает fixed owned mount с mode, соответствующим exact task execution mode.
// Он допускает immutable OS prefix, но не mount leaf symlink; physical PVC/task custody доказывает controller.
func openWorkspaceRestoreMount(ctx context.Context, name string, private, readOnly bool) (int, error) {
	if ctx == nil || ctx.Err() != nil {
		return -1, errWorkspaceObjectUnconfirmed
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, errWorkspaceObjectUnconfirmed
	}
	var info unix.Stat_t
	var filesystem unix.Statfs_t
	mode := uint32(0o2770)
	if private {
		mode = 0o700
	}
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != workspaceReaderRoleUID ||
		info.Mode&0o7777 != mode || !private && info.Gid != workspaceReaderGroup || unix.Fstatfs(fd, &filesystem) != nil ||
		(filesystem.Flags&unix.ST_RDONLY != 0) != readOnly || ctx.Err() != nil {
		_ = unix.Close(fd)
		return -1, errWorkspaceObjectUnconfirmed
	}
	return fd, nil
}

// OpenWorkspaceRestoreRoot opens only the task's fixed writable stage or read-only published tree under a permitted process identity.
// A provider peer is forbidden for reconciliation-only work; no source metadata authorizes a new extraction.
// OpenWorkspaceRestoreRoot открывает только fixed writable stage либо read-only published tree под разрешённой process identity.
// Provider peer запрещён для reconciliation-only work; source metadata не разрешают новое извлечение.
func OpenWorkspaceRestoreRoot(ctx context.Context, task model.Task) (int, error) {
	uid := uint32(os.Geteuid())
	if task.Validate() != nil || task.Workspace == nil || task.Kind != "RESTORE" || task.Workspace.PVCUID == "" ||
		uid != workspaceReaderRoleUID && uid != workspaceReaderProviderUID || os.Getegid() != int(workspaceReaderGroup) ||
		task.Workspace.ReconcileOnly && uid != workspaceReaderRoleUID {
		return -1, errWorkspaceObjectUnconfirmed
	}
	if _, err := workspaceRestoreIntentBytes(task, task.Workspace.PVCUID); err != nil {
		return -1, errWorkspaceObjectUnconfirmed
	}
	return openWorkspaceRestoreMount(ctx, WorkspaceRestoreRoot, false, task.Workspace.ReconcileOnly)
}

// ServeWorkspaceWriter owns only the fixed provider root and writable IPC endpoint without reading API or storage credentials.
// The caller supplies a frozen authorized task; this helper never mounts or initializes a generation itself.
// ServeWorkspaceWriter владеет только fixed provider root и writable IPC endpoint без чтения API/storage credentials.
// Caller передаёт frozen authorized task; helper сам не монтирует и не инициализирует generation.
func ServeWorkspaceWriter(ctx context.Context, task model.Task) (resultErr error) {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != int(workspaceReaderProviderUID) {
		return errWorkspaceObjectUnconfirmed
	}
	if _, err := workspaceWriterTaskDigest(task); err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	root, err := OpenWorkspaceRestoreRoot(ctx, task)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(root)
	listener, err := newWorkspacePeerListener(ctx, WorkspaceWriterSocketDirectory, true,
		workspaceReaderRoleUID, workspaceReaderProviderUID, workspaceReaderGroup)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if listener.Close() != nil {
			resultErr = errWorkspaceObjectUnconfirmed
		}
	}()
	connection, err := listener.Accept(ctx)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return serveWorkspaceWriterPeer(ctx, connection, root, task, workspaceRestoreIdentity{
		actor: workspaceReaderProviderUID, role: workspaceReaderRoleUID, provider: workspaceReaderProviderUID, group: workspaceReaderGroup})
}

// RestoreWorkspace verifies object custody and exact generation publication before returning a work-only result to its owner.
// Physical Pod/PVC admission and terminal owner acknowledgement remain separate; this result cannot start a model by itself.
// RestoreWorkspace проверяет object custody и exact generation publication до возврата work-only result владельцу.
// Physical Pod/PVC admission и terminal owner ACK отдельны; этот result сам не запускает модель.
func RestoreWorkspace(ctx context.Context, store objectstorage.Store, task model.Task) (model.Result, error) {
	return restoreWorkspace(ctx, store, task, nil)
}

// restoreWorkspace permits an additional trusted process-join barrier, used by the native isolated coordinator before publication.
// The barrier cannot replace the mandatory protocol EOF, file closure, complete stream verification or source/intent/inode readback.
// restoreWorkspace допускает дополнительный trusted process-join barrier native isolated coordinator до publication.
// Barrier не заменяет обязательные protocol EOF, file closure, complete stream verification и source/intent/inode readback.
func restoreWorkspace(ctx context.Context, store objectstorage.Store, task model.Task, joined func(context.Context) error) (result model.Result, resultErr error) {
	if ctx == nil || ctx.Err() != nil || store == nil || os.Geteuid() != int(workspaceReaderRoleUID) || task.Workspace == nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	root, err := OpenWorkspaceRestoreRoot(ctx, task)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if unix.Close(root) != nil {
			result, resultErr = model.Result{}, errWorkspaceObjectUnconfirmed
		}
	}()
	parent, err := openWorkspaceRestoreMount(ctx, WorkspaceRestoreGenerationMount, true, task.Workspace.ReconcileOnly)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if unix.Close(parent) != nil {
			result, resultErr = model.Result{}, errWorkspaceObjectUnconfirmed
		}
	}()
	publication, err := newWorkspaceRestorePublication(ctx, parent, task, task.Workspace.PVCUID,
		securefile.OwnedRecordIdentity{UID: workspaceReaderRoleUID, GID: workspaceReaderGroup})
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer func() {
		if publication.Close() != nil {
			result, resultErr = model.Result{}, errWorkspaceObjectUnconfirmed
		}
	}()
	name := workspaceRestoreStagingTree
	if task.Workspace.ReconcileOnly {
		name = "tree"
	}
	expected, err := publication.tree(name)
	var actual unix.Stat_t
	if err != nil || unix.Fstat(root, &actual) != nil || expected.Dev != actual.Dev || expected.Ino != actual.Ino {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	if task.Workspace.ReconcileOnly {
		if _, err := VerifyWorkspaceCheckpoint(ctx, store, *task.Workspace.Checkpoint); err != nil || publication.Rejoin(ctx) != nil {
			return model.Result{}, errWorkspaceObjectUnconfirmed
		}
		return workspaceRestoreResult(*task.Workspace.Checkpoint), nil
	}
	connection, err := dialWorkspacePeer(ctx, WorkspaceWriterSocketDirectory, true,
		workspaceReaderRoleUID, workspaceReaderProviderUID, workspaceReaderGroup)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	identity := workspaceRestoreIdentity{actor: workspaceReaderRoleUID, role: workspaceReaderRoleUID, provider: workspaceReaderProviderUID, group: workspaceReaderGroup}
	writer, err := newWorkspaceFDWriterPeer(ctx, connection, root, task, identity)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer writer.abort()
	files, err := newWorkspaceRestoreFilesystem(ctx, root, task.Workspace.Policy, identity)
	if err != nil {
		return model.Result{}, errWorkspaceObjectUnconfirmed
	}
	defer files.Close()
	return restoreWorkspaceStream(ctx, store, task, workspaceRestoreDestination{role: files, provider: writer,
		commit: func(ctx context.Context) error {
			if writer.Close() != nil || files.Close() != nil || joined != nil && joined(ctx) != nil {
				return errWorkspaceObjectUnconfirmed
			}
			return publication.Commit(ctx, root)
		}})
}

// workspaceRestoreResult projects only the verified source commitment; its callers must complete publication or read-only rejoin first.
// workspaceRestoreResult переносит только verified source commitment; caller сначала завершает publication либо read-only rejoin.
func workspaceRestoreResult(checkpoint runtimecontract.WorkspaceCheckpointBinding) model.Result {
	return model.Result{Success: true, FormatVersion: 2, ObjectKey: checkpoint.Manifest.Key, ObjectVersion: checkpoint.Manifest.Version,
		ObjectETag: checkpoint.Manifest.ETag, ObjectDigest: checkpoint.Manifest.Digest, ObjectSizeBytes: checkpoint.Manifest.SizeBytes,
		SourceSHA256: checkpoint.TreeSHA256, SourceSizeBytes: checkpoint.Bytes, SourceEntryCount: checkpoint.Entries}
}
