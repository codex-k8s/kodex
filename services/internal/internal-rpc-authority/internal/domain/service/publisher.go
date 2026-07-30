package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
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
)

type RestoreCredentialSigner struct {
	Key            internalrpcauth.ES256Key
	SourceRevision uint64
	SourceDigest   string
	KeySetRevision uint64
	Generation     uint64
}

type ControllerIdentity struct {
	SPIFFEID   string
	Key        internalrpcauth.ES256Key
	Generation uint64
}

type Publisher struct {
	registry model.DeliveryTargetRegistry
	signer   RestoreCredentialSigner
	store    repository.PublisherStore
	vault    repository.SecretDelivery
	now      func() time.Time
}

func NewPublisher(
	registry model.DeliveryTargetRegistry,
	signer RestoreCredentialSigner,
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
		store == nil ||
		vault == nil {
		return nil, errors.New("invalid publisher configuration")
	}
	return &Publisher{
		registry: registry,
		signer:   signer,
		store:    store,
		vault:    vault,
		now:      time.Now,
	}, nil
}

func (publisher *Publisher) Publish(
	ctx context.Context,
	controller ControllerIdentity,
	directiveCompact string,
	idempotencyKey string,
) (model.PublishedCredential, error) {
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

func (publisher *Publisher) Ready(ctx context.Context) error {
	if err := publisher.store.PublisherReady(ctx); err != nil {
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"publisher persistence is unavailable",
			err,
		)
	}
	if publisher.signer.Key.Private == nil || len(publisher.registry.Targets) == 0 {
		return failure.New(failure.PersistenceUnavailable, "publisher signer or target registry is unavailable")
	}
	return nil
}

func (publisher *Publisher) Registry() model.DeliveryTargetRegistry {
	return publisher.registry
}

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
