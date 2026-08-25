package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	restoreDirectiveType         = "kodex-internal-rpc-restore-directive+jws"
	restoreACKType               = "kodex-internal-rpc-restore-quiescence-ack+jws"
	restoreDirectiveTTL          = 30 * time.Second
	restoreACKTTL                = 30 * time.Second
	restoreControllerIssuer      = restoreControllerSPIFFE
	restoreWorkloadAudience      = "urn:kodex:internal-rpc-authority-restore-workload"
	restoreRoleCredentialPurpose = "RESTORE_ROLE_CREDENTIAL"
	restoreOperatorSubject       = "system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator"
	restoreOperatorAudience      = "urn:kodex:internal-rpc-authority-restore-controller"
)

// RestorePeer описывает проверенную mTLS-идентичность caller.
type RestorePeer struct {
	SPIFFEID string
}

// RestoreController координирует остановку workload и устойчивое ограждение.
type RestoreController struct {
	databaseClusterID    string
	controllerKey        internalrpcauth.ES256Key
	controllerGeneration uint64
	targetRegistry       model.DeliveryTargetRegistry
	roleCredentialKeys   map[string]VerificationKeyRecord
	roleTrustMetadata    model.RestoreRoleTrustMetadata
	coordination         repository.RestoreCoordinationStore
	fence                repository.RestoreFenceStore
	publisher            repository.RestoreCredentialPublisher
	evidence             repository.RestoreEvidenceVerifier
	now                  func() time.Time
}

// RestoreDirectiveResult содержит директиву и текущее состояние.
type RestoreDirectiveResult struct {
	NoDirective      bool
	State            model.RestoreState
	DirectiveCompact string
	ExpiresAt        time.Time
}

// RestoreACKResult содержит принятую запись и новое состояние.
type RestoreACKResult struct {
	State   model.RestoreState
	Receipt model.RestoreACKRecord
}

// AuthorizeOperator связывает проверенный TokenReview result с exact command.
func (controller *RestoreController) AuthorizeOperator(
	ctx context.Context,
	credential model.RestoreOperatorCredential,
	fullMethod string,
	idempotencyKey string,
	semanticDigest string,
) error {
	if credential.Subject != restoreOperatorSubject ||
		credential.Namespace != "kodex-system" ||
		credential.ServiceAccount != "internal-rpc-authority-restore-operator" ||
		credential.Audience != restoreOperatorAudience ||
		!digestPattern.MatchString(credential.TokenDigestSHA256) ||
		(fullMethod !=
			"/internalrpcauthority.v1.RestoreControllerService/PrepareRestore" &&
			fullMethod !=
				"/internalrpcauthority.v1.RestoreControllerService/CompleteRestore") ||
		!uuidPattern.MatchString(idempotencyKey) ||
		!digestPattern.MatchString(semanticDigest) {
		return failure.New(
			failure.Unauthenticated,
			"restore operator application credential binding rejected",
		)
	}
	err := controller.coordination.AuthorizeOperator(
		ctx,
		model.RestoreOperatorAuthorizationRecord{
			TokenDigestSHA256:    credential.TokenDigestSHA256,
			Subject:              credential.Subject,
			FullMethod:           fullMethod,
			IdempotencyKey:       idempotencyKey,
			SemanticDigestSHA256: semanticDigest,
			AuthorizedAt:         controller.now().UTC().Unix(),
		},
	)
	if err != nil {
		return mapRestorePersistence(
			"reserve restore operator application credential",
			err,
		)
	}
	return nil
}

