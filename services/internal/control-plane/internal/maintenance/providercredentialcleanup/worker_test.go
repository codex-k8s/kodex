package providercredentialcleanup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testLeaseOwner = "61000000-0000-4000-8000-000000000001"
	testTaskRef    = "pcct_61000000-0000-4000-8000-000000000002"
	testAccountRef = "pacc_cleanup_account"
	testReceipt    = "provider-credential-cleanup:61000000-0000-4000-8000-000000000002:g7"
)

var testCredential = entity.ProviderCredentialDescriptor{
	SecretName: "provider-credential-1", SecretUID: "61000000-0000-4000-8000-000000000003",
	SecretResourceVersion: "cleanup-7", ContentSHA256: strings.Repeat("a", 64),
}

type completeCall struct {
	taskRef, leaseOwner, receipt string
	generation                   int64
	completion                   entity.ProviderAuthorizationCleanupResult
}

type failCall struct {
	taskRef, leaseOwner, safeCode string
	generation                    int64
}

type cleanupCall struct {
	taskRef, accountRef string
	generation          int64
	credential          entity.ProviderCredentialDescriptor
}

type repositoryStub struct {
	mu            sync.Mutex
	claim         func(context.Context, string, int32) ([]platformrepository.ProviderCredentialCleanupTask, error)
	completeErr   error
	failErr       error
	completeCalls []completeCall
	failCalls     []failCall
}

func (stub *repositoryStub) ClaimProviderCredentialCleanupTasks(
	ctx context.Context,
	leaseOwner string,
	limit int32,
) ([]platformrepository.ProviderCredentialCleanupTask, error) {
	return stub.claim(ctx, leaseOwner, limit)
}

func (stub *repositoryStub) CompleteProviderCredentialCleanupTask(
	_ context.Context,
	taskRef, leaseOwner string,
	generation int64,
	completion entity.ProviderAuthorizationCleanupResult,
) (platformrepository.ProviderCredentialCleanupResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.completeCalls = append(stub.completeCalls, completeCall{
		taskRef: taskRef, leaseOwner: leaseOwner, generation: generation, receipt: completion.TerminalReceipt,
		completion: completion,
	})
	return platformrepository.ProviderCredentialCleanupResult{}, stub.completeErr
}

func (stub *repositoryStub) FailProviderCredentialCleanupTask(
	_ context.Context,
	taskRef, leaseOwner string,
	generation int64,
	safeCode string,
) (platformrepository.ProviderCredentialCleanupResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.failCalls = append(stub.failCalls, failCall{
		taskRef: taskRef, leaseOwner: leaseOwner, generation: generation, safeCode: safeCode,
	})
	return platformrepository.ProviderCredentialCleanupResult{RetryScheduled: true}, stub.failErr
}

type materializerStub struct {
	cleanup              func(context.Context, string, string, int64, entity.ProviderCredentialDescriptor) (string, error)
	produced             *entity.ProviderCredentialDescriptor
	observe              func(context.Context, entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupObservation, error)
	cleanupAuthorization func(context.Context, entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupResult, error)
}

func (stub *materializerStub) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	generation int64,
	credential entity.ProviderCredentialDescriptor,
	_ ...*entity.ProviderCleanupRecoveryIdentity,
) (entity.ProviderAuthorizationCleanupResult, error) {
	receipt, err := stub.cleanup(ctx, taskRef, accountRef, generation, credential)
	return entity.ProviderAuthorizationCleanupResult{TerminalReceipt: receipt, ProducedCredential: stub.produced}, err
}

func (stub *materializerStub) ObserveAuthorizationCleanup(ctx context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupObservation, error) {
	if stub.observe == nil {
		return entity.ProviderAuthorizationCleanupObservation{}, errors.New("unexpected metadata call")
	}
	return stub.observe(ctx, target)
}

