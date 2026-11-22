package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryArtifactManualPurgeIssue reads a bounded generation under the existing artifact head lock.
// queryArtifactManualPurgeIssue читает ограниченное поколение под existing artifact head lock.
//
//go:embed sql/artifacts_purge_manual_issue.sql
var queryArtifactManualPurgeIssue string

// queryArtifactManualPurgeStore retains the immutable intent until its owner finalizes custody.
// queryArtifactManualPurgeStore сохраняет immutable intent до owner finalization custody.
//
//go:embed sql/artifacts_purge_manual_store.sql
var queryArtifactManualPurgeStore string

// queryArtifactManualPurgeLock checks the exact owner and generation before metadata finalization.
// queryArtifactManualPurgeLock проверяет exact owner и generation до metadata finalization.
//
//go:embed sql/artifacts_purge_manual_lock.sql
var queryArtifactManualPurgeLock string

// queryArtifactManualPurgeAdmit records one irreversible attempt inside the original intent receipt.
// queryArtifactManualPurgeAdmit фиксирует одну irreversible attempt в original intent receipt.
//
//go:embed sql/artifacts_purge_manual_admit.sql
var queryArtifactManualPurgeAdmit string

// queryArtifactManualPurgeAttempted reads durable admission under the already held head mutex.
// queryArtifactManualPurgeAttempted читает durable admission под уже удерживаемым head mutex.
//
//go:embed sql/artifacts_purge_manual_attempted.sql
var queryArtifactManualPurgeAttempted string

// queryArtifactManualPurgeAttemptCount preserves the count when private object locators are scrubbed.
// queryArtifactManualPurgeAttemptCount сохраняет count при очистке private object locators.
//
//go:embed sql/artifacts_purge_manual_attempt_count.sql
var queryArtifactManualPurgeAttemptCount string

// queryArtifactManualProjectLock serializes durable intent with project Trash/Purge, before head locks.
// queryArtifactManualProjectLock сериализует durable intent с project Trash/Purge до head locks.
//
//go:embed sql/artifacts_purge_project_lock.sql
var queryArtifactManualProjectLock string

// queryArtifactManualProjectPending reads only the count of unresolved manual custody.
// queryArtifactManualProjectPending читает только count незавершённой manual custody.
//
//go:embed sql/artifacts_purge_project_pending.sql
var queryArtifactManualProjectPending string

// lockManualPurgeProjectTx keeps the existing project state through durable intent commit;
// projectless artifacts retain their ordinary org boundary. It grants no new source visibility.
// lockManualPurgeProjectTx сохраняет existing project state до durable intent commit;
// projectless artifacts сохраняют ordinary org boundary. Новую source visibility не выдаёт.
func lockManualPurgeProjectTx(ctx context.Context, tx pgx.Tx, current scope, projectID string) error {
	if projectID == "" {
		return nil
	}
	var id string
	if err := tx.QueryRow(ctx, queryArtifactManualProjectLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID}).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		return mapWriteError(err)
	}
	if id != projectID {
		return errs.ErrConflict
	}
	return nil
}

// recheckManualPurgeAccess preserves the first allowance through every wait and commit boundary.
// recheckManualPurgeAccess сохраняет first allowance через все waits и commit boundaries.
func (repository *Repository) recheckManualPurgeAccess(ctx context.Context, tx pgx.Tx, current scope, target resolvedAccessTarget, first time.Time) (time.Time, error) {
	fresh, err := repository.requireAccessUntil(ctx, tx, current, "artifact.purge", target)
	until := earlierAccessDeadline(first, fresh)
	if err != nil {
		return time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	if accessDeadlineExpired(until) {
		return time.Time{}, errs.ErrNotFound
	}
	return until, nil
}

// lockManualArtifactPurge rejects stale, foreign and expired custody before deleting owner rows.
// lockManualArtifactPurge отклоняет stale, foreign и expired custody до удаления owner rows.
func (repository *Repository) lockManualArtifactPurge(ctx context.Context, tx pgx.Tx, current scope, mutation value.Mutation, receipt artifactPurgeReceipt) error {
	var witness int
	if err := tx.QueryRow(ctx, queryArtifactManualPurgeLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_id": receipt.ArtifactID, "actor_id": current.actorID,
		"idempotency_key": mutation.IdempotencyKey, "claim_owner": receipt.ClaimOwner, "claim_generation": receipt.ClaimGeneration,
	}).Scan(&witness); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		return mapWriteError(err)
	}
	return nil
}

