package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	kubernetesWorkerActor        = "runtime-manager-kubernetes-executor"
	kubernetesHealthCheckStepKey = "kubernetes_health_check"
	kubernetesAgentRunStepKey    = "kubernetes_agent_run"
	kubernetesBuildStepKey       = "kubernetes_build"
	redactedDiagnosticValue      = "[redacted]"
	minWorkerRetryDelay          = time.Second
	maxWorkerRetryDelay          = 30 * time.Second
)

type kubernetesWorkerIteration int

const (
	kubernetesWorkerIdle kubernetesWorkerIteration = iota
	kubernetesWorkerProcessed
	kubernetesWorkerRetryableError
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
	w.log().Info("runtime-manager Kubernetes job executor starting")
	timer := time.NewTimer(0)
	defer timer.Stop()
	retryDelay := w.baseRetryDelay()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			result := w.claimAndExecute(ctx)
			delay := w.pollDelay()
			if result == kubernetesWorkerRetryableError {
				delay = retryDelay
				retryDelay = doubleDuration(retryDelay, w.maxRetryDelay())
			} else {
				retryDelay = w.baseRetryDelay()
			}
			timer.Reset(delay)
		}
	}
}

func (w kubernetesJobWorker) claimAndExecute(ctx context.Context) kubernetesWorkerIteration {
	claim, err := w.service.ClaimRunnableJob(ctx, runtimeservice.ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeHealthCheck, enum.JobTypeAgentRun, enum.JobTypeBuild},
		WorkerID:   w.cfg.WorkerID,
		LeaseOwner: w.cfg.WorkerID,
		LeaseUntil: time.Now().UTC().Add(w.cfg.ClaimLeaseTTL),
		Meta:       w.commandMeta("claim", nil),
	})
	if errors.Is(err, errs.ErrNotFound) {
		return kubernetesWorkerIdle
	}
	if err != nil {
		if ctx.Err() != nil {
			return kubernetesWorkerProcessed
		}
		w.log().Warn("runtime-manager Kubernetes job claim failed", slog.String("error_code", "claim_failed"))
		return kubernetesWorkerRetryableError
	}
	return w.executeClaim(ctx, claim)
}

func (w kubernetesJobWorker) executeClaim(ctx context.Context, claim runtimeservice.ClaimRunnableJobResult) kubernetesWorkerIteration {
	started, err := w.executor.Start(ctx, claim.Job)
	if err != nil {
		if ctx.Err() != nil {
			w.log().Info("runtime-manager Kubernetes job start interrupted", slog.String("job_id", claim.Job.ID.String()), slog.String("error_code", "worker_stopped"))
			return kubernetesWorkerProcessed
		}
		code, message := runtimekubernetes.ErrorDiagnostic(err)
		w.failClaimedJob(ctx, claim.Job, claim.LeaseToken, "", code, message)
		return kubernetesWorkerProcessed
	}
	reported, err := w.reportStep(ctx, claim.Job, claim.LeaseToken, enum.JobStepStatusRunning, "", started.ExternalRef, "", "", started.ArtifactRefs)
	if err != nil {
		w.log().Warn("runtime-manager Kubernetes job progress report failed", slog.String("job_id", claim.Job.ID.String()), slog.String("error_code", "report_failed"))
		return workerResultForContext(ctx)
	}
	result := w.executor.Wait(ctx, started)
	if result.Interrupted {
		w.log().Info("runtime-manager Kubernetes job wait interrupted", slog.String("job_id", reported.ID.String()), slog.String("error_code", result.ErrorCode))
		return kubernetesWorkerProcessed
	}
	if result.Succeeded {
		shortLogTail := safeExecutionShortLogTail(reported.JobType, result.ShortLogTail)
		completed, err := w.reportStep(ctx, reported, claim.LeaseToken, enum.JobStepStatusSucceeded, shortLogTail, started.ExternalRef, "", "", nil)
		if err != nil {
			w.log().Warn("runtime-manager Kubernetes job completion step report failed", slog.String("job_id", reported.ID.String()), slog.String("error_code", "report_failed"))
			return workerResultForContext(ctx)
		}
		if _, err := w.service.CompleteJob(ctx, runtimeservice.CompleteJobInput{
			JobID:        completed.ID,
			LeaseToken:   claim.LeaseToken,
			ShortLogTail: shortLogTail,
			Meta:         w.commandMeta("complete", &completed.Version),
		}); err != nil {
			w.log().Warn("runtime-manager Kubernetes job complete failed", slog.String("job_id", completed.ID.String()), slog.String("error_code", "complete_failed"))
			return workerResultForContext(ctx)
		}
		return kubernetesWorkerProcessed
	}
	shortLogTail := safeExecutionShortLogTail(reported.JobType, result.ShortLogTail)
	errorMessage := firstNonEmpty(result.StatusSummary, result.ErrorMessage)
	failed, err := w.reportStep(ctx, reported, claim.LeaseToken, enum.JobStepStatusFailed, shortLogTail, started.ExternalRef, result.ErrorCode, errorMessage, nil)
	if err != nil {
		w.log().Warn("runtime-manager Kubernetes job failure step report failed", slog.String("job_id", reported.ID.String()), slog.String("error_code", "report_failed"))
		w.failClaimedJob(ctx, reported, claim.LeaseToken, shortLogTail, result.ErrorCode, errorMessage)
		return workerResultForContext(ctx)
	}
	w.failClaimedJob(ctx, failed, claim.LeaseToken, shortLogTail, result.ErrorCode, errorMessage)
	return kubernetesWorkerProcessed
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
		StepKey:      kubernetesStepKey(job.JobType),
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
		NextAction:   nextActionForKubernetesError(code),
		TimedOut:     code == "kubernetes_job_timeout",
		Meta:         w.commandMeta("fail", &job.Version),
	}); err != nil {
		w.log().Warn("runtime-manager Kubernetes job fail failed", slog.String("job_id", job.ID.String()), slog.String("error_code", "fail_failed"))
	}
}

