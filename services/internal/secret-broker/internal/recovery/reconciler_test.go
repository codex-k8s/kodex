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

// fakeOwner records recovery, expired-claim and projection decisions under a mutex with injectable errors.
// It substitutes authoritative storage, so its recorded calls do not prove real owner transactions.
// fakeOwner фиксирует recovery, expired-claim и projection decisions под mutex с подставляемыми ошибками.
// Она заменяет authoritative storage; записанные calls не доказывают реальные owner transactions.
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

// failedClaim captures the exact original claim coordinates and closed failure code requested by recovery.
// failedClaim сохраняет точные исходные claim coordinates и закрытый failure code запроса recovery.
type failedClaim struct {
	operationRef    string
	claimantID      string
	claimGeneration int64
	failureCode     controlplanev1.RuntimeSecretFailureCode
}

// Recover records the operation ref, then returns configured action/state or an injected failure;
// the materialization argument is intentionally not verified by this simple owner stub.
// Recover записывает operation ref, затем возвращает настроенные action/state либо failure;
// materialization argument намеренно не проверяется этим простым owner stub.
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

// ListRecoveryWork returns a copy of the outer work slice plus the injected error, not a deep snapshot proof.
// ListRecoveryWork возвращает копию внешнего work slice с подставленной ошибкой, не deep snapshot proof.
func (owner *fakeOwner) ListRecoveryWork(context.Context) ([]RecoveryWork, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return append([]RecoveryWork(nil), owner.work...), owner.workError
}

// FailExpiredClaim records ref/claimant/generation/code even when reporting fails, allowing retry
// observations without pretending the failure receipt committed in a real owner.
// FailExpiredClaim записывает ref/claimant/generation/code даже при reporting failure, позволяя
// наблюдать retry без утверждения о commit failure receipt настоящим owner.
func (owner *fakeOwner) FailExpiredClaim(_ context.Context, operationRef, claimantID string, generation int64, code controlplanev1.RuntimeSecretFailureCode) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.failed = append(owner.failed, failedClaim{
		operationRef: operationRef, claimantID: claimantID, claimGeneration: generation, failureCode: code,
	})
	return owner.failError
}

// ValidateRuntimeCredentialProjection records the lease selector and returns configured eligibility;
// missing selectors default false, without a real authoritative lookup.
// ValidateRuntimeCredentialProjection фиксирует lease selector и возвращает заданную eligibility;
// отсутствующий selector даёт false без настоящего authoritative lookup.
func (owner *fakeOwner) ValidateRuntimeCredentialProjection(_ context.Context, request *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.projectionCalls = append(owner.projectionCalls, request.GetLeaseRef())
	return owner.projectionValidity[request.GetLeaseRef()], owner.projectionError
}

// fakeStore supplies independent managed-effect/projection snapshots and records attempted deletions.
// Configured errors can accompany observations; no Kubernetes transport or ownership check is emulated.
// fakeStore предоставляет отдельные managed-effect/projection snapshots и фиксирует попытки удаления.
// Настроенные ошибки могут сопровождать observations; Kubernetes transport и ownership checks не имитируются.
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

// LookupExpectedEffect returns the injected exact-effect observation/error without deriving it from input.
// LookupExpectedEffect возвращает подставленные exact-effect observation/error без вычисления из input.
func (store *fakeStore) LookupExpectedEffect(context.Context, kubernetesstore.MaterializationEffect) (kubernetesstore.Materialization, error) {
	return store.lookup, store.lookupError
}

// ListManaged copies the outer metadata slice so recovery cannot change its membership by append.
// ListManaged копирует внешний metadata slice, не позволяя recovery менять его состав через append.
func (store *fakeStore) ListManaged(context.Context) ([]kubernetesstore.Materialization, error) {
	return append([]kubernetesstore.Materialization(nil), store.items...), store.listError
}

// ReadbackExact echoes the requested metadata with an injected error instead of reading an object.
// ReadbackExact возвращает запрошенные metadata с подставленной ошибкой вместо чтения объекта.
func (store *fakeStore) ReadbackExact(_ context.Context, materialization kubernetesstore.Materialization) (kubernetesstore.Materialization, error) {
	return materialization, store.readError
}

