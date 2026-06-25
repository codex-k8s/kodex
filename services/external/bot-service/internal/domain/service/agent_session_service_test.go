package service

import (
	"context"
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
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if !strings.Contains(publisher.posts[0].Message, "run-1") {
		t.Fatalf("status message = %q", publisher.posts[0].Message)
	}
	if !strings.Contains(publisher.posts[0].Message, "OpenAI account: `main`") {
		t.Fatalf("status message misses account = %q", publisher.posts[0].Message)
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAgentSessionTurn() error = %v", err)
	}
	if turn.MattermostStatusPostID != "reply-root-1" {
		t.Fatalf("MattermostStatusPostID = %q", turn.MattermostStatusPostID)
	}
}

func TestAgentSessionUpdateTurnStatusEditsSamePost(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.agentSessions["session-1"] = withActiveTurn(store.agentSessions["session-1"], 1, "run-1")
	store.sessionTurns[0].Status = agentSessionTurnRunning
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
	if len(publisher.posts) != 1 {
		t.Fatalf("posts = %#v", publisher.posts)
	}
	if len(publisher.updates) != 1 {
		t.Fatalf("updates = %#v", publisher.updates)
	}
	if publisher.updates[0].PostID != first.PostID || publisher.updates[0].Message != "Проверяю результат" {
		t.Fatalf("update = %#v", publisher.updates[0])
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
	if len(publisher.updates) != 1 {
		t.Fatalf("updates = %#v", publisher.updates)
	}
	message := publisher.updates[0].Message
	for _, expected := range []string{"agent turn completed", "OpenAI account: `main`", "Codex limits:", "🕔 5h", "📅 7d"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("status message misses %q: %q", expected, message)
		}
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

var _ runtimerepo.Runner = (*fakeRuntimeRunner)(nil)
