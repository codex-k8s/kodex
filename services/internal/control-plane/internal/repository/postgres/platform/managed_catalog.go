package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ListManagedConfigurations(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ManagedConfigurationSet, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, "MANAGED_CONFIGURATION", filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryManagedConfigurationList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": filter.ProjectRef, "actor_id": current.actorID,
		"authority_project": current.authorityProjectID, "evaluated_at": time.Now().UTC(),
		"kind": filter.Category, "query": filter.Query, "cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ManagedConfigurationSet, 0, limit+1)
	var total int64
	for rows.Next() {
		var item entity.ManagedConfigurationSet
		var revision entity.ManagedConfigurationRevision
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.Kind, &item.Name, &item.ManagedBy, &item.Source,
			&item.SourceRevision, &item.Version, &item.UpdatedAt, &revision.Ref, &revision.Revision, &revision.State, &revision.Digest, &total); err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		if item.Ref == "" {
			continue
		}
		if revision.Ref != "" {
			item.CurrentRevision = &revision
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "MANAGED_CONFIGURATION", filter, items[len(items)-1].Ref)
	}
	return items, total, next, nil
}