// DeleteExact records every operation-ref attempt before returning the injected failure, including retries.
// DeleteExact записывает каждую operation-ref попытку до возврата подставленного отказа, включая retries.
func (store *fakeStore) DeleteExact(_ context.Context, materialization kubernetesstore.Materialization) error {
	store.deleted = append(store.deleted, materialization.OperationRef)
	return store.deleteError
}

// ListRuntimeCredentialProjections returns an outer copy of the configured projection metadata.
// ListRuntimeCredentialProjections возвращает внешнюю копию заданных projection metadata.
func (store *fakeStore) ListRuntimeCredentialProjections(context.Context) ([]kubernetesstore.CredentialProjection, error) {
	return append([]kubernetesstore.CredentialProjection(nil), store.projections...), nil
}

// DeleteRuntimeCredentialProjection records the exact requested name even when its injected delete fails.
// DeleteRuntimeCredentialProjection записывает точное запрошенное имя даже при подставленном delete failure.
func (store *fakeStore) DeleteRuntimeCredentialProjection(_ context.Context, projection kubernetesstore.CredentialProjection) error {
	store.deletedProjections = append(store.deletedProjections, projection.SecretName)
	return store.projectionDeleteError
}

// TestCredentialProjectionRecoveryDeletesRevokedOrChangedBinding retains the owner-approved lease projection
// and deletes the rejected one after checking both bindings through the fake owner.
// TestCredentialProjectionRecoveryDeletesRevokedOrChangedBinding сохраняет owner-approved lease projection
// и удаляет отклонённую после проверки обеих bindings через fake owner.
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

// TestRecoveryCompletesOrphanAndDeletesRejectedMaterialization checks distinct KEEP/DELETE decisions,
// exact cleanup selection and readiness after a successful complete snapshot scan.
// TestRecoveryCompletesOrphanAndDeletesRejectedMaterialization проверяет разные KEEP/DELETE decisions,
// точный выбор очистки и readiness после успешного полного snapshot scan.
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

// TestFailedDeleteIsRecoveredByNextScan keeps recovery unhealthy after deletion failure and requires
// the next scan to retry the same observed effect once the injected outage clears.
// TestFailedDeleteIsRecoveredByNextScan сохраняет unhealthy recovery после deletion failure и требует
// повторить тот же observed effect следующим scan после снятия подставленного outage.
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

// TestRecoveryRejectsIncompleteOwnerDecision rejects a KEEP action without a corresponding operation state.
// TestRecoveryRejectsIncompleteOwnerDecision отвергает KEEP action без соответствующего operation state.
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

// TestExpiredCreateWithoutMaterializationFailsOriginalFence reports an absent effect as reconciliation
// failure using the old claimant/generation, not the current worker identity.
// TestExpiredCreateWithoutMaterializationFailsOriginalFence сообщает отсутствие эффекта как reconciliation
// failure с прежними claimant/generation, не identity текущего worker.
func TestExpiredCreateWithoutMaterializationFailsOriginalFence(t *testing.T) {
	t.Parallel()
	reconciler, owner, _ := expiredRecoveryFixture(t, controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, kubernetesstore.ErrMaterializationNotFound)
	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owner.failed) != 1 || owner.failed[0].claimantID != "old-pod-uid" || owner.failed[0].claimGeneration != 17 ||
		owner.failed[0].failureCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED {
		t.Fatalf("expired claim завершена не исходным fence: %#v", owner.failed)
	}
}

// TestExpiredCreateWithExactEffectFallsThroughToManagedRecovery routes a matching observed effect to
// normal owner recovery rather than failing the expired claim without considering its materialization.
// TestExpiredCreateWithExactEffectFallsThroughToManagedRecovery передаёт совпавший observed effect в
// обычный owner recovery вместо failure просроченного claim без учёта materialization.
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