func kubernetesStepKey(jobType enum.JobType) string {
	switch jobType {
	case enum.JobTypeAgentRun:
		return kubernetesAgentRunStepKey
	case enum.JobTypeBuild:
		return kubernetesBuildStepKey
	default:
		return kubernetesHealthCheckStepKey
	}
}

func safeExecutionShortLogTail(jobType enum.JobType, shortLogTail string) string {
	switch jobType {
	case enum.JobTypeAgentRun:
		return ""
	case enum.JobTypeBuild:
		return redactBuildLogTail(shortLogTail)
	default:
		return shortLogTail
	}
}

func redactBuildLogTail(shortLogTail string) string {
	if strings.TrimSpace(shortLogTail) == "" {
		return ""
	}
	if runtimekubernetes.ContainsUnsafeDiagnosticMarker(shortLogTail) {
		return redactedDiagnosticValue
	}
	return shortLogTail
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

func (w kubernetesJobWorker) pollDelay() time.Duration {
	if w.cfg.PollInterval > 0 {
		return w.cfg.PollInterval
	}
	return minWorkerRetryDelay
}

func (w kubernetesJobWorker) baseRetryDelay() time.Duration {
	return maxDuration(w.pollDelay(), minWorkerRetryDelay)
}

func (w kubernetesJobWorker) maxRetryDelay() time.Duration {
	return maxDuration(w.baseRetryDelay(), maxWorkerRetryDelay)
}

func workerResultForContext(ctx context.Context) kubernetesWorkerIteration {
	if ctx.Err() != nil {
		return kubernetesWorkerProcessed
	}
	return kubernetesWorkerRetryableError
}

func doubleDuration(value time.Duration, limit time.Duration) time.Duration {
	if value <= 0 {
		return limit
	}
	if value > limit/2 {
		return limit
	}
	return value * 2
}

func maxDuration(left time.Duration, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nextActionForKubernetesError(code string) string {
	switch strings.TrimSpace(code) {
	case "build_context_ref_required", "build_context_pvc_unavailable", "build_context_pvc_not_ready", "build_context_pvc_status_unavailable":
		return "prepare_build_context_pvc"
	case "build_registry_secret_ref_required", "build_registry_secret_unavailable", "build_registry_secret_status_unavailable":
		return "provide_build_registry_secret_ref"
	case "build_context_pvc_access_denied", "build_registry_secret_access_denied", "kubernetes_service_account_unavailable", "kubernetes_service_account_access_denied", "kubernetes_service_account_status_unavailable", "kubernetes_job_create_access_denied":
		return "fix_runtime_kubernetes_rbac"
	default:
		return "review_runtime_kubernetes_job"
	}
}

func (w kubernetesJobWorker) log() *slog.Logger {
	if w.logger != nil {
		return w.logger
	}
	return slog.Default()
}
