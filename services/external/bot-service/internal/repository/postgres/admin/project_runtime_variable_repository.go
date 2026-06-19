package admin

import (
	"context"
	"errors"
	"fmt"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repo *Repository) UpsertProjectRuntimeVariable(ctx context.Context, input adminrepo.UpsertProjectRuntimeVariableInput) (entity.ProjectRuntimeVariable, bool, error) {
	item, created, err := scanProjectRuntimeVariableWithCreated(repo.pool.QueryRow(ctx, query("project_runtime_variables__upsert.sql"),
		input.ProjectID,
		input.Name,
		input.Slug,
		input.Description,
		input.SecretRef,
		input.SecretKey,
		input.Sensitive,
		input.Enabled,
	))
	if err != nil {
		return entity.ProjectRuntimeVariable{}, false, fmt.Errorf("upsert project runtime variable: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetProjectRuntimeVariable(ctx context.Context, id int64) (entity.ProjectRuntimeVariable, error) {
	item, err := scanProjectRuntimeVariable(repo.pool.QueryRow(ctx, query("project_runtime_variables__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
		}
		return entity.ProjectRuntimeVariable{}, fmt.Errorf("get project runtime variable: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListProjectRuntimeVariables(ctx context.Context, projectID int64) ([]entity.ProjectRuntimeVariable, error) {
	rows, err := repo.pool.Query(ctx, query("project_runtime_variables__list.sql"), projectID)
	if err != nil {
		return nil, fmt.Errorf("list project runtime variables: %w", err)
	}
	defer rows.Close()

	var items []entity.ProjectRuntimeVariable
	for rows.Next() {
		item, err := scanProjectRuntimeVariable(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project runtime variable: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project runtime variables: %w", err)
	}
	return items, nil
}

func (repo *Repository) DeleteProjectRuntimeVariable(ctx context.Context, id int64) (entity.ProjectRuntimeVariable, error) {
	item, err := scanProjectRuntimeVariable(repo.pool.QueryRow(ctx, query("project_runtime_variables__delete.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
		}
		return entity.ProjectRuntimeVariable{}, fmt.Errorf("delete project runtime variable: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertAgentRoleRuntimeVariable(ctx context.Context, input adminrepo.UpsertAgentRoleRuntimeVariableInput) (entity.AgentRoleRuntimeVariableBinding, bool, error) {
	item, created, err := scanAgentRoleRuntimeVariableBindingWithCreated(repo.pool.QueryRow(ctx, query("agent_role_runtime_variables__upsert.sql"), input.RoleID, input.VariableID))
	if err != nil {
		return entity.AgentRoleRuntimeVariableBinding{}, false, fmt.Errorf("upsert agent role runtime variable: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) DeleteAgentRoleRuntimeVariable(ctx context.Context, roleID int64, variableID int64) (entity.AgentRoleRuntimeVariableBinding, error) {
	item, err := scanAgentRoleRuntimeVariableBinding(repo.pool.QueryRow(ctx, query("agent_role_runtime_variables__delete.sql"), roleID, variableID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentRoleRuntimeVariableBinding{}, adminrepo.ErrNotFound
		}
		return entity.AgentRoleRuntimeVariableBinding{}, fmt.Errorf("delete agent role runtime variable: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentRoleRuntimeVariables(ctx context.Context, roleID int64) ([]entity.AgentRoleRuntimeVariableBinding, error) {
	rows, err := repo.pool.Query(ctx, query("agent_role_runtime_variables__list.sql"), roleID)
	if err != nil {
		return nil, fmt.Errorf("list agent role runtime variables: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentRoleRuntimeVariableBinding
	for rows.Next() {
		item, err := scanAgentRoleRuntimeVariableBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent role runtime variable: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent role runtime variables: %w", err)
	}
	return items, nil
}

func scanProjectRuntimeVariable(row accountRow) (entity.ProjectRuntimeVariable, error) {
	var item entity.ProjectRuntimeVariable
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.Slug,
		&item.Description,
		&item.SecretRef,
		&item.SecretKey,
		&item.Sensitive,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.ProjectRuntimeVariable{}, err
	}
	return item, nil
}

func scanProjectRuntimeVariableWithCreated(row pgx.Row) (entity.ProjectRuntimeVariable, bool, error) {
	var item entity.ProjectRuntimeVariable
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.Slug,
		&item.Description,
		&item.SecretRef,
		&item.SecretKey,
		&item.Sensitive,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.ProjectRuntimeVariable{}, false, err
	}
	return item, created, nil
}

func scanAgentRoleRuntimeVariableBinding(row accountRow) (entity.AgentRoleRuntimeVariableBinding, error) {
	var item entity.AgentRoleRuntimeVariableBinding
	if err := row.Scan(
		&item.ID,
		&item.RoleID,
		&item.RoleName,
		&item.VariableID,
		&item.ProjectID,
		&item.Name,
		&item.Slug,
		&item.Description,
		&item.SecretRef,
		&item.SecretKey,
		&item.Sensitive,
		&item.Enabled,
		&item.CreatedAt,
	); err != nil {
		return entity.AgentRoleRuntimeVariableBinding{}, err
	}
	return item, nil
}

func scanAgentRoleRuntimeVariableBindingWithCreated(row pgx.Row) (entity.AgentRoleRuntimeVariableBinding, bool, error) {
	var item entity.AgentRoleRuntimeVariableBinding
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.RoleID,
		&item.RoleName,
		&item.VariableID,
		&item.ProjectID,
		&item.Name,
		&item.Slug,
		&item.Description,
		&item.SecretRef,
		&item.SecretKey,
		&item.Sensitive,
		&item.Enabled,
		&item.CreatedAt,
		&created,
	); err != nil {
		return entity.AgentRoleRuntimeVariableBinding{}, false, err
	}
	return item, created, nil
}
