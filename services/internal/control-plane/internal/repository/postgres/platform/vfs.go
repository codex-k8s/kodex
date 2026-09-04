package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type vfsCursor struct {
	Version           int `json:"v"`
	Filter, Path, Ref string
}

func (repository *Repository) ListVFSNodes(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	return repository.vfs(ctx, principal, "TREE", filter)
}
func (repository *Repository) SearchVFS(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	return repository.vfs(ctx, principal, "SEARCH", filter)
}

func (repository *Repository) vfs(ctx context.Context, principal value.Principal, mode string, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeVFSCursor(filter.Page.Token, mode, filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryVFSListNodes, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_ref":     strings.TrimSpace(filter.ProjectRef), "mode": mode, "path": filter.ResourceRef,
		"query": filter.Query,
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("query VFS nodes: %w: %v", errs.ErrUnavailable, err)
	}
	defer rows.Close()
	type candidate struct {
		entity.VFSNode
		accessKind, accessRef string
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.Ref, &item.Path, &item.ParentPath, &item.Name, &item.Kind, &item.Directory,
			&item.ProjectRef, &item.EntityRef, &item.RunRef, &item.SizeBytes, &item.Digest, &item.ModifiedAt,
			&item.accessKind, &item.accessRef); err != nil {
			return nil, 0, "", fmt.Errorf("scan VFS node: %w: %v", errs.ErrUnavailable, err)
		}
		candidates = append(candidates, item)
	}
	if rows.Err() != nil {
		return nil, 0, "", fmt.Errorf("iterate VFS nodes: %w: %v", errs.ErrUnavailable, rows.Err())
	}
	rows.Close()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, 0, "", err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, 0, "", err
	}
	evaluatedAt := time.Now().UTC()
	visibleItems := make([]entity.VFSNode, 0, len(candidates))
	for _, candidate := range candidates {
		visible, visibilityErr := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			candidate.accessKind, candidate.accessRef, candidate.ProjectRef, evaluatedAt)
		if visibilityErr != nil {
			return nil, 0, "", visibilityErr
		}
		if visible {
			visibleItems = append(visibleItems, candidate.VFSNode)
		}
	}
	sort.Slice(visibleItems, func(i, j int) bool {
		return visibleItems[i].Path+"\x00"+visibleItems[i].Ref < visibleItems[j].Path+"\x00"+visibleItems[j].Ref
	})
	total := int64(len(visibleItems))
	items := visibleItems
	if cursor.Path != "" {
		items = make([]entity.VFSNode, 0, len(visibleItems))
		for _, item := range visibleItems {
			if item.Path > cursor.Path || item.Path == cursor.Path && item.Ref > cursor.Ref {
				items = append(items, item)
			}
		}
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeVFSCursor(vfsCursor{Path: last.Path, Ref: last.Ref}, mode, filter)
		if next == filter.Page.Token {
			return nil, 0, "", errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrConflict
	}
	return items, total, next, nil
}

func vfsFilterDigest(mode string, filter query.Filter) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{mode, strings.TrimSpace(filter.ProjectRef), filter.ResourceRef, strings.TrimSpace(filter.Query)}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}
func encodeVFSCursor(cursor vfsCursor, mode string, filter query.Filter) string {
	cursor.Version, cursor.Filter = 1, vfsFilterDigest(mode, filter)
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeVFSCursor(token, mode string, filter query.Filter) (vfsCursor, error) {
	if strings.TrimSpace(token) == "" {
		return vfsCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 1024 {
		return vfsCursor{}, errs.ErrInvalid
	}
	var cursor vfsCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Filter != vfsFilterDigest(mode, filter) || cursor.Path == "" || cursor.Ref == "" {
		return vfsCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}
