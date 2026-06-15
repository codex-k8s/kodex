package admin

import (
	"context"
	"embed"
	"errors"
	"fmt"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var queryFiles embed.FS

type Repository struct {
	pool *pgxpool.Pool
}

var _ adminrepo.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repo *Repository) UpsertRepository(ctx context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	var item entity.Repository
	var created bool
	if err := repo.pool.QueryRow(ctx, query("repositories__upsert.sql"),
		input.Provider,
		input.Owner,
		input.Name,
		input.DefaultBranch,
		input.MattermostChannel,
	).Scan(
		&item.ID,
		&item.Provider,
		&item.Owner,
		&item.Name,
		&item.DefaultBranch,
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

func (repo *Repository) ListAgentProfiles(ctx context.Context) ([]entity.AgentProfile, error) {
	rows, err := repo.pool.Query(ctx, query("agent_profiles__list.sql"))
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()

	var items []entity.AgentProfile
	for rows.Next() {
		var item entity.AgentProfile
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Role,
			&item.Description,
			&item.Enabled,
			&item.OpenAIAccountName,
			&item.GitHubAccountName,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
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
