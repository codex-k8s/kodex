package worker

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

// errWorkspaceWriterWorker hides private task and mount details at the dedicated credential-free writable helper boundary.
// errWorkspaceWriterWorker скрывает private task/mount details на границе отдельного credential-free writable helper.
var errWorkspaceWriterWorker = errors.New("session workspace writer failed")

// workspaceHelperContext applies the existing bounded helper budget without reading credentials or creating a root context.
// workspaceHelperContext применяет существующий bounded helper budget без чтения credentials или создания root context.
func workspaceHelperContext(lifecycle context.Context, uid int) (context.Context, context.CancelFunc, error) {
	if lifecycle == nil || lifecycle.Err() != nil || os.Geteuid() != uid {
		return nil, nil, errWorkspaceWriterWorker
	}
	timeout := 8 * time.Minute
	if raw := os.Getenv("SESSION_ARCHIVE_WORKER_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Minute || value > 15*time.Minute {
			return nil, nil, errWorkspaceWriterWorker
		}
		timeout = value
	}
	ctx, cancel := context.WithTimeout(lifecycle, timeout)
	return ctx, cancel, nil
}

// WriteWorkspace serves only a fresh restore's fixed provider identity and never opens storage credentials or an API client.
// WriteWorkspace обслуживает только fixed provider identity fresh restore без storage credentials/API client.
func WriteWorkspace(lifecycle context.Context) error {
	ctx, cancel, err := workspaceHelperContext(lifecycle, 10002)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	defer cancel()
	task, err := model.DecodeFile(restoreTaskFile)
	if err != nil || task.Workspace == nil || task.Kind != "RESTORE" || task.Workspace.ReconcileOnly ||
		task.Workspace.PVCUID == "" || archive.ServeWorkspaceWriter(ctx, task) != nil {
		return errWorkspaceWriterWorker
	}
	return nil
}

// PrepareWorkspaceGeneration initializes only an owner-pinned fresh restore using the full PVC mount reserved to this init process.
// It neither extracts data nor reads S3/API credentials; reconciliation-only tasks cannot invoke initialization.
// PrepareWorkspaceGeneration инициализирует только owner-pinned fresh restore через full PVC mount этого init process.
// Она не извлекает data и не читает S3/API credentials; reconciliation-only tasks не допускаются.
func PrepareWorkspaceGeneration(lifecycle context.Context) error {
	ctx, cancel, err := workspaceHelperContext(lifecycle, 10001)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	defer cancel()
	task, err := model.DecodeFile(restoreTaskFile)
	if err != nil || task.Workspace == nil || task.Kind != "RESTORE" || task.Workspace.ReconcileOnly ||
		task.Workspace.PVCUID == "" || archive.PrepareWorkspaceRestore(ctx, task) != nil {
		return errWorkspaceWriterWorker
	}
	return nil
}

// PrepareWorkspaceWriter creates only the separate task-private writable socket directory, without a data mount or credentials.
// PrepareWorkspaceWriter создаёт только отдельный task-private writable socket directory без data mount/credentials.
func PrepareWorkspaceWriter(ctx context.Context) error {
	if prepareWorkspaceSocketDirectory(ctx, true) != nil {
		return errWorkspaceWriterWorker
	}
	return nil
}
