package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const (
	// sessionArchiveIdleAfter sets the fifteen-minute idle threshold supplied to snapshot
	// materialization. Pending execution and live leases are independent SQL blockers; reaching this
	// time alone does not authorize archival.
	// sessionArchiveIdleAfter задаёт пятнадцатиминутный idle threshold для snapshot materialization.
	// Pending execution и live leases остаются отдельными SQL blockers; одного истечения времени
	// недостаточно для archival.
	sessionArchiveIdleAfter = 15 * time.Minute
	// sessionArchiveLeaseDuration sets the two-minute expiry requested on claim/renew. Exact
	// lease/fence/generation and an unexpired prior claim remain mandatory; the constant does not
	// extend a task by itself.
	// sessionArchiveLeaseDuration задаёт двухминутный expiry для claim/renew. Exact
	// lease/fence/generation и unexpired prior claim обязательны; сама константа task не продлевает.
	sessionArchiveLeaseDuration = 2 * time.Minute
	// sessionArchiveRetention sets thirty-day archive retention for materialization/completion and
	// superseded-object scheduling. Retention is a storage lifecycle input, not permission to read or
	// delete an archive.
	// sessionArchiveRetention задаёт тридцатидневный archive retention для materialization/completion
	// и superseded-object scheduling. Retention — storage lifecycle input, а не permission читать либо
	// удалять archive.
	sessionArchiveRetention = 30 * 24 * time.Hour
	// sessionArchiveMaxAttempts limits newly materialized archive tasks to five attempts before SQL
	// dead-letter handling. It does not permit an external retry without the next owner claim or prove
	// completion of prior work.
	// sessionArchiveMaxAttempts ограничивает новые archive tasks пятью attempts до SQL dead-letter
	// handling. Не разрешает external retry без next owner claim и не доказывает завершение прежней
	// work.
	sessionArchiveMaxAttempts = 5
)

// sessionArchiveCandidate holds selected task/runtime/session/source/archive descriptor pins
// before a fresh owner claim is assigned. These internal locators and prior attempt data are not
// worker authority until claim commit returns exact lease/fence/generation.
// sessionArchiveCandidate хранит selected task/runtime/session/source/archive descriptor pins до
// назначения fresh owner claim. Internal locators и prior attempt data не являются worker
// authority до claim commit с exact lease/fence/generation.
type sessionArchiveCandidate struct {
	taskID, taskRef, kind, inputDigest, objectKey, objectVersion string
	organizationRef, projectRef, sessionRef, providerAccountRef  string
	runtimeRevisionRef, runtimeRevisionDigest, codexSessionID    string
	sourceRelativePath, sourceSHA256                             string
	archiveRef, archiveObjectKey, archiveObjectVersion           string
	archiveObjectETag, archiveObjectDigest                       string
	archiveSourceRelativePath, archiveSourceSHA256               string
	contentGeneration, runtimeRevisionVersion, sourceSizeBytes   int64
	archiveFormatVersion                                         int32
	archiveObjectSizeBytes, archiveSourceSizeBytes               int64
	attempt                                                      int32
}

