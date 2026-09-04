package recovery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type fakeOwner struct {
	mu                 sync.Mutex
	decisions          map[string]controlplanev1.RuntimeSecretRecoveryAction
	states             map[string]controlplanev1.RuntimeSecretOperationState
	calls              []string
	err                error
	work               []RecoveryWork
	workError          error
	failed             []failedClaim
	failError          error
	projectionValidity map[string]bool
	projectionError    error
	projectionCalls    []string
}

type failedClaim struct {
	operationRef    string
	claimantID      string
	claimGeneration int64
	failureCode     controlplanev1.RuntimeSecretFailureCode
}

func (owner *fakeOwner) Recover(_ context.Context, operationRef string, _ *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.calls = append(owner.calls, operationRef)
	if owner.err != nil {
		return nil, owner.err
	}
	return &controlplanev1.RecoverRuntimeSecretMaterializationResponse{
		Action: owner.decisions[operationRef], OperationState: owner.states[operationRef],
	}, nil
}

func (owner *fakeOwner) ListRecoveryWork(context.Context) ([]RecoveryWork, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return append([]RecoveryWork(nil), owner.work...), owner.workError
}

func (owner *fakeOwner) FailExpiredClaim(_ context.Context, operationRef, claimantID string, generation int64, code controlplanev1.RuntimeSecretFailureCode) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.failed = append(owner.failed, failedClaim{
		operationRef: operationRef, claimantID: claimantID, claimGeneration: generation, failureCode: code,
	})
	return owner.failError
}

func (owner *fakeOwner) ValidateRuntimeCredentialProjection(_ context.Context, request *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.projectionCalls = append(owner.projectionCalls, request.GetLeaseRef())
	return owner.projectionValidity[request.GetLeaseRef()], owner.projectionError
}

type fakeStore struct {
	items                 []kubernetesstore.Materialization
	deleted               []string
	deleteError           error
	listError             error
	readError             error
	lookup                kubernetesstore.Materialization
	lookupError           error
	projections           []kubernetesstore.CredentialProjection
	deletedProjections    []string
	projectionDeleteError error
}

func (store *fakeStore) LookupExpectedEffect(context.Context, kubernetesstore.MaterializationEffect) (kubernetesstore.Materialization, error) {
	return store.lookup, store.lookupError
}

func (store *fakeStore) ListManaged(context.Context) ([]kubernetesstore.Materialization, error) {
	return append([]kubernetesstore.Materialization(nil), store.items...), store.listError
}

func (store *fakeStore) ReadbackExact(_ context.Context, materialization kubernetesstore.Materialization) (kubernetesstore.Materialization, error) {
	return materialization, store.readError
}

func (store *fakeStore) DeleteExact(_ context.Context, materialization kubernetesstore.Materialization) error {
	store.deleted = append(store.deleted, materialization.OperationRef)
	return store.deleteError
}

func (store *fakeStore) ListRuntimeCredentialProjections(context.Context) ([]kubernetesstore.CredentialProjection, error) {
	return append([]kubernetesstore.CredentialProjection(nil), store.projections...), nil
}

func (store *fakeStore) DeleteRuntimeCredentialProjection(_ context.Context, projection kubernetesstore.CredentialProjection) error {
	store.deletedProjections = append(store.deletedProjections, projection.SecretName)
	return store.projectionDeleteError
}

func TestCredentialProjectionRecoveryDeletesRevokedOrChangedBinding(t *testing.T) {
	t.Parallel()
	keep := kubernetesstore.CredentialProjection{SecretName: "projection-keep", Manifest: kubernetesstore.CredentialProjectionManifest{LeaseRef: "lease-keep"}}
	remove := kubernetesstore.CredentialProjection{SecretName: "projection-remove", Manifest: kubernetesstore.CredentialProjectionManifest{LeaseRef: "lease-remove"}}
	owner := &fakeOwner{projectionValidity: map[string]bool{"lease-keep": true}}
	store := &fakeStore{projections: []kubernetesstore.CredentialProjection{keep, remove}}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableCredentialProjectionRecovery(owner, store); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owner.projectionCalls) != 2 || len(store.deletedProjections) != 1 || store.deletedProjections[0] != "projection-remove" {
		t.Fatalf("unexpected projection recovery: calls=%v deleted=%v", owner.projectionCalls, store.deletedProjections)
	}
}

func TestRecoveryCompletesOrphanAndDeletesRejectedMaterialization(t *testing.T) {
	t.Parallel()
	keep := recoveryMaterialization("secop_keep", 1)
	remove := recoveryMaterialization("secop_delete", 2)
	owner := &fakeOwner{
		decisions: map[string]controlplanev1.RuntimeSecretRecoveryAction{
			keep.OperationRef:   controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP,
			remove.OperationRef: controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE,
		},
		states: map[string]controlplanev1.RuntimeSecretOperationState{
			keep.OperationRef:   controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED,
			remove.OperationRef: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED,
		},
	}
	store := &fakeStore{items: []kubernetesstore.Materialization{keep, remove}}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Check(context.Background()); err != nil {
		t.Fatalf("healthy recovery must participate in readiness: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != remove.OperationRef {
		t.Fatalf("unexpected exact cleanup decisions: %v", store.deleted)
	}
}

func TestFailedDeleteIsRecoveredByNextScan(t *testing.T) {
	t.Parallel()
	item := recoveryMaterialization("secop_retry_delete", 3)
	owner := &fakeOwner{
		decisions: map[string]controlplanev1.RuntimeSecretRecoveryAction{item.OperationRef: controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE},
		states:    map[string]controlplanev1.RuntimeSecretOperationState{item.OperationRef: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED},
	}
	store := &fakeStore{items: []kubernetesstore.Materialization{item}, deleteError: errors.New("synthetic outage")}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("failed exact delete must keep recovery unhealthy")
	}
	if err := reconciler.Check(context.Background()); err == nil {
		t.Fatal("failed recovery must be visible in readiness")
	}
	store.deleteError = nil
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 2 {
		t.Fatalf("failed delete was not retried: %v", store.deleted)
	}
}

