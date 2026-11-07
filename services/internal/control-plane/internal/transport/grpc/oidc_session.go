package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResolveOIDCSession returns a typed current self binding only after the real bearer and current owner eligibility have passed transport admission.
// ResolveOIDCSession возвращает typed current self binding лишь после transport admission реального bearer и current owner eligibility.
func (server *Server) ResolveOIDCSession(ctx context.Context, request *cp.ResolveOIDCSessionRequest) (*cp.ResolveOIDCSessionResponse, error) {
	return queryResponse(ctx, cp.OIDCSessionService_ResolveOIDCSession_FullMethodName, func(p value.Principal) (*cp.ResolveOIDCSessionResponse, error) {
		if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
			return nil, errs.ErrInvalid
		}
		binding, err := server.service.StandardOIDCSession(ctx, p, false)
		if err != nil {
			return nil, err
		}
		return &cp.ResolveOIDCSessionResponse{Binding: castOIDCSessionBinding(binding)}, nil
	})
}

// GetOIDCSessionClosure reads only the independently authenticated caller's existing tombstone and never creates application access.
// GetOIDCSessionClosure читает лишь existing tombstone independently authenticated caller и не создаёт application access.
func (server *Server) GetOIDCSessionClosure(ctx context.Context, request *cp.GetOIDCSessionClosureRequest) (*cp.GetOIDCSessionClosureResponse, error) {
	return queryResponse(ctx, cp.OIDCSessionService_GetOIDCSessionClosure_FullMethodName, func(p value.Principal) (*cp.GetOIDCSessionClosureResponse, error) {
		if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
			return nil, errs.ErrInvalid
		}
		binding, err := server.service.StandardOIDCSession(ctx, p, true)
		if err != nil {
			return nil, err
		}
		return &cp.GetOIDCSessionClosureResponse{Binding: castOIDCSessionBinding(binding)}, nil
	})
}

// RevokeOIDCSession irreversibly closes the exact authenticated self tuple and returns its read-back tombstone for lost-response reconciliation.
// RevokeOIDCSession необратимо закрывает exact authenticated self tuple и возвращает read-back tombstone для сверки потерянного ответа.
func (server *Server) RevokeOIDCSession(ctx context.Context, request *cp.RevokeOIDCSessionRequest) (*cp.RevokeOIDCSessionResponse, error) {
	return queryResponse(ctx, cp.OIDCSessionService_RevokeOIDCSession_FullMethodName, func(p value.Principal) (*cp.RevokeOIDCSessionResponse, error) {
		if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
			return nil, errs.ErrInvalid
		}
		binding, err := server.service.RevokeOwnStandardOIDCSession(ctx, p)
		if err != nil {
			return nil, err
		}
		return &cp.RevokeOIDCSessionResponse{Binding: castOIDCSessionBinding(binding)}, nil
	})
}

// castOIDCSessionBinding projects only bounded owner state; provider tokens, raw subject claims, groups and permissions are deliberately absent.
// castOIDCSessionBinding проецирует лишь bounded owner state; provider tokens, raw subject claims, groups и permissions отсутствуют.
func castOIDCSessionBinding(binding platformrepo.StandardOIDCBinding) *cp.OIDCSessionBinding {
	state := cp.OIDCSessionState_OIDC_SESSION_STATE_UNSPECIFIED
	if binding.State == "ACTIVE" {
		state = cp.OIDCSessionState_OIDC_SESSION_STATE_ACTIVE
	}
	if binding.State == "REVOKED" {
		state = cp.OIDCSessionState_OIDC_SESSION_STATE_REVOKED
	}
	return &cp.OIDCSessionBinding{Issuer: binding.Issuer, Subject: binding.Subject, OrganizationId: binding.OrganizationID, SessionId: binding.SessionID, Revision: binding.Revision,
		AuthenticatedAt: timestamppb.New(binding.AuthenticatedAt), AbsoluteExpiresAt: timestamppb.New(binding.AbsoluteExpiry), PolicyRevision: binding.PolicyRevision,
		PolicyDigestSha256: binding.PolicyDigest, State: state}
}
