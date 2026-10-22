package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
)

const (
	// restoreIssuanceType pins the controller's issuance-directive JWS type, separate from the resulting role credential and delivery receipt.
	// restoreIssuanceType закрепляет тип JWS директивы выдачи контроллера отдельно от полученных role credential и квитанции доставки.
	restoreIssuanceType = "kodex-internal-rpc-restore-role-issuance+jws"
	// restoreRoleCredentialType identifies the publisher-signed restore-role credential protocol; it is not the workload ACK or controller directive type.
	// restoreRoleCredentialType обозначает протокол подписанного publisher restore-role credential, а не тип ACK workload или директивы контроллера.
	restoreRoleCredentialType = "kodex-internal-rpc-restore-role-credential+jws"
	// restoreDeliveryReceiptType identifies the signed receipt binding actual role/ACK material delivery to one issuance and readback digest.
	// restoreDeliveryReceiptType обозначает подписанную квитанцию, связывающую фактическую доставку материала role и ACK с одной выдачей и дайджестом readback.
	restoreDeliveryReceiptType = "kodex-internal-rpc-restore-role-delivery-receipt+jws"
	// restoreControllerSPIFFE is the fixed trusted controller identity required for publication requests and issuance claims, not a value chosen from the target registry.
	// restoreControllerSPIFFE — фиксированная доверенная идентичность контроллера для запросов публикации и issuance claims, а не значение из реестра целей.
	restoreControllerSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-controller"
	// restorePublisherSPIFFE is the fixed issuer of restore-role credentials and delivery receipts; transport authentication and signing-key trust are separate requirements.
	// restorePublisherSPIFFE — фиксированный издатель restore-role credentials и квитанций доставки; аутентификация транспорта и доверие ключу подписи требуются отдельно.
	restorePublisherSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher"
	// restorePublisherAudience is the exact audience of controller issuance directives accepted by this publisher, distinct from the controller audience of generated credentials.
	// restorePublisherAudience — точная аудитория принимаемых publisher директив выдачи контроллера отдельно от аудитории контроллера у созданных credentials.
	restorePublisherAudience = "urn:kodex:internal-rpc-authority-restore-role-credential-publisher"
	// restoreControllerAudience binds generated role credentials and delivery receipts to the restore controller rather than an arbitrary workload endpoint.
	// restoreControllerAudience связывает созданные role credentials и квитанции доставки с контроллером восстановления, а не произвольным endpoint workload.
	restoreControllerAudience = "urn:kodex:internal-rpc-authority-restore-controller"
	// restoreIssuanceTTL limits accepted issuance directives to thirty seconds using the common clock-skew rule; delivered role material has a separate lifetime.
	// restoreIssuanceTTL ограничивает принимаемые директивы выдачи тридцатью секундами по общему правилу расхождения часов; доставленный role material имеет отдельный срок.
	restoreIssuanceTTL = 30 * time.Second
	// restoreRoleCredentialTTL sets the five-minute lifetime of role credentials and delivery receipts from the persisted ACK-material issue time, keeping retries byte-stable.
	// restoreRoleCredentialTTL задаёт пятиминутный срок role credentials и квитанций доставки от сохранённого времени выдачи ACK-material, сохраняя стабильные байты повторов.
	restoreRoleCredentialTTL = 5 * time.Minute
	// publishedReadbackIntentTTL keeps pinned intents valid for ten minutes from the bucket time to tolerate projected delivery lag and startup retries; credential lifetime remains separately bounded.
	// publishedReadbackIntentTTL сохраняет PINNED intents на десять минут от времени bucket для задержки projected delivery и повторов запуска; срок credential ограничивается отдельно.
	publishedReadbackIntentTTL = 10 * time.Minute
	// readbackMaterialRotationInterval rounds publication time into two-minute buckets, rotating readback credential intent identities without changing them on every refresh.
	// readbackMaterialRotationInterval округляет время публикации в двухминутные bucket, меняя идентичности намерений readback credential не на каждый refresh.
	readbackMaterialRotationInterval = 2 * time.Minute
)

// RestoreCredentialSigner supplies a private restore-role signer with exact source/digest/key-set/generation provenance; the caller's trust loader authenticates those pins.
// RestoreCredentialSigner предоставляет закрытый подписант restore-role с точным происхождением source, digest, key-set и generation; подлинность привязок устанавливает загрузчик доверия вызывающего.
type RestoreCredentialSigner struct {
	Key            internalrpcauth.ES256Key
	SourceRevision uint64
	SourceDigest   string
	KeySetRevision uint64
	Generation     uint64
}

// ReadbackCredentialSigner carries independent readback-credential signing material and provenance; NewPublisher requires its key ID to differ from the restore-role signer.
// ReadbackCredentialSigner содержит независимый материал подписи readback credential и его происхождение; NewPublisher требует ID ключа, отличающийся от подписанта restore-role.
type ReadbackCredentialSigner struct {
	Key            internalrpcauth.ES256Key
	SourceRevision uint64
	SourceDigest   string
	KeySetRevision uint64
	Generation     uint64
}

// ControllerIdentity supplies transport-established controller SPIFFE identity, public key and generation to publication; carrying this struct alone does not prove authentication.
// ControllerIdentity передаёт установленную транспортом SPIFFE-идентичность контроллера, публичный ключ и поколение для публикации; наличие структуры само не доказывает аутентификацию.
type ControllerIdentity struct {
	SPIFFEID   string
	Key        internalrpcauth.ES256Key
	Generation uint64
}

