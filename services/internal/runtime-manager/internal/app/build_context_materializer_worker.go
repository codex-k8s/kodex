package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	runtimekubernetes "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/kubernetes"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

const (
	buildContextMaterializerActor = "runtime-manager-build-context-materializer"
	buildContextMaterializerLimit = 1
)

type buildContextStore interface {
	ListRunnableBuildContexts(context.Context, int) ([]entity.BuildContext, error)
}

type buildContextLifecycle interface {
	ReportBuildContextProgress(context.Context, runtimeservice.ReportBuildContextProgressInput) (entity.BuildContext, error)
}

type buildContextMaterializer interface {
	Materialize(context.Context, entity.BuildContext) runtimekubernetes.BuildContextMaterializationResult
}

func startBuildContextMaterializerWorker(
	ctx context.Context,
	cfg Config,
	store buildContextStore,
	service buildContextLifecycle,
	materializer buildContextMaterializer,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	if !cfg.KubernetesWorker.Enabled {
		return nil
	}
	if store == nil || service == nil || materializer == nil {
		return errors.New("runtime-manager build context materializer requires store, service and materializer")
	}
	if logger == nil {
		logger = slog.Default()
	}
	worker := buildContextMaterializerWorker{store: store, service: service, materializer: materializer, cfg: cfg.KubernetesWorker, logger: logger}
	go func() {
		if err := worker.run(ctx); err != nil {
			errCh <- err
		}
	}()
	return nil
}

type buildContextMaterializerWorker struct {
	store        buildContextStore
	service      buildContextLifecycle
	materializer buildContextMaterializer
	cfg          RuntimeKubernetesWorkerConfig
	logger       *slog.Logger
}

func (w buildContextMaterializerWorker) run(ctx context.Context) error {
	w.log().Info("runtime-manager build context materializer starting")
	timer := time.NewTimer(0)
	defer timer.Stop()
	retryDelay := minWorkerRetryDelay
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			result := w.materializeOne(ctx)
			delay := w.pollDelay()
			if result == kubernetesWorkerRetryableError {
				delay = retryDelay
				retryDelay = doubleDuration(retryDelay, maxWorkerRetryDelay)
			} else {
				retryDelay = minWorkerRetryDelay
			}
			timer.Reset(delay)
		}
	}
}

func (w buildContextMaterializerWorker) materializeOne(ctx context.Context) kubernetesWorkerIteration {
	contexts, err := w.store.ListRunnableBuildContexts(ctx, buildContextMaterializerLimit)
	if err != nil {
		if ctx.Err() != nil {
			return kubernetesWorkerProcessed
		}
		w.log().Warn("runtime-manager build context lookup failed", slog.String("error_code", "build_context_lookup_failed"))
		return kubernetesWorkerRetryableError
	}
	if len(contexts) == 0 {
		return kubernetesWorkerIdle
	}
	return w.materialize(ctx, contexts[0])
}

func (w buildContextMaterializerWorker) materialize(ctx context.Context, buildContext entity.BuildContext) kubernetesWorkerIteration {
	current := buildContext
	if current.Status == enum.BuildContextStatusPending {
		running, err := w.reportRunning(ctx, current)
		if err != nil {
			if ctx.Err() != nil {
				return kubernetesWorkerProcessed
			}
			w.log().Warn("runtime-manager build context running report failed", slog.String("build_context_id", current.ID.String()), slog.String("error_code", "build_context_report_failed"))
			return kubernetesWorkerRetryableError
		}
		current = running
	}
	result := w.materializer.Materialize(ctx, current)
	if result.Succeeded {
		if _, err := w.reportReady(ctx, current, result); err != nil {
			return w.reportProgressError(ctx, current, "ready")
		}
		return kubernetesWorkerProcessed
	}
	if _, err := w.reportFailed(ctx, current, result); err != nil {
		return w.reportProgressError(ctx, current, "failure")
	}
	return kubernetesWorkerProcessed
}

func (w buildContextMaterializerWorker) reportProgressError(ctx context.Context, buildContext entity.BuildContext, phase string) kubernetesWorkerIteration {
	if ctx.Err() != nil {
		return kubernetesWorkerProcessed
	}
	w.log().Warn(
		"runtime-manager build context "+phase+" report failed",
		slog.String("build_context_id", buildContext.ID.String()),
		slog.String("error_code", "build_context_report_failed"),
	)
	return kubernetesWorkerRetryableError
}

func (w buildContextMaterializerWorker) reportRunning(ctx context.Context, buildContext entity.BuildContext) (entity.BuildContext, error) {
	sourceRef, sourceDigest := runtimekubernetes.BuildContextSourceSnapshot(buildContext)
	return w.service.ReportBuildContextProgress(ctx, runtimeservice.ReportBuildContextProgressInput{
		BuildContextID:       buildContext.ID,
		Status:               enum.BuildContextStatusRunning,
		SourceSnapshotRef:    sourceRef,
		SourceSnapshotDigest: sourceDigest,
		Meta:                 w.commandMeta("running", &buildContext.Version),
	})
}

func (w buildContextMaterializerWorker) reportReady(ctx context.Context, buildContext entity.BuildContext, result runtimekubernetes.BuildContextMaterializationResult) (entity.BuildContext, error) {
	return w.service.ReportBuildContextProgress(ctx, runtimeservice.ReportBuildContextProgressInput{
		BuildContextID:        buildContext.ID,
		Status:                enum.BuildContextStatusReady,
		SourceSnapshotRef:     result.SourceSnapshotRef,
		SourceSnapshotDigest:  result.SourceSnapshotDigest,
		BuildContextRef:       result.BuildContextRef,
		BuildContextDigest:    result.BuildContextDigest,
		ManifestBundleDigests: result.ManifestBundleDigests,
		Meta:                  w.commandMeta("ready", &buildContext.Version),
	})
}

func (w buildContextMaterializerWorker) reportFailed(ctx context.Context, buildContext entity.BuildContext, result runtimekubernetes.BuildContextMaterializationResult) (entity.BuildContext, error) {
	return w.service.ReportBuildContextProgress(ctx, runtimeservice.ReportBuildContextProgressInput{
		BuildContextID: buildContext.ID,
		Status:         enum.BuildContextStatusFailed,
		ErrorCode:      firstNonEmpty(result.ErrorCode, "build_context_materializer_failed"),
		ErrorMessage:   firstNonEmpty(result.ErrorMessage, "build context materializer failed"),
		NextAction:     "review_build_context_materialization",
		Meta:           w.commandMeta("failed", &buildContext.Version),
	})
}

func (w buildContextMaterializerWorker) commandMeta(phase string, expectedVersion *int64) value.CommandMeta {
	return workerCommandMeta(buildContextMaterializerActor, "runtime build context materializer", phase, expectedVersion)
}

func (w buildContextMaterializerWorker) pollDelay() time.Duration {
	if w.cfg.PollInterval > 0 {
		return w.cfg.PollInterval
	}
	return minWorkerRetryDelay
}

func (w buildContextMaterializerWorker) log() *slog.Logger {
	if w.logger != nil {
		return w.logger
	}
	return slog.Default()
}
