package authoritygrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// RestoreRoleCredentialPublisherServer adapts controller-authorized role publication with a configured
// controller generation; it never derives that generation or signing authority from request fields.
// RestoreRoleCredentialPublisherServer адаптирует role публикацию, разрешённую controller, с настроенным
// controller generation; generation и signing полномочия не выводятся из request полей.
type RestoreRoleCredentialPublisherServer struct {
	internalrpcauthorityv1.UnimplementedRestoreRoleCredentialPublisherServiceServer
	application                  *application.Publisher
	expectedControllerGeneration uint64
}

// NewRestoreRoleCredentialPublisherServer stores the application and expected controller generation
// without publishing credentials or performing TLS verification at construction.
// NewRestoreRoleCredentialPublisherServer сохраняет application и expected controller generation без
// публикации credentials и TLS проверки при создании.
func NewRestoreRoleCredentialPublisherServer(
	applicationValue *application.Publisher,
	expectedControllerGeneration uint64,
) *RestoreRoleCredentialPublisherServer {
	return &RestoreRoleCredentialPublisherServer{
		application:                  applicationValue,
		expectedControllerGeneration: expectedControllerGeneration,
	}
}

// PublishRoleCredential validates directive/idempotency/correlation shape, requires the exact configured
// controller mTLS identity and delegates signed-directive publication. The reply contains only delivery
// receipt and credential/ACK generations, not private key material.
// PublishRoleCredential проверяет форму directive/idempotency/correlation, требует точную настроенную
// controller mTLS identity и передаёт signed-directive публикацию. Ответ содержит только delivery
// receipt и credential/ACK поколения без private key material.
func (server *RestoreRoleCredentialPublisherServer) PublishRoleCredential(
	ctx context.Context,
	request *internalrpcauthorityv1.PublishRoleCredentialRequest,
) (*internalrpcauthorityv1.PublishRoleCredentialResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetIssuanceDirectiveCompactJws() == "" ||
		len(request.GetIssuanceDirectiveCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		request.GetIdempotencyKey() == "" ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	controller, err := controllerIdentityFromMTLS(
		ctx,
		server.expectedControllerGeneration,
	)
	if err != nil {
		return nil, authorizationError(publisherPeerErrorSpec, correlationID)
	}
	result, err := server.application.Publish(
		ctx,
		controller,
		request.GetIssuanceDirectiveCompactJws(),
		request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, mapPublisherError(err, correlationID)
	}
	return &internalrpcauthorityv1.PublishRoleCredentialResponse{
		DeliveryReceiptCompactJws:  result.DeliveryReceiptJWS,
		RoleCredentialDigestSha256: result.RoleCredentialDigest,
		CredentialGeneration:       result.CredentialGeneration,
		AckKeyGeneration:           result.ACKKeyGeneration,
	}, nil
}

// CheckReadiness requires the same controller peer admission as publication before application readiness
// and registry/delivery metadata; a storage failure returns Ready=false, not a synthesized successful
// readback.
// CheckReadiness требует тот же controller peer admission, что публикация, до application readiness и
// registry/delivery metadata; storage отказ возвращает Ready=false без выдуманного успешного readback.
func (server *RestoreRoleCredentialPublisherServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, authorizationError(errorSpecMalformedRequest, "")
	}
	if _, err := controllerIdentityFromMTLS(
		ctx,
		server.expectedControllerGeneration,
	); err != nil {
		return nil, authorizationError(publisherPeerErrorSpec, "")
	}
	if err := server.application.Ready(ctx); err != nil {
		return &internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceCheckReadinessResponse{
			Ready: false,
		}, nil
	}
	registry := server.application.Registry()
	return &internalrpcauthorityv1.RestoreRoleCredentialPublisherServiceCheckReadinessResponse{
		Ready:                          true,
		TargetRegistryRevision:         registry.SourceRevision,
		TargetRegistryDigestSha256:     registry.SourceDigest,
		ControllerTrustGeneration:      server.expectedControllerGeneration,
		CredentialSignerGeneration:     server.application.SignerGeneration(),
		SecretExactTargetReadbackReady: true,
	}, nil
}

