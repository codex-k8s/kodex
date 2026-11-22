package grpc

import (
	"context"
	"slices"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// maximumKubernetesInputPublicSequence preserves exact JSON integers, independently from private bigint sequences.
// maximumKubernetesInputPublicSequence сохраняет exact JSON integers независимо от private bigint sequences.
const maximumKubernetesInputPublicSequence uint64 = 1<<53 - 1

// ListManagedKubernetesStreamInputs exposes a bounded exact-node surface before terminal tool receipts exist; every item remains owner-authorized independently of its locator.
// ListManagedKubernetesStreamInputs выдаёт bounded exact-node surface до terminal tool receipts; каждый item авторизуется owner независимо от locator.
func (server *Server) ListManagedKubernetesStreamInputs(ctx context.Context, request *cp.ListManagedKubernetesStreamInputsRequest) (*cp.ListManagedKubernetesStreamInputsResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesStreamInputService_ListManagedKubernetesStreamInputs_FullMethodName, func(p value.Principal) (*cp.ListManagedKubernetesStreamInputsResponse, error) {
		result, err := server.service.ListKubernetesStreamInputs(ctx, p, query.KubernetesStreamInputList{RunRef: request.GetRunRef(), NodeRef: request.GetNodeRef(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		if len(result.Items) > int(page(request.GetPage()).Size) || len(result.Next) > 2048 {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ListManagedKubernetesStreamInputsResponse{Items: make([]*cp.ManagedKubernetesStreamInputView, 0, len(result.Items)), Page: &cp.PageInfo{NextPageToken: result.Next}}
		seen := make(map[string]bool, len(result.Items))
		for _, item := range result.Items {
			view, err := kubernetesStreamInputViewToProto(item)
			if err != nil || item.RunRef != request.GetRunRef() || item.NodeRef != request.GetNodeRef() || seen[item.StreamRef] {
				return nil, errs.ErrUnavailable
			}
			seen[item.StreamRef] = true
			response.Items = append(response.Items, view)
		}
		return response, nil
	})
}

// GetManagedKubernetesStreamInput reads exact safe metadata and optional original-key submission history without recovering bytes or retrying delivery.
// GetManagedKubernetesStreamInput читает exact safe metadata и optional original-key submission history без восстановления bytes либо retry доставки.
func (server *Server) GetManagedKubernetesStreamInput(ctx context.Context, request *cp.GetManagedKubernetesStreamInputRequest) (*cp.GetManagedKubernetesStreamInputResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesStreamInputService_GetManagedKubernetesStreamInput_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesStreamInputResponse, error) {
		result, err := server.service.GetKubernetesStreamInput(ctx, p, query.KubernetesStreamInputRead{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), SubmissionIdempotencyKey: request.GetSubmissionIdempotencyKey()})
		if err != nil {
			return nil, err
		}
		view, submission, err := kubernetesStreamInputReadbackToProto(result, request.GetInvocationRef(), request.GetStreamRef())
		if err != nil || request.GetSubmissionIdempotencyKey() == "" && submission != nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesStreamInputResponse{View: view, Submission: submission}, nil
	})
}

// SubmitManagedKubernetesStreamInput transfers one private bounded payload to the owner and clears transport-owned bytes even on malformed input or a rejected call.
// Sequence, actor and source authority are absent from the request; an acknowledgement contains metadata only.
// SubmitManagedKubernetesStreamInput передаёт один private bounded payload владельцу и очищает transport-owned bytes даже при malformed input либо отказе.
// Sequence, actor и source authority отсутствуют в request; acknowledgement содержит лишь metadata.
func (server *Server) SubmitManagedKubernetesStreamInput(ctx context.Context, request *cp.SubmitManagedKubernetesStreamInputRequest) (*cp.SubmitManagedKubernetesStreamInputResponse, error) {
	if request != nil {
		defer clear(request.Payload)
	}
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesStreamInputService_SubmitManagedKubernetesStreamInput_FullMethodName, func(p value.Principal) (*cp.SubmitManagedKubernetesStreamInputResponse, error) {
		result, err := server.service.SubmitKubernetesStreamInput(ctx, p, mutation(request.GetMutation()), command.KubernetesStreamInputSubmit{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), Channel: request.GetChannel(), PayloadDigest: request.GetPayloadDigest(), Payload: request.GetPayload(), EndOfInput: request.GetEndOfInput()})
		if err != nil {
			return nil, err
		}
		view, submission, err := kubernetesStreamInputReadbackToProto(result, request.GetInvocationRef(), request.GetStreamRef())
		if err != nil || submission == nil || submission.Channel != request.GetChannel() || submission.PayloadDigest != request.GetPayloadDigest() || submission.ByteCount != uint32(len(request.GetPayload())) || submission.EndOfInput != request.GetEndOfInput() {
			return nil, errs.ErrUnavailable
		}
		return &cp.SubmitManagedKubernetesStreamInputResponse{View: view, Submission: submission}, nil
	})
}

