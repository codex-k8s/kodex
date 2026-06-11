package app

import (
	"context"
	"strings"
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
	if len(service.lastClaimJobTypes) != 4 ||
		service.lastClaimJobTypes[0] != enum.JobTypeHealthCheck ||
		service.lastClaimJobTypes[1] != enum.JobTypeAgentRun ||
		service.lastClaimJobTypes[2] != enum.JobTypeBuild ||
		service.lastClaimJobTypes[3] != enum.JobTypeDeploy {
		t.Fatalf("claim job types = %v, want health_check, agent_run, build and deploy", service.lastClaimJobTypes)
	}
	if len(service.reportStatuses) != 2 || service.reportStatuses[0] != enum.JobStepStatusRunning || service.reportStatuses[1] != enum.JobStepStatusSucceeded {
		t.Fatalf("report statuses = %v, want running/succeeded", service.reportStatuses)
	}
	if service.completeCalls != 1 || service.failCalls != 0 {
		t.Fatalf("complete/fail calls = %d/%d, want 1/0", service.completeCalls, service.failCalls)
	}
}

func TestKubernetesJobWorkerReportsBuildStepKey(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000107"), Version: 2},
		JobType: enum.JobTypeBuild,
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
		result: runtimekubernetes.ExecutionResult{Succeeded: true, ShortLogTail: "image pushed Authorization: Bearer secret-token"},
	}
	worker := testKubernetesJobWorker(service, executor)

	worker.claimAndExecute(context.Background())

	if len(service.reportStepKeys) != 2 || service.reportStepKeys[0] != kubernetesBuildStepKey || service.reportStepKeys[1] != kubernetesBuildStepKey {
		t.Fatalf("report step keys = %v, want build step key", service.reportStepKeys)
	}
	if service.completeShortLogTail != redactedDiagnosticValue {
		t.Fatalf("complete short log tail = %q, want redacted build tail", service.completeShortLogTail)
	}
	for _, tail := range append(service.reportShortLogTails, service.completeShortLogTail) {
		if strings.Contains(strings.ToLower(tail), "authorization") || strings.Contains(strings.ToLower(tail), "bearer") || strings.Contains(tail, "secret-token") {
			t.Fatalf("reported build short log tail = %q, want redacted diagnostics", tail)
		}
	}
	if service.completeCalls != 1 || service.failCalls != 0 {
		t.Fatalf("complete/fail calls = %d/%d, want 1/0", service.completeCalls, service.failCalls)
	}
}

func TestRedactBuildLogTailRedactsWholeUnsafeTail(t *testing.T) {
	t.Parallel()

	for _, tail := range []string{
		"Authorization: Bearer token-value",
		"kubeconfig: apiVersion: v1 clusters: []",
		"stdout: prompt body",
		"registry token=secret_value",
		"-----BEGIN PRIVATE KEY-----",
	} {
		if got := redactBuildLogTail(tail); got != redactedDiagnosticValue {
			t.Fatalf("redactBuildLogTail(%q) = %q, want %q", tail, got, redactedDiagnosticValue)
		}
	}
	if got := redactBuildLogTail("image pushed to registry"); got != "image pushed to registry" {
		t.Fatalf("redactBuildLogTail(safe) = %q, want original safe tail", got)
	}
}

func TestKubernetesJobWorkerReportsAgentRunStepKey(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000105"), Version: 2},
		JobType: enum.JobTypeAgentRun,
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
	worker := testKubernetesJobWorker(service, executor)

	worker.claimAndExecute(context.Background())

	if len(service.reportStepKeys) != 2 || service.reportStepKeys[0] != kubernetesAgentRunStepKey || service.reportStepKeys[1] != kubernetesAgentRunStepKey {
		t.Fatalf("report step keys = %v, want agent_run step key", service.reportStepKeys)
	}
	if service.completeCalls != 1 || service.failCalls != 0 {
		t.Fatalf("complete/fail calls = %d/%d, want 1/0", service.completeCalls, service.failCalls)
	}
}

