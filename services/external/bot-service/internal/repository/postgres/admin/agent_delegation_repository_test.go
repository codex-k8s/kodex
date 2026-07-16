package admin_test

import (
	"context"
	"os"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentDelegationRepositoryLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	var projectID, sourceRoleID, targetRoleID, sourceChatID, targetChatID int64
	if err := pool.QueryRow(ctx, "insert into matter_codex_projects(name, slug) values ('Test', 'test') returning id").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'source', 'manager') returning id", projectID).Scan(&sourceRoleID); err != nil {
		t.Fatalf("create source role: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'target', 'worker') returning id", projectID).Scan(&targetRoleID); err != nil {
		t.Fatalf("create target role: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, name, slug) values ($1, 'Source', 'source') returning id", projectID).Scan(&sourceChatID); err != nil {
		t.Fatalf("create source chat: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, name, slug) values ($1, 'Target', 'target') returning id", projectID).Scan(&targetChatID); err != nil {
		t.Fatalf("create target chat: %v", err)
	}

	expiresAt := time.Now().UTC().Add(time.Hour)
	var sourceSessionID, targetSessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, ttl_seconds, expires_at
	) values ('source-session', $1, $2, $3, 'thread_role', 3600, $4) returning id`, projectID, sourceChatID, sourceRoleID, expiresAt).Scan(&sourceSessionID); err != nil {
		t.Fatalf("create source session: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, ttl_seconds, expires_at
	) values ('target-session', $1, $2, $3, 'thread_role', 3600, $4) returning id`, projectID, targetChatID, targetRoleID, expiresAt).Scan(&targetSessionID); err != nil {
		t.Fatalf("create target session: %v", err)
	}
	var sourceTurnID, targetTurnID, callbackTurnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'source-run', 'source-channel', 'source-root', 'source-post', 'source') returning id`, sourceSessionID).Scan(&sourceTurnID); err != nil {
		t.Fatalf("create source turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'target-run', 'target-channel', 'target-root', 'target-post', 'target') returning id`, targetSessionID).Scan(&targetTurnID); err != nil {
		t.Fatalf("create target turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'callback-run', 'source-channel', 'source-root', 'callback-post', 'callback') returning id`, sourceSessionID).Scan(&callbackTurnID); err != nil {
		t.Fatalf("create callback turn: %v", err)
	}

	repository := postgresrepo.NewRepository(pool)
	created, wasCreated, err := repository.CreateAgentDelegation(ctx, domainrepo.CreateAgentDelegationInput{
		ProjectID:       projectID,
		SourceSessionID: sourceSessionID,
		SourceTurnID:    sourceTurnID,
		TargetChatID:    targetChatID,
		TargetRoleID:    targetRoleID,
		WorkItemKey:     "work-1",
		Title:           "Work 1",
	})
	if err != nil || !wasCreated {
		t.Fatalf("CreateAgentDelegation() item=%#v created=%t error=%v", created, wasCreated, err)
	}
	duplicate, wasCreated, err := repository.CreateAgentDelegation(ctx, domainrepo.CreateAgentDelegationInput{
		ProjectID:       projectID,
		SourceSessionID: sourceSessionID,
		SourceTurnID:    sourceTurnID,
		TargetChatID:    targetChatID,
		TargetRoleID:    targetRoleID,
		WorkItemKey:     "work-1",
		Title:           "Work 1",
	})
	if err != nil || wasCreated || duplicate.ID != created.ID {
		t.Fatalf("duplicate item=%#v created=%t error=%v", duplicate, wasCreated, err)
	}
	if _, err := repository.SetAgentDelegationRoot(ctx, created.ID, "target-root"); err != nil {
		t.Fatalf("SetAgentDelegationRoot() error = %v", err)
	}
	started, err := repository.SetAgentDelegationTarget(ctx, created.ID, targetSessionID, targetTurnID, "target-run")
	if err != nil || started.Status != "queued" {
		t.Fatalf("started=%#v error=%v", started, err)
	}
	items, err := repository.ListAgentDelegationsBySource(ctx, sourceSessionID, 20)
	if err != nil || len(items) != 1 || items[0].TargetRootPostID != "target-root" {
		t.Fatalf("items=%#v error=%v", items, err)
	}
	callbackTarget, err := repository.GetAgentDelegationForCallback(ctx, targetSessionID)
	if err != nil || callbackTarget.ID != created.ID {
		t.Fatalf("callback target=%#v error=%v", callbackTarget, err)
	}
	callback, err := repository.SetAgentDelegationCallback(ctx, created.ID, callbackTurnID, "callback-run")
	if err != nil || callback.CallbackRunID != "callback-run" || callback.Status != "callback_queued" {
		t.Fatalf("callback=%#v error=%v", callback, err)
	}
	if _, err := pool.Exec(ctx, "update matter_codex_agent_session_turns set status = 'succeeded' where id = $1", callbackTurnID); err != nil {
		t.Fatalf("complete callback turn: %v", err)
	}
	items, err = repository.ListAgentDelegationsBySource(ctx, sourceSessionID, 20)
	if err != nil || len(items) != 1 || items[0].Status != "callback_succeeded" {
		t.Fatalf("completed callback items=%#v error=%v", items, err)
	}
}
