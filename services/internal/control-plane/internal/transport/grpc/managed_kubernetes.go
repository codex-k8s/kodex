package grpc

import (
	"context"
	"encoding/json"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ResolveManagedKubernetesOperation returns only current broker-private owner metadata.
// The request supplies claim locators, never actor, target configuration or credentials.
// ResolveManagedKubernetesOperation возвращает только current broker-private owner metadata.
// Request передаёт claim locators без actor, target configuration либо credentials.
func (server *Server) ResolveManagedKubernetesOperation(ctx context.Context, request *cp.ResolveManagedKubernetesOperationRequest) (*cp.ResolveManagedKubernetesOperationResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_ResolveManagedKubernetesOperation_FullMethodName, func(p value.Principal) (*cp.ResolveManagedKubernetesOperationResponse, error) {
		if request.GetCleanupOnly() {
			proof, err := server.service.ResolveManagedKubernetesCleanup(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), request.GetStreamRef())
			if err != nil {
				return nil, err
			}
			receipt := managedKubernetesCleanupToProto(proof, managedKubernetesBindingFromProto(request.GetBinding()), request.GetStreamRef())
			if receipt == nil {
				return nil, errs.ErrUnavailable
			}
			return &cp.ResolveManagedKubernetesOperationResponse{CleanupReceipt: receipt}, nil
		}
		result, err := server.service.ResolveManagedKubernetesOperation(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), request.GetStreamRef())
		if err != nil {
			return nil, err
		}
		expires := managedKubernetesTimestamp(result.ExpiresAt)
		if expires == nil || !managedkubernetes.ValidDigest(result.Digest) || !managedkubernetes.ValidDigest(result.SubjectDigest) || result.IdentityGeneration < 1 || !managedKubernetesSnapshotAddressValid(result) {
			return nil, errs.ErrUnavailable
		}
		identity := managedKubernetesIdentityToProto(result.IdentityLocator, result.IdentityRef, result.Target.Ref)
		if identity == nil {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ResolveManagedKubernetesOperationResponse{SnapshotDigest: result.Digest, Operation: result.Operation,
			Risk: result.Risk, ApprovalPolicy: result.ApprovalPolicy, IdentityRef: result.IdentityRef,
			IdentityGeneration: result.IdentityGeneration, SubjectDigest: result.SubjectDigest,
			Target: &cp.ManagedKubernetesTargetSnapshot{TargetRef: result.Target.Ref, Revision: result.Target.Revision,
				ServerOrigin: result.Target.ServerOrigin, TlsServerName: result.Target.TLSServerName,
				CertificateAuthoritySha256: result.Target.CertificateAuthoritySHA256, AddressIps: append([]string(nil), result.Target.AddressIPs...),
				EmbeddedContext: result.Target.EmbeddedContext, ClusterUid: result.Target.ClusterUID,
				KubeconfigSourceOrigins: append([]string(nil), result.Target.KubeconfigSourceOrigins...)},
			IdentityLocator:     identity,
			NormalizedInputJson: append([]byte(nil), result.NormalizedInputJSON...),
			ExpiresAt:           expires, ReconcileOnly: result.ReconcileOnly, PlanRef: result.PlanRef, PlanRevision: result.PlanRevision, PlanDigest: result.PlanDigest}
		response.ProtectedNamespaces = append([]string(nil), result.ProtectedNamespaces...)
		if result.ResourcePolicy != nil {
			raw, err := json.Marshal(result.ResourcePolicy)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			_, digest, err := managedkubernetes.DecodeResourcePolicy(raw)
			if err != nil || digest != result.ResourcePolicySHA256 {
				return nil, errs.ErrUnavailable
			}
			response.Target.ResourcePolicyJson, response.Target.ResourcePolicySha256 = raw, digest
		} else if result.ResourcePolicySHA256 != "" {
			return nil, errs.ErrUnavailable
		}
		if result.Discovery != nil {
			if result.Resource != (value.ManagedKubernetesResourceAddress{}) {
				return nil, errs.ErrUnavailable
			}
			response.Discovery = managedKubernetesDiscoveryToProto(*result.Discovery)
			if response.Discovery == nil {
				return nil, errs.ErrUnavailable
			}
		} else if result.Resource != (value.ManagedKubernetesResourceAddress{}) {
			response.Resource = managedKubernetesResourceToProto(result.Resource)
		}
		if result.NodeTransport != nil {
			response.NodeTransport = managedKubernetesNodeToProto(*result.NodeTransport)
			if response.NodeTransport == nil {
				return nil, errs.ErrUnavailable
			}
		}
		return response, nil
	})
}

