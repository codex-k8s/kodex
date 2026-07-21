//go:build postgres

package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	automationspostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

type automationStopPostgresFixture struct {
	pool              *pgxpool.Pool
	service           *AgentSessionService
	publisher         *fakeThreadPublisher
	projectID         int64
	runPublicID       string
	runtimeTurnID     int64
	ownerMattermostID string
	processRunID      int64
}

func TestAgentSessionStopPersistsAutomationTerminalHistoryPostgres(t *testing.T) {
	for _, status := range []string{agentSessionTurnRunning, agentSessionTurnQueued} {
		t.Run(status, func(t *testing.T) {
			fixture := newAutomationStopPostgresFixture(t, status, true)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := StopAgentSessionTurnsCommand{TurnIDs: []int64{fixture.runtimeTurnID}, UserID: "owner-user", UserName: "owner"}

			if _, err := fixture.service.StopAgentSessionTurns(ctx, command); err != nil {
				t.Fatalf("StopAgentSessionTurns(%s) error=%v", status, err)
			}
			assertAutomationStopPostgresState(t, ctx, fixture)
			cardCount := len(fixture.publisher.cards) + len(fixture.publisher.cardUpdates)

			fixture.service = newAutomationStopPostgresService(fixture.pool, fixture.publisher)
			if _, err := fixture.service.StopAgentSessionTurns(ctx, command); err != nil {
				t.Fatalf("restarted reconcile-only StopAgentSessionTurns(%s) error=%v", status, err)
			}
			assertAutomationStopPostgresState(t, ctx, fixture)
			if got := len(fixture.publisher.cards) + len(fixture.publisher.cardUpdates); got != cardCount {
				t.Fatalf("reconcile-only retry duplicated status card: got=%d want=%d", got, cardCount)
			}
		})
	}
}

func TestAgentSessionStopKeepsNonAutomationBehaviorWithReconcilerPostgres(t *testing.T) {
	fixture := newAutomationStopPostgresFixture(t, agentSessionTurnRunning, false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := StopAgentSessionTurnsCommand{TurnIDs: []int64{fixture.runtimeTurnID}, UserID: "owner-user", UserName: "owner"}

	if _, err := fixture.service.StopAgentSessionTurns(ctx, command); err != nil {
		t.Fatalf("non-automation StopAgentSessionTurns() error=%v", err)
	}
	turn, err := adminpostgres.NewRepository(fixture.pool).GetAgentSessionTurn(ctx, fixture.runtimeTurnID)
	if err != nil || turn.Status != agentSessionTurnCanceled {
		t.Fatalf("non-automation turn=%#v error=%v", turn, err)
	}
	var runCount, auditCount int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_scheduled_runs`).Scan(&runCount); err != nil {
		t.Fatalf("count non-automation scheduled runs: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events`).Scan(&auditCount); err != nil {
		t.Fatalf("count non-automation audit events: %v", err)
	}
	if runCount != 0 || auditCount != 0 {
		t.Fatalf("non-automation stop created automation state: runs=%d audit=%d", runCount, auditCount)
	}
}

