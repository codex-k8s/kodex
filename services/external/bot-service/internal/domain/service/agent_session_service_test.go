package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestAgentSessionCompletePostsFYIToRequester(t *testing.T) {
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
	if len(publisher.posts) != 2 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if publisher.posts[0].Message != "done" {
		t.Fatalf("final message = %q", publisher.posts[0].Message)
	}
	if publisher.posts[1].Message != "@owner fyi: task complete 👆🏻" {
		t.Fatalf("fyi message = %q", publisher.posts[1].Message)
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
	if dispatcher.request.Role.Name != "sre" || dispatcher.request.UserName != "manager" || dispatcher.request.UserMessage != "Проверь staging deploy.\n\n- Не печатай секреты." {
		t.Fatalf("dispatcher request = %#v", dispatcher.request)
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
	request AgentTurnRequest
	queued  AgentTurnQueued
}

func (dispatcher *fakeAgentTurnDispatcher) EnqueueAgentTurn(_ context.Context, request AgentTurnRequest) (AgentTurnQueued, error) {
	dispatcher.request = request
	return dispatcher.queued, nil
}

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