func TestKubernetesJobWorkerDropsAgentRunLogTailDiagnostics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		result runtimekubernetes.ExecutionResult
	}{
		{
			name:   "success",
			result: runtimekubernetes.ExecutionResult{Succeeded: true, ShortLogTail: "prompt body secret-value provider payload"},
		},
		{
			name:   "failure",
			result: runtimekubernetes.ExecutionResult{ErrorCode: "kubernetes_job_failed", ErrorMessage: "Kubernetes Job failed", ShortLogTail: "prompt body secret-value provider payload"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			job := entity.Job{
				Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000106"), Version: 2},
				JobType: enum.JobTypeAgentRun,
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
				result: tc.result,
			}
			worker := testKubernetesJobWorker(service, executor)

			worker.claimAndExecute(context.Background())

			for _, tail := range service.reportShortLogTails {
				if tail != "" || strings.Contains(tail, "secret-value") {
					t.Fatalf("reported short log tail = %q, want empty safe diagnostics", tail)
				}
			}
			if service.completeShortLogTail != "" || service.failShortLogTail != "" {
				t.Fatalf("terminal short log tails = complete %q fail %q, want empty safe diagnostics", service.completeShortLogTail, service.failShortLogTail)
			}
		})
	}
}

func TestKubernetesJobWorkerMarksTimeoutAsTimedOut(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000108"), Version: 2},
		JobType: enum.JobTypeBuild,
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
		result: runtimekubernetes.ExecutionResult{
			Phase:         runtimekubernetes.ExecutionPhaseTimedOut,
			StatusSummary: "Kubernetes Job timed out",
			ErrorCode:     "kubernetes_job_timeout",
			ErrorMessage:  "Kubernetes Job did not finish before timeout",
			ShortLogTail:  "build still running",
		},
	}
	worker := testKubernetesJobWorker(service, executor)

	worker.claimAndExecute(context.Background())

	if service.failCalls != 1 || !service.lastFailTimedOut || service.lastFailCode != "kubernetes_job_timeout" {
		t.Fatalf("fail calls/timedOut/code = %d/%v/%s, want timeout failure", service.failCalls, service.lastFailTimedOut, service.lastFailCode)
	}
	if len(service.reportStatuses) != 2 || service.reportStatuses[1] != enum.JobStepStatusFailed {
		t.Fatalf("report statuses = %v, want running then failed step", service.reportStatuses)
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
	if service.lastFailNextAction != "review_runtime_kubernetes_job" {
		t.Fatalf("next action = %q, want review_runtime_kubernetes_job", service.lastFailNextAction)
	}
	if len(service.reportStatuses) != 0 {
		t.Fatalf("report statuses = %v, want none before executor start", service.reportStatuses)
	}
}

func TestKubernetesJobWorkerMapsBuildPreflightNextAction(t *testing.T) {
	t.Parallel()

	job := entity.Job{
		Base:    entity.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000109"), Version: 2},
		JobType: enum.JobTypeBuild,
		Status:  enum.JobStatusClaimed,
	}
	service := &fakeRuntimeJobLifecycle{claim: runtimeservice.ClaimRunnableJobResult{Job: job, LeaseToken: "lease-token"}}
	worker := testKubernetesJobWorker(service, fakeKubernetesExecutor{
		startErr: &runtimekubernetes.ExecutionError{Code: "build_context_pvc_not_ready", Message: "build context PVC is not bound"},
	})

	worker.claimAndExecute(context.Background())

	if service.failCalls != 1 || service.lastFailCode != "build_context_pvc_not_ready" {
		t.Fatalf("fail calls/code = %d/%s, want build context preflight failure", service.failCalls, service.lastFailCode)
	}
	if service.lastFailNextAction != "prepare_build_context_pvc" {
		t.Fatalf("next action = %q, want prepare_build_context_pvc", service.lastFailNextAction)
	}
	if len(service.reportStatuses) != 0 {
		t.Fatalf("report statuses = %v, want none before executor start", service.reportStatuses)
	}
}

