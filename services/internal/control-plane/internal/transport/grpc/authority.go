package grpc

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authorityservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/authority"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ApplicationAuthenticator владеет точной границей mTLS и OIDC.
type ApplicationAuthenticator interface {
	Authenticate(context.Context) (authoritytype.ApplicationIdentity, error)
	VerifyPeer(context.Context) error
}

// AuthorityServer реализует первый вызов границы выдачи доказательств полномочий.
type AuthorityServer struct {
	internalrpcauthorityv1.UnimplementedAuthorityProofResolverServiceServer
	service       *authorityservice.Service
	authenticator ApplicationAuthenticator
}

// NewAuthorityServer создаёт транспорт без идентичности, управляемой вызывающей
// стороной.
func NewAuthorityServer(
	service *authorityservice.Service,
	authenticator ApplicationAuthenticator,
) (*AuthorityServer, error) {
	if service == nil || authenticator == nil {
		return nil, errors.New("authority resolver transport dependencies are required")
	}
	return &AuthorityServer{
		service:       service,
		authenticator: authenticator,
	}, nil
}

func (server *AuthorityServer) ResolveAuthorityProof(
	ctx context.Context,
	request *internalrpcauthorityv1.ResolveAuthorityProofRequest,
) (*internalrpcauthorityv1.ResolveAuthorityProofResponse, error) {
	if grpcserver.HasMalformedProto(request) {
		return nil, authorityRPCError(errs.ErrInvalidInput)
	}
	identity, err := server.authenticator.Authenticate(ctx)
	if err != nil {
		return nil, authorityRPCError(err)
	}
	proof, err := server.service.Resolve(ctx, authorityservice.ResolveInput{
		Identity:          identity,
		OperationID:       request.GetOperationId(),
		ProjectReference:  request.GetProjectReference(),
		ResourceReference: request.GetResourceReference(),
		IdempotencyKey:    request.GetIdempotencyKey(),
		CorrelationID:     request.GetCorrelationId(),
	})
	if err != nil {
		return nil, authorityRPCError(err)
	}
	expiresAt := timestamppb.New(proof.ExpiresAt)
	if err := expiresAt.CheckValid(); err != nil {
		return nil, authorityRPCError(errs.ErrInternal)
	}
	return &internalrpcauthorityv1.ResolveAuthorityProofResponse{
		AuthorityProofCompactJws: proof.CompactJWS,
		ExpiresAt:                expiresAt,
		ProofRevision:            proof.ProofRevision,
		ProofDigestSha256:        proof.ProofDigest,
		PolicyRevision:           proof.PolicyRevision,
		SignerGeneration:         proof.SignerGeneration,
	}, nil
}

func (server *AuthorityServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse, error) {
	if grpcserver.HasMalformedProto(request) {
		return nil, authorityRPCError(errs.ErrInvalidInput)
	}
	if err := server.authenticator.VerifyPeer(ctx); err != nil {
		return nil, authorityRPCError(err)
	}
	state, err := server.service.Check(ctx)
	if err != nil {
		return nil, authorityRPCError(errs.ErrUnavailable)
	}
	return &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse{
		Ready:                           true,
		PolicyRevision:                  state.PolicyRevision,
		PolicyDigestSha256:              state.PolicyDigest,
		SignerGeneration:                state.SignerGeneration,
		ServedPublicJwkThumbprintSha256: state.PublicThumbprint,
		DomainReadPathReady:             true,
	}, nil
}

func authorityRPCError(err error) error {
	switch {
	case errors.Is(err, errs.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, "authority proof request is invalid")
	case errors.Is(err, errs.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "application credential is invalid")
	case errors.Is(err, errs.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, "authority proof is not permitted")
	case errors.Is(err, errs.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, errs.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, "idempotency key conflicts with persisted request")
	case errors.Is(err, errs.ErrUnavailable):
		return status.Error(codes.Unavailable, "authority proof dependency is unavailable")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
