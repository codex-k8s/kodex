package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	runtimekubernetes "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

const (
	kubernetesWorkerActor = "runtime-manager-kubernetes-executor"
	kubernetesStepKey     = "kubernetes_health_check"
)

type runtimeJobLifecycle interface {
	ClaimRunnableJob(context.Context, runtimeservice.ClaimRunnableJobInput) (runtimeservice.ClaimRunnableJobResult, error)
	ReportJobStepProgress(context.Context, runtimeservice.ReportJobStepProgressInput) (entity.Job, error)
	CompleteJob(context.Context, runtimeservice.CompleteJobInput) (entity.Job, error)
	FailJob(context.Context, runtimeservice.FailJobInput) (entity.Job, error)
}

type kubernetesExecutor interface {
	Start(context.Context, entity.Job) (runtimekubernetes.StartedJob, error)
	Wait(context.Context, runtimekubernetes.StartedJob) runtimekubernetes.ExecutionResult
}

func startKubernetesJobWorker(
	ctx context.Context,
	cfg Config,
	service runtimeJobLifecycle,
	executor kubernetesExecutor,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	if !cfg.KubernetesWorker.Enabled {
		return nil
	}
	if service == nil || executor == nil {
		return errors.New("runtime-manager Kubernetes executor requires service and executor")
	}
	if logger == nil {
		logger = slog.Default()
	}
	worker := kubernetesJobWorker{service: service, executor: executor, cfg: cfg.KubernetesWorker, logger: logger}
	go func() {
		if err := worker.run(ctx); err != nil {
			errCh <- err
		}
	}()
	return nil
}

type kubernetesJobWorker struct {
	service  runtimeJobLifecycle
	executor kubernetesExecutor
	cfg      RuntimeKubernetesWorkerConfig
	logger   *slog.Logger
}

func (w kubernetesJobWorker) run(ctx context.Context) error {
	w.logger.Info("runtime-manager Kubernetes job executor starting")
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			w.claimAndExecute(ctx)
			timer.Reset(w.cfg.PollInterval)
		}
	}
}

func (w kubernetesJobWorker) claimAndExecute(ctx context.Context) {
	claim, err := w.service.ClaimRunnableJob(ctx, runtimeservice.ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeHealthCheck},
		WorkerID:   w.cfg.WorkerID,
		LeaseOwner: w.cfg.WorkerID,
		LeaseUntil: time.Now().UTC().Add(w.cfg.ClaimLeaseTTL),
		Meta:       w.commandMeta("claim", nil),
	})
	if errors.Is(err, errs.ErrNotFound) {
		return
	}
	if err != nil {
		w.logger.Warn("runtime-manager Kubernetes job claim failed", slog.String("error_code", "claim_failed"))
		return
	}
	w.executeClaim(ctx, claim)
}

