package kubernetesoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ResourceExecutor executes one owner child and never treats a child receipt as outer-run completion.
// It performs no automatic retry and releases bytes only under fresh output authority.
// ResourceExecutor исполняет один owner child и не считает его receipt завершением outer run.
// Он не повторяет запросы автоматически и выдаёт bytes только под fresh output authority.
type ResourceExecutor struct {
	owner  targetrepo.ResourceOwner
	access targetrepo.ResourceResultAccess
}

// NewResourceExecutor requires the authoritative owner, with no local admission fallback.
// NewResourceExecutor требует authoritative owner без local admission fallback.
func NewResourceExecutor(owner targetrepo.ResourceOwner, access targetrepo.ResourceResultAccess) (*ResourceExecutor, error) {
	if owner == nil || access == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &ResourceExecutor{owner: owner, access: access}, nil
}

// Execute consumes or closes the preparation on every path. Recovery may supply nil only to
// read an already reconciled result; it must not rediscover or redispatch the external effect.
// A gate returns metadata without dispatch. Complete runs after native join even on cancellation.
// Execute расходует либо закрывает preparation на каждом пути. Recovery может передать nil
// только для чтения reconciled result без повторного discovery либо external effect.
// Gate возвращает metadata без dispatch. Complete идёт после native join даже при cancellation.
func (service *ResourceExecutor) Execute(ctx context.Context, binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, childRef string, prepared targetrepo.ResourcePreparation) (targetrepo.ResourceExecution, error) {
	var empty targetrepo.ResourceExecution
	closed := false
	if prepared != nil {
		defer func() {
			if !closed {
				prepared.Close()
			}
		}()
	}
	if service == nil || service.owner == nil || ctx == nil || ctx.Err() != nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return empty, targetrepo.ErrOperationUnavailable
	}
	child, valid := resourcePlanChild(plan, childRef)
	pin, validSnapshot := streamOwnerReadPin(snapshot)
	if !valid || !validSnapshot {
		return empty, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	current, err := service.owner.ResolveKubernetesOperation(check, binding)
	checkErr := check.Err()
	cancel()
	currentPin, validCurrent := streamOwnerReadPin(current)
	if err != nil || checkErr != nil || !validCurrent || currentPin != pin || current.PlanRef != plan.Ref || current.PlanDigest != plan.Digest || current.PlanRevision != plan.Revision || !current.ExpiresAt.After(time.Now()) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if prepared == nil {
		if !current.ReconcileOnly {
			return empty, targetrepo.ErrOperationUnavailable
		}
	} else {
		actual, ready := prepared.Binding()
		matches := resourceObservationMatches(child.Observation, actual.Observation)
		if integrationpackage.KubernetesMetadataRead(child.Operation) {
			matches = child.Observation == nil && actual.Observation == (managedkubernetes.ResourceObservation{})
		}
		if !ready || actual.Operation != child.Operation || actual.InputDigest != child.InputDigest || actual.TargetDigest != current.Target.MetadataDigest() || actual.SubjectDigest != current.SubjectDigest || !matches {
			return empty, targetrepo.ErrOperationUnavailable
		}
	}
	admit, stop := context.WithTimeout(ctx, 3*time.Second)
	admission, err := service.owner.BeginKubernetesEffect(admit, binding, targetrepo.EffectOpening{PlanRef: plan.Ref, PlanDigest: plan.Digest, PlanRevision: plan.Revision, ChildRef: child.Ref, ChildInputDigest: child.InputDigest, Operation: child.Operation, ExpiresAt: current.ExpiresAt})
	admitErr := admit.Err()
	stop()
	if err != nil || admitErr != nil || admission.History != nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if admission.GateRef != "" {
		if !managedkubernetes.ValidLocator(admission.GateRef) || admission.EffectRef != "" || admission.AcceptedResult != nil || admission.Stream != nil || admission.ReconcileOnly || admission.EffectGeneration != 0 || admission.ChildInputDigest != "" || !admission.ExpiresAt.IsZero() {
			return empty, targetrepo.ErrOperationUnavailable
		}
		return targetrepo.ResourceExecution{PlanRef: plan.Ref, ChildRef: child.Ref, GateRef: admission.GateRef}, nil
	}
	if !managedkubernetes.ValidLocator(admission.EffectRef) || admission.EffectGeneration < 1 || admission.ChildInputDigest != child.InputDigest || admission.Stream != nil || admission.ExpiresAt.After(current.ExpiresAt) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result := targetrepo.ResourceExecution{PlanRef: plan.Ref, ChildRef: child.Ref, EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, ReconcileOnly: admission.ReconcileOnly}
	if admission.ReconcileOnly {
		if prepared != nil {
			prepared.Close()
			closed = true
		}
		if accepted := admission.AcceptedResult; accepted != nil {
			if !resourceOutputCredit(admission.ExpiresAt) || managedkubernetes.ValidateEffectResult(accepted.ResultJSON, accepted.ResultDigest, accepted.Outcome, accepted.ReasonCode) != nil || ctx.Err() != nil {
				return empty, targetrepo.ErrOperationUnavailable
			}
			until, err := service.outputCredit(ctx, current, child, admission.ExpiresAt)
			if err != nil {
				return empty, err
			}
			owned := *accepted
			owned.ResultJSON = bytes.Clone(accepted.ResultJSON)
			result.Result, result.AuthorizedUntil = &owned, until
		}
		return result, nil
	}
	if prepared == nil || admission.AcceptedResult != nil || !resourceOutputCredit(admission.ExpiresAt) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	observed, executeErr := prepared.Execute(ctx, admission.ExpiresAt)
	defer clear(observed.SafeJSON)
	prepared.Close()
	closed = true
	completion, digest := resourceCompletion(admission, observed, executeErr)
	defer clear(completion.ResultJSON)
	complete, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	receipt, err := service.owner.CompleteKubernetesEffect(complete, binding, completion)
	completeErr := complete.Err()
	finish()
	if err != nil || completeErr != nil || ctx.Err() != nil || !receipt.Applied || receipt.SubmissionDigest != digest || receipt.ResultDigest != digest || receipt.State != completion.Outcome || !resourceOutputCredit(receipt.AuthorizedUntil) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result.AuthorizedUntil, err = service.outputCredit(ctx, current, child, receipt.AuthorizedUntil)
	if err != nil {
		return empty, err
	}
	result.Result = &targetrepo.AcceptedEffectResult{Outcome: completion.Outcome, ReasonCode: completion.ReasonCode, ResultDigest: digest, ResultJSON: bytes.Clone(completion.ResultJSON)}
	return result, nil
}

