package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var delegationPostgresSequence atomic.Uint64

func TestReturnToRequesterProductionDispatcherDoesNotSelfLockFrozenSource(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proveGuardedSourceUsesSamePostgresTransaction(t, ctx, fixture)
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов.")
	if err != nil {
		t.Fatalf("ReturnToRequester() under two-connection source lock: %v", err)
	}
	if strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatal("production dispatcher did not persist callback run")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
}

func TestReturnToRequesterProductionDispatcherRevocationFailsBeforeEffects(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := fixture.pool.Exec(ctx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'source-session', 'synthetic test revocation')
`); err != nil {
		t.Fatalf("revoke frozen source: %v", err)
	}
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов."); err == nil {
		t.Fatal("ReturnToRequester() accepted revoked frozen source")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	if fixture.publisher.posts.Load() != 0 {
		t.Fatal("revoked callback published a Mattermost effect")
	}
}

func TestReturnToRequesterProductionDispatcherPoolOneConcurrentIdempotency(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	runIDs := make(chan string, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический конкурентный результат.")
			if err == nil && (result.TargetChat != "target" || result.TargetAgent != "worker-proof") {
				err = fmt.Errorf("concurrent callback returned incomplete target metadata")
			}
			if err == nil {
				runIDs <- result.CallbackRunID
			}
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(runIDs)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent ReturnToRequester() error: %v", err)
		}
	}
	var callbackRunID string
	for runID := range runIDs {
		if strings.TrimSpace(runID) == "" {
			t.Fatal("concurrent callback returned an empty run id")
		}
		if callbackRunID == "" {
			callbackRunID = runID
		} else if callbackRunID != runID {
			t.Fatal("concurrent callback produced multiple run identities")
		}
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
}

func TestReturnToRequesterProductionDispatcherErrorRollsBackAndRetriesOnce(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dispatcher := &failOnceTransactionalDispatcher{AgentTurnDispatcher: fixture.dispatcher, delegate: fixture.dispatcher}
	fixture.service.cfg.TurnDispatcher = dispatcher
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат с повтором."); err == nil {
		t.Fatal("ReturnToRequester() accepted a failed transactional prepare")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат с повтором.")
	if err != nil || strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatalf("ReturnToRequester() retry result=%#v error=%v", result, err)
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
}

type postgresDelegationFixture struct {
	pool       *pgxpool.Pool
	service    *AgentSessionService
	dispatcher *ChatRunService
	publisher  *postgresDelegationPublisher
}

type failOnceTransactionalDispatcher struct {
	AgentTurnDispatcher
	delegate TransactionalAgentTurnDispatcher
	failed   atomic.Bool
}

func (dispatcher *failOnceTransactionalDispatcher) EnqueueExistingAgentTurn(ctx context.Context, store adminrepo.Repository, session entity.AgentSession, request AgentTurnRequest) (AgentTurnQueued, error) {
	queued, err := dispatcher.delegate.EnqueueExistingAgentTurn(ctx, store, session, request)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	if !dispatcher.failed.Swap(true) {
		return AgentTurnQueued{}, fmt.Errorf("synthetic transactional prepare failure")
	}
	return queued, nil
}

type postgresDelegationRunner struct {
	runtimerepo.Runner
	tokenReads     atomic.Int64
	integrityReads atomic.Int64
}

func (runner *postgresDelegationRunner) GetMattermostBotTokenSecret(_ context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error) {
	runner.tokenReads.Add(1)
	if secretName != "target-secret" {
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("synthetic token secret not found")
	}
	return runtimerepo.MattermostBotTokenSecret{SecretName: secretName, Token: "target-token"}, nil
}

func (runner *postgresDelegationRunner) InspectSecretIntegrity(_ context.Context, input runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	runner.integrityReads.Add(1)
	switch input.SecretName + "/" + input.SecretKey {
	case "source-bot-secret/token":
		return runtimerepo.SecretIntegrity{SecretName: input.SecretName, SecretKey: input.SecretKey, ContentSHA256: strings.Repeat("b", 64), UID: "source-bot-uid", ResourceVersion: "1"}, nil
	case "source-secret/token":
		return runtimerepo.SecretIntegrity{SecretName: input.SecretName, SecretKey: input.SecretKey, ContentSHA256: strings.Repeat("a", 64), UID: "source-session-uid", ResourceVersion: "1"}, nil
	default:
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("synthetic integrity binding not found")
	}
}

type postgresDelegationPublisher struct {
	MattermostThreadPublisher
	posts atomic.Int64
}

func (publisher *postgresDelegationPublisher) PostThreadMessage(_ context.Context, _ MattermostThreadPostInput) (MattermostPostRef, error) {
	id := publisher.posts.Add(1)
	return MattermostPostRef{PostID: "synthetic-post-" + strconv.FormatInt(id, 10)}, nil
}

func newPostgresDelegationFixture(t *testing.T, maxConnections int32) postgresDelegationFixture {
	t.Helper()
	dsn := isolatedDelegationPostgresDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 21); err != nil {
		t.Fatalf("migrations through v21: %v", err)
	}
	seedPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open delegation seed pool: %v", err)
	}
	seedPostgresDelegation(t, ctx, seedPool)
	seedPool.Close()
	if err := migrations.RunTo(ctx, dsn, 23); err != nil {
		t.Fatalf("migrations through v23: %v", err)
	}
	seedPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen delegation seed pool: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
update matter_codex_agent_sessions
set secret_content_sha256 = repeat('a', 64), secret_resource_uid = 'source-session-uid', secret_resource_version = '1'
where session_key = 'source-session'
`); err != nil {
		seedPool.Close()
		t.Fatalf("stage session Secret integrity metadata: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
update matter_codex_mattermost_bot_identities
set secret_content_sha256 = repeat('b', 64), secret_resource_uid = 'source-bot-uid', secret_resource_version = '1'
where token_secret_ref = 'source-bot-secret'
`); err != nil {
		seedPool.Close()
		t.Fatalf("stage bot Secret integrity metadata: %v", err)
	}
	seedPool.Close()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrations through v25: %v", err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse delegation pool config: %v", err)
	}
	config.MaxConns = maxConnections
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set lock_timeout = '750ms'; set statement_timeout = '8s'`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open bounded delegation pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository := postgresrepo.NewRepository(pool)
	runner := &postgresDelegationRunner{}
	publisher := &postgresDelegationPublisher{}
	chatRuns := NewChatRunService(ChatRunServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: publisher,
		StorageReady: true, RuntimeReady: true, DisableMonitor: true,
	})
	service := NewAgentSessionService(AgentSessionServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: publisher, TurnDispatcher: chatRuns,
		MattermostSiteURL: "https://mattermost.example", StorageReady: true, RuntimeReady: true,
	})
	return postgresDelegationFixture{pool: pool, service: service, dispatcher: chatRuns, publisher: publisher}
}

