package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestAgentSessionClaimCreatesInitialStatusPost(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://mattermost.example/actions",
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	if !claim.HasTurn || claim.TurnID != 1 {
		t.Fatalf("claim = %#v", claim)
	}
	if len(publisher.cards) != 1 {
		t.Fatalf("cards = %#v", publisher.cards)
	}
	card := publisher.cards[0]
	if card.Message != "matter-codex agent turn status #notrigger" {
		t.Fatalf("card message = %q", card.Message)
	}
	if card.Props["matter_codex_event"] != "agent_status" || card.Props["status"] != agentSessionTurnRunning {
		t.Fatalf("card props = %#v", card.Props)
	}
	if len(card.Actions) != 1 || card.Actions[0].ID != "stopturn" {
		t.Fatalf("card actions = %#v", card.Actions)
	}
	if !strings.Contains(card.Text, "run-1") {
		t.Fatalf("status text = %q", card.Text)
	}
	if !strings.Contains(card.Text, "OpenAI account: `main`") {
		t.Fatalf("status text misses account = %q", card.Text)
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAgentSessionTurn() error = %v", err)
	}
	if turn.MattermostStatusPostID != "card-root-1" {
		t.Fatalf("MattermostStatusPostID = %q", turn.MattermostStatusPostID)
	}
}

func TestAgentSessionClaimReturnsAlreadyRunningTurnAfterLostResponse(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://mattermost.example/actions",
		StorageReady:    true,
		RuntimeReady:    true,
	})

	first, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() first error = %v", err)
	}
	second, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() retry error = %v", err)
	}
	if !second.HasTurn || second.TurnID != first.TurnID || second.RunID != first.RunID || second.Prompt != first.Prompt {
		t.Fatalf("retry claim = %#v, first = %#v", second, first)
	}
	session := store.agentSessions["session-1"]
	if session.ActiveTurnID != first.TurnID || session.ActiveRunID != first.RunID || session.Status != agentSessionStatusRunning {
		t.Fatalf("session = %#v", session)
	}
	if len(publisher.cards) != 1 {
		t.Fatalf("status cards should not duplicate on retry: %#v", publisher.cards)
	}
}

func TestAgentSessionRuntimeUpdateDoesNotDowngradeRunningSessionToIdle(t *testing.T) {
	store, _, _ := agentSessionStatusTestDeps()
	store.agentSessions["session-1"] = withActiveTurn(store.agentSessions["session-1"], 1, "run-1")
	session := store.agentSessions["session-1"]
	session.Status = agentSessionStatusRunning
	store.agentSessions["session-1"] = session

	updated, err := store.UpdateAgentSessionRuntime(context.Background(), adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey:          "session-1",
		Status:              agentSessionStatusIdle,
		KubernetesNamespace: "matter-kodex-prod",
		PodName:             "mc-session-1",
		PVCName:             "mc-session-ws-1",
		TokenSecretRef:      "matter-codex-session-1",
	})
	if err != nil {
		t.Fatalf("UpdateAgentSessionRuntime() error = %v", err)
	}
	if updated.Status != agentSessionStatusRunning || updated.ActiveTurnID != 1 || updated.ActiveRunID != "run-1" {
		t.Fatalf("session = %#v", updated)
	}
	if updated.PodName != "mc-session-1" || updated.PVCName != "mc-session-ws-1" || updated.TokenSecretRef != "matter-codex-session-1" {
		t.Fatalf("runtime refs were not updated: %#v", updated)
	}
}

func TestAgentSessionUpdateTurnStatusPostsProgressWithoutEditingStatusCard(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.agentSessions["session-1"] = withActiveTurn(store.agentSessions["session-1"], 1, "run-1")
	store.sessionTurns[0].Status = agentSessionTurnRunning
	store.sessionTurns[0].MattermostStatusPostID = "status-post-1"
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	first, err := svc.UpdateTurnStatus(context.Background(), "session-1", "session-token", "Планирую работу")
	if err != nil {
		t.Fatalf("UpdateTurnStatus() first error = %v", err)
	}
	second, err := svc.UpdateTurnStatus(context.Background(), "session-1", "session-token", "Проверяю результат")
	if err != nil {
		t.Fatalf("UpdateTurnStatus() second error = %v", err)
	}
	if first.PostID != "reply-root-1" || second.PostID != first.PostID {
		t.Fatalf("post ids first=%q second=%q", first.PostID, second.PostID)
	}
	if len(publisher.posts) != 2 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if len(publisher.updates) != 0 || len(publisher.cardUpdates) != 0 {
		t.Fatalf("updates=%#v cardUpdates=%#v", publisher.updates, publisher.cardUpdates)
	}
	if !strings.Contains(publisher.posts[0].Message, "Планирую работу") || !strings.Contains(publisher.posts[0].Message, "#notrigger") {
		t.Fatalf("first progress post = %#v", publisher.posts[0])
	}
	if !strings.Contains(publisher.posts[1].Message, "Проверяю результат") || !strings.Contains(publisher.posts[1].Message, "#notrigger") {
		t.Fatalf("second progress post = %#v", publisher.posts[1])
	}
	for _, post := range publisher.posts {
		if post.Props["matter_codex_event"] != "agent_progress" || post.Props["turn_id"] != int64(1) || post.Props["run_id"] != "run-1" {
			t.Fatalf("progress props = %#v", post.Props)
		}
	}
}

