//go:build postgres

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	automationspostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresStopFaults struct {
	mu          sync.Mutex
	resetCalls  int
	resetErrors []error
}

type postgresStopStore struct {
	adminrepo.Repository
	adminrepo.CoordinationRepository
	exact  adminrepo.ExactAgentSessionsRuntimeGuardRepository
	faults *postgresStopFaults
}

func newPostgresStopStore(repository *adminpostgres.Repository, faults *postgresStopFaults) *postgresStopStore {
	return &postgresStopStore{
		Repository: repository, CoordinationRepository: repository, exact: repository, faults: faults,
	}
}

func (store *postgresStopStore) WithExactAgentSessionsRuntimeGuard(ctx context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	return store.exact.WithExactAgentSessionsRuntimeGuard(ctx, expected, func(transactional adminrepo.Repository) error {
		coordination, ok := transactional.(adminrepo.CoordinationRepository)
		if !ok {
			return adminrepo.ErrClusterAdminAdmissionDenied
		}
		return sideEffect(&postgresStopStore{
			Repository: transactional, CoordinationRepository: coordination, exact: store.exact, faults: store.faults,
		})
	})
}

func (store *postgresStopStore) CompareAndSwapAgentSessionTurnArtifacts(ctx context.Context, input adminrepo.CompareAndSwapAgentSessionTurnArtifactsInput) (entity.AgentSessionTurn, error) {
	repository, ok := store.Repository.(adminrepo.AgentSessionTurnArtifactsRepository)
	if !ok {
		return entity.AgentSessionTurn{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.CompareAndSwapAgentSessionTurnArtifacts(ctx, input)
}

func (store *postgresStopStore) ResetAgentSessionRuntime(ctx context.Context, sessionKey string, status string) (entity.AgentSession, error) {
	store.faults.mu.Lock()
	store.faults.resetCalls++
	var fault error
	if len(store.faults.resetErrors) > 0 {
		fault = store.faults.resetErrors[0]
		store.faults.resetErrors = store.faults.resetErrors[1:]
	}
	store.faults.mu.Unlock()
	if fault != nil {
		return entity.AgentSession{}, fault
	}
	return store.Repository.ResetAgentSessionRuntime(ctx, sessionKey, status)
}

func (store *postgresStopStore) resetCallCount() int {
	store.faults.mu.Lock()
	defer store.faults.mu.Unlock()
	return store.faults.resetCalls
}

type postgresStopRunner struct {
	runtimerepo.Runner
	mu            sync.Mutex
	cleanupCalls  int
	cleanupErrors []error
}

func (runner *postgresStopRunner) CleanupAgentSession(_ context.Context, sessionKey string) (runtimerepo.AgentSessionCleanupResult, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.cleanupCalls++
	if len(runner.cleanupErrors) > 0 {
		fault := runner.cleanupErrors[0]
		runner.cleanupErrors = runner.cleanupErrors[1:]
		if fault != nil {
			return runtimerepo.AgentSessionCleanupResult{}, fault
		}
	}
	return runtimerepo.AgentSessionCleanupResult{SessionKey: sessionKey, PodDeleted: true}, nil
}

func (runner *postgresStopRunner) cleanupCallCount() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.cleanupCalls
}

type postgresStopPublisher struct {
	statusservice.MattermostThreadPublisher
	mu          sync.Mutex
	cardPosts   []statusservice.MattermostCard
	cardUpdates []statusservice.MattermostCard
}

func (publisher *postgresStopPublisher) PostThreadCard(_ context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.cardPosts = append(publisher.cardPosts, card)
	return statusservice.MattermostPostRef{ChannelID: card.ChannelID, PostID: "unexpected-status-post"}, nil
}

func (publisher *postgresStopPublisher) UpdateThreadCard(_ context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.cardUpdates = append(publisher.cardUpdates, card)
	return statusservice.MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}

func (publisher *postgresStopPublisher) snapshot() ([]statusservice.MattermostCard, []statusservice.MattermostCard) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return append([]statusservice.MattermostCard(nil), publisher.cardPosts...), append([]statusservice.MattermostCard(nil), publisher.cardUpdates...)
}

type postgresStopActorVerifier struct{}

func (postgresStopActorVerifier) VerifyInteractionActor(_ context.Context, userID string, channelID string) (bool, error) {
	return userID == "owner-user" && channelID == "stop-channel", nil
}

type failOnceTerminalReconciler struct {
	next statusservice.AutomationRuntimeTerminalReconciler
	mu   sync.Mutex
	err  error
}

