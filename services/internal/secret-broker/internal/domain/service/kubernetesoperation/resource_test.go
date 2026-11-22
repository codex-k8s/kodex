package kubernetesoperation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// resourceOwnerFixture records exact admission/completion ordering without granting provider authority.
// resourceOwnerFixture фиксирует exact admission/completion ordering без provider authority.
type resourceOwnerFixture struct {
	accessCalls                 int
	accessErr                   error
	snapshot                    value.KubernetesOperationSnapshot
	admission                   targetrepo.EffectAdmission
	opening                     targetrepo.EffectOpening
	completion                  targetrepo.EffectCompletion
	binding                     managedkubernetes.Binding
	prepared                    *resourcePreparationFixture
	resolves, begins, completes int
	beginErr, completeErr       error
	receipt                     func(targetrepo.EffectReceipt) targetrepo.EffectReceipt
}

// AuthorizeKubernetesResourceResult models fresh target READ eligibility independently of owner receipts.
// AuthorizeKubernetesResourceResult моделирует fresh target READ eligibility независимо от owner receipts.
func (owner *resourceOwnerFixture) AuthorizeKubernetesResourceResult(context.Context, value.KubernetesOperationSnapshot, targetrepo.OperationChild) (time.Time, error) {
	owner.accessCalls++
	return time.Now().Add(4 * time.Second), owner.accessErr
}

