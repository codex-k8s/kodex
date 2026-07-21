//go:build postgres

package automations_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutomationRepositoryMigrationAndIdempotency(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automations")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 29); err != nil {
		t.Fatalf("применить миграции N-1: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть пул: %v", err)
	}
	defer pool.Close()

	var projectID, otherProjectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Automation project', 'automation-project') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("создать проект: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Other project', 'other-project') returning id`).Scan(&otherProjectID); err != nil {
		t.Fatalf("создать другой проект: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("создать роль: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'automation-channel', 'Automation chat', 'automation-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("создать чат: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id, enabled) values ($1, $2, true)`, chatID, roleID); err != nil {
		t.Fatalf("создать участника чата: %v", err)
	}
	var projectCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_projects where id in ($1, $2)`, projectID, otherProjectID).Scan(&projectCount); err != nil || projectCount != 2 {
		t.Fatalf("данные N-1 не читаются: count=%d error=%v", projectCount, err)
	}

	if err := migrations.RunTo(ctx, dsn, 30); err != nil {
		t.Fatalf("применить миграцию 30 поверх N-1: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 30 {
		t.Fatalf("версия после N-1 upgrade = %d, error=%v", version, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_projects where id in ($1, $2)`, projectID, otherProjectID).Scan(&projectCount); err != nil || projectCount != 2 {
		t.Fatalf("миграция повредила данные N-1: count=%d error=%v", projectCount, err)
	}

	repository := postgresrepo.NewRepository(pool)
	now := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	nextRunAt := now.Add(23 * time.Hour)
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID:                "schedule-11111111111111111111111111111111",
		ProjectID:               projectID,
		TargetAgentRoleID:       roleID,
		TargetChatID:            chatID,
		Name:                    "Daily check",
		OwnerMattermostUserID:   "owner-user-id",
		OwnerMattermostUserName: "owner",
		Preset:                  string(value.AutomationSchedulePresetDaily),
		LocalTime:               "09:00",
		TimeZone:                "UTC",
		NextRunAt:               nextRunAt,
		PlaybookKey:             value.AutomationPlaybookProjectCheckV1,
		PromptVersion:           value.AutomationPromptVersionV1,
		PromptSnapshot:          "saved playbook",
		PromptSHA256:            bytes.Repeat([]byte{0x11}, 32),
		CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey:          "create-command",
		CommandHash:             bytes.Repeat([]byte{0x22}, 32),
		Now:                     now,
	})
	if err != nil || !created {
		t.Fatalf("CreateSchedule() schedule=%#v created=%t error=%v", schedule, created, err)
	}
	if _, _, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-22222222222222222222222222222222", ProjectID: projectID, TargetAgentRoleID: roleID, TargetChatID: chatID,
		Name: "Changed command", OwnerMattermostUserID: "owner-user-id", OwnerMattermostUserName: "owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC", NextRunAt: nextRunAt,
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: "saved playbook", PromptSHA256: bytes.Repeat([]byte{0x11}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "create-command", CommandHash: bytes.Repeat([]byte{0x33}, 32), Now: now,
	}); !errors.Is(err, automationsrepo.ErrConflict) {
		t.Fatalf("изменённый replay создания error=%v, ожидался ErrConflict", err)
	}
	if _, err := repository.GetSchedule(ctx, schedule.PublicID, projectID, "spoofed-owner"); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("подмена владельца schedule error=%v", err)
	}
	if _, _, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: otherProjectID, OwnerMattermostUserID: "owner-user-id",
		IdempotencyKey: "spoofed-project", OccurrencePublicID: "occurrence-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RunPublicID: "scheduled-run-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ScheduledFor: now, CallbackExpiresAt: now.Add(time.Hour),
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("подмена project при Run Now error=%v", err)
	}

	manualInput := automationsrepo.CreateManualRunInput{
		SchedulePublicID:      schedule.PublicID,
		ProjectID:             projectID,
		OwnerMattermostUserID: "owner-user-id",
		IdempotencyKey:        "run-now-command",
		OccurrencePublicID:    "occurrence-11111111111111111111111111111111",
		RunPublicID:           "scheduled-run-11111111111111111111111111111111",
		ScheduledFor:          now,
		CallbackExpiresAt:     now.Add(time.Hour),
	}
	type createResult struct {
		publicID string
		created  bool
		err      error
	}
	start := make(chan struct{})
	results := make(chan createResult, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			input := manualInput
			if index == 1 {
				input.OccurrencePublicID = "occurrence-22222222222222222222222222222222"
				input.RunPublicID = "scheduled-run-22222222222222222222222222222222"
			}
			run, wasCreated, createErr := repository.CreateManualRun(ctx, input)
			results <- createResult{publicID: run.PublicID, created: wasCreated, err: createErr}
		}()
	}
	close(start)
	group.Wait()
	close(results)
	createdCount := 0
	runPublicID := ""
	for result := range results {
		if result.err != nil {
			t.Fatalf("конкурентный Run Now error=%v", result.err)
		}
		if result.created {
			createdCount++
		}
		if runPublicID == "" {
			runPublicID = result.publicID
		} else if result.publicID != runPublicID {
			t.Fatalf("конкурентный Run Now создал разные runs: %s и %s", runPublicID, result.publicID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("число созданных конкурентных runs = %d, ожидалось 1", createdCount)
	}
	if _, err := repository.GetRun(ctx, runPublicID, projectID, "spoofed-owner"); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("подмена владельца run error=%v", err)
	}
	var occurrenceCount, runCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_schedule_occurrences where schedule_id = $1`, schedule.ID).Scan(&occurrenceCount); err != nil || occurrenceCount != 1 {
		t.Fatalf("occurrence count=%d error=%v", occurrenceCount, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_scheduled_runs where schedule_id = $1`, schedule.ID).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("run count=%d error=%v", runCount, err)
	}
	var persistedNextRun time.Time
	if err := pool.QueryRow(ctx, `select next_run_at from matter_codex_automation_schedules where id = $1`, schedule.ID).Scan(&persistedNextRun); err != nil || !persistedNextRun.Equal(nextRunAt) {
		t.Fatalf("Run Now изменил next_run_at: got=%s want=%s error=%v", persistedNextRun, nextRunAt, err)
	}

	sessionID, turnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, "automation-session-1", "runtime-run-1", now.Add(time.Hour))
	bound, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "automation-session-1", RuntimeTurnID: turnID, RuntimeRunID: "runtime-run-1",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-root-1", Now: now.Add(time.Minute),
	})
	if err != nil || bound.Status != string(value.AutomationRunStatusRunning) {
		t.Fatalf("BindRun() bound=%#v error=%v", bound, err)
	}
	callback := automationsrepo.CompleteCallbackInput{
		RunPublicID: runPublicID, ProjectID: projectID, RuntimeSessionID: sessionID, RuntimeTurnID: turnID, RuntimeRunID: "runtime-run-1",
		CallbackContractVersion: value.AutomationCallbackContractV1, Status: string(value.AutomationRunStatusSucceeded), Outcome: string(value.AutomationRunOutcomeNoAction),
		SafeSummary: "Проверка завершена, действий не требуется", PayloadSHA256: bytes.Repeat([]byte{0x44}, 32), Now: now.Add(2 * time.Minute),
	}
	completed, duplicate, err := repository.CompleteCallback(ctx, callback)
	if err != nil || duplicate || completed.Status != string(value.AutomationRunStatusSucceeded) {
		t.Fatalf("CompleteCallback() run=%#v duplicate=%t error=%v", completed, duplicate, err)
	}
	replayed, duplicate, err := repository.CompleteCallback(ctx, callback)
	if err != nil || !duplicate || replayed.ID != completed.ID {
		t.Fatalf("duplicate callback run=%#v duplicate=%t error=%v", replayed, duplicate, err)
	}
	mismatch := callback
	mismatch.PayloadSHA256 = bytes.Repeat([]byte{0x55}, 32)
	if _, _, err := repository.CompleteCallback(ctx, mismatch); !errors.Is(err, automationsrepo.ErrCallbackMismatch) {
		t.Fatalf("изменённый callback replay error=%v", err)
	}
	crossProject := callback
	crossProject.ProjectID = otherProjectID
	if _, _, err := repository.CompleteCallback(ctx, crossProject); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("межпроектный callback error=%v", err)
	}
	var completionAudits int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events where scheduled_run_id = $1 and event_type = 'run.completed'`, completed.ID).Scan(&completionAudits); err != nil || completionAudits != 1 {
		t.Fatalf("completion audit count=%d error=%v", completionAudits, err)
	}

	revokedRun, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id", IdempotencyKey: "revoked-run-command",
		OccurrencePublicID: "occurrence-33333333333333333333333333333333", RunPublicID: "scheduled-run-33333333333333333333333333333333",
		ScheduledFor: now.Add(3 * time.Minute), CallbackExpiresAt: now.Add(time.Hour),
	})
	if err != nil || !created {
		t.Fatalf("создать run для revoke: created=%t error=%v", created, err)
	}
	revokedSessionID, revokedTurnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, "automation-session-2", "runtime-run-2", now.Add(time.Hour))
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: revokedRun.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: revokedSessionID, RuntimeSessionKey: "automation-session-2", RuntimeTurnID: revokedTurnID, RuntimeRunID: "runtime-run-2",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-root-2", Now: now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("bind revoked run: %v", err)
	}
	if err := repository.RevokeCallback(ctx, revokedRun.PublicID, projectID, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("RevokeCallback() error=%v", err)
	}
	if _, _, err := repository.CompleteCallback(ctx, automationsrepo.CompleteCallbackInput{
		RunPublicID: revokedRun.PublicID, ProjectID: projectID, RuntimeSessionID: revokedSessionID, RuntimeTurnID: revokedTurnID, RuntimeRunID: "runtime-run-2",
		CallbackContractVersion: value.AutomationCallbackContractV1, Status: string(value.AutomationRunStatusSucceeded), Outcome: string(value.AutomationRunOutcomeNoAction),
		SafeSummary: "replayed", PayloadSHA256: bytes.Repeat([]byte{0x66}, 32), Now: now.Add(6 * time.Minute),
	}); !errors.Is(err, automationsrepo.ErrCallbackRevoked) {
		t.Fatalf("отозванный callback error=%v", err)
	}
	restartedRepository := postgresrepo.NewRepository(pool)
	history, err := restartedRepository.ListRuns(ctx, schedule.PublicID, projectID, "owner-user-id", 20)
	if err != nil || len(history) != 2 || history[0].PublicID != revokedRun.PublicID {
		t.Fatalf("история после нового repository instance: %#v error=%v", history, err)
	}
}

func createRuntimeBinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64, roleID int64, chatID int64, sessionKey string, runID string, expiresAt time.Time) (int64, int64) {
	t.Helper()
	var sessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id, mattermost_root_post_id,
		status, ttl_seconds, expires_at
	) values ($1, $2, $3, $4, 'automation-run', 'automation-channel', $1, 'running', 3600, $5) returning id`, sessionKey, projectID, chatID, roleID, expiresAt).Scan(&sessionID); err != nil {
		t.Fatalf("создать runtime session: %v", err)
	}
	var turnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, $2, 'automation-channel', $3, $3, 'saved playbook', 'running') returning id`, sessionID, runID, sessionKey).Scan(&turnID); err != nil {
		t.Fatalf("создать runtime turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = $3 where id = $1`, sessionID, turnID, runID); err != nil {
		t.Fatalf("привязать runtime turn: %v", err)
	}
	return sessionID, turnID
}