// Publisher coordinates trusted registry targets, separate signing purposes, durable receipts and versioned Secret delivery. graphMu protects graph/publication bookkeeping; completed external writes are not rolled back by a later method error.
// Publisher согласует доверенные цели реестра, отдельные назначения подписи, устойчивые квитанции и версионированную доставку Secret. graphMu защищает учёт графа и публикации; поздняя ошибка не откатывает завершённые внешние записи.
type Publisher struct {
	registry       model.DeliveryTargetRegistry
	signer         RestoreCredentialSigner
	readbackSigner ReadbackCredentialSigner
	store          repository.PublisherStore
	delivery       repository.SecretDelivery
	graph          repository.AuthorityGraphLifecycle
	graphMu        sync.RWMutex
	graphState     model.AuthoritySnapshotPublication
	now            func() time.Time
}

// AttachAuthorityGraph installs one nonnil graph under the mutex and rejects replacement. It does not publish state, activate a snapshot or make readiness pass by attachment alone.
// AttachAuthorityGraph устанавливает один ненулевой граф под mutex и отклоняет замену. Состояние не публикуется, снимок не активируется и одного присоединения недостаточно для readiness.
func (publisher *Publisher) AttachAuthorityGraph(
	graph repository.AuthorityGraphLifecycle,
) error {
	if graph == nil {
		return errors.New("authority publisher graph is nil")
	}
	publisher.graphMu.Lock()
	defer publisher.graphMu.Unlock()
	if publisher.graph != nil {
		return errors.New("authority publisher graph is already attached")
	}
	publisher.graph = graph
	return nil
}

// PublishAuthorityGraph requires writable persistence/fence state, publishes through the attached graph and then updates local graph state and target readback source pins under lock. Graph effects can precede a returned error; a nil graph fails closed.
// PublishAuthorityGraph требует доступную запись хранения и ограждения, публикует через присоединённый граф, затем обновляет локальное состояние и привязки readback source целей под блокировкой. Эффекты графа могут предшествовать ошибке; nil-граф закрыто отклоняется.
func (publisher *Publisher) PublishAuthorityGraph(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	if err := publisher.ensureWritable(ctx); err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	publisher.graphMu.RLock()
	graph := publisher.graph
	publisher.graphMu.RUnlock()
	if graph == nil {
		return model.AuthoritySnapshotPublication{}, failure.New(
			failure.PersistenceUnavailable,
			"authority publisher graph is unavailable",
		)
	}
	publication, err := graph.Publish(ctx)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	publisher.graphMu.Lock()
	publisher.graphState = publication
	for targetID, target := range publisher.registry.Targets {
		target.ReadbackSourceRevision = publication.SourceRevision
		target.ReadbackServedStateDigest = publication.SourceDigestSHA256
		publisher.registry.Targets[targetID] = target
	}
	publisher.graphMu.Unlock()
	return publication, nil
}

// NewPublisher checks registry header presence/provenance shape, two private signers with different IDs and nonnil store/delivery ports, then installs the clock. It does not verify the registry signature, deep-copy targets, attach a graph or contact a Secret backend.
// NewPublisher проверяет заголовок реестра и форму происхождения, двух закрытых подписантов с разными ID и ненулевые порты store и delivery, затем задаёт часы. Подпись реестра не проверяется, цели глубоко не копируются, граф не присоединяется и backend Secret не вызывается.
func NewPublisher(
	registry model.DeliveryTargetRegistry,
	signer RestoreCredentialSigner,
	readbackSigner ReadbackCredentialSigner,
	store repository.PublisherStore,
	delivery repository.SecretDelivery,
) (*Publisher, error) {
	if registry.Version != model.ContractVersion ||
		registry.SourceRevision == 0 ||
		!digestPattern.MatchString(registry.SourceDigest) ||
		len(registry.Targets) == 0 ||
		signer.Key.Private == nil ||
		signer.SourceRevision == 0 ||
		!digestPattern.MatchString(signer.SourceDigest) ||
		signer.KeySetRevision == 0 ||
		signer.Generation == 0 ||
		readbackSigner.Key.Private == nil ||
		readbackSigner.SourceRevision == 0 ||
		!digestPattern.MatchString(readbackSigner.SourceDigest) ||
		readbackSigner.KeySetRevision == 0 ||
		readbackSigner.Generation == 0 ||
		readbackSigner.Key.KeyID == signer.Key.KeyID ||
		store == nil ||
		delivery == nil {
		return nil, errors.New("invalid publisher configuration")
	}
	return &Publisher{
		registry:       registry,
		signer:         signer,
		readbackSigner: readbackSigner,
		store:          store,
		delivery:       delivery,
		now:            time.Now,
	}, nil
}

