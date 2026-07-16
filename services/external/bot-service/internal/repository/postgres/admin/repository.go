package admin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sync"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const agentSessionCapacityAdvisoryLockKey int64 = 0x6d61747465726364

//go:embed sql/*.sql
var queryFiles embed.FS

type Repository struct {
	pool *pgxpool.Pool
}

var _ adminrepo.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repo *Repository) MattermostPostMessageMaxRunes(ctx context.Context) (int, error) {
	var maxBytes int
	if err := repo.pool.QueryRow(ctx, `
		select coalesce(max(character_maximum_length), 0)
		from information_schema.columns
		where lower(table_name) = 'posts'
			and lower(column_name) = 'message'
	`).Scan(&maxBytes); err != nil {
		return 0, fmt.Errorf("get Mattermost post message max runes: %w", err)
	}
	if maxBytes <= 0 {
		return 0, nil
	}
	return maxBytes / 4, nil
}

func (repo *Repository) UpsertRepository(ctx context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	var item entity.Repository
	var created bool
	if err := repo.pool.QueryRow(ctx, query("repositories__upsert.sql"),
		input.Provider,
		input.Owner,
		input.Name,
		input.DefaultBranch,
		input.GitHubAccountName,
		input.MattermostChannel,
	).Scan(
		&item.ID,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.DefaultBranch,
		&item.GitHubAccountName,
		&item.Status,
		&item.MattermostChannel,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.Repository{}, false, fmt.Errorf("upsert repository: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetRepository(ctx context.Context, provider string, owner string, name string) (entity.Repository, error) {
	var item entity.Repository
	if err := repo.pool.QueryRow(ctx, query("repositories__get.sql"), provider, owner, name).Scan(
		&item.ID,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.DefaultBranch,
		&item.GitHubAccountName,
		&item.Status,
		&item.MattermostChannel,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Repository{}, adminrepo.ErrNotFound
		}
		return entity.Repository{}, fmt.Errorf("get repository: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListRepositories(ctx context.Context, limit int) ([]entity.Repository, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("repositories__list.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()

	var items []entity.Repository
	for rows.Next() {
		var item entity.Repository
		if err := rows.Scan(
			&item.ID,
			&item.Provider,
			&item.Owner,
			&item.Name,
			&item.DefaultBranch,
			&item.GitHubAccountName,
			&item.Status,
			&item.MattermostChannel,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return items, nil
}

func (repo *Repository) DeleteRepository(ctx context.Context, provider string, owner string, name string) (entity.Repository, error) {
	var item entity.Repository
	if err := repo.pool.QueryRow(ctx, query("repositories__delete.sql"), provider, owner, name).Scan(
		&item.ID,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.DefaultBranch,
		&item.GitHubAccountName,
		&item.Status,
		&item.MattermostChannel,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Repository{}, adminrepo.ErrNotFound
		}
		return entity.Repository{}, fmt.Errorf("delete repository: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertAgentProfile(ctx context.Context, input adminrepo.UpsertAgentProfileInput) (entity.AgentProfile, bool, error) {
	var created bool
	item, err := scanAgentProfileWithCreated(repo.pool.QueryRow(ctx, query("agent_profiles__upsert.sql"),
		input.Name,
		input.Role,
		input.Description,
		input.Enabled,
		input.OpenAIAccountName,
		input.GitHubAccountName,
		input.KubernetesAccess,
		input.SandboxMode,
		input.ConfigOverlay,
	), &created)
	if err != nil {
		return entity.AgentProfile{}, false, fmt.Errorf("upsert agent profile: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetAgentProfile(ctx context.Context, name string) (entity.AgentProfile, error) {
	item, err := scanAgentProfile(repo.pool.QueryRow(ctx, query("agent_profiles__get.sql"), name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentProfile{}, adminrepo.ErrNotFound
		}
		return entity.AgentProfile{}, fmt.Errorf("get agent profile: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error) {
	rows, err := repo.pool.Query(ctx, query("agent_profiles__list.sql"))
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentProfile
	for rows.Next() {
		item, err := scanAgentProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent profile: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent profiles: %w", err)
	}
	return items, nil
}

func (repo *Repository) ListAgentPromptTemplates(ctx context.Context, profileName string) ([]entity.AgentPromptTemplate, error) {
	rows, err := repo.pool.Query(ctx, query("agent_prompt_templates__list.sql"), profileName)
	if err != nil {
		return nil, fmt.Errorf("list agent prompt templates: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentPromptTemplate
	for rows.Next() {
		item, err := scanAgentPromptTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent prompt template: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent prompt templates: %w", err)
	}
	return items, nil
}

func (repo *Repository) GetAgentPromptTemplate(ctx context.Context, profileName string, templateKey string) (entity.AgentPromptTemplate, error) {
	item, err := scanAgentPromptTemplate(repo.pool.QueryRow(ctx, query("agent_prompt_templates__get.sql"), profileName, templateKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentPromptTemplate{}, adminrepo.ErrNotFound
		}
		return entity.AgentPromptTemplate{}, fmt.Errorf("get agent prompt template: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertAgentPromptTemplate(ctx context.Context, input adminrepo.UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error) {
	var created bool
	item, err := scanAgentPromptTemplateWithCreated(repo.pool.QueryRow(ctx, query("agent_prompt_templates__upsert.sql"),
		input.ProfileName,
		input.TemplateKey,
		input.Body,
	), &created)
	if err != nil {
		return entity.AgentPromptTemplate{}, false, fmt.Errorf("upsert agent prompt template: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) UpsertOpenAIAccount(ctx context.Context, input adminrepo.UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error) {
	row := repo.pool.QueryRow(ctx, query("openai_accounts__upsert.sql"),
		input.Name,
		input.CredentialName,
		input.SecretRef,
		input.Status,
	)
	item, created, err := scanOpenAIAccountWithCreated(row)
	if err != nil {
		return entity.OpenAIAccount{}, false, fmt.Errorf("upsert openai account: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) ListOpenAIAccounts(ctx context.Context, limit int) ([]entity.OpenAIAccount, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("openai_accounts__list.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list openai accounts: %w", err)
	}
	defer rows.Close()

	var items []entity.OpenAIAccount
	for rows.Next() {
		item, err := scanOpenAIAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan openai account: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate openai accounts: %w", err)
	}
	return items, nil
}

func (repo *Repository) GetOpenAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, error) {
	item, err := scanOpenAIAccount(repo.pool.QueryRow(ctx, query("openai_accounts__get.sql"), name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.OpenAIAccount{}, adminrepo.ErrNotFound
		}
		return entity.OpenAIAccount{}, fmt.Errorf("get openai account: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpdateOpenAIAccountStatus(ctx context.Context, input adminrepo.UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error) {
	item, err := scanOpenAIAccount(repo.pool.QueryRow(ctx, query("openai_accounts__update_status.sql"),
		input.Name,
		input.SecretRef,
		input.Status,
	))
	if err != nil {
		return entity.OpenAIAccount{}, fmt.Errorf("update openai account status: %w", err)
	}
	return item, nil
}

func (repo *Repository) DeleteOpenAIAccount(ctx context.Context, name string) (entity.OpenAIAccount, error) {
	item, err := scanOpenAIAccount(repo.pool.QueryRow(ctx, query("openai_accounts__delete.sql"), name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.OpenAIAccount{}, adminrepo.ErrNotFound
		}
		return entity.OpenAIAccount{}, fmt.Errorf("delete openai account: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListGitHubAccounts(ctx context.Context, limit int) ([]entity.GitHubAccount, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("github_accounts__list.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list github accounts: %w", err)
	}
	defer rows.Close()

	var items []entity.GitHubAccount
	for rows.Next() {
		item, err := scanGitHubAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan github account: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate github accounts: %w", err)
	}
	return items, nil
}

func (repo *Repository) GetGitHubAccount(ctx context.Context, name string) (entity.GitHubAccount, error) {
	item, err := scanGitHubAccount(repo.pool.QueryRow(ctx, query("github_accounts__get.sql"), name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.GitHubAccount{}, adminrepo.ErrNotFound
		}
		return entity.GitHubAccount{}, fmt.Errorf("get github account: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertGitHubAccount(ctx context.Context, input adminrepo.UpsertGitHubAccountInput) (entity.GitHubAccount, bool, error) {
	item, created, err := scanGitHubAccountWithCreated(repo.pool.QueryRow(ctx, query("github_accounts__upsert.sql"),
		input.Name,
		input.CredentialName,
		input.SecretRef,
		input.Username,
		input.Email,
		input.Scopes,
		input.Status,
	))
	if err != nil {
		return entity.GitHubAccount{}, false, fmt.Errorf("upsert github account: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) DeleteGitHubAccount(ctx context.Context, name string) (entity.GitHubAccount, error) {
	item, err := scanGitHubAccount(repo.pool.QueryRow(ctx, query("github_accounts__delete.sql"), name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.GitHubAccount{}, adminrepo.ErrNotFound
		}
		return entity.GitHubAccount{}, fmt.Errorf("delete github account: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertMattermostBotIdentity(ctx context.Context, input adminrepo.UpsertMattermostBotIdentityInput) (entity.MattermostBotIdentity, bool, error) {
	item, created, err := scanMattermostBotIdentityWithCreated(repo.pool.QueryRow(ctx, query("mattermost_bot_identities__upsert.sql"),
		input.ProjectID,
		input.RoleID,
		input.Username,
		input.DisplayName,
		input.MattermostUserID,
		input.TokenSecretRef,
		input.Status,
		input.LastError,
	))
	if err != nil {
		return entity.MattermostBotIdentity{}, false, fmt.Errorf("upsert mattermost bot identity: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetMattermostBotIdentityByRoleID(ctx context.Context, roleID int64) (entity.MattermostBotIdentity, error) {
	item, err := scanMattermostBotIdentity(repo.pool.QueryRow(ctx, query("mattermost_bot_identities__get_by_role.sql"), roleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
		}
		return entity.MattermostBotIdentity{}, fmt.Errorf("get mattermost bot identity by role: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetMattermostBotIdentityByUserID(ctx context.Context, mattermostUserID string) (entity.MattermostBotIdentity, error) {
	item, err := scanMattermostBotIdentity(repo.pool.QueryRow(ctx, query("mattermost_bot_identities__get_by_user.sql"), mattermostUserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
		}
		return entity.MattermostBotIdentity{}, fmt.Errorf("get mattermost bot identity by user: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListMattermostBotIdentitiesByProject(ctx context.Context, projectID int64) ([]entity.MattermostBotIdentity, error) {
	rows, err := repo.pool.Query(ctx, query("mattermost_bot_identities__list_by_project.sql"), projectID)
	if err != nil {
		return nil, fmt.Errorf("list mattermost bot identities: %w", err)
	}
	defer rows.Close()

	var items []entity.MattermostBotIdentity
	for rows.Next() {
		item, err := scanMattermostBotIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan mattermost bot identity: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mattermost bot identities: %w", err)
	}
	return items, nil
}

func (repo *Repository) UpsertAgentSession(ctx context.Context, input adminrepo.UpsertAgentSessionInput) (entity.AgentSession, bool, error) {
	item, created, err := scanAgentSessionWithCreated(repo.pool.QueryRow(ctx, query("agent_sessions__upsert.sql"),
		input.SessionKey,
		input.ProjectID,
		input.ChatID,
		input.RoleID,
		input.SessionScope,
		input.MattermostChannelID,
		input.MattermostRootPostID,
		input.TTLSeconds,
		input.Capabilities,
	))
	if err != nil {
		return entity.AgentSession{}, false, fmt.Errorf("upsert agent session: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetAgentSession(ctx context.Context, sessionKey string) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__get.sql"), sessionKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSession{}, adminrepo.ErrNotFound
		}
		return entity.AgentSession{}, fmt.Errorf("get agent session: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetAgentSessionByID(ctx context.Context, id int64) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__get_by_id.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSession{}, adminrepo.ErrNotFound
		}
		return entity.AgentSession{}, fmt.Errorf("get agent session by id: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentSessionsByThread(ctx context.Context, chatID int64, rootPostID string) ([]entity.AgentSession, error) {
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_by_thread.sql"), chatID, rootPostID)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions by thread: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) ListAgentSessionsByChat(ctx context.Context, chatID int64) ([]entity.AgentSession, error) {
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_by_chat.sql"), chatID)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions by chat: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) ListAgentSessionsByRole(ctx context.Context, roleID int64) ([]entity.AgentSession, error) {
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_by_role.sql"), roleID)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions by role: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) AcquireAgentSessionCapacityLock(ctx context.Context) (func(), error) {
	conn, err := repo.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire agent session capacity lock connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", agentSessionCapacityAdvisoryLockKey); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquire agent session capacity lock: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = conn.Exec(unlockCtx, "select pg_advisory_unlock($1)", agentSessionCapacityAdvisoryLockKey)
			conn.Release()
		})
	}, nil
}

func (repo *Repository) ListEvictableIdleAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_evictable_idle.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list evictable idle agent sessions: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) ListQueuedIdleAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_queued_idle.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list queued idle agent sessions: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) ListStaleActiveAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_stale_active.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list stale active agent sessions: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) ListRunningActiveAgentSessions(ctx context.Context, limit int) ([]entity.AgentSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_sessions__list_running_active.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list running active agent sessions: %w", err)
	}
	defer rows.Close()
	return scanAgentSessions(rows)
}

func (repo *Repository) UpdateAgentSessionRuntime(ctx context.Context, input adminrepo.UpdateAgentSessionRuntimeInput) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__update_runtime.sql"),
		input.SessionKey,
		input.Status,
		input.ActiveTurnID,
		input.ActiveRunID,
		input.MattermostRootPostID,
		input.KubernetesNamespace,
		input.PodName,
		input.PVCName,
		input.TokenSecretRef,
		input.ExtendTTLSeconds,
	))
	if err != nil {
		return entity.AgentSession{}, fmt.Errorf("update agent session runtime: %w", err)
	}
	return item, nil
}

func (repo *Repository) ResetAgentSessionRuntime(ctx context.Context, sessionKey string, status string) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__reset_runtime.sql"), sessionKey, status))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSession{}, adminrepo.ErrNotFound
		}
		return entity.AgentSession{}, fmt.Errorf("reset agent session runtime: %w", err)
	}
	return item, nil
}

func (repo *Repository) ClearIdleAgentSessionPod(ctx context.Context, sessionKey string, podName string) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__clear_idle_pod.sql"), sessionKey, podName))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSession{}, adminrepo.ErrNotFound
		}
		return entity.AgentSession{}, fmt.Errorf("clear idle agent session pod: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpdateAgentSessionSnapshot(ctx context.Context, input adminrepo.UpdateAgentSessionSnapshotInput) (entity.AgentSession, error) {
	item, err := scanAgentSession(repo.pool.QueryRow(ctx, query("agent_sessions__update_snapshot.sql"),
		input.SessionKey,
		input.CodexSessionID,
		input.SessionArchiveGzipBase64,
		input.Status,
		input.ExtendTTLSeconds,
	))
	if err != nil {
		return entity.AgentSession{}, fmt.Errorf("update agent session snapshot: %w", err)
	}
	return item, nil
}

func (repo *Repository) CreateAgentSessionTurn(ctx context.Context, input adminrepo.CreateAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__insert.sql"),
		input.SessionID,
		input.RunID,
		input.MattermostChannelID,
		input.MattermostRootPostID,
		input.MattermostPostID,
		input.UserID,
		input.UserName,
		input.Message,
	))
	if err != nil {
		return entity.AgentSessionTurn{}, fmt.Errorf("create agent session turn: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetAgentSessionTurn(ctx context.Context, id int64) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionTurn{}, fmt.Errorf("get agent session turn: %w", err)
	}
	return item, nil
}

func (repo *Repository) ClaimNextAgentSessionTurn(ctx context.Context, sessionKey string) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__claim_next.sql"), sessionKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionTurn{}, fmt.Errorf("claim next agent session turn: %w", err)
	}
	return item, nil
}

func (repo *Repository) CompleteAgentSessionTurn(ctx context.Context, input adminrepo.CompleteAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__complete.sql"),
		input.TurnID,
		input.Status,
		input.FinalMessage,
		input.ErrorMessage,
		input.Artifacts,
	))
	if err != nil {
		return entity.AgentSessionTurn{}, fmt.Errorf("complete agent session turn: %w", err)
	}
	return item, nil
}

func (repo *Repository) CancelAgentSessionTurn(ctx context.Context, input adminrepo.CancelAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__cancel.sql"),
		input.TurnID,
		input.ErrorMessage,
		input.Artifacts,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionTurn{}, fmt.Errorf("cancel agent session turn: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpdateAgentSessionTurnStatusPost(ctx context.Context, input adminrepo.UpdateAgentSessionTurnStatusPostInput) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__update_status_post.sql"),
		input.TurnID,
		input.StatusPostID,
	))
	if err != nil {
		return entity.AgentSessionTurn{}, fmt.Errorf("update agent session turn status post: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpdateAgentSessionTurnMessage(ctx context.Context, input adminrepo.UpdateAgentSessionTurnMessageInput) (entity.AgentSessionTurn, error) {
	item, err := scanAgentSessionTurn(repo.pool.QueryRow(ctx, query("agent_session_turns__update_message.sql"),
		input.TurnID,
		input.Message,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionTurn{}, fmt.Errorf("update agent session turn message: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListQueuedAgentSessionTurns(ctx context.Context, sessionID int64) ([]entity.AgentSessionTurn, error) {
	rows, err := repo.pool.Query(ctx, query("agent_session_turns__list_queued.sql"), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list queued agent session turns: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentSessionTurn
	for rows.Next() {
		item, err := scanAgentSessionTurn(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent session turn: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent session turns: %w", err)
	}
	return items, nil
}

func (repo *Repository) CreateAgentDelegation(ctx context.Context, input adminrepo.CreateAgentDelegationInput) (entity.AgentDelegation, bool, error) {
	item, err := scanAgentDelegation(repo.pool.QueryRow(ctx, query("agent_delegations__insert.sql"),
		input.ProjectID,
		input.SourceSessionID,
		input.SourceTurnID,
		input.TargetChatID,
		input.TargetRoleID,
		input.WorkItemKey,
		input.Title,
	))
	if err == nil {
		return item, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentDelegation{}, false, fmt.Errorf("create agent delegation: %w", err)
	}
	item, err = repo.GetAgentDelegationBySourceKey(ctx, input.SourceSessionID, input.WorkItemKey)
	if err != nil {
		return entity.AgentDelegation{}, false, err
	}
	return item, false, nil
}

func (repo *Repository) GetAgentDelegationBySourceKey(ctx context.Context, sourceSessionID int64, workItemKey string) (entity.AgentDelegation, error) {
	item, err := scanAgentDelegation(repo.pool.QueryRow(ctx, query("agent_delegations__get_by_source_key.sql"), sourceSessionID, workItemKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentDelegation{}, adminrepo.ErrNotFound
		}
		return entity.AgentDelegation{}, fmt.Errorf("get agent delegation by source key: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetAgentDelegationForCallback(ctx context.Context, targetSessionID int64) (entity.AgentDelegation, error) {
	item, err := scanAgentDelegation(repo.pool.QueryRow(ctx, query("agent_delegations__get_for_callback.sql"), targetSessionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentDelegation{}, adminrepo.ErrNotFound
		}
		return entity.AgentDelegation{}, fmt.Errorf("get agent delegation for callback: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentDelegationsBySource(ctx context.Context, sourceSessionID int64, limit int) ([]entity.AgentDelegation, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_delegations__list_by_source.sql"), sourceSessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent delegations by source: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentDelegation
	for rows.Next() {
		item, err := scanAgentDelegation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent delegation: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent delegations: %w", err)
	}
	return items, nil
}

func (repo *Repository) SetAgentDelegationRoot(ctx context.Context, id int64, rootPostID string) (entity.AgentDelegation, error) {
	return repo.updateAgentDelegation(ctx, "set root", "agent_delegations__set_root.sql", id, rootPostID)
}

func (repo *Repository) SetAgentDelegationTarget(ctx context.Context, id int64, targetSessionID int64, targetTurnID int64, targetRunID string) (entity.AgentDelegation, error) {
	return repo.updateAgentDelegation(ctx, "set target", "agent_delegations__set_target.sql", id, targetSessionID, targetTurnID, targetRunID)
}

func (repo *Repository) SetAgentDelegationFailed(ctx context.Context, id int64) (entity.AgentDelegation, error) {
	return repo.updateAgentDelegation(ctx, "set failed", "agent_delegations__set_failed.sql", id)
}

func (repo *Repository) SetAgentDelegationCallback(ctx context.Context, id int64, callbackTurnID int64, callbackRunID string) (entity.AgentDelegation, error) {
	item, err := repo.updateAgentDelegation(ctx, "set callback", "agent_delegations__set_callback.sql", id, callbackTurnID, callbackRunID)
	if !errors.Is(err, adminrepo.ErrNotFound) {
		return item, err
	}
	item, err = scanAgentDelegation(repo.pool.QueryRow(ctx, query("agent_delegations__get.sql"), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentDelegation{}, adminrepo.ErrNotFound
		}
		return entity.AgentDelegation{}, fmt.Errorf("get existing agent delegation callback: %w", err)
	}
	return item, nil
}

func (repo *Repository) updateAgentDelegation(ctx context.Context, action string, queryName string, args ...any) (entity.AgentDelegation, error) {
	item, err := scanAgentDelegation(repo.pool.QueryRow(ctx, query(queryName), args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentDelegation{}, adminrepo.ErrNotFound
		}
		return entity.AgentDelegation{}, fmt.Errorf("%s agent delegation: %w", action, err)
	}
	return item, nil
}

func (repo *Repository) CreateAgentFlow(ctx context.Context, input adminrepo.CreateAgentFlowInput) (entity.AgentFlow, bool, error) {
	row := repo.pool.QueryRow(ctx, query("agent_flows__insert.sql"),
		input.FlowID,
		input.Status,
		input.Provider,
		input.Owner,
		input.Name,
		input.BaseBranch,
		input.HeadBranch,
		input.Title,
		input.Task,
		input.Attempt,
		input.MaxAttempts,
		input.DeveloperProfileName,
		input.ReviewerProfileName,
		input.FlowPreset,
		input.OwnerUserID,
		input.OwnerUser,
		input.ActionToken,
		input.Summary,
	)
	item, created, err := scanAgentFlowWithCreated(row)
	if err != nil {
		return entity.AgentFlow{}, false, fmt.Errorf("insert agent flow: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetAgentFlow(ctx context.Context, flowID string) (entity.AgentFlow, error) {
	item, err := scanAgentFlow(repo.pool.QueryRow(ctx, query("agent_flows__get.sql"), flowID))
	if err != nil {
		return entity.AgentFlow{}, fmt.Errorf("get agent flow: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentFlows(ctx context.Context, status string, limit int) ([]entity.AgentFlow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_flows__list.sql"), status, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent flows: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentFlow
	for rows.Next() {
		item, err := scanAgentFlow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent flow: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent flows: %w", err)
	}
	return items, nil
}

func (repo *Repository) UpdateAgentFlow(ctx context.Context, input adminrepo.UpdateAgentFlowInput) (entity.AgentFlow, error) {
	item, err := scanAgentFlow(repo.pool.QueryRow(ctx, query("agent_flows__update.sql"),
		input.FlowID,
		input.Status,
		input.PRURL,
		input.PRNumber,
		input.Attempt,
		input.CurrentDeveloperRunID,
		input.CurrentReviewerRunID,
		input.OwnerUserID,
		input.OwnerUser,
		input.ControlChannelID,
		input.ControlPostID,
		input.ActionToken,
		input.OwnerDecision,
		input.Summary,
	))
	if err != nil {
		return entity.AgentFlow{}, fmt.Errorf("update agent flow: %w", err)
	}
	return item, nil
}

func (repo *Repository) CreateAgentRun(ctx context.Context, input adminrepo.CreateAgentRunInput) (entity.AgentRun, error) {
	row := repo.pool.QueryRow(ctx, query("agent_runs__insert.sql"),
		input.RunID,
		input.FlowID,
		input.ProfileName,
		input.Role,
		input.Provider,
		input.Owner,
		input.Name,
		input.BaseBranch,
		input.HeadBranch,
		input.Status,
		input.KubernetesNamespace,
		input.JobName,
		input.PVCName,
		input.Summary,
	)
	item, err := scanAgentRun(row)
	if err != nil {
		return entity.AgentRun{}, fmt.Errorf("insert agent run: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetAgentRun(ctx context.Context, runID string) (entity.AgentRun, error) {
	item, err := scanAgentRun(repo.pool.QueryRow(ctx, query("agent_runs__get.sql"), runID))
	if err != nil {
		return entity.AgentRun{}, fmt.Errorf("get agent run: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListAgentRuns(ctx context.Context, limit int) ([]entity.AgentRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := repo.pool.Query(ctx, query("agent_runs__list.sql"), limit)
	if err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentRun
	for rows.Next() {
		item, err := scanAgentRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent run: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent runs: %w", err)
	}
	return items, nil
}

func (repo *Repository) ListAgentRunsByFlowID(ctx context.Context, flowID string) ([]entity.AgentRun, error) {
	rows, err := repo.pool.Query(ctx, query("agent_runs__list_by_flow.sql"), flowID)
	if err != nil {
		return nil, fmt.Errorf("list agent runs by flow: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentRun
	for rows.Next() {
		item, err := scanAgentRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent run: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent runs: %w", err)
	}
	return items, nil
}

func (repo *Repository) UpdateAgentRunArtifacts(ctx context.Context, input adminrepo.UpdateAgentRunArtifactsInput) (entity.AgentRun, error) {
	item, err := scanAgentRun(repo.pool.QueryRow(ctx, query("agent_runs__update_artifacts.sql"),
		input.RunID,
		input.Status,
		input.PRURL,
	))
	if err != nil {
		return entity.AgentRun{}, fmt.Errorf("update agent run artifacts: %w", err)
	}
	return item, nil
}

func (repo *Repository) RecordAuditEvent(ctx context.Context, input adminrepo.AuditEventInput) error {
	if _, err := repo.pool.Exec(ctx, query("audit_events__insert.sql"),
		input.EventType,
		input.ActorUserID,
		input.ActorUser,
		input.ResourceType,
		input.ResourceName,
		input.Summary,
	); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

type accountRow interface {
	Scan(dest ...any) error
}

func scanAgentProfile(row accountRow) (entity.AgentProfile, error) {
	var item entity.AgentProfile
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Role,
		&item.Description,
		&item.Enabled,
		&item.OpenAIAccountName,
		&item.GitHubAccountName,
		&item.KubernetesAccess,
		&item.SandboxMode,
		&item.ConfigOverlay,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentProfile{}, err
	}
	return item, nil
}

func scanAgentProfileWithCreated(row pgx.Row, created *bool) (entity.AgentProfile, error) {
	var item entity.AgentProfile
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Role,
		&item.Description,
		&item.Enabled,
		&item.OpenAIAccountName,
		&item.GitHubAccountName,
		&item.KubernetesAccess,
		&item.SandboxMode,
		&item.ConfigOverlay,
		&item.CreatedAt,
		&item.UpdatedAt,
		created,
	); err != nil {
		return entity.AgentProfile{}, err
	}
	return item, nil
}

func scanOpenAIAccount(row accountRow) (entity.OpenAIAccount, error) {
	var item entity.OpenAIAccount
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.CredentialID,
		&item.SecretRef,
		&item.Status,
		&item.ModelPolicy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.OpenAIAccount{}, err
	}
	return item, nil
}

func scanOpenAIAccountWithCreated(row pgx.Row) (entity.OpenAIAccount, bool, error) {
	var item entity.OpenAIAccount
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.CredentialID,
		&item.SecretRef,
		&item.Status,
		&item.ModelPolicy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.OpenAIAccount{}, false, err
	}
	return item, created, nil
}

func scanGitHubAccount(row pgx.Row) (entity.GitHubAccount, error) {
	var item entity.GitHubAccount
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.CredentialID,
		&item.SecretRef,
		&item.Username,
		&item.Email,
		&item.Scopes,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.GitHubAccount{}, err
	}
	return item, nil
}

func scanGitHubAccountWithCreated(row pgx.Row) (entity.GitHubAccount, bool, error) {
	var item entity.GitHubAccount
	var created bool
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.CredentialID,
		&item.SecretRef,
		&item.Username,
		&item.Email,
		&item.Scopes,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
		&created,
	); err != nil {
		return entity.GitHubAccount{}, false, err
	}
	return item, created, nil
}

func scanMattermostBotIdentity(row pgx.Row) (entity.MattermostBotIdentity, error) {
	item, err := scanMattermostBotIdentityFields(row)
	if err != nil {
		return entity.MattermostBotIdentity{}, err
	}
	return item, nil
}

func scanMattermostBotIdentityWithCreated(row pgx.Row) (entity.MattermostBotIdentity, bool, error) {
	var created bool
	item, err := scanMattermostBotIdentityFields(row, &created)
	if err != nil {
		return entity.MattermostBotIdentity{}, false, err
	}
	return item, created, nil
}

func scanMattermostBotIdentityFields(row pgx.Row, extra ...any) (entity.MattermostBotIdentity, error) {
	var item entity.MattermostBotIdentity
	dest := []any{
		&item.ID,
		&item.ProjectID,
		&item.RoleID,
		&item.Username,
		&item.DisplayName,
		&item.MattermostUserID,
		&item.TokenSecretRef,
		&item.Status,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return entity.MattermostBotIdentity{}, err
	}
	return item, nil
}

func scanAgentSession(row pgx.Row) (entity.AgentSession, error) {
	item, err := scanAgentSessionFields(row)
	if err != nil {
		return entity.AgentSession{}, err
	}
	return item, nil
}

func scanAgentSessionWithCreated(row pgx.Row) (entity.AgentSession, bool, error) {
	var created bool
	item, err := scanAgentSessionFields(row, &created)
	if err != nil {
		return entity.AgentSession{}, false, err
	}
	return item, created, nil
}

func scanAgentSessionFields(row pgx.Row, extra ...any) (entity.AgentSession, error) {
	var item entity.AgentSession
	dest := []any{
		&item.ID,
		&item.SessionKey,
		&item.ProjectID,
		&item.ChatID,
		&item.RoleID,
		&item.SessionScope,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.CodexSessionID,
		&item.Status,
		&item.ActiveTurnID,
		&item.ActiveRunID,
		&item.KubernetesNamespace,
		&item.PodName,
		&item.PVCName,
		&item.TokenSecretRef,
		&item.Capabilities,
		&item.SessionArchiveGzipBase64,
		&item.TTLSeconds,
		&item.LastActivityAt,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return entity.AgentSession{}, err
	}
	return item, nil
}

func scanAgentSessions(rows pgx.Rows) ([]entity.AgentSession, error) {
	var items []entity.AgentSession
	for rows.Next() {
		item, err := scanAgentSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent session: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent sessions: %w", err)
	}
	return items, nil
}

func scanAgentSessionTurn(row pgx.Row) (entity.AgentSessionTurn, error) {
	var item entity.AgentSessionTurn
	if err := row.Scan(
		&item.ID,
		&item.SessionID,
		&item.RunID,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.MattermostPostID,
		&item.MattermostStatusPostID,
		&item.UserID,
		&item.UserName,
		&item.Message,
		&item.Status,
		&item.FinalMessage,
		&item.ErrorMessage,
		&item.Artifacts,
		&item.CreatedAt,
		&item.StartedAt,
		&item.FinishedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentSessionTurn{}, err
	}
	return item, nil
}

func scanAgentDelegation(row pgx.Row) (entity.AgentDelegation, error) {
	var item entity.AgentDelegation
	if err := row.Scan(
		&item.ID,
		&item.ProjectID,
		&item.SourceSessionID,
		&item.SourceTurnID,
		&item.TargetChatID,
		&item.TargetRoleID,
		&item.TargetRootPostID,
		&item.TargetSessionID,
		&item.TargetTurnID,
		&item.TargetRunID,
		&item.WorkItemKey,
		&item.Title,
		&item.Status,
		&item.CallbackTurnID,
		&item.CallbackRunID,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentDelegation{}, err
	}
	return item, nil
}

func scanAgentRun(row pgx.Row) (entity.AgentRun, error) {
	var item entity.AgentRun
	if err := row.Scan(
		&item.ID,
		&item.RunID,
		&item.FlowID,
		&item.ProfileName,
		&item.Role,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.BaseBranch,
		&item.HeadBranch,
		&item.Status,
		&item.KubernetesNamespace,
		&item.JobName,
		&item.PVCName,
		&item.PRURL,
		&item.Summary,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentRun{}, err
	}
	return item, nil
}

func scanAgentFlow(row pgx.Row) (entity.AgentFlow, error) {
	item, err := scanAgentFlowFields(row)
	if err != nil {
		return entity.AgentFlow{}, err
	}
	return item, nil
}

func scanAgentFlowWithCreated(row pgx.Row) (entity.AgentFlow, bool, error) {
	var created bool
	item, err := scanAgentFlowFields(row, &created)
	if err != nil {
		return entity.AgentFlow{}, false, err
	}
	return item, created, nil
}

func scanAgentFlowFields(row pgx.Row, extra ...any) (entity.AgentFlow, error) {
	var item entity.AgentFlow
	dest := []any{
		&item.ID,
		&item.FlowID,
		&item.Status,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.BaseBranch,
		&item.HeadBranch,
		&item.Title,
		&item.Task,
		&item.PRURL,
		&item.PRNumber,
		&item.Attempt,
		&item.MaxAttempts,
		&item.DeveloperProfileName,
		&item.ReviewerProfileName,
		&item.FlowPreset,
		&item.CurrentDeveloperRunID,
		&item.CurrentReviewerRunID,
		&item.OwnerUserID,
		&item.OwnerUser,
		&item.ControlChannelID,
		&item.ControlPostID,
		&item.ActionToken,
		&item.OwnerDecision,
		&item.Summary,
		&item.CreatedAt,
		&item.UpdatedAt,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return entity.AgentFlow{}, err
	}
	return item, nil
}

type promptTemplateRow interface {
	Scan(dest ...any) error
}

func scanAgentPromptTemplate(row promptTemplateRow) (entity.AgentPromptTemplate, error) {
	var item entity.AgentPromptTemplate
	if err := row.Scan(
		&item.ID,
		&item.ProfileName,
		&item.TemplateKey,
		&item.Body,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return entity.AgentPromptTemplate{}, err
	}
	return item, nil
}

func scanAgentPromptTemplateWithCreated(row pgx.Row, created *bool) (entity.AgentPromptTemplate, error) {
	var item entity.AgentPromptTemplate
	if err := row.Scan(
		&item.ID,
		&item.ProfileName,
		&item.TemplateKey,
		&item.Body,
		&item.CreatedAt,
		&item.UpdatedAt,
		created,
	); err != nil {
		return entity.AgentPromptTemplate{}, err
	}
	return item, nil
}

func query(name string) string {
	body, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return string(body)
}