// TestExpiredCreateConflictFailsClaimAndKeepsObject retains an object on exact-effect conflict,
// reports the closed conflict code and keeps reconciliation unhealthy rather than deleting uncertain data.
// TestExpiredCreateConflictFailsClaimAndKeepsObject сохраняет объект при exact-effect conflict,
// сообщает закрытый conflict code и оставляет reconciliation unhealthy без удаления неопределённых данных.
func TestExpiredCreateConflictFailsClaimAndKeepsObject(t *testing.T) {
	t.Parallel()
	reconciler, owner, store := expiredRecoveryFixture(t, controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE, kubernetesstore.ErrMaterializationConflict)
	if err := reconciler.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("materialization conflict должен оставить recovery unhealthy до ручной проверки")
	}
	if len(owner.failed) != 1 || owner.failed[0].failureCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT || len(store.deleted) != 0 {
		t.Fatalf("conflict обработан небезопасно: failed=%#v deleted=%#v", owner.failed, store.deleted)
	}
}

// TestExpiredRevealAndRevokeFailWithoutMaterialization checks both no-create kinds receive the
// closed reconciliation failure instead of inventing a materialization effect.
// TestExpiredRevealAndRevokeFailWithoutMaterialization проверяет закрытый reconciliation failure
// для обоих no-create kinds вместо выдуманного materialization effect.
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

// TestWorkerStopsOnLifecycleCancellation cancels the synthetic worker context and observes a joined
// context.Canceled result within one second; it is not a live workload termination test.
// TestWorkerStopsOnLifecycleCancellation отменяет synthetic worker context и наблюдает joined
// context.Canceled результат за секунду; это не проверка остановки живого workload.
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

// expiredRecoveryFixture wires one synthetic expired work item and the caller-selected lookup
// failure; it never runs reconciliation, so success/failure expectations remain in each test.
// expiredRecoveryFixture связывает synthetic expired work item с выбранной caller lookup failure;
// reconciliation не запускается, ожидаемые success/failure остаются в каждом тесте.
func expiredRecoveryFixture(t *testing.T, kind controlplanev1.RuntimeSecretOperationKind, lookupError error) (*Reconciler, *fakeOwner, *fakeStore) {
	t.Helper()
	owner := &fakeOwner{work: []RecoveryWork{recoveryWork(kind)}}
	store := &fakeStore{lookupError: lookupError}
	reconciler := newTestReconciler(t, owner, store)
	if err := reconciler.EnableExpiredClaimRecovery(owner, store, "kodex-runtime"); err != nil {
		t.Fatal(err)
	}
	return reconciler, owner, store
}

// newTestReconciler wires fresh metrics, a discarded logger and one-second scan budgets around
// caller-selected fake ports, without starting its worker.
// newTestReconciler связывает отдельные metrics, discarded logger и секундные scan budgets
// с выбранными fake ports без запуска worker.
func newTestReconciler(t *testing.T, owner Owner, store Store) *Reconciler {
	t.Helper()
	result, err := New(owner, store, time.Second, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), NewMetrics())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// recoveryMaterialization creates deterministic immediate-effect metadata with fixed synthetic object pins.
// recoveryMaterialization создаёт детерминированные immediate-effect metadata с фиксированными synthetic object pins.
func recoveryMaterialization(operationRef string, generation int64) kubernetesstore.Materialization {
	return kubernetesstore.Materialization{
		WorkKind:  kubernetesstore.WorkKindImmediate,
		Namespace: "kodex-runtime", Name: "runtime-secret-test-r1", OperationRef: operationRef,
		ClaimGeneration: generation, SecretRef: "sec_test123456", Key: "value", Revision: 1,
		UID: "uid-test", ResourceVersion: "10", ContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

// recoveryWork builds one expired-work fixture with original claimant/generation and a supplied kind;
// its fixed metadata is not a runtime credential or a grant.
// recoveryWork строит expired-work fixture с исходными claimant/generation и переданным kind;
// фиксированные metadata не являются runtime credential или grant.
func recoveryWork(kind controlplanev1.RuntimeSecretOperationKind) RecoveryWork {
	return RecoveryWork{
		OperationRef: "secop_expired", Kind: kind, ClaimantID: "old-pod-uid", ClaimGeneration: 17,
		Namespace: "kodex-runtime", SecretRef: "sec_test123456", TargetRevision: 1, SecretKey: "value",
		ExpectedContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}
