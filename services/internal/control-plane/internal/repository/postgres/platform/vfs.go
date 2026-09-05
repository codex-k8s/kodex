package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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
	filter.State = strings.TrimSpace(filter.State)
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	if filter.State != "ACTIVE" && filter.State != "DELETED" || !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, 0) ||
		filter.ResourceRef != "" && (!strings.HasPrefix(filter.ResourceRef, "/projects") || strings.Contains(filter.ResourceRef, "..") || strings.ContainsAny(filter.ResourceRef, "\\\x00\r\n") || len(filter.ResourceRef) > 1000) {
		return nil, 0, "", errs.ErrInvalid
	}
	filter.VFSKinds = slices.Clone(filter.VFSKinds)
	slices.Sort(filter.VFSKinds)
	for index, kind := range filter.VFSKinds {
		if !contains([]string{"DIRECTORY", "PROJECT", "AGENT", "WORKFLOW", "RUN", "INPUT", "RESULT", "SKILL", "MEMORY", "AUTOMATION", "ENVIRONMENT", "AVATAR"}, kind) || index > 0 && kind == filter.VFSKinds[index-1] {
			return nil, 0, "", errs.ErrInvalid
		}
	}
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
		"lifecycle_state": filter.State, "kinds": filter.VFSKinds,
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
		item := entity.VFSNode{Ref: row.Ref, Path: row.Path, ParentPath: row.ParentPath,
			Name: row.Name, Kind: row.Kind, Directory: row.Directory, ProjectRef: row.ProjectRef, EntityRef: row.EntityRef,
			RunRef: row.RunRef, SizeBytes: row.SizeBytes, Digest: row.Digest, ModifiedAt: row.ModifiedAt,
			Version: row.Version, Revision: row.Revision, RevisionRef: row.RevisionRef, LifecycleState: row.LifecycleState, ScanState: row.ScanState, ResourceKind: row.ResourceKind,
			NextActions: []string{}, SelectionReason: "DIRECTORY"}
		if err := repository.decorateVFSSelection(ctx, tx, current, row, &item); err != nil {
			return nil, 0, "", err
		}
		items = append(items, item)
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
	Version, Revision             int64
	RevisionRef                   string `json:"revision_ref"`
	LifecycleState                string `json:"lifecycle_state"`
	ScanState                     string `json:"scan_state"`
	ResourceKind                  string `json:"resource_kind"`
	CanManage                     bool   `json:"can_manage"`
}

func (repository *Repository) decorateVFSSelection(ctx context.Context, tx pgx.Tx, current scope, row vfsNodeRow, node *entity.VFSNode) error {
	if node.Directory {
		return nil
	}
	node.SelectionReason = "PERMISSION_REQUIRED"
	switch row.ResourceKind {
	case "ARTIFACT":
		artifact := entity.Artifact{Ref: node.EntityRef, Version: node.Version, LifecycleState: node.LifecycleState, ScanState: node.ScanState}
		if err := projectArtifactEligibility(ctx, tx, current, &artifact); err != nil {
			return err
		}
		node.NextActions = artifact.NextActions
		if node.RunRef != "" && node.Kind == "INPUT" || node.Kind == "AVATAR" {
			node.SelectionReason = "IMMUTABLE_CONTEXT"
			node.NextActions = []string{}
			if contains(artifact.NextActions, "DOWNLOAD") {
				node.NextActions = []string{"DOWNLOAD"}
			}
			return nil
		}
		for _, action := range []string{"DELETE", "PURGE"} {
			if !contains(node.NextActions, action) {
				continue
			}
			impact, _, err := repository.artifactImpactTx(ctx, tx, current, node.EntityRef, action)
			if err != nil {
				return err
			}
			if !impact.Permitted {
				node.NextActions = slices.DeleteFunc(node.NextActions, func(value string) bool { return value == action })
				if len(impact.Blockers) > 0 {
					node.SelectionReason = impact.Blockers[0]
				}
			}
		}
	case "SKILL_BUNDLE", "MEMORY_RECORD":
		if strings.HasPrefix(node.Ref, "context-binding:") {
			node.SelectionReason = "IMMUTABLE_CONTEXT"
			return nil
		}
		if row.CanManage {
			if node.LifecycleState == "ACTIVE" {
				node.NextActions = []string{"ARCHIVE"}
			}
			if node.LifecycleState == "ARCHIVED" {
				node.NextActions = []string{"RESTORE", "PURGE"}
			}
		}
	default:
		node.SelectionReason = "LIFECYCLE_BLOCKED"
	}
	for _, action := range node.NextActions {
		if contains([]string{"DELETE", "ARCHIVE", "RESTORE", "PURGE"}, action) {
			node.Selectable, node.SelectionReason = true, "AVAILABLE"
			break
		}
	}
	return nil
}

func vfsFilterDigest(mode string, filter query.Filter) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{mode, strings.TrimSpace(filter.ProjectRef), filter.ResourceRef, strings.TrimSpace(filter.Query), filter.State, strings.Join(filter.VFSKinds, ",")}, "\x00")))
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
