package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesHistoryToProto preserves original bounded evidence without a fresh grant.
// Expired hard deadlines remain historical; receipt validation cannot mint output/frame authority.
// managedKubernetesHistoryToProto сохраняет original bounded evidence без fresh grant.
// Истёкший hard deadline остаётся историей; receipt validation не создаёт output/frame authority.
func managedKubernetesHistoryToProto(history *entity.ManagedKubernetesStreamHistory) *cp.ManagedKubernetesStreamHistory {
	if history == nil || !managedkubernetes.ValidLocator(history.Ref) || len(history.Ref) > 96 || history.Generation < 1 {
		return nil
	}
	deadline := managedKubernetesTimestamp(history.HardDeadline)
	state := managedKubernetesStateToProto(history.State)
	if deadline == nil || state == cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_UNSPECIFIED {
		return nil
	}
	result := &cp.ManagedKubernetesStreamHistory{StreamRef: history.Ref, Generation: history.Generation, State: state, HardDeadline: deadline}
	if history.Handshake != nil {
		receipt := history.Handshake
		if receipt.Validate() != nil || receipt.Origin.StreamRef != history.Ref || receipt.Origin.StreamGeneration != history.Generation {
			return nil
		}
		result.HandshakeReceipt = &cp.ManagedKubernetesHandshakeReceipt{Origin: managedKubernetesReceiptOriginToProto(receipt.Origin), TransportRef: receipt.TransportRef, TransportGeneration: receipt.TransportGeneration, Protocol: receipt.Protocol, HttpStatus: uint32(receipt.HTTPStatus), NegotiatedProtocol: receipt.NegotiatedProtocol, PeerSpkiSha256: receipt.PeerSPKISHA256}
	}
	if history.Terminal != nil {
		receipt := history.Terminal
		if !history.State.Terminal() || history.State == enum.KubernetesStreamCompleted && receipt.LocalState != "COMPLETED" || receipt.Validate() != nil || receipt.Origin.StreamRef != history.Ref || receipt.Origin.StreamGeneration != history.Generation {
			return nil
		}
		if history.Handshake != nil {
			hash, err := history.Handshake.Digest()
			if err != nil || hash != receipt.HandshakeDigest || history.Handshake.Origin != receipt.Origin {
				return nil
			}
		} else if receipt.HandshakeDigest != "" {
			return nil
		}
		var exit *int32
		if receipt.ExitCode != nil {
			value := *receipt.ExitCode
			exit = &value
		}
		result.TerminalReceipt = &cp.ManagedKubernetesTerminalReceipt{Origin: managedKubernetesReceiptOriginToProto(receipt.Origin), HandshakeDigest: receipt.HandshakeDigest, Protocol: receipt.Protocol, TransportJoined: receipt.TransportJoined, LocalState: managedKubernetesStateToProto(enum.ManagedKubernetesStreamState(receipt.LocalState)), RemoteOutcome: managedKubernetesOutcomeToProto(enum.ManagedKubernetesEffectOutcome(receipt.RemoteOutcome)), ExitCode: exit, CleanEof: receipt.CleanEOF, RemoteStatusCode: uint32(receipt.RemoteStatusCode), ReasonCode: receipt.ReasonCode}
	}
	return result
}

// managedKubernetesReceiptOriginToProto copies only the validated immutable receipt lineage.
// managedKubernetesReceiptOriginToProto копирует лишь validated immutable receipt lineage.
func managedKubernetesReceiptOriginToProto(origin managedkubernetes.StreamReceiptOrigin) *cp.ManagedKubernetesStreamReceiptOrigin {
	return &cp.ManagedKubernetesStreamReceiptOrigin{SnapshotDigest: origin.SnapshotDigest, BindingDigest: origin.BindingDigest, ChildInputDigest: origin.ChildInputDigest, EffectRef: origin.EffectRef, EffectGeneration: origin.EffectGeneration, StreamRef: origin.StreamRef, StreamGeneration: origin.StreamGeneration, Operation: origin.Operation, TargetRef: origin.TargetRef, TargetRevision: origin.TargetRevision}
}

// managedKubernetesCleanupToProto binds closure evidence to the exact requested original claim.
// It emits no current snapshot or execution credit, including for a still-active stream with a closed lease.
// managedKubernetesCleanupToProto связывает closure evidence с exact requested original claim.
// Не выдаёт current snapshot либо execution credit, включая active stream с closed lease.
func managedKubernetesCleanupToProto(proof entity.ManagedKubernetesCleanupReceipt, binding value.ManagedKubernetesBinding, streamRef string) *cp.ManagedKubernetesCleanupReceipt {
	digest, err := managedkubernetes.BindingDigest(binding)
	state := managedKubernetesStateToProto(proof.State)
	if err != nil || !proof.ExecutionClosed || proof.InvocationRef != binding.InvocationRef || proof.StreamRef != streamRef || !managedkubernetes.ValidLocator(streamRef) || len(streamRef) > 96 || proof.Generation != binding.Generation || proof.BindingDigest != digest || !managedkubernetes.ValidDigest(proof.SnapshotDigest) || state == cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_UNSPECIFIED {
		return nil
	}
	return &cp.ManagedKubernetesCleanupReceipt{InvocationRef: proof.InvocationRef, StreamRef: proof.StreamRef, Generation: proof.Generation, BindingDigest: proof.BindingDigest, SnapshotDigest: proof.SnapshotDigest, StreamState: state, ExecutionClosed: true}
}

// managedKubernetesHistoryMatches rejects evidence from another child or original claim.
// Receipt-free history remains unreported metadata and cannot invent a handshake failure.
// managedKubernetesHistoryMatches отклоняет evidence другого child либо original claim.
// История без receipt остаётся unreported metadata, не выдумывает handshake failure.
func managedKubernetesHistoryMatches(history *entity.ManagedKubernetesStreamHistory, effectRef string, generation int64, inputDigest string, binding value.ManagedKubernetesBinding) bool {
	digest, err := managedkubernetes.BindingDigest(binding)
	if history == nil || err != nil || history.Generation != binding.Generation {
		return false
	}
	origins := make([]managedkubernetes.StreamReceiptOrigin, 0, 2)
	if history.Handshake != nil {
		origins = append(origins, history.Handshake.Origin)
	}
	if history.Terminal != nil {
		origins = append(origins, history.Terminal.Origin)
	}
	for _, origin := range origins {
		if origin.BindingDigest != digest || origin.EffectRef != effectRef || origin.EffectGeneration != generation || origin.ChildInputDigest != inputDigest {
			return false
		}
	}
	return true
}