// PrepareManagedKubernetesChildren submits scoped discovery to the owner and returns its assigned
// immutable child set. Candidate metadata never selects actor, credential or a child operation.
// PrepareManagedKubernetesChildren передаёт scoped discovery владельцу и возвращает назначенный
// immutable child set. Candidate metadata не выбирает actor, credential либо child operation.
func (server *Server) PrepareManagedKubernetesChildren(ctx context.Context, request *cp.PrepareManagedKubernetesChildrenRequest) (*cp.PrepareManagedKubernetesChildrenResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	input := command.ManagedKubernetesPrepareChildren{SnapshotDigest: request.GetSnapshotDigest(), ProposalDigest: request.GetProposalDigest()}
	for _, candidate := range request.GetCandidates() {
		entry := command.ManagedKubernetesChildCandidate{ObservationState: managedKubernetesObservationFromProto(candidate.GetObservationState()), Resource: managedKubernetesResourceFromProto(candidate.GetResource()),
			ObservedResourceDigest: candidate.GetObservedResourceDigest(), RegisteredSchemaDigest: candidate.GetRegisteredSchemaDigest(), NamespaceUID: candidate.GetNamespaceUid()}
		if witness := candidate.GetPodWitness(); witness != nil {
			entry.PodWitness = &managedkubernetes.PodNodeWitness{NodeName: witness.GetNodeName(), NodeUID: witness.GetNodeUid(), Container: witness.GetContainer()}
		}
		input.Candidates = append(input.Candidates, entry)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_PrepareManagedKubernetesChildren_FullMethodName, func(p value.Principal) (*cp.PrepareManagedKubernetesChildrenResponse, error) {
		result, err := server.service.PrepareManagedKubernetesChildren(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), input)
		if err != nil {
			return nil, err
		}
		response := ManagedKubernetesPlanToProto(result)
		if response == nil {
			return nil, errs.ErrUnavailable
		}
		return response, nil
	})
}

// BeginManagedKubernetesEffect delegates the one-attempt credit decision to the owner transaction.
// Replayed responses preserve the original admission and cannot refresh a historical expiry.
// BeginManagedKubernetesEffect передаёт решение о one-attempt credit в owner transaction.
// Replayed responses сохраняют original admission без refresh historical expiry.
func (server *Server) BeginManagedKubernetesEffect(ctx context.Context, request *cp.BeginManagedKubernetesEffectRequest) (*cp.BeginManagedKubernetesEffectResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_BeginManagedKubernetesEffect_FullMethodName, func(p value.Principal) (*cp.BeginManagedKubernetesEffectResponse, error) {
		result, err := server.service.BeginManagedKubernetesEffect(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesBeginEffect{
			PlanRef: request.GetPlanRef(), PlanRevision: request.GetPlanRevision(), PlanDigest: request.GetPlanDigest(), ChildRef: request.GetChildRef(), ChildInputDigest: request.GetChildInputDigest(), FileSourceRef: request.GetFileSourceRef(), FileSourceVersion: request.GetFileSourceVersion()})
		if err != nil {
			return nil, err
		}
		expires := managedKubernetesTimestamp(result.ExpiresAt)
		if !managedkubernetes.ValidLocator(result.Ref) || !managedkubernetes.ValidDigest(result.InputDigest) || result.Generation < 1 || expires == nil {
			return nil, errs.ErrUnavailable
		}
		response := &cp.BeginManagedKubernetesEffectResponse{EffectRef: result.Ref, EffectGeneration: result.Generation,
			ChildInputDigest: result.InputDigest, ExpiresAt: expires, ReconcileOnly: result.ReconcileOnly}
		if result.Stream != nil {
			response.Stream = managedKubernetesStreamGrantToProto(*result.Stream)
			if response.Stream == nil {
				return nil, errs.ErrUnavailable
			}
		}
		if result.History != nil {
			if !result.ReconcileOnly || result.Stream != nil {
				return nil, errs.ErrUnavailable
			}
			response.StreamHistory = managedKubernetesHistoryToProto(result.History)
			if response.StreamHistory == nil || !managedKubernetesHistoryMatches(result.History, result.Ref, result.Generation, result.InputDigest, managedKubernetesBindingFromProto(request.GetBinding())) {
				return nil, errs.ErrUnavailable
			}
		}
		if result.AcceptedResult != nil {
			if !result.ReconcileOnly {
				return nil, errs.ErrUnavailable
			}
			response.AcceptedResult = managedKubernetesAcceptedResultToProto(result.AcceptedResult, result.ExpiresAt, time.Now().UTC())
			if response.AcceptedResult == nil {
				return nil, errs.ErrUnavailable
			}
		}

		return response, nil
	})
}

