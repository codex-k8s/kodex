package platform

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// integrationBrokerIdentityWriteOwner carries only already owner-resolved metadata and OCC pins.
// A purpose adapter must first authorize the actor, exact bound package, explicit personal/service
// subject and target. These private helpers are persistence, not a generic setup or permission path.
// integrationBrokerIdentityWriteOwner содержит только owner-resolved metadata и OCC pins.
// Purpose adapter заранее проверяет actor, exact bound package, explicit personal/service subject
// и target. Private helpers сохраняют metadata, не предоставляя generic setup либо permission path.
type integrationBrokerIdentityWriteOwner struct {
	organizationID, connectionID, connectionRef, subjectID, bindingID, bindingRef, targetRef string
	connectionVersion, bindingVersion, bindingGeneration                                     int64
	policy                                                                                   entity.IntegrationCredentialPolicy
	purpose                                                                                  entity.IntegrationBrokerIdentityPurpose
}

// valid checks complete coordinates without inferring a kind, service subject or personal binding.
// valid проверяет полные coordinates без inference kind, service subject либо personal binding.
func (owner integrationBrokerIdentityWriteOwner) valid() bool {
	for _, raw := range []string{owner.organizationID, owner.connectionID, owner.subjectID} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			return false
		}
	}
	if !managedkubernetes.ValidLocator(owner.connectionRef) || !managedkubernetes.ValidLocator(owner.targetRef) ||
		!owner.purpose.Valid() || owner.connectionVersion < 1 {
		return false
	}
	if owner.policy == entity.IntegrationCredentialProjectShared {
		return owner.bindingID == "" && owner.bindingRef == "" && owner.bindingVersion == 0 && owner.bindingGeneration == 0
	}
	id, err := uuid.Parse(owner.bindingID)
	return owner.policy == entity.IntegrationCredentialInitiatorRequired && err == nil && id != uuid.Nil && id.String() == owner.bindingID &&
		managedkubernetes.ValidLocator(owner.bindingRef) && owner.bindingVersion > 0 && owner.bindingGeneration > 0
}

