package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ReadManagedSSHInvocation maps a closed live runtime proof without accepting actor or target authority.
// ReadManagedSSHInvocation преобразует закрытое доказательство runtime без actor или target authority из запроса.
func (s *Server) ReadManagedSSHInvocation(ctx context.Context, req *cp.ReadManagedSSHInvocationRequest) (*cp.ReadManagedSSHInvocationResponse, error) {
	if req == nil || len(req.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedSSHOwnerService_ReadManagedSSHInvocation_FullMethodName, func(p value.Principal) (*cp.ReadManagedSSHInvocationResponse, error) {
		v, err := s.service.ReadSSHRuntimeInvocation(ctx, p, query.SSHRuntimeRead{InvocationRef: req.GetInvocationRef(), LeaseRef: req.GetLeaseRef(), Fence: req.GetFence(), Generation: req.GetGeneration(), RuntimeInputDigest: req.GetRuntimeInputDigest()})
		if err != nil {
			return nil, err
		}
		state, ok := cp.ManagedSSHInvocationState_value["MANAGED_SSH_INVOCATION_STATE_"+string(v.State)]
		if !ok || state == 0 || !v.Valid() {
			return nil, errs.ErrUnavailable
		}
		return &cp.ReadManagedSSHInvocationResponse{InvocationRef: v.InvocationRef, State: cp.ManagedSSHInvocationState(state), ResultSummary: v.ResultSummary, SafeErrorCode: v.SafeErrorCode, EffectReceiptRef: v.EffectReceiptRef, ResultDigest: v.ResultDigest, WaitingForGate: v.WaitingForGate}, nil
	})
}