// admitManualArtifactDelete commits one attempt before storage IO. Replay only observes the same
// exact version; an uncertain commit never invokes DeleteOnce. No caller can renew the old grant.
// admitManualArtifactDelete фиксирует attempt до storage IO. Replay только наблюдает ту же exact
// version; uncertain commit не вызывает DeleteOnce. Caller не продлевает прежний grant.
func (repository *Repository) admitManualArtifactDelete(ctx context.Context, current scope, mutation value.Mutation, receipt artifactPurgeReceipt, object artifactPurgeObject) (bool, time.Time, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, time.Time{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "artifact.purge", "ARTIFACT", receipt.ArtifactRef, "")
	if err != nil {
		return false, time.Time{}, err
	}
	until, err := repository.recheckManualPurgeAccess(ctx, tx, current, target, receipt.ClaimUntil)
	if err != nil {
		return false, time.Time{}, err
	}
	if err := repository.lockManualArtifactPurge(ctx, tx, current, mutation, receipt); err != nil {
		return false, time.Time{}, err
	}
	var attempted bool
	if err := tx.QueryRow(ctx, queryArtifactManualPurgeAttempted, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "idempotency_key": mutation.IdempotencyKey,
		"intent_digest": mutation.IntentDigest, "artifact_id": receipt.ArtifactID, "revision_id": object.RevisionID,
	}).Scan(&attempted); err != nil {
		return false, time.Time{}, mapWriteError(err)
	}
	if !attempted {
		if err := repository.requireManualDeleteCapability(ctx); err != nil {
			return false, time.Time{}, err
		}
	}
	var admitted bool
	var ownerUntil time.Time
	if err := tx.QueryRow(ctx, queryArtifactManualPurgeAdmit, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_id": receipt.ArtifactID, "actor_id": current.actorID,
		"idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest,
		"claim_owner": receipt.ClaimOwner, "claim_generation": receipt.ClaimGeneration, "revision_id": object.RevisionID,
	}).Scan(&admitted, &ownerUntil); err != nil {
		return false, time.Time{}, errs.ErrConflict
	}
	until = earlierAccessDeadline(until, ownerUntil)
	until, err = repository.recheckManualPurgeAccess(ctx, tx, current, target, until)
	if err != nil {
		return false, time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, time.Time{}, errs.ErrConflict
	}
	if accessDeadlineExpired(until) {
		return false, time.Time{}, errs.ErrNotFound
	}
	return admitted, until, nil
}

// requireManualDeleteCapability gates only a new irreversible reservation, never safe readback.
// requireManualDeleteCapability проверяет только новую irreversible reservation, не safe readback.
func (repository *Repository) requireManualDeleteCapability(ctx context.Context) error {
	deleter, ok := repository.objects.(objectstorage.SingleAttemptDeleter)
	if !ok || deleter.CheckDeleteOnce(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// reconcileManualArtifactDelete uses the optional one-attempt transport only after durable admission.
// Replay and uncertain prior outcomes use Head alone, never an ordinary Delete fallback.
// reconcileManualArtifactDelete использует optional one-attempt transport только после durable
// admission. Replay и uncertain prior outcome используют Head, без fallback на обычный Delete.
func (repository *Repository) reconcileManualArtifactDelete(ctx context.Context, object artifactPurgeObject, admitted bool) error {
	acknowledged := false
	if admitted {
		deleter, ok := repository.objects.(objectstorage.SingleAttemptDeleter)
		if !ok {
			return errs.ErrUnavailable
		}
		if err := deleter.DeleteOnce(ctx, object.ObjectKey, object.ObjectVersion); err != nil {
			if errors.Is(err, objectstorage.ErrNotFound) {
				return errs.ErrUnavailable
			}
			return mapObjectStorageError(err)
		}
		acknowledged = true
	}
	if _, err := repository.objects.Head(ctx, object.ObjectKey, object.ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		if err == nil {
			return errs.ErrConflict
		}
		return mapObjectStorageError(err)
	}
	if !acknowledged {
		if err := objectstorage.VerifyVersionAbsent(ctx, repository.objects, object.ObjectKey, object.ObjectVersion); err != nil {
			return mapObjectStorageError(err)
		}
	}
	return nil
}
