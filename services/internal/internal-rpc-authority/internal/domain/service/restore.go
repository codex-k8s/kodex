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
	// restoreDirectiveType pins the controller-signed role-bound quiescing-directive JWS type, distinct from issuance, role credential and workload ACK protocols.
	// restoreDirectiveType закрепляет тип JWS подписанной контроллером ролевой директивы дренирования отдельно от протоколов выдачи, role credential и ACK workload.
	restoreDirectiveType = "kodex-internal-rpc-restore-directive+jws"
	// restoreACKType identifies workload-signed quiescence evidence validated with the ACK public key pinned in its role credential.
	// restoreACKType обозначает подписанное workload доказательство дренирования, проверяемое публичным ACK-ключом из закреплённого role credential.
	restoreACKType = "kodex-internal-rpc-restore-quiescence-ack+jws"
	// restoreDirectiveTTL limits newly issued and accepted quiescing directives to thirty seconds, independently of the five-minute delivered role credential.
	// restoreDirectiveTTL ограничивает новые и принимаемые директивы дренирования тридцатью секундами независимо от пятиминутного доставленного role credential.
	restoreDirectiveTTL = 30 * time.Second
	// restoreACKTTL limits accepted workload ACK timing to thirty seconds; it does not establish the completion safe-window or physical PITR duration.
	// restoreACKTTL ограничивает время принимаемого ACK workload тридцатью секундами, не задавая окно безопасного завершения и длительность физического PITR.
	restoreACKTTL = 30 * time.Second
	// restoreControllerIssuer reuses the fixed controller SPIFFE issuer for signed issuance/directive claims, not a request-selected caller identity.
	// restoreControllerIssuer использует фиксированного SPIFFE-издателя контроллера для подписанных issuance и directive claims, а не выбранную запросом идентичность caller.
	restoreControllerIssuer = restoreControllerSPIFFE
	// restoreWorkloadAudience is the exact audience of directives sent to workloads, separate from the controller audience of role credentials and ACKs.
	// restoreWorkloadAudience — точная аудитория директив для workloads отдельно от аудитории контроллера у role credentials и ACK.
	restoreWorkloadAudience = "urn:kodex:internal-rpc-authority-restore-workload"
	// restoreRoleCredentialPurpose restricts accepted signer records to restore-role credential trust, not any otherwise valid verification key.
	// restoreRoleCredentialPurpose ограничивает принимаемые записи подписанта назначением доверия restore-role credential, а не любым корректным проверяющим ключом.
	restoreRoleCredentialPurpose = "RESTORE_ROLE_CREDENTIAL"
	// restoreOperatorSubject pins the sole operator ServiceAccount subject permitted to reserve prepare/complete application authorization after external TokenReview.
	// restoreOperatorSubject закрепляет единственный subject ServiceAccount оператора для резервирования прикладной авторизации prepare и complete после внешнего TokenReview.
	restoreOperatorSubject = "system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator"
	// restoreOperatorAudience is the exact TokenReview-derived application audience required for restore-operator command authorization.
	// restoreOperatorAudience — точная прикладная аудитория результата TokenReview для авторизации команд оператора восстановления.
	restoreOperatorAudience = "urn:kodex:internal-rpc-authority-restore-controller"
)

// RestorePeer carries the SPIFFE identity established by the authenticated transport. A string in this struct is not itself proof that mTLS or caller authorization ran.
// RestorePeer передаёт SPIFFE-идентичность, установленную аутентифицированным транспортом. Строка структуры сама не доказывает выполнение mTLS и авторизации caller.
type RestorePeer struct {
	SPIFFEID string
}

// RestoreController coordinates durable restore state, database fences, trusted role credentials, publisher delivery and independent completion evidence. It does not itself perform physical PITR or infer operator authority from command fields.
// RestoreController согласует устойчивое состояние восстановления, ограждения БД, доверенные role credentials, доставку publisher и независимые доказательства завершения. Физический PITR не выполняется и полномочия оператора из полей команды не выводятся.
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

// RestoreDirectiveResult represents either a no-directive observation with current state or a persisted signed directive and its expiry; it does not create a caller grant merely by carrying state.
// RestoreDirectiveResult представляет наблюдение без директивы с текущим состоянием либо сохранённую подписанную директиву и срок; перенос состояния сам не создаёт grant вызывающего.
type RestoreDirectiveResult struct {
	NoDirective      bool
	State            model.RestoreState
	DirectiveCompact string
	ExpiresAt        time.Time
}

// RestoreACKResult returns the coordination state and accepted ACK receipt only after the controller's record/fence steps succeed; it is not raw unverified workload input.
// RestoreACKResult возвращает состояние координации и принятую квитанцию ACK лишь после успешных шагов записи и ограждения контроллера; это не сырой непроверенный вход workload.
type RestoreACKResult struct {
	State   model.RestoreState
	Receipt model.RestoreACKRecord
}