// NewRestoreController создаёт контроллер из устойчивых границ состояния.
func NewRestoreController(
	databaseClusterID string,
	controllerKey internalrpcauth.ES256Key,
	controllerGeneration uint64,
	targetRegistry model.DeliveryTargetRegistry,
	roleCredentialKeys map[string]VerificationKeyRecord,
	roleTrustMetadata model.RestoreRoleTrustMetadata,
	coordination repository.RestoreCoordinationStore,
	fence repository.RestoreFenceStore,
	publisher repository.RestoreCredentialPublisher,
	evidence repository.RestoreEvidenceVerifier,
) (*RestoreController, error) {
	if databaseClusterID != "internal-rpc-authority-primary" ||
		controllerKey.Private == nil ||
		controllerGeneration == 0 ||
		targetRegistry.Version != model.ContractVersion ||
		targetRegistry.SourceRevision == 0 ||
		!digestPattern.MatchString(targetRegistry.SourceDigest) ||
		len(targetRegistry.Targets) == 0 ||
		len(roleCredentialKeys) == 0 ||
		roleTrustMetadata.SourceRevision == 0 ||
		!digestPattern.MatchString(roleTrustMetadata.SourceDigest) ||
		roleTrustMetadata.KeySetRevision == 0 ||
		roleTrustMetadata.SignerGeneration == 0 ||
		coordination == nil ||
		fence == nil ||
		publisher == nil ||
		evidence == nil {
		return nil, errors.New("invalid restore controller configuration")
	}
	return &RestoreController{
		databaseClusterID:    databaseClusterID,
		controllerKey:        controllerKey,
		controllerGeneration: controllerGeneration,
		targetRegistry:       targetRegistry,
		roleCredentialKeys:   roleCredentialKeys,
		roleTrustMetadata:    roleTrustMetadata,
		coordination:         coordination,
		fence:                fence,
		publisher:            publisher,
		evidence:             evidence,
		now:                  time.Now,
	}, nil
}

// Prepare начинает цикл восстановления и переводит его в QUIESCING.
func (controller *RestoreController) Prepare(
	ctx context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	if !uuidPattern.MatchString(command.RestoreID) ||
		command.DatabaseClusterID != controller.databaseClusterID ||
		!digestPattern.MatchString(command.BackupManifestDigest) ||
		command.RecoveryTarget.IsZero() ||
		!uuidPattern.MatchString(command.IdempotencyKey) {
		return model.RestoreState{}, failure.New(
			failure.InvalidRequest,
			"restore prepare request is invalid",
		)
	}
	command.Now = controller.now().UTC().Truncate(time.Second)
	expected := make(map[string]model.RestoreExpectedTarget, len(controller.targetRegistry.Targets))
	for id, target := range controller.targetRegistry.Targets {
		expected[id] = model.RestoreExpectedTarget{
			TargetID:             id,
			WorkloadID:           target.WorkloadID,
			WorkloadSPIFFEID:     target.WorkloadSPIFFEID,
			Role:                 target.Role,
			WorkloadGeneration:   target.WorkloadGeneration,
			CredentialGeneration: target.CredentialGeneration,
			ACKKeyGeneration:     target.CredentialGeneration,
		}
	}
	command.ExpectedTargets = expected
	command.ControllerGeneration = controller.controllerGeneration
	command.WorkloadSetRevision = controller.targetRegistry.SourceRevision
	state, err := controller.coordination.Prepare(ctx, command)
	if err != nil {
		return model.RestoreState{}, mapRestorePersistence("prepare durable restore coordination", err)
	}
	if err := controller.fence.ApplyRestoreFence(ctx, state); err != nil {
		return model.RestoreState{}, mapRestorePersistence("apply QUIESCING restore fence", err)
	}
	targetIDs := make([]string, 0, len(expected))
	for id := range expected {
		targetIDs = append(targetIDs, id)
	}
	sort.Strings(targetIDs)
	for _, targetID := range targetIDs {
		issuance, issuanceErr := controller.ensureIssuance(ctx, state, targetID)
		if issuanceErr != nil {
			return model.RestoreState{}, issuanceErr
		}
		target := expected[targetID]
		directive := model.CredentialIssuanceDirective{
			Version:                model.ContractVersion,
			Issuer:                 restoreControllerIssuer,
			Audience:               restorePublisherAudience,
			Subject:                target.WorkloadID,
			JTI:                    issuance.JTI,
			RestoreID:              state.RestoreID,
			RestoreEpoch:           state.RestoreEpoch,
			CoordinationRevision:   state.CoordinationRevision,
			TargetRegistryRevision: controller.targetRegistry.SourceRevision,
			TargetRegistryDigest:   controller.targetRegistry.SourceDigest,
			DeliveryTargetID:       target.TargetID,
			WorkloadID:             target.WorkloadID,
			WorkloadSPIFFEID:       target.WorkloadSPIFFEID,
			Role:                   target.Role,
			WorkloadGeneration:     target.WorkloadGeneration,
			CredentialGeneration:   target.CredentialGeneration,
			ACKKeyGeneration:       target.ACKKeyGeneration,
			IssuedAt:               issuance.IssuedAt,
			NotBefore:              issuance.IssuedAt,
			ExpiresAt:              time.Unix(issuance.IssuedAt, 0).Add(restoreIssuanceTTL).Unix(),
		}
		compact, signErr := internalrpcauth.SignCanonicalJSON(
			directive,
			controller.controllerKey,
			internalrpcauth.ProtectedHeaderExpectation{
				Type:  restoreIssuanceType,
				KeyID: controller.controllerKey.KeyID,
			},
		)
		if signErr != nil {
			return model.RestoreState{}, failure.Wrap(
				failure.Internal,
				"sign restore credential issuance directive",
				signErr,
			)
		}
		delivery, publishErr := controller.publisher.PublishRoleCredential(
			ctx,
			compact,
			issuance.IdempotencyKey,
		)
		if publishErr != nil {
			return model.RestoreState{}, failure.Wrap(
				failure.PersistenceUnavailable,
				"publish restore role credential",
				publishErr,
			)
		}
		delivery.TargetID = targetID
		state, err = controller.coordination.RecordDelivery(ctx, state.RestoreID, delivery)
		if err != nil {
			return model.RestoreState{}, mapRestorePersistence(
				"persist restore credential delivery",
				err,
			)
		}
	}
	return state, nil
}

