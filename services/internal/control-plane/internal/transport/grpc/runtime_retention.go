package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) SetResourceRetentionPolicy(
	ctx context.Context,
	request *controlplanev1.SetResourceRetentionPolicyRequest,
) (*controlplanev1.SetResourceRetentionPolicyResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_SetResourceRetentionPolicy_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	policy, err := server.service.SetResourceRetentionPolicy(ctx, resource.ResourceRetentionPolicyInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExpectedVersion:         request.GetExpectedVersion(),
		PVCRetentionSeconds:     request.GetPvcRetentionSeconds(),
		ArchiveRetentionSeconds: request.GetArchiveRetentionSeconds(),
		ReasonCode:              request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.SetResourceRetentionPolicyResponse{Policy: toProtoRetentionPolicy(policy)}, nil
}

func (server *Server) RetireResourceRetentionPolicy(
	ctx context.Context,
	request *controlplanev1.RetireResourceRetentionPolicyRequest,
) (*controlplanev1.RetireResourceRetentionPolicyResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_RetireResourceRetentionPolicy_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	policy, err := server.service.RetireResourceRetentionPolicy(ctx, resource.ResourceRetentionPolicyInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExpectedVersion: request.GetExpectedVersion(), ReasonCode: request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RetireResourceRetentionPolicyResponse{Policy: toProtoRetentionPolicy(policy)}, nil
}

func (server *Server) GetResourceRetentionPolicy(
	ctx context.Context,
	_ *controlplanev1.GetResourceRetentionPolicyRequest,
) (*controlplanev1.GetResourceRetentionPolicyResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetResourceRetentionPolicy_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	policy, err := server.service.GetResourceRetentionPolicy(ctx, principal)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetResourceRetentionPolicyResponse{Policy: toProtoRetentionPolicy(policy)}, nil
}

func (server *Server) HoldRuntimeRetention(
	ctx context.Context,
	request *controlplanev1.HoldRuntimeRetentionRequest,
) (*controlplanev1.HoldRuntimeRetentionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_HoldRuntimeRetention_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	hold, err := server.service.HoldRuntimeRetention(ctx, resource.RuntimeRetentionHoldInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		SessionID: request.GetSessionId(), ExpectedSessionVersion: request.GetExpectedSessionVersion(),
		Kind: runtimeRetentionHoldKind(request.GetKind()), ReasonCode: request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.HoldRuntimeRetentionResponse{Hold: toProtoRuntimeRetentionHold(hold)}, nil
}

func (server *Server) ReleaseRuntimeRetention(
	ctx context.Context,
	request *controlplanev1.ReleaseRuntimeRetentionRequest,
) (*controlplanev1.ReleaseRuntimeRetentionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ReleaseRuntimeRetention_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	hold, err := server.service.ReleaseRuntimeRetention(ctx, resource.RuntimeRetentionHoldInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		SessionID: request.GetSessionId(), ExpectedSessionVersion: request.GetExpectedSessionVersion(),
		HoldID: request.GetHoldId(), ExpectedHoldVersion: request.GetExpectedHoldVersion(),
		ReasonCode: request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ReleaseRuntimeRetentionResponse{Hold: toProtoRuntimeRetentionHold(hold)}, nil
}

func toProtoRetentionPolicy(policy domainrepo.ResourceRetentionPolicy) *controlplanev1.ResourceRetentionPolicy {
	result := &controlplanev1.ResourceRetentionPolicy{
		PolicyId: policy.ID, Version: policy.Version,
		PvcRetentionSeconds:     policy.PVCRetentionSeconds,
		ArchiveRetentionSeconds: policy.ArchiveRetentionSeconds,
		EffectiveAt:             timestamppb.New(policy.EffectiveFrom),
	}
	if !policy.RetiredAt.IsZero() {
		result.RetiredAt = timestamppb.New(policy.RetiredAt)
	}
	return result
}

func runtimeRetentionHoldKind(kind controlplanev1.RuntimeRetentionHoldKind) string {
	switch kind {
	case controlplanev1.RuntimeRetentionHoldKind_RUNTIME_RETENTION_HOLD_KIND_MANUAL:
		return "MANUAL"
	case controlplanev1.RuntimeRetentionHoldKind_RUNTIME_RETENTION_HOLD_KIND_LEGAL:
		return "LEGAL"
	default:
		return ""
	}
}

func toProtoRuntimeRetentionHold(hold domainrepo.RuntimeRetentionHold) *controlplanev1.RuntimeRetentionHold {
	result := &controlplanev1.RuntimeRetentionHold{
		HoldId: hold.ID, SessionId: hold.SessionID, State: hold.State,
		Version: hold.Version, ReasonCode: hold.ReasonCode,
		CreatedAt: timestamppb.New(hold.CreatedAt), UpdatedAt: timestamppb.New(hold.UpdatedAt),
	}
	if hold.Kind == "MANUAL" {
		result.Kind = controlplanev1.RuntimeRetentionHoldKind_RUNTIME_RETENTION_HOLD_KIND_MANUAL
	} else if hold.Kind == "LEGAL" {
		result.Kind = controlplanev1.RuntimeRetentionHoldKind_RUNTIME_RETENTION_HOLD_KIND_LEGAL
	}
	if !hold.ReleasedAt.IsZero() && !hold.ReleasedAt.Equal(time.Unix(0, 0).UTC()) {
		result.ReleasedAt = timestamppb.New(hold.ReleasedAt)
	}
	return result
}
