package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
)

func TestAutomationRunNowDispatchesSavedPlaybookAndBindsRuntime(t *testing.T) {
	prompt := mustAutomationPlaybook()
	promptHash := sha256.Sum256([]byte(prompt))
	schedule := entity.AutomationSchedule{
		ID: 1, PublicID: "schedule-11111111111111111111111111111111", ProjectID: 1, ProjectName: "MatterCodex",
		TargetAgentRoleID: 2, TargetAgentRoleName: "developer", TargetChatID: 3, TargetChatName: "Development",
		Name: "Daily check", OwnerMattermostUserID: "owner-id", OwnerMattermostUserName: "owner", Enabled: true,
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: prompt, PromptSHA256: promptHash[:], CallbackContractVersion: value.AutomationCallbackContractV1,
	}
	run := entity.ScheduledRun{
		ID: 4, PublicID: "scheduled-run-11111111111111111111111111111111", OccurrenceID: 5,
		ScheduleID: schedule.ID, SchedulePublicID: schedule.PublicID, ScheduleName: schedule.Name,
		ProjectID: 1, ProjectName: schedule.ProjectName, TargetAgentRoleID: 2, TargetAgentRoleName: "developer",
		TargetChatID: 3, TargetChatName: "Development", OwnerMattermostUserID: "owner-id", Status: string(value.AutomationRunStatusQueued),
	}
	run.RuntimeRunID = automationRuntimeRunID(run.PublicID)
	repository := &fakeAutomationRepository{schedule: schedule, run: run, createRunCreated: true}
	catalog := &fakeAutomationCatalog{
		project:      entity.Project{ID: 1, Name: "MatterCodex"},
		role:         entity.AgentRole{ID: 2, ProjectID: 1, Name: "developer", Enabled: true},
		chat:         entity.Chat{ID: 3, ProjectID: 1, Name: "Development", MattermostChannelID: "channel-1"},
		participants: []entity.ChatParticipant{{ChatID: 3, RoleID: 2, Enabled: true}},
	}
	dispatcher := &fakeAutomationDispatcher{queued: AgentTurnQueued{SessionID: 6, SessionKey: "session-1", TurnID: 7, RunID: run.RuntimeRunID}}
	publisher := &fakeAutomationPublisher{ref: MattermostPostRef{ChannelID: "channel-1", PostID: "root-1"}}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	svc := NewAutomationService(AutomationServiceConfig{
		Repository: repository, Catalog: catalog, Dispatcher: dispatcher, Publisher: publisher,
		OwnerMattermostUsername: "owner", StorageReady: true, RuntimeReady: true, Now: func() time.Time { return now },
	})

	result, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "@Owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "command-1",
	})
	if err != nil {
		t.Fatalf("RunNow() error=%v", err)
	}
	if result.Run.Status != string(value.AutomationRunStatusRunning) || dispatcher.calls != 1 || publisher.calls != 1 || repository.bindCalls != 1 {
		t.Fatalf("result=%#v dispatcher=%d publisher=%d binds=%d", result, dispatcher.calls, publisher.calls, repository.bindCalls)
	}
	if !strings.Contains(dispatcher.request.PreparedPrompt, "mattermost_complete_automation") || !strings.Contains(dispatcher.request.PreparedPrompt, run.PublicID) || strings.Contains(dispatcher.request.PreparedPrompt, "session-token") {
		t.Fatalf("prepared prompt не содержит безопасный callback-контракт: %q", dispatcher.request.PreparedPrompt)
	}
	if repository.bindInput.ProjectID != 1 || repository.bindInput.RuntimeSessionID != 6 || repository.bindInput.RuntimeTurnID != 7 || repository.bindInput.RuntimeRunID != run.RuntimeRunID || repository.bindInput.MattermostRootPostID != "root-1" {
		t.Fatalf("runtime binding=%#v", repository.bindInput)
	}

	repository.createRunCreated = false
	duplicate, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "command-1",
	})
	if err != nil || !duplicate.Duplicate || dispatcher.calls != 1 || publisher.calls != 1 || repository.recordThreadCalls != 1 {
		t.Fatalf("duplicate=%#v error=%v dispatcher=%d publisher=%d", duplicate, err, dispatcher.calls, publisher.calls)
	}
	if _, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "other-id", UserName: "developer"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "spoofed-run",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("ручной запуск от не-владельца error=%v", err)
	}
	if dispatcher.calls != 1 || publisher.calls != 1 {
		t.Fatalf("неавторизованный запуск вызвал побочный эффект: dispatcher=%d publisher=%d", dispatcher.calls, publisher.calls)
	}
	repository.run.Status = string(value.AutomationRunStatusWaitingOwner)
	waitingReplay, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "command-1",
	})
	if err != nil || !waitingReplay.Duplicate || waitingReplay.Run.Status != string(value.AutomationRunStatusWaitingOwner) || dispatcher.calls != 1 || publisher.calls != 1 {
		t.Fatalf("waiting_owner replay=%#v error=%v dispatcher=%d publisher=%d", waitingReplay, err, dispatcher.calls, publisher.calls)
	}

	if _, _, err := svc.CreateSchedule(context.Background(), CreateAutomationScheduleCommand{
		Actor: AuthenticatedActor{UserID: "other-id", UserName: "developer"}, ProjectID: 1, TargetAgentRoleID: 2, TargetChatID: 3,
		Name: "Spoofed", Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC",
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, IdempotencyKey: "spoofed",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("создание от не-владельца error=%v", err)
	}
}