var (
	// publisherPeerErrorSpec maps a missing or wrong controller TLS identity to a fixed unauthenticated
	// mTLS-binding failure.
	// publisherPeerErrorSpec преобразует отсутствующую либо неверную controller TLS identity в фиксированный
	// unauthenticated mTLS-binding отказ.
	publisherPeerErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore controller mTLS identity rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	// publisherDirectiveErrorSpec marks invalid or mismatched restore issuance directives as unauthenticated
	// without echoing the compact directive.
	// publisherDirectiveErrorSpec помечает неверные либо несовпадающие restore issuance directives как
	// unauthenticated без возврата compact directive.
	publisherDirectiveErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore issuance directive rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_DIRECTIVE_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	// publisherConflictErrorSpec maps publication replay/idempotency conflict to AlreadyExists with a
	// restore-stage typed reason.
	// publisherConflictErrorSpec преобразует publication replay/idempotency конфликт в AlreadyExists с
	// типизированной restore-stage причиной.
	publisherConflictErrorSpec = errorSpec{
		code:    codes.AlreadyExists,
		message: "restore credential publication idempotency conflict",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_IDEMPOTENCY_CONFLICT,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
)

// mapPublisherError preserves publisher-specific malformed, directive and idempotency classifications
// and uses fixed persistence/internal fallbacks; unlike mapError it has no separate context-priority
// branch.
// mapPublisherError сохраняет publisher-specific malformed, directive и idempotency классификацию с
// фиксированными persistence/internal fallback; в отличие от mapError отдельной context-priority ветки
// нет.
func mapPublisherError(err error, correlationID string) error {
	switch {
	case failure.IsKind(err, failure.InvalidRequest):
		return authorizationError(errorSpecMalformedRequest, correlationID)
	case failure.IsKind(err, failure.Unauthenticated),
		failure.IsKind(err, failure.BindingMismatch):
		return authorizationError(publisherDirectiveErrorSpec, correlationID)
	case failure.IsKind(err, failure.ReplayDetected):
		return authorizationError(publisherConflictErrorSpec, correlationID)
	case failure.IsKind(err, failure.PersistenceUnavailable):
		return authorizationError(errorSpecPersistence, correlationID)
	default:
		return authorizationError(errorSpecInternal, correlationID)
	}
}

// controllerIdentityFromMTLS consumes an already verified single TLS chain, requires one exact
// controller SPIFFE URI and P-256 key, and derives the key ID from certificate bytes with the configured
// generation. It does not validate a certificate signature or grant domain publication by itself.
// controllerIdentityFromMTLS использует уже проверенную единственную TLS chain, требует один точный
// controller SPIFFE URI и P-256 key и выводит key ID из certificate байтов с настроенным generation.
// Подпись сертификата не проверяется, доменные publication полномочия сами не выдаются.
func controllerIdentityFromMTLS(
	ctx context.Context,
	generation uint64,
) (service.ControllerIdentity, error) {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return service.ControllerIdentity{}, errors.New("mTLS peer is absent")
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) != 1 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return service.ControllerIdentity{}, errors.New("verified mTLS chain is absent")
	}
	certificate := tlsInfo.State.VerifiedChains[0][0]
	spiffeID, err := exactSPIFFEURI(certificate)
	if err != nil || spiffeID != restoreControllerSPIFFEForTransport {
		return service.ControllerIdentity{}, errors.New("restore controller SPIFFE identity rejected")
	}
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return service.ControllerIdentity{}, errors.New("restore controller signing key is not ES256")
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	keyID := "controller-tls-" + hex.EncodeToString(fingerprint[:12])
	return service.ControllerIdentity{
		SPIFFEID:   spiffeID,
		Key:        internalrpcauth.ES256Key{KeyID: keyID, Public: publicKey},
		Generation: generation,
	}, nil
}

// restoreControllerSPIFFEForTransport fixes the one accepted publisher controller identity; another
// trusted-domain workload is not an interchangeable controller.
// restoreControllerSPIFFEForTransport закрепляет единственную принимаемую publisher controller identity;
// другой workload доверенного domain не считается тем же controller.
const restoreControllerSPIFFEForTransport = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-controller"
