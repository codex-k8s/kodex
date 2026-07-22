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
	if err := migrations.RunTo(ctx, dsn, 35); err != nil {
		t.Fatalf("последовательно обновить схему 34 -> 35: %v", err)
	}
	if err := migrations.RunTo(ctx, dsn, 36); err != nil {
		t.Fatalf("последовательно обновить схему 35 -> 36: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 36 {
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
	coordinationRepository := adminpostgres.NewRepository(pool)
	process, err := coordinationRepository.EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
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

	sharedIdempotencyKey := "automation:" + run.PublicID
	genericBefore, created, err := coordinationRepository.CreateOwnerAttention(ctx, adminrepo.CreateOwnerAttentionInput{
		ProcessRunID: process.ProcessRunID, TurnID: turnID, Severity: "normal",
		Summary: "Generic request before automation", PauseScope: "turn", IdempotencyKey: sharedIdempotencyKey,
	})
	if err != nil || !created {
		t.Fatalf("generic before automation: request=%#v created=%t error=%v", genericBefore, created, err)
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
	if _, err := coordinationRepository.SetOwnerAttentionPost(ctx, genericBefore.ID, "generic-before-post"); err != nil {
		t.Fatalf("generic setter after automation commit: %v", err)
	}
	var attentionCount int
	var occurrenceStatus, processStatus string
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&attentionCount); err != nil || attentionCount != 1 {
		t.Fatalf("attention count=%d error=%v", attentionCount, err)
	}
	var automationPostID string
	if err := pool.QueryRow(ctx, `select mattermost_post_id from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&automationPostID); err != nil || automationPostID != "" {
		t.Fatalf("generic setter изменил automation row: post=%q error=%v", automationPostID, err)
	}
	if _, err := pool.Exec(ctx, `delete from matter_codex_owner_attention_requests where id = $1`, genericBefore.ID); err != nil {
		t.Fatalf("подготовить независимую последовательность automation -> generic: %v", err)
	}
	genericAfter, created, err := coordinationRepository.CreateOwnerAttention(ctx, adminrepo.CreateOwnerAttentionInput{
		ProcessRunID: process.ProcessRunID, TurnID: turnID, Severity: "normal",
		Summary: "Generic request after automation", PauseScope: "turn", IdempotencyKey: sharedIdempotencyKey,
	})
	if err != nil || !created || genericAfter.ID == deliveryIDForRun(t, ctx, pool, run.ID) {
		t.Fatalf("generic after automation: request=%#v created=%t error=%v", genericAfter, created, err)
	}
	genericLostResponse, created, err := coordinationRepository.CreateOwnerAttention(ctx, adminrepo.CreateOwnerAttentionInput{
		ProcessRunID: process.ProcessRunID, TurnID: turnID, Severity: "critical",
		Summary: "Attacker changed payload", PauseScope: "process", IdempotencyKey: sharedIdempotencyKey,
	})
	if err != nil || created || genericLostResponse.ID != genericAfter.ID || genericLostResponse.MattermostPostID != "" {
		t.Fatalf("generic lost-response replay: request=%#v created=%t error=%v", genericLostResponse, created, err)
	}
	if _, err := coordinationRepository.SetOwnerAttentionPost(ctx, genericAfter.ID, "generic-after-post"); err != nil {
		t.Fatalf("generic lost-response binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `select mattermost_post_id from matter_codex_owner_attention_requests where automation_scheduled_run_id = $1`, run.ID).Scan(&automationPostID); err != nil || automationPostID != "" {
		t.Fatalf("generic lost-response изменил automation row: post=%q error=%v", automationPostID, err)
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
	pendingHistory, err := restarted.ListHistory(ctx, "schedule-owner", 10)
	if err != nil || len(pendingHistory) != 1 || pendingHistory[0].ScheduledRunPublicID != run.PublicID || pendingHistory[0].Status != string(value.AutomationRunStatusWaitingOwner) || pendingHistory[0].HumanDecisionStatus != "open" || pendingHistory[0].DeliveryStatus != "pending" || pendingHistory[0].NextAction != "retry_same_callback" {
		t.Fatalf("PostgreSQL pending history=%#v error=%v", pendingHistory, err)
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
	prematureResolution := automationsrepo.ResolveOwnerGateInput{
		ProjectID: projectID, ActorUserID: "root-owner-id", ActorUserName: "root-owner",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root",
		MattermostResponsePostID: "owner-response-before-delivery", MattermostResponseCreateAt: 1_000,
		Now: now.Add(2 * time.Minute),
	}
	if _, _, err := restarted.ResolveOwnerGate(ctx, prematureResolution); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("решение до delivery proof принято: %v", err)
	}
	claimed, err := restarted.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: run.ID, ClaimToken: "claim-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Now: now.Add(2 * time.Minute), LeaseUntil: now.Add(3 * time.Minute), EligibleBefore: now.Add(2 * time.Minute),
	})
	if err != nil || claimed.Fence != 1 || claimed.ClaimToken == "" {
		t.Fatalf("claim delivery=%#v error=%v", claimed, err)
	}
	if _, err := restarted.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: run.ID, ClaimToken: "claim-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Now: now.Add(2 * time.Minute), LeaseUntil: now.Add(3 * time.Minute), EligibleBefore: now.Add(2 * time.Minute),
	}); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("конкурентный второй claim error=%v", err)
	}
	if err := restarted.RetainOwnerAttentionDelivery(ctx, automationsrepo.RetainOwnerAttentionDeliveryInput{
		AttentionID: delivery.AttentionID, ScheduledRunID: run.ID, DeliveryID: delivery.DeliveryID,
		ClaimToken: claimed.ClaimToken, Fence: claimed.Fence,
		Now: now.Add(2*time.Minute + 10*time.Second), LeaseUntil: now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("удержать неоднозначный claim: %v", err)
	}
	retained, err := restarted.GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || !retained.ConfirmationPending || retained.ClaimToken != claimed.ClaimToken || !retained.LeaseExpiresAt.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("удержанный неоднозначный claim=%#v error=%v", retained, err)
	}
	if err := restarted.DeferOwnerAttentionDelivery(ctx, automationsrepo.DeferOwnerAttentionDeliveryInput{
		AttentionID: delivery.AttentionID, ScheduledRunID: run.ID, DeliveryID: delivery.DeliveryID,
		ClaimToken: claimed.ClaimToken, Fence: claimed.Fence,
		RetryAt: now.Add(2*time.Minute + 20*time.Second), Now: now.Add(2*time.Minute + 15*time.Second),
	}); !errors.Is(err, automationsrepo.ErrConflict) {
		t.Fatalf("неоднозначный claim был очищен обычным defer: %v", err)
	}
	if _, err := restarted.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: run.ID, ClaimToken: "claim-cccccccccccccccccccccccccccccccc",
		Now: now.Add(3 * time.Minute), LeaseUntil: now.Add(4 * time.Minute), EligibleBefore: now.Add(3 * time.Minute),
	}); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("неоднозначный claim был перехвачен до истечения lease: %v", err)
	}
	confirmationClaim, err := restarted.ClaimOwnerAttentionDelivery(ctx, automationsrepo.ClaimOwnerAttentionDeliveryInput{
		ScheduledRunID: run.ID, ClaimToken: "claim-dddddddddddddddddddddddddddddddd",
		Now: now.Add(4*time.Minute + time.Second), LeaseUntil: now.Add(5 * time.Minute), EligibleBefore: now.Add(4*time.Minute + time.Second),
	})
	if err != nil || !confirmationClaim.ConfirmationPending || confirmationClaim.Fence != claimed.Fence+1 {
		t.Fatalf("confirmation-only claim=%#v error=%v", confirmationClaim, err)
	}
	setPostInput := automationsrepo.SetOwnerAttentionPostInput{
		AttentionID: delivery.AttentionID, ScheduledRunID: run.ID, DeliveryID: delivery.DeliveryID,
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", MattermostPostID: "attention-post-1",
		MattermostPostCreateAt: 2_000,
		ClaimToken:             confirmationClaim.ClaimToken, Fence: confirmationClaim.Fence, Now: now.Add(4*time.Minute + time.Second),
	}
	if _, err := restarted.SetOwnerAttentionPost(ctx, setPostInput); err != nil {
		t.Fatalf("сохранить delivery и ordering proof: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_owner_attention_requests
		set automation_mattermost_post_create_at = 2002
		where id = $1`, delivery.AttentionID); err == nil {
		t.Fatal("immutable ordering proof карточки изменён напрямую")
	}
	delayedPreBinding := prematureResolution
	delayedPreBinding.Now = now.Add(5 * time.Minute)
	if _, _, err := restarted.ResolveOwnerGate(ctx, delayedPreBinding); !errors.Is(err, automationsrepo.ErrNotFound) {
		t.Fatalf("задержанный pre-binding post принят после binding: %v", err)
	}
	var openStatus, waitingRunStatus, waitingOccurrenceStatus, waitingProcessStatus string
	if err := pool.QueryRow(ctx, `select attention.status, run.status, occurrence.status, process.status
		from matter_codex_owner_attention_requests attention
		join matter_codex_scheduled_runs run on run.id = attention.automation_scheduled_run_id
		join matter_codex_schedule_occurrences occurrence on occurrence.id = run.occurrence_id
		join matter_codex_process_runs process on process.id = attention.process_run_id
		where attention.id = $1`, delivery.AttentionID).Scan(&openStatus, &waitingRunStatus, &waitingOccurrenceStatus, &waitingProcessStatus); err != nil {
		t.Fatal(err)
	}
	if openStatus != "open" || waitingRunStatus != "waiting_owner" || waitingOccurrenceStatus != "waiting_owner" || waitingProcessStatus != "waiting_owner" {
		t.Fatalf("pre-binding replay изменил gate: attention=%q run=%q occurrence=%q process=%q", openStatus, waitingRunStatus, waitingOccurrenceStatus, waitingProcessStatus)
	}
	resolution := automationsrepo.ResolveOwnerGateInput{
		ProjectID: projectID, ActorUserID: "root-owner-id", ActorUserName: "root-owner",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root",
		MattermostResponsePostID: "owner-response-1", MattermostResponseCreateAt: 2_001,
		Now: now.Add(6 * time.Minute),
	}
	type decisionResult struct {
		run       entity.ScheduledRun
		duplicate bool
		err       error
	}
	decisionResults := make(chan decisionResult, 2)
	raceStart := make(chan struct{})
	for range 2 {
		go func() {
			<-raceStart
			resolvedRun, duplicate, resolveErr := restarted.ResolveOwnerGate(ctx, resolution)
			decisionResults <- decisionResult{run: resolvedRun, duplicate: duplicate, err: resolveErr}
		}()
	}
	close(raceStart)
	decision := decisionResult{}
	duplicateDecisions := 0
	for range 2 {
		candidate := <-decisionResults
		if candidate.err != nil || candidate.run.Status != string(value.AutomationRunStatusSucceeded) || candidate.run.FinishedAt.IsZero() {
			t.Fatalf("конкурентное resolution=%#v", candidate)
		}
		if candidate.duplicate {
			duplicateDecisions++
		} else {
			decision = candidate
		}
	}
	if duplicateDecisions != 1 || decision.run.ID == 0 {
		t.Fatalf("конкурентные решения: duplicate=%d result=%#v", duplicateDecisions, decision)
	}
	posted, err := restarted.GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || posted.MattermostPostID != "attention-post-1" || posted.MattermostPostCreateAt != 2_000 {
		t.Fatalf("delivery proof=%#v error=%v", posted, err)
	}
	if _, err := restarted.SetOwnerAttentionPost(ctx, setPostInput); !errors.Is(err, automationsrepo.ErrConflict) {
		t.Fatalf("stale claim повторно записал post: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_owner_attention_requests set automation_project_id = $2 where id = $1`, delivery.AttentionID, otherProjectID); err == nil {
		t.Fatal("FK разрешил межпроектную подмену owner attention")
	}

	wrongActor := automationsrepo.ResolveOwnerGateInput{
		ProjectID: projectID, ActorUserID: "other-user", ActorUserName: "other",
		MattermostChannelID: "automation-channel", MattermostRootPostID: "owner-gate-root", MattermostResponsePostID: "owner-response-1",
		MattermostResponseCreateAt: 2_001, Now: now.Add(4 * time.Minute),
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
	resolved := decision.run
	duplicate := false
	replayed, duplicate, err := postgresrepo.NewRepository(pool).ResolveOwnerGate(ctx, resolution)
	if err != nil || !duplicate || replayed.ID != resolved.ID || replayed.FinishedAt.IsZero() {
		t.Fatalf("resolution replay run=%#v duplicate=%t error=%v", replayed, duplicate, err)
	}
	var attentionStatus, resolvedUserID, resolvedPostID string
	var responsePostCreateAt int64
	var resolvedAt time.Time
	if err := pool.QueryRow(ctx, `select status, resolved_at, resolved_by_user_id, resolved_by_post_id, automation_response_post_create_at from matter_codex_owner_attention_requests where id = $1`, delivery.AttentionID).Scan(&attentionStatus, &resolvedAt, &resolvedUserID, &resolvedPostID, &responsePostCreateAt); err != nil {
		t.Fatal(err)
	}
	if attentionStatus != "resolved" || resolvedAt.IsZero() || resolvedUserID != "root-owner-id" || resolvedPostID != "owner-response-1" || responsePostCreateAt != 2_001 {
		t.Fatalf("attention status=%q resolved_at=%s actor=%q post=%q create_at=%d", attentionStatus, resolvedAt, resolvedUserID, resolvedPostID, responsePostCreateAt)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_owner_attention_requests
		set automation_response_post_create_at = 2003
		where id = $1`, delivery.AttentionID); err == nil {
		t.Fatal("immutable ordering proof ответа изменён напрямую")
	}
	resolvedHistory, err := restarted.ListHistory(ctx, "schedule-owner", 10)
	if err != nil || len(resolvedHistory) != 1 || resolvedHistory[0].Status != string(value.AutomationRunStatusSucceeded) || resolvedHistory[0].HumanDecisionStatus != "resolved" || resolvedHistory[0].DeliveryStatus != "delivered" || resolvedHistory[0].NextAction != "none" {
		t.Fatalf("PostgreSQL resolved history=%#v error=%v", resolvedHistory, err)
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

func deliveryIDForRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scheduledRunID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `select id from matter_codex_owner_attention_requests where request_kind = 'automation' and automation_scheduled_run_id = $1`, scheduledRunID).Scan(&id); err != nil {
		t.Fatalf("прочитать automation attention id: %v", err)
	}
	return id
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