func (w kubernetesJobWorker) executeClaim(ctx context.Context, claim runtimeservice.ClaimRunnableJobResult) {
	started, err := w.executor.Start(ctx, claim.Job)
	if err != nil {
		code, message := runtimekubernetes.ErrorDiagnostic(err)
		w.failClaimedJob(ctx, claim.Job, claim.LeaseToken, "", code, message)
		return
	}
	reported, err := w.reportStep(ctx, claim.Job, claim.LeaseToken, enum.JobStepStatusRunning, "", started.ExternalRef, "", "", started.ArtifactRefs)
	if err != nil {
		w.logger.Warn("runtime-manager Kubernetes job progress report failed", slog.String("job_id", claim.Job.ID.String()), slog.String("error_code", "report_failed"))
		return
	}
	result := w.executor.Wait(ctx, started)
	if result.Succeeded {
		completed, err := w.reportStep(ctx, reported, claim.LeaseToken, enum.JobStepStatusSucceeded, result.ShortLogTail, started.ExternalRef, "", "", nil)
		if err != nil {
			w.logger.Warn("runtime-manager Kubernetes job completion step report failed", slog.String("job_id", reported.ID.String()), slog.String("error_code", "report_failed"))
			return
		}
		if _, err := w.service.CompleteJob(ctx, runtimeservice.CompleteJobInput{
			JobID:        completed.ID,
			LeaseToken:   claim.LeaseToken,
			ShortLogTail: result.ShortLogTail,
			Meta:         w.commandMeta("complete", &completed.Version),
		}); err != nil {
			w.logger.Warn("runtime-manager Kubernetes job complete failed", slog.String("job_id", completed.ID.String()), slog.String("error_code", "complete_failed"))
		}
		return
	}
	failed, err := w.reportStep(ctx, reported, claim.LeaseToken, enum.JobStepStatusFailed, result.ShortLogTail, started.ExternalRef, result.ErrorCode, result.ErrorMessage, nil)
	if err != nil {
		w.logger.Warn("runtime-manager Kubernetes job failure step report failed", slog.String("job_id", reported.ID.String()), slog.String("error_code", "report_failed"))
		w.failClaimedJob(ctx, reported, claim.LeaseToken, result.ShortLogTail, result.ErrorCode, result.ErrorMessage)
		return
	}
	w.failClaimedJob(ctx, failed, claim.LeaseToken, result.ShortLogTail, result.ErrorCode, result.ErrorMessage)
}

func (w kubernetesJobWorker) reportStep(
	ctx context.Context,
	job entity.Job,
	leaseToken string,
	status enum.JobStepStatus,
	shortLogTail string,
	externalRef string,
	errorCode string,
	errorMessage string,
	refs []runtimeservice.RuntimeArtifactRefInput,
) (entity.Job, error) {
	now := time.Now().UTC()
	var startedAt *time.Time
	var finishedAt *time.Time
	if status == enum.JobStepStatusRunning {
		startedAt = &now
	}
	if status == enum.JobStepStatusSucceeded || status == enum.JobStepStatusFailed || status == enum.JobStepStatusSkipped {
		finishedAt = &now
	}
	return w.service.ReportJobStepProgress(ctx, runtimeservice.ReportJobStepProgressInput{
		JobID:        job.ID,
		LeaseToken:   leaseToken,
		StepKey:      kubernetesStepKey,
		Status:       status,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		ShortLogTail: shortLogTail,
		ExternalRef:  externalRef,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		ArtifactRefs: refs,
		Meta:         w.commandMeta("report-step", &job.Version),
	})
}

func (w kubernetesJobWorker) failClaimedJob(ctx context.Context, job entity.Job, leaseToken string, shortLogTail string, code string, message string) {
	if code == "" {
		code = "runtime_kubernetes_error"
	}
	if message == "" {
		message = "Kubernetes executor failed"
	}
	if _, err := w.service.FailJob(ctx, runtimeservice.FailJobInput{
		JobID:        job.ID,
		LeaseToken:   leaseToken,
		ErrorCode:    code,
		ErrorMessage: message,
		ShortLogTail: shortLogTail,
		NextAction:   "review_runtime_kubernetes_job",
		Meta:         w.commandMeta("fail", &job.Version),
	}); err != nil {
		w.logger.Warn("runtime-manager Kubernetes job fail failed", slog.String("job_id", job.ID.String()), slog.String("error_code", "fail_failed"))
	}
}

func (w kubernetesJobWorker) commandMeta(phase string, expectedVersion *int64) value.CommandMeta {
	return value.CommandMeta{
		CommandID:       uuid.New(),
		ExpectedVersion: expectedVersion,
		Actor:           value.Actor{Type: "service", ID: kubernetesWorkerActor},
		Reason:          "runtime Kubernetes job " + phase,
		RequestID:       kubernetesWorkerActor + "-" + phase,
		RequestContext:  value.RequestContext{Source: kubernetesWorkerActor},
	}
}