func TestAutomationRunNowResumesEveryDurableBoundaryWithSameRuntime(t *testing.T) {
	prompt := mustAutomationPlaybook()
	promptHash := sha256.Sum256([]byte(prompt))
	schedule := entity.AutomationSchedule{
		ID: 1, PublicID: "schedule-11111111111111111111111111111111", ProjectID: 1, ProjectName: "MatterCodex",
		TargetAgentRoleID: 2, TargetChatID: 3, Name: "Restart check", OwnerMattermostUserID: "owner-id", Enabled: true,
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptSnapshot: prompt, PromptSHA256: promptHash[:],
		CallbackContractVersion: value.AutomationCallbackContractV1,
	}
	run := entity.ScheduledRun{
		ID: 4, PublicID: "scheduled-run-11111111111111111111111111111111", OccurrenceID: 5, ScheduleID: 1,
		SchedulePublicID: schedule.PublicID, ProjectID: 1, TargetAgentRoleID: 2, TargetChatID: 3,
		OwnerMattermostUserID: "owner-id", Status: string(value.AutomationRunStatusQueued),
	}
	run.RuntimeRunID = automationRuntimeRunID(run.PublicID)
	repository := &fakeAutomationRepository{
		schedule: schedule, run: run, createRunCreated: true,
		recordThreadErrors: []error{errors.New("synthetic thread checkpoint failure")},
		bindErrors:         []error{errors.New("synthetic bind checkpoint failure")},
	}
	catalog := &fakeAutomationCatalog{
		project:      entity.Project{ID: 1, Name: "MatterCodex"},
		role:         entity.AgentRole{ID: 2, ProjectID: 1, Name: "developer", Enabled: true},
		chat:         entity.Chat{ID: 3, ProjectID: 1, MattermostChannelID: "channel-1"},
		participants: []entity.ChatParticipant{{ChatID: 3, RoleID: 2, Enabled: true}},
	}
	dispatcher := &fakeAutomationDispatcher{queued: AgentTurnQueued{SessionID: 6, SessionKey: "session-1", TurnID: 7, RunID: run.RuntimeRunID}}
	publisher := &fakeAutomationPublisher{
		ref:    MattermostPostRef{ChannelID: "channel-1", PostID: "root-1"},
		errors: []error{errors.New("synthetic publish failure")},
	}
	newService := func() *AutomationService {
		return NewAutomationService(AutomationServiceConfig{
			Repository: repository, Catalog: catalog, Dispatcher: dispatcher, Publisher: publisher,
			OwnerMattermostUsername: "owner", StorageReady: true, RuntimeReady: true,
		})
	}
	command := RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "restart-command",
	}
	if _, err := newService().RunNow(context.Background(), command); err == nil {
		t.Fatal("fault after durable run creation was not returned")
	}
	repository.createRunCreated = false
	if _, err := newService().RunNow(context.Background(), command); err == nil {
		t.Fatal("fault after idempotent publication was not returned")
	}
	if _, err := newService().RunNow(context.Background(), command); err == nil {
		t.Fatal("fault after idempotent enqueue was not returned")
	}
	result, err := newService().RunNow(context.Background(), command)
	if err != nil || result.Run.Status != string(value.AutomationRunStatusRunning) || !result.Duplicate {
		t.Fatalf("resumed RunNow result=%#v error=%v", result, err)
	}
	if publisher.calls != 3 || repository.recordThreadCalls != 2 || dispatcher.calls != 2 || repository.bindCalls != 2 {
		t.Fatalf("unexpected resume steps: publish=%d record=%d dispatch=%d bind=%d", publisher.calls, repository.recordThreadCalls, dispatcher.calls, repository.bindCalls)
	}
	for _, idempotencyID := range publisher.idempotencyIDs {
		if idempotencyID != run.PublicID {
			t.Fatalf("publication identity changed during resume: %q", idempotencyID)
		}
	}
	if dispatcher.request.RequestedRunID != run.RuntimeRunID || repository.bindInput.RuntimeRunID != run.RuntimeRunID {
		t.Fatalf("runtime identity changed during resume: request=%q binding=%q", dispatcher.request.RequestedRunID, repository.bindInput.RuntimeRunID)
	}
}