// ResolveKubernetesOperation returns the current original graph and rejects another binding.
// ResolveKubernetesOperation возвращает current original graph и отвергает другой binding.
func (owner *resourceOwnerFixture) ResolveKubernetesOperation(_ context.Context, binding managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	owner.resolves++
	if binding != owner.binding {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	return owner.snapshot, nil
}

// BeginKubernetesEffect records one exact proposal, including uncertain admission responses.
// BeginKubernetesEffect фиксирует один exact proposal, включая uncertain admission responses.
func (owner *resourceOwnerFixture) BeginKubernetesEffect(_ context.Context, _ managedkubernetes.Binding, opening targetrepo.EffectOpening) (targetrepo.EffectAdmission, error) {
	owner.begins++
	owner.opening = opening
	return owner.admission, owner.beginErr
}

// CompleteKubernetesEffect accepts only after positive native close and under independent cleanup context.
// CompleteKubernetesEffect принимает только после positive native close под independent cleanup context.
func (owner *resourceOwnerFixture) CompleteKubernetesEffect(ctx context.Context, _ managedkubernetes.Binding, completion targetrepo.EffectCompletion) (targetrepo.EffectReceipt, error) {
	owner.completes++
	if ctx.Err() != nil || owner.prepared.closes != 1 {
		return targetrepo.EffectReceipt{}, targetrepo.ErrOperationUnavailable
	}
	owner.completion = completion
	owner.completion.ResultJSON = bytes.Clone(completion.ResultJSON)
	digest := resourceResultDigest(completion.ResultJSON)
	receipt := targetrepo.EffectReceipt{State: completion.Outcome, ResultDigest: digest, SubmissionDigest: digest, Applied: true, AuthorizedUntil: time.Now().Add(10 * time.Second)}
	if owner.receipt != nil {
		receipt = owner.receipt(receipt)
	}
	return receipt, owner.completeErr
}

// resourcePreparationFixture models a one-shot native request and explicit close ordering.
// resourcePreparationFixture моделирует one-shot native request и explicit close ordering.
type resourcePreparationFixture struct {
	pin              targetrepo.ResourcePreparationBinding
	result           value.ResourceOperationResult
	err              error
	executes, closes int
	execute          func()
	close            func()
}

// Binding refuses reuse after close and returns immutable preparation metadata.
// Binding отвергает reuse после close и возвращает immutable preparation metadata.
func (prepared *resourcePreparationFixture) Binding() (targetrepo.ResourcePreparationBinding, bool) {
	return prepared.pin, prepared.closes == 0
}

// Execute records a single native attempt and optionally simulates caller cancellation.
// Execute фиксирует native attempt и при необходимости моделирует caller cancellation.
func (prepared *resourcePreparationFixture) Execute(context.Context, time.Time) (value.ResourceOperationResult, error) {
	prepared.executes++
	if prepared.execute != nil {
		prepared.execute()
	}
	return prepared.result, prepared.err
}

// Close represents the positive native join required before owner completion.
// Close представляет positive native join перед owner completion.
func (prepared *resourcePreparationFixture) Close() {
	prepared.closes++
	if prepared.close != nil {
		prepared.close()
	}
}

// resourceExecutorFixture builds a synthetic exact resource plan with fresh private owner metadata.
// resourceExecutorFixture строит synthetic exact resource plan со fresh private owner metadata.
func resourceExecutorFixture(t *testing.T) (*ResourceExecutor, *resourceOwnerFixture, value.KubernetesOperationSnapshot, targetrepo.OperationPlan, *resourcePreparationFixture) {
	t.Helper()
	_, streamOwner, _, _, _ := currentAccessFixture(t)
	snapshot := streamOwner.snapshot
	input, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.get", []byte(`{"group":"","version":"v1","resource":"configmaps","namespace":"synthetic","name":"example"}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Operation, snapshot.Risk, snapshot.ApprovalPolicy = "kubernetes.get", "READ", "NONE"
	snapshot.Input, snapshot.NormalizedInput = input, canonical
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "synthetic", Name: "example", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17"}
	snapshot.Resource = &address
	child := targetrepo.OperationChild{Ref: "child_synthetic", Operation: snapshot.Operation, InputDigest: resourceResultDigest(canonical), Resource: address, Input: input, NormalizedInput: canonical}
	plan := targetrepo.OperationPlan{Ref: snapshot.PlanRef, Digest: snapshot.PlanDigest, Revision: snapshot.PlanRevision, Children: []targetrepo.OperationChild{child}}
	prepared := &resourcePreparationFixture{pin: targetrepo.ResourcePreparationBinding{Operation: child.Operation, InputDigest: child.InputDigest, TargetDigest: snapshot.Target.MetadataDigest(), SubjectDigest: snapshot.SubjectDigest,
		Observation: managedkubernetes.ResourceObservation{State: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("1", 64), RegisteredSchemaDigest: strings.Repeat("2", 64)}}, result: value.ResourceOperationResult{Outcome: "SUCCEEDED", SafeJSON: []byte(`{"items":["example"]}`), Attempted: true}}
	observation := prepared.pin.Observation
	plan.Children[0].Observation = &managedkubernetes.ChildCandidate{ObservationState: observation.State, Resource: observation.Resource, ObservedResourceDigest: observation.ObservedResourceDigest, RegisteredSchemaDigest: observation.RegisteredSchemaDigest}
	plan.Digest, err = managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{*plan.Children[0].Observation})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.PlanDigest = plan.Digest
	owner := &resourceOwnerFixture{snapshot: snapshot, binding: streamOwner.binding, prepared: prepared, admission: targetrepo.EffectAdmission{EffectRef: "effect_synthetic", EffectGeneration: 1, ChildInputDigest: child.InputDigest, ExpiresAt: time.Now().Add(10 * time.Second)}}
	service, err := NewResourceExecutor(owner, owner)
	if err != nil {
		t.Fatal(err)
	}
	return service, owner, snapshot, plan, prepared
}

// TestResourceExecutorRejectsChangedPlanObservation prevents fresh preparations from replacing approved evidence.
// TestResourceExecutorRejectsChangedPlanObservation не даёт fresh preparations заменить approved evidence.
func TestResourceExecutorRejectsChangedPlanObservation(t *testing.T) {
	for _, variant := range []string{"schema", "body", "state", "namespace", "missing"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
			switch variant {
			case "schema":
				prepared.pin.Observation.RegisteredSchemaDigest = strings.Repeat("9", 64)
			case "body":
				prepared.pin.Observation.ObservedResourceDigest = strings.Repeat("9", 64)
			case "state":
				prepared.pin.Observation.State = managedkubernetes.ObservationAbsent
			case "namespace":
				plan.Children[0].Observation.NamespaceUID = "11111111-1111-4111-8111-111111111111"
				prepared.pin.Observation.NamespaceUID = "22222222-2222-4222-8222-222222222222"
			case "missing":
				plan.Children[0].Observation = nil
			}
			result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
			if err == nil || result.Result != nil || owner.begins != 0 || prepared.executes != 0 || prepared.closes != 1 {
				t.Fatal("changed approved observation reached Begin or native request")
			}
		})
	}
}

