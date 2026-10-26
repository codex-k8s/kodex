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

// ListSkillBundles reads one bounded skill bundle page and eligible total under
// the resolved actor/project scope. SQL filters eligibility before counting;
// a lookahead bundle determines the next filter-bound cursor without being returned.
// ListSkillBundles читает ограниченную страницу наборов навыков и допустимый count
// по разрешённым actor/project. SQL проверяет допуск до подсчёта; лишний набор
// определяет продолжение связанного с фильтром курсора и не включается в ответ.
func (repository *Repository) ListSkillBundles(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.SkillBundle, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, "SKILL_BUNDLE", filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	items, total, err := scanJSONCatalog[entity.SkillBundle](repository.pool.QueryRow(ctx, querySkillBundleList, contextCatalogPageArgs(current, filter, cursor, limit)))
	if err != nil {
		return nil, 0, "", err
	}
	items, next := catalogPageWithCursor(current, "SKILL_BUNDLE", filter, items, limit, func(item entity.SkillBundle) string { return item.Ref })
	return items, total, next, nil
}

// ListSkillBundleRevisions authorizes the current bundle before reading revision
// history in the same read-only repeatable-read snapshot. A positive revision
// position and actor/bundle-bound cursor preserve descending history pagination.
// ListSkillBundleRevisions проверяет доступ к текущему набору до чтения истории
// версий в том же read-only repeatable-read снимке. Положительная позиция версии
// и курсор, связанный с actor/набором, сохраняют убывающую пагинацию истории.
func (repository *Repository) ListSkillBundleRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.SkillBundleRevision, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	filter := query.Filter{ResourceRef: ref, Page: page}
	before, err := decodeCatalogRevisionCursor(current, "SKILL_REVISION", filter)
	if err != nil {
		return nil, 0, "", err
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return nil, 0, "", err
	}
	if err := repository.skillBundleAccess(ctx, tx, current, record); err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(page)
	items, total, err := scanJSONCatalog[entity.SkillBundleRevision](tx.QueryRow(ctx, querySkillRevisionList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "bundle_ref": ref,
		"before_revision": before, "page_size": limit + 1, "evaluated_at": time.Now().UTC(),
	}))
	if err != nil {
		return nil, 0, "", err
	}
	items, next := catalogPageWithCursor(current, "SKILL_REVISION", filter, items, limit, func(item entity.SkillBundleRevision) string { return strconv.FormatInt(item.Revision, 10) })
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