func seedPostgresDelegation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var projectID, sourceRoleID, targetRoleID, sourceChatID, targetChatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Delegation proof', 'delegation-proof') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("seed delegation project: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, 'manager-proof', 'manager', 'cluster-admin') returning id`, projectID).Scan(&sourceRoleID); err != nil {
		t.Fatalf("seed source role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, 'worker-proof', 'worker', 'read-only') returning id`, projectID).Scan(&targetRoleID); err != nil {
		t.Fatalf("seed target role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, chat_type) values ($1, 'source-channel', 'Source', 'source', 'manager') returning id`, projectID).Scan(&sourceChatID); err != nil {
		t.Fatalf("seed source chat: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, chat_type) values ($1, 'target-channel', 'Target', 'target', 'custom') returning id`, projectID).Scan(&targetChatID); err != nil {
		t.Fatalf("seed target chat: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2), ($3, $4)`, sourceChatID, sourceRoleID, targetChatID, targetRoleID); err != nil {
		t.Fatalf("seed participants: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_mattermost_bot_identities(project_id, role_id, username, mattermost_user_id, token_secret_ref, status)
values ($1, $2, 'manager-proof', 'manager-user', 'source-bot-secret', 'active'),
	($1, $3, 'worker-proof', 'worker-user', 'target-bot-secret', 'active')
`, projectID, sourceRoleID, targetRoleID); err != nil {
		t.Fatalf("seed bot identities: %v", err)
	}
	var sourceSessionID, targetSessionID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, status, kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
) values ('source-session', $1, $2, $3, 'thread_role', 'source-channel', 'source-root', 'running', 'mattermost', 'source-pod', 'source-pvc', 'source-secret', 3600, now() + interval '1 hour')
returning id
`, projectID, sourceChatID, sourceRoleID).Scan(&sourceSessionID); err != nil {
		t.Fatalf("seed source session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, status, kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
) values ('target-session', $1, $2, $3, 'thread_role', 'target-channel', 'target-root', 'running', 'mattermost', 'target-pod', 'target-pvc', 'target-secret', 3600, now() + interval '1 hour')
returning id
`, projectID, targetChatID, targetRoleID).Scan(&targetSessionID); err != nil {
		t.Fatalf("seed target session: %v", err)
	}
	var sourceTurnID, targetTurnID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status)
values ($1, 'source-run', 'source-channel', 'source-root', 'source-root', 'source work', 'running') returning id
`, sourceSessionID).Scan(&sourceTurnID); err != nil {
		t.Fatalf("seed source turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status)
values ($1, 'target-run', 'target-channel', 'target-root', 'target-root', 'target work', 'running') returning id
`, targetSessionID).Scan(&targetTurnID); err != nil {
		t.Fatalf("seed target turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $1, active_run_id = 'source-run' where id = $2`, sourceTurnID, sourceSessionID); err != nil {
		t.Fatalf("bind source active turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $1, active_run_id = 'target-run' where id = $2`, targetTurnID, targetSessionID); err != nil {
		t.Fatalf("bind target active turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_agent_delegations(
	project_id, source_session_id, source_turn_id, target_chat_id, target_role_id,
	target_root_post_id, target_session_id, target_turn_id, target_run_id, work_item_key, title, status
) values ($1, $2, $3, $4, $5, 'target-root', $6, $7, 'target-run', 'transaction-proof', 'Транзакционный callback', 'running')
`, projectID, sourceSessionID, sourceTurnID, targetChatID, targetRoleID, targetSessionID, targetTurnID); err != nil {
		t.Fatalf("seed delegation: %v", err)
	}
}

func assertPostgresDelegationCallbackState(t *testing.T, ctx context.Context, fixture postgresDelegationFixture, expectedCallbacks int) {
	t.Helper()
	var callbackRows, queuedTurns, callbackRuns int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_agent_delegations where callback_turn_id is not null and callback_run_id <> ''`).Scan(&callbackRows); err != nil {
		t.Fatalf("count persisted callbacks: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `
select count(*) from matter_codex_agent_session_turns turn_row
join matter_codex_agent_sessions session_row on session_row.id = turn_row.session_id
where session_row.session_key = 'source-session' and turn_row.status = 'queued'
`).Scan(&queuedTurns); err != nil {
		t.Fatalf("count callback turns: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_agent_runs where status = 'queued' and flow_id = 'session-source-session'`).Scan(&callbackRuns); err != nil {
		t.Fatalf("count callback runs: %v", err)
	}
	if callbackRows != expectedCallbacks || queuedTurns != expectedCallbacks || callbackRuns != expectedCallbacks {
		t.Fatalf("callback state rows=%d turns=%d runs=%d, want=%d", callbackRows, queuedTurns, callbackRuns, expectedCallbacks)
	}
}

func proveGuardedSourceUsesSamePostgresTransaction(t *testing.T, ctx context.Context, fixture postgresDelegationFixture) {
	t.Helper()
	source, err := fixture.service.cfg.Store.GetAgentSession(ctx, "source-session")
	if err != nil {
		t.Fatalf("read source session for transaction proof: %v", err)
	}
	repository, ok := fixture.service.cfg.Store.(securityrepo.ClusterAdminPersistenceGuardRepository)
	if !ok {
		t.Fatal("production repository does not expose persistence guard")
	}
	err = repository.WithExistingClusterAdminPersistenceGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: source.RoleID, ProjectID: source.ProjectID, ChatID: source.ChatID,
		MattermostChannelID: source.MattermostChannelID, SessionKey: source.SessionKey,
		Operation: "test.callback.transaction", ActorUser: "test",
	}, func(guardedStore adminrepo.Repository) error {
		input := adminrepo.UpdateAgentSessionRuntimeInput{SessionKey: source.SessionKey, ExtendTTLSeconds: source.TTLSeconds}
		if _, guardedErr := guardedStore.UpdateAgentSessionRuntime(ctx, input); guardedErr != nil {
			return fmt.Errorf("guarded source update did not use the current transaction: %w", guardedErr)
		}
		outsideResult := make(chan error, 1)
		go func() {
			_, outsideErr := fixture.service.cfg.Store.UpdateAgentSessionRuntime(ctx, input)
			outsideResult <- outsideErr
		}()
		select {
		case outsideErr := <-outsideResult:
			if outsideErr == nil {
				return fmt.Errorf("base pool updated a source row locked by the guarded transaction")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("two-connection guarded repository proof: %v", err)
	}
}

func isolatedDelegationPostgresDSN(t *testing.T) string {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	if baseDSN == "" {
		if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
			t.Fatal("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN обязателен для PostgreSQL-теста callback")
		}
		t.Skip("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN не задан")
	}
	schema := "mc_callback_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "_" + strconv.FormatUint(delegationPostgresSequence.Add(1), 36)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("open PostgreSQL for callback schema: %v", err)
	}
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "create schema "+identifier); err != nil {
		pool.Close()
		t.Fatalf("create callback schema: %v", err)
	}
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool, cleanupErr := pgxpool.New(cleanupCtx, baseDSN)
		if cleanupErr != nil {
			t.Errorf("open callback cleanup pool: %v", cleanupErr)
			return
		}
		defer cleanupPool.Close()
		if _, cleanupErr = cleanupPool.Exec(cleanupCtx, "drop schema "+identifier+" cascade"); cleanupErr != nil {
			t.Errorf("drop callback schema: %v", cleanupErr)
		}
	})
	parsed, err := url.Parse(baseDSN)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return baseDSN + " search_path=" + schema
}