// PublishReadbackMaterials gates writes, snapshots target values under graphMu and publishes sequentially using one two-minute bucket. Target order follows map iteration; failure returns no result slice even though earlier target deliveries may already have persisted.
// PublishReadbackMaterials проверяет разрешение записи, копирует значения целей под graphMu и последовательно публикует с одним двухминутным bucket. Порядок целей следует map; при ошибке срез результатов не возвращается, хотя доставки ранних целей могли сохраниться.
func (publisher *Publisher) PublishReadbackMaterials(
	ctx context.Context,
) ([]model.PublishedReadbackMaterial, error) {
	if err := publisher.ensureWritable(ctx); err != nil {
		return nil, err
	}
	observedNow := publisher.now().UTC()
	rotationSeconds := int64(readbackMaterialRotationInterval / time.Second)
	bucket := uint64(observedNow.Unix() / rotationSeconds)
	now := time.Unix(int64(bucket)*rotationSeconds, 0).UTC()
	publisher.graphMu.RLock()
	targets := make([]model.DeliveryTarget, 0, len(publisher.registry.Targets))
	for _, target := range publisher.registry.Targets {
		targets = append(targets, target)
	}
	publisher.graphMu.RUnlock()
	results := make([]model.PublishedReadbackMaterial, 0, len(targets))
	for _, target := range targets {
		result, err := publisher.publishReadbackMaterial(ctx, target, now, bucket)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// publishReadbackMaterial validates graph pins, obtains possession material, durably pins the exact intent and signs/delivers its credential. It recovers an ambiguous delivery only by same-intent readback and verifies returned compact signature, exact payload and named bindings before returning private material to the trusted caller.
// publishReadbackMaterial проверяет привязки графа, получает материал владения, устойчиво закрепляет точное намерение и подписывает и доставляет credential. Неопределённая доставка разрешается только readback того же intent; до возврата закрытого материала доверенному caller проверяются подпись compact, точный payload и именованные привязки.
func (publisher *Publisher) publishReadbackMaterial(
	ctx context.Context,
	target model.DeliveryTarget,
	now time.Time,
	bucket uint64,
) (model.PublishedReadbackMaterial, error) {
	if target.ReadbackSourceRevision == 0 ||
		!digestPattern.MatchString(target.ReadbackServedStateDigest) {
		return model.PublishedReadbackMaterial{}, failure.New(
			failure.PersistenceUnavailable,
			"authority graph readback source is unavailable",
		)
	}
	possession, err := publisher.ensureReadbackPossession(ctx, target)
	if err != nil {
		return model.PublishedReadbackMaterial{}, err
	}
	possessionKey, err := internalrpcauth.ParsePrivateJWK(
		[]byte(possession.Data["possession_private_jwk"]),
	)
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.New(
			failure.PersistenceUnavailable,
			"stored readback possession key rejected",
		)
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(possessionKey.PublicOnly())
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.Wrap(
			failure.Internal,
			"encode readback possession public key",
			err,
		)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		possessionKey.PublicOnly(),
	)
	if err != nil || thumbprint != possession.Data["possession_key_thumbprint_sha256"] {
		return model.PublishedReadbackMaterial{}, failure.New(
			failure.PersistenceUnavailable,
			"stored readback possession key thumbprint rejected",
		)
	}
	effectiveRevision := target.ReadbackIntentRevision*1_000_000_000 + bucket
	intentID := readbackIntentID(target, effectiveRevision)
	expiresAt := now.Add(publishedReadbackIntentTTL)
	intentDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Version                 int    `json:"v"`
		IntentID                string `json:"intent_id"`
		Kind                    string `json:"kind"`
		IntentRevision          uint64 `json:"intent_revision"`
		WorkloadID              string `json:"workload_id"`
		WorkloadSPIFFEID        string `json:"workload_spiffe_id"`
		Role                    string `json:"role"`
		WorkloadGeneration      uint64 `json:"workload_generation"`
		CredentialGeneration    uint64 `json:"credential_generation"`
		MaterialGeneration      uint64 `json:"material_generation"`
		PossessionKeyID         string `json:"possession_key_kid"`
		PossessionKeyGeneration uint64 `json:"possession_key_generation"`
		PossessionPublicJWK     []byte `json:"possession_public_jwk"`
		PossessionThumbprint    string `json:"possession_key_thumbprint_sha256"`
		SourceRevision          uint64 `json:"source_revision"`
		ServedStateDigest       string `json:"served_state_digest_sha256"`
		ExpiresAt               int64  `json:"expires_at"`
	}{
		Version: model.ContractVersion, IntentID: intentID, Kind: "SNAPSHOT",
		IntentRevision: effectiveRevision, WorkloadID: target.WorkloadID,
		WorkloadSPIFFEID: target.WorkloadSPIFFEID, Role: target.Role,
		WorkloadGeneration:      target.WorkloadGeneration,
		CredentialGeneration:    target.CredentialGeneration,
		MaterialGeneration:      target.ReadbackMaterialGeneration,
		PossessionKeyID:         possessionKey.KeyID,
		PossessionKeyGeneration: target.ReadbackMaterialGeneration,
		PossessionPublicJWK:     publicJWK, PossessionThumbprint: thumbprint,
		SourceRevision:    target.ReadbackSourceRevision,
		ServedStateDigest: target.ReadbackServedStateDigest,
		ExpiresAt:         expiresAt.Unix(),
	})
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.Wrap(
			failure.Internal,
			"digest pinned readback intent",
			err,
		)
	}
	intent := model.ReadbackIntent{
		IntentID: intentID, Kind: "SNAPSHOT", IntentRevision: effectiveRevision,
		IntentDigestSHA256: intentDigest, WorkloadID: target.WorkloadID,
		WorkloadSPIFFEID: target.WorkloadSPIFFEID, Role: target.Role,
		WorkloadGeneration:      target.WorkloadGeneration,
		CredentialGeneration:    target.CredentialGeneration,
		MaterialGeneration:      target.ReadbackMaterialGeneration,
		PossessionKeyID:         possessionKey.KeyID,
		PossessionKeyGeneration: target.ReadbackMaterialGeneration,
		PossessionPublicJWK:     publicJWK, PossessionKeyThumbprint: thumbprint,
		SourceRevision:          target.ReadbackSourceRevision,
		ServedStateDigestSHA256: target.ReadbackServedStateDigest,
		Status:                  "PINNED", ExpiresAt: expiresAt,
	}
	pinned, err := publisher.store.PinReadbackIntent(ctx, intent)
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"pin readback intent",
			err,
		)
	}
	credentialJTI := value.JoinedAuthorityUUID(
		"readback-credential",
		intentID,
		publisher.readbackSigner.Key.KeyID,
	)
	claims := model.ReadbackCredentialClaims{
		Version: model.ContractVersion, Issuer: readbackPublisherIssuer,
		Audience: readbackAudience, Subject: target.WorkloadID,
		JTI: credentialJTI, Purpose: "SNAPSHOT_READBACK",
		IntentID: intentID, IntentKind: "SNAPSHOT",
		IntentRevision: effectiveRevision, IntentDigestSHA256: intentDigest,
		WorkloadID: target.WorkloadID, WorkloadSPIFFEID: target.WorkloadSPIFFEID,
		Role: target.Role, WorkloadGeneration: target.WorkloadGeneration,
		CredentialGeneration:    target.CredentialGeneration,
		MaterialGeneration:      target.ReadbackMaterialGeneration,
		PossessionKeyID:         possessionKey.KeyID,
		PossessionKeyGeneration: target.ReadbackMaterialGeneration,
		PossessionPublicJWK:     publicJWK, PossessionKeyThumbprint: thumbprint,
		SignerSourceRevision:     publisher.readbackSigner.SourceRevision,
		SignerSourceDigestSHA256: publisher.readbackSigner.SourceDigest,
		SignerKeySetRevision:     publisher.readbackSigner.KeySetRevision,
		SignerGeneration:         publisher.readbackSigner.Generation,
		IssuedAt:                 now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(readbackCredentialTTL).Unix(),
	}
	credentialCompact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		publisher.readbackSigner.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackCredentialType,
			KeyID: publisher.readbackSigner.Key.KeyID,
		},
	)
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.Wrap(
			failure.Internal,
			"sign readback credential",
			err,
		)
	}
	credentialDigestRaw := sha256.Sum256([]byte(credentialCompact))
	credentialData := map[string]string{
		"pinned_intent_id":                  intentID,
		"readback_credential_compact_jws":   credentialCompact,
		"readback_credential_jti":           credentialJTI,
		"readback_credential_digest_sha256": hex.EncodeToString(credentialDigestRaw[:]),
		"intent_digest_sha256":              intentDigest,
		"expires_at":                        strconv.FormatInt(claims.ExpiresAt, 10),
	}
	existing, found, err := publisher.delivery.ReadVersioned(
		ctx,
		target.ReadbackCredentialPath,
	)
	if err != nil {
		return model.PublishedReadbackMaterial{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"read readback credential delivery",
			err,
		)
	}
	var credentialMaterial repository.SecretMaterial
	if !found {
		credentialMaterial, err = publisher.delivery.CreateVersioned(
			ctx,
			target.ReadbackCredentialPath,
			credentialData,
		)
	} else if existing.Data["intent_digest_sha256"] == intentDigest {
		credentialMaterial = existing
	} else {
		credentialMaterial, err = publisher.delivery.WriteVersionedCAS(
			ctx,
			target.ReadbackCredentialPath,
			existing.Version,
			credentialData,
		)
	}
	if err != nil {
		credentialMaterial, found, err = publisher.delivery.ReadVersioned(
			ctx,
			target.ReadbackCredentialPath,
		)
		if err != nil ||
			!found ||
			credentialMaterial.Data["intent_digest_sha256"] != intentDigest {
			return model.PublishedReadbackMaterial{}, failure.New(
				failure.PersistenceUnavailable,
				"recover concurrent readback credential delivery",
			)
		}
	}
	servedCompact := credentialMaterial.Data["readback_credential_compact_jws"]
	verified, verifyErr := internalrpcauth.VerifyCanonicalJSON(
		servedCompact,
		publisher.readbackSigner.Key.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackCredentialType,
			KeyID: publisher.readbackSigner.Key.KeyID,
		},
	)
	expectedPayload, payloadErr := internalrpcauth.CanonicalJSON(claims)
	servedDigest := sha256.Sum256([]byte(servedCompact))
	if credentialMaterial.Data["pinned_intent_id"] != intentID ||
		credentialMaterial.Data["intent_digest_sha256"] != intentDigest ||
		credentialMaterial.Data["readback_credential_jti"] != credentialJTI ||
		credentialMaterial.Data["expires_at"] !=
			strconv.FormatInt(claims.ExpiresAt, 10) ||
		credentialMaterial.Data["readback_credential_digest_sha256"] !=
			hex.EncodeToString(servedDigest[:]) ||
		verifyErr != nil ||
		payloadErr != nil ||
		!bytes.Equal(verified.CanonicalPayload, expectedPayload) {
		return model.PublishedReadbackMaterial{}, failure.New(
			failure.PersistenceUnavailable,
			"readback credential delivery readback rejected",
		)
	}
	return model.PublishedReadbackMaterial{
		Intent: pinned, ReadbackCredentialJWS: servedCompact,
		PossessionPrivateJWK:       possession.Data["possession_private_jwk"],
		CredentialSecretGeneration: credentialMaterial.Version,
		PossessionSecretGeneration: possession.Version,
	}, nil
}