func (reconciler *failOnceTerminalReconciler) ReconcileRuntimeTerminal(ctx context.Context, command statusservice.AutomationRuntimeTerminalCommand) error {
	reconciler.mu.Lock()
	fault := reconciler.err
	reconciler.err = nil
	reconciler.mu.Unlock()
	if fault != nil {
		return fault
	}
	return reconciler.next.ReconcileRuntimeTerminal(ctx, command)
}

type postgresStopHTTPFixture struct {
	pool          *pgxpool.Pool
	admin         *adminpostgres.Repository
	store         *postgresStopStore
	runner        *postgresStopRunner
	publisher     *postgresStopPublisher
	projectID     int64
	roleID        int64
	chatID        int64
	sessionID     int64
	sessionKey    string
	turnID        int64
	runtimeRunID  string
	processRunID  int64
	scheduledRun  string
	automationNow time.Time
	securityNow   time.Time
}

func TestStopTurnProductionRouterPostgresRecoversPostCommitFailureExactlyOnce(t *testing.T) {
	fixture := newPostgresStopHTTPFixture(t, "router_recovery")
	automation := fixture.automationService()
	fault := errors.New("синтетическая post-commit ошибка terminal reconciliation")
	failing := &failOnceTerminalReconciler{next: automation, err: fault}
	router, security := fixture.router(t, failing, 30*time.Minute)
	originalBody := fixture.issueActionBody(t, security, "stop_turn", fixture.projectID, fixture.sessionKey, fixture.turnID)

	first := serveStopTurnAction(router, originalBody)
	if first.Code != http.StatusBadGateway {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	cards, updates := fixture.publisher.snapshot()
	if len(cards) != 0 || len(updates) != 1 || len(updates[0].Actions) != 1 || updates[0].Actions[0].Context["action"] != "recover_stop_turn" {
		t.Fatalf("recovery cards posts=%#v updates=%#v", cards, updates)
	}
	if fixture.runner.cleanupCallCount() != 1 || fixture.store.resetCallCount() != 1 {
		t.Fatalf("first cleanup=%d reset=%d", fixture.runner.cleanupCallCount(), fixture.store.resetCallCount())
	}
	fixture.assertPendingAutomation(t)
	recoveryBody := stopTurnBodyFromCard(t, updates[0])

	baselineCleanup := fixture.runner.cleanupCallCount()
	baselineReset := fixture.store.resetCallCount()
	baselineCards := len(updates)
	for _, test := range []struct {
		name string
		body func(*Router, *statusservice.InteractionSecurityService) string
	}{
		{name: "actor", body: func(_ *Router, _ *statusservice.InteractionSecurityService) string {
			return mutateStopTurnBody(t, recoveryBody, func(payload map[string]any) { payload["user_id"] = "other-user" })
		}},
		{name: "channel", body: func(_ *Router, _ *statusservice.InteractionSecurityService) string {
			return mutateStopTurnBody(t, recoveryBody, func(payload map[string]any) { payload["channel_id"] = "other-channel" })
		}},
		{name: "post", body: func(_ *Router, _ *statusservice.InteractionSecurityService) string {
			return mutateStopTurnBody(t, recoveryBody, func(payload map[string]any) { payload["post_id"] = "other-post" })
		}},
		{name: "project", body: func(_ *Router, current *statusservice.InteractionSecurityService) string {
			return fixture.issueActionBody(t, current, "recover_stop_turn", fixture.projectID+100, fixture.sessionKey, fixture.turnID)
		}},
		{name: "session", body: func(_ *Router, current *statusservice.InteractionSecurityService) string {
			return fixture.issueActionBody(t, current, "recover_stop_turn", fixture.projectID, "other-session", fixture.turnID)
		}},
		{name: "turn", body: func(_ *Router, current *statusservice.InteractionSecurityService) string {
			return fixture.issueActionBody(t, current, "recover_stop_turn", fixture.projectID, fixture.sessionKey, fixture.turnID+100)
		}},
		{name: "context", body: func(_ *Router, _ *statusservice.InteractionSecurityService) string {
			return mutateStopTurnBody(t, recoveryBody, func(payload map[string]any) { payload["context"].(map[string]any)["changed"] = "true" })
		}},
		{name: "non-stop", body: func(_ *Router, _ *statusservice.InteractionSecurityService) string {
			return mutateStopTurnBody(t, recoveryBody, func(payload map[string]any) { payload["context"].(map[string]any)["action"] = "retry_turn" })
		}},
		{name: "ttl", body: func(_ *Router, current *statusservice.InteractionSecurityService) string {
			expiredSecurity := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
				Repository:    fixture.admin,
				Admission:     statusservice.NewServerSideInteractionAdmission("", postgresStopActorVerifier{}, fixture.admin),
				CapabilityTTL: time.Second,
				Now:           func() time.Time { return fixture.securityNow.Add(-time.Minute) },
			})
			return fixture.issueActionBody(t, expiredSecurity, "recover_stop_turn", fixture.projectID, fixture.sessionKey, fixture.turnID)
		}},
	} {
		t.Run("negative_"+test.name, func(t *testing.T) {
			restarted, restartedSecurity := fixture.router(t, automation, 30*time.Minute)
			response := serveStopTurnAction(restarted, test.body(restarted, restartedSecurity))
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	_, updates = fixture.publisher.snapshot()
	if fixture.runner.cleanupCallCount() != baselineCleanup || fixture.store.resetCallCount() != baselineReset || len(updates) != baselineCards {
		t.Fatalf("negative matrix effects cleanup=%d/%d reset=%d/%d cards=%d/%d", fixture.runner.cleanupCallCount(), baselineCleanup, fixture.store.resetCallCount(), baselineReset, len(updates), baselineCards)
	}

	restarted, _ := fixture.router(t, automation, 30*time.Minute)
	recovered := serveStopTurnAction(restarted, recoveryBody)
	if recovered.Code != http.StatusOK {
		t.Fatalf("recovery status=%d body=%s", recovered.Code, recovered.Body.String())
	}
	fixture.assertTerminalExactlyOnce(t)
	_, updates = fixture.publisher.snapshot()
	if fixture.runner.cleanupCallCount() != 1 || fixture.store.resetCallCount() != 1 || len(updates) != 1 {
		t.Fatalf("recovery duplicates cleanup=%d reset=%d cards=%d", fixture.runner.cleanupCallCount(), fixture.store.resetCallCount(), len(updates))
	}
	for name, body := range map[string]string{"initial": originalBody, "recovery": recoveryBody} {
		response := serveStopTurnAction(restarted, body)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("second %s delivery status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
	fixture.assertTerminalExactlyOnce(t)
	_, updates = fixture.publisher.snapshot()
	if fixture.runner.cleanupCallCount() != 1 || fixture.store.resetCallCount() != 1 || len(updates) != 1 {
		t.Fatalf("second delivery effects cleanup=%d reset=%d cards=%d", fixture.runner.cleanupCallCount(), fixture.store.resetCallCount(), len(updates))
	}
}

func TestStopTurnProductionRouterPostgresResumesCleanupAndResetFaults(t *testing.T) {
	cleanupErr := errors.New("синтетическая ошибка cleanup")
	resetErr := errors.New("синтетическая ошибка reset")
	for _, test := range []struct {
		name        string
		configure   func(*postgresStopHTTPFixture)
		wantCleanup int
		wantReset   int
	}{
		{name: "cleanup", configure: func(fixture *postgresStopHTTPFixture) {
			fixture.runner.cleanupErrors = []error{cleanupErr}
		}, wantCleanup: 2, wantReset: 1},
		{name: "reset", configure: func(fixture *postgresStopHTTPFixture) {
			fixture.store.faults.resetErrors = []error{resetErr}
		}, wantCleanup: 1, wantReset: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPostgresStopHTTPFixture(t, "router_fault_"+test.name)
			test.configure(fixture)
			automation := fixture.automationService()
			router, security := fixture.router(t, automation, 30*time.Minute)
			body := fixture.issueActionBody(t, security, "stop_turn", fixture.projectID, fixture.sessionKey, fixture.turnID)
			first := serveStopTurnAction(router, body)
			if first.Code != http.StatusBadGateway {
				t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
			}
			_, updates := fixture.publisher.snapshot()
			if len(updates) != 0 {
				t.Fatalf("card published before recovery: %#v", updates)
			}

			restarted, _ := fixture.router(t, automation, 30*time.Minute)
			recovered := serveStopTurnAction(restarted, body)
			if recovered.Code != http.StatusOK {
				t.Fatalf("replayed initial Stop status=%d body=%s", recovered.Code, recovered.Body.String())
			}
			fixture.assertTerminalExactlyOnce(t)
			_, updates = fixture.publisher.snapshot()
			if fixture.runner.cleanupCallCount() != test.wantCleanup || fixture.store.resetCallCount() != test.wantReset || len(updates) != 1 {
				t.Fatalf("effects cleanup=%d/%d reset=%d/%d cards=%d", fixture.runner.cleanupCallCount(), test.wantCleanup, fixture.store.resetCallCount(), test.wantReset, len(updates))
			}
			second := serveStopTurnAction(restarted, body)
			if second.Code != http.StatusUnauthorized {
				t.Fatalf("completed initial replay status=%d body=%s", second.Code, second.Body.String())
			}
		})
	}
}

func newPostgresStopHTTPFixture(t *testing.T, schema string) *postgresStopHTTPFixture {
	t.Helper()
	dsn := testsupport.IsolatedSchemaDSN(t, schema)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("применить миграции: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	adminRepository := adminpostgres.NewRepository(pool)
	faults := &postgresStopFaults{}
	fixture := &postgresStopHTTPFixture{
		pool: pool, admin: adminRepository, store: newPostgresStopStore(adminRepository, faults),
		runner: &postgresStopRunner{}, publisher: &postgresStopPublisher{},
		sessionKey: "postgres-stop-session", runtimeRunID: "postgres-stop-runtime-run",
		automationNow: time.Date(2026, time.July, 21, 12, 10, 0, 0, time.UTC),
		securityNow:   time.Date(2026, time.July, 21, 12, 20, 0, 0, time.UTC),
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Postgres Stop', 'postgres-stop') returning id`).Scan(&fixture.projectID); err != nil {
		t.Fatalf("создать проект: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, fixture.projectID).Scan(&fixture.roleID); err != nil {
		t.Fatalf("создать роль: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'stop-channel', 'Stop chat', 'stop-chat') returning id`, fixture.projectID).Scan(&fixture.chatID); err != nil {
		t.Fatalf("создать чат: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id, enabled) values ($1, $2, true)`, fixture.chatID, fixture.roleID); err != nil {
		t.Fatalf("создать участника: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id, mattermost_root_post_id,
		status, active_run_id, kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
	) values ($1, $2, $3, $4, 'automation-run', 'stop-channel', 'stop-root', 'running', $5,
		'mattermost', 'stop-pod', 'stop-pvc', 'stop-secret', 3600, now() + interval '1 hour') returning id`,
		fixture.sessionKey, fixture.projectID, fixture.chatID, fixture.roleID, fixture.runtimeRunID).Scan(&fixture.sessionID); err != nil {
		t.Fatalf("создать session: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id,
		mattermost_status_post_id, user_id, user_name, message, status
	) values ($1, $2, 'stop-channel', 'stop-root', 'stop-root', 'status-post-1', 'owner-user', 'owner', 'saved playbook', 'running') returning id`,
		fixture.sessionID, fixture.runtimeRunID).Scan(&fixture.turnID); err != nil {
		t.Fatalf("создать turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2 where id = $1`, fixture.sessionID, fixture.turnID); err != nil {
		t.Fatalf("привязать active turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_runs(run_id, profile_name, role, provider, owner, name, head_branch, status)
		values ($1, 'worker', 'worker', 'github', 'example', 'repository', 'stop', 'running')`, fixture.runtimeRunID); err != nil {
		t.Fatalf("создать agent run: %v", err)
	}
	var policyRevisionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_policy_revisions(project_id, version, status, activated_at) values ($1, 1, 'active', now()) returning id`, fixture.projectID).Scan(&policyRevisionID); err != nil {
		t.Fatalf("создать policy revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_process_runs(
		public_id, project_id, policy_revision_id, root_role_id, root_initiator_user_id, root_initiator_user_name,
		root_trigger_post_id, root_channel_id, root_thread_post_id, status
	) values ('postgres-stop-process', $1, $2, $3, 'owner-user', 'owner', 'stop-root', 'stop-channel', 'stop-root', 'running') returning id`,
		fixture.projectID, policyRevisionID, fixture.roleID).Scan(&fixture.processRunID); err != nil {
		t.Fatalf("создать process run: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_process_turns(turn_id, process_run_id, launch_post_id) values ($1, $2, 'stop-root')`, fixture.turnID, fixture.processRunID); err != nil {
		t.Fatalf("связать process turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_work_claims(process_run_id, turn_id, role_id, summary, status) values ($1, $2, $3, 'stop proof', 'active')`, fixture.processRunID, fixture.turnID, fixture.roleID); err != nil {
		t.Fatalf("создать work claim: %v", err)
	}
	fixture.seedAutomation(t, ctx)
	return fixture
}

func (fixture *postgresStopHTTPFixture) seedAutomation(t *testing.T, ctx context.Context) {
	t.Helper()
	repository := automationspostgres.NewRepository(fixture.pool)
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: fixture.projectID,
		TargetAgentRoleID: fixture.roleID, TargetChatID: fixture.chatID, Name: "Postgres Stop",
		OwnerMattermostUserID: "owner-user", OwnerMattermostUserName: "owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "12:00", TimeZone: "UTC", NextRunAt: fixture.automationNow.Add(24 * time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: "saved playbook", PromptSHA256: bytes.Repeat([]byte{0x11}, 32), CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey: "create-postgres-stop", CommandHash: bytes.Repeat([]byte{0x22}, 32), Now: fixture.automationNow,
	})
	if err != nil || !created {
		t.Fatalf("создать schedule: schedule=%#v created=%t error=%v", schedule, created, err)
	}
	fixture.scheduledRun = "scheduled-run-cccccccccccccccccccccccccccccccc"
	run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: fixture.projectID, OwnerMattermostUserID: "owner-user",
		IdempotencyKey: "postgres-stop-command", OccurrencePublicID: "occurrence-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RunPublicID: fixture.scheduledRun, ScheduledFor: fixture.automationNow.Add(time.Minute),
		CallbackExpiresAt: fixture.automationNow.Add(time.Hour), RuntimeRunID: fixture.runtimeRunID,
	})
	if err != nil || !created {
		t.Fatalf("создать scheduled run: run=%#v created=%t error=%v", run, created, err)
	}
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: fixture.scheduledRun, ProjectID: fixture.projectID, OwnerMattermostUserID: "owner-user",
		MattermostChannelID: "stop-channel", MattermostRootPostID: "stop-root", Now: fixture.automationNow.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("сохранить automation thread: %v", err)
	}
	if _, err := repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: fixture.scheduledRun, ProjectID: fixture.projectID, OwnerMattermostUserID: "owner-user",
		RuntimeSessionID: fixture.sessionID, RuntimeSessionKey: fixture.sessionKey, RuntimeTurnID: fixture.turnID, RuntimeRunID: fixture.runtimeRunID,
		MattermostChannelID: "stop-channel", MattermostRootPostID: "stop-root", Now: fixture.automationNow.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("привязать automation run: %v", err)
	}
}

func (fixture *postgresStopHTTPFixture) automationService() *statusservice.AutomationService {
	return statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository: automationspostgres.NewRepository(fixture.pool), Catalog: fixture.admin, StorageReady: true,
		Now: func() time.Time { return fixture.automationNow.Add(10 * time.Minute) },
	})
}

func (fixture *postgresStopHTTPFixture) router(t *testing.T, reconciler statusservice.AutomationRuntimeTerminalReconciler, ttl time.Duration) (*Router, *statusservice.InteractionSecurityService) {
	t.Helper()
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Repository:    fixture.admin,
		Admission:     statusservice.NewServerSideInteractionAdmission("", postgresStopActorVerifier{}, fixture.admin),
		CapabilityTTL: ttl,
		Now:           func() time.Time { return fixture.securityNow },
	})
	securedPublisher := statusservice.NewSecuredMattermostThreadPublisher(fixture.publisher, security)
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		t.Fatalf("создать localizer: %v", err)
	}
	sessionService := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Localizer: localizer, Store: fixture.store, RuntimeRunner: fixture.runner,
		ThreadPublisher: securedPublisher, AutomationRuntimeReconciler: reconciler,
		MenuActionURL: "https://mattermost.example/actions", StorageReady: true, RuntimeReady: true,
	})
	return NewRouter(RouterConfig{
		SessionService: sessionService, InteractionSecurity: security, ThreadPublisher: securedPublisher,
		Localizer: localizer, MaxSlashFormBytes: 65536,
	}), security
}

