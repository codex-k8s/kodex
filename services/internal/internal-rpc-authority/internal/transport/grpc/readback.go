package authoritygrpc

import (
	"context"
	"crypto/x509"
	"errors"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthorityReadbackAttestorServer адаптирует независимую проверку к gRPC.
type AuthorityReadbackAttestorServer struct {
	internalrpcauthorityv1.UnimplementedAuthorityReadbackAttestorServiceServer
	application        *application.ReadbackAttestor
	verifierGeneration uint64
}

// NewAuthorityReadbackAttestorServer создаёт сервер независимой проверки.
func NewAuthorityReadbackAttestorServer(
	applicationValue *application.ReadbackAttestor,
	verifierGeneration uint64,
) *AuthorityReadbackAttestorServer {
	return &AuthorityReadbackAttestorServer{
		application:        applicationValue,
		verifierGeneration: verifierGeneration,
	}
}

// IssueAttestationChallenge выдаёт устойчивый одноразовый запрос.
func (server *AuthorityReadbackAttestorServer) IssueAttestationChallenge(
	ctx context.Context,
	request *internalrpcauthorityv1.IssueAttestationChallengeRequest,
) (*internalrpcauthorityv1.IssueAttestationChallengeResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetPinnedIntentId() == "" ||
		request.GetReadbackCredentialCompactJws() == "" ||
		len(request.GetReadbackCredentialCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		request.GetIdempotencyKey() == "" ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	peerSPIFFEID, err := verifiedPeerSPIFFEID(ctx)
	if err != nil {
		return nil, authorizationError(readbackMTLSErrorSpec, correlationID)
	}
	result, err := server.application.IssueChallenge(
		ctx,
		peerSPIFFEID,
		request.GetPinnedIntentId(),
		request.GetReadbackCredentialCompactJws(),
		request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, mapReadbackError(err, correlationID)
	}
	kind := internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT
	if result.Challenge.Intent.Kind == "KEY_DELIVERY" {
		kind = internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_KEY_DELIVERY
	}
	return &internalrpcauthorityv1.IssueAttestationChallengeResponse{
		ChallengeId:                    result.Challenge.ChallengeID,
		ChallengeJti:                   result.Challenge.ChallengeJTI,
		ChallengeNonce:                 result.Challenge.Nonce,
		ChallengeDigestSha256:          result.Challenge.DigestSHA256,
		IssuedAt:                       timestamppb.New(result.Challenge.IssuedAt),
		ExpiresAt:                      timestamppb.New(result.Challenge.ExpiresAt),
		Kind:                           kind,
		PinnedIntentRevision:           result.Challenge.Intent.IntentRevision,
		PinnedIntentDigestSha256:       result.Challenge.Intent.IntentDigestSHA256,
		WorkloadGeneration:             result.Challenge.Intent.WorkloadGeneration,
		CredentialGeneration:           result.Challenge.Intent.CredentialGeneration,
		PossessionKeyGeneration:        result.Challenge.Intent.PossessionKeyGeneration,
		ReadbackCredentialDigestSha256: result.Challenge.ReadbackCredentialDigest,
	}, nil
}

// AttestServedState проверяет владение ключом и фиксирует подтверждение.
func (server *AuthorityReadbackAttestorServer) AttestServedState(
	ctx context.Context,
	request *internalrpcauthorityv1.AttestServedStateRequest,
) (*internalrpcauthorityv1.AttestServedStateResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetPinnedIntentId() == "" ||
		request.GetChallengeId() == "" ||
		request.GetReadbackCredentialCompactJws() == "" ||
		request.GetServedStateAttestationCompactJws() == "" ||
		len(request.GetReadbackCredentialCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		len(request.GetServedStateAttestationCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		request.GetIdempotencyKey() == "" ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	peerSPIFFEID, err := verifiedPeerSPIFFEID(ctx)
	if err != nil {
		return nil, authorizationError(readbackMTLSErrorSpec, correlationID)
	}
	result, err := server.application.Attest(
		ctx,
		peerSPIFFEID,
		request.GetPinnedIntentId(),
		request.GetChallengeId(),
		request.GetReadbackCredentialCompactJws(),
		request.GetServedStateAttestationCompactJws(),
		request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, mapReadbackError(err, correlationID)
	}
	kind := internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT
	if result.Receipt.Intent.Kind == "KEY_DELIVERY" {
		kind = internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_KEY_DELIVERY
	}
	return &internalrpcauthorityv1.AttestServedStateResponse{
		AttestationReceiptId: result.Receipt.ReceiptID,
		Kind:                 kind,
		ExpiresAt:            timestamppb.New(result.Receipt.ExpiresAt),
		EvidenceDigestSha256: result.Receipt.EvidenceDigestSHA256,
		VerifierGeneration:   result.Receipt.VerifierGeneration,
	}, nil
}

// CheckReadiness проверяет доступность устойчивого хранилища.
func (server *AuthorityReadbackAttestorServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.AuthorityReadbackAttestorServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.AuthorityReadbackAttestorServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, authorizationError(errorSpecMalformedRequest, "")
	}
	if _, err := verifiedPeerSPIFFEID(ctx); err != nil {
		return nil, authorizationError(readbackMTLSErrorSpec, "")
	}
	if err := server.application.Ready(ctx); err != nil {
		return &internalrpcauthorityv1.AuthorityReadbackAttestorServiceCheckReadinessResponse{
			Ready: false,
		}, nil
	}
	state := server.application.TrustState()
	return &internalrpcauthorityv1.AuthorityReadbackAttestorServiceCheckReadinessResponse{
		Ready:                                   true,
		VerifierGeneration:                      server.verifierGeneration,
		ReceiptStoreReady:                       true,
		ChallengeStoreReady:                     true,
		ReadbackCredentialTrustSourceRevision:   state.TrustSourceRevision,
		ReadbackCredentialTrustDigestSha256:     state.TrustSetDigestSHA256,
		ReadbackCredentialTrustKeySetRevision:   state.TrustKeySetRevision,
		ReadbackCredentialTrustSignerGeneration: state.SignerGeneration,
		ReadbackCredentialTrustReadbackReady:    true,
		ReadbackManifestRootId:                  state.RootID,
		ReadbackManifestRootFingerprintSha256:   state.RootFingerprintSHA256,
		ReadbackManifestBundleRevision:          state.ManifestBundleRevision,
		ReadbackManifestBundleDigestSha256:      state.ManifestBundleDigestSHA256,
		ReadbackManifestSignerGeneration:        state.SignerGeneration,
		ReadbackManifestRootServedReadbackReady: true,
	}, nil
}

var (
	readbackMTLSErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "readback mTLS peer rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	readbackCredentialErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "readback credential rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_READBACK_CREDENTIAL_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_AUTHORITY_RESOLUTION,
	}
	readbackReplayErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "readback challenge replay detected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_READBACK_CHALLENGE_REPLAY_DETECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REPLAY,
	}
	readbackUnavailableErrorSpec = errorSpec{
		code:      codes.Unavailable,
		message:   "readback challenge store unavailable",
		reason:    internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_READBACK_CHALLENGE_UNAVAILABLE,
		stage:     internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_PERSISTENCE,
		retryable: true,
	}
	readbackNotFoundErrorSpec = errorSpec{
		code:    codes.NotFound,
		message: "authority resource not found",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_AUTHORITY_RESOURCE_NOT_FOUND,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_AUTHORITY_RESOLUTION,
	}
)

func mapReadbackError(err error, correlationID string) error {
	switch {
	case failure.IsKind(err, failure.InvalidRequest):
		return authorizationError(errorSpecMalformedRequest, correlationID)
	case failure.IsKind(err, failure.NotFound):
		return authorizationError(readbackNotFoundErrorSpec, correlationID)
	case failure.IsKind(err, failure.ReplayDetected):
		return authorizationError(readbackReplayErrorSpec, correlationID)
	case failure.IsKind(err, failure.PersistenceUnavailable):
		return authorizationError(readbackUnavailableErrorSpec, correlationID)
	case failure.IsKind(err, failure.Unauthenticated),
		failure.IsKind(err, failure.BindingMismatch):
		return authorizationError(readbackCredentialErrorSpec, correlationID)
	default:
		return authorizationError(errorSpecInternal, correlationID)
	}
}

func verifiedPeerSPIFFEID(ctx context.Context) (string, error) {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return "", errors.New("mTLS peer is absent")
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) != 1 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", errors.New("verified mTLS chain is absent")
	}
	certificate := tlsInfo.State.VerifiedChains[0][0]
	return exactSPIFFEURI(certificate)
}

func exactSPIFFEURI(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", errors.New("exact SPIFFE URI is absent")
	}
	value := certificate.URIs[0].String()
	if value == "" || certificate.URIs[0].Scheme != "spiffe" ||
		certificate.URIs[0].Host != "mattercodex.local" {
		return "", errors.New("SPIFFE URI is outside the trust domain")
	}
	return value, nil
}
