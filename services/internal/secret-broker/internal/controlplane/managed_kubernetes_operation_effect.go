package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BeginKubernetesEffect preserves the exact child pins and makes only one protected RPC attempt.
// Gate metadata never grants dispatch; an uncertain response cannot be converted into a retry.
// BeginKubernetesEffect сохраняет точные child pins и выполняет одну попытку protected RPC.
// Gate metadata не разрешает dispatch; неопределённый ответ не превращается в retry.
func (owner *Owner) BeginKubernetesEffect(ctx context.Context, binding managedkubernetes.Binding, input targetrepo.EffectOpening) (targetrepo.EffectAdmission, error) {
	var empty targetrepo.EffectAdmission
	registered, known := integrationpackage.KubernetesOperationByKey(input.Operation)
	if !owner.kubernetesEffectReady(ctx, binding) || !known || !managedkubernetes.ValidLocator(input.PlanRef) || input.PlanRevision < 1 || !managedkubernetes.ValidDigest(input.PlanDigest) || !managedkubernetes.ValidLocator(input.ChildRef) || !managedkubernetes.ValidDigest(input.ChildInputDigest) || !input.ExpiresAt.After(time.Now()) || registered.Execution == integrationpackage.KubernetesStream && input.HardStreamDeadline.IsZero() {
		return empty, targetrepo.ErrOperationUnavailable
	}
	response, err := owner.client.KubernetesOperations.BeginManagedKubernetesEffect(ctx, &cp.BeginManagedKubernetesEffectRequest{Binding: kubernetesHealthBindingToProto(binding), PlanRef: input.PlanRef, PlanRevision: input.PlanRevision, PlanDigest: input.PlanDigest, ChildRef: input.ChildRef, ChildInputDigest: input.ChildInputDigest, FileSourceRef: input.FileSourceRef, FileSourceVersion: input.FileSourceVersion})
	if err != nil {
		if ctx.Err() == nil {
			if gate := kubernetesEffectGate(err, input); gate != "" {
				return targetrepo.EffectAdmission{GateRef: gate}, nil
			}
		}
		return empty, targetrepo.ErrOperationUnavailable
	}
	if ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(response.GetEffectRef()) || response.GetEffectGeneration() < 1 || response.GetChildInputDigest() != input.ChildInputDigest || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || len(response.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result := targetrepo.EffectAdmission{EffectRef: response.GetEffectRef(), EffectGeneration: response.GetEffectGeneration(), ChildInputDigest: response.GetChildInputDigest(), ExpiresAt: response.GetExpiresAt().AsTime(), ReconcileOnly: response.GetReconcileOnly()}
	if result.ExpiresAt.After(input.ExpiresAt) || !result.ReconcileOnly && !result.ExpiresAt.After(time.Now()) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if response.GetStreamHistory() != nil {
		if !result.ReconcileOnly || response.GetStream() != nil || registered.Execution != integrationpackage.KubernetesStream {
			return empty, targetrepo.ErrOperationUnavailable
		}
		history, valid := kubernetesStreamHistoryFromProto(response.GetStreamHistory(), binding, input, result)
		if !valid {
			return empty, targetrepo.ErrOperationUnavailable
		}
		result.History = history
	}
	if accepted := response.GetAcceptedResult(); accepted != nil {
		if !result.ReconcileOnly || !kubernetesOutputDeadline(result.ExpiresAt, time.Now()) || len(accepted.ProtoReflect().GetUnknown()) != 0 {
			return empty, targetrepo.ErrOperationUnavailable
		}
		outcome := ""
		switch accepted.GetOutcome() {
		case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED:
			outcome = "SUCCEEDED"
		case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_FAILED:
			outcome = "FAILED"
		case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN:
			outcome = "UNKNOWN"
		default:
			return empty, targetrepo.ErrOperationUnavailable
		}
		if managedkubernetes.ValidateEffectResult(accepted.GetResultJson(), accepted.GetResultDigest(), outcome, accepted.GetReasonCode()) != nil {
			return empty, targetrepo.ErrOperationUnavailable
		}
		result.AcceptedResult = &targetrepo.AcceptedEffectResult{Outcome: outcome, ReasonCode: accepted.GetReasonCode(), ResultDigest: accepted.GetResultDigest(), ResultJSON: bytes.Clone(accepted.GetResultJson())}
		if result.History != nil && (result.History.Terminal == nil || managedkubernetes.ValidateStreamEffectResult(*result.History.Terminal, accepted.GetResultJson(), accepted.GetResultDigest(), outcome, accepted.GetReasonCode()) != nil) {
			return empty, targetrepo.ErrOperationUnavailable
		}
	}
	if response.GetStream() != nil {
		// Reconciliation retains identity but supplies no fresh transport grant.
		// Reconciliation сохраняет identity, но не выдаёт fresh transport grant.
		grantClock := time.Now()
		if result.ReconcileOnly {
			grantClock = time.Time{}
		}
		grant, valid := kubernetesStreamGrantFromProtoAt(response.GetStream(), grantClock)
		if !valid || registered.Execution != integrationpackage.KubernetesStream || grant.Generation != binding.Generation || grant.ExpiresAt.After(input.HardStreamDeadline) {
			return empty, targetrepo.ErrOperationUnavailable
		}
		if !result.ReconcileOnly {
			result.Stream = &grant
		}
	} else if !result.ReconcileOnly && registered.Execution == integrationpackage.KubernetesStream {
		return empty, targetrepo.ErrOperationUnavailable
	}
	return result, nil
}

// kubernetesEffectReady checks local wiring and original invocation shape without inventing authority.
// kubernetesEffectReady проверяет local wiring и исходную invocation shape без создания полномочий.
func (owner *Owner) kubernetesEffectReady(ctx context.Context, binding managedkubernetes.Binding) bool {
	return ctx != nil && ctx.Err() == nil && owner != nil && owner.client != nil && owner.client.KubernetesOperations != nil && binding.Validate() == nil && binding.InvocationRef != ""
}

// kubernetesEffectGate accepts one exact typed owner gate and discards arbitrary error messages.
// kubernetesEffectGate принимает один exact typed owner gate и отбрасывает произвольные error messages.
func kubernetesEffectGate(err error, input targetrepo.EffectOpening) string {
	s := status.Convert(err)
	if s.Code() != codes.FailedPrecondition || len(s.Details()) != 1 {
		return ""
	}
	info, ok := s.Details()[0].(*errdetails.ErrorInfo)
	if !ok || len(info.ProtoReflect().GetUnknown()) != 0 || info.Domain != "kodex.control-plane" || info.Reason != "KUBERNETES_APPROVAL_REQUIRED" || len(info.Metadata) != 3 || info.Metadata["childRef"] != input.ChildRef || info.Metadata["planRef"] != input.PlanRef || !managedkubernetes.ValidLocator(info.Metadata["gateRef"]) {
		return ""
	}
	return info.Metadata["gateRef"]
}

// CompleteKubernetesEffect seals exact safe bytes and accepts terminal override without renewing a lease.
// Syntax validation does not sanitize data: the native projection must remove secrets before this call.
// CompleteKubernetesEffect закрепляет exact safe bytes и принимает terminal override без продления lease.
// Syntax validation не очищает данные: native projection удаляет secrets до этого вызова.
func (owner *Owner) CompleteKubernetesEffect(ctx context.Context, binding managedkubernetes.Binding, input targetrepo.EffectCompletion) (targetrepo.EffectReceipt, error) {
	var empty targetrepo.EffectReceipt
	outcome, valid := kubernetesEffectOutcomeToProto(input.Outcome)
	if !owner.kubernetesEffectReady(ctx, binding) || !valid || !managedkubernetes.ValidLocator(input.EffectRef) || input.EffectGeneration < 1 || !managedkubernetes.ValidDigest(input.ChildInputDigest) || input.StreamRef != "" && !managedkubernetes.ValidLocator(input.StreamRef) || len(input.ResultJSON) > managedkubernetes.MaximumEffectResultBytes {
		return empty, targetrepo.ErrOperationUnavailable
	}
	raw := bytes.Clone(input.ResultJSON)
	digest := sha256.Sum256(raw)
	encoded := hex.EncodeToString(digest[:])
	if managedkubernetes.ValidateEffectResult(raw, encoded, input.Outcome, input.ReasonCode) != nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	response, err := owner.client.KubernetesOperations.CompleteManagedKubernetesEffect(ctx, &cp.CompleteManagedKubernetesEffectRequest{Binding: kubernetesHealthBindingToProto(binding), EffectRef: input.EffectRef, EffectGeneration: input.EffectGeneration, ChildInputDigest: input.ChildInputDigest, Outcome: outcome, ResultJson: raw, ResultDigest: encoded, ReasonCode: input.ReasonCode, StreamRef: input.StreamRef})
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetSubmissionDigest() != encoded {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if !response.GetApplied() {
		if response.GetState() != "CANCELLED" || response.GetAuthorizedUntil() != nil || response.GetResultDigest() != kubernetesDiscardedResultDigest {
			return empty, targetrepo.ErrOperationUnavailable
		}
		return targetrepo.EffectReceipt{State: response.GetState(), ResultDigest: response.GetResultDigest(), SubmissionDigest: encoded}, nil
	}
	deadline := response.GetAuthorizedUntil()
	if response.GetState() != input.Outcome || response.GetResultDigest() != encoded || deadline == nil || deadline.CheckValid() != nil || len(deadline.ProtoReflect().GetUnknown()) != 0 || !kubernetesOutputDeadline(deadline.AsTime(), time.Now()) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	return targetrepo.EffectReceipt{State: response.GetState(), ResultDigest: encoded, SubmissionDigest: encoded, Applied: true, AuthorizedUntil: deadline.AsTime()}, nil
}

// kubernetesDiscardedResultDigest identifies the canonical empty stored projection, not submitted bytes.
// kubernetesDiscardedResultDigest обозначает пустую stored projection, не submitted bytes.
const kubernetesDiscardedResultDigest = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

// kubernetesOutputDeadline permits only short fresh delivery credit, never historical execution deadlines.
// kubernetesOutputDeadline допускает только short fresh delivery credit, не historical execution deadlines.
func kubernetesOutputDeadline(deadline, now time.Time) bool {
	return deadline.After(now) && !deadline.After(now.Add(30*time.Second))
}
