package platform

import (
	"context"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// ListMemoryRecords reads one bounded memory page with its eligible total using
// the resolved actor/project scope and a filter-bound cursor. SQL owns eligibility;
// the lookahead row is trimmed before encoding the next record ref.
// ListMemoryRecords читает ограниченную страницу памяти и число допустимых записей
// по разрешённым actor/project и связанному с фильтром курсору. Допуск задаёт SQL;
// лишняя строка удаляется перед кодированием ref следующей страницы.
func (repository *Repository) ListMemoryRecords(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.KodexMemoryRecord, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, "MEMORY_RECORD", filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	items, total, err := scanJSONCatalog[entity.KodexMemoryRecord](repository.pool.QueryRow(ctx, queryMemoryRecordList, contextCatalogPageArgs(current, filter, cursor, limit)))
	if err != nil {
		return nil, 0, "", err
	}
	items, next := catalogPageWithCursor(current, "MEMORY_RECORD", filter, items, limit, func(item entity.KodexMemoryRecord) string { return item.Ref })
	return items, total, next, nil
}

// ListMemoryRecordRevisions resolves and authorizes the current memory record
// before reading descending revisions in one read-only repeatable-read snapshot.
// The cursor binds the actor and record; its position must be a positive revision.
// ListMemoryRecordRevisions разрешает текущую запись памяти и проверяет доступ
// до чтения убывающих версий в одном read-only repeatable-read снимке. Курсор
// связан с actor и записью; его позиция должна быть положительным номером версии.
func (repository *Repository) ListMemoryRecordRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.MemoryRecordRevision, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	filter := query.Filter{ResourceRef: ref, Page: page}
	before, err := decodeCatalogRevisionCursor(current, "MEMORY_REVISION", filter)
	if err != nil {
		return nil, 0, "", err
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
	if err != nil {
		return nil, 0, "", err
	}
	if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(page)
	items, total, err := scanJSONCatalog[entity.MemoryRecordRevision](tx.QueryRow(ctx, queryMemoryRevisionList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "record_ref": ref,
		"before_revision": before, "page_size": limit + 1, "evaluated_at": time.Now().UTC(),
	}))
	if err != nil {
		return nil, 0, "", err
	}
	items, next := catalogPageWithCursor(current, "MEMORY_REVISION", filter, items, limit, func(item entity.MemoryRecordRevision) string { return strconv.FormatInt(item.Revision, 10) })
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