func TestAutomationCallbackStoresOnlyServerOwnedSummaryForSyntheticSecretMatrix(t *testing.T) {
	runID := "scheduled-run-11111111111111111111111111111111"
	repository := &fakeAutomationRepository{run: entity.ScheduledRun{PublicID: runID}}
	svc := NewAutomationService(AutomationServiceConfig{
		Repository:   repository,
		Catalog:      &fakeAutomationCatalog{},
		StorageReady: true,
		Now:          func() time.Time { return time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC) },
	})
	secrets := []string{
		"synthetic-openai-value-111111111111",
		"synthetic-github-value-222222222222",
		"synthetic-mattermost-value-333333333333",
		"synthetic-kubernetes-value-444444444444",
		"synthetic-postgresql-value-555555555555",
		"synthetic-session-value-666666666666",
		"synthetic-mcp-value-777777777777",
	}
	summaries := []string{"Ты выполняешь минимальный playbook автоматизации MatterCodex"}
	for _, secret := range secrets {
		encodedJSON, err := json.Marshal(secret)
		if err != nil {
			t.Fatal(err)
		}
		summaries = append(summaries,
			secret,
			string(encodedJSON),
			base64.StdEncoding.EncodeToString([]byte(secret)),
			strings.Join(splitAutomationSecret(secret, 5), " "),
		)
	}
	for index, summary := range summaries {
		exactPayload, err := json.Marshal(map[string]string{
			"schedule_run_id": runID, "callback_contract": value.AutomationCallbackContractV1,
			"outcome": string(value.AutomationRunOutcomeNoAction), "summary": summary,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CompleteCallback(context.Background(), AutomationCallbackCommand{
			RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-1",
			CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeNoAction),
			AgentSummary: summary, ExactPayload: exactPayload,
		})
		if err != nil {
			t.Fatalf("matrix item %d rejected before server-owned replacement: %v", index, err)
		}
		stored := repository.completeInputs[len(repository.completeInputs)-1]
		if stored.SafeSummary != "Автоматизация завершена: действий не требуется." {
			t.Fatalf("matrix item %d persisted non-server summary", index)
		}
		for _, secret := range secrets {
			if strings.Contains(stored.SafeSummary, secret) {
				t.Fatalf("matrix item %d persisted synthetic secret", index)
			}
		}
	}
}

func TestAutomationCallbackRejectsLossyInputAndHashesExactPayload(t *testing.T) {
	runID := "scheduled-run-11111111111111111111111111111111"
	repository := &fakeAutomationRepository{run: entity.ScheduledRun{PublicID: runID}}
	svc := NewAutomationService(AutomationServiceConfig{Repository: repository, Catalog: &fakeAutomationCatalog{}, StorageReady: true})
	base := AutomationCallbackCommand{
		RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-1",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeNoAction), AgentSummary: "Действий нет",
		ExactPayload: []byte(`{"schedule_run_id":"scheduled-run-11111111111111111111111111111111","callback_contract":"automation.callback.v1","outcome":"no_action","summary":"Действий нет"}`),
	}
	if _, err := svc.CompleteCallback(context.Background(), base); err != nil {
		t.Fatalf("base callback error=%v", err)
	}
	withSuffix := base
	withSuffix.AgentSummary += " "
	withSuffix.ExactPayload = append(append([]byte(nil), base.ExactPayload...), ' ')
	if _, err := svc.CompleteCallback(context.Background(), withSuffix); err != nil {
		t.Fatalf("whitespace suffix callback error=%v", err)
	}
	if len(repository.completeInputs) != 2 || string(repository.completeInputs[0].PayloadSHA256) == string(repository.completeInputs[1].PayloadSHA256) {
		t.Fatal("different exact payload bytes produced the same replay hash")
	}
	for _, invalid := range []string{strings.Repeat("я", maxAutomationCallbackRunes+1), "text\x00suffix", "text\rsuffix"} {
		command := base
		command.AgentSummary = invalid
		if _, err := svc.CompleteCallback(context.Background(), command); err == nil {
			t.Fatal("lossy or over-limit callback input was accepted")
		}
	}
	for _, exactPayload := range [][]byte{nil, bytes.Repeat([]byte{'x'}, maxAutomationCallbackPayloadBytes+1)} {
		command := base
		command.ExactPayload = exactPayload
		if _, err := svc.CompleteCallback(context.Background(), command); err == nil {
			t.Fatal("missing or over-limit exact payload was accepted")
		}
	}
}