func TestAgentSessionPostFallsBackWhenRoleTokenIsInvalid(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {
			ID:               1,
			ProjectID:        1,
			RoleID:           1,
			Username:         "manager",
			MattermostUserID: "manager-user",
			TokenSecretRef:   "role-token-secret",
			Status:           "configured",
		},
	}
	runner.botTokenSecrets["role-token-secret"] = "expired-role-token"
	publisher.postWithTokenErr = errors.New("invalid or expired session")
	store.agentSessions["session-1"] = withActiveTurn(store.agentSessions["session-1"], 1, "run-1")
	store.sessionTurns[0].Status = agentSessionTurnRunning
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	ref, err := svc.UpdateTurnStatus(context.Background(), "session-1", "session-token", "Планирую работу")
	if err != nil {
		t.Fatalf("UpdateTurnStatus() error = %v", err)
	}
	if ref.PostID != "reply-root-1" {
		t.Fatalf("ref = %#v", ref)
	}
	if publisher.postWithTokenCalls != 1 {
		t.Fatalf("postWithTokenCalls = %d", publisher.postWithTokenCalls)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "Планирую работу") || !strings.Contains(publisher.posts[0].Message, "#notrigger") {
		t.Fatalf("fallback posts = %#v", publisher.posts)
	}
}

func TestAgentSessionPostThreadUpdateAddsNoTriggerProps(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {
			ID:               1,
			ProjectID:        1,
			RoleID:           1,
			Username:         "manager",
			MattermostUserID: "manager-user",
			TokenSecretRef:   "role-token-secret",
			Status:           "configured",
		},
	}
	runner.botTokenSecrets["role-token-secret"] = "expired-role-token"
	publisher.postWithTokenErr = errors.New("invalid or expired session")
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	ref, err := svc.PostThreadUpdate(context.Background(), "session-1", "session-token", "Пишу промежуточный статус")
	if err != nil {
		t.Fatalf("PostThreadUpdate() error = %v", err)
	}
	if ref.PostID != "reply-root-1" {
		t.Fatalf("ref = %#v", ref)
	}
	if publisher.postWithTokenCalls != 1 {
		t.Fatalf("postWithTokenCalls = %d", publisher.postWithTokenCalls)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	post := publisher.posts[0]
	if !strings.Contains(post.Message, "Пишу промежуточный статус") || !strings.Contains(post.Message, "#notrigger") {
		t.Fatalf("post message = %q", post.Message)
	}
	if post.Props["matter_codex_event"] != "agent_progress" || post.Props["turn_id"] != int64(0) {
		t.Fatalf("post props = %#v", post.Props)
	}
}

func TestAgentSessionCompleteUpdatesStatusWithCodexLimits(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:       claim.TurnID,
		RunID:        claim.RunID,
		Status:       agentSessionTurnSucceeded,
		FinalMessage: "done",
		Artifacts: map[string]string{
			"openai-account": "main",
			"codex-limits":   "🕔 5h ████████  96% · 24.06 19:31\n📅 7d ███████░  82% · 25.06 03:42",
		},
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if len(publisher.cards) != 1 {
		t.Fatalf("cards = %#v", publisher.cards)
	}
	if len(publisher.cardUpdates) != 1 {
		t.Fatalf("cardUpdates = %#v", publisher.cardUpdates)
	}
	card := publisher.cardUpdates[0]
	if card.PostID != "card-root-1" {
		t.Fatalf("card update post id = %q", card.PostID)
	}
	message := card.Text
	for _, expected := range []string{"completed", "OpenAI account: `main`", "Codex limits:", "🕔 5h", "📅 7d"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("status message misses %q: %q", expected, message)
		}
	}
}

