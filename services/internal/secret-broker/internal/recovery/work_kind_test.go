package recovery

import (
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	store "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// TestManagedRecoveryUsesOnlyExplicitImmediateOwnerRoute gives misleading reference prefixes to explicit
// DRAFT/IMMEDIATE effects and requires only the immediate owner route, with no deletion.
// TestManagedRecoveryUsesOnlyExplicitImmediateOwnerRoute задаёт вводящие в заблуждение reference prefixes
// для явных DRAFT/IMMEDIATE effects и требует лишь immediate owner route без удаления.
func TestManagedRecoveryUsesOnlyExplicitImmediateOwnerRoute(t *testing.T) {
	t.Parallel()
	immediate := recoveryMaterialization("sdop_not_a_route", 1)
	draft := recoveryMaterialization("secop_not_a_route", 2)
	draft.WorkKind = store.WorkKindDraft
	owner := &fakeOwner{
		decisions: map[string]cp.RuntimeSecretRecoveryAction{immediate.OperationRef: cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP},
		states:    map[string]cp.RuntimeSecretOperationState{immediate.OperationRef: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED},
	}
	kube := &fakeStore{items: []store.Materialization{draft, immediate}}
	if err := newTestReconciler(t, owner, kube).ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 1 || owner.calls[0] != immediate.OperationRef || len(kube.deleted) != 0 {
		t.Fatalf("materialization source was inferred from a ref or shared with another owner: calls=%v deletes=%v", owner.calls, kube.deleted)
	}
}

// TestManagedRecoveryRejectsUnknownSourceWithoutEffects supplies empty/unknown/noncanonical work kinds and
// requires an error before any owner recovery call or deletion.
// TestManagedRecoveryRejectsUnknownSourceWithoutEffects передаёт empty/unknown/noncanonical work kinds и
// требует ошибки до owner recovery call и удаления.
func TestManagedRecoveryRejectsUnknownSourceWithoutEffects(t *testing.T) {
	t.Parallel()
	for _, kind := range []store.MaterializationWorkKind{"", "UNKNOWN", "PUBLISH", "CREATE"} {
		t.Run(string(kind), func(t *testing.T) {
			item := recoveryMaterialization("sdop_unknown_source", 1)
			item.WorkKind = kind
			owner := &fakeOwner{}
			kube := &fakeStore{items: []store.Materialization{item}}
			if err := newTestReconciler(t, owner, kube).ReconcileOnce(t.Context()); err == nil {
				t.Fatal("unknown owner source was accepted")
			}
			if len(owner.calls) != 0 || len(kube.deleted) != 0 {
				t.Fatal("unknown owner source caused a recovery or delete effect")
			}
		})
	}
}

// TestImmediateMissingOwnerCannotBecomeDraftOrDelete repeats a synthetic missing-owner error twice and
// requires the same immediate route each time without fallback to draft recovery or deletion.
// TestImmediateMissingOwnerCannotBecomeDraftOrDelete дважды повторяет synthetic missing-owner error и
// требует прежнего immediate route без fallback к draft recovery и удалению.
func TestImmediateMissingOwnerCannotBecomeDraftOrDelete(t *testing.T) {
	t.Parallel()
	item := recoveryMaterialization("sdop_missing_owner", 1)
	owner := &fakeOwner{err: errors.New("synthetic owner not found")}
	kube := &fakeStore{items: []store.Materialization{item}}
	reconciler := newTestReconciler(t, owner, kube)
	for range 2 {
		if err := reconciler.ReconcileOnce(t.Context()); err == nil {
			t.Fatal("missing authoritative owner did not fail closed")
		}
	}
	if len(owner.calls) != 2 || len(kube.deleted) != 0 {
		t.Fatalf("missing owner caused another route or delete: calls=%v deletes=%v", owner.calls, kube.deleted)
	}
}
