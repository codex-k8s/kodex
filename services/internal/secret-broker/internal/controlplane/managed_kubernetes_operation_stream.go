package controlplane

import (
	"context"
	"slices"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// kubernetesStreamGrantFromProto rejects unknown authority fields and returns independent channel sets.
// kubernetesStreamGrantFromProto отклоняет unknown authority fields и возвращает отдельные channel sets.
func kubernetesStreamGrantFromProto(input *cp.ManagedKubernetesOwnerStreamGrant) (value.KubernetesStreamGrant, bool) {
	return kubernetesStreamGrantFromProtoAt(input, time.Now())
}

// kubernetesStreamGrantFromProtoAt separates structural parsing from the caller's lifetime boundary.
// Only non-executable reconciliation may pass a historical time; dispatch uses the current clock.
// kubernetesStreamGrantFromProtoAt отделяет structural parsing от lifetime boundary вызывающего кода.
// Только неисполняемый reconciliation допускает historical time; dispatch использует current clock.
func kubernetesStreamGrantFromProtoAt(input *cp.ManagedKubernetesOwnerStreamGrant, now time.Time) (value.KubernetesStreamGrant, bool) {
	var result value.KubernetesStreamGrant
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.GetExpiresAt() == nil || input.GetExpiresAt().CheckValid() != nil || len(input.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 {
		return result, false
	}
	result = value.KubernetesStreamGrant{Ref: input.GetStreamRef(), Generation: input.GetGeneration(), ExpiresAt: input.GetExpiresAt().AsTime(), MaximumFrameBytes: input.GetMaximumFrameBytes(), MaximumTotalBytes: input.GetMaximumTotalBytes(), InputChannels: slices.Clone(input.GetInputChannels()), OutputChannels: slices.Clone(input.GetOutputChannels())}
	return result, result.Valid(now)
}

// OpenKubernetesStream publishes a handshake once and refuses any changed stream identity or widened credit.
// An uncertain RPC result never permits a second handshake; the caller must close its existing transport.
// OpenKubernetesStream публикует handshake один раз и отклоняет смену stream identity либо расширение credit.
// Неопределённый RPC result не разрешает второй handshake; caller закрывает существующий transport.
func (owner *Owner) OpenKubernetesStream(ctx context.Context, binding managedkubernetes.Binding, input targetrepo.StreamOpening) (value.KubernetesStreamGrant, error) {
	var empty value.KubernetesStreamGrant
	digest, digestErr := input.HandshakeReceipt.Digest()
	bindingDigest, bindingErr := managedkubernetes.BindingDigest(binding)
	origin := input.HandshakeReceipt.Origin
	if owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil || binding.Validate() != nil || binding.InvocationRef == "" ||
		!managedkubernetes.ValidLocator(input.EffectRef) || input.EffectGeneration < 1 || !managedkubernetes.ValidDigest(input.HandshakeReceiptDigest) || !input.Prepared.Valid(time.Now()) || input.Prepared.Generation != binding.Generation ||
		digestErr != nil || bindingErr != nil || digest != input.HandshakeReceiptDigest || origin.BindingDigest != bindingDigest || origin.EffectRef != input.EffectRef || origin.EffectGeneration != input.EffectGeneration || origin.StreamRef != input.Prepared.Ref || origin.StreamGeneration != binding.Generation {
		return empty, targetrepo.ErrFrameUnavailable
	}
	response, err := owner.client.KubernetesOperations.OpenManagedKubernetesStream(ctx, &cp.OpenManagedKubernetesStreamRequest{Binding: kubernetesHealthBindingToProto(binding), EffectRef: input.EffectRef, EffectGeneration: input.EffectGeneration, StreamRef: input.Prepared.Ref, HandshakeReceiptDigest: input.HandshakeReceiptDigest,
		HandshakeReceipt: kubernetesHandshakeToProto(input.HandshakeReceipt)})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return empty, targetrepo.ErrFrameUnavailable
	}
	grant, valid := kubernetesStreamGrantFromProto(response.GetStream())
	if !valid || grant.Ref != input.Prepared.Ref || grant.Generation != input.Prepared.Generation || grant.ExpiresAt.After(input.Prepared.ExpiresAt) ||
		grant.MaximumFrameBytes != input.Prepared.MaximumFrameBytes || grant.MaximumTotalBytes != input.Prepared.MaximumTotalBytes ||
		!slices.Equal(grant.InputChannels, input.Prepared.InputChannels) || !slices.Equal(grant.OutputChannels, input.Prepared.OutputChannels) {
		return empty, targetrepo.ErrFrameUnavailable
	}
	return grant, nil
}

// kubernetesStreamStateToProto accepts only registered lifecycle states without string coercion.
// kubernetesStreamStateToProto принимает только registered lifecycle states без string coercion.
func kubernetesStreamStateToProto(state string) (cp.ManagedKubernetesOwnerStreamState, bool) {
	switch state {
	case "PREPARED":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_PREPARED, true
	case "ACTIVE":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE, true
	case "COMPLETED":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED, true
	case "CANCELED":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_CANCELED, true
	case "REVOKED":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_REVOKED, true
	case "EXPIRED":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_EXPIRED, true
	case "LOST":
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_LOST, true
	default:
		return cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_UNSPECIFIED, false
	}
}

// kubernetesEffectOutcomeToProto keeps unknown remote effects distinct from failure and success.
// kubernetesEffectOutcomeToProto отделяет unknown remote effects от failure и success.
func kubernetesEffectOutcomeToProto(outcome string) (cp.ManagedKubernetesEffectOutcome, bool) {
	switch outcome {
	case "SUCCEEDED":
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, true
	case "FAILED":
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_FAILED, true
	case "UNKNOWN":
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN, true
	default:
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNSPECIFIED, false
	}
}

// CloseKubernetesStream submits one finite receipt; the owner's final state supersedes local intent.
// A joined socket with no exit receipt remains UNKNOWN rather than claiming process termination.
// CloseKubernetesStream передаёт один finite receipt; owner final state приоритетнее local intent.
// Joined socket без exit receipt остаётся UNKNOWN и не утверждает process termination.
func (owner *Owner) CloseKubernetesStream(ctx context.Context, binding managedkubernetes.Binding, input targetrepo.StreamClosing) (targetrepo.StreamReceipt, error) {
	var result targetrepo.StreamReceipt
	digest, digestErr := input.TerminalReceipt.Digest()
	bindingDigest, bindingErr := managedkubernetes.BindingDigest(binding)
	origin := input.TerminalReceipt.Origin
	exitMatches := input.ObservedExitCode == nil && input.TerminalReceipt.ExitCode == nil || input.ObservedExitCode != nil && input.TerminalReceipt.ExitCode != nil && *input.ObservedExitCode == *input.TerminalReceipt.ExitCode
	expected, validExpected := kubernetesStreamStateToProto(input.ExpectedState)
	terminal, validTerminal := kubernetesStreamStateToProto(input.TerminalState)
	outcome, validOutcome := kubernetesEffectOutcomeToProto(input.RemoteOutcome)
	if owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil || binding.Validate() != nil || binding.InvocationRef == "" ||
		!managedkubernetes.ValidLocator(input.StreamRef) || !managedkubernetes.ValidDigest(input.TerminalReceiptDigest) || !validExpected || !validTerminal || !validOutcome ||
		input.TerminalState == "ACTIVE" || input.TerminalState == "PREPARED" || input.TerminalState == "COMPLETED" && (!input.TransportJoined || input.RemoteOutcome == "UNKNOWN") ||
		input.ObservedExitCode != nil && (*input.ObservedExitCode < 0 || *input.ObservedExitCode > 255 || input.RemoteOutcome == "UNKNOWN" || (*input.ObservedExitCode == 0) != (input.RemoteOutcome == "SUCCEEDED")) ||
		digestErr != nil || bindingErr != nil || digest != input.TerminalReceiptDigest || origin.BindingDigest != bindingDigest || origin.StreamRef != input.StreamRef || origin.StreamGeneration != binding.Generation || input.TerminalReceipt.TransportJoined != input.TransportJoined || !exitMatches || input.TerminalReceipt.LocalState != input.TerminalState || input.TerminalReceipt.RemoteOutcome != input.RemoteOutcome {
		return result, targetrepo.ErrFrameUnavailable
	}
	var observed *cp.ManagedKubernetesHandshakeReceipt
	if input.ObservedHandshake != nil {
		witness := *input.ObservedHandshake
		witnessDigest, err := witness.Digest()
		if err != nil || witness.Origin != origin || witness.Protocol != input.TerminalReceipt.Protocol || witnessDigest != input.TerminalReceipt.HandshakeDigest {
			return result, targetrepo.ErrFrameUnavailable
		}
		observed = kubernetesHandshakeToProto(witness)
	}
	response, err := owner.client.KubernetesOperations.CloseManagedKubernetesStream(ctx, &cp.CloseManagedKubernetesStreamRequest{ObservedHandshake: observed, Binding: kubernetesHealthBindingToProto(binding), StreamRef: input.StreamRef, ExpectedState: expected, TerminalState: terminal, RemoteOutcome: outcome, TransportJoined: input.TransportJoined, ObservedExitCode: input.ObservedExitCode, TerminalReceiptDigest: input.TerminalReceiptDigest,
		TerminalReceipt: &cp.ManagedKubernetesTerminalReceipt{Origin: kubernetesStreamOriginToProto(origin), HandshakeDigest: input.TerminalReceipt.HandshakeDigest, Protocol: input.TerminalReceipt.Protocol, TransportJoined: input.TransportJoined, LocalState: terminal, RemoteOutcome: outcome, ExitCode: input.ObservedExitCode, CleanEof: input.TerminalReceipt.CleanEOF, RemoteStatusCode: uint32(input.TerminalReceipt.RemoteStatusCode), ReasonCode: input.TerminalReceipt.ReasonCode}})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrFrameUnavailable
	}
	switch response.GetState() {
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED:
		result.State = "COMPLETED"
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_CANCELED:
		result.State = "CANCELED"
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_REVOKED:
		result.State = "REVOKED"
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_EXPIRED:
		result.State = "EXPIRED"
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_LOST:
		result.State = "LOST"
	default:
		return targetrepo.StreamReceipt{}, targetrepo.ErrFrameUnavailable
	}
	switch response.GetRemoteOutcome() {
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED:
		result.RemoteOutcome = "SUCCEEDED"
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_FAILED:
		result.RemoteOutcome = "FAILED"
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN:
		result.RemoteOutcome = "UNKNOWN"
	default:
		return targetrepo.StreamReceipt{}, targetrepo.ErrFrameUnavailable
	}
	if result.RemoteOutcome != input.RemoteOutcome || result.State == "COMPLETED" && (result.RemoteOutcome == "UNKNOWN" || input.TerminalState != "COMPLETED") {
		return targetrepo.StreamReceipt{}, targetrepo.ErrFrameUnavailable
	}
	return result, nil
}