func TestAgentSessionProviderPolicyBlockStopsSessionWithoutRawProviderError(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:       claim.TurnID,
		RunID:        claim.RunID,
		Status:       agentSessionTurnFailed,
		ErrorMessage: "raw provider response that must not be posted",
		Artifacts: map[string]string{
			agentTurnArtifactFailureCode: agentTurnFailureProviderPolicyBlocked,
			"openai-account":             "main",
		},
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if store.agentSessions["session-1"].Status != agentSessionStatusBlocked {
		t.Fatalf("session = %#v", store.agentSessions["session-1"])
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), claim.TurnID)
	if err != nil || turn.Status != agentSessionTurnBlocked {
		t.Fatalf("turn = %#v error=%v", turn, err)
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "cyber safety") || strings.Contains(publisher.posts[0].Message, "raw provider response") {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	exit, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil || !exit.Exit {
		t.Fatalf("blocked session claim = %#v error=%v", exit, err)
	}
}

func TestAgentSessionSystemStatusUpdatesInitialCardWithCodexLimits(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://mattermost.example/actions",
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	ref, err := svc.UpdateTurnSystemStatus(context.Background(), "session-1", "session-token", UpdateAgentSessionTurnStatusCommand{
		RunID:         claim.RunID,
		Phase:         agentSessionTurnRunning,
		OpenAIAccount: "main",
		CodexLimits:   "5h 96%\n7d 82%",
	})
	if err != nil {
		t.Fatalf("UpdateTurnSystemStatus() error = %v", err)
	}
	if len(publisher.cards) != 1 || len(publisher.cardUpdates) != 1 {
		t.Fatalf("cards=%#v cardUpdates=%#v", publisher.cards, publisher.cardUpdates)
	}
	if ref.PostID != "card-root-1" || publisher.cardUpdates[0].PostID != "card-root-1" {
		t.Fatalf("ref=%#v update=%#v", ref, publisher.cardUpdates[0])
	}
	if len(publisher.cardUpdates[0].Actions) != 1 || publisher.cardUpdates[0].Actions[0].ID != "stopturn" {
		t.Fatalf("updated card actions = %#v", publisher.cardUpdates[0].Actions)
	}
	text := publisher.cardUpdates[0].Text
	for _, expected := range []string{"agent @manager started", "OpenAI account: `main`", "Codex limits:", "5h 96%", "7d 82%"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("updated card misses %q: %q", expected, text)
		}
	}
}

func TestAgentSessionSystemStatusShowsCapacityRetryOnExistingCard(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://mattermost.example/actions",
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	_, err = svc.UpdateTurnSystemStatus(context.Background(), "session-1", "session-token", UpdateAgentSessionTurnStatusCommand{
		RunID:             claim.RunID,
		Phase:             agentSessionTurnRetrying,
		OpenAIAccount:     "main",
		CodexLimits:       "5h 83%",
		RetryAttempt:      1,
		RetryMaxAttempts:  3,
		RetryDelaySeconds: 60,
	})
	if err != nil {
		t.Fatalf("UpdateTurnSystemStatus() error = %v", err)
	}
	if len(publisher.cards) != 1 || len(publisher.cardUpdates) != 1 {
		t.Fatalf("cards=%#v cardUpdates=%#v", publisher.cards, publisher.cardUpdates)
	}
	card := publisher.cardUpdates[0]
	for _, expected := range []string{"модель временно перегружена", "Повтор `1/3`", "через `1` мин", "OpenAI account: `main`", "5h 83%"} {
		if !strings.Contains(card.Text, expected) {
			t.Fatalf("capacity retry card misses %q: %q", expected, card.Text)
		}
	}
	if len(card.Actions) != 1 || card.Actions[0].ID != "stopturn" {
		t.Fatalf("capacity retry actions = %#v", card.Actions)
	}
}

func TestAgentSessionCapacityExhaustionAddsManualRetryAction(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		MenuActionURL:   "https://mattermost.example/actions",
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:         claim.TurnID,
		RunID:          claim.RunID,
		Status:         agentSessionTurnFailed,
		ErrorMessage:   "Codex model remained at capacity",
		CodexSessionID: "codex-session-1",
		Artifacts: map[string]string{
			agentTurnArtifactCapacityRetriesExhausted: "true",
			agentTurnArtifactCapacityRetryCount:       "3",
		},
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if len(publisher.posts) != 0 {
		t.Fatalf("capacity exhaustion should only use the system card, posts=%#v", publisher.posts)
	}
	if len(publisher.cardUpdates) != 1 {
		t.Fatalf("cardUpdates = %#v", publisher.cardUpdates)
	}
	card := publisher.cardUpdates[0]
	for _, expected := range []string{"после `3` автоматических повторов", "Работа и Codex session сохранены", "Запустить ещё раз"} {
		if !strings.Contains(card.Text, expected) {
			t.Fatalf("capacity exhausted card misses %q: %q", expected, card.Text)
		}
	}
	if len(card.Actions) != 1 || card.Actions[0].ID != "retryturn" || card.Actions[0].Context["action"] != "retry_turn" {
		t.Fatalf("capacity exhausted actions = %#v", card.Actions)
	}
}

