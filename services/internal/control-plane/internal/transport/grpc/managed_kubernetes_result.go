package grpc

import (
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// kubernetesEmptyAcceptedResultSHA256 names the canonical discarded result, independently from
// the acknowledged submission digest. No discarded READ payload can be reconstructed from it.
// kubernetesEmptyAcceptedResultSHA256 задаёт canonical discarded result отдельно от submission digest.
// Из него нельзя восстановить discarded READ payload.
const kubernetesEmptyAcceptedResultSHA256 = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

// managedKubernetesOutputDeadline bounds fresh output admission to the current short outer lease.
// A historical effect/stream deadline or receipt timestamp cannot substitute fresh delivery authority.
// managedKubernetesOutputDeadline ограничивает fresh output admission текущей short outer lease.
// Historical effect/stream deadline либо receipt timestamp не заменяет fresh delivery authority.
func managedKubernetesOutputDeadline(deadline, now time.Time) bool {
	return !now.IsZero() && deadline.After(now) && !deadline.After(now.Add(30*time.Second))
}

// managedKubernetesAcceptedResultToProto releases only a stored applied result under fresh output
// credit. It validates exact bytes, closed outcome/reason and digest before any response projection.
// managedKubernetesAcceptedResultToProto выдаёт только stored applied result под fresh output credit.
// Проверяет exact bytes, closed outcome/reason и digest до response projection.
func managedKubernetesAcceptedResultToProto(input *entity.ManagedKubernetesAcceptedEffectResult, deadline, now time.Time) *cp.ManagedKubernetesAcceptedEffectResult {
	if input == nil || !managedKubernetesOutputDeadline(deadline, now) || managedkubernetes.ValidateEffectResult(input.ResultJSON, input.ResultDigest, string(input.Outcome), input.ReasonCode) != nil {
		return nil
	}
	return &cp.ManagedKubernetesAcceptedEffectResult{Outcome: managedKubernetesOutcomeToProto(input.Outcome), ResultJson: append([]byte(nil), input.ResultJSON...), ResultDigest: input.ResultDigest, ReasonCode: input.ReasonCode}
}

// managedKubernetesEffectReceiptToProto keeps acknowledged submission and accepted stored bytes
// distinct. Discarded late READs have no output credit; accepted data requires the exact submitted
// outcome/digest and fresh owner deadline. No UNKNOWN submission can become a successful receipt.
// managedKubernetesEffectReceiptToProto разделяет acknowledged submission и accepted stored bytes.
// Discarded late READ не имеет output credit; accepted data требует exact submitted outcome/digest
// и fresh owner deadline. UNKNOWN submission не превращается в successful receipt.
func managedKubernetesEffectReceiptToProto(input entity.ManagedKubernetesEffectReceipt, submittedDigest string, submittedOutcome enum.ManagedKubernetesEffectOutcome, now time.Time) *cp.CompleteManagedKubernetesEffectResponse {
	if !managedkubernetes.ValidDigest(submittedDigest) || input.SubmissionDigest != submittedDigest || !managedkubernetes.ValidDigest(input.ResultDigest) {
		return nil
	}
	switch submittedOutcome {
	case enum.KubernetesEffectSucceeded, enum.KubernetesEffectFailed, enum.KubernetesEffectUnknown:
	default:
		return nil
	}
	response := &cp.CompleteManagedKubernetesEffectResponse{State: input.State, ResultDigest: input.ResultDigest, SubmissionDigest: input.SubmissionDigest, Applied: input.Applied}
	if !input.Applied {
		if input.State != "CANCELLED" || !input.AuthorizedUntil.IsZero() || input.ResultDigest != kubernetesEmptyAcceptedResultSHA256 {
			return nil
		}
		return response
	}
	if input.State != string(submittedOutcome) || input.ResultDigest != submittedDigest || !managedKubernetesOutputDeadline(input.AuthorizedUntil, now) {
		return nil
	}
	response.AuthorizedUntil = managedKubernetesTimestamp(input.AuthorizedUntil)
	if response.AuthorizedUntil == nil {
		return nil
	}
	return response
}
