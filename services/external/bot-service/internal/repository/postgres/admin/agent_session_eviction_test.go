//go:build postgres

package admin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdlePodEvictionLosesRaceToQueuedTurnWithoutDeletingPod(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "idle_pod_eviction")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate idle pod eviction schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open idle pod eviction pool: %v", err)
	}
	defer pool.Close()

	var projectID, roleID, chatID, sessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Eviction', 'eviction') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'worker', 'worker') returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'eviction-channel', 'Eviction', 'eviction') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_agent_sessions(
			session_key, project_id, chat_id, role_id, session_scope,
			mattermost_channel_id, mattermost_root_post_id, status,
			kubernetes_namespace, pod_name, pvc_name, token_secret_ref,
			ttl_seconds, expires_at
		) values (
			'eviction-session', $1, $2, $3, 'thread_role',
			'eviction-channel', 'eviction-root', 'idle',
			'matter-codex', 'eviction-pod', 'eviction-pvc', 'eviction-token',
			3600, now() + interval '1 hour'
		) returning id
	`, projectID, chatID, roleID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	turnTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin queued turn transaction: %v", err)
	}
	defer func() { _ = turnTx.Rollback(ctx) }()
	if _, err := turnTx.Exec(ctx, `
		insert into matter_codex_agent_session_turns(
			session_id, run_id, mattermost_channel_id, mattermost_root_post_id,
			mattermost_post_id, user_id, user_name, message, status
		) values ($1, 'queued-during-eviction', 'eviction-channel', 'eviction-root', 'trigger-post', 'owner-id', 'owner', 'continue', 'queued')
	`, sessionID); err != nil {
		t.Fatalf("stage queued turn: %v", err)
	}

	repository := postgresrepo.NewRepository(pool)
	sideEffectCalled := make(chan struct{}, 1)
	evictionResult := make(chan error, 1)
	go func() {
		_, evictErr := repository.EvictIdleAgentSessionPod(ctx, "eviction-session", "eviction-pod", func() error {
			sideEffectCalled <- struct{}{}
			return nil
		})
		evictionResult <- evictErr
	}()

	select {
	case err := <-evictionResult:
		t.Fatalf("eviction bypassed the in-flight turn lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := turnTx.Commit(ctx); err != nil {
		t.Fatalf("commit queued turn: %v", err)
	}
	if err := <-evictionResult; !errors.Is(err, adminrepo.ErrNotFound) {
		t.Fatalf("eviction race error = %v", err)
	}
	select {
	case <-sideEffectCalled:
		t.Fatal("eviction side effect ran after the session became busy")
	default:
	}
	var podName string
	if err := pool.QueryRow(ctx, `select pod_name from matter_codex_agent_sessions where id = $1`, sessionID).Scan(&podName); err != nil {
		t.Fatalf("read retained pod binding: %v", err)
	}
	if podName != "eviction-pod" {
		t.Fatalf("retained pod name = %q", podName)
	}
}
