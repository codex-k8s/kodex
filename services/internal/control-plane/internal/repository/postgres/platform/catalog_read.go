package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// readScopedContextCatalogPage resolves the existing principal, validates the caller-selected
// catalog cursor and reads exactly one typed SQL page. The row factory receives that resolved
// scope, decoded position and bounded limit; each caller retains its own SQL and eligibility.
// Scan/decode failures discard output as before, while successful totals and lookahead slices
// keep their original contract. The caller owns the deadline and cancellation; this helper
// neither starts a transaction nor chooses a resource permission or catalog kind.
// readScopedContextCatalogPage разрешает прежний principal, проверяет cursor выбранного caller
// каталога и читает ровно одну типизированную SQL-страницу. Row factory получает разрешённый
// scope, decoded position и bounded limit; каждый caller сохраняет собственный SQL и допуск.
// Scan/decode errors отбрасывают output как прежде, успешные total и lookahead slices сохраняют
// исходный контракт. Deadline и cancel принадлежат caller; helper не начинает transaction,
// не выбирает resource permission либо вид каталога.
func readScopedContextCatalogPage[T any](ctx context.Context, repository *Repository, principal value.Principal, kind string, filter query.Filter, fetch func(scope, string, int32) pgx.Row, position func(T) string) ([]T, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	items, total, err := scanJSONCatalog[T](fetch(current, cursor, limit))
	if err != nil {
		return nil, 0, "", err
	}
	items, next := catalogPageWithCursor(current, kind, filter, items, limit, position)
	return items, total, next, nil
}

// beginContextCatalogRevision resolves scope and validates the positive revision cursor before
// opening the snapshot with the wrapper's exact server-selected transaction options. Scope and
// cursor errors pass through; BeginTx errors retain Unavailable mapping. On error no partial
// scope, position or transaction is returned. Callers build only their immutable ref/page filter
// before entry and keep the deadline, typed resource read, eligibility, rollback and commit.
// No default isolation/access mode, permission, resource or cleanup policy is selected here.
// beginContextCatalogRevision разрешает scope и проверяет положительный revision cursor до
// открытия snapshot с точными выбранными сервером options wrapper. Scope/cursor errors передаются
// без изменения; BeginTx errors сохраняют Unavailable mapping. При отказе частичные scope,
// position и transaction не возвращаются. Caller заранее собирает только неизменяемый ref/page
// filter, сохраняет deadline, типизированное чтение ресурса, допуск, rollback и commit. Helper
// не выбирает default isolation/access mode, permission, ресурс либо cleanup policy.
func (repository *Repository) beginContextCatalogRevision(ctx context.Context, principal value.Principal, kind string, filter query.Filter, options pgx.TxOptions) (scope, int64, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, 0, nil, err
	}
	before, err := decodeCatalogRevisionCursor(current, kind, filter)
	if err != nil {
		return scope{}, 0, nil, err
	}

	tx, err := repository.pool.BeginTx(ctx, options)
	if err != nil {
		return scope{}, 0, nil, errs.ErrUnavailable
	}
	return current, before, tx, nil
}
