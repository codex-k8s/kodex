package automations

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var queryFiles embed.FS

type repositoryDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}

type Repository struct {
	db repositoryDB
}

var _ automationsrepo.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{db: pool}
}

func newTransactionalRepository(tx pgx.Tx) *Repository {
	return &Repository{db: tx}
}

func (repo *Repository) CreateSchedule(ctx context.Context, input automationsrepo.CreateScheduleInput) (entity.AutomationSchedule, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AutomationSchedule{}, false, fmt.Errorf("begin create automation schedule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txRepo := newTransactionalRepository(tx)
	var publicID string
	var commandHash []byte
	var created bool
	err = txRepo.db.QueryRow(ctx, query("automation_schedules__upsert.sql"),
		input.PublicID,
		input.ProjectID,
		input.TargetAgentRoleID,
		input.TargetChatID,
		input.Name,
		input.OwnerMattermostUserID,
		input.OwnerMattermostUserName,
		input.Preset,
		input.LocalTime,
		input.TimeZone,
		input.NextRunAt,
		input.PlaybookKey,
		input.PromptVersion,
		input.PromptSnapshot,
		input.PromptSHA256,
		input.CallbackContractVersion,
		input.IdempotencyKey,
		input.CommandHash,
		input.Now,
	).Scan(&publicID, &commandHash, &created)
	if err != nil {
		return entity.AutomationSchedule{}, false, fmt.Errorf("upsert automation schedule: %w", err)
	}
	if !bytes.Equal(commandHash, input.CommandHash) {
		return entity.AutomationSchedule{}, false, automationsrepo.ErrConflict
	}

	item, err := txRepo.getSchedule(ctx, publicID, input.ProjectID, input.OwnerMattermostUserID)
	if err != nil {
		return entity.AutomationSchedule{}, false, err
	}
	if created {
		if err := txRepo.insertAudit(ctx, item.ProjectID, item.ID, 0, "schedule.created", item.OwnerMattermostUserID, item.OwnerMattermostUserName, item.PublicID, ""); err != nil {
			return entity.AutomationSchedule{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.AutomationSchedule{}, false, fmt.Errorf("commit create automation schedule: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetSchedule(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.AutomationSchedule, error) {
	return repo.getSchedule(ctx, publicID, projectID, ownerMattermostUserID)
}

func (repo *Repository) getSchedule(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.AutomationSchedule, error) {
	item, err := scanSchedule(repo.db.QueryRow(ctx, query("automation_schedules__get.sql"), publicID, projectID, ownerMattermostUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AutomationSchedule{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.AutomationSchedule{}, fmt.Errorf("get automation schedule: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListSchedules(ctx context.Context, projectID int64, ownerMattermostUserID string, limit int) ([]entity.AutomationSchedule, error) {
	limit = normalizedLimit(limit)
	rows, err := repo.db.Query(ctx, query("automation_schedules__list.sql"), projectID, ownerMattermostUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list automation schedules: %w", err)
	}
	defer rows.Close()

	items := make([]entity.AutomationSchedule, 0)
	for rows.Next() {
		item, scanErr := scanSchedule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan automation schedule: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate automation schedules: %w", err)
	}
	return items, nil
}

func (repo *Repository) CreateManualRun(ctx context.Context, input automationsrepo.CreateManualRunInput) (entity.ScheduledRun, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("begin create manual automation run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txRepo := newTransactionalRepository(tx)
	schedule, err := scanSchedule(txRepo.db.QueryRow(ctx, query("automation_schedules__lock.sql"), input.SchedulePublicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ScheduledRun{}, false, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("lock automation schedule: %w", err)
	}
	if schedule.ProjectID != input.ProjectID || schedule.OwnerMattermostUserID != input.OwnerMattermostUserID {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if !schedule.Enabled {
		return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
	}

	var occurrenceID int64
	var occurrencePublicID string
	var occurrenceCreated bool
	if err := txRepo.db.QueryRow(ctx, query("schedule_occurrences__upsert.sql"),
		input.OccurrencePublicID,
		schedule.ID,
		schedule.ProjectID,
		input.IdempotencyKey,
		input.ScheduledFor,
	).Scan(&occurrenceID, &occurrencePublicID, &occurrenceCreated); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("upsert manual schedule occurrence: %w", err)
	}

	var runPublicID string
	var runCreated bool
	if err := txRepo.db.QueryRow(ctx, query("scheduled_runs__insert.sql"),
		input.RunPublicID,
		occurrenceID,
		schedule.ID,
		schedule.ProjectID,
		schedule.TargetAgentRoleID,
		schedule.TargetChatID,
		schedule.OwnerMattermostUserID,
		schedule.OwnerMattermostUserName,
		schedule.PromptVersion,
		schedule.CallbackContractVersion,
		input.CallbackExpiresAt,
	).Scan(&runPublicID, &runCreated); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("insert scheduled run: %w", err)
	}
	created := occurrenceCreated && runCreated
	item, err := txRepo.getRun(ctx, runPublicID, schedule.ProjectID, schedule.OwnerMattermostUserID)
	if err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if created {
		if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.queued", item.OwnerMattermostUserID, item.OwnerMattermostUserName, item.CorrelationID, ""); err != nil {
			return entity.ScheduledRun{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("commit create manual automation run: %w", err)
	}
	_ = occurrencePublicID
	return item, created, nil
}

func (repo *Repository) BindRun(ctx context.Context, input automationsrepo.BindRunInput) (entity.ScheduledRun, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("begin bind scheduled run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	item, err := txRepo.lockRun(ctx, input.RunPublicID)
	if err != nil {
		return entity.ScheduledRun{}, err
	}
	if item.ProjectID != input.ProjectID || item.OwnerMattermostUserID != input.OwnerMattermostUserID {
		return entity.ScheduledRun{}, automationsrepo.ErrForbidden
	}
	if item.Status == string(value.AutomationRunStatusSucceeded) || item.Status == string(value.AutomationRunStatusFailed) {
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	if item.RuntimeSessionID != 0 {
		if item.RuntimeSessionID == input.RuntimeSessionID && item.RuntimeSessionKey == input.RuntimeSessionKey && item.RuntimeTurnID == input.RuntimeTurnID && item.RuntimeRunID == input.RuntimeRunID && item.MattermostChannelID == input.MattermostChannelID && item.MattermostRootPostID == input.MattermostRootPostID {
			if err := tx.Commit(ctx); err != nil {
				return entity.ScheduledRun{}, fmt.Errorf("commit existing scheduled run binding: %w", err)
			}
			return item, nil
		}
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__bind.sql"), item.ID, input.RuntimeSessionID, input.RuntimeSessionKey, input.RuntimeTurnID, input.RuntimeRunID, input.MattermostChannelID, input.MattermostRootPostID, input.Now); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("bind scheduled run: %w", err)
	}
	if _, err := txRepo.db.Exec(ctx, query("schedule_occurrences__status.sql"), item.OccurrenceID, string(value.AutomationRunStatusRunning), input.Now); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("mark schedule occurrence running: %w", err)
	}
	if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.started", item.OwnerMattermostUserID, item.OwnerMattermostUserName, item.CorrelationID, ""); err != nil {
		return entity.ScheduledRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("commit scheduled run binding: %w", err)
	}
	return repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
}

func (repo *Repository) FailRun(ctx context.Context, input automationsrepo.FailRunInput) (entity.ScheduledRun, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("begin fail scheduled run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	item, err := txRepo.lockRun(ctx, input.RunPublicID)
	if err != nil {
		return entity.ScheduledRun{}, err
	}
	if item.ProjectID != input.ProjectID || item.OwnerMattermostUserID != input.OwnerMattermostUserID {
		return entity.ScheduledRun{}, automationsrepo.ErrForbidden
	}
	if item.Status == string(value.AutomationRunStatusFailed) {
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, fmt.Errorf("commit existing failed scheduled run: %w", err)
		}
		return item, nil
	}
	if item.Status == string(value.AutomationRunStatusSucceeded) {
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__fail.sql"), item.ID, input.SafeSummary, input.Now); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("fail scheduled run: %w", err)
	}
	if _, err := txRepo.db.Exec(ctx, query("schedule_occurrences__status.sql"), item.OccurrenceID, string(value.AutomationRunStatusFailed), input.Now); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("fail schedule occurrence: %w", err)
	}
	if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.failed", item.OwnerMattermostUserID, item.OwnerMattermostUserName, item.CorrelationID, input.SafeSummary); err != nil {
		return entity.ScheduledRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("commit failed scheduled run: %w", err)
	}
	return repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
}

func (repo *Repository) GetRun(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.ScheduledRun, error) {
	return repo.getRun(ctx, publicID, projectID, ownerMattermostUserID)
}

func (repo *Repository) getRun(ctx context.Context, publicID string, projectID int64, ownerMattermostUserID string) (entity.ScheduledRun, error) {
	item, err := scanRun(repo.db.QueryRow(ctx, query("scheduled_runs__get.sql"), publicID, projectID, ownerMattermostUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ScheduledRun{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("get scheduled run: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListRuns(ctx context.Context, schedulePublicID string, projectID int64, ownerMattermostUserID string, limit int) ([]entity.ScheduledRun, error) {
	rows, err := repo.db.Query(ctx, query("scheduled_runs__list.sql"), projectID, ownerMattermostUserID, schedulePublicID, normalizedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list scheduled runs: %w", err)
	}
	defer rows.Close()
	items := make([]entity.ScheduledRun, 0)
	for rows.Next() {
		item, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan scheduled run: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduled runs: %w", err)
	}
	return items, nil
}

func (repo *Repository) CompleteCallback(ctx context.Context, input automationsrepo.CompleteCallbackInput) (entity.ScheduledRun, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("begin automation callback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	item, err := txRepo.lockRun(ctx, input.RunPublicID)
	if err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if item.ProjectID != input.ProjectID {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if item.RuntimeSessionID != input.RuntimeSessionID || item.RuntimeTurnID != input.RuntimeTurnID || item.RuntimeRunID != input.RuntimeRunID || item.CallbackContractVersion != input.CallbackContractVersion {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if !item.CallbackRevokedAt.IsZero() || !input.Now.Before(item.CallbackExpiresAt) {
		return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackRevoked
	}
	if item.Status == string(value.AutomationRunStatusSucceeded) || item.Status == string(value.AutomationRunStatusFailed) {
		if bytes.Equal(item.CallbackPayloadSHA256, input.PayloadSHA256) {
			if err := tx.Commit(ctx); err != nil {
				return entity.ScheduledRun{}, false, fmt.Errorf("commit duplicate automation callback: %w", err)
			}
			return item, true, nil
		}
		return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackMismatch
	}
	if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__complete.sql"), item.ID, input.Status, input.Outcome, input.SafeSummary, input.PayloadSHA256, input.Now); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("complete scheduled run: %w", err)
	}
	if _, err := txRepo.db.Exec(ctx, query("schedule_occurrences__status.sql"), item.OccurrenceID, input.Status, input.Now); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("complete schedule occurrence: %w", err)
	}
	if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.completed", "", "", item.CorrelationID, input.SafeSummary); err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("commit automation callback: %w", err)
	}
	result, err := repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
	return result, false, err
}

func (repo *Repository) RevokeCallback(ctx context.Context, runPublicID string, projectID int64, now time.Time) error {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke automation callback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	item, err := txRepo.lockRun(ctx, runPublicID)
	if err != nil {
		return err
	}
	if item.ProjectID != projectID {
		return automationsrepo.ErrForbidden
	}
	if item.CallbackRevokedAt.IsZero() {
		if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__revoke.sql"), runPublicID, projectID, now); err != nil {
			return fmt.Errorf("revoke automation callback: %w", err)
		}
		if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "callback.revoked", "", "", item.CorrelationID, ""); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke automation callback: %w", err)
	}
	return nil
}

func (repo *Repository) lockRun(ctx context.Context, publicID string) (entity.ScheduledRun, error) {
	item, err := scanRun(repo.db.QueryRow(ctx, query("scheduled_runs__lock_callback.sql"), publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ScheduledRun{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("lock scheduled run: %w", err)
	}
	return item, nil
}

func (repo *Repository) insertAudit(ctx context.Context, projectID int64, scheduleID int64, runID int64, eventType string, actorUserID string, actorUserName string, correlationID string, safeSummary string) error {
	var nullableScheduleID any
	if scheduleID != 0 {
		nullableScheduleID = scheduleID
	}
	var nullableRunID any
	if runID != 0 {
		nullableRunID = runID
	}
	if _, err := repo.db.Exec(ctx, query("automation_audit_events__insert.sql"), projectID, nullableScheduleID, nullableRunID, eventType, actorUserID, actorUserName, correlationID, safeSummary); err != nil {
		return fmt.Errorf("insert automation audit event: %w", err)
	}
	return nil
}

func scanSchedule(row pgx.Row) (entity.AutomationSchedule, error) {
	var item entity.AutomationSchedule
	err := row.Scan(
		&item.ID,
		&item.PublicID,
		&item.ProjectID,
		&item.ProjectName,
		&item.TargetAgentRoleID,
		&item.TargetAgentRoleName,
		&item.TargetChatID,
		&item.TargetChatName,
		&item.Name,
		&item.OwnerMattermostUserID,
		&item.OwnerMattermostUserName,
		&item.Preset,
		&item.LocalTime,
		&item.TimeZone,
		&item.Enabled,
		&item.NextRunAt,
		&item.PlaybookKey,
		&item.PromptVersion,
		&item.PromptSnapshot,
		&item.PromptSHA256,
		&item.CallbackContractVersion,
		&item.CommandHash,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func scanRun(row pgx.Row) (entity.ScheduledRun, error) {
	var item entity.ScheduledRun
	err := row.Scan(
		&item.ID,
		&item.PublicID,
		&item.OccurrenceID,
		&item.ScheduleID,
		&item.SchedulePublicID,
		&item.ScheduleName,
		&item.ProjectID,
		&item.ProjectName,
		&item.TargetAgentRoleID,
		&item.TargetAgentRoleName,
		&item.TargetChatID,
		&item.TargetChatName,
		&item.OwnerMattermostUserID,
		&item.OwnerMattermostUserName,
		&item.Source,
		&item.Status,
		&item.Outcome,
		&item.SafeSummary,
		&item.CorrelationID,
		&item.PromptVersion,
		&item.CallbackContractVersion,
		&item.CallbackPayloadSHA256,
		&item.CallbackRevokedAt,
		&item.CallbackExpiresAt,
		&item.RuntimeSessionID,
		&item.RuntimeSessionKey,
		&item.RuntimeTurnID,
		&item.RuntimeRunID,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.StartedAt,
		&item.FinishedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err == nil {
		epoch := time.Unix(0, 0).UTC()
		if item.CallbackRevokedAt.Equal(epoch) {
			item.CallbackRevokedAt = time.Time{}
		}
		if item.StartedAt.Equal(epoch) {
			item.StartedAt = time.Time{}
		}
		if item.FinishedAt.Equal(epoch) {
			item.FinishedAt = time.Time{}
		}
	}
	return item, err
}

func normalizedLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

func query(name string) string {
	body, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return string(body)
}
