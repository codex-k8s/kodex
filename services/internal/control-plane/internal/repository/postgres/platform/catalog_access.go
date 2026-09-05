package platform

import (
	"context"
	"errors"
	"time"

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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if len([]rune(filter.Query)) > 200 {
		return nil, "", errs.ErrInvalid
	}
	cursor, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, "", err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, "", err
	}
	limit := boundedPage(filter.Page)
	items := make([]T, 0, limit+1)
	at := time.Now().UTC()
	for len(items) <= int(limit) {
		batch, err := fetch(ctx, tx, cursor, limit+1)
		if err != nil {
			return nil, "", err
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
				return nil, "", err
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
				return nil, "", err
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
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}
