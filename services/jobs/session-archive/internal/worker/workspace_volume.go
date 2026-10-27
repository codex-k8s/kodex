package worker

import (
	"context"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
)

// PrepareWorkspaceVolume runs the fixed-UID credential-free volume helper and publishes only its bounded local inode observation.
// This entry point does not register owner task admission; the full durable owner create/preparation protocol must authorize its Job separately.
// PrepareWorkspaceVolume запускает fixed-UID credential-free volume helper и публикует только bounded local inode observation.
// Точка входа не включает owner task admission; полный durable owner create/preparation protocol отдельно разрешает её Job.
func PrepareWorkspaceVolume(lifecycle context.Context, reconcileOnly bool) error {
	ctx, cancel, err := workspaceHelperContext(lifecycle, 10001)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	defer cancel()
	raw, err := securefile.Read("/var/run/config/kodex/session-archive/volume.json", runtimecontract.MaximumWorkspaceVolumeReadyBytes)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	preparation, err := runtimecontract.DecodeWorkspaceVolumePreparation(raw)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	ready, err := archive.PrepareWorkspaceVolume(ctx, preparation, reconcileOnly)
	if err != nil {
		return errWorkspaceWriterWorker
	}
	outcome, err := runtimecontract.EncodeWorkspaceVolumeReady(ready)
	if err != nil || os.WriteFile("/dev/termination-log", outcome, 0o600) != nil || ctx.Err() != nil {
		return errWorkspaceWriterWorker
	}
	return nil
}