// GetDirective возвращает workload только его действующую директиву.
func (controller *RestoreController) GetDirective(
	ctx context.Context,
	peer RestorePeer,
	roleCredentialCompact string,
	observedRevision uint64,
) (RestoreDirectiveResult, error) {
	state, err := controller.coordination.Load(ctx)
	if err != nil {
		return RestoreDirectiveResult{}, mapRestorePersistence("load restore coordination", err)
	}
	if state.Phase != "QUIESCING" || observedRevision >= state.CoordinationRevision {
		return RestoreDirectiveResult{NoDirective: true, State: state}, nil
	}
	credential, credentialDigest, err := controller.verifyRoleCredential(
		peer,
		roleCredentialCompact,
		state,
	)
	if err != nil {
		return RestoreDirectiveResult{}, err
	}
	targetID := deliveryTargetID(credential.WorkloadID, credential.Role)
	if existing, ok := state.Directives[targetID]; ok &&
		existing.ExpiresAt > controller.now().UTC().Unix() {
		return RestoreDirectiveResult{
			State:            state,
			DirectiveCompact: existing.CompactJWS,
			ExpiresAt:        time.Unix(existing.ExpiresAt, 0).UTC(),
		}, nil
	}
	jti, err := newUUID()
	if err != nil {
		return RestoreDirectiveResult{}, failure.Wrap(
			failure.Internal,
			"generate restore directive identifier",
			err,
		)
	}
	now := controller.now().UTC().Truncate(time.Second)
	claims := model.RoleBoundRestoreDirectiveClaims{
		Version:                    model.ContractVersion,
		Issuer:                     restoreControllerIssuer,
		Audience:                   restoreWorkloadAudience,
		Subject:                    credential.WorkloadID,
		JTI:                        jti,
		RestoreID:                  state.RestoreID,
		RestoreEpoch:               state.RestoreEpoch,
		CoordinationRevision:       state.CoordinationRevision,
		Phase:                      "QUIESCING",
		WorkloadID:                 credential.WorkloadID,
		WorkloadSPIFFEID:           credential.WorkloadSPIFFEID,
		Role:                       credential.Role,
		WorkloadGeneration:         credential.WorkloadGeneration,
		CredentialGeneration:       credential.CredentialGeneration,
		RoleCredentialDigestSHA256: credentialDigest,
		StopAcceptingRequired:      true,
		DrainInflightRequired:      true,
		IssuedAt:                   now.Unix(),
		NotBefore:                  now.Unix(),
		ExpiresAt:                  now.Add(restoreDirectiveTTL).Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		controller.controllerKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreDirectiveType,
			KeyID: controller.controllerKey.KeyID,
		},
	)
	if err != nil {
		return RestoreDirectiveResult{}, failure.Wrap(
			failure.Internal,
			"sign role-bound restore directive",
			err,
		)
	}
	digest := sha256.Sum256([]byte(compact))
	saved, err := controller.coordination.SaveDirective(
		ctx,
		state.RestoreID,
		model.RestoreDirectiveRecord{
			TargetID:     targetID,
			JTI:          jti,
			CompactJWS:   compact,
			DigestSHA256: hex.EncodeToString(digest[:]),
			ExpiresAt:    claims.ExpiresAt,
		},
	)
	if err != nil {
		return RestoreDirectiveResult{}, mapRestorePersistence(
			"persist role-bound restore directive",
			err,
		)
	}
	return RestoreDirectiveResult{
		State:            state,
		DirectiveCompact: saved.CompactJWS,
		ExpiresAt:        time.Unix(saved.ExpiresAt, 0).UTC(),
	}, nil
}

