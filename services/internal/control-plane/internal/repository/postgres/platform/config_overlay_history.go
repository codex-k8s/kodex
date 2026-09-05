package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/config_overlay_history_list.sql
var queryConfigOverlayHistoryList string

//go:embed sql/config_overlay_history_count.sql
var queryConfigOverlayHistoryCount string

//go:embed sql/config_overlay_history_get.sql
var queryConfigOverlayHistoryGet string

func validOverlayHistoryRef(ref string) bool {
	if ref == "" || len(ref) > 96 {
		return false
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (repository *Repository) overlayHistoryScope(ctx context.Context, tx pgx.Tx, current scope, agentRef string) error {
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agentRef)
	if err != nil {
		return err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return errs.ErrForbidden
	}
	return repository.requireAccess(ctx, tx, current, permission, target)
}

func (repository *Repository) ListConfigOverlayRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ConfigOverlayVersion, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(filter.ResourceRef) || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, '\x00') || len([]rune(filter.Query)) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, filter.ResourceRef); err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, "CONFIG_OVERLAY_HISTORY", filter)
	if err != nil {
		return nil, 0, "", err
	}
	var before int64
	if cursor != "" {
		item, err := scanConfigOverlayHistory(tx.QueryRow(ctx, queryConfigOverlayHistoryGet, current.organizationID, filter.ResourceRef, cursor))
		if errors.Is(err, errs.ErrNotFound) {
			return nil, 0, "", errs.ErrInvalid
		}
		if err != nil {
			return nil, 0, "", err
		}
		before = item.Revision
	}
	var total int64
	if err := tx.QueryRow(ctx, queryConfigOverlayHistoryCount, current.organizationID, filter.ResourceRef, filter.Query).Scan(&total); err != nil || total < 0 || total > 1<<53-1 {
		return nil, 0, "", errs.ErrUnavailable
	}
	limit := boundedPage(filter.Page)
	if limit > 20 {
		limit = 20
	}
	rows, err := tx.Query(ctx, queryConfigOverlayHistoryList, current.organizationID, filter.ResourceRef, filter.Query, before, cursor, limit+1)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	items := make([]entity.ConfigOverlayVersion, 0, limit+1)
	for rows.Next() {
		item, err := scanConfigOverlayHistory(rows)
		if err != nil {
			rows.Close()
			return nil, 0, "", err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "CONFIG_OVERLAY_HISTORY", filter, items[len(items)-1].Ref)
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}

func (repository *Repository) GetConfigOverlayRevision(ctx context.Context, principal value.Principal, agentRef, revisionRef string) (entity.ConfigOverlayVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !validOverlayHistoryRef(agentRef) || !validOverlayHistoryRef(revisionRef) || !strings.HasPrefix(revisionRef, "cov_") {
		return entity.ConfigOverlayVersion{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, agentRef); err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	item, err := scanConfigOverlayHistory(tx.QueryRow(ctx, queryConfigOverlayHistoryGet, current.organizationID, agentRef, revisionRef))
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	return item, nil
}

func scanConfigOverlayHistory(row rowScanner) (entity.ConfigOverlayVersion, error) {
	var item entity.ConfigOverlayVersion
	var messages, diagnostics []byte
	err := row.Scan(&item.Ref, &item.Revision, &item.State, &item.Content, &item.Digest, &messages, &item.CreatedAt, &item.PublishedAt, &diagnostics, &item.SchemaRevision, &item.SchemaDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errs.ErrNotFound
	}
	if err != nil {
		return item, errs.ErrUnavailable
	}
	item.Version = item.Revision
	digest := sha256.Sum256([]byte(item.Content))
	if _, err := runtimecontract.ParseConfigOverlay(item.Content); err != nil || item.Digest != hex.EncodeToString(digest[:]) {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	if item.Revision <= 0 || item.Revision > 1<<53-1 || len(item.Content) > 65536 || item.PublishedAt == nil ||
		(item.State != "PUBLISHED" && item.State != "SUPERSEDED") || decodeStrict(messages, &item.ValidationMessages) != nil || decodeStrict(diagnostics, &item.Diagnostics) != nil {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	return item, nil
}