// lockIntegrationBrokerIdentityWriteOwnerTx rechecks active subject/policy and serializes revisions.
// The caller retains its root-source lock before entering this Connection → binding lock order.
// lockIntegrationBrokerIdentityWriteOwnerTx проверяет active subject/policy и сериализует revisions.
// Caller заранее удерживает root-source lock; порядок здесь — Connection → binding.
func lockIntegrationBrokerIdentityWriteOwnerTx(ctx context.Context, tx pgx.Tx, owner integrationBrokerIdentityWriteOwner) error {
	if !owner.valid() {
		return errs.ErrInvalid
	}
	var version int64
	var policy entity.IntegrationCredentialPolicy
	err := tx.QueryRow(ctx, queryBrokerIdentityConnectionLock, pgx.StrictNamedArgs{
		"organization_id": owner.organizationID, "connection_id": owner.connectionID, "connection_ref": owner.connectionRef,
		"subject_id": owner.subjectID,
	}).Scan(&version, &policy)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if version != owner.connectionVersion || policy != owner.policy {
		return errs.ErrVersionMismatch
	}
	if policy == entity.IntegrationCredentialProjectShared {
		return nil
	}
	var ref string
	var generation int64
	err = tx.QueryRow(ctx, queryBrokerIdentityBindingLock, pgx.StrictNamedArgs{
		"organization_id": owner.organizationID, "binding_id": owner.bindingID, "connection_id": owner.connectionID,
		"subject_id": owner.subjectID, "purpose": string(owner.purpose),
	}).Scan(&ref, &version, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if ref != owner.bindingRef || version != owner.bindingVersion || generation != owner.bindingGeneration {
		return errs.ErrVersionMismatch
	}
	return nil
}

// insertIntegrationBrokerIdentityRevisionTx allocates metadata under fresh Connection/binding locks.
// Unused/expired predecessors remain in the high-watermark; no Secret or lease generation is reused.
// Its result is not yet an executable credential and must remain unattached until purpose completion.
// insertIntegrationBrokerIdentityRevisionTx выделяет metadata под fresh Connection/binding locks.
// Неиспользованные/expired predecessors остаются в high-watermark; Secret и lease generation не переиспользуются.
// Result ещё не является executable credential и не привязывается до purpose completion.
func insertIntegrationBrokerIdentityRevisionTx(ctx context.Context, tx pgx.Tx, owner integrationBrokerIdentityWriteOwner) (string, entity.IntegrationBrokerIdentityRevision, error) {
	if err := lockIntegrationBrokerIdentityWriteOwnerTx(ctx, tx, owner); err != nil {
		return "", entity.IntegrationBrokerIdentityRevision{}, err
	}
	ref, err := newRef("bki")
	if err != nil {
		return "", entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
	}
	var id string
	var result entity.IntegrationBrokerIdentityRevision
	err = tx.QueryRow(ctx, queryBrokerIdentityInsert, pgx.StrictNamedArgs{
		"ref": ref, "organization_id": owner.organizationID, "connection_id": owner.connectionID,
		"binding_id": owner.bindingID, "subject_id": owner.subjectID, "policy": string(owner.policy),
		"purpose": string(owner.purpose), "target_ref": owner.targetRef,
	}).Scan(&id, &result.Ref, &result.TargetRef, &result.Purpose, &result.Revision, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
	}
	if err != nil {
		return "", entity.IntegrationBrokerIdentityRevision{}, mapWriteError(err)
	}
	if !result.ValidMetadata() {
		return "", entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
	}
	return id, result, nil
}

// attachIntegrationBrokerIdentityRevisionTx commits a purpose-validated material fact to its current owner.
// The caller must already have recorded exact immutable purpose material and closed the setup attempt
// in this transaction. This metadata helper never accepts a public locator or performs a material read.
// Existing tests and dependency grants are fenced before generation/source replacement.
// attachIntegrationBrokerIdentityRevisionTx привязывает purpose-validated material fact к current owner.
// Caller заранее сохраняет exact immutable purpose material и закрывает setup attempt в той же transaction.
// Metadata helper не принимает public locator и не читает material.
// Прежние tests и dependency grants закрываются до замены generation/source.
func (repository *Repository) attachIntegrationBrokerIdentityRevisionTx(ctx context.Context, tx pgx.Tx, current scope, owner integrationBrokerIdentityWriteOwner, identityID string, accessUntil time.Time) (entity.IntegrationBrokerIdentityRevision, error) {
	if current.organizationID != owner.organizationID || current.actorID == "" {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
	}
	viewUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: owner.connectionRef,
	})
	if err != nil {
		return entity.IntegrationBrokerIdentityRevision{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	accessUntil = earlierAccessDeadline(accessUntil, viewUntil)
	if owner.policy == entity.IntegrationCredentialProjectShared {
		sharedUntil, err := repository.requireSharedBrokerIdentityAccessUntil(ctx, tx, current, owner.connectionRef)
		if err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		accessUntil = earlierAccessDeadline(accessUntil, sharedUntil)
	}
	if accessDeadlineExpired(accessUntil) {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
	}
	if err := lockIntegrationBrokerIdentityWriteOwnerTx(ctx, tx, owner); err != nil {
		return entity.IntegrationBrokerIdentityRevision{}, err
	}
	var identity entity.IntegrationBrokerIdentityRevision
	err = tx.QueryRow(ctx, queryBrokerIdentityReadOwned, pgx.StrictNamedArgs{
		"organization_id": owner.organizationID, "identity_id": identityID, "connection_id": owner.connectionID,
		"subject_id": owner.subjectID, "binding_id": owner.bindingID, "policy": string(owner.policy),
		"purpose": string(owner.purpose), "target_ref": owner.targetRef,
	}).Scan(&identity.Ref, &identity.TargetRef, &identity.Purpose, &identity.Revision, &identity.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrNotFound
	}
	if err != nil || !identity.ValidMetadata() {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
	}
	// Each purpose must install its own completed-material proof before gaining attachment credit.
	// Каждый purpose устанавливает свой completed-material proof до допуска attachment.
	var authorizedUntil *time.Time
	switch owner.purpose {
	case entity.IntegrationBrokerIdentityKubernetes:
		err = tx.QueryRow(ctx, queryBrokerIdentityKubernetesFact, pgx.StrictNamedArgs{
			"identity_id": identityID, "organization_id": owner.organizationID, "connection_id": owner.connectionID,
		}).Scan(&authorizedUntil)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
		}
		if errors.Is(err, pgx.ErrNoRows) || authorizedUntil == nil {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
		}
		if err := cancelKubernetesIdentitySetupsTx(ctx, tx, owner.organizationID, owner.connectionRef, owner.bindingID); err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
	case entity.IntegrationBrokerIdentitySSH:
		authorizedUntil, err = readSSHIdentityAttachmentFactTx(ctx, tx, identityID, owner.organizationID, owner.connectionID)
		if err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		if err := repository.cancelSSHIdentityDependenciesTx(ctx, tx, current, owner.connectionRef, owner.bindingID); err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
	default:
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
	}
	if authorizedUntil == nil {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
	}
	attachmentUntil := earlierAccessDeadline(*authorizedUntil, accessUntil)
	if owner.policy == entity.IntegrationCredentialInitiatorRequired {
		if current.actorID != owner.subjectID {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
		}
		binding, err := readPersonalCredentialTx(ctx, tx, current, owner.connectionRef)
		if err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		if _, err := platformservice.ReplacePersonalIntegrationBrokerIdentity(binding, entity.IntegrationCredentialInitiator{
			OrganizationID: owner.organizationID, SubjectID: owner.subjectID, SubjectKind: "USER"}, owner.bindingVersion, identity); err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "PERSONAL_CREDENTIAL", owner.bindingRef); err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		if err := cancelPersonalCredentialTests(ctx, tx, owner.organizationID, owner.bindingID); err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, err
		}
		if accessDeadlineExpired(accessUntil) {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
		}
		tag, err := tx.Exec(ctx, queryBrokerIdentityAttachPersonal, pgx.StrictNamedArgs{
			"organization_id": owner.organizationID, "binding_id": owner.bindingID, "connection_id": owner.connectionID,
			"subject_id": owner.subjectID, "purpose": string(owner.purpose), "identity_id": identityID,
			"version": owner.bindingVersion, "generation": owner.bindingGeneration,
			"authorized_until": attachmentUntil,
		})
		if err != nil {
			return entity.IntegrationBrokerIdentityRevision{}, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
		}
		if accessDeadlineExpired(accessUntil) {
			return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
		}
		return identity, nil
	}
	if owner.connectionVersion == math.MaxInt64 {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
	}
	if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "CONNECTION", owner.connectionRef); err != nil {
		return entity.IntegrationBrokerIdentityRevision{}, err
	}
	if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest,
		pgx.StrictNamedArgs{"organization_id": owner.organizationID, "connection_ref": owner.connectionRef}); err != nil {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrUnavailable
	}
	if accessDeadlineExpired(accessUntil) {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
	}
	tag, err := tx.Exec(ctx, queryBrokerIdentityAttachShared, pgx.StrictNamedArgs{
		"organization_id": owner.organizationID, "connection_id": owner.connectionID, "identity_id": identityID,
		"version": owner.connectionVersion, "purpose": string(owner.purpose), "revision": identity.Revision,
		"subject_id":       owner.subjectID,
		"authorized_until": attachmentUntil,
	})
	if err != nil {
		return entity.IntegrationBrokerIdentityRevision{}, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrConflict
	}
	if accessDeadlineExpired(accessUntil) {
		return entity.IntegrationBrokerIdentityRevision{}, errs.ErrForbidden
	}
	return identity, nil
}