// AuthorizeOperator validates externally reviewed operator subject/namespace/account/audience, token digest and one of two exact commands, then reserves authorization for its idempotency/semantic digest. It does not perform TokenReview itself or treat mTLS alone as operator permission.
// AuthorizeOperator проверяет внешне подтверждённые subject, namespace, account и audience оператора, дайджест токена и одну из двух точных команд, затем резервирует авторизацию по idempotency и semantic digest. TokenReview здесь не выполняется и одного mTLS недостаточно для права оператора.
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

// NewRestoreController requires the fixed database cluster, a private controller key, nonzero registry/trust generations and all durable/evidence ports. It stores supplied registry/key maps without deep cloning or performing their external cryptographic loading, and starts no restore.
// NewRestoreController требует фиксированный кластер БД, закрытый ключ контроллера, ненулевые поколения реестра и доверия и все порты состояния и доказательств. Переданные карты сохраняются без глубокого копирования и внешней криптографической загрузки; восстановление не запускается.
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

// Prepare validates intent identifiers, overwrites clock/expected targets/controller pins from trusted state, persists coordination, applies the quiescing fence and publishes/records credentials in sorted target order. These are separate durable/external steps; a later error does not undo earlier fences or deliveries.
// Prepare проверяет идентификаторы намерения, заменяет время, ожидаемые цели и привязки контроллера доверенными данными, сохраняет координацию, применяет ограждение дренирования и публикует и записывает credentials по отсортированным целям. Это отдельные устойчивые и внешние шаги; поздняя ошибка не отменяет ранние ограждения и доставки.
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

// GetDirective returns no-directive state before role verification when not QUIESCING or the supplied revision is current. Otherwise it verifies the peer-bound role, reuses an unexpired persisted target directive or signs and durably saves a fresh stop/drain directive before returning.
// GetDirective возвращает состояние без директивы до проверки роли, если фаза не QUIESCING либо переданная ревизия текущая. Иначе проверяет роль в области peer, переиспользует непросроченную сохранённую директиву цели либо подписывает и устойчиво сохраняет новую stop/drain директиву до возврата.
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

// Acknowledge validates request ID, peer-bound role, controller-owned directive and signed drained-workload evidence, then records a semantic-idempotent ACK and applies its next fence. A fence failure can follow a persisted ACK; no earlier side effect is rolled back here.
// Acknowledge проверяет ID запроса, роль в области peer, принадлежащую контроллеру директиву и подписанное доказательство дренированного workload, затем записывает семантически идемпотентный ACK и применяет следующее ограждение. Отказ ограждения может следовать за сохранённым ACK; ранние эффекты здесь не откатываются.
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

// Complete requires the exact PREPARED restore intent, obtains independent PITR evidence and overwrites caller evidence fields with that result before durable completion/fence application. It does not execute the restore or accept a caller's completion assertion as proof.
// Complete требует точное намерение восстановления PREPARED, получает независимое доказательство PITR и заменяет им переданные поля доказательства до устойчивого завершения и применения ограждения. Восстановление не выполняется и утверждение caller о завершении не принимается за доказательство.
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

// Recover reloads durable coordination, reapplies its database fence and checks exact fence readback. It restores fencing consistency rather than restoring database files or issuing new operator authority.
// Recover перечитывает устойчивую координацию, повторно применяет её ограждение БД и проверяет точный readback. Восстанавливается согласованность ограждения, а не файлы БД или новые полномочия оператора.
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

// Ready requires StartupReady's durable coordination/fence checks and additionally the publisher's working readiness path, returning no state on either failure.
// Ready требует устойчивые проверки координации и ограждения StartupReady и дополнительно рабочую готовность publisher, не возвращая состояние при любом отказе.
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

// StartupReady checks coordination readiness, loads state and verifies the exact database fence without depending on publisher/workload readback, avoiding a cyclic startup dependency.
// StartupReady проверяет готовность координации, загружает состояние и сверяет точное ограждение БД без зависимости от publisher и workload readback, избегая циклической зависимости запуска.
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

// RoleTrustMetadata returns the configured served role-trust metadata by value; it performs no fresh read, trust rotation or signature verification.
// RoleTrustMetadata возвращает настроенные метаданные обслуживаемого доверия роли по значению без свежего чтения, ротации доверия и проверки подписи.
func (controller *RestoreController) RoleTrustMetadata() model.RestoreRoleTrustMetadata {
	return controller.roleTrustMetadata
}