// readbackIntentID derives a stable NUL-joined identifier from the fixed namespace, target ID, graph source revision and effective material revision. It does not allocate a random identity or bind secret values.
// readbackIntentID выводит устойчивый NUL-joined идентификатор из фиксированного пространства, ID цели, ревизии источника графа и эффективной ревизии материала. Случайная идентичность не создаётся и значения секретов не привязываются.
func readbackIntentID(target model.DeliveryTarget, effectiveRevision uint64) string {
	return value.JoinedAuthorityUUID(
		"readback-intent",
		target.TargetID,
		strconv.FormatUint(target.ReadbackSourceRevision, 10),
		strconv.FormatUint(effectiveRevision, 10),
	)
}

// ensureReadbackPossession reuses a cryptographically checked current generation, rejects rollback/gaps and otherwise generates the next target key for create/CAS. A failed write is resolved by reading and validating served material, not by blindly writing another key.
// ensureReadbackPossession переиспользует криптографически проверенное текущее поколение, отклоняет rollback и пропуски, иначе генерирует следующий ключ цели для create или CAS. Отказ записи разрешается чтением и проверкой обслуженного материала, а не слепой записью нового ключа.
func (publisher *Publisher) ensureReadbackPossession(
	ctx context.Context,
	target model.DeliveryTarget,
) (repository.SecretMaterial, error) {
	existing, found, err := publisher.delivery.ReadVersioned(
		ctx,
		target.ReadbackPossessionKeyPath,
	)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"read readback possession key delivery",
			err,
		)
	}
	if found {
		storedGeneration, parseErr := strconv.ParseUint(
			existing.Data["possession_key_generation"],
			10,
			64,
		)
		if parseErr != nil ||
			storedGeneration > target.ReadbackMaterialGeneration ||
			storedGeneration+1 < target.ReadbackMaterialGeneration {
			return repository.SecretMaterial{}, failure.New(
				failure.PersistenceUnavailable,
				"readback possession key generation rollback or gap rejected",
			)
		}
		if storedGeneration == target.ReadbackMaterialGeneration {
			if err := validateReadbackPossession(existing, target); err != nil {
				return repository.SecretMaterial{}, err
			}
			return existing, nil
		}
	}
	keyID := target.TargetID + "-readback-g" + strconv.FormatUint(
		target.ReadbackMaterialGeneration,
		10,
	)
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"generate readback possession key",
			err,
		)
	}
	privateJWK, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"encode readback possession private key",
			err,
		)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(key.PublicOnly())
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"fingerprint readback possession key",
			err,
		)
	}
	data := map[string]string{
		"possession_private_jwk": string(privateJWK),
		"possession_key_kid":     key.KeyID,
		"possession_key_generation": strconv.FormatUint(
			target.ReadbackMaterialGeneration,
			10,
		),
		"possession_key_thumbprint_sha256": thumbprint,
	}
	var delivered repository.SecretMaterial
	if found {
		delivered, err = publisher.delivery.WriteVersionedCAS(
			ctx,
			target.ReadbackPossessionKeyPath,
			existing.Version,
			data,
		)
	} else {
		delivered, err = publisher.delivery.CreateVersioned(
			ctx,
			target.ReadbackPossessionKeyPath,
			data,
		)
	}
	if err != nil {
		delivered, found, err = publisher.delivery.ReadVersioned(
			ctx,
			target.ReadbackPossessionKeyPath,
		)
		if err != nil || !found {
			return repository.SecretMaterial{}, failure.New(
				failure.PersistenceUnavailable,
				"recover concurrent readback possession key delivery",
			)
		}
	}
	if err := validateReadbackPossession(delivered, target); err != nil {
		return repository.SecretMaterial{}, err
	}
	return delivered, nil
}

