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
		if err := rows.Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language,
			&item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt,
			&item.DeletedAt, &item.PurgeAfter, &item.AgentCount, &item.WorkflowCount); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.IntegrationState = "UNKNOWN"
		if item.Lifecycle == "TRASHED" {
			item.NextActions = []string{"RESTORE", "PURGE"}
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

type activeProjectRun struct {
	ref     string
	version int64
}

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