// kubernetesStreamOriginToProto preserves every validated immutable origin field without authority coercion.
// kubernetesStreamOriginToProto сохраняет все validated immutable origin fields без authority coercion.
func kubernetesStreamOriginToProto(origin managedkubernetes.StreamReceiptOrigin) *cp.ManagedKubernetesStreamReceiptOrigin {
	return &cp.ManagedKubernetesStreamReceiptOrigin{SnapshotDigest: origin.SnapshotDigest, BindingDigest: origin.BindingDigest, ChildInputDigest: origin.ChildInputDigest, EffectRef: origin.EffectRef, EffectGeneration: origin.EffectGeneration, StreamRef: origin.StreamRef, StreamGeneration: origin.StreamGeneration, Operation: origin.Operation, TargetRef: origin.TargetRef, TargetRevision: origin.TargetRevision}
}

// kubernetesHandshakeToProto copies an already validated native witness without deriving authority.
// kubernetesHandshakeToProto копирует проверенный native witness без вывода новых полномочий.
func kubernetesHandshakeToProto(receipt managedkubernetes.HandshakeReceipt) *cp.ManagedKubernetesHandshakeReceipt {
	return &cp.ManagedKubernetesHandshakeReceipt{Origin: kubernetesStreamOriginToProto(receipt.Origin), TransportRef: receipt.TransportRef, TransportGeneration: receipt.TransportGeneration, Protocol: receipt.Protocol, HttpStatus: uint32(receipt.HTTPStatus), NegotiatedProtocol: receipt.NegotiatedProtocol, PeerSpkiSha256: receipt.PeerSPKISHA256}
}
