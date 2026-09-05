package platform

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

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
	var raw []byte
	var total int64
	err = repository.pool.QueryRow(ctx, querySkillBundleList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project": current.authorityProjectID, "project_ref": filter.ProjectRef,
		"agent_ref": filter.ResourceRef, "state": filter.State, "query": filter.Query,
		"cursor_ref": cursor, "page_size": limit + 1, "evaluated_at": time.Now().UTC(),
	}).Scan(&raw, &total)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	var items []entity.SkillBundle
	if json.Unmarshal(raw, &items) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "SKILL_BUNDLE", filter, items[len(items)-1].Ref)
	}
	return items, total, next, nil
}

func (repository *Repository) ListSkillBundleRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.SkillBundleRevision, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	filter := query.Filter{ResourceRef: ref, Page: page}
	cursor, err := decodeCatalogCursor(current, "SKILL_REVISION", filter)
	if err != nil {
		return nil, 0, "", err
	}
	var before int64
	if cursor != "" {
		before, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || before < 1 {
			return nil, 0, "", errs.ErrInvalid
		}
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
	var raw []byte
	var total int64
	err = tx.QueryRow(ctx, querySkillRevisionList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "bundle_ref": ref,
		"before_revision": before, "page_size": limit + 1, "evaluated_at": time.Now().UTC(),
	}).Scan(&raw, &total)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	var items []entity.SkillBundleRevision
	if json.Unmarshal(raw, &items) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "SKILL_REVISION", filter, strconv.FormatInt(items[len(items)-1].Revision, 10))
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
