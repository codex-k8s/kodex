package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleCallbackWait holds only this operation's child result until every publication and typed continuation has settled.
// queryRepositoryBundleCallbackWait удерживает child result лишь этой operation до завершения всех publications и typed continuations.
//
//go:embed sql/repository_bundle__callback_wait.sql
var queryRepositoryBundleCallbackWait string

// queryRepositoryBundleCallbackOrigin resolves the original immutable delegation/callback edge through an exclusively owner-witnessed same-child continuation chain.
// queryRepositoryBundleCallbackOrigin разрешает исходное immutable delegation/callback edge через только owner-witnessed same-child continuation chain.
//
//go:embed sql/repository_bundle__callback_origin.sql
var queryRepositoryBundleCallbackOrigin string

// queryRepositoryBundleCallbackReceipt reuses native immutable receipt shape and exact final artifact pins with the new kind's closed chain proof; generic receipt admission stays unchanged.
// queryRepositoryBundleCallbackReceipt переиспользует native immutable receipt shape и exact final artifact pins с closed chain proof нового kind; generic receipt admission не меняется.
//
//go:embed sql/repository_bundle__callback_receipt.sql
var queryRepositoryBundleCallbackReceipt string

// queryRepositoryBundleCallbackJoin selects this kind's existing callback join when one origin owns both publication and delegated work; unknown SQL outcomes never fall back to another scheduler.
// queryRepositoryBundleCallbackJoin выбирает существующий callback join этого kind, когда origin владеет публикацией и делегированием; неизвестный SQL outcome не переключает scheduler.
//
//go:embed sql/repository_bundle__callback_join.sql
var queryRepositoryBundleCallbackJoin string

// repositoryBundleCallback resolves at most one original parent edge after the exact final execution has completed. Missing generic edges do not grant a caller-selected parent, and ineligible known chains fail closed before any parent event.
// repositoryBundleCallback разрешает максимум одно original parent edge после exact final execution. Отсутствие generic edge не разрешает caller-selected parent; ineligible known chain отклоняется до parent event.
func repositoryBundleCallback(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) (callbackRecord, bool, error) {
	rows, err := tx.Query(ctx, queryRepositoryBundleCallbackOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "child_run_id": lease["runID"], "runtime_revision_id": lease["runtimeRevisionID"]})
	if err != nil {
		return callbackRecord{}, false, errs.ErrUnavailable
	}
	defer rows.Close()
	var record callbackRecord
	count := 0
	for rows.Next() {
		var eligible bool
		if rows.Scan(&record.callbackEdgeID, &record.callbackEdgeRef, &record.parentNodeID, &record.parentNodeRef, &record.parentRunID, &eligible) != nil {
			return callbackRecord{}, false, errs.ErrUnavailable
		}
		count++
		if !eligible {
			return callbackRecord{}, false, errs.ErrForbidden
		}
	}
	if rows.Err() != nil || count > 1 {
		return callbackRecord{}, false, errs.ErrUnavailable
	}
	if count == 0 {
		return callbackRecord{}, false, nil
	}
	record.repositoryBundle = true
	return record, true, nil
}
