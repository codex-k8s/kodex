package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// catalogQueryTimeout bounds one authorized catalog read to five seconds, including scope
// metadata, pagination, decoration and commit; it does not impose a global request or worker
// deadline.
// catalogQueryTimeout ограничивает одно чтение авторизованного каталога пятью секундами, включая
// данные доступа, пагинацию, дополнение карточек и Commit; не задаёт общий deadline запроса либо
// worker.
const catalogQueryTimeout = 5 * time.Second

// authorizedCatalog delegates paging to the same-snapshot authorization engine without a
// total-count callback and discards its zero total. It preserves returned items, cursor and error;
// fetch, target and decoration remain caller-specific.
// authorizedCatalog передаёт пагинацию механизму авторизации на одном снимке без callback общего
// числа и отбрасывает нулевой total. Сохраняет items, cursor и ошибку; fetch, target и дополнение
// определяет caller.
func authorizedCatalog[T any](ctx context.Context, repository *Repository, current scope, kind string, filter query.Filter,
	fetch func(context.Context, pgx.Tx, string, int32) ([]T, error),
	target func(T) entity.AccessScope,
	decorate func(pgx.Tx, *T, func(string) bool) error,
) ([]T, string, error) {
	items, _, next, err := authorizedCatalogWithTotal(ctx, repository, current, kind, filter, fetch, target, decorate, nil)
	return items, next, err
}

// queryCatalogSnapshotTime embeds the transaction timestamp used to align count and access
// evaluation when a catalog total is requested. The query does not resolve an actor or permission.
// queryCatalogSnapshotTime встраивает время транзакции для согласования count и проверки доступа
// при запросе общего числа каталога. Запрос не разрешает субъекта и не выдаёт право.
//
//go:embed sql/catalog_snapshot_time.sql
var queryCatalogSnapshotTime string

// queryCatalogRunsCount embeds tenant/project, search, state and authority-project filtering plus
// current SQL run visibility for the total. It counts matching runs rather than the filtered page
// already returned to Go.
// queryCatalogRunsCount встраивает фильтры организации, проекта, поиска, состояния,
// authority-project и текущей SQL-видимости запусков для total. Считает подходящие запуски, а не
// уже возвращённую в Go страницу.
//
//go:embed sql/catalog_runs_count.sql
var queryCatalogRunsCount string

// queryCatalogArtifactsCount embeds artifact total filtering by tenant, owner project, run,
// lifecycle, scan/source/media and search criteria with authoritative artifact visibility. It
// neither reads bytes nor changes retention.
// queryCatalogArtifactsCount встраивает total артефактов по организации, owner project, запуску,
// lifecycle, scan/source/media и поиску с авторитетной видимостью. Не читает байты и не меняет
// retention.
//
//go:embed sql/catalog_artifacts_count.sql
var queryCatalogArtifactsCount string

// authorizedCatalogWithTotal validates search/cursor and uses a read-only RepeatableRead
// transaction with one subject/binding snapshot. It scans bounded batches until a visible page
// plus one is found, skips missing or disallowed targets, decorates allowed items and commits.
// Runtime environments separate project visibility from action authority; optional counts use
// transaction time and JSON-safe totals. Failure discards the accumulated result.
// authorizedCatalogWithTotal проверяет поиск и cursor и использует read-only RepeatableRead с
// единым снимком субъекта и bindings. Читает ограниченные порции до видимой страницы с одним
// запасным элементом, пропускает отсутствующие либо недоступные targets, дополняет допустимые
// карточки и завершает транзакцию. Runtime environments разделяют видимость проекта и права
// действий; optional count использует время транзакции и JSON-safe total. Ошибка отбрасывает
// накопленный результат.
func authorizedCatalogWithTotal[T any](ctx context.Context, repository *Repository, current scope, kind string, filter query.Filter,
	fetch func(context.Context, pgx.Tx, string, int32) ([]T, error),
	target func(T) entity.AccessScope,
	decorate func(pgx.Tx, *T, func(string) bool) error,
	count func(context.Context, pgx.Tx) (int64, error),
	position ...func(T) string,
) ([]T, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	if len([]rune(filter.Query)) > 200 || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, '\x00') {
		return nil, 0, "", errs.ErrInvalid
	}
	cursor, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, 0, "", err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	items := make([]T, 0, limit+1)
	at := time.Now().UTC()
	var total int64
	if count != nil {
		if err := tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&at); err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		total, err = count(ctx, tx)
		if err != nil {
			return nil, 0, "", err
		}
		if total < 0 || total > 1<<53-1 {
			return nil, 0, "", errs.ErrUnavailable
		}
	}
	for len(items) <= int(limit) {
		batch, err := fetch(ctx, tx, cursor, limit+1)
		if err != nil {
			return nil, 0, "", err
		}
		for _, item := range batch {
			actionScope := target(item)
			cursor = actionScope.ResourceRef
			if len(position) != 0 {
				cursor = position[0](item)
			}
			scope := actionScope
			if kind == "RUNTIME_ENVIRONMENT" || kind == "MEMBERSHIP" {
				scope = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: scope.ProjectRef, ProjectRef: scope.ProjectRef}
			}
			resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, scope)
			if errors.Is(err, errs.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, 0, "", err
			}
			if current.authorityProjectID != "" && current.authorityProjectID != resolved.projectID {
				continue
			}
			allowed := func(permission string) bool {
				return accessservice.Evaluate(subject.AccessSubject, permission, resolved.scope, resolved.ownerSubjectRef, bindings, at).Allowed
			}
			if !allowed(visibilityPermission(kind)) {
				continue
			}
			if kind == "RUNTIME_ENVIRONMENT" {
				actionTarget, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, actionScope)
				if errors.Is(err, errs.ErrNotFound) {
					continue
				}
				if err != nil {
					return nil, 0, "", err
				}
				allowed = func(permission string) bool {
					target := resolved
					if strings.HasPrefix(permission, "runtime.environment.") {
						target = actionTarget
					}
					return accessservice.Evaluate(subject.AccessSubject, permission, target.scope, target.ownerSubjectRef, bindings, at).Allowed
				}
			}
			if err := decorate(tx, &item, allowed); err != nil {
				return nil, 0, "", err
			}
			items = append(items, item)
			if len(items) > int(limit) {
				break
			}
		}
		if len(batch) < int(limit+1) {
			break
		}
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := target(items[len(items)-1]).ResourceRef
		if len(position) != 0 {
			last = position[0](items[len(items)-1])
		}
		next = encodeCatalogCursor(current, kind, filter, last)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