// validateReadbackPossession parses the private JWK and decimal generation, recomputes its public thumbprint and requires the target's exact generation/key ID plus matching stored fields. It neither writes material nor authenticates a different Secret resource.
// validateReadbackPossession разбирает закрытый JWK и десятичное поколение, пересчитывает публичный отпечаток и требует точные поколение и ID ключа цели и совпавшие сохранённые поля. Материал не записывается и другой ресурс Secret не аутентифицируется.
func validateReadbackPossession(
	material repository.SecretMaterial,
	target model.DeliveryTarget,
) error {
	generation, generationErr := strconv.ParseUint(
		material.Data["possession_key_generation"],
		10,
		64,
	)
	key, keyErr := internalrpcauth.ParsePrivateJWK(
		[]byte(material.Data["possession_private_jwk"]),
	)
	thumbprint := ""
	var thumbprintErr error
	if keyErr == nil {
		thumbprint, thumbprintErr = internalrpcauth.PublicJWKThumbprintSHA256(
			key.PublicOnly(),
		)
	}
	expectedKeyID := target.TargetID + "-readback-g" + strconv.FormatUint(
		target.ReadbackMaterialGeneration,
		10,
	)
	if generationErr != nil ||
		keyErr != nil ||
		thumbprintErr != nil ||
		generation != target.ReadbackMaterialGeneration ||
		key.KeyID != expectedKeyID ||
		material.Data["possession_key_kid"] != expectedKeyID ||
		material.Data["possession_key_thumbprint_sha256"] != thumbprint {
		return failure.New(
			failure.PersistenceUnavailable,
			"readback possession key cryptographic readback rejected",
		)
	}
	return nil
}

