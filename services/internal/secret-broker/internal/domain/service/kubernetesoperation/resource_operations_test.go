package kubernetesoperation

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// resourcePlanFixture models authoritative planning and records whether observations were repeated.
// resourcePlanFixture моделирует authoritative planning и фиксирует повтор observations.
type resourcePlanFixture struct {
	*resourceOwnerFixture
	plan      targetrepo.OperationPlan
	proposals [][]managedkubernetes.ChildCandidate
	planErr   error
}

// PrepareKubernetesChildren stores one immutable plan and supports metadata-only readback.
// PrepareKubernetesChildren сохраняет immutable plan и поддерживает metadata-only readback.
func (owner *resourcePlanFixture) PrepareKubernetesChildren(_ context.Context, _ managedkubernetes.Binding, _ value.KubernetesOperationSnapshot, candidates []managedkubernetes.ChildCandidate) (targetrepo.OperationPlan, error) {
	owner.proposals = append(owner.proposals, append([]managedkubernetes.ChildCandidate(nil), candidates...))
	owner.snapshot.PlanRef, owner.snapshot.PlanDigest, owner.snapshot.PlanRevision = owner.plan.Ref, owner.plan.Digest, owner.plan.Revision
	return owner.plan, owner.planErr
}

// resourceFactoryFixture represents only private preparation selection, never effect admission.
// resourceFactoryFixture представляет лишь private preparation selection, не effect admission.
type resourceFactoryFixture struct {
	prepared targetrepo.ResourcePreparation
	calls    int
	err      error
}

// AuthorizeKubernetesResourceResult is separate from preparation and does not rediscover the resource.
// AuthorizeKubernetesResourceResult отделён от preparation и не повторяет resource discovery.
func (*resourceFactoryFixture) AuthorizeKubernetesResourceResult(context.Context, value.KubernetesOperationSnapshot, targetrepo.OperationChild) (time.Time, error) {
	return time.Now().Add(4 * time.Second), nil
}

// PrepareKubernetesResource records private access so accepted recovery can prove its absence.
// PrepareKubernetesResource учитывает private access для доказательства его отсутствия при recovery.
func (factory *resourceFactoryFixture) PrepareKubernetesResource(context.Context, value.KubernetesOperationSnapshot) (targetrepo.ResourcePreparation, error) {
	factory.calls++
	return factory.prepared, factory.err
}

// TestResourceOperationsPlanAndGate preserves original observations and closes native material on every path.
// TestResourceOperationsPlanAndGate сохраняет original observations и закрывает native material на каждом пути.
func TestResourceOperationsPlanAndGate(t *testing.T) {
	for _, variant := range []string{"new-plan", "existing-plan", "gate", "lost-plan", "expanded-plan", "changed-input", "changed-factory"} {
		t.Run(variant, func(t *testing.T) {
			_, effectOwner, _, plan, prepared := resourceExecutorFixture(t)
			plan.Children[0].State = "READY"
			owner := &resourcePlanFixture{resourceOwnerFixture: effectOwner, plan: plan}
			factory := &resourceFactoryFixture{prepared: prepared}
			if variant != "existing-plan" {
				owner.snapshot.PlanRef, owner.snapshot.PlanDigest, owner.snapshot.PlanRevision = "", "", 0
			}
			switch variant {
			case "gate":
				owner.admission = targetrepo.EffectAdmission{GateRef: "gate_synthetic"}
			case "lost-plan":
				owner.planErr = errors.New("synthetic lost plan acknowledgement")
			case "expanded-plan":
				owner.plan.Children = append(owner.plan.Children, owner.plan.Children[0])
				owner.plan.Children[1].Ref = "child_second"
			case "changed-input":
				owner.plan.Children[0].NormalizedInput = []byte(`{"invalid":true}`)
			case "changed-factory":
				prepared.pin.SubjectDigest = "different"
			}
			service, err := NewResourceOperations(owner, factory)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Execute(t.Context(), owner.binding)
			wantSuccess := variant == "new-plan" || variant == "existing-plan" || variant == "gate"
			if (err == nil) != wantSuccess || factory.calls != 1 || prepared.closes != 1 {
				t.Fatal("resource orchestration outcome or cleanup mismatch")
			}
			if variant == "changed-factory" {
				if len(owner.proposals) != 0 || owner.begins != 0 {
					t.Fatal("foreign native preparation reached owner plan")
				}
				return
			}
			if len(owner.proposals) != 1 {
				t.Fatal("planning was retried")
			}
			if (len(owner.proposals[0]) == 0) != (variant == "existing-plan") {
				t.Fatal("existing plan rediscovered or new plan omitted observations")
			}
			if variant == "gate" {
				if result.GateRef == "" || prepared.executes != 0 {
					t.Fatal("gate dispatched")
				}
			} else if wantSuccess && (prepared.executes != 1 || result.Result == nil) {
				t.Fatal("resource effect not completed")
			}
			if !wantSuccess && (prepared.executes != 0 || owner.begins != 0) {
				t.Fatal("invalid plan dispatched")
			}
		})
	}
}

// TestResourceOperationsRecoverySkipsPrivateAccess recovers all historical states without target observations.
// TestResourceOperationsRecoverySkipsPrivateAccess восстанавливает historical states без target observations.
func TestResourceOperationsRecoverySkipsPrivateAccess(t *testing.T) {
	for _, state := range []string{"ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			_, effectOwner, _, plan, prepared := resourceExecutorFixture(t)
			plan.Children[0].State = state
			effectOwner.snapshot.ReconcileOnly, effectOwner.admission.ReconcileOnly = true, true
			raw := []byte(`{"accepted":true}`)
			if state == "SUCCEEDED" {
				effectOwner.admission.AcceptedResult = &targetrepo.AcceptedEffectResult{Outcome: "SUCCEEDED", ResultJSON: raw, ResultDigest: resourceResultDigest(raw)}
			}
			owner := &resourcePlanFixture{resourceOwnerFixture: effectOwner, plan: plan}
			factory := &resourceFactoryFixture{prepared: prepared, err: errors.New("private access must not happen")}
			service, err := NewResourceOperations(owner, factory)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Execute(t.Context(), owner.binding)
			if err != nil || !result.ReconcileOnly || factory.calls != 0 || prepared.executes != 0 || owner.completes != 0 || len(owner.proposals) != 1 || len(owner.proposals[0]) != 0 {
				t.Fatal("recovery repeated native access")
			}
			if state == "SUCCEEDED" && (result.Result == nil || !bytes.Equal(raw, result.Result.ResultJSON)) {
				t.Fatal("accepted recovery result lost")
			}
		})
	}
}
