package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ReadManagedKubernetesInvocation maps a closed live runtime proof without accepting actor or target authority.
// ReadManagedKubernetesInvocation преобразует закрытое доказательство runtime без actor или target authority из запроса.
func (s *Server) ReadManagedKubernetesInvocation(ctx context.Context, req *cp.ReadManagedKubernetesInvocationRequest) (*cp.ReadManagedKubernetesInvocationResponse, error) {
	if !managedKubernetesWireValid(req) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_ReadManagedKubernetesInvocation_FullMethodName, func(p value.Principal) (*cp.ReadManagedKubernetesInvocationResponse, error) {
		v, err := s.service.ReadKubernetesRuntimeInvocation(ctx, p, query.KubernetesRuntimeRead{InvocationRef: req.GetInvocationRef(), LeaseRef: req.GetLeaseRef(), Fence: req.GetFence(), Generation: req.GetGeneration(), RuntimeInputDigest: req.GetRuntimeInputDigest()})
		if err != nil {
			return nil, err
		}
		state, ok := cp.ManagedKubernetesToolState_value["MANAGED_KUBERNETES_TOOL_STATE_"+string(v.State)]
		if !ok || state == 0 || !v.Valid() {
			return nil, errs.ErrUnavailable
		}
		return &cp.ReadManagedKubernetesInvocationResponse{InvocationRef: v.InvocationRef, State: cp.ManagedKubernetesToolState(state), ResultSummary: v.ResultSummary, SafeErrorCode: v.SafeErrorCode, EffectReceiptRef: v.EffectReceiptRef, ResultDigest: v.ResultDigest, WaitingForGate: v.WaitingForGate}, nil
	})
}