// Publish gates persistence and controller identity, resolves a same-directive persisted idempotent receipt before revalidating new issuance, then validates target, delivers stable ACK/role material and signs/persists a delivery receipt. Failures after external delivery do not undo Secret writes; conflicting inputs cannot reuse the receipt.
// Publish проверяет хранение и идентичность контроллера, разрешает сохранённую квитанцию идемпотентности той же директивы до проверки новой выдачи, затем проверяет цель, доставляет стабильный материал ACK и role и подписывает и сохраняет квитанцию. Отказ после внешней доставки не откатывает Secret; конфликтующий вход не переиспользует квитанцию.
func (publisher *Publisher) Publish(
	ctx context.Context,
	controller ControllerIdentity,
	directiveCompact string,
	idempotencyKey string,
) (model.PublishedCredential, error) {
	if err := publisher.ensureWritable(ctx); err != nil {
		return model.PublishedCredential{}, err
	}
	if !uuidPattern.MatchString(idempotencyKey) ||
		controller.SPIFFEID != restoreControllerSPIFFE ||
		controller.Key.Public == nil ||
		controller.Generation == 0 {
		return model.PublishedCredential{}, failure.New(
			failure.InvalidRequest,
			"restore credential publication request is invalid",
		)
	}
	directiveDigestRaw := sha256.Sum256([]byte(directiveCompact))
	directiveDigest := hex.EncodeToString(directiveDigestRaw[:])
	if saved, found, err := publisher.store.LoadPublishedCredential(
		ctx,
		idempotencyKey,
	); err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"load persisted restore credential delivery",
			err,
		)
	} else if found {
		if saved.DirectiveDigest != directiveDigest {
			return model.PublishedCredential{}, failure.New(
				failure.ReplayDetected,
				"restore credential publication idempotency conflict",
			)
		}
		return saved, nil
	}
	directive, err := publisher.verifyDirective(
		controller,
		directiveCompact,
	)
	if err != nil {
		return model.PublishedCredential{}, err
	}
	target, ok := publisher.registry.Targets[directive.DeliveryTargetID]
	if !ok || !directiveMatchesTarget(directive, target, publisher.registry) {
		return model.PublishedCredential{}, failure.New(
			failure.BindingMismatch,
			"restore issuance target registry binding failed",
		)
	}
	ackMaterial, err := publisher.ensureACKMaterial(
		ctx,
		target,
		directive,
		directiveDigest,
	)
	if err != nil {
		return model.PublishedCredential{}, err
	}
	ackPrivate, err := internalrpcauth.ParsePrivateJWK(
		[]byte(ackMaterial.Data["ack_private_jwk"]),
	)
	if err != nil {
		return model.PublishedCredential{}, failure.New(
			failure.PersistenceUnavailable,
			"stored restore ACK key readback rejected",
		)
	}
	if ackPrivate.KeyID != ackMaterial.Data["ack_key_kid"] {
		return model.PublishedCredential{}, failure.New(
			failure.PersistenceUnavailable,
			"stored restore ACK key binding rejected",
		)
	}
	ackThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		ackPrivate.PublicOnly(),
	)
	if err != nil || ackThumbprint != ackMaterial.Data["ack_key_thumbprint_sha256"] {
		return model.PublishedCredential{}, failure.New(
			failure.PersistenceUnavailable,
			"stored restore ACK key thumbprint rejected",
		)
	}
	issuedAt, err := strconv.ParseInt(ackMaterial.Data["issued_at"], 10, 64)
	if err != nil {
		return model.PublishedCredential{}, failure.New(
			failure.PersistenceUnavailable,
			"stored restore delivery time rejected",
		)
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(ackPrivate.PublicOnly())
	if err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.Internal,
			"encode restore ACK public key",
			err,
		)
	}
	roleClaims := model.RestoreRoleCredentialClaims{
		Version: model.ContractVersion, Issuer: restorePublisherSPIFFE,
		Audience: restoreControllerAudience, Subject: target.WorkloadID,
		JTI:        ackMaterial.Data["role_credential_jti"],
		WorkloadID: target.WorkloadID, WorkloadSPIFFEID: target.WorkloadSPIFFEID,
		Role: target.Role, WorkloadGeneration: target.WorkloadGeneration,
		CredentialGeneration: target.CredentialGeneration,
		RestoreID:            directive.RestoreID, RestoreEpoch: directive.RestoreEpoch,
		CoordinationRevision:     directive.CoordinationRevision,
		SignerSourceRevision:     publisher.signer.SourceRevision,
		SignerSourceDigestSHA256: publisher.signer.SourceDigest,
		SignerKeySetRevision:     publisher.signer.KeySetRevision,
		SignerGeneration:         publisher.signer.Generation,
		SignerKeyID:              publisher.signer.Key.KeyID,
		ACKKeyID:                 ackPrivate.KeyID, ACKKeyGeneration: directive.ACKKeyGeneration,
		ACKPublicJWK: publicJWK, ACKKeyThumbprintSHA256: ackThumbprint,
		IssuedAt: issuedAt, NotBefore: issuedAt,
		ExpiresAt: time.Unix(issuedAt, 0).Add(restoreRoleCredentialTTL).Unix(),
	}
	roleCompact, err := internalrpcauth.SignCanonicalJSON(
		roleClaims,
		publisher.signer.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreRoleCredentialType,
			KeyID: publisher.signer.Key.KeyID,
		},
	)
	if err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.Internal,
			"sign restore role credential",
			err,
		)
	}
	roleDigestRaw := sha256.Sum256([]byte(roleCompact))
	roleDigest := hex.EncodeToString(roleDigestRaw[:])
	roleMaterial, err := publisher.delivery.CreateVersioned(
		ctx,
		target.RestoreCredentialPath,
		map[string]string{
			"semantic_digest_sha256":        directiveDigest,
			"issuance_directive_jti":        directive.JTI,
			"role_credential_compact_jws":   roleCompact,
			"role_credential_digest_sha256": roleDigest,
			"delivery_receipt_jti":          ackMaterial.Data["delivery_receipt_jti"],
			"issued_at":                     ackMaterial.Data["issued_at"],
		},
	)
	if err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"deliver restore role credential",
			err,
		)
	}
	if roleMaterial.Data["semantic_digest_sha256"] != directiveDigest ||
		roleMaterial.Data["issuance_directive_jti"] != directive.JTI ||
		roleMaterial.Data["role_credential_digest_sha256"] != roleDigest ||
		roleMaterial.Data["role_credential_compact_jws"] != roleCompact {
		return model.PublishedCredential{}, failure.New(
			failure.ReplayDetected,
			"restore role credential delivery conflict",
		)
	}
	readbackDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		ACKVersion  uint64 `json:"ack_secret_generation"`
		ACKDigest   string `json:"ack_material_digest_sha256"`
		RoleVersion uint64 `json:"role_secret_generation"`
		RoleDigest  string `json:"role_material_digest_sha256"`
	}{
		ACKVersion: ackMaterial.Version, ACKDigest: ackMaterial.Digest,
		RoleVersion: roleMaterial.Version, RoleDigest: roleMaterial.Digest,
	})
	if err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.Internal,
			"digest restore credential delivery readback",
			err,
		)
	}
	receiptClaims := model.CredentialDeliveryReceiptClaims{
		Version: model.ContractVersion, Issuer: restorePublisherSPIFFE,
		Audience: restoreControllerAudience, Subject: target.WorkloadID,
		JTI:                  ackMaterial.Data["delivery_receipt_jti"],
		IssuanceDirectiveJTI: directive.JTI,
		RestoreID:            directive.RestoreID, RestoreEpoch: directive.RestoreEpoch,
		CoordinationRevision: directive.CoordinationRevision,
		DeliveryTargetID:     target.TargetID, WorkloadID: target.WorkloadID,
		Role: target.Role, WorkloadGeneration: target.WorkloadGeneration,
		CredentialGeneration:       target.CredentialGeneration,
		RoleCredentialDigestSHA256: roleDigest,
		ACKKeyID:                   ackPrivate.KeyID, ACKKeyGeneration: directive.ACKKeyGeneration,
		ACKKeyThumbprintSHA256: ackThumbprint,
		SecretGeneration:       roleMaterial.Version,
		DeliveryReadbackDigest: readbackDigest,
		IssuedAt:               issuedAt, NotBefore: issuedAt,
		ExpiresAt: time.Unix(issuedAt, 0).Add(restoreRoleCredentialTTL).Unix(),
	}
	receiptCompact, err := internalrpcauth.SignCanonicalJSON(
		receiptClaims,
		publisher.signer.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreDeliveryReceiptType,
			KeyID: publisher.signer.Key.KeyID,
		},
	)
	if err != nil {
		return model.PublishedCredential{}, failure.Wrap(
			failure.Internal,
			"sign restore credential delivery receipt",
			err,
		)
	}
	result := model.PublishedCredential{
		IdempotencyKey:       idempotencyKey,
		DirectiveJTI:         directive.JTI,
		DirectiveDigest:      directiveDigest,
		DeliveryReceiptJWS:   receiptCompact,
		RoleCredentialDigest: roleDigest,
		CredentialGeneration: target.CredentialGeneration,
		ACKKeyGeneration:     directive.ACKKeyGeneration,
		AcceptedAt:           time.Unix(issuedAt, 0).UTC(),
	}
	saved, err := publisher.store.SavePublishedCredential(ctx, result)
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyConflict) {
			return model.PublishedCredential{}, failure.New(
				failure.ReplayDetected,
				"restore credential publication idempotency conflict",
			)
		}
		return model.PublishedCredential{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"persist restore credential delivery receipt",
			err,
		)
	}
	return saved, nil
}