// TestResourceExecutorRetainsOptionalNamespaceWitness does not turn absence of a new witness into mandatory GET.
// TestResourceExecutorRetainsOptionalNamespaceWitness не превращает отсутствие нового witness в mandatory GET.
func TestResourceExecutorRetainsOptionalNamespaceWitness(t *testing.T) {
	service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
	plan.Children[0].Observation.NamespaceUID = "11111111-1111-4111-8111-111111111111"
	result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
	if err != nil || result.Result == nil || plan.Children[0].Observation.NamespaceUID == "" {
		t.Fatal("optional witness lost or made mandatory")
	}
}

// TestResourceExecutorRejectsStreamHistory prevents a stream-only variant from supplying resource dispatch credit.
// TestResourceExecutorRejectsStreamHistory не позволяет stream-only variant выдать resource dispatch credit.
func TestResourceExecutorRejectsStreamHistory(t *testing.T) {
	service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
	owner.admission.History = &targetrepo.StreamHistory{Ref: "stream_synthetic"}
	result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
	if err == nil || result.Result != nil || prepared.executes != 0 || owner.completes != 0 || prepared.closes != 1 {
		t.Fatal("stream history supplied resource dispatch credit")
	}
}

// TestResourceExecutorFreshResult requires fresh owner graph, exact child, native join and output credit.
// TestResourceExecutorFreshResult требует fresh owner graph, exact child, native join и output credit.
func TestResourceExecutorFreshResult(t *testing.T) {
	service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
	result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
	if err != nil || result.Result == nil || string(result.Result.ResultJSON) != `{"items":["example"]}` || owner.resolves != 1 || owner.begins != 1 || owner.completes != 1 || prepared.executes != 1 || prepared.closes != 1 || owner.opening.ChildInputDigest != plan.Children[0].InputDigest || !result.AuthorizedUntil.After(time.Now()) {
		t.Fatal("exact resource execution was not admitted")
	}
	if !bytes.Equal(prepared.result.SafeJSON, make([]byte, len(prepared.result.SafeJSON))) {
		t.Fatal("native owned result was retained")
	}
}

// TestResourceExecutorPriorEffectDoesNotBlockNextChild keeps plan-level history separate from child admission.
// TestResourceExecutorPriorEffectDoesNotBlockNextChild отделяет plan-level history от child admission.
func TestResourceExecutorPriorEffectDoesNotBlockNextChild(t *testing.T) {
	service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
	// Resolve marks any plan containing an effect; Begin alone decides this exact child's dispatch.
	// Resolve отмечает любой plan с effect; только Begin решает dispatch именно этого child.
	owner.snapshot.ReconcileOnly = true
	result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
	if err != nil || result.Result == nil || prepared.executes != 1 || owner.completes != 1 {
		t.Fatal("prior plan effect blocked an independently admitted child")
	}
}

// TestResourceExecutorRecoveryNeverDispatches covers stored results, gates and uncertain owner replies.
// TestResourceExecutorRecoveryNeverDispatches проверяет stored results, gates и uncertain owner replies.
func TestResourceExecutorRecoveryNeverDispatches(t *testing.T) {
	for _, mode := range []string{"accepted", "metadata", "gate", "lost-begin"} {
		t.Run(mode, func(t *testing.T) {
			service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
			switch mode {
			case "accepted", "metadata":
				owner.snapshot.ReconcileOnly, owner.admission.ReconcileOnly = true, true
				if mode == "accepted" {
					raw := []byte(`{"accepted":true}`)
					owner.admission.AcceptedResult = &targetrepo.AcceptedEffectResult{Outcome: "SUCCEEDED", ResultJSON: raw, ResultDigest: resourceResultDigest(raw)}
				}
			case "gate":
				owner.admission = targetrepo.EffectAdmission{GateRef: "gate_synthetic"}
			case "lost-begin":
				owner.beginErr = errors.New("synthetic lost acknowledgement")
			}
			var preparation targetrepo.ResourcePreparation = prepared
			if mode == "accepted" || mode == "metadata" {
				preparation = nil
			}
			result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, preparation)
			if (err != nil) != (mode == "lost-begin") || prepared.executes != 0 || owner.completes != 0 || owner.begins != 1 {
				t.Fatal("recovery or gate dispatched an effect")
			}
			if mode == "accepted" {
				if result.Result == nil {
					t.Fatal("accepted result missing")
				}
				owner.admission.AcceptedResult.ResultJSON[0] = 'x'
				if result.Result.ResultJSON[0] != '{' {
					t.Fatal("accepted result aliases owner memory")
				}
			}
			if mode == "gate" && result.GateRef != "gate_synthetic" {
				t.Fatal("gate metadata lost")
			}
		})
	}
}