func (fixture *postgresStopHTTPFixture) issueActionBody(t *testing.T, security *statusservice.InteractionSecurityService, action string, projectID int64, sessionKey string, turnID int64) string {
	t.Helper()
	card := statusservice.MattermostCard{
		ChannelID: "stop-channel", PostID: "status-post-1",
		Actions: []statusservice.MattermostCardAction{{ID: "stopturn", Context: map[string]any{
			"kind": "agent_turn", "action": action, "turn_ids": strconv.FormatInt(turnID, 10),
			"resource_type": "agent_session_turn", "resource_id": strconv.FormatInt(turnID, 10),
		}}},
	}
	if err := security.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner-user", UserName: "owner"}, statusservice.InteractionScope{
		Workspace: strconv.FormatInt(projectID, 10), Session: sessionKey,
	}); err != nil {
		t.Fatalf("подписать action capability: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"user_id": "owner-user", "channel_id": card.ChannelID, "post_id": card.PostID, "context": card.Actions[0].Context,
	})
	if err != nil {
		t.Fatalf("сериализовать action body: %v", err)
	}
	return string(payload)
}

func (fixture *postgresStopHTTPFixture) assertPendingAutomation(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := automationspostgres.NewRepository(fixture.pool).GetRun(ctx, fixture.scheduledRun, fixture.projectID, "owner-user")
	if err != nil || run.Status != string(value.AutomationRunStatusRunning) {
		t.Fatalf("pending run=%#v error=%v", run, err)
	}
	var terminalAudit int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events where scheduled_run_id = $1 and event_type = 'run.runtime_terminal'`, run.ID).Scan(&terminalAudit); err != nil || terminalAudit != 0 {
		t.Fatalf("pending terminal audit=%d error=%v", terminalAudit, err)
	}
}

func (fixture *postgresStopHTTPFixture) assertTerminalExactlyOnce(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	turn, err := fixture.admin.GetAgentSessionTurn(ctx, fixture.turnID)
	if err != nil || turn.Status != "canceled" {
		t.Fatalf("turn=%#v error=%v", turn, err)
	}
	artifacts := map[string]any{}
	if err := json.Unmarshal([]byte(turn.Artifacts), &artifacts); err != nil {
		t.Fatalf("прочитать stop artifacts: %v", err)
	}
	for _, key := range []string{"matter-codex-stop-cleanup-completed", "matter-codex-stop-reset-completed", "matter-codex-stop-card-completed", "matter-codex-stop-process-completed", "matter-codex-stop-automation-completed"} {
		if artifacts[key] != "true" {
			t.Fatalf("stop phase %s=%#v artifacts=%#v", key, artifacts[key], artifacts)
		}
	}
	session, err := fixture.admin.GetAgentSession(ctx, fixture.sessionKey)
	if err != nil || session.Status == "running" || session.ActiveTurnID != 0 || session.ActiveRunID != "" || session.KubernetesNamespace != "" || session.PodName != "" || session.PVCName != "" || session.TokenSecretRef != "" {
		t.Fatalf("session=%#v error=%v", session, err)
	}
	run, err := automationspostgres.NewRepository(fixture.pool).GetRun(ctx, fixture.scheduledRun, fixture.projectID, "owner-user")
	if err != nil || run.Status != string(value.AutomationRunStatusFailed) || run.Outcome != string(value.AutomationRunOutcomeFailed) {
		t.Fatalf("scheduled run=%#v error=%v", run, err)
	}
	var occurrenceStatus, processStatus, claimStatus string
	if err := fixture.pool.QueryRow(ctx, `select status from matter_codex_schedule_occurrences where id = $1`, run.OccurrenceID).Scan(&occurrenceStatus); err != nil {
		t.Fatalf("прочитать occurrence: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select process.status, claim.status from matter_codex_process_runs process join matter_codex_work_claims claim on claim.process_run_id = process.id where process.id = $1`, fixture.processRunID).Scan(&processStatus, &claimStatus); err != nil {
		t.Fatalf("прочитать process reconciliation: %v", err)
	}
	var runCount, occurrenceCount, terminalAuditCount int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_scheduled_runs where schedule_id = $1`, run.ScheduleID).Scan(&runCount); err != nil {
		t.Fatalf("посчитать runs: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_schedule_occurrences where schedule_id = $1`, run.ScheduleID).Scan(&occurrenceCount); err != nil {
		t.Fatalf("посчитать occurrences: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_automation_audit_events where scheduled_run_id = $1 and event_type = 'run.runtime_terminal'`, run.ID).Scan(&terminalAuditCount); err != nil {
		t.Fatalf("посчитать terminal audit: %v", err)
	}
	if occurrenceStatus != string(value.AutomationRunStatusFailed) || processStatus != "completed" || claimStatus != "canceled" || runCount != 1 || occurrenceCount != 1 || terminalAuditCount != 1 {
		t.Fatalf("terminal occurrence=%q process=%q claim=%q runs=%d occurrences=%d audit=%d", occurrenceStatus, processStatus, claimStatus, runCount, occurrenceCount, terminalAuditCount)
	}
}