func TestAutomationRequiresHumanPersistsPendingGateAndPublishesServerOwnedCard(t *testing.T) {
	runID := "scheduled-run-11111111111111111111111111111111"
	repository := &fakeAutomationRepository{
		run: entity.ScheduledRun{ID: 41, PublicID: runID, ProjectID: 1, RuntimeTurnID: 7},
		gateContext: entity.AutomationOwnerGateContext{
			ScheduledRunID: 41, ScheduledRunPublicID: runID, ProjectID: 1, RuntimeTurnID: 7,
			ProcessRunID: 13, ProcessPublicID: "process-run-1", PolicyRevisionID: 17,
			RootInitiatorUserID: "owner-id", RootInitiatorName: "owner",
			MattermostChannelID: "channel-1", MattermostRootPostID: "root-1",
		},
	}
	publisher := &fakeAutomationPublisher{ref: MattermostPostRef{ChannelID: "channel-1", PostID: "attention-1"}}
	svc := NewAutomationService(AutomationServiceConfig{
		Repository: repository, Catalog: &fakeAutomationCatalog{}, Publisher: publisher, StorageReady: true,
		Now: func() time.Time { return time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC) },
	})
	secretSummary := "Нужен владелец; synthetic-secret-must-not-leak"
	command := AutomationCallbackCommand{
		RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-1",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeRequiresHuman),
		AgentSummary: secretSummary, ExactPayload: []byte(`{"outcome":"requires_human","summary":"synthetic-secret-must-not-leak"}`),
	}

	result, err := svc.CompleteCallback(context.Background(), command)
	if err != nil {
		t.Fatalf("CompleteCallback() error=%v", err)
	}
	if result.Run.Status != string(value.AutomationRunStatusWaitingOwner) || result.Run.Outcome != string(value.AutomationRunOutcomeRequiresHuman) || result.HumanDecisionStatus != "open" || result.DeliveryStatus != "delivered" || result.NextAction != "wait_for_owner_response" {
		t.Fatalf("pending result=%#v", result)
	}
	if result.OwnerAttentionID != 71 || publisher.reconcileCalls != 1 || repository.setPostCalls != 1 {
		t.Fatalf("attention=%d reconcile=%d set_post=%d", result.OwnerAttentionID, publisher.reconcileCalls, repository.setPostCalls)
	}
	if strings.Contains(publisher.reconcileInput.Message, secretSummary) || strings.Contains(publisher.reconcileInput.Message, "synthetic-secret") {
		t.Fatalf("карточка раскрыла агентское резюме: %q", publisher.reconcileInput.Message)
	}
	if !strings.Contains(publisher.reconcileInput.Message, runID) || publisher.reconcileInput.IdempotencyID == "" {
		t.Fatalf("карточка не содержит точный запуск или delivery-id: %#v", publisher.reconcileInput)
	}
	if publisher.reconcileInput.Props["matter_codex_human_decision_status"] != "pending" || publisher.reconcileInput.Props["matter_codex_event"] != "automation_owner_attention" {
		t.Fatalf("props карточки=%#v", publisher.reconcileInput.Props)
	}
}

func TestAutomationRequiresHumanRestartAndReplayRepairFailedPublicationWithoutSecondPost(t *testing.T) {
	runID := "scheduled-run-22222222222222222222222222222222"
	repository := &fakeAutomationRepository{
		run: entity.ScheduledRun{ID: 42, PublicID: runID, ProjectID: 1, RuntimeTurnID: 8},
		gateContext: entity.AutomationOwnerGateContext{
			ScheduledRunID: 42, ScheduledRunPublicID: runID, ProjectID: 1, RuntimeTurnID: 8,
			ProcessRunID: 14, ProcessPublicID: "process-run-2", PolicyRevisionID: 18,
			RootInitiatorUserID: "owner-id", RootInitiatorName: "owner",
			MattermostChannelID: "channel-2", MattermostRootPostID: "root-2",
		},
	}
	publisher := &fakeAutomationPublisher{
		ref:             MattermostPostRef{ChannelID: "channel-2", PostID: "attention-2"},
		reconcileErrors: []error{errors.New("synthetic publish failure"), errors.New("synthetic restart failure")},
	}
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	newService := func() *AutomationService {
		return NewAutomationService(AutomationServiceConfig{
			Repository: repository, Catalog: &fakeAutomationCatalog{}, Publisher: publisher, StorageReady: true,
			Now: func() time.Time { return now },
		})
	}
	command := AutomationCallbackCommand{
		RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-2",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeRequiresHuman),
		AgentSummary: "Нужно решение", ExactPayload: []byte(`{"outcome":"requires_human","summary":"Нужно решение"}`),
	}

	first, err := newService().CompleteCallback(context.Background(), command)
	if err != nil || first.Run.Status != string(value.AutomationRunStatusWaitingOwner) || first.DeliveryStatus != "pending" || first.NextAction != "retry_same_callback" {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	now = now.Add(automationDeliveryRetryDelay)
	if delivered, reconcileErr := newService().ReconcileOwnerAttentionDeliveries(context.Background(), 20); reconcileErr == nil || delivered != 0 {
		t.Fatalf("restart reconciliation delivered=%d error=%v", delivered, reconcileErr)
	}
	now = now.Add(automationDeliveryRetryDelay)
	second, err := newService().CompleteCallback(context.Background(), command)
	if err != nil || !second.Duplicate || second.DeliveryStatus != "delivered" {
		t.Fatalf("replay=%#v error=%v", second, err)
	}
	third, err := newService().CompleteCallback(context.Background(), command)
	if err != nil || !third.Duplicate || third.DeliveryStatus != "delivered" {
		t.Fatalf("second replay=%#v error=%v", third, err)
	}
	if publisher.reconcileCalls != 3 || repository.setPostCalls != 1 {
		t.Fatalf("reconcile=%d set_post=%d", publisher.reconcileCalls, repository.setPostCalls)
	}
	if repository.firstDeliveryID == "" || publisher.reconcileIDs[0] != repository.firstDeliveryID || publisher.reconcileIDs[1] != repository.firstDeliveryID || publisher.reconcileIDs[2] != repository.firstDeliveryID {
		t.Fatalf("delivery identity changed: repository=%q calls=%#v", repository.firstDeliveryID, publisher.reconcileIDs)
	}

	mismatch := command
	mismatch.ExactPayload = append(append([]byte(nil), command.ExactPayload...), ' ')
	if _, err := newService().CompleteCallback(context.Background(), mismatch); !errors.Is(err, automationsrepo.ErrCallbackMismatch) {
		t.Fatalf("mismatched replay error=%v", err)
	}
}

func TestAutomationRequiresHumanConcurrentExactReplayPostsExactlyOnce(t *testing.T) {
	runID := "scheduled-run-33333333333333333333333333333333"
	repository := &fakeAutomationRepository{
		run: entity.ScheduledRun{ID: 43, PublicID: runID, ProjectID: 1, RuntimeTurnID: 9},
		gateContext: entity.AutomationOwnerGateContext{
			ScheduledRunID: 43, ScheduledRunPublicID: runID, ProjectID: 1, RuntimeTurnID: 9,
			ProcessRunID: 15, ProcessPublicID: "process-run-3", PolicyRevisionID: 19,
			RootInitiatorUserID: "owner-id", RootInitiatorName: "owner",
			MattermostChannelID: "channel-3", MattermostRootPostID: "root-3",
		},
	}
	publishStarted := make(chan struct{})
	publishRelease := make(chan struct{})
	publisher := &fakeAutomationPublisher{
		ref:              MattermostPostRef{ChannelID: "channel-3", PostID: "attention-3"},
		reconcileStarted: publishStarted,
		reconcileRelease: publishRelease,
	}
	svc := NewAutomationService(AutomationServiceConfig{
		Repository: repository, Catalog: &fakeAutomationCatalog{}, Publisher: publisher, StorageReady: true,
		Now: func() time.Time { return time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC) },
	})
	command := AutomationCallbackCommand{
		RunPublicID: runID, AuthenticatedProjectID: 1, AuthenticatedSessionID: 2, AuthenticatedSessionKey: "session-3",
		CallbackContractVersion: value.AutomationCallbackContractV1, Outcome: string(value.AutomationRunOutcomeRequiresHuman),
		AgentSummary: "Нужно решение", ExactPayload: []byte(`{"outcome":"requires_human","summary":"Нужно решение"}`),
	}
	type callbackResult struct {
		result AutomationCallbackResult
		err    error
	}
	firstDone := make(chan callbackResult, 1)
	go func() {
		result, err := svc.CompleteCallback(context.Background(), command)
		firstDone <- callbackResult{result: result, err: err}
	}()
	<-publishStarted
	second, err := svc.CompleteCallback(context.Background(), command)
	if err != nil || !second.Duplicate || second.DeliveryStatus != "pending" {
		t.Fatalf("concurrent replay=%#v error=%v", second, err)
	}
	close(publishRelease)
	first := <-firstDone
	if first.err != nil || first.result.DeliveryStatus != "delivered" {
		t.Fatalf("first callback=%#v error=%v", first.result, first.err)
	}
	if publisher.reconcileCalls != 1 || repository.setPostCalls != 1 {
		t.Fatalf("Mattermost posts=%d set_post=%d", publisher.reconcileCalls, repository.setPostCalls)
	}
}