func (stub *materializerStub) CleanupAuthorization(ctx context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupResult, error) {
	if stub.cleanupAuthorization == nil {
		return entity.ProviderAuthorizationCleanupResult{}, errors.New("unexpected authorization cleanup call")
	}
	return stub.cleanupAuthorization(ctx, target)
}

func TestWorkerCompletesSuccessfulCleanup(t *testing.T) {
	t.Parallel()
	task := cleanupTask(testTaskRef, 7)
	var cleanup cleanupCall
	repository := &repositoryStub{claim: func(_ context.Context, owner string, limit int32) ([]platformrepository.ProviderCredentialCleanupTask, error) {
		if owner != testLeaseOwner || limit != 16 {
			t.Fatalf("claim identity: owner=%q limit=%d", owner, limit)
		}
		return []platformrepository.ProviderCredentialCleanupTask{task}, nil
	}}
	materializer := &materializerStub{cleanup: func(_ context.Context, taskRef, accountRef string, generation int64, credential entity.ProviderCredentialDescriptor) (string, error) {
		cleanup = cleanupCall{taskRef: taskRef, accountRef: accountRef, generation: generation, credential: credential}
		return testReceipt, nil
	}}
	worker := newTestWorker(t, repository, materializer)
	processed, err := worker.runCycle(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("run cycle: processed=%d err=%v", processed, err)
	}
	wantCleanup := cleanupCall{
		taskRef: task.Ref, accountRef: task.AccountRef,
		generation: task.Generation, credential: task.Credential,
	}
	if cleanup != wantCleanup {
		t.Fatalf("cleanup call: got=%#v want=%#v", cleanup, wantCleanup)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.completeCalls) != 1 || len(repository.failCalls) != 0 {
		t.Fatalf("finalization calls: complete=%d fail=%d", len(repository.completeCalls), len(repository.failCalls))
	}
	want := completeCall{taskRef: task.Ref, leaseOwner: testLeaseOwner, generation: task.Generation, receipt: testReceipt,
		completion: entity.ProviderAuthorizationCleanupResult{TerminalReceipt: testReceipt}}
	if repository.completeCalls[0] != want {
		t.Fatalf("complete call: got=%#v want=%#v", repository.completeCalls[0], want)
	}
}

func TestWorkerPreservesProducedCredentialAndMetadataCompletion(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"CREDENTIAL", "AUTHORIZATION_ATTEMPT", "AUTHORIZATION_ABSENCE", "AUTHORIZATION_METADATA"} {
		t.Run(kind, func(t *testing.T) {
			task := cleanupTask(testTaskRef, 7)
			task.TargetKind = kind
			task.Authorization = entity.ProviderAuthorizationCleanupTarget{TaskRef: task.Ref, AccountRef: task.AccountRef, Generation: task.Generation}
			repository := &repositoryStub{}
			materializer := &materializerStub{
				produced: &testCredential,
				cleanup: func(context.Context, string, string, int64, entity.ProviderCredentialDescriptor) (string, error) {
					return testReceipt, nil
				},
				cleanupAuthorization: func(_ context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupResult, error) {
					if target != task.Authorization {
						t.Fatal("cleanup owner target lost")
					}
					return entity.ProviderAuthorizationCleanupResult{TerminalReceipt: testReceipt, ProducedCredential: &testCredential}, nil
				},
				observe: func(_ context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupObservation, error) {
					if target != task.Authorization {
						t.Fatal("metadata owner target lost")
					}
					return entity.ProviderAuthorizationCleanupObservation{State: "ABSENT_UNFENCED", Target: target, ProducedCredential: &testCredential}, nil
				},
			}
			if stage := newTestWorker(t, repository, materializer).processTask(context.Background(), task); stage != "" {
				t.Fatalf("unexpected completion stage %s", stage)
			}
			if len(repository.completeCalls) != 1 || len(repository.failCalls) != 0 {
				t.Fatal("cleanup result was not completed exactly once")
			}
			completion := repository.completeCalls[0].completion
			if kind == "AUTHORIZATION_METADATA" {
				if completion.Observation == nil || completion.Observation.State != "ABSENT_UNFENCED" || completion.TerminalReceipt != "" || completion.ProducedCredential != nil {
					t.Fatal("metadata read was promoted to cleanup receipt")
				}
			} else if completion.ProducedCredential == nil || *completion.ProducedCredential != testCredential || completion.TerminalReceipt != testReceipt {
				t.Fatal("produced credential was lost before owner completion")
			}
		})
	}
}