// OpenManagedKubernetesStream records only a matching first handshake, never a new execution.
// OpenManagedKubernetesStream фиксирует только matching first handshake без нового execution.
func (server *Server) OpenManagedKubernetesStream(ctx context.Context, request *cp.OpenManagedKubernetesStreamRequest) (*cp.OpenManagedKubernetesStreamResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_OpenManagedKubernetesStream_FullMethodName, func(p value.Principal) (*cp.OpenManagedKubernetesStreamResponse, error) {
		result, err := server.service.OpenManagedKubernetesStream(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesOpenStream{
			EffectRef: request.GetEffectRef(), EffectGeneration: request.GetEffectGeneration(), StreamRef: request.GetStreamRef(), HandshakeReceiptDigest: request.GetHandshakeReceiptDigest(), HandshakeReceipt: managedKubernetesHandshakeFromProto(request.GetHandshakeReceipt())})
		if err != nil {
			return nil, err
		}
		grant := managedKubernetesStreamGrantToProto(result)
		if grant == nil || grant.GetStreamRef() != request.GetStreamRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.OpenManagedKubernetesStreamResponse{Stream: grant}, nil
	})
}

// CompleteManagedKubernetesEffect forwards only a registered observation and bounded safe result.
// Late receipts may retain evidence but cannot revive an invocation, stream, lease or grant.
// CompleteManagedKubernetesEffect передаёт registered observation и bounded safe result.
// Late receipts могут сохранить evidence, не восстанавливая invocation, stream, lease либо grant.
func (server *Server) CompleteManagedKubernetesEffect(ctx context.Context, request *cp.CompleteManagedKubernetesEffectRequest) (*cp.CompleteManagedKubernetesEffectResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesEffect_FullMethodName, func(p value.Principal) (*cp.CompleteManagedKubernetesEffectResponse, error) {
		result, err := server.service.CompleteManagedKubernetesEffect(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesCompleteEffect{
			EffectRef: request.GetEffectRef(), EffectGeneration: request.GetEffectGeneration(), ChildInputDigest: request.GetChildInputDigest(),
			Outcome: managedKubernetesOutcomeFromProto(request.GetOutcome()), ResultJSON: append([]byte(nil), request.GetResultJson()...),
			ResultDigest: request.GetResultDigest(), ReasonCode: request.GetReasonCode(), StreamRef: request.GetStreamRef()})
		if err != nil {
			return nil, err
		}
		response := managedKubernetesEffectReceiptToProto(result, request.GetResultDigest(), managedKubernetesOutcomeFromProto(request.GetOutcome()), time.Now().UTC())
		if response == nil {
			return nil, errs.ErrUnavailable
		}
		return response, nil
	})
}

// BeginManagedKubernetesFrame requests exact sequence credit; no payload or raw channel command enters CP.
// BeginManagedKubernetesFrame запрашивает exact sequence credit без payload либо raw channel command в CP.
func (server *Server) BeginManagedKubernetesFrame(ctx context.Context, request *cp.BeginManagedKubernetesFrameRequest) (*cp.BeginManagedKubernetesFrameResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_BeginManagedKubernetesFrame_FullMethodName, func(p value.Principal) (*cp.BeginManagedKubernetesFrameResponse, error) {
		var nativeUntil time.Time
		if witness := request.GetNativeAuthorizedUntil(); witness != nil {
			if witness.CheckValid() != nil {
				return nil, errs.ErrInvalid
			}
			nativeUntil = witness.AsTime()
		}
		forward, valid := managedKubernetesPortForwardWitnessFromProto(request.GetPortForwardConnection(), request.GetByteCount())
		if !valid || forward != nil && (request.GetDirection() != cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_OUTPUT || request.GetChannel() != "PORT_FORWARD" || request.GetEndOfInput()) || forward == nil && request.GetDirection() == cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_OUTPUT && request.GetChannel() == "PORT_FORWARD" {
			return nil, errs.ErrInvalid
		}
		result, err := server.service.BeginManagedKubernetesFrame(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesBeginFrame{
			StreamRef: request.GetStreamRef(), Direction: managedKubernetesDirectionFromProto(request.GetDirection()), Sequence: request.GetSequence(),
			Channel: request.GetChannel(), ByteCount: request.GetByteCount(), PayloadDigest: request.GetPayloadDigest(), EndOfInput: request.GetEndOfInput(), NativeAuthorizedUntil: nativeUntil, PortForward: forward})
		if err != nil {
			return nil, err
		}
		if result.Disposition == enum.KubernetesFrameClosed && forward == nil {
			return nil, errs.ErrUnavailable
		}
		response := managedKubernetesFrameAdmissionToProto(result, managedKubernetesDirectionFromProto(request.GetDirection()))
		if response == nil {
			return nil, errs.ErrUnavailable
		}
		return response, nil
	})
}