func TestAutomationDeliveryWorkerDrainsBacklogPastHundredSkipsFailureAndRestarts(t *testing.T) {
	now := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	repository := newFakeDeliveryQueueRepository(t, 205)
	publisher := &fakeDeliveryQueuePublisher{failDeliveryID: repository.deliveries[0].DeliveryID, calls: make(map[string]int)}
	newService := func() *AutomationService {
		return NewAutomationService(AutomationServiceConfig{
			Repository: repository, Catalog: &fakeAutomationCatalog{}, Publisher: publisher, StorageReady: true,
			Now: func() time.Time { return now },
		})
	}

	delivered, err := newService().ReconcileOwnerAttentionDeliveries(context.Background(), 4)
	if err == nil || delivered != 204 {
		t.Fatalf("first worker pass delivered=%d error=%v", delivered, err)
	}
	if got := repository.deliveredCount(); got != 204 {
		t.Fatalf("persisted deliveries after first pass=%d", got)
	}
	now = now.Add(automationDeliveryRetryDelay)
	delivered, err = newService().ReconcileOwnerAttentionDeliveries(context.Background(), 4)
	if err != nil || delivered != 1 || repository.deliveredCount() != 205 {
		t.Fatalf("restart pass delivered=%d total=%d error=%v", delivered, repository.deliveredCount(), err)
	}
	if publisher.maxConcurrent > 4 {
		t.Fatalf("publisher concurrency=%d", publisher.maxConcurrent)
	}
	if publisher.calls[publisher.failDeliveryID] != 2 {
		t.Fatalf("failed head attempts=%d", publisher.calls[publisher.failDeliveryID])
	}
}

func splitAutomationSecret(value string, size int) []string {
	parts := make([]string, 0, (len(value)+size-1)/size)
	for len(value) > 0 {
		length := size
		if len(value) < length {
			length = len(value)
		}
		parts = append(parts, value[:length])
		value = value[length:]
	}
	return parts
}

func TestNextDailyAutomationRunUsesIANAZone(t *testing.T) {
	now := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	next, err := nextDailyAutomationRun(now, "09:00", "Europe/Moscow")
	if err != nil {
		t.Fatalf("nextDailyAutomationRun() error=%v", err)
	}
	want := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
	if _, err := nextDailyAutomationRun(now, "09:00", "Local"); err == nil {
		t.Fatal("не-IANA зона Local принята")
	}
}

