package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesSetupBeginOperation fixes admission authority and audit independently from payload hints.
// kubernetesSetupBeginOperation фиксирует admission authority и audit независимо от payload hints.
const kubernetesSetupBeginOperation = "platform.integrations.kubernetes.setup.begin"

// kubernetesSetupImmutableInput contains only owner-derived pins used by the private snapshot digest.
// Explicit fields prevent a redacted provision's json omission from silently weakening its digest.
// kubernetesSetupImmutableInput содержит только owner-derived pins для private snapshot digest.
// Явные fields не позволяют json omission скрытого provision незаметно ослабить digest.
type kubernetesSetupImmutableInput struct {
	SetupRef, OrganizationID, ActorID, ConnectionID, IdentityID, IdentityRef string
	TargetRef, DefinitionDigest, ConfigurationDigest, ContentDigest          string
	IdentityRevision, RegistryRevision, TargetRevision, ConnectionVersion    int64
	BindingVersion, BindingGeneration                                        int64
	Policy                                                                   entity.IntegrationCredentialPolicy
	ContentBytes                                                             uint32
	ExpiresAt                                                                time.Time
}

// String keeps original owner coordinates and content hashes out of diagnostics.
// String не допускает original owner coordinates и content hashes в диагностику.
func (kubernetesSetupImmutableInput) String() string { return "[Kubernetes immutable setup input]" }

// GoString applies the same redaction to detailed formatting of immutable setup input.
// GoString сохраняет redaction при подробном форматировании immutable setup input.
func (input kubernetesSetupImmutableInput) GoString() string { return input.String() }

// beginKubernetesSetupOwnerTx fixes the purpose already established by the exact Kubernetes package.
// It shares metadata ownership resolution without generalizing target or material authority.
// beginKubernetesSetupOwnerTx фиксирует purpose, установленный exact Kubernetes package.
// Разделяет metadata ownership resolution без обобщения target либо material authority.
func (repository *Repository) beginKubernetesSetupOwnerTx(ctx context.Context, tx pgx.Tx, current scope, connection lockedIntegrationConnection, ref string, expectedBinding int64, configuration map[string]string) (integrationBrokerIdentityWriteOwner, time.Time, error) {
	return repository.beginBrokerIdentitySetupOwnerTx(ctx, tx, current, connection, ref, expectedBinding, configuration, entity.IntegrationBrokerIdentityKubernetes)
}