// Ready checks writable persistence/fence state, available signer/registry/attached publication and graph readiness for that publication. It is not a pure check of local configuration or a newly created connection.
// Ready проверяет запись хранения и ограждения, наличие подписанта, реестра и присоединённой публикации и готовность графа этой публикации. Это не чистая проверка локальной конфигурации или созданного соединения.
func (publisher *Publisher) Ready(ctx context.Context) error {
	if err := publisher.ensureWritable(ctx); err != nil {
		return err
	}
	publisher.graphMu.RLock()
	graph := publisher.graph
	graphState := publisher.graphState
	publisher.graphMu.RUnlock()
	if publisher.signer.Key.Private == nil ||
		len(publisher.registry.Targets) == 0 ||
		graph == nil ||
		graphState.SourceRevision == 0 {
		return failure.New(failure.PersistenceUnavailable, "publisher signer or target registry is unavailable")
	}
	if err := graph.Ready(ctx, graphState); err != nil {
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"authority publisher graph is not ready",
			err,
		)
	}
	return nil
}

// ensureWritable delegates the persistence/restore-fence readiness gate to the store and returns a typed unavailable failure on refusal without fallback writes.
// ensureWritable передаёт проверку готовности хранения и ограждения восстановления store и при отказе возвращает типизированную unavailable без запасного пути записи.
func (publisher *Publisher) ensureWritable(ctx context.Context) error {
	if err := publisher.store.PublisherReady(ctx); err != nil {
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"publisher persistence or restore fence is unavailable",
			err,
		)
	}
	return nil
}

// Registry returns the configured registry struct by value, with its target map still shared rather than a defensive copy. Callers must not mutate it or infer a fresh durable readback from this getter.
// Registry возвращает настроенную структуру реестра по значению, сохраняя общую карту целей вместо защитной копии. Вызывающий не должен менять её или считать getter свежим устойчивым readback.
func (publisher *Publisher) Registry() model.DeliveryTargetRegistry {
	return publisher.registry
}

// SignerGeneration reports the configured restore-role signing generation, not the separate readback signer or latest registry/source revision.
// SignerGeneration сообщает настроенное поколение подписи restore-role, а не отдельного подписанта readback или последнюю ревизию реестра и источника.
func (publisher *Publisher) SignerGeneration() uint64 {
	return publisher.signer.Generation
}

