package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	runtimekubernetes "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/clients/kubernetes"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func TestKubernetesJobWorkerCompletesClaimedHealthCheck(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000101"), Version: 2},
		JobType: enum.JobTypeHealthCheck,
		Status:  enum.JobStatusClaimed,
	}
	service := &fakeRuntimeJobLifecycle{claim: runtimeservice.ClaimRunnableJobResult{Job: job, LeaseToken: "lease-token"}}
	executor := fakeKubernetesExecutor{
		started: runtimekubernetes.StartedJob{
			RuntimeJobID: job.ID,
			Namespace:    "runtime-jobs",
			JobName:      "kodex-rt-test",
			ExternalRef:  "kubernetes://cluster/namespaces/runtime-jobs/jobs/kodex-rt-test",
		},
		result: runtimekubernetes.ExecutionResult{Succeeded: true, ShortLogTail: "ok"},
	}
	worker := kubernetesJobWorker{
		service:  service,
		executor: executor,
		cfg: RuntimeKubernetesWorkerConfig{
			WorkerID:      "runtime-manager-kubernetes-executor",
			PollInterval:  time.Second,
			ClaimLeaseTTL: time.Minute,
		},
	}

	worker.claimAndExecute(context.Background())

	if service.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", service.claimCalls)
	}
	if len(service.reportStatuses) != 2 || service.reportStatuses[0] != enum.JobStepStatusRunning || service.reportStatuses[1] != enum.JobStepStatusSucceeded {
		t.Fatalf("report statuses = %v, want running/succeeded", service.reportStatuses)
	}
	if service.completeCalls != 1 || service.failCalls != 0 {
		t.Fatalf("complete/fail calls = %d/%d, want 1/0", service.completeCalls, service.failCalls)
	}
}

func TestKubernetesJobWorkerFailsClaimedJobOnExecutorError(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000102"), Version: 2},
		JobType: enum.JobTypeHealthCheck,
		Status:  enum.JobStatusClaimed,
	}
	service := &fakeRuntimeJobLifecycle{claim: runtimeservice.ClaimRunnableJobResult{Job: job, LeaseToken: "lease-token"}}
	executor := fakeKubernetesExecutor{startErr: &runtimekubernetes.ExecutionError{Code: "cluster_secret_unavailable", Message: "Kubernetes cluster access secret is unavailable"}}
	worker := kubernetesJobWorker{
		service:  service,
		executor: executor,
		cfg: RuntimeKubernetesWorkerConfig{
			WorkerID:      "runtime-manager-kubernetes-executor",
			PollInterval:  time.Second,
			ClaimLeaseTTL: time.Minute,
		},
	}

	worker.claimAndExecute(context.Background())

	if service.failCalls != 1 || service.lastFailCode != "cluster_secret_unavailable" {
		t.Fatalf("fail calls/code = %d/%s, want one cluster_secret_unavailable", service.failCalls, service.lastFailCode)
	}
	if len(service.reportStatuses) != 0 {
		t.Fatalf("report statuses = %v, want none before executor start", service.reportStatuses)
	}
}

func TestKubernetesJobWorkerLeavesClaimWhenWaitIsInterrupted(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000103"), Version: 2},
		JobType: enum.JobTypeHealthCheck,
		Status:  enum.JobStatusClaimed,
	}
	service := &fakeRuntimeJobLifecycle{claim: runtimeservice.ClaimRunnableJobResult{Job: job, LeaseToken: "lease-token"}}
	executor := fakeKubernetesExecutor{
		started: runtimekubernetes.StartedJob{
			RuntimeJobID: job.ID,
			Namespace:    "runtime-jobs",
			JobName:      "kodex-rt-test",
			ExternalRef:  "kubernetes://cluster/namespaces/runtime-jobs/jobs/kodex-rt-test",
		},
		result: runtimekubernetes.ExecutionResult{Interrupted: true, ErrorCode: "runtime_worker_stopped"},
	}
	worker := testKubernetesJobWorker(service, executor)

	result := worker.claimAndExecute(context.Background())

	if result != kubernetesWorkerProcessed {
		t.Fatalf("iteration result = %v, want processed", result)
	}
	if len(service.reportStatuses) != 1 || service.reportStatuses[0] != enum.JobStepStatusRunning {
		t.Fatalf("report statuses = %v, want only running", service.reportStatuses)
	}
	if service.completeCalls != 0 || service.failCalls != 0 {
		t.Fatalf("complete/fail calls = %d/%d, want 0/0", service.completeCalls, service.failCalls)
	}
}