func TestWorkerFailsRetryableTaskAndContinuesBatch(t *testing.T) {
	t.Parallel()
	failedTask := cleanupTask(testTaskRef, 7)
	successfulTask := cleanupTask("pcct_61000000-0000-4000-8000-000000000004", 8)
	repository := &repositoryStub{claim: func(context.Context, string, int32) ([]platformrepository.ProviderCredentialCleanupTask, error) {
		return []platformrepository.ProviderCredentialCleanupTask{failedTask, successfulTask}, nil
	}}
	materializer := &materializerStub{cleanup: func(_ context.Context, taskRef, _ string, _ int64, _ entity.ProviderCredentialDescriptor) (string, error) {
		if taskRef == failedTask.Ref {
			return "", status.Error(codes.Unavailable, "materializer unavailable")
		}
		return testReceipt, nil
	}}
	worker := newTestWorker(t, repository, materializer)
	processed, err := worker.runCycle(context.Background())
	if err != nil || processed != 2 {
		t.Fatalf("run cycle: processed=%d err=%v", processed, err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.failCalls) != 1 || len(repository.completeCalls) != 1 {
		t.Fatalf("batch stopped after task error: complete=%d fail=%d", len(repository.completeCalls), len(repository.failCalls))
	}
	wantFail := failCall{
		taskRef: failedTask.Ref, leaseOwner: testLeaseOwner,
		generation: failedTask.Generation, safeCode: SafeCodeUnavailable,
	}
	if repository.failCalls[0] != wantFail || repository.completeCalls[0].taskRef != successfulTask.Ref {
		t.Fatalf("batch finalization: complete=%#v fail=%#v", repository.completeCalls, repository.failCalls)
	}
}

func TestWorkerCancellationDoesNotFinalizeClaim(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	repository := &repositoryStub{claim: func(context.Context, string, int32) ([]platformrepository.ProviderCredentialCleanupTask, error) {
		return []platformrepository.ProviderCredentialCleanupTask{cleanupTask(testTaskRef, 7)}, nil
	}}
	materializer := &materializerStub{cleanup: func(ctx context.Context, _ string, _ string, _ int64, _ entity.ProviderCredentialDescriptor) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	worker := newTestWorker(t, repository, materializer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cancellation: %v", err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.completeCalls) != 0 || len(repository.failCalls) != 0 {
		t.Fatalf("cancelled claim was finalized: complete=%d fail=%d", len(repository.completeCalls), len(repository.failCalls))
	}
}

func TestWorkerClaimHealthDegradesAndRecovers(t *testing.T) {
	failed := make(chan struct{})
	allowRecovery := make(chan struct{})
	recovered := make(chan struct{})
	var calls int
	repository := &repositoryStub{claim: func(ctx context.Context, _ string, _ int32) ([]platformrepository.ProviderCredentialCleanupTask, error) {
		calls++
		if calls == 1 {
			close(failed)
			return nil, errors.New("claim unavailable")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-allowRecovery:
			if calls == 2 {
				close(recovered)
			}
			return nil, nil
		}
	}}
	materializer := &materializerStub{cleanup: func(context.Context, string, string, int64, entity.ProviderCredentialDescriptor) (string, error) {
		return "", errors.New("unexpected cleanup")
	}}
	health := serviceruntime.NewReadiness()
	worker, err := New(repository, materializer, health, discardLogger(), Config{
		LeaseOwner: testLeaseOwner, BatchSize: 16, PollInterval: minimumPollInterval, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-failed
	waitForHealth(t, health, false, claimUnavailableReason)
	close(allowRecovery)
	<-recovered
	waitForHealth(t, health, true, "ready")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("stop worker: %v", err)
	}
}

func TestSafeErrorCodeUsesOnlyRepositoryCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "unavailable", err: status.Error(codes.Unavailable, "unavailable"), want: SafeCodeUnavailable},
		{name: "resource exhausted", err: status.Error(codes.ResourceExhausted, "busy"), want: SafeCodeUnavailable},
		{name: "rejected", err: status.Error(codes.FailedPrecondition, "rejected"), want: SafeCodeRejected},
		{name: "deadline", err: context.DeadlineExceeded, want: SafeCodeTimeout},
		{name: "failed", err: errors.New("failed"), want: SafeCodeFailed},
	}
	allowed := map[string]bool{
		SafeCodeUnavailable: true, SafeCodeRejected: true, SafeCodeTimeout: true, SafeCodeFailed: true,
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := safeErrorCode(test.err)
			if got != test.want || !allowed[got] {
				t.Fatalf("safe code: got=%q want=%q", got, test.want)
			}
		})
	}
}

