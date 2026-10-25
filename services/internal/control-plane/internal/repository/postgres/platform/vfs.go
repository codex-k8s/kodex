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

// vfsCursor holds version, filter/scope digest and last virtual path/ref for unsigned VFS
// pagination. It is a position token without source-revision pin, signature or independent
// authority.
// vfsCursor хранит version, filter/scope digest и последние virtual path/ref для неподписанной VFS
// pagination. Это position token без source-revision pin, signature либо самостоятельных
// полномочий.
type vfsCursor struct {
	Version           int `json:"v"`
	Filter, Path, Ref string
}

// ListVFSNodes delegates tree-mode listing with the original principal/filter to the common VFS
// reader. Tree selection remains SQL-visible metadata and does not grant content access or
// filesystem mutation.
// ListVFSNodes делегирует tree-mode listing с исходными principal/filter общему VFS reader. Tree
// selection остаётся SQL-visible metadata и не выдаёт content access либо filesystem mutation.
func (repository *Repository) ListVFSNodes(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	return repository.vfs(ctx, principal, "TREE", filter)
}

// SearchVFS delegates search-mode listing to the same scoped VFS reader as tree navigation. Search
// terms select metadata; canonical SQL eligibility and action decoration still apply before
// return.
// SearchVFS делегирует search-mode listing тому же scoped VFS reader, что tree navigation. Search
// terms выбирают metadata; canonical SQL eligibility и action decoration сохраняются до возврата.
func (repository *Repository) SearchVFS(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	return repository.vfs(ctx, principal, "SEARCH", filter)
}

// vfs normalizes lifecycle, validates bounded path/query/unique sorted kinds and resolves
// actor-bound cursor scope, then reads visible SQL nodes in a five-second read-only RepeatableRead
// transaction. It decorates actions, trims one extra row, rejects a repeating next cursor and
// commits before return. Query failure retains the existing wrapped backend diagnostic; neither
// cursor nor selection hints replace fresh command permission.
// vfs нормализует lifecycle, проверяет bounded path/query/unique sorted kinds и разрешает
// actor-bound cursor scope, затем читает visible SQL nodes в пятисекундной read-only
// RepeatableRead transaction. Добавляет actions, отбрасывает extra row, отклоняет repeating next
// cursor и требует commit до возврата. Query failure сохраняет прежнюю wrapped backend diagnostic;
// cursor и selection hints не заменяют fresh command permission.
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

// vfsNodeRow represents the stored JSON VFS projection with virtual identity/path, immutable
// revision/content pins, lifecycle/scan, resource kind and management hint. SQL supplies
// eligibility; these fields do not themselves authorize actions.
// vfsNodeRow представляет stored JSON VFS projection с virtual identity/path, immutable
// revision/content pins, lifecycle/scan, resource kind и management hint. Eligibility задаёт SQL;
// сами поля не разрешают actions.
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

// decorateVFSSelection skips ordinary directories, reprojects current artifact actions and narrows
// immutable input/avatar/knowledge/skill-file nodes to download only. Delete/purge hints are
// further limited by impact; managed skill/memory lifecycle hints and selection reasons use stored
// scope metadata. It mutates the node, can return after partial decoration, and performs no actual
// lifecycle command.
// decorateVFSSelection пропускает обычные directories, заново проецирует current artifact actions
// и ограничивает immutable input/avatar/knowledge/skill-file nodes только download. Delete/purge
// hints дополнительно ограничены impact; managed skill/memory lifecycle hints и selection reasons
// используют stored scope metadata. Меняет node, может вернуть ошибку после partial decoration, не
// выполняет lifecycle command.
func (repository *Repository) decorateVFSSelection(ctx context.Context, tx pgx.Tx, current scope, row vfsNodeRow, node *entity.VFSNode) error {
	if node.Directory && row.ResourceKind != "SKILL_BUNDLE" {
		return nil
	}
	node.SelectionReason = "PERMISSION_REQUIRED"
	switch row.ResourceKind {
	case "ARTIFACT":
		artifact := entity.Artifact{Ref: node.EntityRef, Version: node.Version, LifecycleState: node.LifecycleState, ScanState: node.ScanState}
		if err := projectArtifactActions(ctx, tx, current, &artifact); err != nil {
			return err
		}
		if node.ScanState != "CLEAN" {
			artifact.NextActions = slices.DeleteFunc(artifact.NextActions, func(action string) bool { return action == "DOWNLOAD" || action == "BIND" })
		}
		node.NextActions = artifact.NextActions
		if node.RunRef != "" && node.Kind == "INPUT" || node.Kind == "AVATAR" || strings.HasPrefix(node.Ref, "knowledge-input:") || strings.HasPrefix(node.Ref, "skill-file:") {
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

// vfsFilterDigest hashes scope/mode plus normalized project/query, path, state and ordered kinds
// into a twelve-byte SHA-256 prefix encoded as raw URL base64. It is a filter binding, not a MAC,
// source snapshot commitment or permission.
// vfsFilterDigest хеширует scope/mode с normalized project/query, path, state и ordered kinds в
// двенадцатибайтовый SHA-256 prefix, закодированный raw URL base64. Это filter binding, а не MAC,
// source snapshot commitment либо permission.
func vfsFilterDigest(mode string, filter query.Filter) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{mode, strings.TrimSpace(filter.ProjectRef), filter.ResourceRef, strings.TrimSpace(filter.Query), filter.State, strings.Join(filter.VFSKinds, ",")}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

// encodeVFSCursor sets version one and the current filter/scope digest on a cursor copy before
// ordinary JSON/raw URL base64 encoding. It preserves the ignored marshal error and adds no
// signature, expiry or source version.
// encodeVFSCursor назначает version один и current filter/scope digest cursor copy до ordinary
// JSON/raw URL base64 encoding. Сохраняет ignored marshal error, не добавляет signature, expiry
// либо source version.
func encodeVFSCursor(cursor vfsCursor, mode string, filter query.Filter) string {
	cursor.Version, cursor.Filter = 1, vfsFilterDigest(mode, filter)
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeVFSCursor accepts whitespace-only input as first page; otherwise bounds token/raw bytes
// and requires decodable JSON, version one, exact filter/scope digest and nonempty path/ref.
// Unknown fields are not rejected by ordinary Unmarshal; the returned locator is not authority or
// proof that the source stayed unchanged.
// decodeVFSCursor принимает whitespace-only input как first page; иначе ограничивает token/raw
// bytes и требует decodable JSON, version один, exact filter/scope digest и nonempty path/ref.
// Unknown fields не отклоняются ordinary Unmarshal; returned locator не является authority либо
// доказательством неизменности source.
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
