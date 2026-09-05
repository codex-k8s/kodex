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

// Проверка каждой строки и чтение страницы используют один снимок полномочий.
func authorizedCatalog[T any](ctx context.Context, repository *Repository, current scope, kind string, filter query.Filter,
	fetch func(context.Context, pgx.Tx, string, int32) ([]T, error),
	target func(T) entity.AccessScope,
	decorate func(pgx.Tx, *T, func(string) bool) error,
) ([]T, string, error) {
	items, _, next, err := authorizedCatalogWithTotal(ctx, repository, current, kind, filter, fetch, target, decorate, nil)
	return items, next, err
}

//go:embed sql/catalog_snapshot_time.sql
var queryCatalogSnapshotTime string

//go:embed sql/catalog_runs_count.sql
var queryCatalogRunsCount string

//go:embed sql/catalog_artifacts_count.sql
var queryCatalogArtifactsCount string

func authorizedCatalogWithTotal[T any](ctx context.Context, repository *Repository, current scope, kind string, filter query.Filter,
	fetch func(context.Context, pgx.Tx, string, int32) ([]T, error),
	target func(T) entity.AccessScope,
	decorate func(pgx.Tx, *T, func(string) bool) error,
	count func(context.Context, pgx.Tx) (int64, error),
) ([]T, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
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
			scope := target(item)
			cursor = scope.ResourceRef
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
		next = encodeCatalogCursor(current, kind, filter, target(items[len(items)-1]).ResourceRef)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
