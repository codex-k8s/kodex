package automations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		input.RuntimeRunID,
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

func (repo *Repository) RecordRunThread(ctx context.Context, input automationsrepo.RecordRunThreadInput) (entity.ScheduledRun, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("begin record automation thread: %w", err)
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
	if item.Status != string(value.AutomationRunStatusQueued) {
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	if item.MattermostRootPostID != "" || item.MattermostChannelID != "" {
		if item.MattermostChannelID != input.MattermostChannelID || item.MattermostRootPostID != input.MattermostRootPostID {
			return entity.ScheduledRun{}, automationsrepo.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, fmt.Errorf("commit existing automation thread: %w", err)
		}
		return item, nil
	}
	if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__record_thread.sql"), item.ID, input.MattermostChannelID, input.MattermostRootPostID, input.Now); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("record automation thread: %w", err)
	}
	if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.thread_recorded", item.OwnerMattermostUserID, item.OwnerMattermostUserName, item.CorrelationID, ""); err != nil {
		return entity.ScheduledRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("commit automation thread: %w", err)
	}
	return repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
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
	if automationRunIsTerminal(item.Status) {
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	if item.RuntimeRunID != input.RuntimeRunID || item.MattermostChannelID != input.MattermostChannelID || item.MattermostRootPostID != input.MattermostRootPostID {
		return entity.ScheduledRun{}, automationsrepo.ErrConflict
	}
	binding, err := txRepo.lockRuntimeBinding(ctx, input.RuntimeSessionID, input.RuntimeTurnID)
	if err != nil {
		return entity.ScheduledRun{}, err
	}
	if binding.SessionID != input.RuntimeSessionID || binding.SessionKey != input.RuntimeSessionKey || binding.ProjectID != item.ProjectID || binding.ChatID != item.TargetChatID || binding.RoleID != item.TargetAgentRoleID ||
		binding.TurnID != input.RuntimeTurnID || binding.TurnSessionID != binding.SessionID || binding.TurnRunID != input.RuntimeRunID ||
		binding.TurnChannelID != input.MattermostChannelID || binding.TurnRootPostID != input.MattermostRootPostID || binding.TurnPostID != input.MattermostRootPostID {
		return entity.ScheduledRun{}, automationsrepo.ErrForbidden
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
	if binding.TurnStatus != "queued" && binding.TurnStatus != "running" {
		if err := txRepo.failRunLocked(ctx, item, "Среда выполнения завершилась до сохранения полной привязки автоматизации.", input.Now, "run.runtime_terminal"); err != nil {
			return entity.ScheduledRun{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, fmt.Errorf("commit terminal automation binding: %w", err)
		}
		return repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
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

func (repo *Repository) GetOwnerGateContext(ctx context.Context, input automationsrepo.OwnerGateContextInput) (entity.AutomationOwnerGateContext, error) {
	return repo.getOwnerGateContext(ctx, input)
}

func (repo *Repository) getOwnerGateContext(ctx context.Context, input automationsrepo.OwnerGateContextInput) (entity.AutomationOwnerGateContext, error) {
	var item entity.AutomationOwnerGateContext
	err := repo.db.QueryRow(ctx, query("automation_owner_gate_context__get.sql"),
		input.RunPublicID,
		input.AuthenticatedProjectID,
		input.AuthenticatedSessionID,
		input.AuthenticatedSessionKey,
	).Scan(
		&item.ScheduledRunID,
		&item.ScheduledRunPublicID,
		&item.ProjectID,
		&item.RuntimeTurnID,
		&item.ProcessRunID,
		&item.ProcessPublicID,
		&item.PolicyRevisionID,
		&item.RootInitiatorUserID,
		&item.RootInitiatorName,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AutomationOwnerGateContext{}, automationsrepo.ErrForbidden
	}
	if err != nil {
		return entity.AutomationOwnerGateContext{}, fmt.Errorf("get automation owner gate context: %w", err)
	}
	return item, nil
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
	if item.ProjectID != input.AuthenticatedProjectID || item.RuntimeSessionID != input.AuthenticatedSessionID || item.RuntimeSessionKey != input.AuthenticatedSessionKey || item.CallbackContractVersion != input.CallbackContractVersion {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if automationRunIsTerminal(item.Status) || item.Status == string(value.AutomationRunStatusWaitingOwner) {
		if bytes.Equal(item.CallbackPayloadSHA256, input.PayloadSHA256) {
			if err := tx.Commit(ctx); err != nil {
				return entity.ScheduledRun{}, false, fmt.Errorf("commit duplicate automation callback: %w", err)
			}
			return item, true, nil
		}
		return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackMismatch
	}
	if item.RuntimeSessionID == 0 || item.RuntimeTurnID == 0 || item.RuntimeRunID == "" {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	binding, err := txRepo.lockRuntimeBinding(ctx, item.RuntimeSessionID, item.RuntimeTurnID)
	if err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if binding.SessionID != item.RuntimeSessionID || binding.SessionKey != item.RuntimeSessionKey || binding.ProjectID != item.ProjectID || binding.ChatID != item.TargetChatID || binding.RoleID != item.TargetAgentRoleID ||
		binding.SessionStatus != "running" ||
		binding.ActiveTurnID != item.RuntimeTurnID || binding.ActiveRunID != item.RuntimeRunID ||
		binding.TurnID != item.RuntimeTurnID || binding.TurnSessionID != item.RuntimeSessionID || binding.TurnRunID != item.RuntimeRunID ||
		binding.TurnChannelID != item.MattermostChannelID || binding.TurnRootPostID != item.MattermostRootPostID || binding.TurnPostID != item.MattermostRootPostID ||
		(binding.TurnStatus != "queued" && binding.TurnStatus != "running") {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if !item.CallbackRevokedAt.IsZero() || !input.Now.Before(item.CallbackExpiresAt) {
		return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackRevoked
	}
	if input.Outcome == string(value.AutomationRunOutcomeRequiresHuman) {
		if input.OwnerGate == nil {
			return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
		}
		gateContext, err := txRepo.getOwnerGateContext(ctx, automationsrepo.OwnerGateContextInput{
			RunPublicID:             input.RunPublicID,
			AuthenticatedProjectID:  input.AuthenticatedProjectID,
			AuthenticatedSessionID:  input.AuthenticatedSessionID,
			AuthenticatedSessionKey: input.AuthenticatedSessionKey,
		})
		if err != nil {
			return entity.ScheduledRun{}, false, err
		}
		if err := validateOwnerGatePlan(item, gateContext, *input.OwnerGate); err != nil {
			return entity.ScheduledRun{}, false, err
		}
		var attentionID int64
		err = txRepo.db.QueryRow(ctx, query("automation_owner_attention__insert.sql"),
			gateContext.ProcessRunID,
			gateContext.RuntimeTurnID,
			input.OwnerGate.AttentionSummary,
			input.OwnerGate.AttentionRecommendation,
			"automation:"+item.PublicID,
			item.ID,
			item.ProjectID,
			gateContext.PolicyRevisionID,
			gateContext.RootInitiatorUserID,
			item.MattermostChannelID,
			item.MattermostRootPostID,
			input.OwnerGate.DeliveryID,
			input.OwnerGate.DeliveryMessage,
			input.OwnerGate.DeliveryPropsJSON,
			input.OwnerGate.DeliveryPayloadSHA256,
		).Scan(&attentionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
		if err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("create automation owner attention: %w", err)
		}
		if attentionID <= 0 {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
		if _, err := txRepo.db.Exec(ctx, query("scheduled_runs__wait_owner.sql"), item.ID, input.SafeSummary, input.PayloadSHA256, input.Now); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("mark scheduled run waiting for owner: %w", err)
		}
		if _, err := txRepo.db.Exec(ctx, query("schedule_occurrences__status.sql"), item.OccurrenceID, string(value.AutomationRunStatusWaitingOwner), input.Now); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("mark schedule occurrence waiting for owner: %w", err)
		}
		if _, err := txRepo.db.Exec(ctx, query("process_runs__wait_owner.sql"), gateContext.ProcessRunID, input.Now); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("mark automation process waiting for owner: %w", err)
		}
		if err := txRepo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, "run.waiting_owner", "", "", item.CorrelationID, input.SafeSummary); err != nil {
			return entity.ScheduledRun{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("commit automation owner gate: %w", err)
		}
		result, err := repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
		return result, false, err
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

func (repo *Repository) GetOwnerAttentionDelivery(ctx context.Context, scheduledRunID int64) (entity.AutomationOwnerAttentionDelivery, error) {
	item, err := scanOwnerAttentionDelivery(repo.db.QueryRow(ctx, query("automation_owner_attention__get.sql"), scheduledRunID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, fmt.Errorf("get automation owner attention delivery: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListHistory(ctx context.Context, ownerMattermostUsername string, limit int) ([]entity.AutomationHistoryItem, error) {
	ownerMattermostUsername = strings.TrimSpace(ownerMattermostUsername)
	if ownerMattermostUsername == "" {
		return nil, automationsrepo.ErrForbidden
	}
	rows, err := repo.db.Query(ctx, query("automation_history__list.sql"), ownerMattermostUsername, normalizedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list automation history: %w", err)
	}
	defer rows.Close()
	items := make([]entity.AutomationHistoryItem, 0)
	for rows.Next() {
		var item entity.AutomationHistoryItem
		if err := rows.Scan(
			&item.ScheduledRunPublicID,
			&item.Status,
			&item.Outcome,
			&item.OwnerAttentionID,
			&item.HumanDecisionStatus,
			&item.DeliveryStatus,
			&item.NextAction,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan automation history: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate automation history: %w", err)
	}
	return items, nil
}

func (repo *Repository) ClaimOwnerAttentionDelivery(ctx context.Context, input automationsrepo.ClaimOwnerAttentionDeliveryInput) (entity.AutomationOwnerAttentionDelivery, error) {
	if input.ScheduledRunID < 0 || len(strings.TrimSpace(input.ClaimToken)) < 16 || len(input.ClaimToken) > 128 || !input.LeaseUntil.After(input.Now) || input.EligibleBefore.IsZero() {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrForbidden
	}
	item, err := scanOwnerAttentionDelivery(repo.db.QueryRow(ctx, query("automation_owner_attention__claim.sql"),
		input.ScheduledRunID,
		input.ClaimToken,
		input.Now,
		input.LeaseUntil,
		input.EligibleBefore,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, fmt.Errorf("claim automation owner attention delivery: %w", err)
	}
	return item, nil
}

func (repo *Repository) DeferOwnerAttentionDelivery(ctx context.Context, input automationsrepo.DeferOwnerAttentionDeliveryInput) error {
	if input.AttentionID <= 0 || input.ScheduledRunID <= 0 || strings.TrimSpace(input.DeliveryID) == "" || len(strings.TrimSpace(input.ClaimToken)) < 16 || input.Fence <= 0 || input.RetryAt.Before(input.Now) {
		return automationsrepo.ErrForbidden
	}
	tag, err := repo.db.Exec(ctx, query("automation_owner_attention__defer.sql"),
		input.AttentionID,
		input.ScheduledRunID,
		input.DeliveryID,
		input.ClaimToken,
		input.Fence,
		input.RetryAt,
		input.Now,
	)
	if err != nil {
		return fmt.Errorf("defer automation owner attention delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return automationsrepo.ErrConflict
	}
	return nil
}

func (repo *Repository) RetainOwnerAttentionDelivery(ctx context.Context, input automationsrepo.RetainOwnerAttentionDeliveryInput) error {
	if input.AttentionID <= 0 || input.ScheduledRunID <= 0 || strings.TrimSpace(input.DeliveryID) == "" || len(strings.TrimSpace(input.ClaimToken)) < 16 || input.Fence <= 0 || !input.LeaseUntil.After(input.Now) {
		return automationsrepo.ErrForbidden
	}
	tag, err := repo.db.Exec(ctx, query("automation_owner_attention__retain.sql"),
		input.AttentionID,
		input.ScheduledRunID,
		input.DeliveryID,
		input.ClaimToken,
		input.Fence,
		input.LeaseUntil,
		input.Now,
	)
	if err != nil {
		return fmt.Errorf("retain ambiguous automation owner attention delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return automationsrepo.ErrConflict
	}
	return nil
}

func (repo *Repository) SetOwnerAttentionPost(ctx context.Context, input automationsrepo.SetOwnerAttentionPostInput) (entity.AutomationOwnerAttentionDelivery, error) {
	if input.AttentionID <= 0 || input.ScheduledRunID <= 0 || strings.TrimSpace(input.DeliveryID) == "" || strings.TrimSpace(input.MattermostPostID) == "" || len(strings.TrimSpace(input.ClaimToken)) < 16 || input.Fence <= 0 {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrForbidden
	}
	tag, err := repo.db.Exec(ctx, query("automation_owner_attention__set_post.sql"),
		input.AttentionID,
		input.ScheduledRunID,
		input.DeliveryID,
		input.MattermostChannelID,
		input.MattermostRootPostID,
		input.MattermostPostID,
		input.ClaimToken,
		input.Fence,
		input.Now,
	)
	if err != nil {
		return entity.AutomationOwnerAttentionDelivery{}, fmt.Errorf("set automation owner attention post: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrConflict
	}
	return repo.GetOwnerAttentionDelivery(ctx, input.ScheduledRunID)
}

func (repo *Repository) ResolveOwnerGate(ctx context.Context, input automationsrepo.ResolveOwnerGateInput) (entity.ScheduledRun, bool, error) {
	if input.ProjectID <= 0 || strings.TrimSpace(input.ActorUserID) == "" || strings.TrimSpace(input.MattermostChannelID) == "" || strings.TrimSpace(input.MattermostRootPostID) == "" || strings.TrimSpace(input.MattermostResponsePostID) == "" {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("begin resolve automation owner gate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	rows, err := txRepo.db.Query(ctx, query("automation_owner_attention__lock_resolution.sql"),
		input.ProjectID,
		strings.TrimSpace(input.ActorUserID),
		strings.TrimSpace(input.MattermostChannelID),
		strings.TrimSpace(input.MattermostRootPostID),
		strings.TrimSpace(input.MattermostResponsePostID),
	)
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("lock automation owner gate resolution: %w", err)
	}
	type resolutionRow struct {
		attentionID     int64
		attentionStatus string
		resolvedUserID  string
		resolvedPostID  string
		runID           int64
		runPublicID     string
		occurrenceID    int64
		scheduleID      int64
		ownerUserID     string
		processRunID    int64
	}
	candidates := make([]resolutionRow, 0, 2)
	for rows.Next() {
		var candidate resolutionRow
		if err := rows.Scan(
			&candidate.attentionID,
			&candidate.attentionStatus,
			&candidate.resolvedUserID,
			&candidate.resolvedPostID,
			&candidate.runID,
			&candidate.runPublicID,
			&candidate.occurrenceID,
			&candidate.scheduleID,
			&candidate.ownerUserID,
			&candidate.processRunID,
		); err != nil {
			rows.Close()
			return entity.ScheduledRun{}, false, fmt.Errorf("scan automation owner gate resolution: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("iterate automation owner gate resolution: %w", err)
	}
	if len(candidates) == 0 {
		return entity.ScheduledRun{}, false, automationsrepo.ErrNotFound
	}
	if len(candidates) != 1 {
		return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
	}
	candidate := candidates[0]
	duplicate := candidate.attentionStatus == "resolved"
	if duplicate {
		if candidate.resolvedUserID != strings.TrimSpace(input.ActorUserID) || candidate.resolvedPostID != strings.TrimSpace(input.MattermostResponsePostID) {
			return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
		}
	} else {
		tag, err := txRepo.db.Exec(ctx, query("automation_owner_attention__resolve.sql"), candidate.attentionID, strings.TrimSpace(input.ActorUserID), strings.TrimSpace(input.MattermostResponsePostID), input.Now)
		if err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("resolve automation owner attention: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
		tag, err = txRepo.db.Exec(ctx, query("scheduled_runs__resolve_owner.sql"), candidate.runID, input.Now)
		if err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("complete scheduled run after owner decision: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
		tag, err = txRepo.db.Exec(ctx, query("schedule_occurrences__status.sql"), candidate.occurrenceID, string(value.AutomationRunStatusSucceeded), input.Now)
		if err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("complete schedule occurrence after owner decision: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
		if err := txRepo.insertAudit(ctx, input.ProjectID, candidate.scheduleID, candidate.runID, "run.owner_resolved", strings.TrimSpace(input.ActorUserID), strings.TrimSpace(input.ActorUserName), candidate.runPublicID, ""); err != nil {
			return entity.ScheduledRun{}, false, err
		}
		tag, err = txRepo.db.Exec(ctx, query("process_runs__reconcile_owner_gate.sql"), candidate.processRunID, input.Now)
		if err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("reconcile process after automation owner decision: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return entity.ScheduledRun{}, false, automationsrepo.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("commit automation owner gate resolution: %w", err)
	}
	result, err := repo.getRun(ctx, candidate.runPublicID, input.ProjectID, candidate.ownerUserID)
	return result, duplicate, err
}

func (repo *Repository) ReconcileRuntimeTerminal(ctx context.Context, input automationsrepo.ReconcileRuntimeTerminalInput) (entity.ScheduledRun, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("begin automation runtime reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepo := newTransactionalRepository(tx)
	item, err := txRepo.lockRunByRuntime(ctx, input.ProjectID, input.RuntimeRunID)
	if errors.Is(err, automationsrepo.ErrNotFound) {
		return entity.ScheduledRun{}, false, nil
	}
	if err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if item.RuntimeRunID != input.RuntimeRunID || item.ProjectID != input.ProjectID {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if item.RuntimeSessionID != 0 && (item.RuntimeSessionID != input.RuntimeSessionID || item.RuntimeTurnID != input.RuntimeTurnID) {
		return entity.ScheduledRun{}, false, automationsrepo.ErrForbidden
	}
	if item.Status == string(value.AutomationRunStatusWaitingOwner) {
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("commit waiting-owner automation reconciliation: %w", err)
		}
		return item, false, nil
	}
	if automationRunIsTerminal(item.Status) {
		if err := tx.Commit(ctx); err != nil {
			return entity.ScheduledRun{}, false, fmt.Errorf("commit existing terminal automation run: %w", err)
		}
		return item, false, nil
	}
	if err := txRepo.failRunLocked(ctx, item, input.SafeSummary, input.Now, "run.runtime_terminal"); err != nil {
		return entity.ScheduledRun{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ScheduledRun{}, false, fmt.Errorf("commit automation runtime reconciliation: %w", err)
	}
	result, err := repo.getRun(ctx, item.PublicID, item.ProjectID, item.OwnerMattermostUserID)
	return result, true, err
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

func (repo *Repository) lockRunByRuntime(ctx context.Context, projectID int64, runtimeRunID string) (entity.ScheduledRun, error) {
	item, err := scanRun(repo.db.QueryRow(ctx, query("scheduled_runs__lock_runtime_run.sql"), projectID, runtimeRunID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ScheduledRun{}, automationsrepo.ErrNotFound
	}
	if err != nil {
		return entity.ScheduledRun{}, fmt.Errorf("lock scheduled run by runtime: %w", err)
	}
	return item, nil
}

type runtimeBinding struct {
	SessionID      int64
	SessionKey     string
	ProjectID      int64
	ChatID         int64
	RoleID         int64
	ActiveTurnID   int64
	ActiveRunID    string
	SessionStatus  string
	TurnID         int64
	TurnSessionID  int64
	TurnRunID      string
	TurnStatus     string
	TurnChannelID  string
	TurnRootPostID string
	TurnPostID     string
}

func (repo *Repository) lockRuntimeBinding(ctx context.Context, sessionID int64, turnID int64) (runtimeBinding, error) {
	var binding runtimeBinding
	err := repo.db.QueryRow(ctx, query("automation_runtime_binding__lock.sql"), sessionID, turnID).Scan(
		&binding.SessionID,
		&binding.SessionKey,
		&binding.ProjectID,
		&binding.ChatID,
		&binding.RoleID,
		&binding.ActiveTurnID,
		&binding.ActiveRunID,
		&binding.SessionStatus,
		&binding.TurnID,
		&binding.TurnSessionID,
		&binding.TurnRunID,
		&binding.TurnStatus,
		&binding.TurnChannelID,
		&binding.TurnRootPostID,
		&binding.TurnPostID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeBinding{}, automationsrepo.ErrForbidden
	}
	if err != nil {
		return runtimeBinding{}, fmt.Errorf("lock automation runtime binding: %w", err)
	}
	return binding, nil
}

func (repo *Repository) failRunLocked(ctx context.Context, item entity.ScheduledRun, safeSummary string, now time.Time, eventType string) error {
	if _, err := repo.db.Exec(ctx, query("scheduled_runs__fail.sql"), item.ID, safeSummary, now); err != nil {
		return fmt.Errorf("fail scheduled run: %w", err)
	}
	if _, err := repo.db.Exec(ctx, query("schedule_occurrences__status.sql"), item.OccurrenceID, string(value.AutomationRunStatusFailed), now); err != nil {
		return fmt.Errorf("fail schedule occurrence: %w", err)
	}
	if err := repo.insertAudit(ctx, item.ProjectID, item.ScheduleID, item.ID, eventType, "", "", item.CorrelationID, safeSummary); err != nil {
		return err
	}
	return nil
}

func automationRunIsTerminal(status string) bool {
	return status == string(value.AutomationRunStatusSucceeded) || status == string(value.AutomationRunStatusFailed)
}

func validateOwnerGatePlan(run entity.ScheduledRun, gateContext entity.AutomationOwnerGateContext, plan automationsrepo.OwnerGatePlanInput) error {
	if gateContext.ScheduledRunID != run.ID || gateContext.ScheduledRunPublicID != run.PublicID || gateContext.ProjectID != run.ProjectID || gateContext.RuntimeTurnID != run.RuntimeTurnID ||
		plan.ProcessRunID != gateContext.ProcessRunID || plan.PolicyRevisionID != gateContext.PolicyRevisionID || strings.TrimSpace(plan.RootInitiatorUserID) != gateContext.RootInitiatorUserID || strings.TrimSpace(plan.RootInitiatorName) != gateContext.RootInitiatorName {
		return automationsrepo.ErrForbidden
	}
	if strings.TrimSpace(plan.AttentionSummary) == "" || strings.TrimSpace(plan.AttentionRecommendation) == "" || !validOwnerAttentionDeliveryID(plan.DeliveryID) || !strings.Contains(plan.DeliveryMessage, "\n\n#notrigger") || len(plan.DeliveryPropsJSON) == 0 || len(plan.DeliveryPayloadSHA256) != sha256.Size {
		return automationsrepo.ErrForbidden
	}
	var props map[string]any
	if err := json.Unmarshal(plan.DeliveryPropsJSON, &props); err != nil {
		return automationsrepo.ErrForbidden
	}
	if props["matter_codex_event"] != "automation_owner_attention" || props["matter_codex_callback_delivery_id"] != plan.DeliveryID || props["matter_codex_automation_run_id"] != run.PublicID || props["matter_codex_process_run_id"] != gateContext.ProcessPublicID || props["matter_codex_human_decision_status"] != "pending" {
		return automationsrepo.ErrForbidden
	}
	payload := struct {
		ChannelID  string         `json:"channel_id"`
		RootPostID string         `json:"root_post_id"`
		Message    string         `json:"message"`
		Props      map[string]any `json:"props"`
	}{
		ChannelID:  gateContext.MattermostChannelID,
		RootPostID: gateContext.MattermostRootPostID,
		Message:    plan.DeliveryMessage,
		Props:      props,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return automationsrepo.ErrForbidden
	}
	digest := sha256.Sum256(encoded)
	if !bytes.Equal(digest[:], plan.DeliveryPayloadSHA256) {
		return automationsrepo.ErrForbidden
	}
	return nil
}

func validOwnerAttentionDeliveryID(deliveryID string) bool {
	if len(deliveryID) != 26 {
		return false
	}
	for _, char := range deliveryID {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
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

func scanOwnerAttentionDelivery(row pgx.Row) (entity.AutomationOwnerAttentionDelivery, error) {
	var item entity.AutomationOwnerAttentionDelivery
	var claimToken *string
	var claimedAt *time.Time
	var leaseExpiresAt *time.Time
	err := row.Scan(
		&item.AttentionID,
		&item.ScheduledRunID,
		&item.ScheduledRunPublicID,
		&item.ProcessRunID,
		&item.PolicyRevisionID,
		&item.RootInitiatorUserID,
		&item.MattermostChannelID,
		&item.MattermostRootPostID,
		&item.MattermostPostID,
		&item.Status,
		&item.DeliveryID,
		&item.DeliveryMessage,
		&item.DeliveryPropsJSON,
		&item.DeliveryPayloadSHA256,
		&claimToken,
		&claimedAt,
		&leaseExpiresAt,
		&item.ConfirmationPending,
		&item.Fence,
	)
	if claimToken != nil {
		item.ClaimToken = *claimToken
	}
	if claimedAt != nil {
		item.ClaimedAt = claimedAt.UTC()
	}
	if leaseExpiresAt != nil {
		item.LeaseExpiresAt = leaseExpiresAt.UTC()
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
