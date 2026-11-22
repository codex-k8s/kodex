package kubernetesoperation

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// TestMetadataOperationsPreserveOwnerLifecycle proves addressless reads still require one owner admission, joined completion and receipt; malformed object evidence never reaches admission.
// TestMetadataOperationsPreserveOwnerLifecycle доказывает, что чтение без адреса проходит owner admission, join, completion и receipt; постороннее object evidence не достигает admission.
func TestMetadataOperationsPreserveOwnerLifecycle(t *testing.T) {
	for _, operation := range []string{"kubernetes.version", "kubernetes.identity", "kubernetes.help", "kubernetes.config.metadata"} {
		for _, variant := range []string{"new", "existing", "recovery", "snapshot-resource", "child-resource", "child-observation", "preparation-observation", "foreign-subject"} {
			t.Run(operation+"/"+variant, func(t *testing.T) {
				_, effectOwner, _, plan, prepared := resourceExecutorFixture(t)
				input, canonical, err := managedkubernetes.DecodeOperationInput(operation, []byte(`{}`))
				if err != nil {
					t.Fatal(err)
				}
				originalResource := *effectOwner.snapshot.Resource
				originalObservation := prepared.pin.Observation
				effectOwner.snapshot.Operation = operation
				effectOwner.snapshot.Input, effectOwner.snapshot.NormalizedInput = input, canonical
				effectOwner.snapshot.Resource, effectOwner.snapshot.Discovery = nil, nil
				child := &plan.Children[0]
				child.Operation, child.State = operation, "READY"
				child.Input, child.NormalizedInput, child.InputDigest = input, canonical, resourceResultDigest(canonical)
				child.Resource, child.Observation, child.NodeTransport = managedkubernetes.ResourceAddress{}, nil, nil
				prepared.pin.Operation, prepared.pin.InputDigest = operation, child.InputDigest
				prepared.pin.Observation = managedkubernetes.ResourceObservation{}
				effectOwner.admission.ChildInputDigest = child.InputDigest
				if variant != "existing" && variant != "recovery" {
					effectOwner.snapshot.PlanRef, effectOwner.snapshot.PlanDigest, effectOwner.snapshot.PlanRevision = "", "", 0
				}
				switch variant {
				case "recovery":
					child.State = "SUCCEEDED"
					effectOwner.snapshot.ReconcileOnly, effectOwner.admission.ReconcileOnly = true, true
					raw := []byte(`{"accepted":true}`)
					effectOwner.admission.AcceptedResult = &targetrepo.AcceptedEffectResult{Outcome: "SUCCEEDED", ResultJSON: raw, ResultDigest: resourceResultDigest(raw)}
				case "snapshot-resource":
					effectOwner.snapshot.Resource = &originalResource
				case "child-resource":
					child.Resource = originalResource
				case "child-observation":
					child.Observation = &managedkubernetes.ChildCandidate{ObservationState: originalObservation.State, Resource: originalResource, ObservedResourceDigest: originalObservation.ObservedResourceDigest, RegisteredSchemaDigest: originalObservation.RegisteredSchemaDigest}
				case "preparation-observation":
					prepared.pin.Observation = originalObservation
				case "foreign-subject":
					prepared.pin.SubjectDigest = "foreign-subject"
				}
				owner := &resourcePlanFixture{resourceOwnerFixture: effectOwner, plan: plan}
				factory := &resourceFactoryFixture{prepared: prepared}
				service, err := NewResourceOperations(owner, factory)
				if err != nil {
					t.Fatal(err)
				}
				result, err := service.Execute(t.Context(), owner.binding)
				for _, proposal := range owner.proposals {
					if len(proposal) != 0 {
						t.Fatal("metadata invented resource observation")
					}
				}
				switch variant {
				case "new", "existing":
					if err != nil || result.Result == nil || result.Result.Outcome != "SUCCEEDED" || owner.begins != 1 || owner.completes != 1 || prepared.executes != 1 || prepared.closes != 1 || factory.calls != 1 {
						t.Fatal("metadata did not use joined owner effect lifecycle", err)
					}
				case "recovery":
					if err != nil || !result.ReconcileOnly || result.Result == nil || factory.calls != 0 || prepared.executes != 0 || prepared.closes != 0 || owner.begins != 1 || owner.completes != 0 {
						t.Fatal("metadata recovery replayed target request", err)
					}
				default:
					if err == nil || owner.begins != 0 || owner.completes != 0 || prepared.executes != 0 || prepared.closes != factory.calls {
						t.Fatal("invalid metadata evidence reached admission or leaked preparation")
					}
				}
			})
		}
	}
}
