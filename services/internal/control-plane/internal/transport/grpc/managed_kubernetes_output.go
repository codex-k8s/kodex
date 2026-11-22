package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// AcceptManagedKubernetesOutputFrame forwards only the bounded exact frame and original binding.
// Namespace, object locator, actor, artifact and delivery TTL are never caller-selected.
// AcceptManagedKubernetesOutputFrame передаёт лишь bounded exact frame и original binding.
// Namespace/object locator/actor/artifact/delivery TTL не назначаются caller.
func (server *Server) AcceptManagedKubernetesOutputFrame(ctx context.Context, request *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
	if request != nil {
		defer clear(request.Payload)
	}
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_AcceptManagedKubernetesOutputFrame_FullMethodName, func(p value.Principal) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		lookup := command.ManagedKubernetesOutputFrameLookup{StreamRef: request.GetStreamRef(), Sequence: request.GetSequence(), DeliveryOrdinal: request.GetDeliveryOrdinal()}
		result, err := server.service.AcceptManagedKubernetesOutputFrame(ctx, p, binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: lookup, Channel: request.GetChannel(), ByteCount: request.GetByteCount(), PayloadDigest: request.GetPayloadDigest(), Payload: request.GetPayload()})
		if err != nil {
			return nil, err
		}
		receipt := managedKubernetesOutputReceiptToProto(result, binding, lookup)
		if receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
	})
}

// GetManagedKubernetesOutputFrame returns only the stored tuple and original credit for one lost ACK.
// It cannot materialize another version, return payload or turn a historical receipt into new credit.
// GetManagedKubernetesOutputFrame возвращает лишь stored tuple и original credit после lost ACK.
// Не создаёт новую version, не выдаёт payload и не превращает historical receipt в new credit.
func (server *Server) GetManagedKubernetesOutputFrame(ctx context.Context, request *cp.GetManagedKubernetesOutputFrameRequest) (*cp.GetManagedKubernetesOutputFrameResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_GetManagedKubernetesOutputFrame_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesOutputFrameResponse, error) {
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		lookup := command.ManagedKubernetesOutputFrameLookup{StreamRef: request.GetStreamRef(), Sequence: request.GetSequence(), DeliveryOrdinal: request.GetDeliveryOrdinal()}
		result, err := server.service.GetManagedKubernetesOutputFrame(ctx, p, binding, lookup)
		if err != nil {
			return nil, err
		}
		receipt := managedKubernetesOutputReceiptToProto(result, binding, lookup)
		if receipt == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
	})
}

// managedKubernetesOutputReceiptToProto rejects mixed lineage and non-ACCEPTED credit before projection.
// Past accepted deadlines remain past metadata during Get; freshness is not fabricated in a mapper.
// managedKubernetesOutputReceiptToProto отвергает mixed lineage и nonACCEPTED credit до projection.
// Past accepted deadline остаётся past metadata при Get; mapper не выдумывает freshness.
func managedKubernetesOutputReceiptToProto(result entity.ManagedKubernetesOutputFrameReceipt, binding value.ManagedKubernetesBinding, lookup command.ManagedKubernetesOutputFrameLookup) *cp.ManagedKubernetesOutputFrameReceipt {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || !lookup.Valid() || result.InvocationRef != binding.InvocationRef || result.Generation != binding.Generation || result.BindingDigest != digest || result.StreamRef != lookup.StreamRef || result.Sequence != lookup.Sequence || result.DeliveryOrdinal != lookup.DeliveryOrdinal || !strings.HasPrefix(result.Ref, "kof_") || len(result.Ref) > 96 || !managedkubernetes.ValidLocator(result.Ref) || !result.State.Known() || !managedkubernetes.ValidChannel(result.Channel) || result.Channel == "STDIN" || result.Channel == "TERMINAL_RESIZE" || !managedkubernetes.ValidDigest(result.PayloadDigest) || result.ByteCount == 0 || result.ByteCount > managedkubernetes.MaximumFrameBytes {
		return nil
	}
	state, known := cp.ManagedKubernetesOutputFrameState_value["MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_"+string(result.State)]
	if !known || state == 0 {
		return nil
	}
	receipt := &cp.ManagedKubernetesOutputFrameReceipt{Ref: result.Ref, InvocationRef: result.InvocationRef, StreamRef: result.StreamRef, Generation: result.Generation, Sequence: result.Sequence, Channel: result.Channel, PayloadDigest: result.PayloadDigest, ByteCount: result.ByteCount, DeliveryOrdinal: result.DeliveryOrdinal, State: cp.ManagedKubernetesOutputFrameState(state), BindingDigest: result.BindingDigest}
	if result.State == enum.KubernetesOutputAccepted {
		receipt.AuthorizedUntil = managedKubernetesTimestamp(result.AuthorizedUntil)
		if receipt.AuthorizedUntil == nil {
			return nil
		}
	} else if !result.AuthorizedUntil.IsZero() {
		return nil
	}
	return receipt
}
