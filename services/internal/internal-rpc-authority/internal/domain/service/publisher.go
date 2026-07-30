package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	restoreIssuanceType        = "mattercodex-internal-rpc-restore-role-issuance+jws"
	restoreRoleCredentialType  = "mattercodex-internal-rpc-restore-role-credential+jws"
	restoreDeliveryReceiptType = "mattercodex-internal-rpc-restore-role-delivery-receipt+jws"
	restoreControllerSPIFFE    = "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-restore-controller"
	restorePublisherSPIFFE     = "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-publisher"
	restorePublisherAudience   = "urn:mattercodex:internal-rpc-authority-restore-role-credential-publisher"
	restoreControllerAudience  = "urn:mattercodex:internal-rpc-authority-restore-controller"
	restoreIssuanceTTL         = 30 * time.Second
	restoreRoleCredentialTTL   = 5 * time.Minute
	publishedReadbackIntentTTL = 2 * time.Minute
)

// RestoreCredentialSigner описывает ключ удостоверений восстановления.
type RestoreCredentialSigner struct {
	Key            internalrpcauth.ES256Key
	SourceRevision uint64
	SourceDigest   string
	KeySetRevision uint64
	Generation     uint64
}

// ReadbackCredentialSigner описывает независимый ключ проверки выдачи.
type ReadbackCredentialSigner struct {
	Key            internalrpcauth.ES256Key
	SourceRevision uint64
	SourceDigest   string
	KeySetRevision uint64
	Generation     uint64
}

// ControllerIdentity связывает workload контроллера с его поколением и ключом.
type ControllerIdentity struct {
	SPIFFEID   string
	Key        internalrpcauth.ES256Key
	Generation uint64
}

// Publisher доставляет назначенные ключи и удостоверения через Vault.
type Publisher struct {
	registry       model.DeliveryTargetRegistry
	signer         RestoreCredentialSigner
	readbackSigner ReadbackCredentialSigner
	store          repository.PublisherStore
	vault          repository.SecretDelivery
	graph          repository.AuthorityGraphLifecycle
	graphMu        sync.RWMutex
	graphState     model.AuthoritySnapshotPublication
	now            func() time.Time
}

// AttachAuthorityGraph присоединяет обязательный полный publish/readback path.
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

// PublishAuthorityGraph публикует snapshot, auth/proof keys и trust bundles.
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

// NewPublisher создаёт publisher из реестра, независимых ключей и хранилищ.
func NewPublisher(
	registry model.DeliveryTargetRegistry,
	signer RestoreCredentialSigner,
	readbackSigner ReadbackCredentialSigner,
	store repository.PublisherStore,
	vault repository.SecretDelivery,
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
		vault == nil {
		return nil, errors.New("invalid publisher configuration")
	}
	return &Publisher{
		registry:       registry,
		signer:         signer,
		readbackSigner: readbackSigner,
		store:          store,
		vault:          vault,
		now:            time.Now,
	}, nil
}

// PublishReadbackMaterials доставляет отдельные материалы проверки выдачи.
func (publisher *Publisher) PublishReadbackMaterials(
	ctx context.Context,
) ([]model.PublishedReadbackMaterial, error) {
	if err := publisher.ensureWritable(ctx); err != nil {
		return nil, err
	}
	observedNow := publisher.now().UTC()
	bucket := uint64(observedNow.Unix() / 10)
	now := time.Unix(int64(bucket*10), 0).UTC()
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
	intentID := deterministicUUID(
		"readback-intent",
		target.TargetID,
		strconv.FormatUint(effectiveRevision, 10),
	)
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
	credentialJTI := deterministicUUID(
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
	existing, found, err := publisher.vault.ReadKV2(
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
		credentialMaterial, err = publisher.vault.CreateKV2(
			ctx,
			target.ReadbackCredentialPath,
			credentialData,
		)
	} else if existing.Data["intent_digest_sha256"] == intentDigest {
		credentialMaterial = existing
	} else {
		credentialMaterial, err = publisher.vault.WriteKV2CAS(
			ctx,
			target.ReadbackCredentialPath,
			existing.Version,
			credentialData,
		)
	}
	if err != nil {
		credentialMaterial, found, err = publisher.vault.ReadKV2(
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
		PossessionPrivateJWK:   possession.Data["possession_private_jwk"],
		CredentialVaultVersion: credentialMaterial.Version,
		PossessionVaultVersion: possession.Version,
	}, nil
}

func (publisher *Publisher) ensureReadbackPossession(
	ctx context.Context,
	target model.DeliveryTarget,
) (repository.SecretMaterial, error) {
	existing, found, err := publisher.vault.ReadKV2(
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
		delivered, err = publisher.vault.WriteKV2CAS(
			ctx,
			target.ReadbackPossessionKeyPath,
			existing.Version,
			data,
		)
	} else {
		delivered, err = publisher.vault.CreateKV2(
			ctx,
			target.ReadbackPossessionKeyPath,
			data,
		)
	}
	if err != nil {
		delivered, found, err = publisher.vault.ReadKV2(
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

func deterministicUUID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	value := digest[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}

// Publish выпускает и доставляет роль восстановления для целевого workload.
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
	roleMaterial, err := publisher.vault.CreateKV2(
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
		ACKVersion  uint64 `json:"ack_vault_metadata_version"`
		ACKDigest   string `json:"ack_material_digest_sha256"`
		RoleVersion uint64 `json:"role_vault_metadata_version"`
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
		VaultMetadataVersion:   roleMaterial.Version,
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

// Ready сверяет хранилище, Vault и фактически опубликованные материалы.
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

// Registry возвращает проверенный реестр целей доставки.
func (publisher *Publisher) Registry() model.DeliveryTargetRegistry {
	return publisher.registry
}

// SignerGeneration возвращает обслуживаемое поколение ключа восстановления.
func (publisher *Publisher) SignerGeneration() uint64 {
	return publisher.signer.Generation
}

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

func (publisher *Publisher) ensureACKMaterial(
	ctx context.Context,
	target model.DeliveryTarget,
	directive model.CredentialIssuanceDirective,
	directiveDigest string,
) (repository.SecretMaterial, error) {
	if existing, found, err := publisher.vault.ReadKV2(
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
	material, err := publisher.vault.CreateKV2(
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