// CompleteManagedKubernetesFrame returns only the stored exact ACK cursor; a mismatch fails closed.
// CompleteManagedKubernetesFrame возвращает stored exact ACK cursor; mismatch закрыто отклоняется.
func (server *Server) CompleteManagedKubernetesFrame(ctx context.Context, request *cp.CompleteManagedKubernetesFrameRequest) (*cp.CompleteManagedKubernetesFrameResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesFrame_FullMethodName, func(p value.Principal) (*cp.CompleteManagedKubernetesFrameResponse, error) {
		ack, err := server.service.CompleteManagedKubernetesFrame(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesCompleteFrame{
			StreamRef: request.GetStreamRef(), Direction: managedKubernetesDirectionFromProto(request.GetDirection()), Sequence: request.GetSequence(), PayloadDigest: request.GetPayloadDigest()})
		if err != nil {
			return nil, err
		}
		if ack != request.GetSequence() {
			return nil, errs.ErrUnavailable
		}
		return &cp.CompleteManagedKubernetesFrameResponse{AcknowledgedSequence: ack}, nil
	})
}

// CloseManagedKubernetesStream preserves the owner's terminal decision and separate remote outcome.
// TransportJoined never substitutes for a proven remote exit or restores a canceled grant.
// CloseManagedKubernetesStream сохраняет owner terminal decision и отдельный remote outcome.
// TransportJoined не заменяет proven remote exit и не восстанавливает canceled grant.
func (server *Server) CloseManagedKubernetesStream(ctx context.Context, request *cp.CloseManagedKubernetesStreamRequest) (*cp.CloseManagedKubernetesStreamResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CloseManagedKubernetesStream_FullMethodName, func(p value.Principal) (*cp.CloseManagedKubernetesStreamResponse, error) {
		result, err := server.service.CloseManagedKubernetesStream(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()), command.ManagedKubernetesCloseStream{
			StreamRef: request.GetStreamRef(), ExpectedState: managedKubernetesStateFromProto(request.GetExpectedState()), TerminalState: managedKubernetesStateFromProto(request.GetTerminalState()),
			RemoteOutcome: managedKubernetesOutcomeFromProto(request.GetRemoteOutcome()), TransportJoined: request.GetTransportJoined(),
			ObservedExitCode: request.ObservedExitCode, TerminalReceiptDigest: request.GetTerminalReceiptDigest(), TerminalReceipt: managedKubernetesTerminalFromProto(request.GetTerminalReceipt()), ObservedHandshake: managedKubernetesOptionalHandshakeFromProto(request.GetObservedHandshake())})
		if err != nil {
			return nil, err
		}
		state, remote := managedKubernetesStateToProto(result.State), managedKubernetesOutcomeToProto(result.RemoteOutcome)
		if !result.State.Terminal() || state == cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_UNSPECIFIED ||
			remote == cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNSPECIFIED {
			return nil, errs.ErrUnavailable
		}
		return &cp.CloseManagedKubernetesStreamResponse{State: state, RemoteOutcome: remote}, nil
	})
}

// CheckManagedKubernetesOwnerReadiness follows the registered exact method and owner repository path.
// It proves neither a selected account's RBAC nor a kubelet stream protocol's availability.
// CheckManagedKubernetesOwnerReadiness использует registered exact method и owner repository path.
// Не доказывает selected account RBAC либо доступность kubelet stream protocol.
func (server *Server) CheckManagedKubernetesOwnerReadiness(ctx context.Context, request *cp.CheckManagedKubernetesOwnerReadinessRequest) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	if !managedKubernetesWireValid(request) || (request.GetDeliveryContractOnly() && request.GetIdentityHealthOnly()) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CheckManagedKubernetesOwnerReadiness_FullMethodName, func(p value.Principal) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
		return server.managedKubernetesReadiness(ctx, p, request)
	})
}

