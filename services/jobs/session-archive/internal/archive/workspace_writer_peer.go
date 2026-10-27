package archive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// workspaceWriterTaskDigest commits the complete fresh restore input without accepting snapshot or reconciliation-only work.
// workspaceWriterTaskDigest закрепляет полный fresh restore input и отклоняет snapshot/reconciliation-only work.
func workspaceWriterTaskDigest(task model.Task) ([32]byte, error) {
	if task.Validate() != nil || task.Kind != "RESTORE" || task.Workspace == nil || task.Workspace.ReconcileOnly {
		return [32]byte{}, errWorkspaceObjectUnconfirmed
	}
	raw, err := json.Marshal(task)
	if err != nil || len(raw) > model.MaximumTaskBytes {
		return [32]byte{}, errWorkspaceObjectUnconfirmed
	}
	return sha256.Sum256(raw), nil
}

// acknowledgeWorkspaceWriter emits only the descriptor-free success marker after the requested durable effect completed.
// acknowledgeWorkspaceWriter отправляет descriptor-free success marker только после завершения durable effect.
func acknowledgeWorkspaceWriter(connection *net.UnixConn) error {
	n, controls, err := connection.WriteMsgUnix([]byte{2}, nil, nil)
	if err != nil || n != 1 || controls != 0 {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// serveWorkspaceWriterPeer owns one bounded authenticated connection and one fixed provider-identity filesystem adapter.
// It never receives credentials or owner IDs from the wire, and leaves every uncertain file unpublished and intact.
// serveWorkspaceWriterPeer владеет bounded authenticated connection и filesystem adapter фиксированного provider UID.
// Wire не передаёт credentials/owner IDs; uncertain files остаются unpublished и сохраняются.
func serveWorkspaceWriterPeer(ctx context.Context, connection *net.UnixConn, rootFD int, task model.Task, identity workspaceRestoreIdentity) error {
	if ctx == nil || connection == nil || ctx.Err() != nil || identity.actor != identity.provider || uint32(os.Geteuid()) != identity.provider {
		return errWorkspaceObjectUnconfirmed
	}
	defer connection.Close()
	deadline, bounded := ctx.Deadline()
	digest, err := workspaceWriterTaskDigest(task)
	if !bounded || err != nil || connection.SetDeadline(deadline) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	peer, err := workspaceReaderPeerUID(connection)
	if err != nil || peer != identity.role {
		return errWorkspaceObjectUnconfirmed
	}
	files, err := newWorkspaceRestoreFilesystem(ctx, rootFD, task.Workspace.Policy, identity)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	defer files.Close()
	stop := watchWorkspaceReaderCancel(ctx, connection)
	defer stop()
	var root unix.Stat_t
	if unix.Fstat(rootFD, &root) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	var pending *workspaceRestoreFile
	defer func() {
		if pending != nil {
			_ = pending.Close()
		}
	}()
	hello := false
	for requests := int64(0); requests < 2*task.Workspace.Policy.MaximumFileCount+2; requests++ {
		request, err := receiveWorkspaceWriterRequest(connection)
		var current unix.Stat_t
		if err != nil || ctx.Err() != nil || request.taskSHA256 != digest || request.device != uint64(root.Dev) || request.inode != root.Ino ||
			unix.Fstat(rootFD, &current) != nil || current.Dev != root.Dev || current.Ino != root.Ino ||
			current.Uid != identity.role || current.Gid != identity.group || current.Mode&0o7777 != 0o2770 ||
			!hello && request.operation != workspaceWriterHello || pending != nil && request.operation != workspaceWriterComplete {
			return errWorkspaceObjectUnconfirmed
		}
		switch request.operation {
		case workspaceWriterHello:
			if hello {
				return errWorkspaceObjectUnconfirmed
			}
			hello = true
		case workspaceWriterDirectory:
			if files.Directory(ctx, request.path) != nil {
				return errWorkspaceObjectUnconfirmed
			}
		case workspaceWriterFile:
			writer, err := files.File(ctx, request.path, request.mode, request.size)
			if err != nil {
				return errWorkspaceObjectUnconfirmed
			}
			pending = writer.(*workspaceRestoreFile)
			fd, err := pending.exportWriter()
			if err != nil {
				return errWorkspaceObjectUnconfirmed
			}
			rights := unix.UnixRights(fd)
			n, controls, err := connection.WriteMsgUnix([]byte{1}, rights, nil)
			if err != nil || n != 1 || controls != len(rights) {
				return errWorkspaceObjectUnconfirmed
			}
			continue
		case workspaceWriterComplete:
			if pending == nil || pending.completeExternal() != nil {
				return errWorkspaceObjectUnconfirmed
			}
			pending = nil
		case workspaceWriterSeal:
			if files.SealDirectory(ctx, request.path, request.mode) != nil {
				return errWorkspaceObjectUnconfirmed
			}
		case workspaceWriterFinish:
			if files.Close() != nil {
				return errWorkspaceObjectUnconfirmed
			}
			return acknowledgeWorkspaceWriter(connection)
		default:
			return errWorkspaceObjectUnconfirmed
		}
		if acknowledgeWorkspaceWriter(connection) != nil {
			return errWorkspaceObjectUnconfirmed
		}
	}
	return errWorkspaceObjectUnconfirmed
}
