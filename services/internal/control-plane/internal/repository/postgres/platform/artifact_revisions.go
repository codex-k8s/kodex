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
	// queryArtifactRevisionsList embeds descending revisions of an exact tenant ACTIVE artifact with
	// effective scan/preview and optional earlier-revision cursor. Actor access is established through
	// the current head before this query; it returns metadata, not object bytes or a download grant.
	// queryArtifactRevisionsList встраивает descending revisions exact tenant ACTIVE artifact с
	// effective scan/preview и optional earlier-revision cursor. Actor access устанавливается через
	// current head до query; возвращает metadata, а не object bytes либо download grant.
	//go:embed sql/artifact_revisions_list.sql
	queryArtifactRevisionsList string
	// queryArtifactRevisionsCount embeds total stored revisions for an exact tenant ACTIVE artifact
	// head. It counts independently of page position and requires caller head admission; it performs
	// no read of object content.
	// queryArtifactRevisionsCount встраивает total stored revisions exact tenant ACTIVE artifact head.
	// Считает независимо от page position и требует caller head admission; object content не читает.
	//go:embed sql/artifact_revisions_count.sql
	queryArtifactRevisionsCount string
	// queryArtifactRevisionGet embeds one exact tenant/artifact/revision metadata row only while the
	// artifact is ACTIVE, deriving scan/preview from effective revision proof. It performs no actor
	// permission check itself and cannot authorize bytes by a revision ref alone.
	// queryArtifactRevisionGet встраивает одну exact tenant/artifact/revision metadata row только для
	// ACTIVE artifact с scan/preview по effective revision proof. Сам не проверяет actor permission и
	// не разрешает bytes по одному revision ref.
	//go:embed sql/artifact_revision_get.sql
	queryArtifactRevisionGet string
)

// artifactRevisionReadHead reads the current artifact projection, hides non-ACTIVE heads as
// NotFound and applies current actions, immutable revision and visible-binding eligibility in the
// caller transaction. It returns metadata only and does not issue a content grant or commit.
// artifactRevisionReadHead читает current artifact projection, скрывает non-ACTIVE heads как
// NotFound и применяет current actions, immutable revision и visible-binding eligibility в caller
// transaction. Возвращает только metadata, не выдаёт content grant и не выполняет commit.
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

// artifactRevisionRow scans the eleven-field revision projection, returning a zero revision with
// NotFound for no row or Unavailable for other scan failure. Successful scan is a representation
// adapter, not actor admission or external content validation.
// artifactRevisionRow читает eleven-field revision projection, возвращая zero revision с NotFound
// при no row либо Unavailable при другой scan failure. Successful scan — representation adapter, а
// не actor admission либо external content validation.
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

// GetArtifactRevision resolves current principal and active head admission in a five-second
// read-only RepeatableRead transaction before reading the exact historical revision. It commits
// before returning; all error paths discard the result and deferred rollback does not create a
// download grant.
// GetArtifactRevision разрешает current principal и active head admission в пятисекундной
// read-only RepeatableRead transaction до чтения exact historical revision. Требует commit до
// возврата; error paths отбрасывают result, deferred rollback не создаёт download grant.
func (repository *Repository) GetArtifactRevision(ctx context.Context, principal value.Principal, ref, revisionRef string) (entity.ArtifactRevision, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.ArtifactRevision{}, err
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

// ListArtifactRevisions validates a0..50 page size with default10 and an actor-bound
// version/revision cursor, then rechecks active head/version in a five-second read-only
// RepeatableRead snapshot. It reads total plus one extra revision, closes/checks rows and commits;
// head drift is VersionMismatch, backend/commit failure Unavailable. No historical content is
// downloaded.
// ListArtifactRevisions проверяет page size0..50 с default10 и actor-bound version/revision
// cursor, затем заново проверяет active head/version в пятисекундном read-only RepeatableRead
// snapshot. Читает total и extra revision, закрывает/проверяет rows и требует commit; head drift
// даёт VersionMismatch, backend/commit failure — Unavailable. Historical content не загружается.
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