// Acknowledge проверяет и резервирует подтверждение остановки workload.
func (controller *RestoreController) Acknowledge(
	ctx context.Context,
	peer RestorePeer,
	roleCredentialCompact string,
	directiveCompact string,
	ackCompact string,
	idempotencyKey string,
) (RestoreACKResult, error) {
	if !uuidPattern.MatchString(idempotencyKey) {
		return RestoreACKResult{}, failure.New(
			failure.InvalidRequest,
			"restore ACK idempotency key is invalid",
		)
	}
	state, err := controller.coordination.Load(ctx)
	if err != nil {
		return RestoreACKResult{}, mapRestorePersistence("load restore coordination", err)
	}
	credential, credentialDigest, err := controller.verifyRoleCredential(
		peer,
		roleCredentialCompact,
		state,
	)
	if err != nil {
		return RestoreACKResult{}, err
	}
	targetID := deliveryTargetID(credential.WorkloadID, credential.Role)
	directive, err := controller.verifyDirective(
		directiveCompact,
		credential,
		credentialDigest,
		state,
		targetID,
	)
	if err != nil {
		return RestoreACKResult{}, err
	}
	ack, ackDigest, err := controller.verifyACK(
		ackCompact,
		credential,
		directive,
		credentialDigest,
	)
	if err != nil {
		return RestoreACKResult{}, err
	}
	semanticDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		RoleCredentialDigest string `json:"role_credential_digest_sha256"`
		DirectiveDigest      string `json:"directive_digest_sha256"`
		ACKDigest            string `json:"ack_digest_sha256"`
	}{
		RoleCredentialDigest: digestCompact(roleCredentialCompact),
		DirectiveDigest:      digestCompact(directiveCompact),
		ACKDigest:            ackDigest,
	})
	if err != nil {
		return RestoreACKResult{}, failure.Wrap(
			failure.Internal,
			"digest restore ACK request",
			err,
		)
	}
	receiptID, err := newUUID()
	if err != nil {
		return RestoreACKResult{}, failure.Wrap(
			failure.Internal,
			"generate restore ACK receipt",
			err,
		)
	}
	now := controller.now().UTC().Truncate(time.Second)
	next, receipt, err := controller.coordination.RecordACK(
		ctx,
		state.RestoreID,
		model.RestoreACKRecord{
			TargetID:              targetID,
			ReceiptID:             receiptID,
			IdempotencyKey:        idempotencyKey,
			ACKJTI:                ack.JTI,
			SemanticRequestDigest: semanticDigest,
			AcceptedACKDigest:     ackDigest,
			AcceptedAt:            now.Unix(),
		},
	)
	if err != nil {
		return RestoreACKResult{}, mapRestorePersistence("persist restore ACK", err)
	}
	if err := controller.fence.ApplyRestoreFence(ctx, next); err != nil {
		return RestoreACKResult{}, mapRestorePersistence(
			"apply restore ACK phase fence",
			err,
		)
	}
	return RestoreACKResult{State: next, Receipt: receipt}, nil
}

