package authoritygrpc

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	correlationPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	certificatePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// IssuerServer адаптирует выпуск контекста к gRPC.
type IssuerServer struct {
	internalrpcauthorityv1.UnimplementedAuthorizationIssuerServiceServer
	application *application.Authority
}

// NewIssuerServer создаёт сервер выпуска контекста.
func NewIssuerServer(applicationValue *application.Authority) *IssuerServer {
	return &IssuerServer{application: applicationValue}
}

// IssueAuthorizationContext выпускает контекст для проверенного UDS peer.
func (server *IssuerServer) IssueAuthorizationContext(
	ctx context.Context,
	request *internalrpcauthorityv1.IssueAuthorizationContextRequest,
) (*internalrpcauthorityv1.IssueAuthorizationContextResponse, error) {
	correlationID := correlationFromIssuer(request)
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetOperationId() == "" ||
		len(request.GetOperationId()) > 128 ||
		request.GetAuthorityProofCompactJws() == "" ||
		len(request.GetAuthorityProofCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	result, err := server.application.Issue(ctx, application.IssueCommand{
		OperationID:   request.GetOperationId(),
		ProofCompact:  request.GetAuthorityProofCompactJws(),
		RequestDigest: request.GetRequestDigestSha256(),
	})
	if err != nil {
		return nil, mapError(err, correlationID)
	}
	return &internalrpcauthorityv1.IssueAuthorizationContextResponse{
		CompactJws:          result.Compact,
		ExpiresAt:           timestamppb.New(result.Claims.ExpiryTime()),
		SourceRevision:      result.Claims.SourceRevision,
		SourceDigestSha256:  result.Claims.SourceDigestSHA256,
		KeySetRevision:      result.Claims.KeySetRevision,
		PolicyRevision:      result.Claims.PolicyRevision,
		SignerGeneration:    result.Claims.SignerGeneration,
		AuthorityAbiVersion: result.Claims.AuthorityABIVersion,
	}, nil
}

// IssueContinuationAuthorizationContext выпускает child context из принятого parent.
func (server *IssuerServer) IssueContinuationAuthorizationContext(
	ctx context.Context,
	request *internalrpcauthorityv1.IssueContinuationAuthorizationContextRequest,
) (*internalrpcauthorityv1.IssueContinuationAuthorizationContextResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil || grpcserver.HasMalformedProto(request) || request.GetOperationId() == "" ||
		request.GetParentAuthorizationContextCompactJws() == "" || request.GetRequestId() == "" ||
		len(request.GetParentAuthorizationContextCompactJws()) > internalrpcauth.MaxCompactJWSBytes || !validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	result, err := server.application.IssueContinuation(ctx, application.ContinuationCommand{
		OperationID: request.GetOperationId(), ParentCompact: request.GetParentAuthorizationContextCompactJws(),
		RequestID: request.GetRequestId(), CorrelationID: correlationID, RequestDigest: request.GetRequestDigestSha256(),
	})
	if err != nil {
		return nil, mapError(err, correlationID)
	}
	return &internalrpcauthorityv1.IssueContinuationAuthorizationContextResponse{
		CompactJws: result.Compact, ExpiresAt: timestamppb.New(result.Claims.ExpiryTime()),
		SourceRevision: result.Claims.SourceRevision, SourceDigestSha256: result.Claims.SourceDigestSHA256,
		KeySetRevision: result.Claims.KeySetRevision, PolicyRevision: result.Claims.PolicyRevision,
		SignerGeneration: result.Claims.SignerGeneration, AuthorityAbiVersion: result.Claims.AuthorityABIVersion,
	}, nil
}

// CheckReadiness проверяет тот же путь хранилища, что и рабочий RPC.
func (server *IssuerServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, authorizationError(errorSpecMalformedRequest, "")
	}
	state := server.application.SnapshotState()
	if err := server.application.Ready(ctx); err != nil {
		return &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessResponse{
			Ready: false,
		}, nil
	}
	return &internalrpcauthorityv1.AuthorizationIssuerServiceCheckReadinessResponse{
		Ready:                true,
		SourceRevision:       state.SourceRevision,
		SnapshotDigestSha256: state.SourceDigestSHA256,
		KeySetRevision:       state.KeySetRevision,
		PolicyRevision:       state.PolicyRevision,
		SignerGeneration:     state.SignerGeneration,
		AuthorityAbiVersion:  model.AuthorityABIVersion,
	}, nil
}

