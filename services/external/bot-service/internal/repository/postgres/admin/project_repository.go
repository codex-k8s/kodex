package admin

import (
	"context"
	"errors"
	"fmt"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repo *Repository) UpsertProject(ctx context.Context, input adminrepo.UpsertProjectInput) (entity.Project, bool, error) {
	item, created, err := scanProjectWithCreated(repo.pool.QueryRow(ctx, query("projects__upsert.sql"),
		input.Name,
		input.Slug,
		input.MattermostTeamID,
		input.Description,
		input.AdvancedSettings,
	))
	if err != nil {
		return entity.Project{}, false, fmt.Errorf("upsert project: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetProject(ctx context.Context, id int64) (entity.Project, error) {
	item, err := scanProject(repo.pool.QueryRow(ctx, query("projects__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Project{}, adminrepo.ErrNotFound
		}
		return entity.Project{}, fmt.Errorf("get project: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetProjectBySlug(ctx context.Context, slug string) (entity.Project, error) {
	item, err := scanProject(repo.pool.QueryRow(ctx, query("projects__get_by_slug.sql"), slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Project{}, adminrepo.ErrNotFound
		}
		return entity.Project{}, fmt.Errorf("get project by slug: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListProjects(ctx context.Context, limit int) ([]entity.Project, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("projects__list.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var items []entity.Project
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return items, nil
}

func (repo *Repository) UpsertProjectRepository(ctx context.Context, input adminrepo.UpsertProjectRepositoryInput) (entity.ProjectRepository, bool, error) {
	item, created, err := scanProjectRepositoryWithCreated(repo.pool.QueryRow(ctx, query("project_repositories__upsert.sql"),
		input.ProjectID,
		input.RepositoryID,
		input.IsDefault,
		input.Metadata,
	))
	if err != nil {
		return entity.ProjectRepository{}, false, fmt.Errorf("upsert project repository: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) ListProjectRepositories(ctx context.Context, projectID int64) ([]entity.ProjectRepository, error) {
	rows, err := repo.pool.Query(ctx, query("project_repositories__list.sql"), projectID)
	if err != nil {
		return nil, fmt.Errorf("list project repositories: %w", err)
	}
	defer rows.Close()

	var items []entity.ProjectRepository
	for rows.Next() {
		item, err := scanProjectRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project repository: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project repositories: %w", err)
	}
	return items, nil
}

func (repo *Repository) UpsertAgentRole(ctx context.Context, input adminrepo.UpsertAgentRoleInput) (entity.AgentRole, bool, error) {
	item, created, err := scanAgentRoleWithCreated(repo.pool.QueryRow(ctx, query("agent_roles__upsert.sql"),
		input.ProjectID,
		input.Name,
		input.RoleType,
		input.Description,
		input.PromptTemplate,
		input.PromptMode,
		input.GitHubAccountName,
		input.OpenAIAccountName,
		input.KubernetesAccess,
		input.SandboxMode,
		input.ConfigOverlay,
		input.AdvancedSettings,
		input.Enabled,
		input.BotIdentity,
	))
	if err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("upsert agent role: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetAgentRole(ctx context.Context, id int64) (entity.AgentRole, error) {
	item, err := scanAgentRole(repo.pool.QueryRow(ctx, query("agent_roles__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentRole{}, adminrepo.ErrNotFound
		}
		return entity.AgentRole{}, fmt.Errorf("get agent role: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentRoles(ctx context.Context, projectID int64) ([]entity.AgentRole, error) {
	rows, err := repo.pool.Query(ctx, query("agent_roles__list.sql"), projectID)
	if err != nil {
		return nil, fmt.Errorf("list agent roles: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentRole
	for rows.Next() {
		item, err := scanAgentRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent role: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent roles: %w", err)
	}
	return items, nil
}

func (repo *Repository) CreateChat(ctx context.Context, input adminrepo.CreateChatInput) (entity.Chat, bool, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return entity.Chat{}, false, fmt.Errorf("begin create chat: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, created, err := scanChatWithCreated(tx.QueryRow(ctx, query("chats__upsert.sql"),
		input.ProjectID,
		input.MattermostChannelID,
		input.Name,
		input.Slug,
		input.Description,
		input.ChatType,
		input.RootGitHubIssue,
		input.WorkPolicy,
		input.Settings,
	))
	if err != nil {
		return entity.Chat{}, false, fmt.Errorf("upsert chat: %w", err)
	}
	if _, err := tx.Exec(ctx, query("chat_participants__delete_by_chat.sql"), item.ID); err != nil {
		return entity.Chat{}, false, fmt.Errorf("delete chat participants: %w", err)
	}
	for _, roleID := range input.RoleIDs {
		if roleID <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, query("chat_participants__insert.sql"), item.ID, roleID); err != nil {
			return entity.Chat{}, false, fmt.Errorf("insert chat participant: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, query("chat_repositories__delete_by_chat.sql"), item.ID); err != nil {
		return entity.Chat{}, false, fmt.Errorf("delete chat repositories: %w", err)
	}
	for _, repositoryID := range input.RepositoryIDs {
		if repositoryID <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, query("chat_repositories__insert.sql"), item.ID, repositoryID); err != nil {
			return entity.Chat{}, false, fmt.Errorf("insert chat repository: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Chat{}, false, fmt.Errorf("commit create chat: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetChat(ctx context.Context, id int64) (entity.Chat, error) {
	item, err := scanChat(repo.pool.QueryRow(ctx, query("chats__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Chat{}, adminrepo.ErrNotFound
		}
		return entity.Chat{}, fmt.Errorf("get chat: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListChats(ctx context.Context, projectID int64) ([]entity.Chat, error) {
	rows, err := repo.pool.Query(ctx, query("chats__list.sql"), projectID)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	defer rows.Close()

	var items []entity.Chat
	for rows.Next() {
		item, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chats: %w", err)
	}
	return items, nil
}

func (repo *Repository) ListChatParticipants(ctx context.Context, chatID int64) ([]entity.ChatParticipant, error) {
	rows, err := repo.pool.Query(ctx, query("chat_participants__list.sql"), chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat participants: %w", err)
	}
	defer rows.Close()

	var items []entity.ChatParticipant
	for rows.Next() {
		var item entity.ChatParticipant
		if err := rows.Scan(&item.ID, &item.ChatID, &item.RoleID, &item.RoleName, &item.Enabled, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat participant: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat participants: %w", err)
	}
	return items, nil
}

func (repo *Repository) ListChatRepositories(ctx context.Context, chatID int64) ([]entity.ChatRepositoryBinding, error) {
	rows, err := repo.pool.Query(ctx, query("chat_repositories__list.sql"), chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat repositories: %w", err)
	}
	defer rows.Close()

	var items []entity.ChatRepositoryBinding
	for rows.Next() {
		var item entity.ChatRepositoryBinding
		if err := rows.Scan(&item.ID, &item.ChatID, &item.RepositoryID, &item.Provider, &item.Owner, &item.Name, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat repository: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat repositories: %w", err)
	}
	return items, nil
}

func scanProject(row accountRow) (entity.Project, error) {
	var item entity.Project
	if err := row.Scan(&item.ID, &item.Name, &item.Slug, &item.MattermostTeamID, &item.Description, &item.AdvancedSettings, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return entity.Project{}, err
	}
	return item, nil
}

func scanProjectWithCreated(row pgx.Row) (entity.Project, bool, error) {
	var item entity.Project
	var created bool
	if err := row.Scan(&item.ID, &item.Name, &item.Slug, &item.MattermostTeamID, &item.Description, &item.AdvancedSettings, &item.CreatedAt, &item.UpdatedAt, &created); err != nil {
		return entity.Project{}, false, err
	}
	return item, created, nil
}

func scanProjectRepository(row accountRow) (entity.ProjectRepository, error) {
	var item entity.ProjectRepository
	if err := row.Scan(&item.ID, &item.ProjectID, &item.RepositoryID, &item.Provider, &item.Owner, &item.Name, &item.DefaultBranch, &item.IsDefault, &item.Metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return entity.ProjectRepository{}, err
	}
	return item, nil
}

func scanProjectRepositoryWithCreated(row pgx.Row) (entity.ProjectRepository, bool, error) {
	var item entity.ProjectRepository
	var created bool
	if err := row.Scan(&item.ID, &item.ProjectID, &item.RepositoryID, &item.Provider, &item.Owner, &item.Name, &item.DefaultBranch, &item.IsDefault, &item.Metadata, &item.CreatedAt, &item.UpdatedAt, &created); err != nil {
		return entity.ProjectRepository{}, false, err
	}
	return item, created, nil
}

func scanAgentRole(row accountRow) (entity.AgentRole, error) {
	var item entity.AgentRole
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.RoleType,
		&item.Description,
		&item.PromptTemplate,
		&item.PromptMode,
		&item.GitHubAccountName,
		&item.OpenAIAccountName,
		&item.KubernetesAccess,
		&item.SandboxMode,
		&item.ConfigOverlay,
		&item.AdvancedSettings,
		&item.Enabled,
		&item.BotIdentity,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentRole{}, err
	}
	return item, nil
}

func scanAgentRoleWithCreated(row pgx.Row) (entity.AgentRole, bool, error) {
	var item entity.AgentRole
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.RoleType,
		&item.Description,
		&item.PromptTemplate,
		&item.PromptMode,
		&item.GitHubAccountName,
		&item.OpenAIAccountName,
		&item.KubernetesAccess,
		&item.SandboxMode,
		&item.ConfigOverlay,
		&item.AdvancedSettings,
		&item.Enabled,
		&item.BotIdentity,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.AgentRole{}, false, err
	}
	return item, created, nil
}

func scanChat(row accountRow) (entity.Chat, error) {
	var item entity.Chat
	if err := row.Scan(&item.ID, &item.ProjectID, &item.MattermostChannelID, &item.Name, &item.Slug, &item.Description, &item.ChatType, &item.RootGitHubIssue, &item.WorkPolicy, &item.Settings, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return entity.Chat{}, err
	}
	return item, nil
}

func scanChatWithCreated(row pgx.Row) (entity.Chat, bool, error) {
	var item entity.Chat
	var created bool
	if err := row.Scan(&item.ID, &item.ProjectID, &item.MattermostChannelID, &item.Name, &item.Slug, &item.Description, &item.ChatType, &item.RootGitHubIssue, &item.WorkPolicy, &item.Settings, &item.CreatedAt, &item.UpdatedAt, &created); err != nil {
		return entity.Chat{}, false, err
	}
	return item, created, nil
}