// managedKubernetesReadiness selects a closed read-only readiness profile after the exact-method principal entry point. Identity health retains all core checks while full execution additionally requires OUTPUT; gateway delivery proof cannot assume broker authority.
// managedKubernetesReadiness выбирает закрытый read-only readiness profile после exact-method principal entry point. Identity health сохраняет все core checks; full execution дополнительно требует OUTPUT, а gateway delivery proof не получает broker authority.
func (server *Server) managedKubernetesReadiness(ctx context.Context, p value.Principal, request *cp.CheckManagedKubernetesOwnerReadinessRequest) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	if request == nil || (request.GetDeliveryContractOnly() && request.GetIdentityHealthOnly()) {
		return nil, errs.ErrInvalid
	}
	revision, err := server.service.CheckManagedKubernetesDeliveryContract(ctx, p)
	if err != nil {
		return nil, err
	}
	if request.GetDeliveryContractOnly() {
		return &cp.CheckManagedKubernetesOwnerReadinessResponse{DeliveryContractRevision: revision}, nil
	}
	if request.GetIdentityHealthOnly() {
		ready, err := server.service.CheckManagedKubernetesIdentityHealthReadiness(ctx, p)
		if err != nil {
			return nil, err
		}
		return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: ready, DeliveryContractRevision: revision}, nil
	}
	ready, err := server.service.CheckManagedKubernetesOwnerReadiness(ctx, p)
	if err != nil {
		return nil, err
	}
	outputRevision, err := server.service.CheckManagedKubernetesOutputContract(ctx, p)
	if err != nil {
		return nil, err
	}
	inputRevision, err := server.service.CheckManagedKubernetesInputContract(ctx, p)
	if err != nil {
		return nil, err
	}
	fileRevision, err := server.service.CheckManagedKubernetesFileInputContract(ctx, p)
	if err != nil {
		return nil, err
	}
	return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: ready && outputRevision == managedkubernetes.OutputContractRevision && inputRevision == managedkubernetes.InputContractRevision && fileRevision == managedkubernetes.FileInputContractRevision, DeliveryContractRevision: revision, OutputContractRevision: outputRevision, InputContractRevision: inputRevision, FileInputContractRevision: fileRevision}, nil
}

// managedKubernetesFrameAdmissionToProto separates SEND credit from metadata-only acknowledgement/uncertainty.
// A non-SEND has no deadline or ordinal; only OUTPUT SEND can carry a durable delivery ordinal.
// managedKubernetesFrameAdmissionToProto отделяет SEND credit от metadata-only acknowledgement/uncertainty.
// NonSEND не имеет deadline/ordinal; только OUTPUT SEND несёт durable delivery ordinal.
func managedKubernetesFrameAdmissionToProto(result entity.ManagedKubernetesFrameAdmission, direction enum.ManagedKubernetesFrameDirection) *cp.BeginManagedKubernetesFrameResponse {
	response := &cp.BeginManagedKubernetesFrameResponse{}
	if result.PortForwardRetention != nil {
		if result.Disposition != enum.KubernetesFrameSend || direction != enum.KubernetesFrameInput || result.PortForwardRetention.Validate(managedkubernetes.MaximumForwardPorts, nil) != nil {
			return nil
		}
		for _, item := range result.PortForwardRetention.Connections {
			response.PortForwardRetention = append(response.PortForwardRetention, &cp.ManagedKubernetesPortForwardRetainedConnection{ConnectionRef: item.ConnectionRef, Generation: int64(item.Generation), Slot: uint32(item.Slot)})
		}
	}
	switch result.Disposition {
	case enum.KubernetesFrameSend:
		response.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_SEND
		response.ExpiresAt = managedKubernetesTimestamp(result.ExpiresAt)
		if response.ExpiresAt == nil {
			return nil
		}
		switch direction {
		case enum.KubernetesFrameOutput:
			if result.DeliveryOrdinal < 1 || result.DeliveryOrdinal > managedkubernetes.MaximumOutputDeliveries {
				return nil
			}
		case enum.KubernetesFrameInput:
			if result.DeliveryOrdinal != 0 {
				return nil
			}
		default:
			return nil
		}
		response.DeliveryOrdinal = result.DeliveryOrdinal
	case enum.KubernetesFrameAcknowledged:
		response.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_ACKNOWLEDGED
	case enum.KubernetesFrameUnknown:
		response.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_UNKNOWN
	case enum.KubernetesFrameClosed:
		if direction != enum.KubernetesFrameOutput {
			return nil
		}
		response.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_CLOSED
	default:
		return nil
	}
	if result.Disposition != enum.KubernetesFrameSend && (!result.ExpiresAt.IsZero() || result.DeliveryOrdinal != 0) {
		return nil
	}
	return response
}