// SignerGeneration reports the configured controller directive-signing generation, distinct from workload ACK or publisher role-credential generations.
// SignerGeneration сообщает настроенное поколение подписи директив контроллера отдельно от поколений ACK workload и role credential publisher.
func (controller *RestoreController) SignerGeneration() uint64 {
	return controller.controllerGeneration
}

// ensureIssuance proposes fresh UUIDs and current second-truncated time to the durable target issuance operation, which returns the canonical existing-or-new record. The returned record, not a discarded proposal, supplies later signed identity/time.
// ensureIssuance предлагает новые UUID и текущее время с точностью секунды устойчивой операции выдачи цели, возвращающей каноническую прежнюю либо новую запись. Последующим подписанным идентичности и времени служит возвращённая запись, а не отброшенное предложение.
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

// verifyRoleCredential verifies current/previous purpose-bound publisher trust, canonical signature/time and exact peer/restore/phase-generation bindings, then requires an expected target and persisted matching delivery digest. It returns claims and the compact digest without consuming ACK replay state.
// verifyRoleCredential проверяет текущее либо предыдущее доверие publisher с назначением, каноническую подпись и время и точные привязки peer, восстановления, фазы и поколений, затем требует ожидаемую цель и сохранённый дайджест доставки. Claims и дайджест compact возвращаются без потребления replay ACK.
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

// verifyDirective first requires the exact controller-owned persisted compact/digest, then verifies its JWS with the controller public key and checks role/restore/phase/credential and stop/drain bindings. This differs from the workload agent's mounted-certificate trust path.
// verifyDirective сначала требует точные сохранённые compact и дайджест, принадлежащие контроллеру, затем проверяет JWS публичным ключом контроллера и привязки роли, восстановления, фазы, credential и stop/drain. Это отличается от доверия смонтированному сертификату у агента workload.
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

// verifyACK derives and fingerprints the credential-pinned public ACK key, verifies canonical ACK signature/time and exact directive/role/generation/digest bindings, requiring stopped admission, drained work and zero in-flight count. Replay reservation is a later coordination operation.
// verifyACK получает и вычисляет отпечаток закреплённого credential публичного ACK-ключа, проверяет канонические подпись и время и точные привязки директивы, роли, поколений и дайджестов, требуя остановленный допуск, дренирование и нулевой счётчик запросов. Replay резервируется позднее в координации.
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

// restoreCredentialRejected returns the fixed typed unauthenticated error for rejected role credentials without exposing signer, peer or secret details.
// restoreCredentialRejected возвращает фиксированную типизированную unauthenticated для отклонённого role credential без раскрытия подписанта, peer и деталей секрета.
func restoreCredentialRejected() error {
	return failure.New(
		failure.Unauthenticated,
		"restore role credential rejected",
	)
}

// mapRestorePersistence maps replay, idempotency conflict and absence to their closed domain failure kinds and everything else to persistence-unavailable, preserving the cause under the supplied fixed operation message.
// mapRestorePersistence переводит replay, конфликт идемпотентности и отсутствие в закрытые доменные виды ошибок, остальное — в persistence-unavailable, сохраняя причину под переданным фиксированным сообщением операции.
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

// deliveryTargetID preserves target lookup spelling by replacing underscores in the role with hyphens and lowercasing ASCII before appending it to workload ID. It is formatting, not target registration or authorization.
// deliveryTargetID сохраняет форму поиска цели, заменяя подчёркивания роли дефисами и переводя ASCII в нижний регистр перед добавлением к ID workload. Это форматирование, а не регистрация цели и авторизация.
func deliveryTargetID(workloadID, role string) string {
	value := role
	for index := range value {
		if value[index] == '_' {
			value = value[:index] + "-" + value[index+1:]
		}
	}
	return workloadID + "." + lowerASCII(value)
}

// lowerASCII copies the input bytes and lowercases only ASCII A-Z, preserving all other bytes without Unicode normalization or locale-sensitive casing.
// lowerASCII копирует входные байты и переводит только ASCII A-Z в нижний регистр, сохраняя остальные байты без нормализации Unicode и локального преобразования регистра.
func lowerASCII(value string) string {
	buffer := []byte(value)
	for index := range buffer {
		if buffer[index] >= 'A' && buffer[index] <= 'Z' {
			buffer[index] += 'a' - 'A'
		}
	}
	return string(buffer)
}

// digestCompact returns lowercase SHA-256 hex over exact compact JWS bytes; digest equality does not replace canonical decoding, signer trust or signature verification.
// digestCompact возвращает строчный hex SHA-256 точных compact-байтов JWS; равенство дайджеста не заменяет канонический разбор, доверие подписанту и проверку подписи.
func digestCompact(compact string) string {
	digest := sha256.Sum256([]byte(compact))
	return hex.EncodeToString(digest[:])
}
