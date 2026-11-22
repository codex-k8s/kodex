package kubernetesoperation

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// reconcileHistory closes lost routing metadata without preparing credentials or repeating a native handshake.
// Another broker replica may still own a writer: only stored native evidence may claim join or a known remote result.
// A new receipt grants no output; a later owner read must independently admit its accepted projection.
// reconcileHistory закрывает metadata потерянного маршрута без подготовки credentials либо повторения native handshake.
// Writer может принадлежать другой broker replica: лишь stored native evidence доказывает join либо known remote result.
// Новый receipt не выдаёт output; следующий owner read отдельно допускает accepted projection.
func (service *StreamOperations) reconcileHistory(binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild, admission targetrepo.EffectAdmission) (targetrepo.OperationExecution, error) {
	result, err := streamReconciledResult(snapshot, plan, child, admission)
	if err != nil || admission.History == nil {
		return result, err
	}
	history := admission.History
	origin, valid := streamHistoryOrigin(binding, snapshot, child, admission)
	if !valid {
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
	terminal := managedkubernetes.TerminalReceipt{Origin: origin, Protocol: streamChildProtocol(child), LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "OWNER_STATE_LOST"}
	if history.Handshake != nil {
		terminal.HandshakeDigest, _ = history.Handshake.Digest()
		terminal.ReasonCode = "OUTPUT_LOST"
	}
	if history.Terminal != nil {
		terminal = *history.Terminal
	}
	if accepted := admission.AcceptedResult; accepted != nil {
		if history.Terminal == nil || managedkubernetes.ValidateStreamEffectResult(terminal, accepted.ResultJSON, accepted.ResultDigest, accepted.Outcome, accepted.ReasonCode) != nil {
			return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
		}
		return result, nil
	}
	if terminal.Validate() != nil || service.recordStreamTerminal(binding, terminal, history.Handshake, history.State) != nil {
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
	return result, nil
}

// streamHistoryOrigin compares owner history with the exact retained plan and registered native transport pin.
// It refuses invented receipt lineage and does not infer a handshake failure from absent evidence.
// streamHistoryOrigin сравнивает owner history с exact retained plan и registered native transport pin.
// Отклоняет выдуманную receipt lineage и не выводит неуспех handshake из отсутствия evidence.
func streamHistoryOrigin(binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, admission targetrepo.EffectAdmission) (managedkubernetes.StreamReceiptOrigin, bool) {
	var empty managedkubernetes.StreamReceiptOrigin
	history := admission.History
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || history == nil || streamChildProtocol(child) == "" || history.Generation != binding.Generation || !managedkubernetes.ValidLocator(history.Ref) || len(history.Ref) > 96 || history.HardDeadline.IsZero() || (child.NodeTransport != nil && history.HardDeadline.After(child.NodeTransport.ExpiresAt)) {
		return empty, false
	}
	switch history.State {
	case "PREPARED", "ACTIVE", "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
	default:
		return empty, false
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: digest, ChildInputDigest: child.InputDigest, EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, StreamRef: history.Ref, StreamGeneration: history.Generation, Operation: child.Operation, TargetRef: snapshot.Target.ID, TargetRevision: int64(snapshot.Target.Revision)}
	if origin.Validate() != nil || history.State == "ACTIVE" && history.Handshake == nil || history.State == "PREPARED" && history.Handshake != nil || history.State == "COMPLETED" && history.Terminal == nil {
		return empty, false
	}
	if handshake := history.Handshake; handshake != nil {
		if handshake.Validate() != nil || handshake.Origin != origin || handshake.Protocol != streamChildProtocol(child) || !streamHistoryTransportMatches(*handshake, child, snapshot) {
			return empty, false
		}
	}
	if terminal := history.Terminal; terminal != nil {
		if terminal.Validate() != nil || terminal.Origin != origin || terminal.Protocol != streamChildProtocol(child) || history.State == "ACTIVE" || history.State == "PREPARED" || history.State == "COMPLETED" && terminal.LocalState != "COMPLETED" {
			return empty, false
		}
		if history.Handshake == nil {
			if terminal.HandshakeDigest != "" {
				return empty, false
			}
		} else if hash, hashErr := history.Handshake.Digest(); hashErr != nil || terminal.HandshakeDigest != hash {
			return empty, false
		}
	}
	return origin, true
}

// streamHistoryTransportMatches validates retained transport coordinates without requesting new credentials or output credit.
// HTTP receipts pin API target/revision; privileged receipts additionally require the registered node key.
// streamHistoryTransportMatches проверяет retained transport coordinates без новых credentials либо output credit.
// HTTP receipts закрепляют API target/revision; privileged receipts дополнительно требуют registered node key.
func streamHistoryTransportMatches(handshake managedkubernetes.HandshakeReceipt, child targetrepo.OperationChild, snapshot value.KubernetesOperationSnapshot) bool {
	if (child.Operation == "kubernetes.logs" || child.Operation == "kubernetes.watch" || child.Operation == "kubernetes.watch.all-namespaces") && child.NodeTransport == nil {
		return handshake.TransportRef == snapshot.Target.ID && handshake.TransportGeneration == int64(snapshot.Target.Revision)
	}
	return child.NodeTransport != nil && handshake.TransportRef == child.NodeTransport.Ref && handshake.TransportGeneration == child.NodeTransport.Generation && handshake.PeerSPKISHA256 == child.NodeTransport.ServerPublicKeySHA256
}