// verifyDirective verifies the controller key ID/protected type and canonical issuance JWS, then checks thirty-second timing and exact issuer/audience/restore identity generations. Target-registry/workload binding is checked separately by directiveMatchesTarget.
// verifyDirective проверяет ID ключа контроллера, защищённый тип и канонический issuance JWS, затем тридцатисекундное время и точные issuer, audience, идентичность восстановления и поколения. Привязка реестра целей и workload отдельно проверяется directiveMatchesTarget.
func (publisher *Publisher) verifyDirective(
	controller ControllerIdentity,
	compact string,
) (model.CredentialIssuanceDirective, error) {
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil ||
		header.Type != restoreIssuanceType ||
		header.KeyID != controller.Key.KeyID {
		return model.CredentialIssuanceDirective{}, failure.New(
			failure.Unauthenticated,
			"restore issuance directive header rejected",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		controller.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: restoreIssuanceType, KeyID: controller.Key.KeyID,
		},
	)
	if err != nil {
		return model.CredentialIssuanceDirective{}, failure.Wrap(
			failure.Unauthenticated,
			"restore issuance directive signature rejected",
			err,
		)
	}
	var claims model.CredentialIssuanceDirective
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&claims,
	); err != nil {
		return model.CredentialIssuanceDirective{}, failure.Wrap(
			failure.Unauthenticated,
			"restore issuance directive claims rejected",
			err,
		)
	}
	now := publisher.now().UTC().Truncate(time.Second)
	if err := internalrpcauth.ValidateTimes(
		now,
		time.Unix(claims.IssuedAt, 0),
		time.Unix(claims.NotBefore, 0),
		time.Unix(claims.ExpiresAt, 0),
		restoreIssuanceTTL,
		readbackAllowedClockSkew,
	); err != nil ||
		claims.Version != model.ContractVersion ||
		claims.Issuer != restoreControllerSPIFFE ||
		claims.Audience != restorePublisherAudience ||
		!uuidPattern.MatchString(claims.JTI) ||
		!uuidPattern.MatchString(claims.RestoreID) ||
		claims.RestoreEpoch == 0 ||
		claims.CoordinationRevision == 0 ||
		claims.ACKKeyGeneration == 0 {
		return model.CredentialIssuanceDirective{}, failure.New(
			failure.Unauthenticated,
			"restore issuance directive binding rejected",
		)
	}
	return claims, nil
}

// ensureACKMaterial reuses only material matching directive digest/JTI or creates a new target ACK key, stable role/receipt JTIs and issue time once. It checks served directive binding after creation; Publish performs the later key/thumbprint and credential readback checks.
// ensureACKMaterial переиспользует лишь материал с совпавшими дайджестом директивы и JTI либо однократно создаёт новый ACK-ключ цели, устойчивые JTI роли и квитанции и время. После создания проверяется привязка обслуженной директивы; поздние проверки ключа, отпечатка и credential readback выполняет Publish.
func (publisher *Publisher) ensureACKMaterial(
	ctx context.Context,
	target model.DeliveryTarget,
	directive model.CredentialIssuanceDirective,
	directiveDigest string,
) (repository.SecretMaterial, error) {
	if existing, found, err := publisher.delivery.ReadVersioned(
		ctx,
		target.RestoreACKKeyPath,
	); err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"read restore ACK key delivery",
			err,
		)
	} else if found {
		if existing.Data["semantic_digest_sha256"] != directiveDigest ||
			existing.Data["issuance_directive_jti"] != directive.JTI {
			return repository.SecretMaterial{}, failure.New(
				failure.ReplayDetected,
				"restore ACK key delivery conflict",
			)
		}
		return existing, nil
	}
	ackKeyID := target.TargetID + "-ack-g" + strconv.FormatUint(
		directive.ACKKeyGeneration,
		10,
	)
	ackKey, err := internalrpcauth.GenerateES256Key(ackKeyID)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"generate restore ACK key",
			err,
		)
	}
	privateJWK, err := internalrpcauth.MarshalPrivateJWK(ackKey)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"encode restore ACK private key",
			err,
		)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(ackKey.PublicOnly())
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.Internal,
			"fingerprint restore ACK key",
			err,
		)
	}
	roleCredentialJTI, err := newUUID()
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(failure.Internal, "create role credential jti", err)
	}
	receiptJTI, err := newUUID()
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(failure.Internal, "create delivery receipt jti", err)
	}
	issuedAt := publisher.now().UTC().Truncate(time.Second)
	material, err := publisher.delivery.CreateVersioned(
		ctx,
		target.RestoreACKKeyPath,
		map[string]string{
			"semantic_digest_sha256":    directiveDigest,
			"issuance_directive_jti":    directive.JTI,
			"ack_private_jwk":           string(privateJWK),
			"ack_key_kid":               ackKey.KeyID,
			"ack_key_thumbprint_sha256": thumbprint,
			"role_credential_jti":       roleCredentialJTI,
			"delivery_receipt_jti":      receiptJTI,
			"issued_at":                 strconv.FormatInt(issuedAt.Unix(), 10),
		},
	)
	if err != nil {
		return repository.SecretMaterial{}, failure.Wrap(
			failure.PersistenceUnavailable,
			"deliver restore ACK key",
			err,
		)
	}
	if material.Data["semantic_digest_sha256"] != directiveDigest ||
		material.Data["issuance_directive_jti"] != directive.JTI {
		return repository.SecretMaterial{}, failure.New(
			failure.ReplayDetected,
			"restore ACK key delivery conflict",
		)
	}
	return material, nil
}

// directiveMatchesTarget compares the signed subject, registry revision/digest and workload/role/generation fields with the exact configured target. It does not verify the directive signature or derive a target from untrusted text.
// directiveMatchesTarget сравнивает подписанные subject, ревизию и дайджест реестра и поля workload, роли и поколений с точной настроенной целью. Подпись директивы не проверяется и цель из недоверенного текста не выводится.
func directiveMatchesTarget(
	directive model.CredentialIssuanceDirective,
	target model.DeliveryTarget,
	registry model.DeliveryTargetRegistry,
) bool {
	return directive.Subject == target.WorkloadID &&
		directive.TargetRegistryRevision == registry.SourceRevision &&
		directive.TargetRegistryDigest == registry.SourceDigest &&
		directive.WorkloadID == target.WorkloadID &&
		directive.WorkloadSPIFFEID == target.WorkloadSPIFFEID &&
		directive.Role == target.Role &&
		directive.WorkloadGeneration == target.WorkloadGeneration &&
		directive.CredentialGeneration == target.CredentialGeneration
}