func TestKubernetesJobWorkerRetriesAfterClaimError(t *testing.T) {
	t.Parallel()

	service := &fakeRuntimeJobLifecycle{claimErr: context.DeadlineExceeded}
	worker := testKubernetesJobWorker(service, fakeKubernetesExecutor{})

	result := worker.claimAndExecute(context.Background())

	if result != kubernetesWorkerRetryableError {
		t.Fatalf("iteration result = %v, want retryable error", result)
	}
	if service.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", service.claimCalls)
	}
}

func TestKubernetesJobWorkerDoesNotFailClaimWhenStartIsInterrupted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000104"), Version: 2},
		JobType: enum.JobTypeHealthCheck,
		Status:  enum.JobStatusClaimed,
	}
	service := &fakeRuntimeJobLifecycle{claim: runtimeservice.ClaimRunnableJobResult{Job: job, LeaseToken: "lease-token"}}
	worker := testKubernetesJobWorker(service, fakeKubernetesExecutor{startErr: context.Canceled})

	result := worker.claimAndExecute(ctx)

	if result != kubernetesWorkerProcessed {
		t.Fatalf("iteration result = %v, want processed", result)
	}
	if service.failCalls != 0 || len(service.reportStatuses) != 0 {
		t.Fatalf("fail/report = %d/%v, want no lifecycle mutation", service.failCalls, service.reportStatuses)
	}
}

func TestKubernetesJobWorkerRetryDelayBounds(t *testing.T) {
	t.Parallel()

	worker := kubernetesJobWorker{cfg: RuntimeKubernetesWorkerConfig{PollInterval: 10 * time.Millisecond}}

	if got := worker.baseRetryDelay(); got != time.Second {
		t.Fatalf("base retry delay = %s, want 1s", got)
	}
	if got := doubleDuration(20*time.Second, worker.maxRetryDelay()); got != 30*time.Second {
		t.Fatalf("doubled retry delay = %s, want 30s cap", got)
	}
}

func testKubernetesJobWorker(service *fakeRuntimeJobLifecycle, executor fakeKubernetesExecutor) kubernetesJobWorker {
	return kubernetesJobWorker{
		service:  service,
		executor: executor,
		cfg: RuntimeKubernetesWorkerConfig{
			WorkerID:      "runtime-manager-kubernetes-executor",
			PollInterval:  time.Second,
			ClaimLeaseTTL: time.Minute,
		},
	}
}

type fakeRuntimeJobLifecycle struct {
	claim          runtimeservice.ClaimRunnableJobResult
	claimErr       error
	claimCalls     int
	reportStatuses []enum.JobStepStatus
	completeCalls  int
	failCalls      int
	lastFailCode   string
}

func (s *fakeRuntimeJobLifecycle) ClaimRunnableJob(context.Context, runtimeservice.ClaimRunnableJobInput) (runtimeservice.ClaimRunnableJobResult, error) {
	s.claimCalls++
	return s.claim, s.claimErr
}

func (s *fakeRuntimeJobLifecycle) ReportJobStepProgress(_ context.Context, input runtimeservice.ReportJobStepProgressInput) (entity.Job, error) {
	s.reportStatuses = append(s.reportStatuses, input.Status)
	job := s.claim.Job
	if len(s.reportStatuses) > 0 {
		job.Version += int64(len(s.reportStatuses))
	}
	return job, nil
}

func (s *fakeRuntimeJobLifecycle) CompleteJob(context.Context, runtimeservice.CompleteJobInput) (entity.Job, error) {
	s.completeCalls++
	job := s.claim.Job
	job.Status = enum.JobStatusSucceeded
	return job, nil
}

func (s *fakeRuntimeJobLifecycle) FailJob(_ context.Context, input runtimeservice.FailJobInput) (entity.Job, error) {
	s.failCalls++
	s.lastFailCode = input.ErrorCode
	job := s.claim.Job
	job.Status = enum.JobStatusFailed
	return job, nil
}

type fakeKubernetesExecutor struct {
	started  runtimekubernetes.StartedJob
	result   runtimekubernetes.ExecutionResult
	startErr error
}

func (e fakeKubernetesExecutor) Start(context.Context, entity.Job) (runtimekubernetes.StartedJob, error) {
	if e.startErr != nil {
		return runtimekubernetes.StartedJob{}, e.startErr
	}
	return e.started, nil
}

func (e fakeKubernetesExecutor) Wait(context.Context, runtimekubernetes.StartedJob) runtimekubernetes.ExecutionResult {
	return e.result
}