// Complete завершает восстановление только после полного набора подтверждений.
func (controller *RestoreController) Complete(
	ctx context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	if !uuidPattern.MatchString(command.RestoreID) ||
		command.DatabaseClusterID != controller.databaseClusterID ||
		!digestPattern.MatchString(command.BackupManifestDigest) ||
		command.RecoveryTarget.IsZero() ||
		!uuidPattern.MatchString(command.IdempotencyKey) {
		return model.RestoreState{}, failure.New(
			failure.InvalidRequest,
			"restore completion request is invalid",
		)
	}
	command.Now = controller.now().UTC().Truncate(time.Second)
	current, err := controller.coordination.Load(ctx)
	if err != nil {
		return model.RestoreState{}, mapRestorePersistence(
			"load restore state before evidence verification",
			err,
		)
	}
	if current.RestoreID != command.RestoreID ||
		current.DatabaseClusterID != command.DatabaseClusterID ||
		current.BackupManifestDigest != command.BackupManifestDigest ||
		current.RecoveryTargetUnix != command.RecoveryTarget.Unix() ||
		current.Phase != "PREPARED" {
		return model.RestoreState{}, failure.New(
			failure.OperationNotAllowed,
			"restore completion intent does not match prepared state",
		)
	}
	evidence, err := controller.evidence.VerifyCompletedEvidence(ctx, current)
	if err != nil {
		return model.RestoreState{}, failure.Wrap(
			failure.OperationNotAllowed,
			"independent PITR completion evidence rejected",
			err,
		)
	}
	command.EvidenceDigest = evidence.CompactJWSDigestSHA256
	command.EvidenceAnchor = evidence.AnchorRevision
	command.EvidenceRestoreEpoch = evidence.RestoreEpoch
	command.RestoredClusterUID = evidence.RestoredClusterUID
	command.RestoredTimelineID = evidence.RestoredTimelineID
	command.RestoreCompletedAt = evidence.RestoreCompletedAt
	state, err := controller.coordination.Complete(ctx, command)
	if err != nil {
		return model.RestoreState{}, mapRestorePersistence(
			"complete durable restore coordination",
			err,
		)
	}
	if err := controller.fence.ApplyRestoreFence(ctx, state); err != nil {
		return model.RestoreState{}, mapRestorePersistence(
			"apply completed restore fence",
			err,
		)
	}
	return state, nil
}

// Recover восстанавливает согласованность ограждения после перезапуска.
func (controller *RestoreController) Recover(ctx context.Context) error {
	state, err := controller.coordination.Load(ctx)
	if err != nil {
		return mapRestorePersistence("load restore recovery state", err)
	}
	if err := controller.fence.ApplyRestoreFence(ctx, state); err != nil {
		return mapRestorePersistence("recover durable restore fence", err)
	}
	return controller.fence.RestoreFenceReady(ctx, state)
}

// Ready сверяет устойчивое состояние координации и ограждения.
func (controller *RestoreController) Ready(ctx context.Context) (model.RestoreState, error) {
	state, err := controller.StartupReady(ctx)
	if err != nil {
		return model.RestoreState{}, err
	}
	if err := controller.publisher.PublisherReady(ctx); err != nil {
		return model.RestoreState{}, err
	}
	return state, nil
}

