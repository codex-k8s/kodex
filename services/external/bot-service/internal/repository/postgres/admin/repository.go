package admin

import (
	"context"
	"embed"
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

func (repo *Repository) CreateAgentRun(ctx context.Context, input adminrepo.CreateAgentRunInput) (entity.AgentRun, error) {
	row := repo.pool.QueryRow(ctx, query("agent_runs__insert.sql"),
		input.RunID,
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

func scanAgentRun(row pgx.Row) (entity.AgentRun, error) {
	var item entity.AgentRun
	if err := row.Scan(
		&item.ID,
		&item.RunID,
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

func query(name string) string {
	body, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return string(body)
}
