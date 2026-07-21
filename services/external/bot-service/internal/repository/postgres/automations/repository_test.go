//go:build postgres

package automations_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5"
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
		RunPublicID: "scheduled-run-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ScheduledFor: now, CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "automation-runtime-spoofed",
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
		RuntimeRunID:          "runtime-run-1",
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
	repository = postgresrepo.NewRepository(pool)
	replayedRun, replayCreated, err := repository.CreateManualRun(ctx, manualInput)
	if err != nil || replayCreated || replayedRun.PublicID != runPublicID || replayedRun.RuntimeRunID != manualInput.RuntimeRunID {
		t.Fatalf("restart после create run=%#v created=%t error=%v", replayedRun, replayCreated, err)
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
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-1", Now: now.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("RecordRunThread() error=%v", err)
	}
	repository = postgresrepo.NewRepository(pool)
	threadReplay, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-1", Now: now.Add(45 * time.Second),
	})
	if err != nil || threadReplay.MattermostRootPostID != "automation-session-1" {
		t.Fatalf("restart после publish checkpoint run=%#v error=%v", threadReplay, err)
	}
	adminRepository := adminpostgres.NewRepository(pool)
	replayedTurn, err := adminRepository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: sessionID, RunID: "runtime-run-1", MattermostChannelID: "automation-channel",
		MattermostRootPostID: "automation-session-1", MattermostPostID: "automation-session-1", Message: "saved playbook",
	})
	if err != nil || replayedTurn.ID != turnID || replayedTurn.Status != "running" {
		t.Fatalf("restart после enqueue turn=%#v error=%v", replayedTurn, err)
	}
	if _, err := adminpostgres.NewRepository(pool).CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: sessionID, RunID: "runtime-run-1", MattermostChannelID: "automation-channel",
		MattermostRootPostID: "automation-session-1", MattermostPostID: "automation-session-1", Message: "changed playbook",
	}); err == nil {
		t.Fatal("изменённый replay enqueue принят")
	}
	if _, err := adminRepository.CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID: "runtime-run-1", ProfileName: "worker", Role: "worker", Provider: "github",
		Owner: "example", Name: "repository", BaseBranch: "main", HeadBranch: "automation", Status: "failed",
	}); err != nil {
		t.Fatalf("создать terminal agent run: %v", err)
	}
	agentRunReplay, err := adminpostgres.NewRepository(pool).CreateAgentRun(ctx, adminrepo.CreateAgentRunInput{
		RunID: "runtime-run-1", ProfileName: "worker", Role: "worker", Provider: "github",
		Owner: "example", Name: "repository", BaseBranch: "main", HeadBranch: "automation", Status: "queued",
	})
	if err != nil || agentRunReplay.Status != "failed" {
		t.Fatalf("restart enqueue понизил terminal agent run: run=%#v error=%v", agentRunReplay, err)
	}
	bound, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "automation-session-1", RuntimeTurnID: turnID, RuntimeRunID: "runtime-run-1",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-1", Now: now.Add(time.Minute),
	})
	if err != nil || bound.Status != string(value.AutomationRunStatusRunning) {
		t.Fatalf("BindRun() bound=%#v error=%v", bound, err)
	}
	repository = postgresrepo.NewRepository(pool)
	boundReplay, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "automation-session-1", RuntimeTurnID: turnID, RuntimeRunID: "runtime-run-1",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-1", Now: now.Add(90 * time.Second),
	})
	if err != nil || boundReplay.ID != bound.ID || boundReplay.Status != string(value.AutomationRunStatusRunning) {
		t.Fatalf("restart после bind run=%#v error=%v", boundReplay, err)
	}
	callback := automationsrepo.CompleteCallbackInput{
		RunPublicID: runPublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: "automation-session-1",
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
	var nextTurnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, 'runtime-run-next', 'automation-channel', 'automation-session-1', 'next-post', 'next turn', 'running') returning id`, sessionID).Scan(&nextTurnID); err != nil {
		t.Fatalf("создать следующий runtime turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = 'runtime-run-next' where id = $1`, sessionID, nextTurnID); err != nil {
		t.Fatalf("переключить active turn: %v", err)
	}
	if err := repository.RevokeCallback(ctx, runPublicID, projectID, now.Add(90*time.Minute)); err != nil {
		t.Fatalf("revoke accepted callback: %v", err)
	}
	expiredReplay := callback
	expiredReplay.Now = now.Add(2 * time.Hour)
	if replayed, duplicate, err := repository.CompleteCallback(ctx, expiredReplay); err != nil || !duplicate || replayed.ID != completed.ID {
		t.Fatalf("terminal replay after next turn/expiry/revoke run=%#v duplicate=%t error=%v", replayed, duplicate, err)
	}
	mismatch := callback
	mismatch.PayloadSHA256 = bytes.Repeat([]byte{0x55}, 32)
	if _, _, err := repository.CompleteCallback(ctx, mismatch); !errors.Is(err, automationsrepo.ErrCallbackMismatch) {
		t.Fatalf("изменённый callback replay error=%v", err)
	}
	crossProject := callback
	crossProject.AuthenticatedProjectID = otherProjectID
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
		ScheduledFor: now.Add(3 * time.Minute), CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "runtime-run-2",
	})
	if err != nil || !created {
		t.Fatalf("создать run для revoke: created=%t error=%v", created, err)
	}
	revokedSessionID, revokedTurnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, "automation-session-2", "runtime-run-2", now.Add(time.Hour))
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: revokedRun.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-2", Now: now.Add(3*time.Minute + 30*time.Second),
	}); err != nil {
		t.Fatalf("record revoked run thread: %v", err)
	}
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: revokedRun.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: revokedSessionID, RuntimeSessionKey: "automation-session-2", RuntimeTurnID: revokedTurnID, RuntimeRunID: "runtime-run-2",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-2", Now: now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("bind revoked run: %v", err)
	}
	if err := repository.RevokeCallback(ctx, revokedRun.PublicID, projectID, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("RevokeCallback() error=%v", err)
	}
	if _, _, err := repository.CompleteCallback(ctx, automationsrepo.CompleteCallbackInput{
		RunPublicID: revokedRun.PublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: revokedSessionID, AuthenticatedSessionKey: "automation-session-2",
		CallbackContractVersion: value.AutomationCallbackContractV1, Status: string(value.AutomationRunStatusSucceeded), Outcome: string(value.AutomationRunOutcomeNoAction),
		SafeSummary: "replayed", PayloadSHA256: bytes.Repeat([]byte{0x66}, 32), Now: now.Add(6 * time.Minute),
	}); !errors.Is(err, automationsrepo.ErrCallbackRevoked) {
		t.Fatalf("отозванный callback error=%v", err)
	}

	failureRun, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id", IdempotencyKey: "runtime-failure-command",
		OccurrencePublicID: "occurrence-44444444444444444444444444444444", RunPublicID: "scheduled-run-44444444444444444444444444444444",
		ScheduledFor: now.Add(7 * time.Minute), CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "runtime-run-failure",
	})
	if err != nil || !created {
		t.Fatalf("создать run для runtime failure: created=%t error=%v", created, err)
	}
	failureSessionID, failureTurnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, "automation-session-failure", "runtime-run-failure", now.Add(time.Hour))
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: failureRun.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-failure", Now: now.Add(8 * time.Minute),
	}); err != nil {
		t.Fatalf("record failure run thread: %v", err)
	}
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: failureRun.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: failureSessionID, RuntimeSessionKey: "automation-session-failure", RuntimeTurnID: failureTurnID, RuntimeRunID: "runtime-run-failure",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "automation-session-failure", Now: now.Add(9 * time.Minute),
	}); err != nil {
		t.Fatalf("bind failure run: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_session_turns set status = 'failed', finished_at = now() where id = $1`, failureTurnID); err != nil {
		t.Fatalf("пометить runtime turn failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set status = 'error', active_turn_id = null, active_run_id = '' where id = $1`, failureSessionID); err != nil {
		t.Fatalf("очистить runtime binding после failure: %v", err)
	}
	reconciled, changed, err := repository.ReconcileRuntimeTerminal(ctx, automationsrepo.ReconcileRuntimeTerminalInput{
		ProjectID: projectID, RuntimeSessionID: failureSessionID, RuntimeTurnID: failureTurnID, RuntimeRunID: "runtime-run-failure",
		RuntimeStatus: "failed", SafeSummary: "Среда выполнения автоматизации завершилась ошибкой.", Now: now.Add(10 * time.Minute),
	})
	if err != nil || !changed || reconciled.Status != string(value.AutomationRunStatusFailed) {
		t.Fatalf("runtime terminal reconciliation run=%#v changed=%t error=%v", reconciled, changed, err)
	}
	restartedRepository := postgresrepo.NewRepository(pool)
	replayedFailure, changed, err := restartedRepository.ReconcileRuntimeTerminal(ctx, automationsrepo.ReconcileRuntimeTerminalInput{
		ProjectID: projectID, RuntimeSessionID: failureSessionID, RuntimeTurnID: failureTurnID, RuntimeRunID: "runtime-run-failure",
		RuntimeStatus: "failed", SafeSummary: "Среда выполнения автоматизации завершилась ошибкой.", Now: now.Add(11 * time.Minute),
	})
	if err != nil || changed || replayedFailure.ID != reconciled.ID {
		t.Fatalf("runtime terminal replay run=%#v changed=%t error=%v", replayedFailure, changed, err)
	}
	history, err := restartedRepository.ListRuns(ctx, schedule.PublicID, projectID, "owner-user-id", 20)
	if err != nil || len(history) != 3 || history[0].PublicID != failureRun.PublicID {
		t.Fatalf("история после нового repository instance: %#v error=%v", history, err)
	}
}

func TestAutomationCallbackConcurrentRuntimeFences(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_callback_fences")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("применить миграции: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть пул: %v", err)
	}
	defer pool.Close()
	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Fence project', 'fence-project') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'fence-channel', 'Fence chat', 'fence-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	repository := postgresrepo.NewRepository(pool)
	now := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	schedule, _, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-99999999999999999999999999999999", ProjectID: projectID, TargetAgentRoleID: roleID, TargetChatID: chatID,
		Name: "Fence schedule", OwnerMattermostUserID: "owner-id", OwnerMattermostUserName: "owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC", NextRunAt: now.Add(time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1, PromptSnapshot: "saved",
		PromptSHA256: bytes.Repeat([]byte{0x71}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "fence-schedule", CommandHash: bytes.Repeat([]byte{0x72}, 32), Now: now,
	})
	if err != nil {
		t.Fatalf("создать расписание: %v", err)
	}

	tests := []struct {
		name      string
		lockSQL   string
		mutateSQL string
		target    string
	}{
		{name: "CompleteTurn", lockSQL: `select id from matter_codex_agent_session_turns where id = $1 for update`, mutateSQL: `update matter_codex_agent_session_turns set status = 'failed', finished_at = now() where id = $1`, target: "turn"},
		{name: "active binding cleanup", lockSQL: `select id from matter_codex_agent_sessions where id = $1 for update`, mutateSQL: `update matter_codex_agent_sessions set active_turn_id = null, active_run_id = '' where id = $1`, target: "session"},
		{name: "next claim", lockSQL: `select id from matter_codex_agent_sessions where id = $1 for update`, mutateSQL: `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = 'runtime-next' where id = $1`, target: "session"},
		{name: "runtime remap", lockSQL: `select id from matter_codex_agent_session_turns where id = $1 for update`, mutateSQL: `update matter_codex_agent_session_turns set run_id = run_id || '-remapped' where id = $1`, target: "turn"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%032x", index+1)
			runtimeRunID := fmt.Sprintf("runtime-fence-%d", index+1)
			sessionKey := fmt.Sprintf("automation-fence-session-%d", index+1)
			run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
				SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-id", IdempotencyKey: "fence-" + suffix,
				OccurrencePublicID: "occurrence-" + suffix, RunPublicID: "scheduled-run-" + suffix,
				ScheduledFor: now.Add(time.Duration(index+1) * time.Minute), CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: runtimeRunID,
			})
			if err != nil || !created {
				t.Fatalf("создать run: created=%t error=%v", created, err)
			}
			sessionID, turnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, sessionKey, runtimeRunID, now.Add(time.Hour))
			if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
				RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-id",
				MattermostChannelID: "automation-channel", MattermostRootPostID: sessionKey, Now: now,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
				RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-id",
				RuntimeSessionID: sessionID, RuntimeSessionKey: sessionKey, RuntimeTurnID: turnID, RuntimeRunID: runtimeRunID,
				MattermostChannelID: "automation-channel", MattermostRootPostID: sessionKey, Now: now,
			}); err != nil {
				t.Fatal(err)
			}
			var nextTurnID int64
			if test.name == "next claim" {
				if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
					session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
				) values ($1, 'runtime-next', 'automation-channel', $2, 'next-post', 'next turn', 'running') returning id`, sessionID, sessionKey).Scan(&nextTurnID); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			targetID := turnID
			if test.target == "session" {
				targetID = sessionID
			}
			if _, err := tx.Exec(ctx, test.lockSQL, targetID); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			type callbackResult struct{ err error }
			result := make(chan callbackResult, 1)
			go func() {
				_, _, callbackErr := repository.CompleteCallback(ctx, automationsrepo.CompleteCallbackInput{
					RunPublicID: run.PublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: sessionKey,
					CallbackContractVersion: value.AutomationCallbackContractV1, Status: string(value.AutomationRunStatusSucceeded), Outcome: string(value.AutomationRunOutcomeNoAction),
					SafeSummary: "Серверное резюме", PayloadSHA256: bytes.Repeat([]byte{byte(index + 1)}, 32), Now: now.Add(time.Minute),
				})
				result <- callbackResult{err: callbackErr}
			}()
			var mutateErr error
			if test.name == "next claim" {
				_, mutateErr = tx.Exec(ctx, test.mutateSQL, targetID, nextTurnID)
			} else {
				_, mutateErr = tx.Exec(ctx, test.mutateSQL, targetID)
			}
			if mutateErr != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(mutateErr)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case callback := <-result:
				if !errors.Is(callback.err, automationsrepo.ErrForbidden) {
					t.Fatalf("конкурентный callback error=%v", callback.err)
				}
			case <-ctx.Done():
				t.Fatalf("конкурентный callback не завершился: %v", ctx.Err())
			}
			persisted, err := repository.GetRun(ctx, run.PublicID, projectID, "owner-id")
			if err != nil || persisted.Status != string(value.AutomationRunStatusRunning) {
				t.Fatalf("callback прошёл fence: run=%#v error=%v", persisted, err)
			}
		})
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
