package grpc

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/authorityproof"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AuthorityProofServer struct {
	internalrpcauthorityv1.UnimplementedAuthorityProofResolverServiceServer
	service *authorityproof.Service
}

func NewAuthorityProofServer(service *authorityproof.Service) (*AuthorityProofServer, error) {
	if service == nil {
		return nil, errors.New("authority proof service is required")
	}
	return &AuthorityProofServer{service: service}, nil
}

func (server *AuthorityProofServer) ResolveAuthorityProof(ctx context.Context, request *internalrpcauthorityv1.ResolveAuthorityProofRequest) (*internalrpcauthorityv1.ResolveAuthorityProofResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "authority proof request is required")
	}
	spiffeID, err := verifiedPeerSPIFFEID(ctx)
	if err != nil {
		return nil, err
	}
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 {
		return nil, status.Error(codes.Unauthenticated, "application credential is required")
	}
	result, err := server.service.Resolve(ctx, authorityproof.ResolveInput{
		PeerSPIFFEID: spiffeID, Authorization: values[0], OperationID: strings.TrimSpace(request.GetOperationId()),
		ResourceReference: strings.TrimSpace(request.GetResourceReference()),
		ProjectReference:  strings.TrimSpace(request.GetProjectReference()), IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
		CorrelationID: strings.TrimSpace(request.GetCorrelationId()), RequestDigestSHA256: strings.TrimSpace(request.GetRequestDigestSha256()),
	})
	if err != nil {
		return nil, proofStatus(err)
	}
	return &internalrpcauthorityv1.ResolveAuthorityProofResponse{
		AuthorityProofCompactJws: result.CompactJWS, ExpiresAt: timestamppb.New(result.ExpiresAt),
		ProofRevision: result.ProofRevision, ProofDigestSha256: result.DigestSHA256,
		PolicyRevision: result.PolicyRevision, SignerGeneration: result.SignerGeneration,
	}, nil
}

func (server *AuthorityProofServer) CheckReadiness(ctx context.Context, _ *internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessRequest) (*internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse, error) {
	if _, err := verifiedPeerSPIFFEID(ctx); err != nil {
		return nil, err
	}
	state, err := server.service.Ready(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "authority proof resolver is not ready")
	}
	return &internalrpcauthorityv1.AuthorityProofResolverServiceCheckReadinessResponse{
		Ready: true, PolicyRevision: state.PolicyRevision, PolicyDigestSha256: state.PolicyDigestSHA256,
		SignerGeneration: state.SignerGeneration, ServedPublicJwkThumbprintSha256: state.SignerThumbprintSHA256,
		DomainReadPathReady: true,
	}, nil
}

func verifiedPeerSPIFFEID(ctx context.Context) (string, error) {
	value, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.PermissionDenied, "mTLS peer is required")
	}
	tlsInfo, ok := value.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) != 1 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", status.Error(codes.PermissionDenied, "mTLS peer is rejected")
	}
	return exactSPIFFEURI(tlsInfo.State.VerifiedChains[0][0])
}

func exactSPIFFEURI(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", status.Error(codes.PermissionDenied, "mTLS SPIFFE identity is rejected")
	}
	identity := certificate.URIs[0]
	if identity.Scheme != "spiffe" || identity.Host != "kodex.local" || identity.RawQuery != "" || identity.Fragment != "" || identity.User != nil {
		return "", status.Error(codes.PermissionDenied, "mTLS SPIFFE identity is rejected")
	}
	return identity.String(), nil
}

func proofStatus(err error) error {
	switch {
	case errors.Is(err, errs.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "authority proof credential is rejected")
	case errors.Is(err, errs.ErrForbidden):
		return status.Error(codes.PermissionDenied, "authority proof permission is rejected")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.Aborted, "authority proof state conflict")
	case errors.Is(err, errs.ErrUnavailable):
		return status.Error(codes.Unavailable, "authority proof owner is unavailable")
	default:
		return status.Error(codes.PermissionDenied, "authority proof request is rejected")
	}
}

var _ internalrpcauthorityv1.AuthorityProofResolverServiceServer = (*AuthorityProofServer)(nil)
