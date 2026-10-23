package authoritygrpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// restoreOperatorSPIFFE fixes the one workload allowed to invoke operator-only restore commands; a
	// trusted peer from another role is not sufficient.
	// restoreOperatorSPIFFE закрепляет единственный workload для operator-only restore команд; доверенного
	// peer другой роли недостаточно.
	restoreOperatorSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-operator"
	// restoreOperatorApplicationAudience pins the exact projected application-token audience reviewed
	// independently of TLS identity.
	// restoreOperatorApplicationAudience закрепляет точную audience projected application-token, проверяемую
	// независимо от TLS identity.
	restoreOperatorApplicationAudience = "urn:kodex:internal-rpc-authority-restore-controller"
)

// RestoreControllerServer adapts specialized restore commands and readback to the application while
// independently requiring operator/workload TLS and reviewed bearer credentials where applicable.
// RestoreControllerServer адаптирует специализированные restore команды и readback к application с
// независимым требованием operator/workload TLS и reviewed bearer credentials в нужных методах.
type RestoreControllerServer struct {
	internalrpcauthorityv1.UnimplementedRestoreControllerServiceServer
	application        *application.RestoreController
	credentialVerifier repository.RestoreOperatorCredentialVerifier
}

// NewRestoreControllerServer stores the application and projected-token verifier; it performs no
// admission, fencing or restore transition during construction.
// NewRestoreControllerServer сохраняет application и projected-token verifier без admission, fencing и
// restore переходов при создании.
func NewRestoreControllerServer(
	applicationValue *application.RestoreController,
	credentialVerifier repository.RestoreOperatorCredentialVerifier,
) *RestoreControllerServer {
	return &RestoreControllerServer{
		application:        applicationValue,
		credentialVerifier: credentialVerifier,
	}
}

// PrepareRestore validates its Proto envelope/time before the shared exact operator/digest/bearer gate,
// then invokes only the specialized Prepare command with the same semantic digest and idempotency key. A
// caller's cluster/restore identifiers are intent fields, not authority.
// PrepareRestore проверяет Proto envelope/time до общей точной operator/digest/bearer границы, затем
// вызывает только специализированную Prepare команду с теми же semantic digest и idempotency key. Caller
// cluster/restore идентификаторы являются intent полями, не полномочиями.
func (server *RestoreControllerServer) PrepareRestore(
	ctx context.Context,
	request *internalrpcauthorityv1.PrepareRestoreRequest,
) (*internalrpcauthorityv1.PrepareRestoreResponse, error) {
	correlationID := correlationFromPrepare(request)
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetRecoveryTargetTime() == nil ||
		request.GetRecoveryTargetTime().CheckValid() != nil ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	semanticDigest, err := server.authorizeRestoreCommand(ctx, request,
		internalrpcauthorityv1.RestoreControllerService_PrepareRestore_FullMethodName, correlationID)
	if err != nil {
		return nil, err
	}
	state, err := server.application.Prepare(
		ctx,
		model.PrepareRestoreCommand{
			RestoreID:            request.GetRestoreId(),
			DatabaseClusterID:    request.GetDatabaseClusterId(),
			BackupManifestDigest: request.GetBackupManifestDigestSha256(),
			RecoveryTarget:       request.GetRecoveryTargetTime().AsTime(),
			IdempotencyKey:       request.GetIdempotencyKey(),
			SemanticDigest:       semanticDigest,
		},
	)
	if err != nil {
		return nil, mapRestoreError(err, correlationID)
	}
	return &internalrpcauthorityv1.PrepareRestoreResponse{
		Transition: restoreTransition(state),
	}, nil
}