// VerifierServer адаптирует проверку контекста к gRPC.
type VerifierServer struct {
	internalrpcauthorityv1.UnimplementedAuthorizationVerifierServiceServer
	application *application.Authority
}

// NewVerifierServer создаёт сервер проверки контекста.
func NewVerifierServer(applicationValue *application.Authority) *VerifierServer {
	return &VerifierServer{application: applicationValue}
}

// VerifyAuthorizationContext проверяет контекст для точного RPC и mTLS peer.
func (server *VerifierServer) VerifyAuthorizationContext(
	ctx context.Context,
	request *internalrpcauthorityv1.VerifyAuthorizationContextRequest,
) (*internalrpcauthorityv1.VerifyAuthorizationContextResponse, error) {
	correlationID := correlationFromVerifier(request)
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetCompactJws() == "" ||
		len(request.GetCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		request.GetObservedFullMethod() == "" ||
		len(request.GetObservedFullMethod()) > 256 ||
		request.GetDownstreamPeer() == nil ||
		request.GetDownstreamPeer().GetSpiffeId() == "" ||
		len(request.GetDownstreamPeer().GetSpiffeId()) > 512 ||
		!certificatePattern.MatchString(request.GetDownstreamPeer().GetCertificateSha256()) ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	claims, err := server.application.Verify(ctx, application.VerifyCommand{
		Compact:               request.GetCompactJws(),
		ObservedFullMethod:    request.GetObservedFullMethod(),
		DownstreamSPIFFEID:    request.GetDownstreamPeer().GetSpiffeId(),
		ObservedRequestDigest: request.GetObservedRequestDigestSha256(),
	})
	if err != nil {
		return nil, mapError(err, correlationID)
	}
	return &internalrpcauthorityv1.VerifyAuthorizationContextResponse{
		Context: castVerifiedContext(claims),
	}, nil
}

// CheckReadiness проверяет обслуживаемый снимок и хранилище повторов.
func (server *VerifierServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, authorizationError(errorSpecMalformedRequest, "")
	}
	state := server.application.SnapshotState()
	if err := server.application.Ready(ctx); err != nil {
		return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{
			Ready:            false,
			ReplayStoreReady: false,
		}, nil
	}
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{
		Ready:                true,
		SourceRevision:       state.SourceRevision,
		SnapshotDigestSha256: state.SourceDigestSHA256,
		KeySetRevision:       state.KeySetRevision,
		PolicyRevision:       state.PolicyRevision,
		SignerGeneration:     state.SignerGeneration,
		ReplayStoreReady:     true,
		AuthorityAbiVersion:  model.AuthorityABIVersion,
	}, nil
}