// ClaimSessionArchiveTasks requires the exact worker/claim permission, nonblank instance and
// limit1..16, then materializes the trusted global sweep and wakes changed storage before tenant
// task selection. It assigns fresh lease/fence, per-attempt snapshot key, input digest and
// generation, emitting storage notifications inside one ReadCommitted commit. Any error returns no
// claims; external archive/PVC/object effects occur only after committed tasks leave this method.
// ClaimSessionArchiveTasks требует exact worker/claim permission, nonblank instance и limit1..16,
// затем выполняет trusted global materializer sweep и storage wake до tenant task selection.
// Назначает fresh lease/fence, per-attempt snapshot key, input digest и generation со storage
// notifications в одном ReadCommitted commit. Любая ошибка не выдаёт claims; external
// archive/PVC/object effects начинаются после committed tasks вне метода.
func (repository *Repository) ClaimSessionArchiveTasks(ctx context.Context, principal value.Principal, workloadInstance string, limit int32) ([]map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != "platform.session-archive.tasks.claim" ||
		strings.TrimSpace(workloadInstance) == "" || limit < 1 || limit > 16 {
		return nil, errs.ErrForbidden
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	changedRows, err := tx.Query(ctx, querySessionArchiveMaterializeTasks, pgx.StrictNamedArgs{
		"idle_seconds": int64(sessionArchiveIdleAfter / time.Second), "retention_seconds": int64(sessionArchiveRetention / time.Second),
		"maximum_attempts": sessionArchiveMaxAttempts,
	})
	if err != nil {
		return nil, fmt.Errorf("materialize session archive tasks: %w", errs.ErrUnavailable)
	}
	var changedSessions []string
	for changedRows.Next() {
		var sessionID string
		if err := changedRows.Scan(&sessionID); err != nil {
			changedRows.Close()
			return nil, errs.ErrUnavailable
		}
		changedSessions = append(changedSessions, sessionID)
	}
	changedRows.Close()
	if changedRows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	for _, sessionID := range changedSessions {
		if err := repository.emitSessionStorageRunChanged(ctx, tx, scope, sessionID); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, querySessionArchiveSelectClaimableTasks, pgx.StrictNamedArgs{"organization_id": scope.organizationID, "limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	candidates := make([]sessionArchiveCandidate, 0, limit)
	for rows.Next() {
		var candidate sessionArchiveCandidate
		if err := rows.Scan(
			&candidate.taskID, &candidate.taskRef, &candidate.kind, &candidate.contentGeneration,
			&candidate.inputDigest, &candidate.objectKey, &candidate.objectVersion, &candidate.attempt,
			&candidate.organizationRef, &candidate.projectRef, &candidate.sessionRef, &candidate.providerAccountRef,
			&candidate.runtimeRevisionRef, &candidate.runtimeRevisionVersion, &candidate.runtimeRevisionDigest,
			&candidate.codexSessionID, &candidate.sourceRelativePath, &candidate.sourceSHA256, &candidate.sourceSizeBytes,
			&candidate.archiveRef, &candidate.archiveFormatVersion, &candidate.archiveObjectKey,
			&candidate.archiveObjectVersion, &candidate.archiveObjectETag, &candidate.archiveObjectDigest,
			&candidate.archiveObjectSizeBytes, &candidate.archiveSourceRelativePath,
			&candidate.archiveSourceSHA256, &candidate.archiveSourceSizeBytes,
		); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, errs.ErrUnavailable
	}
	rows.Close()

	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		leaseRef, err := newRef("lea")
		if err != nil {
			return nil, err
		}
		fence, err := newRef("fnc")
		if err != nil {
			return nil, err
		}
		objectKey := candidate.objectKey
		if candidate.kind == "SNAPSHOT" {
			project := candidate.projectRef
			if project == "" {
				project = "_system"
			}
			objectKey = fmt.Sprintf("session-archive/v1/%s/%s/%s/g%d/%s-a%d.tar", candidate.organizationRef,
				project, candidate.sessionRef, candidate.contentGeneration, candidate.taskRef, candidate.attempt+1)
		}
		inputDigest, err := digestSessionArchiveInput(candidate, objectKey)
		if err != nil {
			return nil, errs.ErrConflict
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		expiresAt := time.Now().UTC().Add(sessionArchiveLeaseDuration)
		var attempt int32
		var generation int64
		if err := tx.QueryRow(ctx, querySessionArchiveClaimTask, pgx.StrictNamedArgs{
			"task_id": candidate.taskID, "object_key": objectKey, "input_digest": inputDigest,
			"workload_instance": workloadInstance, "lease_ref": leaseRef,
			"fence_digest": hex.EncodeToString(fenceDigest[:]), "lease_expires_at": expiresAt,
		}).Scan(&attempt, &generation); err != nil {
			return nil, errs.ErrConflict
		}
		if candidate.kind != "DELETE_OBJECT" {
			if err := repository.emitSessionStorageRunChanged(ctx, tx, scope, candidate.sessionRef); err != nil {
				return nil, err
			}
		}
		pvcName, err := runtimecontract.SessionPVCName(candidate.sessionRef)
		if err != nil {
			return nil, errs.ErrConflict
		}
		archive := map[string]any{}
		if candidate.archiveRef != "" {
			archive = map[string]any{"archiveRef": candidate.archiveRef, "formatVersion": candidate.archiveFormatVersion,
				"objectKey": candidate.archiveObjectKey, "objectVersion": candidate.archiveObjectVersion,
				"objectETag": candidate.archiveObjectETag, "objectDigest": candidate.archiveObjectDigest,
				"objectSizeBytes": candidate.archiveObjectSizeBytes, "sourceRelativePath": candidate.archiveSourceRelativePath,
				"sourceSHA256": candidate.archiveSourceSHA256, "sourceSizeBytes": candidate.archiveSourceSizeBytes}
		}
		items = append(items, map[string]any{
			"taskRef": candidate.taskRef, "kind": candidate.kind, "organizationRef": candidate.organizationRef,
			"projectRef": candidate.projectRef, "sessionRef": candidate.sessionRef,
			"providerAccountRef": candidate.providerAccountRef, "runtimeRevisionRef": candidate.runtimeRevisionRef,
			"runtimeRevisionVersion": candidate.runtimeRevisionVersion, "runtimeRevisionDigest": candidate.runtimeRevisionDigest,
			"codexSessionID": candidate.codexSessionID, "contentGeneration": candidate.contentGeneration,
			"sourceRelativePath": candidate.sourceRelativePath, "sourceSHA256": candidate.sourceSHA256,
			"sourceSizeBytes": candidate.sourceSizeBytes, "objectKey": objectKey, "objectVersion": candidate.objectVersion,
			"pvcName": pvcName, "archive": archive, "inputDigest": inputDigest, "attempt": attempt,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return items, nil
}

// digestSessionArchiveInput hashes the ordered JSON tuple of task/kind,
// tenant/project/session/account/runtime/source pins, target object key/version and archive
// ref/digest/size. Lease/fence/generation and attempt number are not tuple fields; snapshot
// attempt identity is reflected in its object key. This digest is a commitment, not a signature or
// authority check.
// digestSessionArchiveInput хеширует ordered JSON tuple task/kind,
// tenant/project/session/account/runtime/source pins, target object key/version и archive
// ref/digest/size. Lease/fence/generation и attempt number не входят отдельными tuple fields;
// snapshot attempt identity отражена в object key. Digest — commitment, а не signature либо
// authority check.
func digestSessionArchiveInput(candidate sessionArchiveCandidate, objectKey string) (string, error) {
	raw, err := json.Marshal([]any{candidate.taskRef, candidate.kind, candidate.organizationRef,
		candidate.projectRef, candidate.sessionRef, candidate.providerAccountRef,
		candidate.runtimeRevisionRef, candidate.runtimeRevisionVersion, candidate.runtimeRevisionDigest,
		candidate.codexSessionID, candidate.contentGeneration, candidate.sourceRelativePath,
		candidate.sourceSHA256, candidate.sourceSizeBytes, objectKey, candidate.objectVersion,
		candidate.archiveRef, candidate.archiveObjectDigest, candidate.archiveObjectSizeBytes})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// RenewSessionArchiveTask requires the exact worker/renew permission and nonempty task/lease/fence
// with positive generation, resolves tenant and hashes the supplied fence before SQL extends only
// the matching live claim. No row is Forbidden and backend failure Unavailable; it returns the
// same lease/fence/generation with stored expiry and performs no external work.
// RenewSessionArchiveTask требует exact worker/renew permission и nonempty task/lease/fence с
// positive generation, разрешает tenant и хеширует supplied fence до SQL продления только matching
// live claim. No row даёт Forbidden, backend failure — Unavailable; возвращает те же
// lease/fence/generation со stored expiry без external work.
func (repository *Repository) RenewSessionArchiveTask(ctx context.Context, principal value.Principal, input command.SessionArchiveTaskInput) (map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != "platform.session-archive.tasks.renew" ||
		input.TaskRef == "" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 {
		return nil, errs.ErrForbidden
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(input.Fence))
	expiresAt := time.Now().UTC().Add(sessionArchiveLeaseDuration)
	if err := repository.pool.QueryRow(ctx, querySessionArchiveRenewTask, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "task_ref": input.TaskRef, "lease_ref": input.LeaseRef,
		"fence_digest": hex.EncodeToString(digest[:]), "generation": input.Generation,
		"lease_expires_at": expiresAt,
	}).Scan(&expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	} else if err != nil {
		return nil, errs.ErrUnavailable
	}
	return map[string]any{"leaseRef": input.LeaseRef, "fence": input.Fence, "generation": input.Generation, "expiresAt": expiresAt}, nil
}