func newAutomationStopPostgresFixture(t *testing.T, turnStatus string, withAutomation bool) automationStopPostgresFixture {
	t.Helper()
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_stop_"+turnStatus)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("применить миграции: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Automation stop project', 'automation-stop-project') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("создать проект: %v", err)
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

	runtimeRunID := "runtime-run-stop-" + turnStatus
	sessionKey := "automation-stop-" + turnStatus
	sessionStatus := agentSessionStatusIdle
	if turnStatus == agentSessionTurnRunning {
		sessionStatus = agentSessionStatusRunning
	}
	var sessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
		mattermost_root_post_id, status, ttl_seconds, expires_at
	) values ($1, $2, $3, $4, 'automation-run', 'automation-channel', $1, $5, 3600, now() + interval '1 hour')
	returning id`, sessionKey, projectID, chatID, roleID, sessionStatus).Scan(&sessionID); err != nil {
		t.Fatalf("создать runtime session: %v", err)
	}
	var turnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, $2, 'automation-channel', $3, $3, 'saved playbook', $4) returning id`, sessionID, runtimeRunID, sessionKey, turnStatus).Scan(&turnID); err != nil {
		t.Fatalf("создать runtime turn: %v", err)
	}
	if turnStatus == agentSessionTurnRunning {
		if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = $3 where id = $1`, sessionID, turnID, runtimeRunID); err != nil {
			t.Fatalf("привязать running runtime turn: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_runs(run_id, profile_name, role, provider, owner, name, head_branch, status)
		values ($1, 'worker', 'worker', 'github', 'example', 'repository', 'automation-stop', $2)`, runtimeRunID, turnStatus); err != nil {
		t.Fatalf("создать agent run: %v", err)
	}
	var policyRevisionID, processRunID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_policy_revisions(project_id, version, status, activated_at) values ($1, 1, 'active', now()) returning id`, projectID).Scan(&policyRevisionID); err != nil {
		t.Fatalf("создать policy revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_process_runs(
		public_id, project_id, policy_revision_id, root_role_id, root_initiator_user_id,
		root_initiator_user_name, root_trigger_post_id, root_channel_id, root_thread_post_id, status
	) values ($1, $2, $3, $4, 'owner-user', 'owner', $5, 'automation-channel', $5, 'running') returning id`,
		"automation-stop-process-"+turnStatus, projectID, policyRevisionID, roleID, sessionKey).Scan(&processRunID); err != nil {
		t.Fatalf("создать process run: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_process_turns(turn_id, process_run_id, launch_post_id) values ($1, $2, $3)`, turnID, processRunID, sessionKey); err != nil {
		t.Fatalf("связать process turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_work_claims(process_run_id, turn_id, role_id, summary, status) values ($1, $2, $3, 'automation stop proof', 'active')`, processRunID, turnID, roleID); err != nil {
		t.Fatalf("создать work claim: %v", err)
	}

	fixture := automationStopPostgresFixture{
		pool: pool, publisher: &fakeThreadPublisher{}, projectID: projectID,
		runtimeTurnID: turnID, ownerMattermostID: "owner-user-id", processRunID: processRunID,
	}
	if withAutomation {
		fixture.runPublicID = seedAutomationStopRun(t, ctx, pool, projectID, roleID, chatID, sessionID, turnID, sessionKey, runtimeRunID)
	}
	fixture.service = newAutomationStopPostgresService(pool, fixture.publisher)
	return fixture
}

func seedAutomationStopRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64, roleID int64, chatID int64, sessionID int64, turnID int64, sessionKey string, runtimeRunID string) string {
	t.Helper()
	repository := automationspostgres.NewRepository(pool)
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	schedulePublicID := "schedule-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: schedulePublicID, ProjectID: projectID, TargetAgentRoleID: roleID, TargetChatID: chatID,
		Name: "Stop reconciliation", OwnerMattermostUserID: "owner-user-id", OwnerMattermostUserName: "owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "12:00", TimeZone: "UTC", NextRunAt: now.Add(24 * time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: "saved playbook", PromptSHA256: bytes.Repeat([]byte{0x11}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "create-stop-schedule", CommandHash: bytes.Repeat([]byte{0x22}, 32), Now: now,
	})
	if err != nil || !created {
		t.Fatalf("создать automation schedule: schedule=%#v created=%t error=%v", schedule, created, err)
	}
	runPublicID := "scheduled-run-cccccccccccccccccccccccccccccccc"
	run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedulePublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		IdempotencyKey: "stop-runtime-command", OccurrencePublicID: "occurrence-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RunPublicID: runPublicID, ScheduledFor: now.Add(time.Minute), CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: runtimeRunID,
	})
	if err != nil || !created {
		t.Fatalf("создать automation run: run=%#v created=%t error=%v", run, created, err)
	}
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: sessionKey, Now: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("сохранить automation thread: %v", err)
	}
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: runPublicID, ProjectID: projectID, OwnerMattermostUserID: "owner-user-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: sessionKey, RuntimeTurnID: turnID, RuntimeRunID: runtimeRunID,
		MattermostChannelID: "automation-channel", MattermostRootPostID: sessionKey, Now: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("привязать automation run: %v", err)
	}
	return runPublicID
}

