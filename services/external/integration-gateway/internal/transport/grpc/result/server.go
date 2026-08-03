// Package result реализует защищённый producer-owned read/ack transport.
package result

import (
	"context"
	"errors"
	"slices"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	GrantMetadata        = "x-mattercodex-integration-result-grant"
	resolveOperation     = "integration.result.resolve"
	acknowledgeOperation = "integration.result.acknowledge"
	readinessOperation   = "integration.result.readiness"
	callerWorkload       = "agent-runner"
	callerSPIFFEID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"
	targetWorkload       = "integration-gateway"
	targetSPIFFEID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
)

type checker interface{ Check(context.Context) error }

type Server struct {
	integrationgatewayv1.UnimplementedIntegrationResultServiceServer
	service  *domainservice.Service
	grant    *integrationgatewayauth.Verifier
	postgres checker
}

func New(service *domainservice.Service, grant *integrationgatewayauth.Verifier, postgres checker) (*Server, error) {
	if service == nil || grant == nil || postgres == nil {
		return nil, errors.New("integration result server dependencies are required")
	}
	return &Server{service: service, grant: grant, postgres: postgres}, nil
}

func (server *Server) ResolveIntegrationResult(
	ctx context.Context,
	_ *integrationgatewayv1.ResolveIntegrationResultRequest,
) (*integrationgatewayv1.ResolveIntegrationResultResponse, error) {
	binding, err := server.binding(ctx, resolveOperation)
	if err != nil {
		return nil, err
	}
	resolved, err := server.service.ResolveResult(ctx, binding)
	if err != nil {
		return nil, rpcError(err)
	}
	return &integrationgatewayv1.ResolveIntegrationResultResponse{
		InvocationId: resolved.InvocationID, AttemptId: resolved.AttemptID,
		ResultSha256: resolved.ResultSHA256, StructuredResultJson: resolved.StructuredJSON,
		DeliveryVersion: resolved.DeliveryVersion, DeliveryFence: resolved.DeliveryFence,
		CompletedAt: timestamppb.New(resolved.CompletedAt),
	}, nil
}

func (server *Server) AcknowledgeIntegrationResult(
	ctx context.Context,
	request *integrationgatewayv1.AcknowledgeIntegrationResultRequest,
) (*integrationgatewayv1.AcknowledgeIntegrationResultResponse, error) {
	binding, err := server.binding(ctx, acknowledgeOperation)
	if err != nil {
		return nil, err
	}
	if request.GetExpectedResultSha256() != binding.ResultSHA256 {
		return nil, status.Error(codes.FailedPrecondition, "result acknowledgement binding mismatch")
	}
	acknowledged, err := server.service.AcknowledgeResult(ctx, domainservice.ResultAcknowledgement{
		Binding: binding, IdempotencyKey: request.GetIdempotencyKey(),
		DeliveryVersion: request.GetExpectedDeliveryVersion(), DeliveryFence: request.GetExpectedDeliveryFence(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	if acknowledged.AcknowledgedAt == nil {
		return nil, status.Error(codes.FailedPrecondition, "result acknowledgement binding mismatch")
	}
	return &integrationgatewayv1.AcknowledgeIntegrationResultResponse{
		InvocationId: acknowledged.InvocationID, AttemptId: acknowledged.AttemptID,
		ResultSha256: acknowledged.ResultSHA256, DeliveryVersion: acknowledged.DeliveryVersion,
		DeliveryFence: acknowledged.DeliveryFence, AcknowledgedAt: timestamppb.New(*acknowledged.AcknowledgedAt),
	}, nil
}

func (server *Server) CheckReadiness(
	ctx context.Context,
	_ *integrationgatewayv1.IntegrationResultServiceCheckReadinessRequest,
) (*integrationgatewayv1.IntegrationResultServiceCheckReadinessResponse, error) {
	if _, err := verifiedContext(ctx, readinessOperation); err != nil {
		return nil, err
	}
	if err := server.postgres.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "integration result dependency unavailable")
	}
	return &integrationgatewayv1.IntegrationResultServiceCheckReadinessResponse{
		Ready: true, SchemaVersion: 1, AuthorityReady: true, PostgresReady: true,
	}, nil
}

func (server *Server) binding(ctx context.Context, operation string) (domainservice.ResultAccessBinding, error) {
	verified, err := verifiedContext(ctx, operation)
	if err != nil {
		return domainservice.ResultAccessBinding{}, err
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return domainservice.ResultAccessBinding{}, status.Error(codes.Unauthenticated, "result access grant required")
	}
	values := incoming.Get(GrantMetadata)
	if len(values) != 1 {
		return domainservice.ResultAccessBinding{}, status.Error(codes.Unauthenticated, "result access grant required")
	}
	claims, err := server.grant.Verify(ctx, values[0])
	if err != nil || claims.Purpose != integrationgatewayauth.PurposeResultAccess ||
		claims.Subject != verified.GetSubject() || claims.WorkloadID != callerWorkload ||
		claims.CallerSPIFFEID != callerSPIFFEID || !slices.Contains(claims.AllowedOperationIDs, operation) {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "result access grant rejected")
	}
	authority := verified.GetAuthority()
	if authority == nil || authority.GetActor() == nil || authority.GetTenant() == nil || authority.GetProject() == nil ||
		authority.GetActor().GetId() != claims.Subject || authority.GetTenant().GetId() != claims.OrganizationID ||
		authority.GetProject().GetId() != claims.ProjectID {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "result authority binding mismatch")
	}
	return domainservice.ResultAccessBinding{
		TenantID: claims.OrganizationID, ProjectID: claims.ProjectID,
		ActorID: claims.Subject, InvocationID: claims.InvocationID, AttemptID: claims.ResultAttemptID,
		ResultSHA256: claims.ResultSHA256,
	}, nil
}

func verifiedContext(ctx context.Context, operation string) (*internalrpcauthorityv1.VerifiedAuthorizationContext, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetCallerWorkloadId() != callerWorkload || verified.GetCallerSpiffeId() != callerSPIFFEID ||
		verified.GetTargetWorkloadId() != targetWorkload || verified.GetTargetSpiffeId() != targetSPIFFEID ||
		verified.GetOperationId() != operation {
		return nil, status.Error(codes.PermissionDenied, "verified result authorization context rejected")
	}
	return verified, nil
}

func rpcError(err error) error {
	switch {
	case errors.Is(err, errs.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authentication failed")
	case errors.Is(err, errs.ErrForbidden):
		return status.Error(codes.PermissionDenied, "access denied")
	case errors.Is(err, errs.ErrNotFound):
		return status.Error(codes.NotFound, "integration result not found")
	case errors.Is(err, errs.ErrInvalid):
		return status.Error(codes.InvalidArgument, "request is invalid")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.FailedPrecondition, "integration result binding conflict")
	default:
		return status.Error(codes.Internal, "integration result operation failed")
	}
}