// StartupReady сверяет durable coordination и fence без циклической
// зависимости от readback всех workload, которые сами опрашивают controller.
func (controller *RestoreController) StartupReady(
	ctx context.Context,
) (model.RestoreState, error) {
	if err := controller.coordination.CoordinationReady(ctx); err != nil {
		return model.RestoreState{}, err
	}
	state, err := controller.coordination.Load(ctx)
	if err != nil {
		return model.RestoreState{}, err
	}
	if err := controller.fence.RestoreFenceReady(ctx, state); err != nil {
		return model.RestoreState{}, err
	}
	return state, nil
}

// RoleTrustMetadata возвращает обслуживаемую метаинформацию доверия.
func (controller *RestoreController) RoleTrustMetadata() model.RestoreRoleTrustMetadata {
	return controller.roleTrustMetadata
}

// SignerGeneration возвращает поколение ключа директив контроллера.
func (controller *RestoreController) SignerGeneration() uint64 {
	return controller.controllerGeneration
}

func (controller *RestoreController) ensureIssuance(
	ctx context.Context,
	state model.RestoreState,
	targetID string,
) (model.RestoreIssuanceRecord, error) {
	jti, err := newUUID()
	if err != nil {
		return model.RestoreIssuanceRecord{}, err
	}
	idempotencyKey, err := newUUID()
	if err != nil {
		return model.RestoreIssuanceRecord{}, err
	}
	record, err := controller.coordination.EnsureIssuance(
		ctx,
		state.RestoreID,
		model.RestoreIssuanceRecord{
			TargetID:       targetID,
			JTI:            jti,
			IdempotencyKey: idempotencyKey,
			IssuedAt:       controller.now().UTC().Truncate(time.Second).Unix(),
		},
	)
	if err != nil {
		return model.RestoreIssuanceRecord{}, mapRestorePersistence(
			"persist restore issuance directive",
			err,
		)
	}
	return record, nil
}

func (controller *RestoreController) verifyRoleCredential(
	peer RestorePeer,
	compact string,
	state model.RestoreState,
) (model.RestoreRoleCredentialClaims, string, error) {
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != restoreRoleCredentialType {
		return model.RestoreRoleCredentialClaims{}, "", restoreCredentialRejected()
	}
	record, ok := controller.roleCredentialKeys[header.KeyID]
	now := controller.now().UTC().Truncate(time.Second)
	_, audienceOK := record.Audiences[restoreControllerAudience]
	if !ok ||
		record.Issuer != restorePublisherSPIFFE ||
		record.Purpose != restoreRoleCredentialPurpose ||
		(record.Status != "CURRENT" && record.Status != "PREVIOUS") ||
		!audienceOK ||
		now.Before(record.NotBefore) ||
		!now.Before(record.NotAfter) {
		return model.RestoreRoleCredentialClaims{}, "", restoreCredentialRejected()
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		record.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreRoleCredentialType,
			KeyID: header.KeyID,
		},
	)
	if err != nil {
		return model.RestoreRoleCredentialClaims{}, "", restoreCredentialRejected()
	}
	var claims model.RestoreRoleCredentialClaims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims); err != nil ||
		internalrpcauth.ValidateTimes(
			now,
			time.Unix(claims.IssuedAt, 0),
			time.Unix(claims.NotBefore, 0),
			time.Unix(claims.ExpiresAt, 0),
			restoreRoleCredentialTTL,
			readbackAllowedClockSkew,
		) != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != record.Issuer ||
		claims.Audience != restoreControllerAudience ||
		claims.SignerGeneration != record.Generation ||
		claims.SignerKeyID != header.KeyID ||
		claims.RestoreID != state.RestoreID ||
		claims.RestoreEpoch != state.RestoreEpoch ||
		claims.CoordinationRevision > state.CoordinationRevision ||
		(state.Phase == "QUIESCING" &&
			claims.CoordinationRevision != state.CoordinationRevision) ||
		(state.Phase == "PREPARED" &&
			claims.CoordinationRevision+1 != state.CoordinationRevision) ||
		claims.WorkloadSPIFFEID != peer.SPIFFEID {
		return model.RestoreRoleCredentialClaims{}, "", restoreCredentialRejected()
	}
	targetID := deliveryTargetID(claims.WorkloadID, claims.Role)
	target, expected := state.ExpectedTargets[targetID]
	delivery, delivered := state.Deliveries[targetID]
	if !expected || !delivered ||
		target.WorkloadSPIFFEID != peer.SPIFFEID ||
		target.WorkloadGeneration != claims.WorkloadGeneration ||
		target.CredentialGeneration != claims.CredentialGeneration ||
		target.ACKKeyGeneration != claims.ACKKeyGeneration ||
		delivery.RoleCredentialDigestSHA256 != digestCompact(compact) {
		return model.RestoreRoleCredentialClaims{}, "", restoreCredentialRejected()
	}
	return claims, digestCompact(compact), nil
}

