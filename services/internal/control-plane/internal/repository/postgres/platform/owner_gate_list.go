package platform

import (
	"context"
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

// ListOwnerGates normalizes bounded search and a cloned sorted closed state filter, validates the
// scoped timestamp/ref cursor and reads SQL-visible page refs plus total in read-only
// RepeatableRead. It closes rows before resolving each gate and projecting actor-safe intent, then
// commits and returns the next cursor. Any failure discards output; list visibility and next
// actions do not authorize resolution.
// ListOwnerGates нормализует bounded search и скопированный сортированный closed state filter,
// проверяет scoped timestamp/ref cursor и читает SQL-visible page refs с total в read-only
// RepeatableRead. Закрывает rows до разрешения каждого gate и проекции actor-safe intent, затем
// делает Commit и возвращает next cursor. Любая ошибка отбрасывает результат; list visibility и
// next actions не разрешают решение gate.
func (repository *Repository) ListOwnerGates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.OwnerGate, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.States) > 6 || filter.State != "" && len(filter.States) != 0 {
		return nil, 0, "", errs.ErrInvalid
	}
	filter.States = slices.Clone(filter.States)
	slices.Sort(filter.States)
	for index, state := range filter.States {
		if !slices.Contains([]string{"OPEN", "APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED"}, state) || index > 0 && filter.States[index-1] == state {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 200 || strings.ContainsRune(filter.Query, 0) ||
		!slices.Contains([]string{"", "OPEN", "APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED"}, filter.State) {
		return nil, 0, "", errs.ErrInvalid
	}
	cursor, err := decodeCatalogCursor(current, "OWNER_GATE", filter)
	if err != nil {
		return nil, 0, "", err
	}
	cursorRef := ""
	var cursorTime *time.Time
	if cursor != "" {
		stamp, ref, found := strings.Cut(cursor, "|")
		parsed, parseErr := time.Parse(time.RFC3339Nano, stamp)
		if !found || ref == "" || parseErr != nil {
			return nil, 0, "", errs.ErrInvalid
		}
		cursorRef, cursorTime = ref, &parsed
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	limit := boundedPage(filter.Page)
	states := filter.States
	if filter.State != "" {
		states = []string{filter.State}
	}
	if states == nil {
		states = []string{}
	}
	rows, err := tx.Query(ctx, queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState,
		current.organizationID, filter.ProjectRef, states, current.role, current.actorID, limit+1, cursorRef, cursorTime, filter.Query, current.authorityProjectID)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	var refs []string
	var total int64
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref, &total); err != nil {
			rows.Close()
			return nil, 0, "", errs.ErrUnavailable
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	hasMore := len(refs) > int(limit)
	if hasMore {
		refs = refs[:limit]
	}
	result := make([]entity.OwnerGate, 0, len(refs))
	for _, ref := range refs {
		item, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId,
			current.organizationID, ref, current.role, current.actorID), true)
		if err != nil {
			return nil, 0, "", err
		}
		if err := repository.projectGateIntent(ctx, tx, current, &item, true); err != nil {
			return nil, 0, "", err
		}
		result = append(result, item)
	}
	next := ""
	if hasMore {
		last := result[len(result)-1]
		next = encodeCatalogCursor(current, "OWNER_GATE", filter, last.CreatedAt.UTC().Format(time.RFC3339Nano)+"|"+last.Ref)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return result, total, next, nil
}
