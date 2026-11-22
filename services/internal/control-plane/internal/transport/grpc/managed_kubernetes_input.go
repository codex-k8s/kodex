package grpc

import (
	"bytes"
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimManagedKubernetesInputFrame returns bytes only for a validated first owner delivery.
// It creates no retry, payload readback, generic artifact path or actor selected by the request.
// ClaimManagedKubernetesInputFrame возвращает bytes лишь для validated first owner delivery.
// Retry/payload readback/generic artifact path и actor из request не создаются.
func (server *Server) ClaimManagedKubernetesInputFrame(ctx context.Context, request *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName, func(p value.Principal) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
		binding := managedKubernetesBindingFromProto(request.GetBinding())
		result, err := server.service.ClaimManagedKubernetesInputFrame(ctx, p, binding, request.GetStreamRef())
		if result.Frame != nil {
			defer clear(result.Frame.Payload)
		}
		if err != nil {
			return nil, err
		}
		response := managedKubernetesInputAdmissionToProto(result, binding, request.GetStreamRef(), time.Now())
		if response == nil {
			return nil, errs.ErrUnavailable
		}
		return response, nil
	})
}

// managedKubernetesInputAdmissionToProto rejects mixed lineage and hidden bytes on non-delivery variants.
// It transfers one owned response copy; the domain buffer is cleared by the enclosing handler.
// managedKubernetesInputAdmissionToProto отвергает mixed lineage и bytes на non-delivery variants.
// Передаёт одну owned response copy; domain buffer очищает enclosing handler.
func managedKubernetesInputAdmissionToProto(result entity.ManagedKubernetesInputFrameAdmission, binding value.ManagedKubernetesBinding, streamRef string, now time.Time) *cp.ClaimManagedKubernetesInputFrameResponse {
	if !result.Disposition.Known() {
		return nil
	}
	value, exists := cp.ManagedKubernetesInputFrameDisposition_value["MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_"+string(result.Disposition)]
	if !exists || value == 0 {
		return nil
	}
	response := &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition(value)}
	if result.Disposition != enum.KubernetesInputFrameDeliver {
		if result.Frame != nil {
			return nil
		}
		return response
	}
	frame := result.Frame
	if frame == nil || frame.Validate(binding, streamRef, now) != nil {
		return nil
	}
	until := managedKubernetesTimestamp(frame.DeliveryExpiresAt)
	if until == nil {
		return nil
	}
	response.Frame = &cp.ManagedKubernetesInputFrame{Ref: frame.Ref, InvocationRef: frame.InvocationRef, StreamRef: frame.StreamRef, Generation: frame.Generation, Sequence: frame.Sequence, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, EndOfInput: frame.EndOfInput, Payload: bytes.Clone(frame.Payload), DeliveryExpiresAt: until, BindingDigest: frame.BindingDigest}
	return response
}