func (controller *RestoreController) verifyDirective(
	compact string,
	credential model.RestoreRoleCredentialClaims,
	credentialDigest string,
	state model.RestoreState,
	targetID string,
) (model.RoleBoundRestoreDirectiveClaims, error) {
	stored, ok := state.Directives[targetID]
	if !ok || stored.CompactJWS != compact || stored.DigestSHA256 != digestCompact(compact) {
		return model.RoleBoundRestoreDirectiveClaims{}, failure.New(
			failure.AuthorityRejected,
			"restore directive is not controller-owned",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		controller.controllerKey.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreDirectiveType,
			KeyID: controller.controllerKey.KeyID,
		},
	)
	if err != nil {
		return model.RoleBoundRestoreDirectiveClaims{}, failure.New(
			failure.AuthorityRejected,
			"restore directive signature rejected",
		)
	}
	var claims model.RoleBoundRestoreDirectiveClaims
	now := controller.now().UTC().Truncate(time.Second)
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
		internalrpcauth.ValidateTimes(
			now,
			time.Unix(claims.IssuedAt, 0),
			time.Unix(claims.NotBefore, 0),
			time.Unix(claims.ExpiresAt, 0),
			restoreDirectiveTTL,
			readbackAllowedClockSkew,
		) != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != restoreControllerIssuer ||
		claims.Audience != restoreWorkloadAudience ||
		claims.Phase != "QUIESCING" ||
		claims.RestoreID != state.RestoreID ||
		claims.RestoreEpoch != state.RestoreEpoch ||
		claims.CoordinationRevision != credential.CoordinationRevision ||
		claims.CoordinationRevision > state.CoordinationRevision ||
		(state.Phase == "QUIESCING" &&
			claims.CoordinationRevision != state.CoordinationRevision) ||
		(state.Phase == "PREPARED" &&
			claims.CoordinationRevision+1 != state.CoordinationRevision) ||
		claims.WorkloadID != credential.WorkloadID ||
		claims.WorkloadSPIFFEID != credential.WorkloadSPIFFEID ||
		claims.Role != credential.Role ||
		claims.WorkloadGeneration != credential.WorkloadGeneration ||
		claims.CredentialGeneration != credential.CredentialGeneration ||
		claims.RoleCredentialDigestSHA256 != credentialDigest ||
		!claims.StopAcceptingRequired ||
		!claims.DrainInflightRequired {
		return model.RoleBoundRestoreDirectiveClaims{}, failure.New(
			failure.BindingMismatch,
			"restore directive binding rejected",
		)
	}
	return claims, nil
}