func TestNextActionForKubernetesError(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"build_context_ref_required":                    "prepare_build_context_pvc",
		"build_context_pvc_unavailable":                 "prepare_build_context_pvc",
		"build_context_pvc_not_ready":                   "prepare_build_context_pvc",
		"build_context_pvc_status_unavailable":          "prepare_build_context_pvc",
		"build_registry_secret_ref_required":            "provide_build_registry_secret_ref",
		"build_registry_secret_unavailable":             "provide_build_registry_secret_ref",
		"build_registry_secret_status_unavailable":      "provide_build_registry_secret_ref",
		"build_context_pvc_access_denied":               "fix_runtime_kubernetes_rbac",
		"build_registry_secret_access_denied":           "fix_runtime_kubernetes_rbac",
		"kubernetes_service_account_unavailable":        "fix_runtime_kubernetes_rbac",
		"kubernetes_service_account_access_denied":      "fix_runtime_kubernetes_rbac",
		"kubernetes_service_account_status_unavailable": "fix_runtime_kubernetes_rbac",
		"kubernetes_job_create_access_denied":           "fix_runtime_kubernetes_rbac",
		"kubernetes_job_status_access_denied":           "fix_runtime_kubernetes_rbac",
		"cluster_secret_unavailable":                    "review_runtime_kubernetes_job",
	}
	for code, want := range tests {
		code := code
		want := want
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			if got := nextActionForKubernetesError(code); got != want {
				t.Fatalf("next action = %q, want %q", got, want)
			}
		})
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
	claim                runtimeservice.ClaimRunnableJobResult
	claimErr             error
	claimCalls           int
	lastClaimJobTypes    []enum.JobType
	reportStatuses       []enum.JobStepStatus
	reportStepKeys       []string
	reportShortLogTails  []string
	completeCalls        int
	failCalls            int
	lastFailCode         string
	lastFailTimedOut     bool
	lastFailNextAction   string
	completeShortLogTail string
	failShortLogTail     string
}

func (s *fakeRuntimeJobLifecycle) ClaimRunnableJob(_ context.Context, input runtimeservice.ClaimRunnableJobInput) (runtimeservice.ClaimRunnableJobResult, error) {
	s.claimCalls++
	s.lastClaimJobTypes = append([]enum.JobType(nil), input.JobTypes...)
	return s.claim, s.claimErr
}

func (s *fakeRuntimeJobLifecycle) ReportJobStepProgress(_ context.Context, input runtimeservice.ReportJobStepProgressInput) (entity.Job, error) {
	s.reportStatuses = append(s.reportStatuses, input.Status)
	s.reportStepKeys = append(s.reportStepKeys, input.StepKey)
	s.reportShortLogTails = append(s.reportShortLogTails, input.ShortLogTail)
	job := s.claim.Job
	if len(s.reportStatuses) > 0 {
		job.Version += int64(len(s.reportStatuses))
	}
	return job, nil
}

func (s *fakeRuntimeJobLifecycle) CompleteJob(_ context.Context, input runtimeservice.CompleteJobInput) (entity.Job, error) {
	s.completeCalls++
	s.completeShortLogTail = input.ShortLogTail
	job := s.claim.Job
	job.Status = enum.JobStatusSucceeded
	return job, nil
}

func (s *fakeRuntimeJobLifecycle) FailJob(_ context.Context, input runtimeservice.FailJobInput) (entity.Job, error) {
	s.failCalls++
	s.lastFailCode = input.ErrorCode
	s.lastFailTimedOut = input.TimedOut
	s.lastFailNextAction = input.NextAction
	s.failShortLogTail = input.ShortLogTail
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