func TestAgentSessionManualCapacityRetryUsesDispatcher(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	session := store.agentSessions["session-1"]
	session.Status = agentSessionStatusError
	session.CodexSessionID = "codex-session-1"
	store.agentSessions["session-1"] = session
	store.sessionTurns[0].Status = agentSessionTurnFailed
	store.sessionTurns[0].Artifacts = `{"codex-capacity-retries-exhausted":"true","codex-capacity-retry-count":"3"}`
	dispatcher := &fakeAgentTurnDispatcher{retryQueued: AgentTurnQueued{RunID: "run-retry-1", TurnID: 2, SessionKey: "session-1"}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		TurnDispatcher:  dispatcher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	result, err := svc.RetryFailedTurn(context.Background(), RetryAgentSessionTurnCommand{
		TurnID:    1,
		UserID:    "owner-user",
		UserName:  "owner",
		ChannelID: "channel-1",
		PostID:    "status-post-1",
	})
	if err != nil {
		t.Fatalf("RetryFailedTurn() error = %v", err)
	}
	if dispatcher.retryCalls != 1 || dispatcher.retryRequest.Session.ID != 1 || dispatcher.retryRequest.Turn.ID != 1 || dispatcher.retryRequest.UserName != "owner" {
		t.Fatalf("retry dispatcher = %#v", dispatcher)
	}
	if result.RunID != "run-retry-1" || result.Card == nil || result.Card.PostID != "status-post-1" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Message, "run-retry-1") {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestAgentSessionCompleteDoesNotPostFYIToRequester(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.sessionTurns[0].UserName = "owner"
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:       claim.TurnID,
		RunID:        claim.RunID,
		Status:       agentSessionTurnSucceeded,
		FinalMessage: "done",
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if len(publisher.cards) != 1 || len(publisher.cardUpdates) != 1 {
		t.Fatalf("cards=%#v cardUpdates=%#v", publisher.cards, publisher.cardUpdates)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if publisher.posts[0].Message != "done" {
		t.Fatalf("final message = %q", publisher.posts[0].Message)
	}
}

func TestAgentSessionCompleteIsIdempotentForTerminalTurn(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.sessionTurns[0].UserName = "owner"
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	command := CompleteAgentSessionTurnCommand{
		TurnID:         claim.TurnID,
		RunID:          claim.RunID,
		Status:         agentSessionTurnSucceeded,
		FinalMessage:   "done",
		CodexSessionID: "codex-session-1",
	}
	if err := svc.CompleteTurn(context.Background(), "session-1", "session-token", command); err != nil {
		t.Fatalf("CompleteTurn() first error = %v", err)
	}
	postCount := len(publisher.posts)
	cardUpdateCount := len(publisher.cardUpdates)

	if err := svc.CompleteTurn(context.Background(), "session-1", "session-token", command); err != nil {
		t.Fatalf("CompleteTurn() retry error = %v", err)
	}
	if len(publisher.posts) != postCount || len(publisher.cardUpdates) != cardUpdateCount {
		t.Fatalf("retry duplicated output: posts=%#v cardUpdates=%#v", publisher.posts, publisher.cardUpdates)
	}
	session := store.agentSessions["session-1"]
	if session.ActiveTurnID != 0 || session.Status != agentSessionStatusIdle || session.CodexSessionID != "codex-session-1" {
		t.Fatalf("session = %#v", session)
	}
}

func TestAgentSessionCompleteSplitsLongFinalMessageUsingMattermostLimit(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.postMessageMaxRunes = 1100
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	longFinal := strings.Repeat("0123456789", 260)
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:       claim.TurnID,
		RunID:        claim.RunID,
		Status:       agentSessionTurnSucceeded,
		FinalMessage: longFinal,
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if len(publisher.posts) < 3 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	for index, post := range publisher.posts {
		if len([]rune(post.Message)) > store.postMessageMaxRunes {
			t.Fatalf("post %d length = %d, want <= %d", index+1, len([]rune(post.Message)), store.postMessageMaxRunes)
		}
		if !strings.Contains(post.Message, "Часть ") {
			t.Fatalf("post %d misses chunk header: %q", index+1, post.Message)
		}
	}
}

func TestAgentSessionCompleteSkipsFYIWhenRequesterIsAgentBot(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.sessionTurns[0].UserName = "manager"
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
	}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil {
		t.Fatalf("ClaimNextTurn() error = %v", err)
	}
	err = svc.CompleteTurn(context.Background(), "session-1", "session-token", CompleteAgentSessionTurnCommand{
		TurnID:       claim.TurnID,
		RunID:        claim.RunID,
		Status:       agentSessionTurnSucceeded,
		FinalMessage: "done",
	})
	if err != nil {
		t.Fatalf("CompleteTurn() error = %v", err)
	}
	if len(publisher.cards) != 1 || len(publisher.cardUpdates) != 1 {
		t.Fatalf("cards=%#v cardUpdates=%#v", publisher.cards, publisher.cardUpdates)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if publisher.posts[0].Message != "done" {
		t.Fatalf("final message = %q", publisher.posts[0].Message)
	}
}

func TestAgentSessionRequestAgentPostsSystemAuditMessage(t *testing.T) {
	now := time.Now().UTC()
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	store.agentSessions = map[string]entity.AgentSession{
		"session-1": {
			ID:                   1,
			SessionKey:           "session-1",
			ProjectID:            1,
			ChatID:               1,
			RoleID:               1,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: "root-1",
			Status:               agentSessionStatusIdle,
			TokenSecretRef:       "session-secret",
			TTLSeconds:           defaultThreadSessionTTLSeconds,
			LastActivityAt:       now,
			ExpiresAt:            now.Add(time.Hour),
		},
	}
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"session-secret": "session-token"}}
	publisher := &fakeThreadPublisher{}
	roleBotManager := &fakeRoleBotManager{}
	dispatcher := &fakeAgentTurnDispatcher{queued: AgentTurnQueued{
		RunID:      "run-sre-1",
		TurnID:     7,
		SessionKey: "session-sre",
		Role:       store.agentRoles[2],
	}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		RoleBotManager:  roleBotManager,
		TurnDispatcher:  dispatcher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	result, err := svc.RequestAgent(context.Background(), "session-1", "session-token", "@sre", "Проверь staging deploy.\n\n- Не печатай секреты.")
	if err != nil {
		t.Fatalf("RequestAgent() error = %v", err)
	}
	if result.RequestedRunID != "run-sre-1" || result.RequestedRoleName != "sre" || result.AuditPostID == "" {
		t.Fatalf("result = %#v", result)
	}
	if dispatcher.calls != 1 || dispatcher.request.Role.Name != "sre" || dispatcher.request.UserName != "manager" {
		t.Fatalf("dispatcher request = %#v", dispatcher.request)
	}
	if roleBotManager.channelMemberTeam != "platform" || roleBotManager.channelMemberChannelID != "channel-1" || roleBotManager.channelMemberUserID != "sre-user" {
		t.Fatalf("role bot channel member call = team %q channel %q user %q", roleBotManager.channelMemberTeam, roleBotManager.channelMemberChannelID, roleBotManager.channelMemberUserID)
	}
	for _, expected := range []string{"# Запрос к агенту через MatterCodex", "- Инициатор: @manager", "- Целевой агент: @sre", "Проверь staging deploy.", "Не печатай секреты."} {
		if !strings.Contains(dispatcher.request.UserMessage, expected) {
			t.Fatalf("dispatcher prompt missing %q: %q", expected, dispatcher.request.UserMessage)
		}
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	post := publisher.posts[0]
	if post.ChannelID != "channel-1" || post.RootPostID != "root-1" {
		t.Fatalf("post binding = %#v", post)
	}
	for _, expected := range []string{"matter-codex: @manager запустил @sre", "```markdown", "Проверь staging deploy.", "Не печатай секреты."} {
		if !strings.Contains(post.Message, expected) {
			t.Fatalf("audit message missing %q: %q", expected, post.Message)
		}
	}
	if post.Props["matter_codex_event"] != "agent_request" || post.Props["source_agent"] != "manager" || post.Props["target_agent"] != "sre" {
		t.Fatalf("props = %#v", post.Props)
	}
}

func TestAgentSessionRequestAgentGuardsEveryClusterAdminSideEffect(t *testing.T) {
	tests := []struct {
		name            string
		denyAt          int
		wantMembership  bool
		wantDispatches  int
		wantGuardOps    []string
		wantSessionKeys []string
	}{
		{name: "membership", denyAt: 1, wantGuardOps: []string{"agent_request.membership.side_effect"}, wantSessionKeys: []string{"target"}},
		{name: "enqueue", denyAt: 2, wantMembership: true, wantGuardOps: []string{"agent_request.membership.side_effect", "agent_request.enqueue.side_effect"}, wantSessionKeys: []string{"target", ""}},
		{name: "audit post", denyAt: 3, wantMembership: true, wantDispatches: 1, wantGuardOps: []string{"agent_request.membership.side_effect", "agent_request.enqueue.side_effect", "agent_request.audit.side_effect"}, wantSessionKeys: []string{"target", "", "target"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			baseStore := chatRuntimeStore()
			baseStore.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", Enabled: true}
			baseStore.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "mattercodex-admin", RoleType: "admin", KubernetesAccess: "cluster-admin", Enabled: true}
			baseStore.botIdentities = map[int64]entity.MattermostBotIdentity{
				1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
				2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "mattercodex-admin", MattermostUserID: "admin-user", Status: "configured"},
			}
			baseStore.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Control", Slug: "agents-control", ChatType: "multi_role_custom"}
			baseStore.setChatBindings(1, []int64{1, 2}, nil)
			baseStore.agentSessions = map[string]entity.AgentSession{
				"session-1": {
					ID: 1, SessionKey: "session-1", ProjectID: 1, ChatID: 1, RoleID: 1,
					SessionScope: agentSessionScopeThreadRole, MattermostChannelID: "channel-1",
					MattermostRootPostID: "root-1", Status: agentSessionStatusIdle,
					TokenSecretRef: "session-secret", TTLSeconds: defaultThreadSessionTTLSeconds,
					LastActivityAt: now, ExpiresAt: now.Add(time.Hour),
				},
			}
			store := &admittedAdminStore{fakeAdminStore: baseStore, allowed: true, denyGuardAt: test.denyAt}
			runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"session-secret": "session-token"}}
			publisher := &fakeThreadPublisher{}
			roleBotManager := &fakeRoleBotManager{}
			targetSessionKey := agentSessionKey(1, 2, agentSessionScopeThreadRole, "root-1")
			dispatcher := &fakeAgentTurnDispatcher{queued: AgentTurnQueued{
				RunID: "run-admin-1", TurnID: 7, SessionKey: targetSessionKey, Role: baseStore.agentRoles[2],
			}}
			svc := NewAgentSessionService(AgentSessionServiceConfig{
				Localizer: testLocalizer(t, texti18n.RussianLocale), Store: store, RuntimeRunner: runner,
				ThreadPublisher: publisher, RoleBotManager: roleBotManager, TurnDispatcher: dispatcher,
				StorageReady: true, RuntimeReady: true,
			})

			_, err := svc.RequestAgent(context.Background(), "session-1", "session-token", "mattercodex-admin", "Проверь состояние.")
			if !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
				t.Fatalf("RequestAgent() error = %v", err)
			}
			if (roleBotManager.channelMemberUserID != "") != test.wantMembership {
				t.Fatalf("membership side effect=%q, want=%t", roleBotManager.channelMemberUserID, test.wantMembership)
			}
			if dispatcher.calls != test.wantDispatches {
				t.Fatalf("dispatcher calls=%d, want=%d", dispatcher.calls, test.wantDispatches)
			}
			if len(publisher.posts) != 0 {
				t.Fatalf("denied audit guard опубликовал сообщения: %#v", publisher.posts)
			}
			if len(store.guardInputs) != len(test.wantGuardOps) {
				t.Fatalf("guard inputs=%#v", store.guardInputs)
			}
			for index, operation := range test.wantGuardOps {
				wantSessionKey := test.wantSessionKeys[index]
				if wantSessionKey == "target" {
					wantSessionKey = targetSessionKey
				}
				if store.guardInputs[index].Operation != operation || store.guardInputs[index].SessionKey != wantSessionKey {
					t.Fatalf("guard %d=%#v", index, store.guardInputs[index])
				}
			}
		})
	}
}