func castVerifiedContext(
	claims model.AuthorizationClaims,
) *internalrpcauthorityv1.VerifiedAuthorizationContext {
	result := &internalrpcauthorityv1.VerifiedAuthorizationContext{
		ContractVersion:          uint32(claims.Version),
		Issuer:                   claims.Issuer,
		Audience:                 claims.Audience,
		Subject:                  claims.Subject,
		CallerWorkloadId:         claims.Caller.WorkloadID,
		CallerSpiffeId:           claims.Caller.SPIFFEID,
		TargetWorkloadId:         claims.Target.WorkloadID,
		TargetSpiffeId:           claims.Target.SPIFFEID,
		FullMethod:               claims.FullMethod,
		OperationId:              claims.OperationID,
		Authority:                castAuthority(claims.Authority),
		Permission:               claims.Permission,
		Jti:                      claims.JTI,
		IssuedAt:                 timestamppb.New(time.Unix(claims.IssuedAt, 0)),
		NotBefore:                timestamppb.New(time.Unix(claims.NotBefore, 0)),
		ExpiresAt:                timestamppb.New(time.Unix(claims.ExpiresAt, 0)),
		SourceRevision:           claims.SourceRevision,
		SourceDigestSha256:       claims.SourceDigestSHA256,
		KeySetRevision:           claims.KeySetRevision,
		PolicyRevision:           claims.PolicyRevision,
		SignerGeneration:         claims.SignerGeneration,
		CallerCredentialRevision: claims.CallerCredentialRevision,
		RequestDigestSha256:      claims.RequestDigestSHA256,
		AuthorityAbiVersion:      claims.AuthorityABIVersion,
		RequestBindingMode:       claims.RequestBindingMode,
	}
	if claims.Continuation != nil {
		result.Continuation = &internalrpcauthorityv1.ContinuationLineage{
			RootJti: claims.Continuation.RootJTI, RootOperationId: claims.Continuation.RootOperationID,
			RootFullMethod: claims.Continuation.RootFullMethod, RootSourceRevision: claims.Continuation.RootSourceRevision,
			RootSourceDigestSha256: claims.Continuation.RootSourceDigestSHA256, ParentJti: claims.Continuation.ParentJTI,
			ParentOperationId: claims.Continuation.ParentOperationID, ParentFullMethod: claims.Continuation.ParentFullMethod,
			RequestId: claims.Continuation.RequestID, CorrelationId: claims.Continuation.CorrelationID,
		}
	}
	if claims.CredentialAuthentication != nil {
		result.CredentialAuthenticatedAt = timestamppb.New(time.Unix(claims.CredentialAuthentication.AuthenticatedAt, 0))
		result.CredentialAcr = claims.CredentialAuthentication.ACR
		result.CredentialAmr = append([]string(nil), claims.CredentialAuthentication.AMR...)
	}
	return result
}

func castAuthority(value model.Authority) *internalrpcauthorityv1.CallerAuthority {
	result := &internalrpcauthorityv1.CallerAuthority{
		ActorKind: actorKind(value.ActorKind),
		Actor:     castIdentity(value.Actor),
		Tenant:    castIdentity(value.Tenant),
	}
	if value.Project != nil {
		result.Project = castIdentity(*value.Project)
	}
	return result
}

func castIdentity(value model.Identity) *internalrpcauthorityv1.AuthorityIdentity {
	return &internalrpcauthorityv1.AuthorityIdentity{
		Id: value.ID,
		Provenance: &internalrpcauthorityv1.AuthorityProvenance{
			Source:       authoritySource(value.Provenance.Source),
			Reference:    value.Provenance.Reference,
			Revision:     value.Provenance.Revision,
			DigestSha256: value.Provenance.DigestSHA256,
		},
	}
}

func actorKind(value string) internalrpcauthorityv1.ActorKind {
	return map[string]internalrpcauthorityv1.ActorKind{
		"HUMAN":      internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN,
		"AGENT":      internalrpcauthorityv1.ActorKind_ACTOR_KIND_AGENT,
		"SERVICE":    internalrpcauthorityv1.ActorKind_ACTOR_KIND_SERVICE,
		"AUTOMATION": internalrpcauthorityv1.ActorKind_ACTOR_KIND_AUTOMATION,
	}[value]
}

func authoritySource(value string) internalrpcauthorityv1.AuthoritySource {
	return map[string]internalrpcauthorityv1.AuthoritySource{
		"OIDC_SESSION":             internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_OIDC_SESSION,
		"MATTERMOST_EVENT":         internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_MATTERMOST_EVENT,
		"DOMAIN_STATE":             internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE,
		"AGENT_SESSION":            internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AGENT_SESSION,
		"PROCESS_RUN":              internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROCESS_RUN,
		"AUTOMATION_OCCURRENCE":    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_AUTOMATION_OCCURRENCE,
		"INTEGRATION_CONTINUATION": internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_INTEGRATION_CONTINUATION,
		"IMAGE_BUILD":              internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_IMAGE_BUILD,
		"IMAGE_ARTIFACT":           internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_IMAGE_ARTIFACT,
		"IMAGE_PROMOTION_CLAIM":    internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_IMAGE_PROMOTION_CLAIM,
		"LEGACY_MIGRATION":         internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_LEGACY_MIGRATION,
		"WORKLOAD_READINESS":       internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_WORKLOAD_READINESS,
		"PROVIDER_READBACK":        internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROVIDER_READBACK,
		"GIT_RECONCILIATION":       internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_GIT_RECONCILIATION,
		"RUNTIME_EXECUTION":        internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_RUNTIME_EXECUTION,
	}[value]
}