func TestCASSnapshotRecoveryRequiresExactAuthenticatedDetail(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, domain, reason string
		code                 codes.Code
		metadata             map[string]string
		want                 string
	}{
		{"exact", "kodex.provider_credential_cleanup", "CAS_SNAPSHOT_CHANGED", codes.FailedPrecondition, nil, SafeCodeCASChanged},
		{"foreign domain", "foreign", "CAS_SNAPSHOT_CHANGED", codes.FailedPrecondition, nil, SafeCodeRejected},
		{"unknown reason", "kodex.provider_credential_cleanup", "UNKNOWN", codes.FailedPrecondition, nil, SafeCodeRejected},
		{"timeout", "kodex.provider_credential_cleanup", "CAS_SNAPSHOT_CHANGED", codes.DeadlineExceeded, nil, SafeCodeTimeout},
		{"unexpected metadata", "kodex.provider_credential_cleanup", "CAS_SNAPSHOT_CHANGED", codes.FailedPrecondition, map[string]string{"unexpected": "value"}, SafeCodeRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, err := status.New(test.code, "cleanup failed").WithDetails(&errdetails.ErrorInfo{Domain: test.domain, Reason: test.reason, Metadata: test.metadata})
			if err != nil {
				t.Fatal(err)
			}
			if got := safeErrorCode(st.Err()); got != test.want {
				t.Fatalf("safe error code: got %s want %s", got, test.want)
			}
		})
	}
}

func cleanupTask(ref string, generation int64) platformrepository.ProviderCredentialCleanupTask {
	return platformrepository.ProviderCredentialCleanupTask{
		Ref: ref, AccountRef: testAccountRef, Generation: generation,
		TargetKind: "CREDENTIAL",
		Credential: testCredential, LeaseExpiresAt: time.Now().Add(time.Minute),
	}
}

func newTestWorker(t *testing.T, repository Repository, materializer Materializer) *Worker {
	t.Helper()
	health := serviceruntime.NewReadiness()
	worker, err := New(repository, materializer, health, discardLogger(), Config{
		LeaseOwner: testLeaseOwner, BatchSize: 16,
		PollInterval: minimumPollInterval, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	return worker
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForHealth(t *testing.T, health *serviceruntime.Readiness, wantReady bool, wantReason string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ready, reason := health.Ready()
		if ready == wantReady && reason == wantReason {
			return
		}
		time.Sleep(time.Millisecond)
	}
	ready, reason := health.Ready()
	t.Fatalf("health did not converge: ready=%t reason=%q", ready, reason)
}