func TestAgentSessionRequestAgentMergesIntoQueuedTargetTurn(t *testing.T) {
	now := time.Now().UTC()
	store := chatRuntimeStore()
	store.agentRoles[1] = entity.AgentRole{ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", OpenAIAccountName: "main", Enabled: true}
	store.agentRoles[2] = entity.AgentRole{ID: 2, ProjectID: 1, Name: "sre", RoleType: "sre", OpenAIAccountName: "main", Enabled: true}
	store.botIdentities = map[int64]entity.MattermostBotIdentity{
		1: {ID: 1, ProjectID: 1, RoleID: 1, Username: "manager", MattermostUserID: "manager-user", Status: "configured"},
		2: {ID: 2, ProjectID: 1, RoleID: 2, Username: "sre", MattermostUserID: "sre-user", Status: "configured"},
	}
	store.chats[1] = entity.Chat{ID: 1, ProjectID: 1, MattermostChannelID: "channel-1", Name: "Ops", ChatType: "multi_role_custom"}
	store.setChatBindings(1, []int64{1, 2}, nil)
	targetSessionKey := agentSessionKey(1, 2, agentSessionScopeThreadRole, "root-1")
	store.agentSessions = map[string]entity.AgentSession{
		"session-1": {
			ID:                   1,
			SessionKey:           "session-1",
			ProjectID:            1,
			ChatID:               1,
			RoleID:               1,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: "root-1",
			Status:               agentSessionStatusIdle,
			TokenSecretRef:       "session-secret",
			TTLSeconds:           defaultThreadSessionTTLSeconds,
			LastActivityAt:       now,
			ExpiresAt:            now.Add(time.Hour),
		},
		targetSessionKey: {
			ID:                   2,
			SessionKey:           targetSessionKey,
			ProjectID:            1,
			ChatID:               1,
			RoleID:               2,
			SessionScope:         agentSessionScopeThreadRole,
			MattermostChannelID:  "channel-1",
			MattermostRootPostID: "root-1",
			Status:               agentSessionStatusRunning,
			TokenSecretRef:       "target-secret",
			TTLSeconds:           defaultThreadSessionTTLSeconds,
			LastActivityAt:       now,
			ExpiresAt:            now.Add(time.Hour),
		},
	}
	store.sessionTurns = []entity.AgentSessionTurn{{
		ID:                   10,
		SessionID:            2,
		RunID:                "queued-sre",
		MattermostChannelID:  "channel-1",
		MattermostRootPostID: "root-1",
		MattermostPostID:     "root-1",
		UserName:             "architect",
		Message:              "# Запрос к агенту через MatterCodex\n\n- Инициатор: @architect\n\nпервый запрос",
		Status:               agentSessionTurnQueued,
	}}
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"session-secret": "session-token", "target-secret": "target-token"}}
	publisher := &fakeThreadPublisher{}
	dispatcher := &fakeAgentTurnDispatcher{queued: AgentTurnQueued{RunID: "should-not-be-used"}}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.RussianLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		TurnDispatcher:  dispatcher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	result, err := svc.RequestAgent(context.Background(), "session-1", "session-token", "sre", "второй запрос от manager")
	if err != nil {
		t.Fatalf("RequestAgent() error = %v", err)
	}
	if result.RequestedRunID != "queued-sre" || result.TargetSessionKey != targetSessionKey {
		t.Fatalf("result = %#v", result)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher was called: %#v", dispatcher)
	}
	if len(store.sessionTurns) != 1 {
		t.Fatalf("sessionTurns = %#v", store.sessionTurns)
	}
	merged := store.sessionTurns[0].Message
	for _, expected := range []string{"первый запрос", "# Дополнительный запрос к этому же занятому агенту", "- Инициатор: @manager", "- Целевой агент: @sre", "второй запрос от manager", "объединен"} {
		if !strings.Contains(merged, expected) {
			t.Fatalf("merged prompt missing %q:\n%s", expected, merged)
		}
	}
	if len(publisher.posts) != 1 || !strings.Contains(publisher.posts[0].Message, "matter-codex: @manager запустил @sre") {
		t.Fatalf("audit posts = %#v", publisher.posts)
	}
}

