package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) DetachAccessResource(ctx context.Context,
	request *controlplanev1.DetachAccessResourceRequest,
) (*controlplanev1.DetachAccessResourceResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_DetachAccessResource_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.DetachAccessResource(ctx, resource.DetachAccessResourceInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ResourceID: request.GetResourceId(), ExpectedVersion: request.GetExpectedVersion(),
		ExpectedKind: fromProtoKind(request.GetExpectedKind()),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.DetachAccessResourceResponse{Resource: encoded}, nil
}

func (server *Server) CopyAccessResource(ctx context.Context,
	request *controlplanev1.CopyAccessResourceRequest,
) (*controlplanev1.CopyAccessResourceResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_CopyAccessResource_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.CopyAccessResource(ctx, resource.CopyAccessResourceInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		SourceResourceID:      request.GetSourceResourceId(),
		ExpectedSourceVersion: request.GetExpectedSourceVersion(),
		ExpectedKind:          fromProtoKind(request.GetExpectedKind()), Name: request.GetName(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CopyAccessResourceResponse{Resource: encoded}, nil
}

func (server *Server) ListRuntimeIncidents(ctx context.Context,
	request *controlplanev1.ListRuntimeIncidentsRequest,
) (*controlplanev1.ListRuntimeIncidentsResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_ListRuntimeIncidents_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	incidents, err := server.service.ListRuntimeIncidents(ctx, resource.ListRuntimeIncidentsInput{
		Principal: principal,
		Filter:    query.RuntimeIncidentFilter{AfterID: request.GetPageToken(), Limit: limit},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListRuntimeIncidentsResponse{
		Projections: make([]*controlplanev1.RuntimeIncidentOwnerProjection, 0, len(incidents)),
	}
	for _, incident := range incidents {
		projection, projectionErr := server.service.RuntimeIncidentOwnerProjection(ctx, principal, incident)
		if projectionErr != nil {
			return nil, rpcError(principal.CorrelationID, projectionErr)
		}
		response.Projections = append(response.Projections, runtimeIncidentOwnerProjectionToProto(projection))
	}
	if len(incidents) == limit {
		response.NextPageToken = incidents[len(incidents)-1].ID
	}
	return response, nil
}

func (server *Server) AdmitOwnerSession(ctx context.Context,
	request *controlplanev1.AdmitOwnerSessionRequest,
) (*controlplanev1.AdmitOwnerSessionResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_AdmitOwnerSession_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	state, err := server.service.AdmitOwnerSession(ctx, resource.OwnerSessionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.AdmitOwnerSessionResponse{Session: ownerSessionState(state)}, nil
}

func (server *Server) RevokeOwnerSession(ctx context.Context,
	request *controlplanev1.RevokeOwnerSessionRequest,
) (*controlplanev1.RevokeOwnerSessionResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_RevokeOwnerSession_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	state, err := server.service.RevokeOwnerSession(ctx, resource.OwnerSessionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExpectedRevision: request.GetExpectedRevision(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RevokeOwnerSessionResponse{Session: ownerSessionState(state)}, nil
}

func ownerSessionState(state domainrepo.OwnerSessionState) *controlplanev1.OwnerSessionState {
	return &controlplanev1.OwnerSessionState{
		SessionId: state.SessionID, CurrentRevision: state.CurrentRevision,
		Active: state.RevokedAt.IsZero(), UpdatedAt: timestamppb.New(state.UpdatedAt),
	}
}

func (server *Server) PrepareGatewayPublicTLS(ctx context.Context,
	request *controlplanev1.PrepareGatewayPublicTLSRequest,
) (*controlplanev1.PrepareGatewayPublicTLSResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_PrepareGatewayPublicTLS_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if request.GetNotBefore() == nil || request.GetNotAfter() == nil ||
		request.GetNotBefore().CheckValid() != nil || request.GetNotAfter().CheckValid() != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	state, err := server.service.PrepareGatewayPublicTLS(ctx, resource.PrepareGatewayPublicTLSInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Generation: request.GetGeneration(), CertificateSHA256: request.GetCertificateSha256(),
		PredecessorGeneration:        request.GetPredecessorGeneration(),
		PredecessorCertificateSHA256: request.GetPredecessorCertificateSha256(),
		NotBefore:                    request.GetNotBefore().AsTime(), NotAfter: request.GetNotAfter().AsTime(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.PrepareGatewayPublicTLSResponse{State: gatewayPublicTLSState(state)}, nil
}

func (server *Server) ConfirmGatewayPublicTLS(ctx context.Context,
	request *controlplanev1.ConfirmGatewayPublicTLSRequest,
) (*controlplanev1.ConfirmGatewayPublicTLSResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_ConfirmGatewayPublicTLS_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	state, err := server.service.ConfirmGatewayPublicTLS(ctx, resource.ConfirmGatewayPublicTLSInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Generation: request.GetGeneration(), CertificateSHA256: request.GetCertificateSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ConfirmGatewayPublicTLSResponse{State: gatewayPublicTLSState(state)}, nil
}

func (server *Server) CheckGatewayPublicTLS(ctx context.Context,
	request *controlplanev1.CheckGatewayPublicTLSRequest,
) (*controlplanev1.CheckGatewayPublicTLSResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_CheckGatewayPublicTLS_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	state, err := server.service.CheckGatewayPublicTLS(ctx, resource.CheckGatewayPublicTLSInput{
		Principal: principal, Generation: request.GetGeneration(), CertificateSHA256: request.GetCertificateSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CheckGatewayPublicTLSResponse{State: gatewayPublicTLSState(state)}, nil
}

func gatewayPublicTLSState(state domainrepo.GatewayPublicTLSState) *controlplanev1.GatewayPublicTLSState {
	result := &controlplanev1.GatewayPublicTLSState{UpdatedAt: timestamppb.New(state.UpdatedAt)}
	result.Applied = gatewayPublicTLSMaterial(state.Applied)
	result.Pending = gatewayPublicTLSMaterial(state.Pending)
	result.Previous = gatewayPublicTLSMaterial(state.Previous)
	if !state.OverlapExpiresAt.IsZero() {
		result.OverlapExpiresAt = timestamppb.New(state.OverlapExpiresAt)
	}
	return result
}

func gatewayPublicTLSMaterial(material domainrepo.GatewayPublicTLSMaterial) *controlplanev1.GatewayPublicTLSMaterial {
	if material.Generation == 0 {
		return nil
	}
	return &controlplanev1.GatewayPublicTLSMaterial{
		Generation: material.Generation, CertificateSha256: material.CertificateSHA256,
		NotBefore: timestamppb.New(material.NotBefore), NotAfter: timestamppb.New(material.NotAfter),
	}
}

func toProtoRuntimeIncidentKind(value string) controlplanev1.RuntimeIncidentKind {
	return controlplanev1.RuntimeIncidentKind(
		controlplanev1.RuntimeIncidentKind_value["RUNTIME_INCIDENT_KIND_"+value],
	)
}