type fakeAutomationRepository struct {
	automationsrepo.Repository
	mu                  sync.Mutex
	schedule            entity.AutomationSchedule
	run                 entity.ScheduledRun
	createRunCreated    bool
	bindInput           automationsrepo.BindRunInput
	bindCalls           int
	recordThreadCalls   int
	completeInputs      []automationsrepo.CompleteCallbackInput
	recordThreadErrors  []error
	bindErrors          []error
	gateContext         entity.AutomationOwnerGateContext
	delivery            entity.AutomationOwnerAttentionDelivery
	gateAccepted        bool
	gatePayloadHash     []byte
	firstDeliveryID     string
	setPostCalls        int
	deliveryNextAttempt time.Time
}

func (repository *fakeAutomationRepository) CreateManualRun(_ context.Context, _ automationsrepo.CreateManualRunInput) (entity.ScheduledRun, bool, error) {
	return repository.run, repository.createRunCreated, nil
}

func (repository *fakeAutomationRepository) GetSchedule(_ context.Context, _ string, _ int64, _ string) (entity.AutomationSchedule, error) {
	return repository.schedule, nil
}

func (repository *fakeAutomationRepository) RecordRunThread(_ context.Context, input automationsrepo.RecordRunThreadInput) (entity.ScheduledRun, error) {
	repository.recordThreadCalls++
	if err := popAutomationError(&repository.recordThreadErrors); err != nil {
		return entity.ScheduledRun{}, err
	}
	repository.run.MattermostChannelID = input.MattermostChannelID
	repository.run.MattermostRootPostID = input.MattermostRootPostID
	return repository.run, nil
}

func (repository *fakeAutomationRepository) BindRun(_ context.Context, input automationsrepo.BindRunInput) (entity.ScheduledRun, error) {
	repository.bindCalls++
	repository.bindInput = input
	if err := popAutomationError(&repository.bindErrors); err != nil {
		return entity.ScheduledRun{}, err
	}
	bound := repository.run
	bound.Status = string(value.AutomationRunStatusRunning)
	bound.RuntimeSessionID = input.RuntimeSessionID
	bound.RuntimeSessionKey = input.RuntimeSessionKey
	bound.RuntimeTurnID = input.RuntimeTurnID
	bound.RuntimeRunID = input.RuntimeRunID
	bound.MattermostChannelID = input.MattermostChannelID
	bound.MattermostRootPostID = input.MattermostRootPostID
	repository.run = bound
	return bound, nil
}

func (repository *fakeAutomationRepository) CompleteCallback(_ context.Context, input automationsrepo.CompleteCallbackInput) (entity.ScheduledRun, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completeInputs = append(repository.completeInputs, input)
	if input.OwnerGate != nil && repository.gateAccepted {
		if !bytes.Equal(repository.gatePayloadHash, input.PayloadSHA256) {
			return entity.ScheduledRun{}, false, automationsrepo.ErrCallbackMismatch
		}
		return repository.run, true, nil
	}
	completed := repository.run
	completed.Status = input.Status
	completed.Outcome = input.Outcome
	completed.SafeSummary = input.SafeSummary
	completed.CallbackPayloadSHA256 = append([]byte(nil), input.PayloadSHA256...)
	repository.run = completed
	if input.OwnerGate != nil {
		repository.gateAccepted = true
		repository.gatePayloadHash = append([]byte(nil), input.PayloadSHA256...)
		repository.firstDeliveryID = input.OwnerGate.DeliveryID
		repository.delivery = entity.AutomationOwnerAttentionDelivery{
			AttentionID: 71, ScheduledRunID: completed.ID, ScheduledRunPublicID: completed.PublicID,
			ProcessRunID: input.OwnerGate.ProcessRunID, PolicyRevisionID: input.OwnerGate.PolicyRevisionID,
			RootInitiatorUserID: input.OwnerGate.RootInitiatorUserID,
			MattermostChannelID: repository.gateContext.MattermostChannelID, MattermostRootPostID: repository.gateContext.MattermostRootPostID,
			Status: "open", DeliveryID: input.OwnerGate.DeliveryID, DeliveryMessage: input.OwnerGate.DeliveryMessage,
			DeliveryPropsJSON: append([]byte(nil), input.OwnerGate.DeliveryPropsJSON...), DeliveryPayloadSHA256: append([]byte(nil), input.OwnerGate.DeliveryPayloadSHA256...),
		}
	}
	return completed, false, nil
}

func (repository *fakeAutomationRepository) GetOwnerGateContext(_ context.Context, _ automationsrepo.OwnerGateContextInput) (entity.AutomationOwnerGateContext, error) {
	return repository.gateContext, nil
}

func (repository *fakeAutomationRepository) GetOwnerAttentionDelivery(_ context.Context, _ int64) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.delivery, nil
}