func TestAgentSessionStopRunningTurnCancelsAndDeletesPod(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	session := withActiveTurn(store.agentSessions["session-1"], 1, "run-1")
	session.KubernetesNamespace = "mattermost"
	session.PodName = "mc-session-session-1"
	session.PVCName = "mc-session-ws-session-1"
	session.TokenSecretRef = "matter-codex-session-session-1"
	store.agentSessions["session-1"] = session
	store.sessionTurns[0].Status = agentSessionTurnRunning
	store.sessionTurns[0].MattermostStatusPostID = "status-post-1"
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	result, err := svc.StopAgentSessionTurns(context.Background(), StopAgentSessionTurnsCommand{
		TurnIDs:   []int64{1},
		UserID:    "owner-user",
		UserName:  "owner",
		ChannelID: "channel-1",
		PostID:    "queue-card-1",
	})
	if err != nil {
		t.Fatalf("StopAgentSessionTurns() error = %v", err)
	}
	if !strings.Contains(result.Message, "stopped agent turns: `1`") || result.Card == nil {
		t.Fatalf("result = %#v", result)
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAgentSessionTurn() error = %v", err)
	}
	if turn.Status != agentSessionTurnCanceled {
		t.Fatalf("turn status = %q", turn.Status)
	}
	session = store.agentSessions["session-1"]
	if session.Status != agentSessionStatusIdle || session.PodName != "" || session.TokenSecretRef != "" || runner.cleanedSessionKey != "session-1" {
		t.Fatalf("session=%#v runner=%#v", session, runner)
	}
	if len(publisher.cardUpdates) != 1 || !strings.Contains(publisher.cardUpdates[0].Text, "turn stopped") {
		t.Fatalf("cardUpdates = %#v", publisher.cardUpdates)
	}
}