// TestResourceExecutorRefusesChangedPins closes preparation before any admission of another graph.
// TestResourceExecutorRefusesChangedPins закрывает preparation до admission другого graph.
func TestResourceExecutorRefusesChangedPins(t *testing.T) {
	for _, mode := range []string{"target", "identity", "plan", "child-input", "duplicate-child", "native-target", "native-subject", "native-resource", "native-input"} {
		t.Run(mode, func(t *testing.T) {
			service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
			switch mode {
			case "target":
				owner.snapshot.Target.Revision++
			case "identity":
				owner.snapshot.Identity.ResourceVersion = "99"
			case "plan":
				owner.snapshot.PlanDigest = strings.Repeat("9", 64)
			case "child-input":
				plan.Children[0].InputDigest = strings.Repeat("9", 64)
			case "duplicate-child":
				plan.Children = append(plan.Children, plan.Children[0])
			case "native-target":
				prepared.pin.TargetDigest = strings.Repeat("9", 64)
			case "native-subject":
				prepared.pin.SubjectDigest = strings.Repeat("9", 64)
			case "native-resource":
				prepared.pin.Observation.Resource.ResourceVersion = "99"
			case "native-input":
				prepared.pin.InputDigest = strings.Repeat("9", 64)
			}
			result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
			if err == nil || result.Result != nil || owner.begins != 0 || prepared.executes != 0 || prepared.closes != 1 {
				t.Fatal("changed pin admitted")
			}
		})
	}
}

// TestResourceExecutorCompletionDoesNotGrantStaleOutput preserves cleanup but rejects stale or altered receipts.
// TestResourceExecutorCompletionDoesNotGrantStaleOutput сохраняет cleanup, отвергая stale либо altered receipts.
func TestResourceExecutorCompletionDoesNotGrantStaleOutput(t *testing.T) {
	for _, mode := range []string{"cancel", "discarded", "expired", "wide", "outcome", "digest", "submission", "lost-complete"} {
		t.Run(mode, func(t *testing.T) {
			service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				prepared.execute = cancel
			}
			if mode == "lost-complete" {
				owner.completeErr = errors.New("synthetic lost completion")
			}
			owner.receipt = func(receipt targetrepo.EffectReceipt) targetrepo.EffectReceipt {
				switch mode {
				case "discarded":
					receipt.Applied = false
				case "expired":
					receipt.AuthorizedUntil = time.Now().Add(-time.Second)
				case "wide":
					receipt.AuthorizedUntil = time.Now().Add(time.Hour)
				case "outcome":
					receipt.State = "FAILED"
				case "digest":
					receipt.ResultDigest = strings.Repeat("9", 64)
				case "submission":
					receipt.SubmissionDigest = strings.Repeat("9", 64)
				}
				return receipt
			}
			result, err := service.Execute(ctx, owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
			if err == nil || result.Result != nil || prepared.executes != 1 || prepared.closes != 1 || owner.completes != 1 || len(owner.completion.ResultJSON) == 0 {
				t.Fatal("completion lost cleanup or released unauthorized bytes")
			}
		})
	}
}

// TestResourceExecutorUncertainNativeResult records UNKNOWN and never retries or publishes apparent success.
// TestResourceExecutorUncertainNativeResult фиксирует UNKNOWN без retry либо apparent success.
func TestResourceExecutorUncertainNativeResult(t *testing.T) {
	service, owner, snapshot, plan, prepared := resourceExecutorFixture(t)
	prepared.err = errors.New("synthetic interrupted transport")
	result, err := service.Execute(t.Context(), owner.binding, snapshot, plan, plan.Children[0].Ref, prepared)
	if err != nil || result.Result == nil || result.Result.Outcome != "UNKNOWN" || string(result.Result.ResultJSON) != "{}" || owner.completion.Outcome != "UNKNOWN" || prepared.executes != 1 {
		t.Fatal("uncertain attempt was promoted or replayed")
	}
}