// outputCredit intersects owner delivery authority with current target READ permissions without resource replay.
// Mutation receipts contain only effect metadata and do not grant a read-through-write capability.
// outputCredit пересекает owner delivery authority с current target READ permissions без resource replay.
// Mutation receipts содержат лишь effect metadata и не дают read-through-write capability.
func (service *ResourceExecutor) outputCredit(ctx context.Context, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, until time.Time) (time.Time, error) {
	operation, known := integrationpackage.KubernetesOperationByKey(child.Operation)
	if !known || !resourceOutputCredit(until) || ctx.Err() != nil {
		return time.Time{}, targetrepo.ErrOperationUnavailable
	}
	if operation.Risk == integrationpackage.RiskRead {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		targetUntil, err := service.access.AuthorizeKubernetesResourceResult(check, snapshot, child)
		checkErr := check.Err()
		cancel()
		if err != nil || checkErr != nil || !targetUntil.After(time.Now()) || targetUntil.After(time.Now().Add(5*time.Second)) {
			return time.Time{}, targetrepo.ErrOperationUnavailable
		}
		if targetUntil.Before(until) {
			until = targetUntil
		}
	}
	if !until.After(time.Now()) || ctx.Err() != nil {
		return time.Time{}, targetrepo.ErrOperationUnavailable
	}
	return until, nil
}