func (repository *fakeAutomationRepository) ClaimOwnerAttentionDelivery(_ context.Context, input automationsrepo.ClaimOwnerAttentionDeliveryInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.AttentionID == 0 || repository.delivery.MattermostPostID != "" || (input.ScheduledRunID > 0 && input.ScheduledRunID != repository.delivery.ScheduledRunID) || repository.deliveryNextAttempt.After(input.EligibleBefore) || (repository.delivery.ClaimToken != "" && repository.delivery.LeaseExpiresAt.After(input.Now)) {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrNotFound
	}
	repository.delivery.ClaimToken = input.ClaimToken
	repository.delivery.ClaimedAt = input.Now
	repository.delivery.LeaseExpiresAt = input.LeaseUntil
	repository.delivery.Fence++
	return repository.delivery, nil
}

func (repository *fakeAutomationRepository) DeferOwnerAttentionDelivery(_ context.Context, input automationsrepo.DeferOwnerAttentionDeliveryInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.ClaimToken != input.ClaimToken || repository.delivery.Fence != input.Fence {
		return automationsrepo.ErrConflict
	}
	repository.delivery.ClaimToken = ""
	repository.delivery.ClaimedAt = time.Time{}
	repository.delivery.LeaseExpiresAt = time.Time{}
	repository.deliveryNextAttempt = input.RetryAt
	return nil
}

func (repository *fakeAutomationRepository) SetOwnerAttentionPost(_ context.Context, input automationsrepo.SetOwnerAttentionPostInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.delivery.ClaimToken != input.ClaimToken || repository.delivery.Fence != input.Fence {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrConflict
	}
	repository.setPostCalls++
	repository.delivery.MattermostPostID = input.MattermostPostID
	repository.delivery.ClaimToken = ""
	repository.delivery.ClaimedAt = time.Time{}
	repository.delivery.LeaseExpiresAt = time.Time{}
	return repository.delivery, nil
}

func (repository *fakeAutomationRepository) FailRun(_ context.Context, _ automationsrepo.FailRunInput) (entity.ScheduledRun, error) {
	failed := repository.run
	failed.Status = string(value.AutomationRunStatusFailed)
	failed.Outcome = string(value.AutomationRunOutcomeFailed)
	return failed, nil
}

type fakeAutomationCatalog struct {
	project      entity.Project
	role         entity.AgentRole
	chat         entity.Chat
	participants []entity.ChatParticipant
	repositories []entity.ProjectRepository
	session      entity.AgentSession
}

func (catalog *fakeAutomationCatalog) GetProject(context.Context, int64) (entity.Project, error) {
	return catalog.project, nil
}

func (catalog *fakeAutomationCatalog) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return catalog.role, nil
}

func (catalog *fakeAutomationCatalog) GetChat(context.Context, int64) (entity.Chat, error) {
	return catalog.chat, nil
}

func (catalog *fakeAutomationCatalog) ListChatParticipants(context.Context, int64) ([]entity.ChatParticipant, error) {
	return catalog.participants, nil
}

func (catalog *fakeAutomationCatalog) ListProjectRepositories(context.Context, int64) ([]entity.ProjectRepository, error) {
	return catalog.repositories, nil
}

func (catalog *fakeAutomationCatalog) GetAgentSession(context.Context, string) (entity.AgentSession, error) {
	return catalog.session, nil
}

type fakeAutomationDispatcher struct {
	request AgentTurnRequest
	queued  AgentTurnQueued
	calls   int
}

func (dispatcher *fakeAutomationDispatcher) EnqueueAgentTurn(_ context.Context, request AgentTurnRequest) (AgentTurnQueued, error) {
	dispatcher.calls++
	dispatcher.request = request
	return dispatcher.queued, nil
}

type fakeAutomationPublisher struct {
	input            MattermostThreadPostInput
	ref              MattermostPostRef
	calls            int
	errors           []error
	idempotencyIDs   []string
	reconcileInput   MattermostThreadPostInput
	reconcileCalls   int
	reconcileErrors  []error
	reconcileIDs     []string
	reconcileStarted chan struct{}
	reconcileRelease chan struct{}
}

func (publisher *fakeAutomationPublisher) ReconcileOrPostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.reconcileCalls++
	publisher.reconcileInput = input
	publisher.reconcileIDs = append(publisher.reconcileIDs, input.IdempotencyID)
	if publisher.reconcileStarted != nil {
		close(publisher.reconcileStarted)
		publisher.reconcileStarted = nil
	}
	if publisher.reconcileRelease != nil {
		<-publisher.reconcileRelease
	}
	if err := popAutomationError(&publisher.reconcileErrors); err != nil {
		return MattermostPostRef{}, err
	}
	return publisher.ref, nil
}

type fakeDeliveryQueueRepository struct {
	automationsrepo.Repository
	mu          sync.Mutex
	deliveries  []entity.AutomationOwnerAttentionDelivery
	nextAttempt map[int64]time.Time
}

