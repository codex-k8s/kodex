package archive

import (
	"context"
	"os"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// ServeWorkspaceReader owns a fixed UID10002 read-only peer without API tokens, source writes or storage credentials.
// Its only accepted connection has the fixed main UID and matching task/root; cancellation joins all socket work.
// ServeWorkspaceReader владеет fixed UID10002 read-only peer без API tokens/source writes/storage credentials.
// Единственный connection требует fixed main UID и matching task/root; cancel завершает socket work.
func ServeWorkspaceReader(ctx context.Context, task model.Task) (resultErr error) {
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
	listener, err := newWorkspacePeerListener(ctx, WorkspaceReaderSocketDirectory, false,
		workspaceReaderRoleUID, workspaceReaderProviderUID, workspaceReaderGroup)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	defer func() {
		if listener.Close() != nil {
			resultErr = errWorkspaceReaderInvalid
		}
	}()
	connection, err := listener.Accept(ctx)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	return serveWorkspaceReaderPeer(ctx, connection, rootFD, digest, workspaceReaderRoleUID, workspaceReaderProviderUID,
		workspaceReaderGroup, task.Workspace.Policy.MaximumFileCount)
}