// resourcePlanChild verifies exact canonical input and unique ordered children before owner calls.
// resourcePlanChild проверяет exact canonical input и unique ordered children до owner calls.
func resourcePlanChild(plan targetrepo.OperationPlan, ref string) (targetrepo.OperationChild, bool) {
	var selected targetrepo.OperationChild
	if !managedkubernetes.ValidLocator(plan.Ref) || !managedkubernetes.ValidDigest(plan.Digest) || plan.Revision < 1 || len(plan.Children) == 0 || len(plan.Children) > 256 {
		return selected, false
	}
	seen := make(map[string]bool, len(plan.Children))
	for _, child := range plan.Children {
		if !managedkubernetes.ValidLocator(child.Ref) || seen[child.Ref] {
			return selected, false
		}
		seen[child.Ref] = true
		if child.Ref == ref {
			selected = child
		}
	}
	registered, known := integrationpackage.KubernetesOperationByKey(selected.Operation)
	_, canonical, err := managedkubernetes.DecodeOperationInput(selected.Operation, selected.NormalizedInput)
	if !known || err != nil || !bytes.Equal(canonical, selected.NormalizedInput) || resourceResultDigest(canonical) != selected.InputDigest || selected.NodeTransport != nil {
		return targetrepo.OperationChild{}, false
	}
	if integrationpackage.KubernetesMetadataRead(selected.Operation) {
		return selected, selected.Resource == (managedkubernetes.ResourceAddress{}) && selected.Observation == nil
	}
	if registered.Execution != integrationpackage.KubernetesResource || selected.Resource.Validate() != nil || selected.Observation == nil || selected.Observation.Validate() != nil || selected.Observation.Resource != selected.Resource || selected.Observation.PodWitness != nil {
		return targetrepo.OperationChild{}, false
	}
	return selected, true
}

// resourceObservationMatches binds the original plan's complete observation before fresh child admission.
// Optional namespace evidence is retained in the plan; missing current evidence creates no new GET requirement.
// resourceObservationMatches связывает complete observation original plan до fresh child admission.
// Optional namespace evidence сохраняется в plan; отсутствие current evidence не требует нового GET.
func resourceObservationMatches(planned *managedkubernetes.ChildCandidate, observed managedkubernetes.ResourceObservation) bool {
	return planned != nil && planned.Validate() == nil && planned.PodWitness == nil && observed.Validate() == nil &&
		planned.ObservationState == observed.State && planned.Resource == observed.Resource &&
		planned.ObservedResourceDigest == observed.ObservedResourceDigest && planned.RegisteredSchemaDigest == observed.RegisteredSchemaDigest &&
		(planned.NamespaceUID == "" || observed.NamespaceUID == "" || planned.NamespaceUID == observed.NamespaceUID)
}

// resourceCompletion preserves valid native facts and records malformed or uncertain tails without replay.
// resourceCompletion сохраняет valid native facts, а malformed либо uncertain tail фиксирует без replay.
func resourceCompletion(admission targetrepo.EffectAdmission, observed value.ResourceOperationResult, executeErr error) (targetrepo.EffectCompletion, string) {
	raw := bytes.Clone(observed.SafeJSON)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	digest := resourceResultDigest(raw)
	if managedkubernetes.ValidateEffectResult(raw, digest, observed.Outcome, observed.ReasonCode) != nil || executeErr != nil && observed.Outcome == "SUCCEEDED" {
		clear(raw)
		raw = []byte("{}")
		observed.Outcome, observed.ReasonCode = "FAILED", managedkubernetes.ReasonTargetUnavailable
		if observed.Attempted {
			observed.Outcome, observed.ReasonCode = "UNKNOWN", managedkubernetes.ReasonTransportUnknown
		}
		digest = resourceResultDigest(raw)
	}
	return targetrepo.EffectCompletion{EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, ChildInputDigest: admission.ChildInputDigest, Outcome: observed.Outcome, ReasonCode: observed.ReasonCode, ResultJSON: raw}, digest
}

// resourceResultDigest hashes the exact safe bytes, independently of outcome and output authority.
// resourceResultDigest хеширует exact safe bytes независимо от outcome и output authority.
func resourceResultDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// resourceOutputCredit permits only short future delivery credit, never a historical completion.
// resourceOutputCredit допускает лишь короткий future delivery credit, не historical completion.
func resourceOutputCredit(until time.Time) bool {
	now := time.Now()
	return until.After(now) && !until.After(now.Add(30*time.Second))
}