// kubernetesInputPublicRef accepts bounded owner-issued coordinates without treating a known reference as authority.
// kubernetesInputPublicRef принимает bounded owner-issued coordinates, не считая известную reference полномочием.
func kubernetesInputPublicRef(ref, prefix string) bool {
	return len(ref) >= len(prefix)+8 && len(ref) <= 96 && strings.HasPrefix(ref, prefix) && managedkubernetes.ValidLocator(ref)
}

// kubernetesStreamInputSubmissionToProto projects immutable tuple metadata only and rejects unknown delivery states or false empty-frame receipts.
// kubernetesStreamInputSubmissionToProto выдаёт immutable tuple metadata и отвергает unknown delivery states либо ложные empty-frame receipts.
func kubernetesStreamInputSubmissionToProto(input *entity.KubernetesStreamInputSubmission, view entity.KubernetesStreamInputView) (*cp.ManagedKubernetesStreamInputSubmission, error) {
	if input == nil {
		return nil, nil
	}
	state, known := cp.ManagedKubernetesStreamInputSubmissionState_value["MANAGED_KUBERNETES_STREAM_INPUT_SUBMISSION_STATE_"+string(input.State)]
	if !known || state == 0 || !input.State.Known() || !kubernetesInputPublicRef(input.Ref, "kif_") || input.InvocationRef != view.InvocationRef || input.StreamRef != view.StreamRef || input.Generation != view.Generation || !managedkubernetes.ValidFrameSequence(input.Sequence) || input.Sequence > maximumKubernetesInputPublicSequence || !managedkubernetes.ValidDigest(input.PayloadDigest) || input.ByteCount > view.MaximumFrameBytes || input.ByteCount == 0 && !input.EndOfInput || input.ByteCount != 0 && input.EndOfInput || !slices.Contains(view.InputChannels, input.Channel) {
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesStreamInputSubmission{Ref: input.Ref, InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Generation: input.Generation, Sequence: input.Sequence, Channel: input.Channel, PayloadDigest: input.PayloadDigest, ByteCount: input.ByteCount, EndOfInput: input.EndOfInput, State: cp.ManagedKubernetesStreamInputSubmissionState(state)}, nil
}

// kubernetesStreamInputViewToProto preserves historical owner state without turning elapsed time or public channel labels into fresh input credit.
// kubernetesStreamInputViewToProto сохраняет historical owner state, не превращая elapsed time либо public channel labels в fresh input credit.
func kubernetesStreamInputViewToProto(input entity.KubernetesStreamInputView) (*cp.ManagedKubernetesStreamInputView, error) {
	state, known := cp.ManagedKubernetesOwnerStreamState_value["MANAGED_KUBERNETES_OWNER_STREAM_STATE_"+string(input.State)]
	operation, registered := integrationpackage.KubernetesOperationByKey(input.Operation)
	deadline := managedKubernetesTimestamp(input.HardDeadline)
	if !known || state == 0 || !registered || operation.Execution != integrationpackage.KubernetesStream || !kubernetesInputPublicRef(input.InvocationRef, "inv_") || !kubernetesInputPublicRef(input.StreamRef, "kst_") || !kubernetesInputPublicRef(input.RunRef, "run_") || !kubernetesInputPublicRef(input.NodeRef, "nod_") || !validMaterialPublicVersion(input.Generation) || !validMaterialPublicVersion(input.Version) || managedkubernetes.ValidateStreamInputViewResource(input.Operation, input.Namespace, input.PodName, input.Container, input.Resource) != nil || !managedkubernetes.ValidLocator(input.TargetRef) || len(input.TargetRef) > 96 || !validMaterialPublicVersion(input.TargetRevision) || !kubernetesInputPublicRef(input.ConnectionRef, "int_") || input.MaximumStagedBytes == 0 || input.MaximumStagedBytes > managedkubernetes.MaximumStagedBytes || input.MaximumStagedObjects == 0 || input.MaximumStagedObjects > managedkubernetes.MaximumStagedObjects || input.MaximumFrameBytes == 0 || input.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes || input.MaximumTotalBytes < uint64(input.MaximumFrameBytes) || input.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes || input.AcknowledgedSequence > maximumKubernetesInputPublicSequence || deadline == nil || input.ReasonCode != "" && !managedkubernetes.ValidReasonCode(input.ReasonCode) || len(input.InputChannels) > 7 || len(input.ClosedInputChannels) > 7 {
		return nil, errs.ErrUnavailable
	}
	if (input.Operation == "kubernetes.watch" || input.Operation == "kubernetes.watch.all-namespaces") && len(input.InputChannels) != 0 {
		return nil, errs.ErrUnavailable
	}
	for index, channel := range input.InputChannels {
		if !managedkubernetes.ValidChannel(channel) || channel == "STDOUT" || channel == "STDERR" || slices.Contains(input.InputChannels[:index], channel) {
			return nil, errs.ErrUnavailable
		}
	}
	for index, channel := range input.ClosedInputChannels {
		if !slices.Contains(input.InputChannels, channel) || slices.Contains(input.ClosedInputChannels[:index], channel) {
			return nil, errs.ErrUnavailable
		}
	}
	pending, err := kubernetesStreamInputSubmissionToProto(input.Pending, input)
	if err != nil {
		return nil, err
	}
	return &cp.ManagedKubernetesStreamInputView{Resource: managedKubernetesResourceToProto(input.Resource), TargetRef: input.TargetRef, TargetRevision: input.TargetRevision, ConnectionRef: input.ConnectionRef, InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, RunRef: input.RunRef, NodeRef: input.NodeRef, Generation: input.Generation, Operation: input.Operation, Namespace: input.Namespace, PodName: input.PodName, Container: input.Container, State: cp.ManagedKubernetesOwnerStreamState(state), Version: input.Version, InputChannels: slices.Clone(input.InputChannels), ClosedInputChannels: slices.Clone(input.ClosedInputChannels), MaximumFrameBytes: input.MaximumFrameBytes, MaximumTotalBytes: input.MaximumTotalBytes, MaximumStagedBytes: input.MaximumStagedBytes, MaximumStagedObjects: input.MaximumStagedObjects, AcknowledgedSequence: input.AcknowledgedSequence, Pending: pending, HardDeadline: deadline, ReasonCode: input.ReasonCode}, nil
}

// kubernetesStreamInputReadbackToProto rejects mixed invocation/stream/generation metadata before either a normal read or lost-ACK readback is returned.
// kubernetesStreamInputReadbackToProto отвергает mixed invocation/stream/generation metadata до normal read либо lost-ACK readback.
func kubernetesStreamInputReadbackToProto(input entity.KubernetesStreamInputReadback, invocation, stream string) (*cp.ManagedKubernetesStreamInputView, *cp.ManagedKubernetesStreamInputSubmission, error) {
	view, err := kubernetesStreamInputViewToProto(input.View)
	if err != nil || view.InvocationRef != invocation || view.StreamRef != stream {
		return nil, nil, errs.ErrUnavailable
	}
	submission, err := kubernetesStreamInputSubmissionToProto(input.Submission, input.View)
	return view, submission, err
}

// StopManagedKubernetesStream requests owner closure of the exact original tool, preserving immutable remote facts and avoiding whole-Run cancellation or fabricated worker fences.
// StopManagedKubernetesStream запрашивает owner closure exact original tool с сохранением immutable remote facts, без отмены всего Run либо выдуманного worker fence.
func (server *Server) StopManagedKubernetesStream(ctx context.Context, request *cp.StopManagedKubernetesStreamRequest) (*cp.StopManagedKubernetesStreamResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesStreamInputService_StopManagedKubernetesStream_FullMethodName, func(p value.Principal) (*cp.StopManagedKubernetesStreamResponse, error) {
		result, err := server.service.StopKubernetesStream(ctx, p, mutation(request.GetMutation()), command.KubernetesStreamInputStop{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef()})
		if err != nil {
			return nil, err
		}
		view, err := kubernetesStreamInputViewToProto(result)
		if err != nil || !result.State.Terminal() || view.InvocationRef != request.GetInvocationRef() || view.StreamRef != request.GetStreamRef() {
			return nil, errs.ErrUnavailable
		}
		return &cp.StopManagedKubernetesStreamResponse{View: view}, nil
	})
}
