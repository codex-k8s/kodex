package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesQuarantinePersonalLock selects only the still-current original purpose identity.
// queryKubernetesQuarantinePersonalLock выбирает только ещё текущую original purpose identity.
//
//go:embed sql/kubernetes_identity__quarantine_personal_lock.sql
var queryKubernetesQuarantinePersonalLock string

// queryKubernetesQuarantinePersonal advances the exact old binding fence without replacing its source.
// queryKubernetesQuarantinePersonal продвигает exact old binding fence без замены source.
//
//go:embed sql/kubernetes_identity__quarantine_personal.sql
var queryKubernetesQuarantinePersonal string

// queryKubernetesQuarantineSharedCurrent refuses to invalidate a subsequently selected new identity.
// queryKubernetesQuarantineSharedCurrent запрещает инвалидировать subsequently selected new identity.
//
//go:embed sql/kubernetes_identity__quarantine_shared_current.sql
var queryKubernetesQuarantineSharedCurrent string

// queryKubernetesQuarantineShared resets only current old shared test evidence and Connection version.
// queryKubernetesQuarantineShared сбрасывает только current old shared test evidence и Connection version.
//
//go:embed sql/kubernetes_identity__quarantine_shared.sql
var queryKubernetesQuarantineShared string

// lockKubernetesQuarantineBindingTx reserves the original current binding before setup/attempt locks.
// A moved or already revoked source yields no work and cannot revoke a new setup generation on replay.
// lockKubernetesQuarantineBindingTx резервирует original current binding до setup/attempt locks.
// Moved либо already revoked source не создаёт work и не отзывает new setup generation при replay.
func lockKubernetesQuarantineBindingTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow) (string, error) {
	if row.owner.policy != entity.IntegrationCredentialInitiatorRequired {
		return "", nil
	}
	var ref string
	err := tx.QueryRow(ctx, queryKubernetesQuarantinePersonalLock, pgx.StrictNamedArgs{
		"organization_id": row.owner.organizationID, "binding_id": row.owner.bindingID,
		"connection_id": row.owner.connectionID, "subject_id": row.owner.subjectID, "identity_id": row.input.IdentityID,
	}).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	return ref, nil
}

// quarantineKubernetesIdentityTx closes only still-current old authority after conflicting ACK evidence.
// The purpose receipt remains immutable and every new read/test/effect must also reject that RI.
// It performs no external cleanup, preserves completed setup history and never revokes a moved source.
// quarantineKubernetesIdentityTx закрывает только ещё текущую old authority после conflicting ACK evidence.
// Purpose receipt остаётся immutable; каждый new read/test/effect также обязан отвергать эту RI.
// Не выполняет external cleanup, сохраняет completed setup history и не отзывает moved source.
func (repository *Repository) quarantineKubernetesIdentityTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow, bindingRef, correlation string) error {
	current := scope{organizationID: row.input.OrganizationID, organizationRef: row.organizationRef,
		actorID: row.input.ActorID, actorRef: row.actorRef, correlationRef: correlation}
	if row.owner.policy == entity.IntegrationCredentialInitiatorRequired {
		if bindingRef == "" {
			return nil
		}
		if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "PERSONAL_CREDENTIAL", bindingRef); err != nil {
			return err
		}
		if err := cancelPersonalCredentialTests(ctx, tx, row.owner.organizationID, row.owner.bindingID); err != nil {
			return err
		}
		if err := cancelKubernetesIdentitySetupsTx(ctx, tx, row.owner.organizationID, row.connectionRef, row.owner.bindingID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, queryKubernetesQuarantinePersonal, pgx.StrictNamedArgs{
			"organization_id": row.owner.organizationID, "binding_id": row.owner.bindingID,
			"connection_id": row.owner.connectionID, "subject_id": row.owner.subjectID, "identity_id": row.input.IdentityID,
		})
		if err != nil {
			return mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		return nil
	}
	var selected bool
	args := pgx.StrictNamedArgs{"organization_id": row.owner.organizationID, "connection_id": row.owner.connectionID, "identity_id": row.input.IdentityID}
	if tx.QueryRow(ctx, queryKubernetesQuarantineSharedCurrent, args).Scan(&selected) != nil {
		return errs.ErrUnavailable
	}
	if !selected {
		return nil
	}
	if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "CONNECTION", row.connectionRef); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest,
		pgx.StrictNamedArgs{"organization_id": row.owner.organizationID, "connection_ref": row.connectionRef}); err != nil {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryKubernetesQuarantineShared, args)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
