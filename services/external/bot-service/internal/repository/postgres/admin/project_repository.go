package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repo *Repository) UpsertProject(ctx context.Context, input adminrepo.UpsertProjectInput) (entity.Project, bool, error) {
	item, created, err := scanProjectWithCreated(repo.db.QueryRow(ctx, query("projects__upsert.sql"),
		input.Name,
		input.Slug,
		input.MattermostTeamID,
		input.GitHubAccountName,
		input.GitHubOwner,
		input.GitHubOwnerType,
		input.Description,
		input.AdvancedSettings,
	))
	if err != nil {
		return entity.Project{}, false, fmt.Errorf("upsert project: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) UpdateProjectRunsChannel(ctx context.Context, projectID int64, channelID string) (entity.Project, error) {
	item, err := scanProject(repo.db.QueryRow(ctx, query("projects__update_runs_channel.sql"), projectID, channelID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Project{}, adminrepo.ErrNotFound
		}
		return entity.Project{}, fmt.Errorf("update project runs channel: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetProject(ctx context.Context, id int64) (entity.Project, error) {
	item, err := scanProject(repo.db.QueryRow(ctx, query("projects__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Project{}, adminrepo.ErrNotFound
		}
		return entity.Project{}, fmt.Errorf("get project: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetProjectBySlug(ctx context.Context, slug string) (entity.Project, error) {
	item, err := scanProject(repo.db.QueryRow(ctx, query("projects__get_by_slug.sql"), slug))
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
	rows, err := repo.db.Query(ctx, query("projects__list.sql"), limit)
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
	item, created, err := scanProjectRepositoryWithCreated(repo.db.QueryRow(ctx, query("project_repositories__upsert.sql"),
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
	rows, err := repo.db.Query(ctx, query("project_repositories__list.sql"), projectID)
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
	if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") {
		return repo.upsertFrozenClusterAdminRole(ctx, input)
	}
	item, created, err := scanAgentRoleWithCreated(repo.db.QueryRow(ctx, query("agent_roles__upsert.sql"),
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
		if errors.Is(err, pgx.ErrNoRows) && strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") {
			return entity.AgentRole{}, false, adminrepo.ErrClusterAdminAdmissionDenied
		}
		return entity.AgentRole{}, false, fmt.Errorf("upsert agent role: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) upsertFrozenClusterAdminRole(ctx context.Context, input adminrepo.UpsertAgentRoleInput) (entity.AgentRole, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("begin cluster-admin role upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, created, err := scanAgentRoleWithCreated(tx.QueryRow(ctx, query("agent_roles__update_frozen_cluster_admin.sql"),
		input.ProjectID, input.Name, input.RoleType, input.Description, input.PromptTemplate, input.PromptMode,
		input.GitHubAccountName, input.OpenAIAccountName, input.KubernetesAccess, input.SandboxMode,
		input.ConfigOverlay, input.AdvancedSettings, input.Enabled, input.BotIdentity,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		if auditErr := repo.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
			EventType: "cluster_admin.admission.denied", ActorUser: "repository",
			ResourceType: "agent_role", ResourceName: input.Name, Summary: "agent_role.upsert: denied",
		}); auditErr != nil {
			return entity.AgentRole{}, false, fmt.Errorf("audit denied cluster-admin agent role upsert: %w", auditErr)
		}
		return entity.AgentRole{}, false, adminrepo.ErrClusterAdminAdmissionDenied
	}
	if err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("upsert cluster-admin agent role: %w", err)
	}
	if _, err := tx.Exec(ctx, query("audit_events__insert.sql"),
		"cluster_admin.admission.allowed", "", "repository", "agent_role", input.Name, "agent_role.upsert: allowed",
	); err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("audit cluster-admin agent role upsert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("commit cluster-admin agent role upsert: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetAgentRole(ctx context.Context, id int64) (entity.AgentRole, error) {
	item, err := scanAgentRole(repo.db.QueryRow(ctx, query("agent_roles__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentRole{}, adminrepo.ErrNotFound
		}
		return entity.AgentRole{}, fmt.Errorf("get agent role: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentRoles(ctx context.Context, projectID int64) ([]entity.AgentRole, error) {
	rows, err := repo.db.Query(ctx, query("agent_roles__list.sql"), projectID)
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
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.Chat{}, false, fmt.Errorf("begin create chat: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bindingsAllowed, err := lockClusterAdminChatMutation(ctx, tx, input)
	if err != nil {
		return entity.Chat{}, false, err
	}
	if !bindingsAllowed {
		_ = tx.Rollback(ctx)
		if auditErr := repo.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
			EventType:    "cluster_admin.binding.denied",
			ActorUser:    "repository",
			ResourceType: "chat",
			ResourceName: input.Slug,
			Summary:      "chat.create: denied",
		}); auditErr != nil {
			return entity.Chat{}, false, fmt.Errorf("audit denied cluster-admin chat binding: %w", auditErr)
		}
		return entity.Chat{}, false, adminrepo.ErrClusterAdminAdmissionDenied
	}

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
	if _, err := tx.Exec(ctx, query("chat_participants__delete_not_selected.sql"), item.ID, input.RoleIDs); err != nil {
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
	if _, err := tx.Exec(ctx, query("chat_repositories__delete_not_selected.sql"), item.ID, input.RepositoryIDs); err != nil {
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

func lockClusterAdminChatMutation(ctx context.Context, tx pgx.Tx, input adminrepo.CreateChatInput) (bool, error) {
	rows, err := tx.Query(ctx, query("cluster_admin_chat_roles__lock.sql"), input.ProjectID, input.RoleIDs)
	if err != nil {
		return false, fmt.Errorf("lock cluster-admin chat roles: %w", err)
	}
	clusterAdminRoleIDs := make([]int64, 0, len(input.RoleIDs))
	for rows.Next() {
		var roleID int64
		var clusterAdmin bool
		if err := rows.Scan(&roleID, &clusterAdmin); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan locked cluster-admin chat role: %w", err)
		}
		if clusterAdmin {
			clusterAdminRoleIDs = append(clusterAdminRoleIDs, roleID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("read locked cluster-admin chat roles: %w", err)
	}
	rows.Close()
	if len(clusterAdminRoleIDs) == 0 {
		return true, nil
	}

	var chatID int64
	var channelID string
	err = tx.QueryRow(ctx, query("chats__get_by_project_slug_for_update.sql"), input.ProjectID, input.Slug).Scan(&chatID, &channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock frozen cluster-admin chat: %w", err)
	}
	if strings.TrimSpace(channelID) != strings.TrimSpace(input.MattermostChannelID) {
		return false, nil
	}

	participantRows, err := tx.Query(ctx, query("chat_participants__lock.sql"), chatID, clusterAdminRoleIDs)
	if err != nil {
		return false, fmt.Errorf("lock frozen cluster-admin chat participants: %w", err)
	}
	for participantRows.Next() {
		var roleID int64
		var enabled bool
		if err := participantRows.Scan(&roleID, &enabled); err != nil {
			participantRows.Close()
			return false, fmt.Errorf("scan locked cluster-admin chat participant: %w", err)
		}
	}
	if err := participantRows.Err(); err != nil {
		participantRows.Close()
		return false, fmt.Errorf("read locked cluster-admin chat participants: %w", err)
	}
	participantRows.Close()
	for _, roleID := range clusterAdminRoleIDs {
		if err := lockClusterAdminRuntimeVariables(ctx, tx, roleID); err != nil {
			return false, err
		}
		if err := lockClusterAdminRuntimeDependencies(ctx, tx, roleID); err != nil {
			return false, err
		}
	}
	allowedRepositoryRows, err := tx.Query(ctx, query("cluster_admin_chat_repositories__lock_allowed.sql"), clusterAdminRoleIDs, chatID)
	if err != nil {
		return false, fmt.Errorf("lock frozen cluster-admin chat repositories: %w", err)
	}
	allowedRepositories := make(map[int64]map[int64]struct{}, len(clusterAdminRoleIDs))
	for allowedRepositoryRows.Next() {
		var roleID int64
		var repositoryID int64
		if err := allowedRepositoryRows.Scan(&roleID, &repositoryID); err != nil {
			allowedRepositoryRows.Close()
			return false, fmt.Errorf("scan frozen cluster-admin chat repository: %w", err)
		}
		if allowedRepositories[roleID] == nil {
			allowedRepositories[roleID] = make(map[int64]struct{})
		}
		allowedRepositories[roleID][repositoryID] = struct{}{}
	}
	if err := allowedRepositoryRows.Err(); err != nil {
		allowedRepositoryRows.Close()
		return false, fmt.Errorf("read frozen cluster-admin chat repositories: %w", err)
	}
	allowedRepositoryRows.Close()
	for _, roleID := range clusterAdminRoleIDs {
		for _, repositoryID := range input.RepositoryIDs {
			if repositoryID <= 0 {
				continue
			}
			if _, ok := allowedRepositories[roleID][repositoryID]; !ok {
				return false, nil
			}
		}
	}

	var allowed bool
	if err := tx.QueryRow(ctx, query("cluster_admin_bindings__chat_allowed.sql"),
		input.ProjectID, input.Slug, input.MattermostChannelID, clusterAdminRoleIDs,
	).Scan(&allowed); err != nil {
		return false, fmt.Errorf("check frozen cluster-admin chat bindings: %w", err)
	}
	return allowed, nil
}

func (repo *Repository) GetChat(ctx context.Context, id int64) (entity.Chat, error) {
	item, err := scanChat(repo.db.QueryRow(ctx, query("chats__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Chat{}, adminrepo.ErrNotFound
		}
		return entity.Chat{}, fmt.Errorf("get chat: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetChatByMattermostChannelID(ctx context.Context, channelID string) (entity.Chat, error) {
	item, err := scanChat(repo.db.QueryRow(ctx, query("chats__get_by_channel.sql"), channelID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Chat{}, adminrepo.ErrNotFound
		}
		return entity.Chat{}, fmt.Errorf("get chat by Mattermost channel: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListChats(ctx context.Context, projectID int64) ([]entity.Chat, error) {
	rows, err := repo.db.Query(ctx, query("chats__list.sql"), projectID)
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
	rows, err := repo.db.Query(ctx, query("chat_participants__list.sql"), chatID)
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
	rows, err := repo.db.Query(ctx, query("chat_repositories__list.sql"), chatID)
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

func (repo *Repository) GetThreadContext(ctx context.Context, chatID int64, rootPostID string) (entity.ThreadContext, error) {
	item, err := scanThreadContext(repo.db.QueryRow(ctx, query("thread_contexts__get.sql"), chatID, rootPostID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ThreadContext{}, adminrepo.ErrNotFound
		}
		return entity.ThreadContext{}, fmt.Errorf("get thread context: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetThreadContextByID(ctx context.Context, id int64) (entity.ThreadContext, error) {
	item, err := scanThreadContext(repo.db.QueryRow(ctx, query("thread_contexts__get_by_id.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ThreadContext{}, adminrepo.ErrNotFound
		}
		return entity.ThreadContext{}, fmt.Errorf("get thread context by id: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertThreadContext(ctx context.Context, input adminrepo.UpsertThreadContextInput) (entity.ThreadContext, bool, error) {
	item, created, err := scanThreadContextWithCreated(repo.db.QueryRow(ctx, query("thread_contexts__upsert.sql"),
		input.ProjectID,
		input.ChatID,
		input.MattermostChannelID,
		input.MattermostRootPostID,
		input.RepositoryID,
		input.Status,
		input.PendingMattermostPostID,
		input.PendingUserID,
		input.PendingUserName,
		input.PendingMessage,
	))
	if err != nil {
		return entity.ThreadContext{}, false, fmt.Errorf("upsert thread context: %w", err)
	}
	return item, created, nil
}

func scanProject(row accountRow) (entity.Project, error) {
	var item entity.Project
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Slug,
		&item.MattermostTeamID,
		&item.MattermostRunsChannelID,
		&item.GitHubAccountName,
		&item.GitHubOwner,
		&item.GitHubOwnerType,
		&item.Description,
		&item.AdvancedSettings,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.Project{}, err
	}
	return item, nil
}

func scanProjectWithCreated(row pgx.Row) (entity.Project, bool, error) {
	var item entity.Project
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Slug,
		&item.MattermostTeamID,
		&item.MattermostRunsChannelID,
		&item.GitHubAccountName,
		&item.GitHubOwner,
		&item.GitHubOwnerType,
		&item.Description,
		&item.AdvancedSettings,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
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

func scanThreadContext(row accountRow) (entity.ThreadContext, error) {
	var item entity.ThreadContext
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.ChatID,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.RepositoryID,
		&item.RepositoryProvider,
		&item.RepositoryOwner,
		&item.RepositoryName,
		&item.RepositoryDefaultBranch,
		&item.Status,
		&item.PendingMattermostPostID,
		&item.PendingUserID,
		&item.PendingUserName,
		&item.PendingMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.ThreadContext{}, err
	}
	return item, nil
}

func scanThreadContextWithCreated(row pgx.Row) (entity.ThreadContext, bool, error) {
	var item entity.ThreadContext
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.ChatID,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.RepositoryID,
		&item.RepositoryProvider,
		&item.RepositoryOwner,
		&item.RepositoryName,
		&item.RepositoryDefaultBranch,
		&item.Status,
		&item.PendingMattermostPostID,
		&item.PendingUserID,
		&item.PendingUserName,
		&item.PendingMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.ThreadContext{}, false, err
	}
	return item, created, nil
}
