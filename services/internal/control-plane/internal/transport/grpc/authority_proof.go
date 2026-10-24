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

// rejectedSPIFFEIdentityMessage names the fixed public rejection for malformed certificate identity.
// rejectedSPIFFEIdentityMessage задаёт фиксированный публичный отказ для неверной identity сертификата.
const rejectedSPIFFEIdentityMessage = "mTLS SPIFFE identity is rejected"

// AuthorityProofServer exposes proof resolution and readiness through the owner service, with peer
// checks separate from application-credential validation.
// AuthorityProofServer предоставляет разрешение proof и готовность через сервис-владелец, отделяя
// проверку peer от проверки application credential.
type AuthorityProofServer struct {
	internalrpcauthorityv1.UnimplementedAuthorityProofResolverServiceServer
	service *authorityproof.Service
}

// NewAuthorityProofServer requires a nonnil authority-proof service before constructing the
// transport adapter without starting a listener.
// NewAuthorityProofServer требует ненулевой сервис authority proof до создания transport adapter
// без запуска listener.
func NewAuthorityProofServer(service *authorityproof.Service) (*AuthorityProofServer, error) {
	if service == nil {
		return nil, errors.New("authority proof service is required")
	}
	return &AuthorityProofServer{service: service}, nil
}

// ResolveAuthorityProof requires a request, verified SPIFFE peer and exactly one authorization
// value before forwarding trimmed bindings to owner resolution.
// ResolveAuthorityProof требует запрос, проверенный SPIFFE peer и ровно одно значение
// authorization до передачи нормализованных привязок разрешению владельца.
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

// CheckReadiness checks the verified peer and owner readiness, returning served policy and signer
// metadata; this method does not validate a bearer credential itself.
// CheckReadiness проверяет peer и готовность владельца, возвращая обслуживаемые метаданные policy
// и signer; сам метод не проверяет bearer credential.
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

// verifiedPeerSPIFFEID requires TLSInfo with exactly one nonempty verified chain and extracts
// identity from its verified leaf certificate.
// verifiedPeerSPIFFEID требует TLSInfo с ровно одной непустой проверенной цепочкой и извлекает
// identity из её проверенного leaf certificate.
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

// exactSPIFFEURI requires one certificate URI with the configured SPIFFE scheme and host and no
// query, fragment or userinfo, leaving operation eligibility to the service.
// exactSPIFFEURI требует единственный URI сертификата с заданными SPIFFE scheme и host без query,
// fragment либо userinfo, оставляя доступность операции сервису.
func exactSPIFFEURI(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", status.Error(codes.PermissionDenied, rejectedSPIFFEIdentityMessage)
	}
	identity := certificate.URIs[0]
	if identity.Scheme != "spiffe" || identity.Host != "kodex.local" || identity.RawQuery != "" || identity.Fragment != "" || identity.User != nil {
		return "", status.Error(codes.PermissionDenied, rejectedSPIFFEIdentityMessage)
	}
	return identity.String(), nil
}

// proofStatus maps known owner failures to fixed public statuses and rejects every unclassified
// failure as PermissionDenied without exposing its message.
// proofStatus переводит известные отказы владельца в фиксированные публичные статусы и отклоняет
// прочие как PermissionDenied без раскрытия их текста.
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
