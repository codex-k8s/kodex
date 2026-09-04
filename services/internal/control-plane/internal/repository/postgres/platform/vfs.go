package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

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
	rows, err := repository.pool.Query(ctx, queryVFSListNodes, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_platform_role": current.role, "actor_id": current.actorID,
		"project_ref": strings.TrimSpace(filter.ProjectRef), "mode": mode, "path": filter.ResourceRef,
		"query": filter.Query, "cursor_path": cursor.Path, "cursor_ref": cursor.Ref, "page_size": limit + 1,
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("query VFS nodes: %w: %v", errs.ErrUnavailable, err)
	}
	defer rows.Close()
	items := make([]entity.VFSNode, 0, limit+1)
	var total int64
	for rows.Next() {
		var item entity.VFSNode
		if err := rows.Scan(&item.Ref, &item.Path, &item.ParentPath, &item.Name, &item.Kind, &item.Directory,
			&item.ProjectRef, &item.EntityRef, &item.RunRef, &item.SizeBytes, &item.Digest, &item.ModifiedAt, &total); err != nil {
			return nil, 0, "", fmt.Errorf("scan VFS node: %w: %v", errs.ErrUnavailable, err)
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, 0, "", fmt.Errorf("iterate VFS nodes: %w: %v", errs.ErrUnavailable, rows.Err())
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