func newAutomationStopPostgresService(pool *pgxpool.Pool, publisher *fakeThreadPublisher) *AgentSessionService {
	adminRepository := adminpostgres.NewRepository(pool)
	automationService := NewAutomationService(AutomationServiceConfig{
		Repository: automationspostgres.NewRepository(pool), Catalog: adminRepository, StorageReady: true,
		Now: func() time.Time { return time.Date(2026, time.July, 21, 12, 10, 0, 0, time.UTC) },
	})
	return NewAgentSessionService(AgentSessionServiceConfig{
		Store: adminRepository, ThreadPublisher: publisher,
		AutomationRuntimeReconciler: automationService, StorageReady: true, RuntimeReady: true,
	})
}

func assertAutomationStopPostgresState(t *testing.T, ctx context.Context, fixture automationStopPostgresFixture) {
	t.Helper()
	turn, err := adminpostgres.NewRepository(fixture.pool).GetAgentSessionTurn(ctx, fixture.runtimeTurnID)
	if err != nil || turn.Status != agentSessionTurnCanceled {
		t.Fatalf("runtime turn=%#v error=%v", turn, err)
	}
	run, err := automationspostgres.NewRepository(fixture.pool).GetRun(ctx, fixture.runPublicID, fixture.projectID, fixture.ownerMattermostID)
	if err != nil {
		t.Fatalf("прочитать scheduled run: %v", err)
	}
	if run.Status != string(value.AutomationRunStatusFailed) || run.Outcome != string(value.AutomationRunOutcomeFailed) || run.SafeSummary != automationRuntimeTerminalSummary(agentSessionTurnCanceled) {
		t.Fatalf("scheduled run terminal state=%#v", run)
	}
	var occurrenceStatus string
	if err := fixture.pool.QueryRow(ctx, `select status from matter_codex_schedule_occurrences where id = $1`, run.OccurrenceID).Scan(&occurrenceStatus); err != nil {
		t.Fatalf("прочитать occurrence status: %v", err)
	}
	if occurrenceStatus != string(value.AutomationRunStatusFailed) {
		t.Fatalf("occurrence status=%q", occurrenceStatus)
	}
	var processStatus, workStatus string
	if err := fixture.pool.QueryRow(ctx, `select process.status, claim.status from matter_codex_process_runs process join matter_codex_work_claims claim on claim.process_run_id = process.id where process.id = $1`, fixture.processRunID).Scan(&processStatus, &workStatus); err != nil {
		t.Fatalf("прочитать ProcessRun reconciliation: %v", err)
	}
	if processStatus != "completed" || workStatus != agentSessionTurnCanceled {
		t.Fatalf("ProcessRun reconciliation process=%q work=%q", processStatus, workStatus)
	}
	var runCount, occurrenceCount, terminalAuditCount int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_scheduled_runs where schedule_id = $1`, run.ScheduleID).Scan(&runCount); err != nil {
		t.Fatalf("посчитать scheduled runs: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_schedule_occurrences where schedule_id = $1`, run.ScheduleID).Scan(&occurrenceCount); err != nil {
		t.Fatalf("посчитать occurrences: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events where scheduled_run_id = $1 and event_type = 'run.runtime_terminal'`, run.ID).Scan(&terminalAuditCount); err != nil {
		t.Fatalf("посчитать terminal audit: %v", err)
	}
	if runCount != 1 || occurrenceCount != 1 || terminalAuditCount != 1 {
		t.Fatalf("terminal history duplicates: runs=%d occurrences=%d terminal_audit=%d", runCount, occurrenceCount, terminalAuditCount)
	}
}
