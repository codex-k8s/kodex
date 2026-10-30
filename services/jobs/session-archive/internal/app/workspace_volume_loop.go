package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"github.com/google/uuid"
	"google.golang.org/grpc/status"
)

const (
	// workspaceVolumeLoopFailureMessage fixes the safe diagnostic shared by the physical queue stages.
	// workspaceVolumeLoopFailureMessage задаёт безопасную диагностику этапов physical queue.
	workspaceVolumeLoopFailureMessage = "workspace volume worker cycle failed"
)

// archiveWorkerReadiness joins the working archive and optional physical-volume paths under
// one lock; a healthy worker cannot hide the other's failed or untested authority/RBAC path.
// archiveWorkerReadiness соединяет working archive и optional physical-volume paths под одной
// блокировкой; healthy worker не скрывает failed либо untested authority/RBAC path другого.
type archiveWorkerReadiness struct {
	mutex                       sync.Mutex
	archiveReady, volumeReady   bool
	archiveReason, volumeReason string
	readiness                   *serviceruntime.Readiness
	metrics                     *sharedobservability.Metrics
}

// newArchiveWorkerReadiness requires the physical path only for the explicitly enabled profile;
// the legacy-only profile retains its single working-claim readiness requirement.
// newArchiveWorkerReadiness требует physical path только при явно включённом profile;
// legacy-only profile сохраняет требование одного working-claim readiness path.
func newArchiveWorkerReadiness(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, requireVolume bool) *archiveWorkerReadiness {
	return &archiveWorkerReadiness{readiness: readiness, metrics: metrics, volumeReady: !requireVolume,
		archiveReason: "control_plane_unchecked", volumeReason: "workspace_volume_unchecked"}
}

// setArchive publishes the archive worker's observed working-path outcome without resetting
// the separate physical-path state.
// setArchive публикует observed working-path outcome archive worker без сброса отдельного physical path.
func (state *archiveWorkerReadiness) setArchive(ready bool, reason string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.archiveReady, state.archiveReason = ready, reason
	state.publishLocked()
}

// setVolume publishes physical RBAC/working-claim readiness without overriding archive failure.
// setVolume публикует physical RBAC/working-claim readiness, не перекрывая archive failure.
func (state *archiveWorkerReadiness) setVolume(ready bool, reason string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.volumeReady, state.volumeReason = ready, reason
	state.publishLocked()
}

// publishLocked updates the existing technical/readiness metrics from both required paths;
// callers hold the state lock, so concurrent healthy updates cannot mask a failed path.
// publishLocked обновляет existing technical/readiness metrics по обоим required paths;
// caller держит state lock, поэтому concurrent healthy updates не маскируют failed path.
func (state *archiveWorkerReadiness) publishLocked() {
	ready, reason := true, "ready"
	if !state.archiveReady {
		ready, reason = false, state.archiveReason
	} else if !state.volumeReady {
		ready, reason = false, state.volumeReason
	}
	state.readiness.Set(ready, reason)
	state.metrics.SetReady(ready)
}

// runWorkspaceVolumeLoop owns one bounded physical claim at a time, retaining an uncertain
// claim's idempotency key until an authoritative response. It runs under the same cancel/join
// group as the archive worker, so physical preparation cannot starve history/checkpoint capture.
// runWorkspaceVolumeLoop владеет одной bounded physical claim за раз, сохраняя idempotency key
// неизвестного claim до authoritative response. Работает в общем cancel/join group с archive
// worker, поэтому physical preparation не блокирует history/checkpoint capture.
func runWorkspaceVolumeLoop(control *controlplaneclient.Client, kube *controller.Controller, readiness *archiveWorkerReadiness, owned *archiveMetrics, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		key := uuid.NewString()
		var checked time.Time
		kubernetesReady := false
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if checked.IsZero() || time.Since(checked) >= config.ReadinessInterval {
				check, cancel := context.WithTimeout(ctx, config.RPCDeadline)
				err := kube.CheckWorkspaceVolume(check)
				cancel()
				checked, kubernetesReady = time.Now(), err == nil
				if err != nil {
					readiness.setVolume(false, "kubernetes_unavailable")
					logger.WarnContext(ctx, workspaceVolumeLoopFailureMessage, "stage", "kubernetes_api")
				}
			}
			if kubernetesReady {
				rpc, cancel := context.WithTimeout(ctx, config.RPCDeadline)
				response, err := control.WorkspaceVolumes.ClaimWorkspaceVolumeTasks(rpc, &cp.ClaimWorkspaceVolumeTasksRequest{
					Mutation: &cp.MutationContext{IdempotencyKey: key}, WorkloadInstance: config.InstanceID, Limit: 1})
				cancel()
				if err == nil && (response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.GetTasks()) > 1) {
					err = errWorkspaceVolumeClaimInvalid
				}
				if err == nil && len(response.GetTasks()) == 1 {
					work, decodeErr := model.WorkspaceVolumeFromProto(response.GetTasks()[0])
					if decodeErr != nil || !validWorkspaceVolumeLease(response.GetTasks()[0].GetLease(), int64(work.Attempt)) {
						err = errWorkspaceVolumeClaimInvalid
					}
				}
				readiness.setVolume(err == nil, "control_plane_unavailable")
				if err != nil {
					owned.cycles.WithLabelValues("error").Inc()
					logger.WarnContext(ctx, workspaceVolumeLoopFailureMessage, "stage", "claim", archiveRPCCodeAttribute, status.Code(err).String())
				} else {
					key = uuid.NewString()
					owned.cycles.WithLabelValues("success").Inc()
					if len(response.GetTasks()) == 1 {
						if err := processWorkspaceVolume(ctx, control.WorkspaceVolumes, kube, response.GetTasks()[0], owned, config.RPCDeadline); err != nil {
							logger.WarnContext(ctx, workspaceVolumeLoopFailureMessage, "stage", "task_processing", archiveRPCCodeAttribute, status.Code(err).String())
						}
					}
				}
			}
			timer := time.NewTimer(config.PollInterval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