func TestAgentSessionStopLastQueuedTurnResetsIdleRuntime(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	session := store.agentSessions["session-1"]
	session.KubernetesNamespace = "mattermost"
	session.PodName = "mc-session-session-1"
	session.PVCName = "mc-session-ws-session-1"
	session.TokenSecretRef = "matter-codex-session-session-1"
	store.agentSessions["session-1"] = session
	store.sessionTurns[0].MattermostStatusPostID = "status-post-1"
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer:       testLocalizer(t, texti18n.DefaultLocale),
		Store:           store,
		RuntimeRunner:   runner,
		ThreadPublisher: publisher,
		StorageReady:    true,
		RuntimeReady:    true,
	})

	result, err := svc.StopAgentSessionTurns(context.Background(), StopAgentSessionTurnsCommand{
		TurnIDs:   []int64{1},
		UserID:    "owner-user",
		UserName:  "owner",
		ChannelID: "channel-1",
		PostID:    "queue-card-1",
	})
	if err != nil {
		t.Fatalf("StopAgentSessionTurns() error = %v", err)
	}
	if !strings.Contains(result.Message, "stopped agent turns: `1`") {
		t.Fatalf("result = %#v", result)
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAgentSessionTurn() error = %v", err)
	}
	if turn.Status != agentSessionTurnCanceled {
		t.Fatalf("turn status = %q", turn.Status)
	}
	session = store.agentSessions["session-1"]
	if session.Status != agentSessionStatusIdle || session.PodName != "" || session.PVCName != "" || session.TokenSecretRef != "" || runner.cleanedSessionKey != "session-1" {
		t.Fatalf("session=%#v runner=%#v", session, runner)
	}
	if len(publisher.cardUpdates) != 1 || !strings.Contains(publisher.cardUpdates[0].Text, "turn stopped") {
		t.Fatalf("cardUpdates = %#v", publisher.cardUpdates)
	}
}

