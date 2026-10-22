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

// internalAuthorizationFailureMessage is the one fixed public fallback for both unknown authority
// errors and failure to encode typed status details; private causes never supply this message.
// internalAuthorizationFailureMessage задаёт один фиксированный public fallback неизвестных authority
// ошибок и ошибок кодирования typed status details; private причины не задают этот текст.
const internalAuthorizationFailureMessage = "internal authorization failure"

var (
	// correlationPattern bounds nonempty diagnostic correlation IDs to 128 ASCII identifier characters;
	// empty IDs remain permitted by validCorrelation.
	// correlationPattern ограничивает непустые diagnostic correlation ID 128 ASCII identifier символами;
	// validCorrelation отдельно допускает пустые ID.
	correlationPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	// certificatePattern checks the lowercase SHA-256 shape of an observed certificate fingerprint; matching
	// its syntax is not proof of certificate identity or trust.
	// certificatePattern проверяет lowercase SHA-256 форму наблюдённого fingerprint сертификата;
	// синтаксическое совпадение не доказывает identity и доверие сертификату.
	certificatePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// IssuerServer adapts domain-owned issuance to generated gRPC methods without owning actor, tenant or
// policy state; proof, local peer and replay admission belong to the application/domain boundary.
// IssuerServer адаптирует доменный выпуск к generated gRPC методам без владения actor, tenant и policy
// состоянием; proof, local peer и replay admission принадлежат application/domain границе.
type IssuerServer struct {
	internalrpcauthorityv1.UnimplementedAuthorizationIssuerServiceServer
	application *application.Authority
}

// NewIssuerServer wraps the supplied authority application without starting listeners, checking
// readiness or validating a caller proof.
// NewIssuerServer оборачивает переданное authority приложение без запуска listeners, проверки readiness
// и caller proof.
func NewIssuerServer(applicationValue *application.Authority) *IssuerServer {
	return &IssuerServer{application: applicationValue}
}

// IssueAuthorizationContext validates the bounded issue envelope and correlation, then delegates
// operation/proof/request-digest admission and durable replay handling to the authority application.
// Only its issued result is serialized; payload identifiers alone grant no authority.
// IssueAuthorizationContext проверяет ограниченный issue envelope и correlation, затем передаёт
// operation/proof/request-digest admission и durable replay authority приложению. Сериализуется только
// выданный им результат; payload идентификаторы сами полномочий не дают.
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

// IssueContinuationAuthorizationContext checks the child envelope and passes parent compact context plus
// request/correlation/digest to domain continuation admission. It does not reconstruct or replace root
// actor/tenant lineage from request fields.
// IssueContinuationAuthorizationContext проверяет child envelope и передаёт parent compact context с
// request/correlation/digest в доменный continuation admission. Root actor/tenant lineage не строится и
// не заменяется из request полей.
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

// CheckReadiness rejects malformed requests, samples snapshot metadata and delegates the working
// storage/admission readiness path; failure returns Ready=false rather than exposing dependency error
// text. The sampled metadata is not a new signed snapshot.
// CheckReadiness отклоняет malformed запрос, читает snapshot metadata и передаёт проверку рабочему
// storage/admission readiness пути; отказ возвращает Ready=false без dependency error текста.
// Прочитанные metadata не являются новым signed snapshot.
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

// VerifierServer exposes context verification for a trusted local consumer reporting its observed
// RPC/peer binding; signature, policy, source monotonicity and replay consumption remain domain
// responsibilities.
// VerifierServer предоставляет проверку context доверенному local consumer, передающему наблюдённые
// RPC/peer привязки; подпись, policy, монотонность source и потребление replay остаются доменными
// обязанностями.
type VerifierServer struct {
	internalrpcauthorityv1.UnimplementedAuthorizationVerifierServiceServer
	application *application.Authority
}

// NewVerifierServer binds an authority application to the verifier transport without accepting a token
// or activating a snapshot.
// NewVerifierServer связывает authority приложение с verifier transport без принятия token и активации
// snapshot.
func NewVerifierServer(applicationValue *application.Authority) *VerifierServer {
	return &VerifierServer{application: applicationValue}
}

// VerifyAuthorizationContext checks compact/method/peer envelope bounds and fingerprint shape, then
// delegates exact verification with the observed SPIFFE ID and request digest. This adapter does not
// itself authenticate a downstream certificate from payload text; it serializes only domain-returned
// claims.
// VerifyAuthorizationContext проверяет границы compact/method/peer envelope и форму fingerprint, затем
// передаёт точную проверку с наблюдённым SPIFFE ID и request digest. Адаптер сам не аутентифицирует
// downstream сертификат по payload тексту; сериализуются только доменные claims.
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

// CheckReadiness probes application readiness before reporting the sampled served snapshot metadata and
// replay-store readiness; a dependency failure returns both readiness flags false without a gRPC
// dependency payload.
// CheckReadiness проверяет application readiness до выдачи прочитанных served snapshot metadata и
// replay-store readiness; dependency отказ возвращает оба readiness признака false без gRPC dependency
// payload.
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

// castVerifiedContext copies already verified claims, provenance, request binding and optional
// continuation/authentication metadata into the Proto reply, cloning AMR slices; it performs no
// additional signature or owner lookup.
// castVerifiedContext копирует уже проверенные claims, происхождение, request binding и optional
// continuation/authentication metadata в Proto ответ с копией AMR slices; дополнительные подпись и owner
// lookup не выполняются.
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

// castAuthority converts resolved actor/tenant identities and an optional project into the transport
// model, preserving absence of project rather than inventing a scope.
// castAuthority преобразует разрешённые actor/tenant identity и optional project в transport модель,
// сохраняя отсутствие project без выдуманного scope.
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

// castIdentity copies one resolved identity and exact provenance reference/revision/digest into Proto;
// converting these fields is not an authorization decision.
// castIdentity копирует одну разрешённую identity с точными provenance reference/revision/digest в
// Proto; преобразование полей не является authorization решением.
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

// actorKind maps the closed known actor kinds to Proto enum values; an unknown string becomes the enum's
// unspecified zero value rather than a privileged fallback.
// actorKind преобразует закрытые известные actor kinds в Proto enum; неизвестная строка становится
// unspecified нулевым значением, не privileged fallback.
func actorKind(value string) internalrpcauthorityv1.ActorKind {
	return map[string]internalrpcauthorityv1.ActorKind{
		"HUMAN":      internalrpcauthorityv1.ActorKind_ACTOR_KIND_HUMAN,
		"AGENT":      internalrpcauthorityv1.ActorKind_ACTOR_KIND_AGENT,
		"SERVICE":    internalrpcauthorityv1.ActorKind_ACTOR_KIND_SERVICE,
		"AUTOMATION": internalrpcauthorityv1.ActorKind_ACTOR_KIND_AUTOMATION,
	}[value]
}

// authoritySource maps supported provenance names to their explicit Proto values, preserving existing
// wire vocabulary without deciding whether a source is currently eligible under policy.
// authoritySource преобразует поддерживаемые provenance имена в явные Proto значения, сохраняя
// существующий wire словарь без решения о текущей policy допустимости source.
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

// validCorrelation accepts an omitted correlation ID or the bounded ASCII pattern; correlation remains
// diagnostic metadata, never a source of authorization.
// validCorrelation допускает отсутствие correlation ID либо ограниченный ASCII pattern; correlation
// остаётся diagnostic metadata, не источником полномочий.
func validCorrelation(value string) bool {
	return value == "" || correlationPattern.MatchString(value)
}

// correlationFromIssuer reads issue-request correlation without dereferencing nil so malformed-request
// errors can still carry the supplied diagnostic identifier.
// correlationFromIssuer читает issue-request correlation без разыменования nil, позволяя
// malformed-request ошибке нести переданный diagnostic identifier.
func correlationFromIssuer(
	request *internalrpcauthorityv1.IssueAuthorizationContextRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

// correlationFromVerifier extracts verifier-request correlation or empty for nil; it does not validate
// or sanitize the returned string.
// correlationFromVerifier извлекает verifier-request correlation либо пустое значение для nil без
// проверки и очистки возвращённой строки.
func correlationFromVerifier(
	request *internalrpcauthorityv1.VerifyAuthorizationContextRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

// errorSpec defines server-owned gRPC code, fixed public message, typed reason/stage and retryability;
// correlation is supplied separately by the handler.
// errorSpec задаёт server-owned gRPC code, фиксированное public сообщение, типизированные reason/stage и
// retryability; correlation передаётся handler отдельно.
type errorSpec struct {
	code      codes.Code
	message   string
	reason    internalrpcauthorityv1.AuthorizationErrorReason
	stage     internalrpcauthorityv1.AuthorizationFailureStage
	retryable bool
}

var (
	// errorSpecMalformedRequest classifies invalid transport input as InvalidArgument at the request stage
	// without echoing the proof or raw request body.
	// errorSpecMalformedRequest классифицирует неверный transport вход как InvalidArgument на request stage
	// без возврата proof и сырого request body.
	errorSpecMalformedRequest = errorSpec{
		code:    codes.InvalidArgument,
		message: "malformed authorization request",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MALFORMED_REQUEST,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REQUEST,
	}
	// errorSpecInvalidSignature returns a fixed unauthenticated signature rejection rather than serializing
	// cryptographic failure details.
	// errorSpecInvalidSignature возвращает фиксированный unauthenticated отказ подписи вместо сериализации
	// деталей криптографической ошибки.
	errorSpecInvalidSignature = errorSpec{
		code:    codes.Unauthenticated,
		message: "invalid authorization signature",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_INVALID_SIGNATURE,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_JWS_SIGNATURE,
	}
	// errorSpecOperationNotAllowed maps closed policy-operation denial to PermissionDenied and the policy
	// failure stage.
	// errorSpecOperationNotAllowed преобразует запрет policy operation в PermissionDenied и policy failure
	// stage.
	errorSpecOperationNotAllowed = errorSpec{
		code:    codes.PermissionDenied,
		message: "authorization operation not allowed",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_OPERATION_NOT_ALLOWED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	// errorSpecAuthorityRejected preserves a distinct provenance-rejection reason while exposing only a
	// fixed policy-denial message.
	// errorSpecAuthorityRejected сохраняет отдельную причину provenance отказа, выдавая только фиксированное
	// policy-denial сообщение.
	errorSpecAuthorityRejected = errorSpec{
		code:    codes.PermissionDenied,
		message: "authority provenance rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_AUTHORITY_PROVENANCE_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	// errorSpecBindingMismatch maps both binding mismatch and permission denial to the shared safe
	// permission-mismatch reason without exposing private bindings.
	// errorSpecBindingMismatch преобразует binding mismatch и permission denial в общую безопасную
	// permission-mismatch причину без раскрытия private привязок.
	errorSpecBindingMismatch = errorSpec{
		code:    codes.PermissionDenied,
		message: "authorization binding rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERMISSION_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY,
	}
	// errorSpecReplay marks a consumed/repeated authorization context as unauthenticated at the replay stage
	// rather than treating it as a transient store failure.
	// errorSpecReplay помечает потреблённый/повторный authorization context как unauthenticated на replay
	// stage, не как временный storage отказ.
	errorSpecReplay = errorSpec{
		code:    codes.Unauthenticated,
		message: "authorization context replay detected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_REPLAY_DETECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_REPLAY,
	}
	// errorSpecSnapshot reports monotonic snapshot rejection as FailedPrecondition without publishing the
	// rejected snapshot contents.
	// errorSpecSnapshot сообщает о нарушении монотонности snapshot как FailedPrecondition без публикации
	// отклонённого snapshot содержимого.
	errorSpecSnapshot = errorSpec{
		code:    codes.FailedPrecondition,
		message: "authorization snapshot rollback rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_SNAPSHOT_ROLLBACK,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_SNAPSHOT,
	}
	// errorSpecPersistence classifies unavailable authority persistence as retryable Unavailable; context
	// cancellation/deadline are handled before this category.
	// errorSpecPersistence классифицирует недоступную authority persistence как retryable Unavailable;
	// context cancellation/deadline обрабатываются раньше этой категории.
	errorSpecPersistence = errorSpec{
		code:      codes.Unavailable,
		message:   "authorization persistence unavailable",
		reason:    internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERSISTENCE_UNAVAILABLE,
		stage:     internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_PERSISTENCE,
		retryable: true,
	}
	// errorSpecInternal is the fixed closed fallback for unrecognized authorization failures; internal
	// causes never become its public message.
	// errorSpecInternal задаёт фиксированный закрытый fallback неизвестных authorization отказов; внутренние
	// причины не становятся public сообщением.
	errorSpecInternal = errorSpec{
		code:    codes.Internal,
		message: internalAuthorizationFailureMessage,
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_INTERNAL,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_INTERNAL,
	}
)

// mapError prioritizes wrapped caller cancellation/deadline over domain failure categories, then maps
// known authority failures to typed fixed details. Unknown errors use Internal; private cause text is
// not returned.
// mapError ставит wrapped caller cancellation/deadline перед доменными failure категориями, затем
// преобразует известные authority отказы в типизированные фиксированные details. Неизвестная ошибка даёт
// Internal; private cause текст не возвращается.
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

// authorizationError attaches the server-owned reason/stage/retryability and supplied correlation string
// as a typed Proto detail. It does not sanitize correlation itself; detail-encoding failure returns the
// fixed Internal fallback.
// authorizationError присоединяет server-owned reason/stage/retryability и переданную correlation строку
// как типизированный Proto detail. Correlation отдельно не очищается; ошибка detail кодирования
// возвращает фиксированный Internal fallback.
func authorizationError(spec errorSpec, correlationID string) error {
	base := status.New(spec.code, spec.message)
	withDetail, err := base.WithDetails(&internalrpcauthorityv1.AuthorizationErrorDetail{
		Reason:        spec.reason,
		Stage:         spec.stage,
		Retryable:     spec.retryable,
		CorrelationId: correlationID,
	})
	if err != nil {
		return status.Error(codes.Internal, internalAuthorizationFailureMessage)
	}
	return withDetail.Err()
}
