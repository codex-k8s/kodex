//go:build postgres

package automations_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutomationOwnerGateDurabilityAndBoundaries(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_owner_gate")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 34); err != nil {
		t.Fatalf("применить миграции до 34: %v", err)
	}
	if err := migrations.RunTo(ctx, dsn, 37); err != nil {
		t.Fatalf("применить миграцию 37: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 37 {
		t.Fatalf("версия миграций=%d error=%v", version, err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть пул: %v", err)
	}
	defer pool.Close()

	var projectID, otherProjectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Owner gate project', 'owner-gate-project') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Other owner gate project', 'other-owner-gate-project') returning id`).Scan(&otherProjectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'automation-channel', 'Owner gate chat', 'owner-gate-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	repository := postgresrepo.NewRepository(pool)
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-77777777777777777777777777777777", ProjectID: projectID, TargetAgentRoleID: roleID, TargetChatID: chatID,
		Name: "Owner gate schedule", OwnerMattermostUserID: "schedule-owner-id", OwnerMattermostUserName: "schedule-owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC", NextRunAt: now.Add(time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1, PromptSnapshot: "saved",
		PromptSHA256: bytes.Repeat([]byte{0x31}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "owner-gate-schedule", CommandHash: bytes.Repeat([]byte{0x32}, 32), Now: now,
	})
	if err != nil || !created {
		t.Fatalf("создать расписание: created=%t error=%v", created, err)
	}
	run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		IdempotencyKey: "owner-gate-run", OccurrencePublicID: "occurrence-77777777777777777777777777777777",
		RunPublicID: "scheduled-run-77777777777777777777777777777777", ScheduledFor: now,
		CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "runtime-owner-gate",
	})
	if err != nil || !created {
		t.Fatalf("создать запуск: created=%t error=%v", created, err)
	}
	sessionID, turnID := createRuntimeBinding(t, ctx, pool, projectID, roleID, chatID, "owner-gate-root", "runtime-owner-gate", now.Add(time.Hour))
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "schedule-owner-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "owner-gate-root", RuntimeTurnID: turnID, RuntimeRunID: "runtime-owner-gate",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	process, err := adminpostgres.NewRepository(pool).EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
		TurnID: turnID, ProjectID: projectID, RoleID: roleID,
		InitiatorUserID: "root-owner-id", InitiatorUserName: "root-owner", TriggerPostID: "owner-gate-root",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root",
	})
	if err != nil {
		t.Fatalf("создать сохранённый process context: %v", err)
	}
	gateContext, err := repository.GetOwnerGateContext(ctx, automationsrepo.OwnerGateContextInput{
		RunPublicID: run.PublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: "owner-gate-root",
	})
	if err != nil {
		t.Fatalf("получить owner gate context: %v", err)
	}
	if gateContext.ProcessRunID != process.ProcessRunID || gateContext.PolicyRevisionID != process.PolicyRevisionID || gateContext.RootInitiatorUserID != "root-owner-id" {
		t.Fatalf("gate context=%#v process=%#v", gateContext, process)
	}
	if _, err := repository.GetOwnerGateContext(ctx, automationsrepo.OwnerGateContextInput{
		RunPublicID: run.PublicID, AuthenticatedProjectID: otherProjectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: "owner-gate-root",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("межпроектный context error=%v", err)
	}
	if _, err := repository.GetOwnerGateContext(ctx, automationsrepo.OwnerGateContextInput{
		RunPublicID: run.PublicID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID + 1, AuthenticatedSessionKey: "owner-gate-root",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("чужой turn/session context error=%v", err)
	}

	plan := testOwnerGatePlan(t, gateContext)
	invalidPlan := plan
	invalidPlan.RootInitiatorUserID = "agent-supplied-owner"
	if _, _, err := repository.CompleteCallback(ctx, testOwnerGateCallback(run.PublicID, projectID, sessionID, "owner-gate-root", invalidPlan, now)); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("agent-supplied owner принят: %v", err)
	}
	invalidPlan = plan
	invalidPlan.PolicyRevisionID++
	if _, _, err := repository.CompleteCallback(ctx, testOwnerGateCallback(run.PublicID, projectID, sessionID, "owner-gate-root", invalidPlan, now)); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("несохранённая policy revision принята: %v", err)
	}

	callback := testOwnerGateCallback(run.PublicID, projectID, sessionID, "owner-gate-root", plan, now.Add(time.Minute))
	type callbackResult struct {
		run       entity.ScheduledRun
		duplicate bool
		err       error
	}
	start := make(chan struct{})
	results := make(chan callbackResult, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			completed, duplicate, completeErr := repository.CompleteCallback(ctx, callback)
			results <- callbackResult{run: completed, duplicate: duplicate, err: completeErr}
		}()
	}
	close(start)
	group.Wait()
	close(results)
	duplicates := 0
	for result := range results {
		if result.err != nil || result.run.Status != string(value.AutomationRunStatusWaitingOwner) || result.run.Outcome != string(value.AutomationRunOutcomeRequiresHuman) || !result.run.FinishedAt.IsZero() {
			t.Fatalf("конкурентный callback result=%#v", result)
		}
		if result.duplicate {
			duplicates++
		}
	}
	if duplicates != 1 {
		t.Fatalf("duplicate callbacks=%d", duplicates)
	}
	var attentionCount int
	var occurrenceStatus, processStatus string
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&attentionCount); err != nil || attentionCount != 1 {
		t.Fatalf("attention count=%d error=%v", attentionCount, err)
	}
	if err := pool.QueryRow(ctx, `select status from matter_codex_schedule_occurrences where id = $1`, run.OccurrenceID).Scan(&occurrenceStatus); err != nil || occurrenceStatus != "waiting_owner" {
		t.Fatalf("occurrence status=%q error=%v", occurrenceStatus, err)
	}
	if err := pool.QueryRow(ctx, `select status from matter_codex_process_runs where id = $1`, process.ProcessRunID).Scan(&processStatus); err != nil || processStatus != "waiting_owner" {
		t.Fatalf("process status=%q error=%v", processStatus, err)
	}
	var genericReplyTurnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, 'generic-owner-reply', 'automation-channel', 'owner-gate-root', 'generic-owner-reply-post', 'owner reply', 'running') returning id`, sessionID).Scan(&genericReplyTurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := adminpostgres.NewRepository(pool).EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
		TurnID: genericReplyTurnID, ProjectID: projectID, RoleID: roleID,
		InitiatorUserID: "root-owner-id", InitiatorUserName: "root-owner", TriggerPostID: "generic-owner-reply-post",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root",
	}); err != nil {
		t.Fatalf("generic process path: %v", err)
	}
	var automationAttentionStatus string
	if err := pool.QueryRow(ctx, `select status from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&automationAttentionStatus); err != nil || automationAttentionStatus != "open" {
		t.Fatalf("generic process path перехватил automation gate: status=%q error=%v", automationAttentionStatus, err)
	}

	restarted := postgresrepo.NewRepository(pool)
	delivery, err := restarted.GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || delivery.DeliveryID != plan.DeliveryID || delivery.MattermostPostID != "" || delivery.RootInitiatorUserID != "root-owner-id" {
		t.Fatalf("delivery after restart=%#v error=%v", delivery, err)
	}
	mismatch := callback
	mismatch.PayloadSHA256 = bytes.Repeat([]byte{0x94}, 32)
	if _, _, err := restarted.CompleteCallback(ctx, mismatch); !errors.Is(err, automationsrepo.ErrCallbackMismatch) {
		t.Fatalf("изменённый replay error=%v", err)
	}
	reconciled, changed, err := restarted.ReconcileRuntimeTerminal(ctx, automationsrepo.ReconcileRuntimeTerminalInput{
		ProjectID: projectID, RuntimeSessionID: sessionID, RuntimeTurnID: turnID, RuntimeRunID: "runtime-owner-gate",
		RuntimeStatus: "succeeded", SafeSummary: "runtime terminal", Now: now.Add(2 * time.Minute),
	})
	if err != nil || changed || reconciled.Status != string(value.AutomationRunStatusWaitingOwner) {
		t.Fatalf("runtime reconciliation закрыл gate: run=%#v changed=%t error=%v", reconciled, changed, err)
	}
	posted, err := restarted.SetOwnerAttentionPost(ctx, automationsrepo.SetOwnerAttentionPostInput{
		AttentionID: delivery.AttentionID, ScheduledRunID: run.ID, DeliveryID: delivery.DeliveryID,
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", MattermostPostID: "attention-post-1", Now: now.Add(2 * time.Minute),
	})
	if err != nil || posted.MattermostPostID != "attention-post-1" {
		t.Fatalf("сохранить post binding: delivery=%#v error=%v", posted, err)
	}
	if _, err := restarted.SetOwnerAttentionPost(ctx, automationsrepo.SetOwnerAttentionPostInput{
		AttentionID: delivery.AttentionID, ScheduledRunID: run.ID, DeliveryID: delivery.DeliveryID,
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", MattermostPostID: "attention-post-1", Now: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("replay post binding: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_owner_attention_requests set automation_project_id = $2 where id = $1`, delivery.AttentionID, otherProjectID); err == nil {
		t.Fatal("FK разрешил межпроектную подмену owner attention")
	}

	wrongActor := automationsrepo.ResolveOwnerGateInput{
		ProjectID: projectID, ActorUserID: "other-user", ActorUserName: "other",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", MattermostResponsePostID: "owner-response-1", Now: now.Add(4 * time.Minute),
	}
	if _, _, err := restarted.ResolveOwnerGate(ctx, wrongActor); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("чужой actor resolution error=%v", err)
	}
	wrongThread := wrongActor
	wrongThread.ActorUserID = "root-owner-id"
	wrongThread.MattermostRootPostID = "other-root"
	if _, _, err := restarted.ResolveOwnerGate(ctx, wrongThread); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("чужой thread resolution error=%v", err)
	}
	resolution := wrongActor
	resolution.ActorUserID = "root-owner-id"
	resolution.ActorUserName = "root-owner"
	resolved, duplicate, err := restarted.ResolveOwnerGate(ctx, resolution)
	if err != nil || duplicate || resolved.Status != string(value.AutomationRunStatusSucceeded) || resolved.FinishedAt.IsZero() {
		t.Fatalf("resolution run=%#v duplicate=%t error=%v", resolved, duplicate, err)
	}
	replayed, duplicate, err := postgresrepo.NewRepository(pool).ResolveOwnerGate(ctx, resolution)
	if err != nil || !duplicate || replayed.ID != resolved.ID || replayed.FinishedAt.IsZero() {
		t.Fatalf("resolution replay run=%#v duplicate=%t error=%v", replayed, duplicate, err)
	}
	var attentionStatus, resolvedUserID, resolvedPostID string
	var resolvedAt time.Time
	if err := pool.QueryRow(ctx, `select status, resolved_at, resolved_by_user_id, resolved_by_post_id from matter_codex_owner_attention_requests where id = $1`, delivery.AttentionID).Scan(&attentionStatus, &resolvedAt, &resolvedUserID, &resolvedPostID); err != nil {
		t.Fatal(err)
	}
	if attentionStatus != "resolved" || resolvedAt.IsZero() || resolvedUserID != "root-owner-id" || resolvedPostID != "owner-response-1" {
		t.Fatalf("attention status=%q resolved_at=%s actor=%q post=%q", attentionStatus, resolvedAt, resolvedUserID, resolvedPostID)
	}
	if err := pool.QueryRow(ctx, `select status from matter_codex_schedule_occurrences where id = $1`, run.OccurrenceID).Scan(&occurrenceStatus); err != nil || occurrenceStatus != "succeeded" {
		t.Fatalf("resolved occurrence status=%q error=%v", occurrenceStatus, err)
	}
	var resolutionAudits int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events where scheduled_run_id = $1 and event_type = 'run.owner_resolved'`, run.ID).Scan(&resolutionAudits); err != nil || resolutionAudits != 1 {
		t.Fatalf("resolution audits=%d error=%v", resolutionAudits, err)
	}
}

func testOwnerGateCallback(runID string, projectID int64, sessionID int64, sessionKey string, plan automationsrepo.OwnerGatePlanInput, now time.Time) automationsrepo.CompleteCallbackInput {
	return automationsrepo.CompleteCallbackInput{
		RunPublicID: runID, AuthenticatedProjectID: projectID, AuthenticatedSessionID: sessionID, AuthenticatedSessionKey: sessionKey,
		CallbackContractVersion: value.AutomationCallbackContractV1, Status: string(value.AutomationRunStatusWaitingOwner),
		Outcome: string(value.AutomationRunOutcomeRequiresHuman), SafeSummary: "Автоматизация ожидает решения владельца.",
		PayloadSHA256: bytes.Repeat([]byte{0x93}, 32), OwnerGate: &plan, Now: now,
	}
}

func testOwnerGatePlan(t *testing.T, gateContext entity.AutomationOwnerGateContext) automationsrepo.OwnerGatePlanInput {
	t.Helper()
	props := map[string]any{
		"matter_codex_event":                 "automation_owner_attention",
		"matter_codex_callback_delivery_id":  "aaaaaaaaaaaaaaaaaaaaaaaaaa",
		"matter_codex_automation_run_id":     gateContext.ScheduledRunPublicID,
		"matter_codex_process_run_id":        gateContext.ProcessPublicID,
		"matter_codex_human_decision_status": "pending",
	}
	propsJSON, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	message := "Требуется решение владельца по точному ScheduledRun.\n\n#notrigger"
	payloadJSON, err := json.Marshal(struct {
		ChannelID  string         `json:"channel_id"`
		RootPostID string         `json:"root_post_id"`
		Message    string         `json:"message"`
		Props      map[string]any `json:"props"`
	}{gateContext.MattermostChannelID, gateContext.MattermostRootPostID, message, props})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payloadJSON)
	return automationsrepo.OwnerGatePlanInput{
		ProcessRunID: gateContext.ProcessRunID, PolicyRevisionID: gateContext.PolicyRevisionID,
		RootInitiatorUserID: gateContext.RootInitiatorUserID, RootInitiatorName: gateContext.RootInitiatorName,
		AttentionSummary: "Автоматизация ожидает решения владельца.", AttentionRecommendation: "Ответьте в точном треде.",
		DeliveryID: "aaaaaaaaaaaaaaaaaaaaaaaaaa", DeliveryMessage: message, DeliveryPropsJSON: propsJSON, DeliveryPayloadSHA256: digest[:],
	}
}