func validCorrelation(value string) bool {
	return value == "" || correlationPattern.MatchString(value)
}

func correlationFromIssuer(
	request *internalrpcauthorityv1.IssueAuthorizationContextRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

func correlationFromVerifier(
	request *internalrpcauthorityv1.VerifyAuthorizationContextRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

type errorSpec struct {
	code      codes.Code
	message   string
	reason    internalrpcauthorityv1.AuthorizationErrorReason
	stage     internalrpcauthorityv1.AuthorizationFailureStage
	retryable bool
}

var (
	errorSpecMalformedRequest = errorSpec{
		code:    codes.InvalidArgument,
		message: "malformed authorization request",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MALFORMED_REQUEST,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REQUEST,
	}
	errorSpecInvalidSignature = errorSpec{
		code:    codes.Unauthenticated,
		message: "invalid authorization signature",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_INVALID_SIGNATURE,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_JWS_SIGNATURE,
	}
	errorSpecOperationNotAllowed = errorSpec{
		code:    codes.PermissionDenied,
		message: "authorization operation not allowed",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_OPERATION_NOT_ALLOWED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	errorSpecAuthorityRejected = errorSpec{
		code:    codes.PermissionDenied,
		message: "authority provenance rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_AUTHORITY_PROVENANCE_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	errorSpecBindingMismatch = errorSpec{
		code:    codes.PermissionDenied,
		message: "authorization binding rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERMISSION_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	errorSpecReplay = errorSpec{
		code:    codes.Unauthenticated,
		message: "authorization context replay detected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_REPLAY_DETECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REPLAY,
	}
	errorSpecSnapshot = errorSpec{
		code:    codes.FailedPrecondition,
		message: "authorization snapshot rollback rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_SNAPSHOT_ROLLBACK,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_SNAPSHOT,
	}
	errorSpecPersistence = errorSpec{
		code:      codes.Unavailable,
		message:   "authorization persistence unavailable",
		reason:    internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERSISTENCE_UNAVAILABLE,
		stage:     internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_PERSISTENCE,
		retryable: true,
	}
	errorSpecInternal = errorSpec{
		code:    codes.Internal,
		message: "internal authorization failure",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_INTERNAL,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_INTERNAL,
	}
)

func mapError(err error, correlationID string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	case failure.IsKind(err, failure.OperationNotAllowed):
		return authorizationError(errorSpecOperationNotAllowed, correlationID)
	case failure.IsKind(err, failure.AuthorityRejected):
		return authorizationError(errorSpecAuthorityRejected, correlationID)
	case failure.IsKind(err, failure.BindingMismatch),
		failure.IsKind(err, failure.PermissionDenied):
		return authorizationError(errorSpecBindingMismatch, correlationID)
	case failure.IsKind(err, failure.ReplayDetected):
		return authorizationError(errorSpecReplay, correlationID)
	case failure.IsKind(err, failure.SnapshotRejected):
		return authorizationError(errorSpecSnapshot, correlationID)
	case failure.IsKind(err, failure.PersistenceUnavailable):
		return authorizationError(errorSpecPersistence, correlationID)
	case failure.IsKind(err, failure.Unauthenticated):
		if errors.Is(err, internalrpcauth.ErrSignature) {
			return authorizationError(errorSpecInvalidSignature, correlationID)
		}
		return authorizationError(errorSpecInvalidSignature, correlationID)
	default:
		return authorizationError(errorSpecInternal, correlationID)
	}
}

func authorizationError(spec errorSpec, correlationID string) error {
	base := status.New(spec.code, spec.message)
	withDetail, err := base.WithDetails(&internalrpcauthorityv1.AuthorizationErrorDetail{
		Reason:        spec.reason,
		Stage:         spec.stage,
		Retryable:     spec.retryable,
		CorrelationId: correlationID,
	})
	if err != nil {
		return status.Error(codes.Internal, "internal authorization failure")
	}
	return withDetail.Err()
}