// BeginKubernetesIdentitySetup durably allocates one immutable intent before returning its sole permit.
// Replay returns safe metadata without recovering the permit; a lost acknowledgement is reconciled
// by the original Begin key. No kubeconfig or caller-selected target enters the control plane.
// BeginKubernetesIdentitySetup устойчиво выделяет один immutable intent до возврата единственного permit.
// Replay возвращает safe metadata без восстановления permit; lost acknowledgement сверяется по
// original Begin key. Kubeconfig и caller-selected target не входят в control plane.
func (repository *Repository) BeginKubernetesIdentitySetup(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.KubernetesIdentitySetupBegin) (result entity.KubernetesIdentitySetupAdmission, resultErr error) {
	if err := kubernetesSetupAPICaller(principal, kubernetesSetupBeginOperation); err != nil {
		return result, err
	}
	if len(input.ConnectionRef) > 96 || !managedkubernetes.ValidLocator(input.ConnectionRef) || input.ExpectedConnectionVersion < 1 ||
		mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 0 || !managedkubernetes.ValidDigest(input.ContentSHA256) ||
		input.ContentBytes == 0 || input.ContentBytes > managedkubernetes.MaximumCredentialBytes ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 || !utf8.ValidString(mutation.IdempotencyKey) ||
		strings.ContainsAny(mutation.IdempotencyKey, "\r\n\x00") {
		return result, errs.ErrInvalid
	}
	intent, err := json.Marshal(struct {
		Input          command.KubernetesIdentitySetupBegin
		BindingVersion int64
	}{input, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrInvalid
	}
	intentHash := sha256.Sum256(intent)
	intentDigest := hex.EncodeToString(intentHash[:])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	viewUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef,
	})
	if err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	source, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, queryKubernetesSetupSerializeActor, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
	}); err != nil {
		return result, errs.ErrUnavailable
	}
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, input.ConnectionRef)
	if err != nil {
		return result, err
	}
	var setupRef, storedConnection, storedIntent string
	err = tx.QueryRow(ctx, queryKubernetesSetupBeginReceipt, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "begin_key": mutation.IdempotencyKey,
	}).Scan(&setupRef, &storedConnection, &storedIntent)
	if err == nil {
		if storedConnection != input.ConnectionRef || storedIntent != intentDigest {
			return result, errs.ErrIdempotencyReuse
		}
		result.View, err = readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupReadOwned, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID,
			"connection_ref": input.ConnectionRef, "setup_ref": setupRef, "begin_key": "",
		}))
		if err != nil {
			return entity.KubernetesIdentitySetupAdmission{}, err
		}
		if err := commitIdentitySetupAdmission(ctx, tx, viewUntil, time.Time{}); err != nil {
			return entity.KubernetesIdentitySetupAdmission{}, err
		}
		result.ReconcileOnly = true
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if repository.kubernetesOperator == nil || repository.kubernetesOperatorDigest != source.digest {
		return result, errs.ErrUnavailable
	}
	if connection.version != input.ExpectedConnectionVersion {
		return result, errs.ErrVersionMismatch
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, input.ConnectionRef,
		connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
	if err != nil {
		return result, err
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || definition.Spec.Adapter != "KUBERNETES" || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity ||
		delivery.BrokerPurpose != string(entity.IntegrationBrokerIdentityKubernetes) {
		return result, errs.ErrConflict
	}
	var rawConfiguration []byte
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesSetupBeginContext, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_id": connection.id,
	}).Scan(&rawConfiguration, &now) != nil || len(rawConfiguration) > 16384 {
		return result, errs.ErrUnavailable
	}
	now = now.UTC()
	var configuration map[string]string
	if json.Unmarshal(rawConfiguration, &configuration) != nil || definition.ValidateConfiguration(configuration) != nil {
		return result, errs.ErrConflict
	}
	expectedBinding := *mutation.ExpectedVersion
	if connection.credentialPolicy == entity.IntegrationCredentialProjectShared {
		if expectedBinding != connection.version {
			return result, errs.ErrVersionMismatch
		}
		// Shared OCC belongs to Connection; no personal binding is invented.
		// Shared OCC относится к Connection; personal binding не создаётся.
		expectedBinding = 0
	}
	owner, sharedUntil, err := repository.beginKubernetesSetupOwnerTx(ctx, tx, current, connection, input.ConnectionRef, expectedBinding, configuration)
	if err != nil {
		return result, err
	}
	var target value.ManagedKubernetesTarget
	for _, entry := range source.registry.Targets {
		if entry.Target.Ref == owner.targetRef {
			target = entry.Target
		}
	}
	if target.Ref == "" || source.registry.UploadEndpoint.Validate(now) != nil {
		return result, errs.ErrUnavailable
	}
	var capacity bool
	if tx.QueryRow(ctx, queryKubernetesSetupCapacity, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "connection_id": connection.id,
	}).Scan(&capacity) != nil {
		return result, errs.ErrUnavailable
	}
	if !capacity {
		return result, errs.ErrConflict
	}
	identityID, identity, err := insertIntegrationBrokerIdentityRevisionTx(ctx, tx, owner)
	if err != nil {
		return result, err
	}
	setupRef, err = newRef("ksu")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	permit := make([]byte, 32)
	if _, err := rand.Read(permit); err != nil {
		return result, errs.ErrUnavailable
	}
	permitHash := sha256.Sum256(permit)
	configurationBytes, err := json.Marshal(configuration)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	configurationHash := sha256.Sum256(configurationBytes)
	configurationDigest := hex.EncodeToString(configurationHash[:])
	accessUntil := earlierAccessDeadline(viewUntil, sharedUntil)
	expiresAt := earlierAccessDeadline(now.Add(90*time.Second), accessUntil).UTC()
	if !expiresAt.After(now) || accessDeadlineExpired(accessUntil) {
		return result, errs.ErrForbidden
	}
	snapshot := kubernetesSetupImmutableInput{SetupRef: setupRef, OrganizationID: current.organizationID, ActorID: current.actorID,
		ConnectionID: connection.id, IdentityID: identityID, IdentityRef: identity.Ref, TargetRef: target.Ref,
		IdentityRevision: identity.Revision, RegistryRevision: source.registry.Revision, TargetRevision: target.Revision,
		ConnectionVersion: owner.connectionVersion, BindingVersion: owner.bindingVersion, BindingGeneration: owner.bindingGeneration,
		Policy: owner.policy, DefinitionDigest: connection.definitionDigest, ConfigurationDigest: configurationDigest,
		ContentDigest: input.ContentSHA256, ContentBytes: input.ContentBytes, ExpiresAt: expiresAt}
	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	snapshotHash := sha256.Sum256(snapshotBytes)
	if _, err := tx.Exec(ctx, queryKubernetesSetupInsert, pgx.StrictNamedArgs{
		"ref": setupRef, "organization_id": current.organizationID, "connection_id": connection.id, "actor_id": current.actorID,
		"identity_id": identityID, "registry_revision": source.registry.Revision, "target_ref": target.Ref, "target_revision": target.Revision,
		"connection_version": owner.connectionVersion, "binding_version": owner.bindingVersion, "binding_generation": owner.bindingGeneration,
		"definition_digest": connection.definitionDigest, "configuration_digest": configurationDigest, "begin_key": mutation.IdempotencyKey,
		"intent_digest": intentDigest, "permit_digest": hex.EncodeToString(permitHash[:]), "snapshot_digest": hex.EncodeToString(snapshotHash[:]),
		"content_digest": input.ContentSHA256, "content_bytes": input.ContentBytes, "created_at": now, "expires_at": expiresAt,
	}); err != nil {
		return result, mapWriteError(err)
	}
	result.View, err = readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupReadOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"connection_ref": input.ConnectionRef, "setup_ref": setupRef, "begin_key": "",
	}))
	if err != nil {
		return entity.KubernetesIdentitySetupAdmission{}, err
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.KubernetesIdentitySetupAdmission{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef,
		current.organizationID, nil, current.actorID, kubernetesSetupBeginOperation, "INTEGRATION_CONNECTION", input.ConnectionRef,
		"i18n:KUBERNETES_IDENTITY_SETUP_PREPARED", current.correlationRef); err != nil {
		return entity.KubernetesIdentitySetupAdmission{}, errs.ErrUnavailable
	}
	if err := commitIdentitySetupAdmission(ctx, tx, accessUntil, expiresAt); err != nil {
		return entity.KubernetesIdentitySetupAdmission{}, err
	}
	result.Permit, result.Endpoint = permit, source.registry.UploadEndpoint
	return result, nil
}
