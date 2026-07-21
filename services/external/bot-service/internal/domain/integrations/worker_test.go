package integrations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerRepositoryStub struct {
	claim       ExecutionClaim
	found       bool
	finalizeErr error
	failCalls   int
}

func (stub *workerRepositoryStub) ListCatalog(context.Context, SessionContext, time.Time) ([]CatalogEntry, error) {
	return nil, nil
}
func (stub *workerRepositoryStub) CreateOrReplayInvocation(context.Context, CreateInvocationInput) (Invocation, bool, error) {
	return Invocation{}, false, nil
}
func (stub *workerRepositoryStub) ClaimApprovalDelivery(context.Context, int64, string, time.Time, time.Duration) (ApprovalDelivery, bool, error) {
	return ApprovalDelivery{}, false, nil
}
func (stub *workerRepositoryStub) CompleteApprovalDelivery(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (stub *workerRepositoryStub) ReleaseApprovalDelivery(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (stub *workerRepositoryStub) DecideApproval(context.Context, ApprovalDecisionInput) (Invocation, error) {
	return Invocation{}, nil
}
func (stub *workerRepositoryStub) ClaimExecution(context.Context, string, string, time.Time, time.Duration) (ExecutionClaim, bool, error) {
	return stub.claim, stub.found, nil
}
func (stub *workerRepositoryStub) CancelExecution(context.Context, ExecutionClaim, string, time.Time) error {
	stub.failCalls++
	return nil
}
func (stub *workerRepositoryStub) FinalizeExecution(context.Context, ExecutionClaim, time.Time) (Invocation, error) {
	return Invocation{}, stub.finalizeErr
}

type workerExecutorStub struct {
	receipt ExecutionReceipt
	err     error
}

func (stub workerExecutorStub) Execute(context.Context, ExecutionClaim) (ExecutionReceipt, error) {
	return stub.receipt, stub.err
}

func TestWorkerRejectsFalseSuccessWithoutReceiptReadback(t *testing.T) {
	claim := ExecutionClaim{InvocationID: 1, ExecutionFence: "fence_0123456789abcdef0123456789abcdef", ArgumentsHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LeaseOwner: "worker"}
	repository := &workerRepositoryStub{claim: claim, found: true, finalizeErr: ErrReceiptMissing}
	worker := NewWorker(WorkerConfig{Repository: repository, Executor: workerExecutorStub{receipt: ExecutionReceipt{InvocationID: 1}}, WorkerID: "worker"})
	worked, err := worker.RunOnce(context.Background())
	if !worked || !errors.Is(err, ErrReceiptMissing) {
		t.Fatalf("RunOnce() worked=%v err=%v", worked, err)
	}
}

func TestWorkerFailsClaimWhenFreshAuthorizationChanged(t *testing.T) {
	claim := ExecutionClaim{InvocationID: 1, ExecutionFence: "fence_0123456789abcdef0123456789abcdef", LeaseOwner: "worker"}
	repository := &workerRepositoryStub{claim: claim, found: true}
	worker := NewWorker(WorkerConfig{Repository: repository, Executor: workerExecutorStub{err: ErrAuthorizationChanged}, WorkerID: "worker"})
	worked, err := worker.RunOnce(context.Background())
	if !worked || !errors.Is(err, ErrAuthorizationChanged) || repository.failCalls != 1 {
		t.Fatalf("RunOnce() worked=%v err=%v failCalls=%d", worked, err, repository.failCalls)
	}
}
