package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesoperation"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// nativeResourceOwner models only the owner port; the resource transport and preparation remain real.
// nativeResourceOwner моделирует только owner port; resource transport и preparation остаются настоящими.
type nativeResourceOwner struct {
	snapshot       value.KubernetesOperationSnapshot
	prepared       *PreparedResourceOperation
	accepted       *targetrepo.AcceptedEffectResult
	opening        targetrepo.EffectOpening
	beforeBegin    func()
	lostCompletion bool
	completes      int
}

// AuthorizeKubernetesResourceResult forbids read-through-write in this mutation-only fixture.
// AuthorizeKubernetesResourceResult запрещает read-through-write в mutation-only fixture.
func (*nativeResourceOwner) AuthorizeKubernetesResourceResult(context.Context, value.KubernetesOperationSnapshot, targetrepo.OperationChild) (time.Time, error) {
	return time.Time{}, errors.New("unexpected read admission")
}

// ResolveKubernetesOperation returns immutable synthetic metadata with fresh eligibility.
// ResolveKubernetesOperation возвращает immutable synthetic metadata со fresh eligibility.
func (owner *nativeResourceOwner) ResolveKubernetesOperation(context.Context, managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	return owner.snapshot, nil
}

// BeginKubernetesEffect admits one native request or reads the exact already accepted result.
// BeginKubernetesEffect допускает один native request либо читает exact already accepted result.
func (owner *nativeResourceOwner) BeginKubernetesEffect(_ context.Context, _ managedkubernetes.Binding, opening targetrepo.EffectOpening) (targetrepo.EffectAdmission, error) {
	owner.opening = opening
	if owner.beforeBegin != nil {
		owner.beforeBegin()
	}
	return targetrepo.EffectAdmission{EffectRef: "effect_synthetic", EffectGeneration: 1, ChildInputDigest: opening.ChildInputDigest, ExpiresAt: time.Now().Add(10 * time.Second), ReconcileOnly: owner.snapshot.ReconcileOnly, AcceptedResult: owner.accepted}, nil
}

// CompleteKubernetesEffect stores exact safe bytes after native close, optionally losing only the ACK.
// CompleteKubernetesEffect сохраняет exact safe bytes после native close, при необходимости теряя только ACK.
func (owner *nativeResourceOwner) CompleteKubernetesEffect(_ context.Context, _ managedkubernetes.Binding, completion targetrepo.EffectCompletion) (targetrepo.EffectReceipt, error) {
	owner.completes++
	owner.prepared.mutex.Lock()
	closed, done := owner.prepared.closed, owner.prepared.done
	owner.prepared.mutex.Unlock()
	if !closed || done == nil {
		return targetrepo.EffectReceipt{}, errors.New("native preparation not closed")
	}
	select {
	case <-done:
	default:
		return targetrepo.EffectReceipt{}, errors.New("native request not joined")
	}
	sum := sha256.Sum256(completion.ResultJSON)
	digest := hex.EncodeToString(sum[:])
	owner.accepted = &targetrepo.AcceptedEffectResult{ResultJSON: bytes.Clone(completion.ResultJSON), ResultDigest: digest, Outcome: completion.Outcome, ReasonCode: completion.ReasonCode}
	if owner.lostCompletion {
		return targetrepo.EffectReceipt{}, errors.New("synthetic lost acknowledgement")
	}
	return targetrepo.EffectReceipt{State: completion.Outcome, ResultDigest: digest, SubmissionDigest: digest, Applied: true, AuthorizedUntil: time.Now().Add(10 * time.Second)}, nil
}

