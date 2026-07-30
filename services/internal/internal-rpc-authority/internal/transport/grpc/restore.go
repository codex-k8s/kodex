package authoritygrpc

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const restoreOperatorSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-restore-operator"

type RestoreControllerServer struct {
	internalrpcauthorityv1.UnimplementedRestoreControllerServiceServer
	application *application.RestoreController
}

func NewRestoreControllerServer(
	applicationValue *application.RestoreController,
) *RestoreControllerServer {
	return &RestoreControllerServer{application: applicationValue}
}

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
	if err := requireRestoreOperator(ctx); err != nil {
		return nil, authorizationError(restoreOperatorErrorSpec, correlationID)
	}
	semanticDigest, err := restoreCommandDigest(
		request.GetRestoreId(),
		request.GetDatabaseClusterId(),
		request.GetBackupManifestDigestSha256(),
		request.GetRecoveryTargetTime().AsTime(),
	)
	if err != nil {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
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
		request.GetRoleCredentialCompactJws() == "" ||
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
	if err := requireRestoreOperator(ctx); err != nil {
		return nil, authorizationError(restoreOperatorErrorSpec, correlationID)
	}
	semanticDigest, err := restoreCommandDigest(
		request.GetRestoreId(),
		request.GetDatabaseClusterId(),
		request.GetBackupManifestDigestSha256(),
		request.GetRecoveryTargetTime().AsTime(),
	)
	if err != nil {
		return nil, authorizationError(errorSpecMalformedRequest, correlationID)
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

func requireRestoreOperator(ctx context.Context) error {
	value, err := restorePeerSPIFFE(ctx)
	if err != nil || value != restoreOperatorSPIFFE {
		return errors.New("restore operator mTLS identity rejected")
	}
	return nil
}

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

func exactRestoreSPIFFE(certificate *x509.Certificate) (string, error) {
	return exactSPIFFEURI(certificate)
}

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
	restoreOperatorErrorSpec = errorSpec{
		code:    codes.PermissionDenied,
		message: "restore operator mTLS identity rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	restoreWorkloadErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore workload mTLS identity rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_MTLS_PEER_MISMATCH,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_MTLS_BINDING,
	}
	restoreCredentialErrorSpec = errorSpec{
		code:    codes.Unauthenticated,
		message: "restore role credential rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_ROLE_CREDENTIAL_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	restoreDirectiveErrorSpec = errorSpec{
		code:    codes.PermissionDenied,
		message: "restore directive or ACK binding rejected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_DIRECTIVE_REJECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	restoreACKReplayErrorSpec = errorSpec{
		code:    codes.AlreadyExists,
		message: "restore ACK replay detected",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_ACK_REPLAY_DETECTED,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	restoreBarrierErrorSpec = errorSpec{
		code:    codes.FailedPrecondition,
		message: "restore barrier is incomplete",
		reason:  internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_BARRIER_INCOMPLETE,
		stage:   internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
	}
	restorePersistenceErrorSpec = errorSpec{
		code:      codes.Unavailable,
		message:   "restore coordination is unavailable",
		reason:    internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_RESTORE_COORDINATION_UNAVAILABLE,
		stage:     internalrpcauthorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_RESTORE,
		retryable: true,
	}
)

func correlationFromPrepare(
	request *internalrpcauthorityv1.PrepareRestoreRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}

func correlationFromComplete(
	request *internalrpcauthorityv1.CompleteRestoreRequest,
) string {
	if request == nil {
		return ""
	}
	return request.GetCorrelationId()
}