// GetRestoreDirective excludes the operator identity from workload polling, then delegates role
// credential and observed revision checks to the application. It returns either a typed role-bound
// directive or verified no-directive metadata with a short retry hint.
// GetRestoreDirective исключает operator identity из workload polling, затем передаёт role credential и
// observed revision проверки application. Возвращается типизированная role-bound directive либо
// проверенные no-directive metadata с короткой retry подсказкой.
func (server *RestoreControllerServer) GetRestoreDirective(
	ctx context.Context,
	request *internalrpcauthorityv1.GetRestoreDirectiveRequest,
) (*internalrpcauthorityv1.GetRestoreDirectiveResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		len(request.GetRoleCredentialCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	spiffeID, err := restorePeerSPIFFE(ctx)
	if err != nil || spiffeID == restoreOperatorSPIFFE {
		return nil, authorizationError(restoreWorkloadErrorSpec, correlationID)
	}
	result, err := server.application.GetDirective(
		ctx,
		service.RestorePeer{SPIFFEID: spiffeID},
		request.GetRoleCredentialCompactJws(),
		request.GetObservedCoordinationRevision(),
	)
	if err != nil {
		return nil, mapRestoreError(err, correlationID)
	}
	if result.NoDirective {
		return &internalrpcauthorityv1.GetRestoreDirectiveResponse{
			Result: &internalrpcauthorityv1.GetRestoreDirectiveResponse_NoDirective{
				NoDirective: &internalrpcauthorityv1.NoRestoreDirective{
					CoordinationRevision: result.State.CoordinationRevision,
					RestoreEpoch:         result.State.RestoreEpoch,
					RetryNotBefore:       timestamppb.New(time.Now().UTC().Add(2 * time.Second)),
					VerifiedTransition:   restoreTransition(result.State),
				},
			},
		}, nil
	}
	return &internalrpcauthorityv1.GetRestoreDirectiveResponse{
		Result: &internalrpcauthorityv1.GetRestoreDirectiveResponse_Directive{
			Directive: &internalrpcauthorityv1.RoleBoundRestoreDirective{
				DirectiveCompactJws:  result.DirectiveCompact,
				Transition:           restoreTransition(result.State),
				CoordinationRevision: result.State.CoordinationRevision,
				ExpiresAt:            timestamppb.New(result.ExpiresAt),
			},
		},
	}, nil
}

// AcknowledgeQuiescence validates required bounded role/directive/ACK proofs and a non-operator verified
// peer before domain one-time ACK admission, then serializes the persisted receipt and resulting
// transition.
// AcknowledgeQuiescence проверяет обязательные ограниченные role/directive/ACK proofs и проверенный
// non-operator peer до доменного одноразового ACK admission, затем сериализует сохранённую квитанцию и
// полученный переход.
func (server *RestoreControllerServer) AcknowledgeQuiescence(
	ctx context.Context,
	request *internalrpcauthorityv1.AcknowledgeQuiescenceRequest,
) (*internalrpcauthorityv1.AcknowledgeQuiescenceResponse, error) {
	correlationID := ""
	if request != nil {
		correlationID = request.GetCorrelationId()
	}
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetRoleCredentialCompactJws() == "" ||
		request.GetDirectiveCompactJws() == "" ||
		request.GetQuiescenceAckCompactJws() == "" ||
		len(request.GetRoleCredentialCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		len(request.GetDirectiveCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		len(request.GetQuiescenceAckCompactJws()) > internalrpcauth.MaxCompactJWSBytes ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	spiffeID, err := restorePeerSPIFFE(ctx)
	if err != nil || spiffeID == restoreOperatorSPIFFE {
		return nil, authorizationError(restoreWorkloadErrorSpec, correlationID)
	}
	result, err := server.application.Acknowledge(
		ctx,
		service.RestorePeer{SPIFFEID: spiffeID},
		request.GetRoleCredentialCompactJws(),
		request.GetDirectiveCompactJws(),
		request.GetQuiescenceAckCompactJws(),
		request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, mapRestoreError(err, correlationID)
	}
	return &internalrpcauthorityv1.AcknowledgeQuiescenceResponse{
		Transition: restoreTransition(result.State),
		Receipt: &internalrpcauthorityv1.QuiescenceAckReceipt{
			ReceiptId:                   result.Receipt.ReceiptID,
			IdempotencyKey:              result.Receipt.IdempotencyKey,
			AckJti:                      result.Receipt.ACKJTI,
			SemanticRequestDigestSha256: result.Receipt.SemanticRequestDigest,
			AcceptedAckDigestSha256:     result.Receipt.AcceptedACKDigest,
			CoordinationRevision:        result.State.CoordinationRevision,
			ResultingPhase:              restorePhase(result.Receipt.ResultingPhase),
			AcceptedAt:                  timestamppb.New(time.Unix(result.Receipt.AcceptedAt, 0).UTC()),
		},
	}, nil
}

// CompleteRestore validates its own Proto envelope/time, applies the same ordered operator/digest/bearer
// gate with the Complete method constant and invokes only the specialized domain Complete command.
// Barrier/terminal eligibility remains domain-owned.
// CompleteRestore проверяет свой Proto envelope/time, применяет ту же упорядоченную
// operator/digest/bearer границу с константой Complete метода и вызывает только специализированную
// domain Complete команду. Допустимость barrier/terminal остаётся доменной.
func (server *RestoreControllerServer) CompleteRestore(
	ctx context.Context,
	request *internalrpcauthorityv1.CompleteRestoreRequest,
) (*internalrpcauthorityv1.CompleteRestoreResponse, error) {
	correlationID := correlationFromComplete(request)
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetRecoveryTargetTime() == nil ||
		request.GetRecoveryTargetTime().CheckValid() != nil ||
		!validCorrelation(correlationID) {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
	}
	semanticDigest, err := server.authorizeRestoreCommand(ctx, request,
		internalrpcauthorityv1.RestoreControllerService_CompleteRestore_FullMethodName, correlationID)
	if err != nil {
		return nil, err
	}
	state, err := server.application.Complete(
		ctx,
		model.CompleteRestoreCommand{
			RestoreID:            request.GetRestoreId(),
			DatabaseClusterID:    request.GetDatabaseClusterId(),
			BackupManifestDigest: request.GetBackupManifestDigestSha256(),
			RecoveryTarget:       request.GetRecoveryTargetTime().AsTime(),
			IdempotencyKey:       request.GetIdempotencyKey(),
			SemanticDigest:       semanticDigest,
		},
	)
	if err != nil {
		return nil, mapRestoreError(err, correlationID)
	}
	return &internalrpcauthorityv1.CompleteRestoreResponse{
		Transition: restoreTransition(state),
	}, nil
}

// CheckReadiness requires exact operator TLS and a reviewed bearer token before querying durable
// coordination readiness and served role/ACK trust metadata. It reviews the token but does not reserve a
// state-changing command here.
// CheckReadiness требует точные operator TLS и reviewed bearer token до запроса durable coordination
// readiness и served role/ACK trust metadata. Token проверяется, но state-changing команда здесь не
// резервируется.
func (server *RestoreControllerServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.RestoreControllerServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.RestoreControllerServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, authorizationError(errorSpecMalformedRequest, "")
	}
	if err := requireRestoreOperator(ctx); err != nil {
		return nil, authorizationError(restoreOperatorErrorSpec, "")
	}
	if _, err := server.reviewOperatorCredential(ctx); err != nil {
		return nil, authorizationError(restoreOperatorErrorSpec, "")
	}
	state, err := server.application.Ready(ctx)
	if err != nil {
		return &internalrpcauthorityv1.RestoreControllerServiceCheckReadinessResponse{
			Ready: false,
		}, nil
	}
	trust := server.application.RoleTrustMetadata()
	return &internalrpcauthorityv1.RestoreControllerServiceCheckReadinessResponse{
		Ready:                               true,
		AnchorRevision:                      state.AnchorRevision,
		RestoreEpoch:                        state.RestoreEpoch,
		ServedEvidenceDigestSha256:          state.EvidenceDigest,
		SignerGeneration:                    server.application.SignerGeneration(),
		AdmissionPolicyObserved:             true,
		RoleCredentialTrustSourceRevision:   trust.SourceRevision,
		RoleCredentialTrustDigestSha256:     trust.SourceDigest,
		RoleCredentialTrustSignerGeneration: trust.SignerGeneration,
		RoleCredentialTrustReadbackReady:    true,
		AckVerificationRegistryReady:        true,
		RoleCredentialTrustKeySetRevision:   trust.KeySetRevision,
	}, nil
}

// restoreCommandRequest exposes only the common input getters of already shape-validated restore
// requests. These payload fields bind a semantic digest; authority still comes from verified mTLS,
// bearer review and the domain's method/idempotency reservation, never this interface.
// restoreCommandRequest предоставляет только общие getters уже проверенных по форме restore запросов.
// Payload поля связывают semantic digest; полномочия по-прежнему задают проверенные mTLS, bearer review
// и доменное резервирование method/idempotency, а не этот интерфейс.
type restoreCommandRequest interface {
	// GetRestoreId supplies semantic input identity, not proof of ownership of that restore.
	// GetRestoreId задаёт semantic input identity, не доказательство владения restore.
	GetRestoreId() string
	// GetDatabaseClusterId supplies the cluster bound into the semantic request digest.
	// GetDatabaseClusterId задаёт cluster, включаемый в semantic request digest.
	GetDatabaseClusterId() string
	// GetBackupManifestDigestSha256 supplies the exact backup intent digest checked by the domain.
	// GetBackupManifestDigestSha256 задаёт точный backup intent digest, проверяемый доменом.
	GetBackupManifestDigestSha256() string
	// GetRecoveryTargetTime is nonnil and Proto-valid before the shared authorization path is entered.
	// GetRecoveryTargetTime непуст и корректен по Proto до входа в общий authorization путь.
	GetRecoveryTargetTime() *timestamppb.Timestamp
	// GetIdempotencyKey binds operator authorization and the subsequent specialized domain command.
	// GetIdempotencyKey связывает operator авторизацию и следующую специализированную доменную команду.
	GetIdempotencyKey() string
}

// authorizeRestoreCommand preserves the shared operator-peer, digest and bearer authorization order
// for Prepare and Complete after each handler validates its own Proto envelope and timestamp. It
// changes no restore state and leaves the two domain commands and their error mappings separate.
// authorizeRestoreCommand сохраняет общий порядок operator-peer, digest и bearer авторизации Prepare
// и Complete после проверки каждым handler своего Proto envelope и timestamp. Restore состояние здесь
// не изменяется; две доменные команды и преобразование их ошибок остаются раздельными.
func (server *RestoreControllerServer) authorizeRestoreCommand(
	ctx context.Context,
	request restoreCommandRequest,
	fullMethod, correlationID string,
) (string, error) {
	if err := requireRestoreOperator(ctx); err != nil {
		return "", authorizationError(restoreOperatorErrorSpec, correlationID)
	}
	semanticDigest, err := restoreCommandDigest(
		request.GetRestoreId(),
		request.GetDatabaseClusterId(),
		request.GetBackupManifestDigestSha256(),
		request.GetRecoveryTargetTime().AsTime(),
	)
	if err != nil {
		return "", authorizationError(errorSpecMalformedRequest, correlationID)
	}
	if err := server.authorizeRestoreOperator(
		ctx,
		fullMethod,
		request.GetIdempotencyKey(),
		semanticDigest,
	); err != nil {
		return "", mapRestoreError(err, correlationID)
	}
	return semanticDigest, nil
}

// authorizeRestoreOperator reviews the projected bearer independently of transport identity and then
// delegates exact method/idempotency/semantic-digest reservation to the domain; review failure is
// wrapped as unauthenticated without raw token data.
// authorizeRestoreOperator проверяет projected bearer независимо от transport identity, затем передаёт
// домену резервирование точных method/idempotency/semantic-digest; review отказ оборачивается как
// unauthenticated без сырых token данных.
func (server *RestoreControllerServer) authorizeRestoreOperator(
	ctx context.Context,
	fullMethod string,
	idempotencyKey string,
	semanticDigest string,
) error {
	credential, err := server.reviewOperatorCredential(ctx)
	if err != nil {
		return failure.Wrap(
			failure.Unauthenticated,
			"restore operator application credential rejected",
			err,
		)
	}
	return server.application.AuthorizeOperator(
		ctx,
		credential,
		fullMethod,
		idempotencyKey,
		semanticDigest,
	)
}

// reviewOperatorCredential requires a configured verifier and exactly one nonempty Bearer metadata
// value, then verifies it for the fixed restore audience. It does not accept a token from request
// payload or treat TLS as bearer review.
// reviewOperatorCredential требует настроенный verifier и ровно одно непустое Bearer metadata значение,
// затем проверяет его для фиксированной restore audience. Token из request payload не принимается, TLS
// не считается bearer review.
func (server *RestoreControllerServer) reviewOperatorCredential(
	ctx context.Context,
) (model.RestoreOperatorCredential, error) {
	if server.credentialVerifier == nil {
		return model.RestoreOperatorCredential{}, errors.New(
			"restore operator credential verifier is unavailable",
		)
	}
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 ||
		!strings.HasPrefix(values[0], "Bearer ") ||
		len(values[0]) <= len("Bearer ") {
		return model.RestoreOperatorCredential{}, errors.New(
			"restore operator bearer credential is absent",
		)
	}
	return server.credentialVerifier.VerifyOperatorCredential(
		ctx,
		strings.TrimPrefix(values[0], "Bearer "),
		restoreOperatorApplicationAudience,
	)
}

// requireRestoreOperator requires the exact operator SPIFFE returned by the verified-chain parser; it
// does not replace application-token review or domain command authorization.
// requireRestoreOperator требует точный operator SPIFFE, возвращённый verified-chain parser;
// application-token review и domain command авторизация не заменяются.
func requireRestoreOperator(ctx context.Context) error {
	value, err := restorePeerSPIFFE(ctx)
	if err != nil || value != restoreOperatorSPIFFE {
		return errors.New("restore operator mTLS identity rejected")
	}
	return nil
}

// restorePeerSPIFFE extracts one leaf SPIFFE URI from already verified TLS chains and preserves
// restore-specific absent-peer/chain errors; role eligibility is enforced by its caller.
// restorePeerSPIFFE извлекает один leaf SPIFFE URI из уже проверенной TLS chain и сохраняет
// restore-specific ошибки отсутствия peer/chain; role eligibility проверяет вызывающий.
func restorePeerSPIFFE(ctx context.Context) (string, error) {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return "", errors.New("restore mTLS peer is absent")
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok ||
		len(tlsInfo.State.VerifiedChains) != 1 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", errors.New("restore verified mTLS chain is absent")
	}
	return exactSPIFFEURI(tlsInfo.State.VerifiedChains[0][0])
}

// restoreCommandDigest hashes canonical JSON of restore/cluster/backup intent and a UTC second-truncated
// recovery target, so subsecond timestamp differences intentionally do not change this semantic binding.
// The digest is not an operator credential.
// restoreCommandDigest хеширует canonical JSON restore/cluster/backup intent и recovery target,
// усечённого до UTC секунды; subsecond различия намеренно не меняют semantic привязку. Digest не
// является operator credential.
func restoreCommandDigest(
	restoreID string,
	databaseClusterID string,
	backupDigest string,
	recoveryTarget time.Time,
) (string, error) {
	return internalrpcauth.CanonicalJSONSHA256(struct {
		RestoreID         string `json:"restore_id"`
		DatabaseClusterID string `json:"database_cluster_id"`
		BackupDigest      string `json:"backup_manifest_digest_sha256"`
		RecoveryTarget    int64  `json:"recovery_target_unix"`
	}{
		RestoreID:         restoreID,
		DatabaseClusterID: databaseClusterID,
		BackupDigest:      backupDigest,
		RecoveryTarget:    recoveryTarget.UTC().Truncate(time.Second).Unix(),
	})
}

// restoreTransition serializes domain-returned restore identity, phase, monotonic metadata and an
// optional safe-window timestamp; a zero safe window remains absent, not Unix epoch.
// restoreTransition сериализует доменные restore identity, phase, monotonic metadata и optional
// safe-window timestamp; нулевое safe window остаётся отсутствующим, не Unix epoch.
func restoreTransition(
	state model.RestoreState,
) *internalrpcauthorityv1.RestoreTransition {
	var safeWindow *timestamppb.Timestamp
	if state.SafeWindowNotBefore != 0 {
		safeWindow = timestamppb.New(
			time.Unix(state.SafeWindowNotBefore, 0).UTC(),
		)
	}
	return &internalrpcauthorityv1.RestoreTransition{
		RestoreId:            state.RestoreID,
		Phase:                restorePhase(state.Phase),
		AnchorRevision:       state.AnchorRevision,
		RestoreEpoch:         state.RestoreEpoch,
		EvidenceDigestSha256: state.EvidenceDigest,
		SafeWindowNotBefore:  safeWindow,
	}
}

// restorePhase maps the closed persisted restore phase vocabulary to Proto, including fenced safe
// window, and leaves unknown state unspecified instead of manufacturing terminal success.
// restorePhase преобразует закрытый словарь persisted restore phase в Proto, включая fenced safe window,
// и оставляет неизвестное состояние unspecified без выдуманного terminal успеха.
func restorePhase(value string) internalrpcauthorityv1.RestorePhase {
	switch value {
	case "OPEN":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_OPEN
	case "QUIESCING":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_QUIESCING
	case "PREPARED":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_PREPARED
	case "RESTORING":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_RESTORING
	case "COMPLETED":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_COMPLETED
	case "FENCED_SAFE_WINDOW":
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_FENCED_SAFE_WINDOW
	default:
		return internalrpcauthorityv1.RestorePhase_RESTORE_PHASE_UNSPECIFIED
	}
}

// mapRestoreError keeps operator credential, directive/ACK, replay, barrier and persistence failures
// distinct through fixed typed details; unrecognized causes become Internal rather than leaking storage
// or proof text.
// mapRestoreError разделяет operator credential, directive/ACK, replay, barrier и persistence отказы
// фиксированными typed details; неизвестная причина становится Internal без утечки storage или proof
// текста.
func mapRestoreError(err error, correlationID string) error {
	switch {
	case failure.IsKind(err, failure.InvalidRequest):
		return authorizationError(errorSpecMalformedRequest, correlationID)
	case failure.IsKind(err, failure.Unauthenticated):
		return authorizationError(restoreCredentialErrorSpec, correlationID)
	case failure.IsKind(err, failure.PermissionDenied),
		failure.IsKind(err, failure.BindingMismatch),
		failure.IsKind(err, failure.AuthorityRejected):
		return authorizationError(restoreDirectiveErrorSpec, correlationID)
	case failure.IsKind(err, failure.ReplayDetected):
		return authorizationError(restoreACKReplayErrorSpec, correlationID)
	case failure.IsKind(err, failure.OperationNotAllowed),
		failure.IsKind(err, failure.NotFound):
		return authorizationError(restoreBarrierErrorSpec, correlationID)
	case failure.IsKind(err, failure.PersistenceUnavailable):
		return authorizationError(restorePersistenceErrorSpec, correlationID)
	default:
		return authorizationError(errorSpecInternal, correlationID)
	}
}

var (
	// restoreOperatorErrorSpec rejects a non-operator transport identity with PermissionDenied at the
	// mTLS-binding stage, separately from bearer rejection.
	// restoreOperatorErrorSpec отклоняет non-operator transport identity как PermissionDenied на
	// mTLS-binding stage отдельно от bearer отказа.
	restoreOperatorErrorSpec = errorSpec{
		code:    codes.PermissionDenied,
		message: "restore operator mTLS identity rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	// restoreWorkloadErrorSpec rejects invalid workload TLS on role-bound directive/ACK paths, including
	// inappropriate use of the operator identity.
	// restoreWorkloadErrorSpec отклоняет неверный workload TLS на role-bound directive/ACK путях, включая
	// неуместную operator identity.
	restoreWorkloadErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore workload mTLS identity rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	// restoreCredentialErrorSpec classifies rejected restore application/role credentials as unauthenticated
	// with a fixed restore-stage message.
	// restoreCredentialErrorSpec классифицирует отклонённые restore application/role credentials как
	// unauthenticated с фиксированным restore-stage сообщением.
	restoreCredentialErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore role credential rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_ROLE_CREDENTIAL_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	// restoreDirectiveErrorSpec maps directive/ACK authority or binding mismatch to PermissionDenied without
	// returning signed payload contents.
	// restoreDirectiveErrorSpec преобразует directive/ACK authority либо binding mismatch в PermissionDenied
	// без возврата содержимого signed payload.
	restoreDirectiveErrorSpec = errorSpec{
		code:    codes.PermissionDenied,
		message: "restore directive or ACK binding rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_DIRECTIVE_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	// restoreACKReplayErrorSpec reports a repeated restore ACK as AlreadyExists with a specific replay
	// reason rather than an accepted second transition.
	// restoreACKReplayErrorSpec сообщает о повторном restore ACK как AlreadyExists с отдельной replay
	// причиной, а не о принятом втором переходе.
	restoreACKReplayErrorSpec = errorSpec{
		code:    codes.AlreadyExists,
		message: "restore ACK replay detected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_ACK_REPLAY_DETECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	// restoreBarrierErrorSpec maps an incomplete or ineligible restore barrier to FailedPrecondition instead
	// of implying recovery is complete.
	// restoreBarrierErrorSpec преобразует неполный либо недопустимый restore barrier в FailedPrecondition
	// без утверждения о завершённом recovery.
	restoreBarrierErrorSpec = errorSpec{
		code:    codes.FailedPrecondition,
		message: "restore barrier is incomplete",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_BARRIER_INCOMPLETE,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	// restorePersistenceErrorSpec marks unavailable coordination persistence as retryable Unavailable while
	// retaining the domain's exact idempotency semantics.
	// restorePersistenceErrorSpec помечает недоступную coordination persistence как retryable Unavailable с
	// сохранением точной доменной idempotency семантики.
	restorePersistenceErrorSpec = errorSpec{
		code:      codes.Unavailable,
		message:   "restore coordination is unavailable",
		reason:    internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_COORDINATION_UNAVAILABLE,
		stage:     internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
		retryable: true,
	}
)

// correlationFromPrepare extracts Prepare correlation or empty on nil before envelope validation; it
// supplies diagnostics, not a trusted authority selector.
// correlationFromPrepare извлекает Prepare correlation либо пустое значение для nil до envelope
// проверки; это diagnostic данные, не доверенный authority selector.
func correlationFromPrepare(
	request *internalrpcauthorityv1.PrepareRestoreRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

// correlationFromComplete extracts Complete correlation without dereferencing a nil request and leaves
// validation to the handler.
// correlationFromComplete извлекает Complete correlation без разыменования nil request, оставляя
// проверку handler.
func correlationFromComplete(
	request *internalrpcauthorityv1.CompleteRestoreRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}
