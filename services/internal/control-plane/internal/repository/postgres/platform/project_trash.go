package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// ListTrashedProjects requires organization-level OWNER/ADMINISTRATOR authority, validates the
// trash cursor and reads bounded TRASHED/PURGE_PENDING metadata. It returns RESTORE/PURGE or
// explicit recovery next actions and a deleted-time/ref cursor without claiming purge completion.
// Query/scan errors discard output; no lifecycle state changes occur.
// ListTrashedProjects требует organization-level OWNER/ADMINISTRATOR authority, проверяет trash
// cursor и читает bounded TRASHED/PURGE_PENDING metadata. Возвращает RESTORE/PURGE либо явные
// recovery next actions и deleted-time/ref cursor без утверждения о завершении purge. Query/scan
// errors отбрасывают результат; lifecycle state не меняется.
func (repository *Repository) ListTrashedProjects(ctx context.Context, principal value.Principal, page query.Page) ([]entity.Project, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if current.authorityProjectID != "" || current.role != "OWNER" && current.role != "ADMINISTRATOR" {
		return nil, "", errs.ErrNotFound
	}
	cursorAt, cursorRef, err := decodeMVPCursor("project-trash", page.Token)
	if err != nil || cursorRef != "" && (!strings.HasPrefix(cursorRef, "prj_") || strings.ContainsAny(cursorRef, "\r\n")) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(page)
	rows, err := repository.pool.Query(ctx, queryProjectTrashList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"cursor_at":       cursorAt,
		"cursor_ref":      cursorRef,
		"page_size":       limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.Project, 0, limit+1)
	for rows.Next() {
		var item entity.Project
		var recoveryRequired bool
		if err := rows.Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language,
			&item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt,
			&item.DeletedAt, &item.PurgeAfter, &item.AgentCount, &item.WorkflowCount, &recoveryRequired); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.IntegrationState = "UNKNOWN"
		if item.Lifecycle == "TRASHED" {
			item.NextActions = []string{"RESTORE", "PURGE"}
		} else if item.Lifecycle == "PURGE_PENDING" && recoveryRequired {
			item.NextActions = []string{"RESOLVE_PURGE"}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeMVPCursor("project-trash", *last.DeletedAt, last.Ref)
	}
	return items, next, nil
}

// activeProjectRun carries one active representative run ref/version per root for caller-owned
// cancellation with OCC, preferring the root when active. It does not confer authority or include
// the entire graph snapshot.
// activeProjectRun хранит ref/version одного активного представителя каждого root для caller-owned
// отмены с OCC, предпочитая активный root. Не выдаёт полномочия и не содержит полный graph
// snapshot.
type activeProjectRun struct {
	ref     string
	version int64
}