func TestRecoveryRejectsIncompleteOwnerDecision(t *testing.T) {
	t.Parallel()
	item := recoveryMaterialization("secop_incomplete", 4)
	reconciler := newTestReconciler(t, &fakeOwner{
		decisions: map[string]controlplanev1.RuntimeSecretRecoveryAction{item.OperationRef: controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP},
		states:    map[string]controlplanev1.RuntimeSecretOperationState{},
	}, &fakeStore{items: []kubernetesstore.Materialization{item}})
	if err := reconciler.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("incomplete owner decision must fail closed")
	}
}

func TestExpiredCreateWithoutMaterializationFailsOriginalFence(t *testing.T) {
	t.Parallel()
	owner := &fakeOwner{work: []RecoveryWork{recoveryWork(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE)}}
	store := &fakeStore{lookupError: kubernetesstore.ErrMaterializationNotFound}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, "kodex-runtime"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owner.failed) != 1 || owner.failed[0].claimantID != "old-pod-uid" || owner.failed[0].claimGeneration != 17 ||
		owner.failed[0].failureCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED {
		t.Fatalf("expired claim завершена не исходным fence: %#v", owner.failed)
	}
}

func TestExpiredCreateWithExactEffectFallsThroughToManagedRecovery(t *testing.T) {
	t.Parallel()
	work := recoveryWork(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE)
	materialized := recoveryMaterialization(work.OperationRef, work.ClaimGeneration)
	owner := &fakeOwner{
		work: []RecoveryWork{work},
		decisions: map[string]controlplanev1.RuntimeSecretRecoveryAction{
			work.OperationRef: controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP,
		},
		states: map[string]controlplanev1.RuntimeSecretOperationState{
			work.OperationRef: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED,
		},
	}
	store := &fakeStore{lookup: materialized, items: []kubernetesstore.Materialization{materialized}}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, "kodex-runtime"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owner.failed) != 0 || len(owner.calls) != 1 || owner.calls[0] != work.OperationRef {
		t.Fatalf("exact effect не передан обычному managed recovery: failed=%#v calls=%#v", owner.failed, owner.calls)
	}
}

func TestExpiredCreateConflictFailsClaimAndKeepsObject(t *testing.T) {
	t.Parallel()
	owner := &fakeOwner{work: []RecoveryWork{recoveryWork(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE)}}
	store := &fakeStore{lookupError: kubernetesstore.ErrMaterializationConflict}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, "kodex-runtime"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("materialization conflict должен оставить recovery unhealthy до ручной проверки")
	}
	if len(owner.failed) != 1 || owner.failed[0].failureCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT || len(store.deleted) != 0 {
		t.Fatalf("conflict обработан небезопасно: failed=%#v deleted=%#v", owner.failed, store.deleted)
	}
}

func TestExpiredRevealAndRevokeFailWithoutMaterialization(t *testing.T) {
	t.Parallel()
	owner := &fakeOwner{work: []RecoveryWork{
		recoveryWork(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL),
		recoveryWork(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE),
	}}
	store := &fakeStore{}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, "kodex-runtime"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owner.failed) != 2 {
		t.Fatalf("не все operation без durable effect завершены: %#v", owner.failed)
	}
	for _, failed := range owner.failed {
		if failed.failureCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED {
			t.Fatalf("неожиданный failure code: %#v", failed)
		}
	}
}

func TestWorkerStopsOnLifecycleCancellation(t *testing.T) {
	t.Parallel()
	reconciler := newTestReconciler(t, &fakeOwner{}, &fakeStore{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- reconciler.Worker()(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected worker shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery worker did not join after cancellation")
	}
}

func newTestReconciler(t *testing.T, owner Owner, store Store) *Reconciler {
	t.Helper()
	result, err := New(owner, store, time.Second, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), NewMetrics())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func recoveryMaterialization(operationRef string, generation int64) kubernetesstore.Materialization {
	return kubernetesstore.Materialization{
		Namespace: "kodex-runtime", Name: "runtime-secret-test-r1", OperationRef: operationRef,
		ClaimGeneration: generation, SecretRef: "sec_test123456", Key: "value", Revision: 1,
		UID: "uid-test", ResourceVersion: "10", ContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func recoveryWork(kind controlplanev1.RuntimeSecretOperationKind) RecoveryWork {
	return RecoveryWork{
		OperationRef: "secop_expired", Kind: kind, ClaimantID: "old-pod-uid", ClaimGeneration: 17,
		Namespace: "kodex-runtime", SecretRef: "sec_test123456", TargetRevision: 1, SecretKey: "value",
		ExpectedContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}
