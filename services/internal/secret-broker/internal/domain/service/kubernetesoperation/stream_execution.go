package kubernetesoperation

import (
	"bytes"
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// streamEffectOpening binds one owner child to a short opening lease and a separate immutable upper lifetime.
// streamEffectOpening связывает owner child с short opening lease и отдельным immutable upper lifetime.
func streamEffectOpening(snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild, hard time.Time) targetrepo.EffectOpening {
	return targetrepo.EffectOpening{PlanRef: plan.Ref, PlanDigest: plan.Digest, PlanRevision: plan.Revision, ChildRef: child.Ref, ChildInputDigest: child.InputDigest, Operation: child.Operation, ExpiresAt: snapshot.ExpiresAt, HardStreamDeadline: hard}
}

// validStreamGate accepts only a pending gate with no hidden effect or dispatch credit.
// validStreamGate принимает только pending gate без скрытого effect либо dispatch credit.
func validStreamGate(admission targetrepo.EffectAdmission) bool {
	return managedkubernetes.ValidLocator(admission.GateRef) && admission.EffectRef == "" && admission.EffectGeneration == 0 && admission.ChildInputDigest == "" && admission.Stream == nil && admission.AcceptedResult == nil && admission.History == nil && !admission.ReconcileOnly && admission.ExpiresAt.IsZero()
}

// validStreamAdmission checks exact fresh credit before any native stream can open.
// validStreamAdmission проверяет exact fresh credit до открытия native stream.
func validStreamAdmission(binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, hard time.Time, admission targetrepo.EffectAdmission) bool {
	return managedkubernetes.ValidLocator(admission.EffectRef) && admission.EffectGeneration > 0 && admission.ChildInputDigest == child.InputDigest && admission.GateRef == "" && !admission.ReconcileOnly && admission.AcceptedResult == nil && admission.History == nil && admission.Stream != nil && admission.Stream.Valid(time.Now()) && admission.Stream.Generation == binding.Generation && !admission.Stream.ExpiresAt.After(hard) && resourceOutputCredit(admission.ExpiresAt) && !admission.ExpiresAt.After(snapshot.ExpiresAt)
}

// reconcile reads accepted effect metadata without loading credentials or recreating a lost local buffer.
// The bound is verification-only for a historical grant; it never renews that grant in the owner.
// reconcile читает accepted effect metadata без загрузки credentials либо воссоздания lost local buffer.
// Bound используется лишь для проверки historical grant и не продлевает его в owner.
func (service *StreamOperations) reconcile(ctx context.Context, binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild) (targetrepo.OperationExecution, error) {
	read, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	defer cancel()
	admission, err := service.owner.BeginKubernetesEffect(read, binding, streamEffectOpening(snapshot, plan, child, streamReconcileDeadline(snapshot, child)))
	if err != nil || read.Err() != nil || !admission.ReconcileOnly {
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
	return service.reconcileHistory(binding, snapshot, plan, child, admission)
}

// streamReconciledResult copies only safe owner-accepted bytes under current output credit.
// In-progress recovery remains explicit and supplies neither a descriptor nor native dispatch authority.
// streamReconciledResult копирует лишь safe owner-accepted bytes под current output credit.
// In-progress recovery остаётся явным и не выдаёт descriptor либо native dispatch authority.
func streamReconciledResult(snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild, admission targetrepo.EffectAdmission) (targetrepo.OperationExecution, error) {
	var empty targetrepo.OperationExecution
	if !admission.ReconcileOnly || admission.GateRef != "" || !managedkubernetes.ValidLocator(admission.EffectRef) || admission.EffectGeneration < 1 || admission.ChildInputDigest != child.InputDigest || admission.Stream != nil || admission.ExpiresAt.After(snapshot.ExpiresAt) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result := targetrepo.OperationExecution{Effect: targetrepo.ResourceExecution{PlanRef: plan.Ref, ChildRef: child.Ref, EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, ReconcileOnly: true}}
	if accepted := admission.AcceptedResult; accepted != nil {
		if !resourceOutputCredit(admission.ExpiresAt) || managedkubernetes.ValidateEffectResult(accepted.ResultJSON, accepted.ResultDigest, accepted.Outcome, accepted.ReasonCode) != nil {
			return empty, targetrepo.ErrOperationUnavailable
		}
		owned := *accepted
		owned.ResultJSON = bytes.Clone(accepted.ResultJSON)
		result.Effect.Result, result.Effect.AuthorizedUntil = &owned, admission.ExpiresAt
	}
	return result, nil
}

// start performs one native handshake, records it in the owner and transfers transport to joined workers.
// Every failed opening is closed before late UNKNOWN evidence; no error permits a second handshake.
// start выполняет один native handshake, записывает его в owner и передаёт transport joined workers.
// Каждый failed opening закрывается до late UNKNOWN evidence; ошибка не разрешает второй handshake.
func (service *StreamOperations) start(ctx context.Context, binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, plan targetrepo.OperationPlan, child targetrepo.OperationChild, prepared targetrepo.PodStreamPreparation, admission targetrepo.EffectAdmission) (targetrepo.OperationExecution, *ActiveStream, *CurrentStreamAccess, managedkubernetes.HandshakeReceipt, error) {
	var empty targetrepo.OperationExecution
	var noReceipt managedkubernetes.HandshakeReceipt
	origin := streamOrigin(binding, snapshot, child, admission)
	transport, handshake, err := prepared.Open(service.lifecycle, ctx, binding, admission)
	prepared.Close()
	if err != nil || transport == nil {
		terminal := managedkubernetes.TerminalReceipt{Origin: origin, Protocol: streamChildProtocol(child), LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "HANDSHAKE_FAILED", TransportJoined: true}
		service.closeFailedStream(binding, transport, terminal, nil, "PREPARED")
		return empty, nil, nil, noReceipt, targetrepo.ErrOperationUnavailable
	}
	digest, err := handshake.Digest()
	if err != nil || handshake.Origin != origin {
		terminal := managedkubernetes.TerminalReceipt{Origin: origin, Protocol: streamChildProtocol(child), LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "HANDSHAKE_FAILED", TransportJoined: true}
		service.closeFailedStream(binding, transport, terminal, nil, "PREPARED")
		return empty, nil, nil, noReceipt, targetrepo.ErrOperationUnavailable
	}
	open, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	grant, err := service.owner.OpenKubernetesStream(open, binding, targetrepo.StreamOpening{EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, Prepared: *admission.Stream, HandshakeReceiptDigest: digest, HandshakeReceipt: handshake})
	openErr := open.Err()
	cancel()
	if err != nil || openErr != nil || !sameStreamGrant(grant, *admission.Stream) {
		service.closeFailedStream(binding, transport, failedOpenedStream(handshake), &handshake, "ACTIVE")
		return empty, nil, nil, noReceipt, targetrepo.ErrOperationUnavailable
	}
	snapshot.NodeTransport = child.NodeTransport
	access, err := NewCurrentStreamAccess(service.owner, prepared.TargetAccess(), binding, origin, snapshot)
	if err != nil {
		service.closeFailedStream(binding, transport, failedOpenedStream(handshake), &handshake, "ACTIVE")
		return empty, nil, nil, noReceipt, targetrepo.ErrOperationUnavailable
	}
	active, err := StartActiveStream(service.lifecycle, service.owner, access, transport, binding, grant, handshake)
	if err != nil {
		service.closeFailedStream(binding, transport, failedOpenedStream(handshake), &handshake, "ACTIVE")
		return empty, nil, nil, noReceipt, targetrepo.ErrOperationUnavailable
	}
	result := targetrepo.OperationExecution{Effect: targetrepo.ResourceExecution{PlanRef: plan.Ref, ChildRef: child.Ref, EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration}, Stream: &grant}
	return result, active, access, handshake, nil
}

// streamOrigin derives receipt identity only from already verified owner metadata and the original binding.
// streamOrigin выводит receipt identity только из verified owner metadata и original binding.
func streamOrigin(binding managedkubernetes.Binding, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild, admission targetrepo.EffectAdmission) managedkubernetes.StreamReceiptOrigin {
	digest, _ := managedkubernetes.BindingDigest(binding)
	return managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: digest, ChildInputDigest: child.InputDigest, EffectRef: admission.EffectRef, EffectGeneration: admission.EffectGeneration, StreamRef: admission.Stream.Ref, StreamGeneration: admission.Stream.Generation, Operation: child.Operation, TargetRef: snapshot.Target.ID, TargetRevision: int64(snapshot.Target.Revision)}
}

// sameStreamGrant rejects expanded or changed owner credit while accepting a shortened hard deadline.
// sameStreamGrant отклоняет expanded/changed owner credit, допуская сокращённый hard deadline.
func sameStreamGrant(actual, expected value.KubernetesStreamGrant) bool {
	return actual.Valid(time.Now()) && actual.Ref == expected.Ref && actual.Generation == expected.Generation && !actual.ExpiresAt.After(expected.ExpiresAt) && actual.MaximumFrameBytes == expected.MaximumFrameBytes && actual.MaximumTotalBytes == expected.MaximumTotalBytes && slices.Equal(actual.InputChannels, expected.InputChannels) && slices.Equal(actual.OutputChannels, expected.OutputChannels)
}

// failedOpenedStream preserves observed handshake facts but never invents a remote outcome after local close.
// failedOpenedStream сохраняет observed handshake facts, не выдумывая remote outcome после local close.
func failedOpenedStream(handshake managedkubernetes.HandshakeReceipt) managedkubernetes.TerminalReceipt {
	digest, _ := handshake.Digest()
	return managedkubernetes.TerminalReceipt{Origin: handshake.Origin, HandshakeDigest: digest, Protocol: handshake.Protocol, LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "TRANSPORT_LOST", TransportJoined: true}
}

// finish persists exact terminal evidence before the effect result, using independent bounded cleanup contexts.
// An unknown owner ACK stays an error; a matching discarded late receipt grants no business success or output.
// finish сохраняет exact terminal evidence до effect result через independent bounded cleanup contexts.
// Unknown owner ACK остаётся ошибкой; matching discarded late receipt не даёт business success либо output.
func (service *StreamOperations) finish(binding managedkubernetes.Binding, terminal managedkubernetes.TerminalReceipt, handshake *managedkubernetes.HandshakeReceipt, expected string) error {
	if !terminal.TransportJoined {
		return targetrepo.ErrOperationUnavailable
	}
	return service.recordStreamTerminal(binding, terminal, handshake, expected)
}

// recordStreamTerminal persists validated evidence without interpreting a foreign or missing writer as joined.
// Locally owned transports must call finish after positive join; history recovery supplies explicit uncertainty instead.
// recordStreamTerminal сохраняет validated evidence, не объявляя foreign либо missing writer joined.
// Local transports вызывают finish после positive join; history recovery передаёт явную неопределённость.
func (service *StreamOperations) recordStreamTerminal(binding managedkubernetes.Binding, terminal managedkubernetes.TerminalReceipt, handshake *managedkubernetes.HandshakeReceipt, expected string) error {
	digest, err := terminal.Digest()
	if err != nil {
		return targetrepo.ErrOperationUnavailable
	}
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(service.lifecycle), outputOwnerTimeout)
	receipt, err := service.owner.CloseKubernetesStream(closeCtx, binding, targetrepo.StreamClosing{StreamRef: terminal.Origin.StreamRef, ExpectedState: expected, TerminalState: terminal.LocalState, RemoteOutcome: terminal.RemoteOutcome, TransportJoined: terminal.TransportJoined, ObservedExitCode: terminal.ExitCode, TerminalReceiptDigest: digest, TerminalReceipt: terminal, ObservedHandshake: handshake})
	closeErr := closeCtx.Err()
	cancel()
	if err != nil || closeErr != nil || !validFinalStreamReceipt(receipt) || receipt.RemoteOutcome != terminal.RemoteOutcome {
		return targetrepo.ErrOperationUnavailable
	}
	result, err := managedkubernetes.BuildStreamEffectResult(terminal)
	if err != nil {
		return targetrepo.ErrOperationUnavailable
	}
	defer clear(result.ResultJSON)
	complete, finish := context.WithTimeout(context.WithoutCancel(service.lifecycle), outputOwnerTimeout)
	ack, err := service.owner.CompleteKubernetesEffect(complete, binding, targetrepo.EffectCompletion{EffectRef: terminal.Origin.EffectRef, EffectGeneration: terminal.Origin.EffectGeneration, ChildInputDigest: terminal.Origin.ChildInputDigest, StreamRef: terminal.Origin.StreamRef, Outcome: result.Outcome, ReasonCode: result.ReasonCode, ResultJSON: result.ResultJSON})
	completeErr := complete.Err()
	finish()
	if err != nil || completeErr != nil || ack.SubmissionDigest != result.ResultDigest {
		return targetrepo.ErrOperationUnavailable
	}
	if ack.Applied {
		if ack.ResultDigest != result.ResultDigest || ack.State != result.Outcome {
			return targetrepo.ErrOperationUnavailable
		}
	} else if ack.State != "CANCELLED" || ack.ResultDigest != resourceResultDigest([]byte("{}")) || !ack.AuthorizedUntil.IsZero() {
		return targetrepo.ErrOperationUnavailable
	}
	return nil
}