func agentSessionStatusTestDeps() (*fakeAdminStore, *fakeRuntimeRunner, *fakeThreadPublisher) {
	now := time.Now().UTC()
	store := &fakeAdminStore{
		agentRoles: map[int64]entity.AgentRole{
			1: {
				ID:                1,
				ProjectID:         1,
				Name:              "manager",
				RoleType:          "manager",
				OpenAIAccountName: "main",
				Enabled:           true,
			},
		},
		agentSessions: map[string]entity.AgentSession{
			"session-1": {
				ID:                   1,
				SessionKey:           "session-1",
				ProjectID:            1,
				ChatID:               1,
				RoleID:               1,
				SessionScope:         agentSessionScopeThreadRole,
				MattermostChannelID:  "channel-1",
				MattermostRootPostID: "root-1",
				Status:               agentSessionStatusIdle,
				TokenSecretRef:       "session-secret",
				TTLSeconds:           defaultThreadSessionTTLSeconds,
				LastActivityAt:       now,
				ExpiresAt:            now.Add(time.Hour),
			},
		},
		sessionTurns: []entity.AgentSessionTurn{
			{
				ID:                   1,
				SessionID:            1,
				RunID:                "run-1",
				MattermostChannelID:  "channel-1",
				MattermostRootPostID: "root-1",
				MattermostPostID:     "source-1",
				Message:              "do work",
				Status:               agentSessionTurnQueued,
			},
		},
	}
	runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"session-secret": "session-token"}}
	publisher := &fakeThreadPublisher{}
	return store, runner, publisher
}

func withActiveTurn(session entity.AgentSession, turnID int64, runID string) entity.AgentSession {
	session.Status = agentSessionStatusRunning
	session.ActiveTurnID = turnID
	session.ActiveRunID = runID
	return session
}

type fakeAgentTurnDispatcher struct {
	request      AgentTurnRequest
	queued       AgentTurnQueued
	calls        int
	retryRequest AgentTurnRetryRequest
	retryQueued  AgentTurnQueued
	retryCalls   int
}

func (dispatcher *fakeAgentTurnDispatcher) EnqueueAgentTurn(_ context.Context, request AgentTurnRequest) (AgentTurnQueued, error) {
	dispatcher.calls++
	dispatcher.request = request
	return dispatcher.queued, nil
}

func (dispatcher *fakeAgentTurnDispatcher) RetryAgentTurn(_ context.Context, request AgentTurnRetryRequest) (AgentTurnQueued, error) {
	dispatcher.retryCalls++
	dispatcher.retryRequest = request
	if dispatcher.retryQueued.RunID != "" {
		return dispatcher.retryQueued, nil
	}
	return dispatcher.queued, nil
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