func newFakeDeliveryQueueRepository(t *testing.T, count int) *fakeDeliveryQueueRepository {
	t.Helper()
	repository := &fakeDeliveryQueueRepository{deliveries: make([]entity.AutomationOwnerAttentionDelivery, 0, count), nextAttempt: make(map[int64]time.Time)}
	for index := 1; index <= count; index++ {
		runID := fmt.Sprintf("scheduled-run-%032x", index)
		deliveryID := fmt.Sprintf("%026d", index)
		message := fmt.Sprintf("Карточка %d\n\n#notrigger", index)
		props := map[string]any{
			"matter_codex_event":                 "automation_owner_attention",
			"matter_codex_callback_delivery_id":  deliveryID,
			"matter_codex_automation_run_id":     runID,
			"matter_codex_process_run_id":        fmt.Sprintf("process-%d", index),
			"matter_codex_human_decision_status": "pending",
		}
		propsJSON, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := callbackDeliveryPayloadHash("channel", "root", message, props)
		if err != nil {
			t.Fatal(err)
		}
		repository.deliveries = append(repository.deliveries, entity.AutomationOwnerAttentionDelivery{
			AttentionID: int64(index), ScheduledRunID: int64(index), ScheduledRunPublicID: runID,
			MattermostChannelID: "channel", MattermostRootPostID: "root", Status: "open",
			DeliveryID: deliveryID, DeliveryMessage: message, DeliveryPropsJSON: propsJSON, DeliveryPayloadSHA256: digest,
		})
	}
	return repository
}

func (repository *fakeDeliveryQueueRepository) ClaimOwnerAttentionDelivery(_ context.Context, input automationsrepo.ClaimOwnerAttentionDeliveryInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for index := range repository.deliveries {
		delivery := &repository.deliveries[index]
		if delivery.MattermostPostID != "" || repository.nextAttempt[delivery.AttentionID].After(input.EligibleBefore) || (delivery.ClaimToken != "" && delivery.LeaseExpiresAt.After(input.Now)) || (input.ScheduledRunID > 0 && delivery.ScheduledRunID != input.ScheduledRunID) {
			continue
		}
		delivery.ClaimToken = input.ClaimToken
		delivery.ClaimedAt = input.Now
		delivery.LeaseExpiresAt = input.LeaseUntil
		delivery.Fence++
		return *delivery, nil
	}
	return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrNotFound
}

func (repository *fakeDeliveryQueueRepository) DeferOwnerAttentionDelivery(_ context.Context, input automationsrepo.DeferOwnerAttentionDeliveryInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	delivery := &repository.deliveries[input.AttentionID-1]
	if delivery.ClaimToken != input.ClaimToken || delivery.Fence != input.Fence {
		return automationsrepo.ErrConflict
	}
	delivery.ClaimToken = ""
	delivery.ClaimedAt = time.Time{}
	delivery.LeaseExpiresAt = time.Time{}
	repository.nextAttempt[delivery.AttentionID] = input.RetryAt
	return nil
}

func (repository *fakeDeliveryQueueRepository) SetOwnerAttentionPost(_ context.Context, input automationsrepo.SetOwnerAttentionPostInput) (entity.AutomationOwnerAttentionDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	delivery := &repository.deliveries[input.AttentionID-1]
	if delivery.ClaimToken != input.ClaimToken || delivery.Fence != input.Fence || delivery.MattermostPostID != "" {
		return entity.AutomationOwnerAttentionDelivery{}, automationsrepo.ErrConflict
	}
	delivery.MattermostPostID = input.MattermostPostID
	delivery.ClaimToken = ""
	delivery.ClaimedAt = time.Time{}
	delivery.LeaseExpiresAt = time.Time{}
	return *delivery, nil
}

func (repository *fakeDeliveryQueueRepository) deliveredCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	count := 0
	for _, delivery := range repository.deliveries {
		if delivery.MattermostPostID != "" {
			count++
		}
	}
	return count
}

type fakeDeliveryQueuePublisher struct {
	mu                sync.Mutex
	failDeliveryID    string
	failed            bool
	calls             map[string]int
	currentConcurrent int
	maxConcurrent     int
}

func (publisher *fakeDeliveryQueuePublisher) ReconcileOrPostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.mu.Lock()
	publisher.calls[input.IdempotencyID]++
	publisher.currentConcurrent++
	if publisher.currentConcurrent > publisher.maxConcurrent {
		publisher.maxConcurrent = publisher.currentConcurrent
	}
	shouldFail := input.IdempotencyID == publisher.failDeliveryID && !publisher.failed
	if shouldFail {
		publisher.failed = true
	}
	publisher.mu.Unlock()
	time.Sleep(time.Millisecond)
	publisher.mu.Lock()
	publisher.currentConcurrent--
	publisher.mu.Unlock()
	if shouldFail {
		return MattermostPostRef{}, errors.New("synthetic head delivery failure")
	}
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "post-" + input.IdempotencyID}, nil
}

func (publisher *fakeDeliveryQueuePublisher) PostThreadMessage(context.Context, MattermostThreadPostInput) (MattermostPostRef, error) {
	return MattermostPostRef{}, errors.New("unexpected non-idempotent publish")
}

func (publisher *fakeAutomationPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.calls++
	publisher.input = input
	publisher.idempotencyIDs = append(publisher.idempotencyIDs, input.IdempotencyID)
	if err := popAutomationError(&publisher.errors); err != nil {
		return MattermostPostRef{}, err
	}
	return publisher.ref, nil
}

func popAutomationError(items *[]error) error {
	if len(*items) == 0 {
		return nil
	}
	err := (*items)[0]
	*items = (*items)[1:]
	return err
}
