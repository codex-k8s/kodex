package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// sshIdentityMaterialNamespace is a fixed isolated purpose store, never a caller namespace or the control-plane namespace containing unrelated platform credentials.
// sshIdentityMaterialNamespace задаёт fixed isolated purpose store, а не caller namespace либо control-plane namespace с другими platform credentials.
const sshIdentityMaterialNamespace = "kodex-ssh-identities"

// querySSHSetupContext reads the locked connection's exact stored configuration and owner clock only for an active USER; a SERVICE is the selected shared credential owner, never the browser initiator.
// querySSHSetupContext читает exact stored configuration и owner clock locked Connection лишь для active USER; SERVICE является selected shared credential owner, а не browser initiator.
//
//go:embed sql/ssh_setup__context.sql
var querySSHSetupContext string

// sshSetupOwnerContext retains the exact current source used to allocate a private revision; an absent profile, operator target or verification cannot be inferred from these declarative coordinates.
// sshSetupOwnerContext сохраняет exact current source для private revision; эти declarative coordinates не доказывают profile, operator target либо verification.
type sshSetupOwnerContext struct {
	owner                                 integrationBrokerIdentityWriteOwner
	definitionDigest, configurationDigest string
	now, accessUntil                      time.Time
}

// sshSetupOwnerTx authorizes one exact bound SSH-purpose package and configuration before common personal/SERVICE allocation; neither configuration nor a requested subject grants authority by itself.
// sshSetupOwnerTx проверяет exact bound SSH-purpose package и configuration до common personal/SERVICE allocation; configuration и requested subject сами не предоставляют authority.
func (repository *Repository) sshSetupOwnerTx(ctx context.Context, tx pgx.Tx, current scope, connection lockedIntegrationConnection, ref string, expectedBinding int64) (sshSetupOwnerContext, error) {
	var result sshSetupOwnerContext
	viewUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: ref})
	if err != nil {
		return result, sshSetupAccessError(err)
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, ref, connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
	if err != nil {
		return result, err
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || definition.Spec.Adapter != "SSH_TARGET" || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != managedssh.IdentityPurpose {
		return result, errs.ErrConflict
	}
	var raw []byte
	if tx.QueryRow(ctx, querySSHSetupContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_id": connection.id, "actor_id": current.actorID}).Scan(&raw, &result.now) != nil || len(raw) > 16384 || result.now.IsZero() {
		return result, errs.ErrUnavailable
	}
	result.now = result.now.UTC()
	var configuration map[string]string
	if json.Unmarshal(raw, &configuration) != nil || definition.ValidateConfiguration(configuration) != nil || !managedssh.OwnerReference(configuration["target_ref"]) {
		return result, errs.ErrConflict
	}
	for key := range configuration {
		if key != "target_ref" && key != "service_subject_ref" {
			return result, errs.ErrConflict
		}
	}
	canonical, err := json.Marshal(configuration)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	digest := sha256.Sum256(canonical)
	result.configurationDigest = hex.EncodeToString(digest[:])
	result.definitionDigest = connection.definitionDigest
	var sharedUntil time.Time
	result.owner, sharedUntil, err = repository.beginBrokerIdentitySetupOwnerTx(ctx, tx, current, connection, ref, expectedBinding, configuration, entity.IntegrationBrokerIdentitySSH)
	result.accessUntil = earlierAccessDeadline(viewUntil, sharedUntil)
	if err == nil && accessDeadlineExpired(result.accessUntil) {
		err = errs.ErrForbidden
	}
	return result, err
}

// sshSetupImmutableInput contains every owner/material coordinate hashed before the sole permit is returned; explicit exported fields prevent redacted locator JSON tags from weakening the private digest.
// sshSetupImmutableInput содержит все owner/material coordinates, хешируемые до возврата sole permit; explicit exported fields не дают redacted locator JSON tags ослабить private digest.
type sshSetupImmutableInput struct {
	SetupRef, OrganizationID, ActorID, ConnectionID, ConnectionRef, IdentityID, IdentityRef, Namespace string
	SubjectID, BindingID, BindingRef, TargetRef, DefinitionDigest, ConfigurationDigest, ContentDigest  string
	IdentityRevision, ConnectionVersion, BindingVersion, BindingGeneration                             int64
	Policy                                                                                             entity.IntegrationCredentialPolicy
	ContentBytes                                                                                       uint32
	ExpiresAt                                                                                          time.Time
}

// String hides private integrity and owner pins from diagnostics even though all fields must remain present in the immutable digest.
// String скрывает private integrity и owner pins из diagnostics, сохраняя все fields в immutable digest.
func (sshSetupImmutableInput) String() string { return "[SSH immutable setup input]" }

// GoString preserves the same private diagnostic boundary for detailed Go formatting.
// GoString сохраняет ту же private diagnostic boundary при подробном Go formatting.
func (input sshSetupImmutableInput) GoString() string { return input.String() }

// digest rejects malformed owner metadata and canonicalizes only UTC time; it never trims source references, normalizes case or derives an actor from a broker payload.
// digest отклоняет malformed owner metadata и канонизирует лишь UTC time; source references не обрезаются, case не меняется, actor не выводится из broker payload.
func (input sshSetupImmutableInput) digest() (string, error) {
	for _, raw := range []string{input.OrganizationID, input.ActorID, input.ConnectionID, input.IdentityID, input.SubjectID} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			return "", errs.ErrUnavailable
		}
	}
	for _, ref := range []string{input.SetupRef, input.ConnectionRef, input.IdentityRef, input.TargetRef} {
		if !managedssh.OwnerReference(ref) {
			return "", errs.ErrUnavailable
		}
	}
	if input.Namespace != sshIdentityMaterialNamespace || !managedssh.Digest(input.DefinitionDigest) || !managedssh.Digest(input.ConfigurationDigest) || !managedssh.Digest(input.ContentDigest) || input.ConnectionVersion < 1 || input.IdentityRevision < 1 || input.ContentBytes < 1 || input.ContentBytes > 16384 || input.ExpiresAt.IsZero() {
		return "", errs.ErrUnavailable
	}
	switch input.Policy {
	case entity.IntegrationCredentialInitiatorRequired:
		id, err := uuid.Parse(input.BindingID)
		if err != nil || id == uuid.Nil || id.String() != input.BindingID || !managedssh.OwnerReference(input.BindingRef) || input.BindingVersion < 1 || input.BindingGeneration < 1 || input.ActorID != input.SubjectID {
			return "", errs.ErrUnavailable
		}
	case entity.IntegrationCredentialProjectShared:
		if input.BindingID != "" || input.BindingRef != "" || input.BindingVersion != 0 || input.BindingGeneration != 0 {
			return "", errs.ErrUnavailable
		}
	default:
		return "", errs.ErrUnavailable
	}
	input.ExpiresAt = input.ExpiresAt.UTC()
	raw, err := json.Marshal(input)
	if err != nil {
		return "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// sshPurposeReference checks an owner-assigned purpose prefix without treating a syntactically valid reference as authority.
// sshPurposeReference проверяет owner-assigned purpose prefix, не считая syntactically valid reference полномочием.
func sshPurposeReference(ref, prefix string) bool {
	return managedssh.OwnerReference(ref) && strings.HasPrefix(ref, prefix) && len(ref) >= len(prefix)+8
}

// sshSetupAccessError conceals denied resource visibility while retaining unavailable infrastructure as unavailable; an SQL failure never proves a hidden or absent owner.
// sshSetupAccessError скрывает denied resource visibility, сохраняя infrastructure failure как unavailable; SQL failure не доказывает hidden либо absent owner.
func sshSetupAccessError(err error) error {
	if err == nil {
		return nil
	}
	if brokerIdentitySetupAuthorityLost(err) {
		return errs.ErrNotFound
	}
	return errs.ErrUnavailable
}
