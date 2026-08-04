// Package result реализует защищённый producer-owned read/ack transport.
package result

import (
	"context"
	"errors"
	"fmt"
	"slices"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	resolveOperation     = "integration.result.resolve"
	acknowledgeOperation = "integration.result.acknowledge"
	readinessOperation   = "integration.result.readiness"
	callerWorkload       = "agent-runner"
	callerSPIFFEID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"
	targetWorkload       = "integration-gateway"
	targetSPIFFEID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
)

type resultStore interface {
	Check(context.Context) error
	AdmitResultGrantVerifierState(context.Context, domainrepo.ResultGrantVerifierState) error
}

type accessValidator interface {
	ValidateResultAccess(context.Context, string) (*controlplanev1.ValidateIntegrationResultAccessResponse, error)
	Check(context.Context) error
}

type Server struct {
	integrationgatewayv1.UnimplementedIntegrationResultServiceServer
	service  *domainservice.Service
	grant    *integrationgatewayauth.Verifier
	postgres resultStore
	control  accessValidator
}

func New(service *domainservice.Service, grant *integrationgatewayauth.Verifier, postgres resultStore, control accessValidator) (*Server, error) {
	if service == nil || grant == nil || postgres == nil || control == nil {
		return nil, errors.New("integration result server dependencies are required")
	}
	return &Server{service: service, grant: grant, postgres: postgres, control: control}, nil
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
		Outcome: outcomeProto(resolved.Outcome), Reference: resolved.Reference,
		ReferenceSha256: resolved.ReferenceSHA256, StructuredOutcomeJson: resolved.StructuredJSON,
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
	if request.GetExpectedReferenceSha256() != binding.ReferenceSHA256 {
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
		Outcome: outcomeProto(acknowledged.Outcome), Reference: acknowledged.Reference,
		ReferenceSha256: acknowledged.ReferenceSHA256, DeliveryVersion: acknowledged.DeliveryVersion,
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
	state := server.grant.State()
	if err := server.postgres.AdmitResultGrantVerifierState(ctx, domainrepo.ResultGrantVerifierState{
		KeysetRevision: state.Revision, HighWatermark: state.HighWatermark,
		ServedGeneration: state.ServedGeneration, KeysetSHA256: state.KeysetSHA256,
		SignerGeneration: state.ServedGeneration,
	}); err != nil {
		return nil, status.Error(codes.Unavailable, "integration result verifier state unavailable")
	}
	if err := server.control.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "integration result owner validation unavailable")
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
	values := incoming.Get(integrationgatewayauth.ResultAccessGrantMetadata)
	if len(values) != 1 {
		return domainservice.ResultAccessBinding{}, status.Error(codes.Unauthenticated, "result access grant required")
	}
	compact := values[0]
	claims, err := server.grant.Verify(ctx, compact)
	if err != nil || claims.Purpose != integrationgatewayauth.PurposeResultAccess ||
		claims.Subject != verified.GetSubject() || claims.WorkloadID != callerWorkload ||
		claims.CallerSPIFFEID != callerSPIFFEID || !slices.Contains(claims.AllowedOperationIDs, operation) {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "result access grant rejected")
	}
	authority := verified.GetAuthority()
	if authority == nil || authority.GetActor() == nil || authority.GetTenant() == nil || authority.GetProject() == nil ||
		authority.GetActor().GetId() != claims.Subject || authority.GetTenant().GetId() != claims.OrganizationID ||
		authority.GetProject().GetId() != claims.ProjectID ||
		!matchesResultAuthorityProvenance(authority.GetActor(), claims) ||
		!matchesResultAuthorityProvenance(authority.GetTenant(), claims) ||
		!matchesResultAuthorityProvenance(authority.GetProject(), claims) {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "result authority binding mismatch")
	}
	served := server.grant.State()
	if err := server.postgres.AdmitResultGrantVerifierState(ctx, domainrepo.ResultGrantVerifierState{
		KeysetRevision: served.Revision, HighWatermark: served.HighWatermark,
		ServedGeneration: served.ServedGeneration, KeysetSHA256: served.KeysetSHA256,
		SignerGeneration: claims.SignerGeneration,
	}); err != nil {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "result signer generation rejected")
	}
	validated, err := server.control.ValidateResultAccess(ctx, compact)
	if err != nil || validated.GetContinuation().GetContinuationId() != claims.ContinuationID ||
		validated.GetContinuation().GetVersion() != claims.ContinuationVersion ||
		validated.GetContinuation().GetFence() != claims.ContinuationFence ||
		validated.GetOutcome() != claims.Outcome || validated.GetReference() != claims.Reference ||
		validated.GetReferenceSha256() != claims.ReferenceSHA256 ||
		validated.GetResultAttemptId() != claims.ResultAttemptID ||
		validated.GetSignerGeneration() != claims.SignerGeneration {
		return domainservice.ResultAccessBinding{}, status.Error(codes.PermissionDenied, "current result owner state rejected")
	}
	return domainservice.ResultAccessBinding{
		TenantID: claims.OrganizationID, ProjectID: claims.ProjectID,
		ActorID: claims.Subject, InvocationID: claims.InvocationID, AttemptID: claims.ResultAttemptID,
		Outcome: enum.InvocationStatus(claims.Outcome), Reference: claims.Reference,
		ReferenceSHA256: claims.ReferenceSHA256, ContinuationID: claims.ContinuationID,
		ContinuationVersion: claims.ContinuationVersion, ContinuationFence: claims.ContinuationFence,
	}, nil
}

func matchesResultAuthorityProvenance(
	identity *internalrpcauthorityv1.AuthorityIdentity,
	claims integrationgatewayauth.Claims,
) bool {
	provenance := identity.GetProvenance()
	return provenance != nil &&
		provenance.GetSource() == internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_INTEGRATION_CONTINUATION &&
		provenance.GetReference() == fmt.Sprintf("%s/%d/%d", claims.TurnID, claims.Attempt, claims.GrantGeneration) &&
		provenance.GetRevision() == claims.GrantGeneration && provenance.GetDigestSha256() == claims.InputSHA256
}

func outcomeProto(outcome enum.InvocationStatus) integrationgatewayv1.IntegrationOutcome {
	return map[enum.InvocationStatus]integrationgatewayv1.IntegrationOutcome{
		enum.InvocationSucceeded: integrationgatewayv1.IntegrationOutcome_INTEGRATION_OUTCOME_SUCCEEDED,
		enum.InvocationFailed:    integrationgatewayv1.IntegrationOutcome_INTEGRATION_OUTCOME_FAILED,
		enum.InvocationUnknown:   integrationgatewayv1.IntegrationOutcome_INTEGRATION_OUTCOME_UNKNOWN,
	}[outcome]
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
