package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// beginBrokerIdentitySetupOwnerTx resolves one explicit personal or SERVICE owner under current locks.
// Purpose must come from the exact bound package; a SERVICE selector never replaces organization authority.
// Personal setup rejects a SERVICE selector instead of silently discarding it or changing identity.
// beginBrokerIdentitySetupOwnerTx разрешает explicit personal либо SERVICE owner под current locks.
// Purpose выводится из exact bound package; SERVICE selector не заменяет organization authority.
// Personal setup отклоняет SERVICE selector без молчаливого игнорирования либо смены identity.
func (repository *Repository) beginBrokerIdentitySetupOwnerTx(ctx context.Context, tx pgx.Tx, current scope, connection lockedIntegrationConnection, ref string, expectedBinding int64, configuration map[string]string, purpose entity.IntegrationBrokerIdentityPurpose) (integrationBrokerIdentityWriteOwner, time.Time, error) {
	owner := integrationBrokerIdentityWriteOwner{organizationID: current.organizationID, connectionID: connection.id,
		connectionRef: ref, connectionVersion: connection.version, policy: connection.credentialPolicy,
		purpose: purpose, targetRef: configuration["target_ref"]}
	if !purpose.Valid() || !managedkubernetes.ValidLocator(owner.targetRef) {
		return owner, time.Time{}, errs.ErrConflict
	}
	if owner.policy == entity.IntegrationCredentialProjectShared {
		if expectedBinding != 0 {
			return owner, time.Time{}, errs.ErrInvalid
		}
		sharedUntil, err := repository.requireSharedBrokerIdentityAccessUntil(ctx, tx, current, ref)
		if err != nil {
			return owner, time.Time{}, err
		}
		serviceRef := configuration["service_subject_ref"]
		if !managedkubernetes.ValidLocator(serviceRef) {
			return owner, time.Time{}, errs.ErrConflict
		}
		if err := tx.QueryRow(ctx, queryBrokerIdentityServiceSubject, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "subject_ref": serviceRef,
		}).Scan(&owner.subjectID); errors.Is(err, pgx.ErrNoRows) {
			return owner, time.Time{}, errs.ErrNotFound
		} else if err != nil {
			return owner, time.Time{}, errs.ErrUnavailable
		}
		return owner, sharedUntil, nil
	}
	if owner.policy != entity.IntegrationCredentialInitiatorRequired || configuration["service_subject_ref"] != "" {
		return owner, time.Time{}, errs.ErrConflict
	}
	owner.subjectID = current.actorID
	binding, err := readPersonalCredentialTx(ctx, tx, current, ref)
	if errors.Is(err, errs.ErrNotFound) && expectedBinding == 0 {
		bindingRef, allocationErr := newRef("ipc")
		if allocationErr != nil {
			return owner, time.Time{}, errs.ErrUnavailable
		}
		if err := tx.QueryRow(ctx, queryPersonalCredentialCreate, pgx.StrictNamedArgs{
			"ref": bindingRef, "organization_id": current.organizationID, "subject_id": current.actorID,
			"connection_ref": ref, "connection_version": connection.version,
			"credential_kind": string(entity.IntegrationCredentialBrokerIdentity), "broker_purpose": string(owner.purpose),
		}).Scan(&bindingRef); err != nil {
			return owner, time.Time{}, mapWriteError(err)
		}
		binding, err = readPersonalCredentialTx(ctx, tx, current, ref)
	} else if err == nil && binding.Version != expectedBinding {
		return owner, time.Time{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return owner, time.Time{}, err
	}
	if binding.Kind != entity.IntegrationCredentialBrokerIdentity || binding.BrokerPurpose != owner.purpose {
		return owner, time.Time{}, errs.ErrConflict
	}
	if err := tx.QueryRow(ctx, queryPersonalCredentialLockOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "subject_id": current.actorID, "connection_ref": ref,
	}).Scan(&owner.bindingID, &owner.policy, &owner.connectionVersion); err != nil {
		return owner, time.Time{}, errs.ErrUnavailable
	}
	owner.bindingRef, owner.bindingVersion, owner.bindingGeneration = binding.Ref, binding.Version, binding.Generation
	return owner, time.Time{}, nil
}

// requireSharedBrokerIdentityAccessUntil checks both permissions required to change SERVICE custody and returns their earliest finite deadline.
// requireSharedBrokerIdentityAccessUntil проверяет оба разрешения, необходимые для изменения SERVICE custody, и возвращает наиболее ранний конечный срок.
func (repository *Repository) requireSharedBrokerIdentityAccessUntil(ctx context.Context, tx pgx.Tx, current scope, ref string) (time.Time, error) {
	manageUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.manage", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: ref,
	})
	if err != nil {
		return time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	organizationUntil, err := repository.requireAccessUntil(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef))
	if err != nil {
		return time.Time{}, concealAccessDenial(err, errs.ErrForbidden)
	}
	return earlierAccessDeadline(manageUntil, organizationUntil), nil
}

// queryBrokerIdentityServiceSubject selects only the explicit active tenant SERVICE under a shared lock.
// queryBrokerIdentityServiceSubject выбирает только explicit active tenant SERVICE под shared lock.
//
//go:embed sql/broker_identity__service_subject.sql
var queryBrokerIdentityServiceSubject string

// brokerIdentitySetupAuthorityLost recognizes only definitive owner denials; query, timeout, corruption and availability failures cannot become terminal authority transitions.
// brokerIdentitySetupAuthorityLost распознаёт лишь definitive owner denials; query, timeout, corruption и availability failures не превращаются в terminal authority transitions.
func brokerIdentitySetupAuthorityLost(err error) bool {
	if errors.Is(err, errs.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrVersionMismatch)
}

// commitIdentitySetupAdmission checks the current permission and the durable setup or attempt window on both sides of COMMIT before a broker permit can escape.
// A commit that is acknowledged after either deadline may leave a ledger row but never returns authorization to dispatch a storage effect.
// commitIdentitySetupAdmission проверяет действующее разрешение и срок durable setup или attempt до и после COMMIT, прежде чем выдать broker permit.
// Если COMMIT подтверждён после любого из сроков, запись может остаться в ledger, но разрешение на storage effect не выдаётся.
func commitIdentitySetupAdmission(ctx context.Context, tx pgx.Tx, permissionUntil, ledgerUntil time.Time) error {
	if accessDeadlineExpired(permissionUntil) || accessDeadlineExpired(ledgerUntil) {
		return errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	if accessDeadlineExpired(permissionUntil) || accessDeadlineExpired(ledgerUntil) {
		return errs.ErrForbidden
	}
	return nil
}