// changeProjectLifecycle requires typed ref/OCC, locks tenant project and enforces
// authority-project before state transitions. Trash cancels due/claimed/retry schedule occurrences
// and active run representatives atomically; restore uses the SQL retention deadline and does not
// resume them. Purge persists PURGE_PENDING/receipt and cancels READY archive tasks, leaving
// physical purge to its owner worker. It returns PROJECT_CHANGED metadata without independent
// commit; caller supplies owner-role authorization.
// changeProjectLifecycle требует typed ref/OCC, блокирует tenant project и проверяет
// authority-project до переходов. Trash атомарно отменяет due/claimed/retry schedule occurrences и
// активные run representatives; restore использует SQL retention deadline и не возобновляет их.
// Purge сохраняет PURGE_PENDING/receipt и отменяет READY archive tasks, оставляя физический purge
// owner worker. Возвращает PROJECT_CHANGED metadata без отдельного Commit; caller проверяет owner
// role.
func (repository *Repository) changeProjectLifecycle(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectLifecycleInput)
	if !ok || payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID string
	var item entity.Project
	err := tx.QueryRow(ctx, queryProjectTrashLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_ref":     payload.Ref,
	}).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle,
		&item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt, &item.PurgeAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != projectID {
		return commandOutcome{}, errs.ErrNotFound
	}
	if item.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	query := queryProjectTrashMark
	arguments := pgx.StrictNamedArgs{
		"organization_id":  current.organizationID,
		"project_id":       projectID,
		"expected_version": item.Version,
	}
	summary := "i18n:PROJECT_TRASHED"
	state := "TRASHED"
	if input.Kind == command.TrashProject {
		if item.Lifecycle != "ACTIVE" {
			return commandOutcome{}, errs.ErrConflict
		}
		occurrences, err := repository.cancelProjectScheduleOccurrences(ctx, tx, current, projectID)
		if err != nil {
			return commandOutcome{}, err
		}
		for _, occurrenceID := range occurrences {
			if err := repository.emitScheduleOccurrenceChange(ctx, tx, current, occurrenceID); err != nil {
				return commandOutcome{}, err
			}
		}
		roots, err := repository.activeProjectRunRoots(ctx, tx, current, projectID)
		if err != nil {
			return commandOutcome{}, err
		}
		for _, root := range roots {
			_, err := repository.changeRun(ctx, tx, current, command.Command{
				Kind:     command.CancelRun,
				Mutation: value.Mutation{ExpectedVersion: &root.version},
				Payload:  command.RunCommandInput{RunRef: root.ref},
			})
			if err != nil {
				return commandOutcome{}, err
			}
		}
		arguments["actor_id"] = current.actorID
	} else if input.Kind == command.RestoreProject {
		if item.Lifecycle != "TRASHED" {
			return commandOutcome{}, errs.ErrConflict
		}
		query = queryProjectTrashRestore
		summary = "i18n:PROJECT_RESTORED"
		state = "ACTIVE"
	} else if input.Kind == command.PurgeProject {
		if item.Lifecycle != "TRASHED" {
			return commandOutcome{}, errs.ErrConflict
		}
		query = queryProjectTrashPurge
		summary = "i18n:PROJECT_PURGE_REQUESTED"
		state = "PURGE_PENDING"
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	err = tx.QueryRow(ctx, query, arguments).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle,
		&item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt, &item.PurgeAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if state == "PURGE_PENDING" {
		if _, err := tx.Exec(ctx, queryProjectTrashPurgeReceipt, pgx.StrictNamedArgs{
			"project_id": projectID, "organization_id": current.organizationID,
			"project_ref": item.Ref, "actor_id": current.actorID,
		}); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryProjectPurgeCancelReadyArchiveTasks, pgx.StrictNamedArgs{
			"project_id": projectID, "organization_id": current.organizationID,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if state == "TRASHED" {
		item.NextActions = []string{"RESTORE", "PURGE"}
	} else if state == "ACTIVE" {
		item.NextActions = []string{"OPEN", "EDIT"}
	}
	item.IntegrationState = "UNKNOWN"
	return commandOutcome{
		result:    command.Result{Project: &item},
		projectID: projectID, projectRef: item.Ref, resourceKind: "PROJECT", resourceRef: item.Ref,
		summary: summary, platformEvent: "PROJECT_CHANGED", platformAggregateVersion: item.Version,
		platformState: state,
	}, nil
}

// cancelProjectScheduleOccurrences cancels exact tenant/project DUE/CLAIMED/RETRY_WAIT occurrences
// in the supplied transaction, clearing lease/fence/instance and returning IDs for caller events.
// Query/scan/rows failures are Unavailable and require outer rollback; it does not change schedule
// definitions or commit.
// cancelProjectScheduleOccurrences отменяет точные tenant/project DUE/CLAIMED/RETRY_WAIT
// occurrences в переданной транзакции, очищая lease/fence/instance и возвращая IDs для событий
// caller. Query/scan/rows failures дают Unavailable и требуют outer rollback; не меняет schedule
// definitions и не делает Commit.
func (repository *Repository) cancelProjectScheduleOccurrences(ctx context.Context, tx pgx.Tx, current scope, projectID string) ([]string, error) {
	rows, err := tx.Query(ctx, queryProjectTrashCancelOccurrences, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_id":      projectID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var occurrences []string
	for rows.Next() {
		var occurrenceID string
		if err := rows.Scan(&occurrenceID); err != nil {
			return nil, errs.ErrUnavailable
		}
		occurrences = append(occurrences, occurrenceID)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return occurrences, nil
}

// activeProjectRunRoots reads one active representative per exact tenant/project root with the
// current version, preferring an active root over children. It closes rows and discards partial
// results as Unavailable on query/scan/iteration errors; cancellation, permissions and lifecycle
// locking remain with the caller.
// activeProjectRunRoots читает одного активного представителя каждого exact tenant/project root с
// текущей версией, предпочитая активный root дочерним runs. Закрывает rows и отбрасывает partial
// results как Unavailable при query/scan/iteration errors; отмена, права и lifecycle locking
// остаются у caller.
func (repository *Repository) activeProjectRunRoots(ctx context.Context, tx pgx.Tx, current scope, projectID string) ([]activeProjectRun, error) {
	rows, err := tx.Query(ctx, queryProjectTrashActiveRunRoots, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"project_id":      projectID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var roots []activeProjectRun
	for rows.Next() {
		var root activeProjectRun
		if err := rows.Scan(&root.ref, &root.version); err != nil {
			return nil, errs.ErrUnavailable
		}
		roots = append(roots, root)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return roots, nil
}
