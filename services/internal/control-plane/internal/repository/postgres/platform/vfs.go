package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	cursorScope := strings.Join([]string{mode, current.organizationID, current.actorID, current.authorityProjectID}, ":")
	cursor, err := decodeVFSCursor(filter.Page.Token, cursorScope, filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	var total int64
	err = tx.QueryRow(ctx, queryVFSListNodes, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_ref":     strings.TrimSpace(filter.ProjectRef), "mode": mode, "path": filter.ResourceRef,
		"query": filter.Query, "actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"evaluated_at": time.Now().UTC(), "cursor_path": cursor.Path, "cursor_ref": cursor.Ref, "page_size": limit + 1,
	}).Scan(&raw, &total)
	if err != nil {
		return nil, 0, "", fmt.Errorf("query VFS nodes: %w: %v", errs.ErrUnavailable, err)
	}
	var stored []vfsNodeRow
	if json.Unmarshal(raw, &stored) != nil || len(stored) > int(limit+1) {
		return nil, 0, "", errs.ErrUnavailable
	}
	items := make([]entity.VFSNode, 0, len(stored))
	for _, row := range stored {
		items = append(items, entity.VFSNode{Ref: row.Ref, Path: row.Path, ParentPath: row.ParentPath,
			Name: row.Name, Kind: row.Kind, Directory: row.Directory, ProjectRef: row.ProjectRef, EntityRef: row.EntityRef,
			RunRef: row.RunRef, SizeBytes: row.SizeBytes, Digest: row.Digest, ModifiedAt: row.ModifiedAt})
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeVFSCursor(vfsCursor{Path: last.Path, Ref: last.Ref}, cursorScope, filter)
		if next == filter.Page.Token {
			return nil, 0, "", errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrConflict
	}
	return items, total, next, nil
}

type vfsNodeRow struct {
	Ref, Path, Name, Kind, Digest string
	Directory                     bool
	ParentPath                    string    `json:"parent_path"`
	ProjectRef                    string    `json:"project_ref"`
	EntityRef                     string    `json:"entity_ref"`
	RunRef                        string    `json:"run_ref"`
	SizeBytes                     int64     `json:"size_bytes"`
	ModifiedAt                    time.Time `json:"modified_at"`
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
	if len(token) > 2048 {
		return vfsCursor{}, errs.ErrInvalid
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