func (controller *RestoreController) verifyACK(
	compact string,
	credential model.RestoreRoleCredentialClaims,
	directive model.RoleBoundRestoreDirectiveClaims,
	credentialDigest string,
) (model.QuiescenceACKClaims, string, error) {
	key, err := internalrpcauth.ParsePublicJWK(credential.ACKPublicJWK)
	if err != nil ||
		key.KeyID != credential.ACKKeyID {
		return model.QuiescenceACKClaims{}, "", failure.New(
			failure.AuthorityRejected,
			"restore ACK key binding rejected",
		)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(key)
	if err != nil || thumbprint != credential.ACKKeyThumbprintSHA256 {
		return model.QuiescenceACKClaims{}, "", failure.New(
			failure.AuthorityRejected,
			"restore ACK key thumbprint rejected",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreACKType,
			KeyID: credential.ACKKeyID,
		},
	)
	if err != nil {
		return model.QuiescenceACKClaims{}, "", failure.New(
			failure.AuthorityRejected,
			"restore ACK signature rejected",
		)
	}
	var claims model.QuiescenceACKClaims
	now := controller.now().UTC().Truncate(time.Second)
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
		internalrpcauth.ValidateTimes(
			now,
			time.Unix(claims.IssuedAt, 0),
			time.Unix(claims.NotBefore, 0),
			time.Unix(claims.ExpiresAt, 0),
			restoreACKTTL,
			readbackAllowedClockSkew,
		) != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != credential.WorkloadSPIFFEID ||
		claims.Audience != restoreControllerAudience ||
		claims.Subject != credential.WorkloadID ||
		!uuidPattern.MatchString(claims.JTI) ||
		claims.DirectiveJTI != directive.JTI ||
		claims.RestoreID != directive.RestoreID ||
		claims.RestoreEpoch != directive.RestoreEpoch ||
		claims.CoordinationRevision != directive.CoordinationRevision ||
		claims.WorkloadID != credential.WorkloadID ||
		claims.WorkloadSPIFFEID != credential.WorkloadSPIFFEID ||
		claims.Role != credential.Role ||
		claims.WorkloadGeneration != credential.WorkloadGeneration ||
		claims.CredentialGeneration != credential.CredentialGeneration ||
		claims.ACKKeyID != credential.ACKKeyID ||
		claims.ACKKeyGeneration != credential.ACKKeyGeneration ||
		claims.ACKKeyThumbprintSHA256 != credential.ACKKeyThumbprintSHA256 ||
		claims.RoleCredentialDigestSHA256 != credentialDigest ||
		!digestPattern.MatchString(claims.ServedSnapshotDigest) ||
		!claims.AcceptingStopped ||
		!claims.InflightDrained ||
		claims.InflightCount != 0 {
		return model.QuiescenceACKClaims{}, "", failure.New(
			failure.BindingMismatch,
			"restore ACK binding rejected",
		)
	}
	return claims, digestCompact(compact), nil
}

func restoreCredentialRejected() error {
	return failure.New(
		failure.Unauthenticated,
		"restore role credential rejected",
	)
}

func mapRestorePersistence(message string, err error) error {
	switch {
	case errors.Is(err, repository.ErrReplay):
		return failure.Wrap(failure.ReplayDetected, message, err)
	case errors.Is(err, repository.ErrIdempotencyConflict):
		return failure.Wrap(failure.OperationNotAllowed, message, err)
	case errors.Is(err, repository.ErrNotFound):
		return failure.Wrap(failure.NotFound, message, err)
	default:
		return failure.Wrap(failure.PersistenceUnavailable, message, err)
	}
}

func deliveryTargetID(workloadID, role string) string {
	value := role
	for index := range value {
		if value[index] == '_' {
			value = value[:index] + "-" + value[index+1:]
		}
	}
	return workloadID + "." + lowerASCII(value)
}

func lowerASCII(value string) string {
	buffer := []byte(value)
	for index := range buffer {
		if buffer[index] >= 'A' && buffer[index] <= 'Z' {
			buffer[index] += 'a' - 'A'
		}
	}
	return string(buffer)
}

func digestCompact(compact string) string {
	digest := sha256.Sum256([]byte(compact))
	return hex.EncodeToString(digest[:])
}