// TestResourceExecutorNativeTLS proves real one-shot writes, current RBAC and recovery without target reads.
// This is local broker/native qualification; the owner database and live cluster are separate contours.
// TestResourceExecutorNativeTLS доказывает real one-shot writes, current RBAC и recovery без target reads.
// Это local broker/native qualification; owner database и live cluster являются отдельными контурами.
func TestResourceExecutorNativeTLS(t *testing.T) {
	for _, variant := range []string{"success", "lost-completion", "revoked-before-dispatch", "read-cache", "read-revoked"} {
		t.Run(variant, func(t *testing.T) {
			wire := &resourceWireFixture{}
			credential := resourceTLSFixture(t, wire)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Body: json.RawMessage(resourceWireObject("10"))}
			operation, risk := "kubernetes.replace", "WRITE"
			read := strings.HasPrefix(variant, "read-")
			if read {
				operation, risk, input.Body = "kubernetes.get", "READ", nil
			}
			raw, err := managedkubernetes.EncodeOperationInput(operation, input)
			if err != nil {
				t.Fatal(err)
			}
			_, canonical, err := managedkubernetes.DecodeOperationInput(operation, raw)
			if err != nil {
				t.Fatal(err)
			}
			address := ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "example", Name: "selected", UID: input.UID, ResourceVersion: input.ResourceVersion}
			prepared, err := credential.PrepareResourceOperation(t.Context(), operation, input, address, subjectPin(t), []string{"private"})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			pin, valid := prepared.Binding()
			if !valid {
				t.Fatal("native binding missing")
			}
			sum := sha256.Sum256(canonical)
			if pin.InputDigest != hex.EncodeToString(sum[:]) || pin.TargetDigest != credential.target.MetadataDigest() || pin.Observation != prepared.Observation() {
				t.Fatal("native binding lost exact immutable input")
			}
			identityRef := "identity_synthetic"
			name, err := managedkubernetes.IdentitySecretName(identityRef, 1)
			if err != nil {
				t.Fatal(err)
			}
			identity := value.KubernetesIdentityLocator{IdentityRef: identityRef, TargetRef: credential.target.ID, Namespace: "kodex-kubernetes-identities", Name: name, UID: "24b97594-dda7-4f98-84c2-ce32bf76d739", ResourceVersion: "12", ContentSHA256: strings.Repeat("e", 64), Revision: 1}
			snapshot := value.KubernetesOperationSnapshot{Digest: strings.Repeat("a", 64), Operation: operation, Risk: risk, ApprovalPolicy: "HUMAN_EACH_EFFECT", IdentityRef: identityRef, IdentityGeneration: 1, Identity: identity, SubjectDigest: subjectPin(t), Target: credential.target, Input: input, NormalizedInput: canonical, Resource: &address, ExpiresAt: time.Now().Add(time.Minute), PlanRef: "plan_synthetic", PlanDigest: strings.Repeat("b", 64), PlanRevision: 1, ProtectedNamespaces: []string{"private"}}
			plan := targetrepo.OperationPlan{Ref: snapshot.PlanRef, Digest: snapshot.PlanDigest, Revision: snapshot.PlanRevision, Children: []targetrepo.OperationChild{{Ref: "child_synthetic", Operation: snapshot.Operation, InputDigest: pin.InputDigest, Resource: pin.Observation.Resource, Input: input, NormalizedInput: canonical}}}
			plan.Children[0].Observation = &managedkubernetes.ChildCandidate{ObservationState: pin.Observation.State, Resource: pin.Observation.Resource, ObservedResourceDigest: pin.Observation.ObservedResourceDigest, RegisteredSchemaDigest: pin.Observation.RegisteredSchemaDigest, NamespaceUID: pin.Observation.NamespaceUID}
			binding := managedkubernetes.Binding{InvocationRef: "inv_synthetic", WorkloadInstance: "worker_synthetic", LeaseRef: "lease_synthetic", Fence: "fence_synthetic", Generation: 1, InputDigest: strings.Repeat("a", 64)}
			owner := &nativeResourceOwner{snapshot: snapshot, prepared: prepared, lostCompletion: variant == "lost-completion"}
			var access targetrepo.ResourceResultAccess = owner
			if read {
				reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
				t.Cleanup(func() { clear(reader.raw) })
				snapshot.Identity = identityLocatorFixture(t, reader.raw, credential.target)
				snapshot.IdentityRef = snapshot.Identity.IdentityRef
				owner.snapshot = snapshot
				access, err = NewResourcePreparer(reader, snapshot.Identity.Namespace)
				if err != nil {
					t.Fatal(err)
				}
			}
			if variant == "revoked-before-dispatch" {
				owner.beforeBegin = func() { wire.deny.Store(true) }
			}
			service, err := kubernetesoperation.NewResourceExecutor(owner, access)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Execute(t.Context(), binding, snapshot, plan, "child_synthetic", prepared)
			if (err != nil) != owner.lostCompletion || owner.completes != 1 || owner.accepted == nil {
				t.Fatal("native completion was not recorded once")
			}
			expectedWrites, expectedOutcome := int64(1), "SUCCEEDED"
			if read {
				expectedWrites = 0
			}
			if variant == "revoked-before-dispatch" {
				expectedWrites, expectedOutcome = 0, "FAILED"
			}
			if wire.effects.Load() != expectedWrites || owner.accepted.Outcome != expectedOutcome || owner.lostCompletion && result.Result != nil {
				t.Fatal("wire count or owner outcome mismatch")
			}
			owner.snapshot.ReconcileOnly = true
			if variant == "read-revoked" {
				wire.deny.Store(true)
			}
			reads := wire.reads.Load()
			recovered, err := service.Execute(t.Context(), binding, snapshot, plan, "child_synthetic", nil)
			if variant == "read-revoked" {
				if err == nil || recovered.Result != nil || wire.effects.Load() != expectedWrites || wire.reads.Load() != reads || owner.completes != 1 {
					t.Fatal("revoked READ exposed cache or replayed resource access")
				}
			} else if err != nil || recovered.Result == nil || !bytes.Equal(recovered.Result.ResultJSON, owner.accepted.ResultJSON) || wire.effects.Load() != expectedWrites || wire.reads.Load() != reads || owner.completes != 1 {
				t.Fatal("recovery redispatched or rediscovered native target")
			}
		})
	}
}