// closeFailedStream reports failed opening evidence only after a positive transport join.
// Failed joins latch dependency retention and never manufacture a joined terminal receipt.
// closeFailedStream сообщает failed opening evidence лишь после подтверждённого join transport.
// Ошибка join удерживает зависимости и не порождает вымышленную joined terminal receipt.
func (service *StreamOperations) closeFailedStream(binding managedkubernetes.Binding, transport targetrepo.LiveStream, terminal managedkubernetes.TerminalReceipt, handshake *managedkubernetes.HandshakeReceipt, expected string) {
	if transport != nil && transport.Close() != nil {
		service.unjoinedTransport.Store(true)
		return
	}
	_ = service.finish(binding, terminal, handshake, expected)
}

// streamChildProtocol distinguishes API log receipts from privileged node transport receipts.
// streamChildProtocol различает API log receipts и privileged node transport receipts.
func streamChildProtocol(child targetrepo.OperationChild) string {
	if (child.Operation == "kubernetes.watch" || child.Operation == "kubernetes.watch.all-namespaces") && child.NodeTransport == nil {
		return managedkubernetes.HTTPWatchV1
	}
	if child.Operation == "kubernetes.logs" && child.NodeTransport == nil {
		return managedkubernetes.HTTPLogsV1
	}
	if child.NodeTransport != nil {
		return string(child.NodeTransport.Protocol)
	}
	return ""
}

// streamReconcileDeadline supplies a bounded query envelope; authoritative history retains its original deadline unchanged.
// streamReconcileDeadline задаёт bounded query envelope; authoritative history сохраняет original deadline без изменений.
func streamReconcileDeadline(snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild) time.Time {
	if child.NodeTransport != nil {
		return child.NodeTransport.ExpiresAt
	}
	return snapshot.ExpiresAt
}
