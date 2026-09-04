package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
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
	items := make([]entity.ManagedConfigurationSet, 0, limit+1)
	var total int64
	after := ""
	at := time.Now().UTC()
	// Total считается тем же evaluator и в том же снимке, что и выдача.
	for {
		rows, err := tx.Query(ctx, queryManagedConfigurationList, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_ref": filter.ProjectRef,
			"kind": filter.Category, "query": filter.Query, "cursor_ref": after, "page_size": limit + 1,
		})
		if err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		count := 0
		for rows.Next() {
			var item entity.ManagedConfigurationSet
			var revision entity.ManagedConfigurationRevision
			if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.Kind, &item.Name, &item.ManagedBy, &item.Source,
				&item.SourceRevision, &item.Version, &item.UpdatedAt, &revision.Ref, &revision.Revision, &revision.State, &revision.Digest); err != nil {
				rows.Close()
				return nil, 0, "", errs.ErrUnavailable
			}
			count++
			after = item.Ref
			target, permission := organizationTarget(current.organizationRef), "organization.view"
			if item.ProjectRef != "" {
				target = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: item.ProjectRef, ProjectRef: item.ProjectRef}
				permission = "project.view"
			}
			if !accessservice.Evaluate(subject.AccessSubject, permission, target, "", bindings, at).Allowed {
				continue
			}
			total++
			if item.Ref <= cursor || len(items) > int(limit) {
				continue
			}
			if revision.Ref != "" {
				item.CurrentRevision = &revision
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		if count < int(limit+1) {
			break
		}
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "MANAGED_CONFIGURATION", filter, items[len(items)-1].Ref)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
