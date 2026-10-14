package platform

import (
	"context"
	_ "embed"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/artifact_revisions_list.sql
	queryArtifactRevisionsList string
	//go:embed sql/artifact_revisions_count.sql
	queryArtifactRevisionsCount string
	//go:embed sql/artifact_revision_get.sql
	queryArtifactRevisionGet string
)

func (repository *Repository) artifactRevisionReadHead(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.Artifact, error) {
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId,
		current.organizationID, ref, current.role, current.actorID))
	if err != nil {
		return entity.Artifact{}, err
	}
	if item.LifecycleState != "ACTIVE" {
		return entity.Artifact{}, errs.ErrNotFound
	}
	if err := projectArtifactEligibility(ctx, tx, current, &item); err != nil {
		return entity.Artifact{}, err
	}
	return item, nil
}

func artifactRevisionRow(row rowScanner) (entity.ArtifactRevision, error) {
	var item entity.ArtifactRevision
	err := row.Scan(&item.Ref, &item.ArtifactRef, &item.Revision, &item.FileName, &item.MediaType,
		&item.SizeBytes, &item.Digest, &item.ScanState, &item.Source, &item.PreviewAvailable, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ArtifactRevision{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ArtifactRevision{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) GetArtifactRevision(ctx context.Context, principal value.Principal, ref, revisionRef string) (entity.ArtifactRevision, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ArtifactRevision{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.ArtifactRevision{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.artifactRevisionReadHead(ctx, tx, current, ref); err != nil {
		return entity.ArtifactRevision{}, err
	}
	item, err := artifactRevisionRow(tx.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": ref, "revision_ref": revisionRef}))
	if err != nil {
		return entity.ArtifactRevision{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.ArtifactRevision{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) ListArtifactRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.ArtifactRevision, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	if page.Size < 0 || page.Size > 50 {
		return nil, 0, "", errs.ErrInvalid
	}
	if page.Size == 0 {
		page.Size = 10
	}
	filter := query.Filter{ResourceRef: ref, Page: page, Limit: page.Size}
	cursor, err := decodeCatalogCursor(current, "ARTIFACT_REVISION", filter)
	if err != nil {
		return nil, 0, "", err
	}
	var before, version int64
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 {
			return nil, 0, "", errs.ErrInvalid
		}
		version, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version < 1 {
			return nil, 0, "", errs.ErrInvalid
		}
		before, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || before < 1 {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	head, err := repository.artifactRevisionReadHead(ctx, tx, current, ref)
	if err != nil {
		return nil, 0, "", err
	}
	if version != 0 && version != head.Version {
		return nil, 0, "", errs.ErrVersionMismatch
	}
	var total int64
	if err := tx.QueryRow(ctx, queryArtifactRevisionsCount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": ref}).Scan(&total); err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryArtifactRevisionsList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": ref, "before_revision": before, "page_size": page.Size + 1})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	items := make([]entity.ArtifactRevision, 0, page.Size+1)
	for rows.Next() {
		var item entity.ArtifactRevision
		if rows.Scan(&item.Ref, &item.ArtifactRef, &item.Revision, &item.FileName, &item.MediaType, &item.SizeBytes,
			&item.Digest, &item.ScanState, &item.Source, &item.PreviewAvailable, &item.CreatedAt) != nil {
			rows.Close()
			return nil, 0, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(page.Size) {
		items = items[:page.Size]
		next = encodeCatalogCursor(current, "ARTIFACT_REVISION", filter, strconv.FormatInt(head.Version, 10)+":"+strconv.FormatInt(items[len(items)-1].Revision, 10))
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}
