package controlplane

import (
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// kubernetesStreamHistoryFromProto validates original metadata without using its expired deadline as execution credit.
// Actual process ownership and current output authority remain separate checks in the domain consumer.
// kubernetesStreamHistoryFromProto проверяет original metadata без превращения истёкшего deadline в execution credit.
// Фактический process ownership и current output authority проверяет отдельный domain consumer.
func kubernetesStreamHistoryFromProto(input *cp.ManagedKubernetesStreamHistory, binding managedkubernetes.Binding, opening targetrepo.EffectOpening, admission targetrepo.EffectAdmission) (*targetrepo.StreamHistory, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(input.GetStreamRef()) || len(input.GetStreamRef()) > 96 ||
		input.GetGeneration() != binding.Generation || input.GetHardDeadline() == nil || input.GetHardDeadline().CheckValid() != nil || len(input.GetHardDeadline().ProtoReflect().GetUnknown()) != 0 {
		return nil, false
	}
	state := strings.TrimPrefix(input.GetState().String(), "MANAGED_KUBERNETES_OWNER_STREAM_STATE_")
	deadline := input.GetHardDeadline().AsTime()
	if _, valid := kubernetesStreamStateToProto(state); !valid || deadline.IsZero() || deadline.After(opening.HardStreamDeadline) {
		return nil, false
	}
	history := &targetrepo.StreamHistory{Ref: input.GetStreamRef(), Generation: input.GetGeneration(), State: state, HardDeadline: deadline}
	if wire := input.GetHandshakeReceipt(); wire != nil {
		origin, valid := kubernetesStreamOriginFromProto(wire.GetOrigin())
		receipt := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: wire.GetTransportRef(), TransportGeneration: wire.GetTransportGeneration(), Protocol: wire.GetProtocol(), HTTPStatus: int(wire.GetHttpStatus()), NegotiatedProtocol: wire.GetNegotiatedProtocol(), PeerSPKISHA256: wire.GetPeerSpkiSha256()}
		if len(wire.ProtoReflect().GetUnknown()) != 0 || !valid || receipt.Validate() != nil || !kubernetesHistoryOriginMatches(origin, binding, opening, admission, history.Ref) {
			return nil, false
		}
		history.Handshake = &receipt
	}
	if wire := input.GetTerminalReceipt(); wire != nil {
		origin, valid := kubernetesStreamOriginFromProto(wire.GetOrigin())
		receipt := managedkubernetes.TerminalReceipt{Origin: origin, HandshakeDigest: wire.GetHandshakeDigest(), Protocol: wire.GetProtocol(), TransportJoined: wire.GetTransportJoined(), LocalState: strings.TrimPrefix(wire.GetLocalState().String(), "MANAGED_KUBERNETES_OWNER_STREAM_STATE_"), RemoteOutcome: strings.TrimPrefix(wire.GetRemoteOutcome().String(), "MANAGED_KUBERNETES_EFFECT_OUTCOME_"), CleanEOF: wire.GetCleanEof(), RemoteStatusCode: int(wire.GetRemoteStatusCode()), ReasonCode: wire.GetReasonCode()}
		if wire.ExitCode != nil {
			code := *wire.ExitCode
			receipt.ExitCode = &code
		}
		if len(wire.ProtoReflect().GetUnknown()) != 0 || !valid || receipt.Validate() != nil || !kubernetesHistoryOriginMatches(origin, binding, opening, admission, history.Ref) || state == "ACTIVE" || state == "PREPARED" || state == "COMPLETED" && receipt.LocalState != "COMPLETED" {
			return nil, false
		}
		if history.Handshake != nil {
			digest, err := history.Handshake.Digest()
			if err != nil || digest != receipt.HandshakeDigest || history.Handshake.Origin != receipt.Origin || history.Handshake.Protocol != receipt.Protocol {
				return nil, false
			}
		} else if receipt.HandshakeDigest != "" {
			return nil, false
		}
		history.Terminal = &receipt
	}
	if state == "ACTIVE" && history.Handshake == nil || state == "PREPARED" && history.Handshake != nil || state == "COMPLETED" && history.Terminal == nil {
		return nil, false
	}
	return history, true
}

// kubernetesStreamOriginFromProto preserves the closed immutable receipt origin without trusting caller-selected identifiers.
// kubernetesStreamOriginFromProto сохраняет closed immutable receipt origin без доверия caller-selected identifiers.
func kubernetesStreamOriginFromProto(input *cp.ManagedKubernetesStreamReceiptOrigin) (managedkubernetes.StreamReceiptOrigin, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedkubernetes.StreamReceiptOrigin{}, false
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: input.GetSnapshotDigest(), BindingDigest: input.GetBindingDigest(), ChildInputDigest: input.GetChildInputDigest(), EffectRef: input.GetEffectRef(), EffectGeneration: input.GetEffectGeneration(), StreamRef: input.GetStreamRef(), StreamGeneration: input.GetStreamGeneration(), Operation: input.GetOperation(), TargetRef: input.GetTargetRef(), TargetRevision: input.GetTargetRevision()}
	return origin, origin.Validate() == nil
}

// kubernetesHistoryOriginMatches binds every received fact to the original effect and claim, not a new attempt.
// kubernetesHistoryOriginMatches связывает каждый полученный факт с исходными effect и claim, не новой attempt.
func kubernetesHistoryOriginMatches(origin managedkubernetes.StreamReceiptOrigin, binding managedkubernetes.Binding, opening targetrepo.EffectOpening, admission targetrepo.EffectAdmission, streamRef string) bool {
	digest, err := managedkubernetes.BindingDigest(binding)
	return err == nil && origin.BindingDigest == digest && origin.StreamRef == streamRef && origin.StreamGeneration == binding.Generation && origin.EffectRef == admission.EffectRef && origin.EffectGeneration == admission.EffectGeneration && origin.ChildInputDigest == opening.ChildInputDigest && origin.Operation == opening.Operation
}
