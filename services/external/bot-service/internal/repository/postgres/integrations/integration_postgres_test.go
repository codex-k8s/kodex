//go:build postgres

package integrations_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/integration/recording"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	repository "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/integrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresFixture struct {
	pool      *pgxpool.Pool
	repo      *repository.Repository
	service   *domain.Service
	session   domain.SessionContext
	publisher *approvalPublisherStub
}

type sessionAdmissionStub struct {
	session domain.SessionContext
}

type authoritativeActorVerifier struct {
	human bool
	err   error
}

func (verifier *authoritativeActorVerifier) VerifyInteractionActor(_ context.Context, userID string, channelID string) (statusservice.MattermostInteractionActorProof, error) {
	return statusservice.MattermostInteractionActorProof{
		UserID: userID, ChannelID: channelID, Active: true, Human: verifier.human, ChannelMember: true,
	}, verifier.err
}

type integrationTokenRunner struct {
	runtimerepo.Runner
}

func (integrationTokenRunner) GetMattermostBotTokenSecret(_ context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error) {
	return runtimerepo.MattermostBotTokenSecret{SecretName: secretName, Token: "session-bearer"}, nil
}

func (stub sessionAdmissionStub) AuthorizeIntegrationSession(_ context.Context, sessionKey string, token string) (domain.SessionContext, error) {
	if sessionKey != stub.session.SessionKey || token != "session-bearer" {
		return domain.SessionContext{}, domain.ErrUnauthorized
	}
	return stub.session, nil
}

type approvalPublisherStub struct {
	mu         sync.Mutex
	deliveries []domain.ApprovalDelivery
}

type approvalCardCapture struct {
	statusservice.MattermostThreadPublisher
	card statusservice.MattermostCard
}

func (capture *approvalCardCapture) ReconcileOrPostThreadCard(_ context.Context, card statusservice.MattermostCard) (statusservice.MattermostPostRef, error) {
	capture.card = card
	return statusservice.MattermostPostRef{ChannelID: card.ChannelID, PostID: "captured-approval-post"}, nil
}

func (stub *approvalPublisherStub) EnsureApprovalCard(_ context.Context, delivery domain.ApprovalDelivery) (string, error) {
	stub.mu.Lock()
	stub.deliveries = append(stub.deliveries, delivery)
	stub.mu.Unlock()
	return "approval-post-" + delivery.ApprovalPublicID, nil
}

func TestIntegrationApprovalVerticalSliceExactlyOnce(t *testing.T) {
	fixture := newPostgresFixture(t, "vertical")
	ctx := context.Background()

	rejected := fixture.request(t, "restart:test:reject:0001")
	if rejected.Status != domain.InvocationStatusPending || fixture.executionCount(t) != 0 {
		t.Fatalf("request status=%s executions=%d", rejected.Status, fixture.executionCount(t))
	}
	fixture.decide(t, rejected, domain.ApprovalDecisionReject)
	if fixture.executionCount(t) != 0 {
		t.Fatal("reject создал recording execution")
	}

	approved := fixture.request(t, "restart:test:approve:0001")
	if _, err := fixture.pool.Exec(ctx, `update matter_codex_agent_roles set name = 'renamed-after-request', role_type = 'reviewer' where id = $1`, fixture.session.RoleID); err != nil {
		t.Fatalf("rename role after request: %v", err)
	}
	fixture.concurrentDecision(t, approved, domain.ApprovalDecisionApprove)
	if fixture.executionCount(t) != 0 {
		t.Fatal("approve выполнил executor синхронно с human callback")
	}
	fixture.concurrentWorkers(t, 8)
	if fixture.executionCount(t) != 1 {
		t.Fatalf("concurrent workers receipts=%d", fixture.executionCount(t))
	}
	replay := fixture.request(t, "restart:test:approve:0001")
	if replay.Status != domain.InvocationStatusSucceeded || replay.Execution == nil || replay.Execution.ExecutionID == "" {
		t.Fatalf("replay status=%s execution=%+v", replay.Status, replay.Execution)
	}
	fixture.concurrentWorkers(t, 4)
	if fixture.executionCount(t) != 1 {
		t.Fatalf("repeated workers receipts=%d", fixture.executionCount(t))
	}
	if _, err := fixture.service.DecideApproval(ctx, fixture.decisionInput(t, approved, domain.ApprovalDecisionApprove)); err == nil {
		t.Fatal("approval после terminal execution не отклонён")
	}
	if fixture.executionCount(t) != 1 {
		t.Fatal("повтор approval создал вторую квитанцию")
	}
	var auditCount, auditTypes int
	if err := fixture.pool.QueryRow(ctx, `
select count(*), count(distinct event_type)
from matter_codex_audit_events
where correlation_id = (select correlation_id from matter_codex_tool_invocations where public_id = $1)
`, approved.InvocationID).Scan(&auditCount, &auditTypes); err != nil {
		t.Fatalf("read integration audit chain: %v", err)
	}
	if auditCount != 3 || auditTypes != 3 {
		t.Fatalf("integration audit chain events=%d types=%d", auditCount, auditTypes)
	}
	if _, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_test_executions set result = '{}'::jsonb`); err == nil {
		t.Fatal("immutable execution receipt was modified")
	}
}

func TestIntegrationSessionAdmissionUsesExactRootOrDirectHuman(t *testing.T) {
	fixture := newPostgresFixture(t, "session_admission")
	store := adminpostgres.NewRepository(fixture.pool)
	service := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Store: store, RuntimeRunner: integrationTokenRunner{}, StorageReady: true, RuntimeReady: true,
	})
	direct, err := service.AuthorizeIntegrationSession(context.Background(), fixture.session.SessionKey, "session-bearer")
	if err != nil || direct.ApproverUserID != "direct-human" || direct.SubjectRef != fixture.session.SubjectRef {
		t.Fatalf("direct human admission=%+v err=%v", direct, err)
	}
	var policyID, processID int64
	if err := fixture.pool.QueryRow(context.Background(), `
insert into matter_codex_policy_revisions(project_id, version, status, settings, activated_at)
values ($1, 1, 'active', '{}'::jsonb, now()) returning id
`, fixture.session.ProjectID).Scan(&policyID); err != nil {
		t.Fatalf("seed integration policy revision: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
insert into matter_codex_process_runs(
	public_id, project_id, policy_revision_id, root_role_id,
	root_initiator_user_id, root_initiator_user_name, root_trigger_post_id,
	root_channel_id, root_thread_post_id, status
) values ('process_integration_admission', $1, $2, $3, 'root-human', 'root-owner',
	'integration-post', 'integration-channel', 'integration-root', 'running')
returning id
`, fixture.session.ProjectID, policyID, fixture.session.RoleID).Scan(&processID); err != nil {
		t.Fatalf("seed integration process: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_process_turns(turn_id, process_run_id, launch_post_id)
values ($1, $2, 'integration-post')
`, fixture.session.TurnID, processID); err != nil {
		t.Fatalf("bind integration turn process: %v", err)
	}
	root, err := service.AuthorizeIntegrationSession(context.Background(), fixture.session.SessionKey, "session-bearer")
	if err != nil || root.ApproverUserID != "root-human" || root.ApproverUserName != "root-owner" {
		t.Fatalf("root human admission=%+v err=%v", root, err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_mattermost_bot_identities(
	project_id, role_id, username, display_name, mattermost_user_id, token_secret_ref, status
) values ($1, $2, 'integration-approval-bot', 'Integration approval bot', 'root-human', 'bot-secret-ref', 'active')
`, fixture.session.ProjectID, fixture.session.RoleID); err != nil {
		t.Fatalf("seed root bot identity: %v", err)
	}
	if _, err := service.AuthorizeIntegrationSession(context.Background(), fixture.session.SessionKey, "session-bearer"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("bot root initiator admission error=%v", err)
	}
}

func TestIntegrationSessionAdmissionRejectsEveryDirectKubernetesMutationPath(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*postgresFixture)
	}{
		{name: "current role cluster admin", mutate: func(fixture *postgresFixture) {
			tx, err := fixture.pool.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin legacy cluster-admin fixture: %v", err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(context.Background(), `set local session_replication_role = replica`); err != nil {
				t.Fatalf("disable mutation trigger for legacy fixture: %v", err)
			}
			if _, err := tx.Exec(context.Background(), `update matter_codex_agent_roles set kubernetes_access = 'cluster-admin' where id = $1`, fixture.session.RoleID); err != nil {
				t.Fatalf("seed legacy current cluster-admin role: %v", err)
			}
			if err := tx.Commit(context.Background()); err != nil {
				t.Fatalf("commit legacy current cluster-admin role: %v", err)
			}
		}},
		{name: "frozen session cluster admin", mutate: func(fixture *postgresFixture) {
			_, err := fixture.pool.Exec(context.Background(), `
update matter_codex_agent_sessions
set capabilities = jsonb_set(capabilities, '{kubernetes_access}', '"cluster-admin"'::jsonb, true)
where id = $1
`, fixture.session.SessionID)
			if err != nil {
				t.Fatalf("seed frozen cluster-admin capability: %v", err)
			}
		}},
		{name: "frozen direct kubeconfig", mutate: func(fixture *postgresFixture) {
			_, err := fixture.pool.Exec(context.Background(), `
update matter_codex_agent_sessions
set capabilities = jsonb_set(capabilities, '{runtime_env}', '[{"name":"KUBECONFIG"}]'::jsonb, true)
where id = $1
`, fixture.session.SessionID)
			if err != nil {
				t.Fatalf("seed frozen kubeconfig capability: %v", err)
			}
		}},
		{name: "current direct service account token", mutate: func(fixture *postgresFixture) {
			var variableID int64
			if err := fixture.pool.QueryRow(context.Background(), `
insert into matter_codex_project_runtime_variables(project_id, name, slug, secret_ref)
values ($1, 'KUBERNETES_SERVICE_ACCOUNT_TOKEN_FILE', 'direct-kube-token', 'synthetic-direct-kube-token-ref')
returning id
`, fixture.session.ProjectID).Scan(&variableID); err != nil {
				t.Fatalf("seed current direct Kubernetes variable: %v", err)
			}
			if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_agent_role_runtime_variables(role_id, variable_id) values ($1, $2)
`, fixture.session.RoleID, variableID); err != nil {
				t.Fatalf("bind current direct Kubernetes variable: %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPostgresFixture(t, "direct_path_"+strings.ReplaceAll(test.name, " ", "_"))
			test.mutate(fixture)
			store := adminpostgres.NewRepository(fixture.pool)
			admission := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
				Store: store, RuntimeRunner: integrationTokenRunner{}, StorageReady: true, RuntimeReady: true,
			})
			service := domain.NewService(domain.ServiceConfig{
				Repository: fixture.repo, Admission: admission, CardPublisher: fixture.publisher,
			})
			if _, err := service.Catalog(context.Background(), fixture.session.SessionKey, "session-bearer"); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("managed tools/list admission error=%v", err)
			}
			if _, err := service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", fixture.input("restart:test:direct-path:0001")); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("managed tools/call admission error=%v", err)
			}
			var invocations, approvals int
			if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_tool_invocations`).Scan(&invocations); err != nil {
				t.Fatalf("count denied invocations: %v", err)
			}
			if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_approval_requests`).Scan(&approvals); err != nil {
				t.Fatalf("count denied approvals: %v", err)
			}
			fixture.publisher.mu.Lock()
			cards := len(fixture.publisher.deliveries)
			fixture.publisher.mu.Unlock()
			if invocations != 0 || approvals != 0 || cards != 0 || fixture.executionCount(t) != 0 {
				t.Fatalf("denied path side effects: invocations=%d approvals=%d cards=%d executions=%d", invocations, approvals, cards, fixture.executionCount(t))
			}
		})
	}
}

func TestIntegrationConcurrentRequestsReuseOneInvocation(t *testing.T) {
	fixture := newPostgresFixture(t, "request_race")
	const count = 8
	results := make(chan domain.ToolResult, count)
	errorsCh := make(chan error, count)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := fixture.service.RestartWorkload(
				context.Background(), fixture.session.SessionKey, "session-bearer",
				fixture.input("restart:test:request-race:0001"),
			)
			results <- result
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent request: %v", err)
		}
	}
	var expected domain.ToolResult
	for result := range results {
		if expected.InvocationID == "" {
			expected = result
		}
		if result.Status != domain.InvocationStatusPending || result.InvocationID != expected.InvocationID ||
			result.ApprovalID != expected.ApprovalID || result.ArgumentsHash != expected.ArgumentsHash {
			t.Fatalf("concurrent replay mismatch: first=%+v current=%+v", expected, result)
		}
	}
	var invocations, approvals int
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_tool_invocations`).Scan(&invocations); err != nil {
		t.Fatalf("count concurrent invocations: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_approval_requests`).Scan(&approvals); err != nil {
		t.Fatalf("count concurrent approvals: %v", err)
	}
	fixture.publisher.mu.Lock()
	deliveries := len(fixture.publisher.deliveries)
	fixture.publisher.mu.Unlock()
	if invocations != 1 || approvals != 1 || deliveries != 1 || fixture.executionCount(t) != 0 {
		t.Fatalf("concurrent request invocations=%d approvals=%d deliveries=%d receipts=%d", invocations, approvals, deliveries, fixture.executionCount(t))
	}
}

func TestIntegrationCrashRecoveryReusesImmutableReceipt(t *testing.T) {
	fixture := newPostgresFixture(t, "crash")
	result := fixture.request(t, "restart:test:crash:0001")
	fixture.decide(t, result, domain.ApprovalDecisionApprove)
	crash := errors.New("synthetic crash after receipt")
	worker := domain.NewWorker(domain.WorkerConfig{
		Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader), WorkerID: "worker-crash",
		Lease: time.Second, Hooks: domain.WorkerHooks{AfterReceipt: func(domain.ExecutionReceipt) error { return crash }},
	})
	worked, err := worker.RunOnce(context.Background())
	if !worked || !errors.Is(err, crash) || fixture.executionCount(t) != 1 {
		t.Fatalf("crash worked=%v err=%v receipts=%d", worked, err, fixture.executionCount(t))
	}
	if _, err := fixture.pool.Exec(context.Background(), `
update matter_codex_tool_invocations
set execution_lease_expires_at = now() - interval '1 second'
where public_id = $1 and state = 'executing'
`, result.InvocationID); err != nil {
		t.Fatalf("expire synthetic worker lease: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_grants set enabled = false, revision = revision + 1`); err != nil {
		t.Fatalf("revoke grant after committed receipt: %v", err)
	}
	recovery := domain.NewWorker(domain.WorkerConfig{
		Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader), WorkerID: "worker-recovery",
	})
	worked, err = recovery.RunOnce(context.Background())
	if !worked || err != nil || fixture.executionCount(t) != 1 {
		t.Fatalf("recovery worked=%v err=%v receipts=%d", worked, err, fixture.executionCount(t))
	}
	var state string
	if err := fixture.pool.QueryRow(context.Background(), `select state from matter_codex_tool_invocations where public_id = $1`, result.InvocationID).Scan(&state); err != nil {
		t.Fatalf("read recovered invocation: %v", err)
	}
	if state != string(domain.InvocationStatusSucceeded) {
		t.Fatalf("recovered invocation state=%q", state)
	}
}

func TestIntegrationConcurrentApprovalAndWorkersCreateOneReceipt(t *testing.T) {
	fixture := newPostgresFixture(t, "approval_worker_race")
	result := fixture.request(t, "restart:test:approval-worker-race:0001")
	decision := fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	start := make(chan struct{})
	errorsCh := make(chan error, 9)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		_, err := fixture.service.DecideApproval(context.Background(), decision)
		errorsCh <- err
	}()
	for index := range 8 {
		wait.Add(1)
		go func(workerIndex int) {
			defer wait.Done()
			<-start
			worker := domain.NewWorker(domain.WorkerConfig{
				Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader),
				WorkerID: fmt.Sprintf("worker-race-%d", workerIndex),
			})
			_, err := worker.RunOnce(context.Background())
			errorsCh <- err
		}(index)
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent approval/worker: %v", err)
		}
	}
	if fixture.executionCount(t) == 0 {
		worker := domain.NewWorker(domain.WorkerConfig{
			Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader), WorkerID: "worker-race-retry",
		})
		if worked, err := worker.RunOnce(context.Background()); !worked || err != nil {
			t.Fatalf("approval/worker recovery worked=%v err=%v", worked, err)
		}
	}
	if fixture.executionCount(t) != 1 {
		t.Fatalf("concurrent approval/worker receipts=%d", fixture.executionCount(t))
	}
	fixture.concurrentWorkers(t, 4)
	if fixture.executionCount(t) != 1 {
		t.Fatalf("repeated workers after approval race receipts=%d", fixture.executionCount(t))
	}
}

func TestIntegrationApprovalDecisionAndCapabilityConsumeAreAtomic(t *testing.T) {
	fixture := newPostgresFixture(t, "approval_atomic")
	result := fixture.request(t, "restart:test:approval-atomic:0001")
	decision := fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	store := adminpostgres.NewRepository(fixture.pool)
	now := time.Now().UTC()
	issue := securityrepo.IssueCapabilityInput{
		TokenHash: bytes.Repeat([]byte{1}, 32), ContextHash: bytes.Repeat([]byte{2}, 32),
		Kind: "action", Operation: "kind=integration_approval;action=approve;",
		ResourceType: "integration_approval", ResourceID: result.ApprovalID,
		ChannelID: "integration-channel", PostBinding: decision.PostID,
		ActorUserID: "direct-human", ActorUserName: "owner",
		InstallationScope: domain.InstallationScope, WorkspaceScope: fixture.session.WorkspaceScope,
		SessionScope: fixture.session.SessionKey, IssuedAt: now, ExpiresAt: now.Add(time.Minute),
		State: securityrepo.CapabilityStateUnused,
	}
	if err := store.IssueInteractionCapability(context.Background(), issue); err != nil {
		t.Fatalf("issue atomic callback capability: %v", err)
	}
	consume := securityrepo.ConsumeCapabilityInput{
		TokenHash: issue.TokenHash, ContextHash: issue.ContextHash, Kind: issue.Kind, Operation: issue.Operation,
		ResourceType: issue.ResourceType, ResourceID: issue.ResourceID, ChannelID: issue.ChannelID,
		PostBinding: issue.PostBinding, ActorUserID: issue.ActorUserID, Now: now,
	}
	decide := func(input domain.ApprovalDecisionInput) func(adminrepo.Repository) error {
		return func(transactional adminrepo.Repository) error {
			decisionStore, ok := transactional.(interface {
				DecideIntegrationApproval(context.Context, domain.ApprovalDecisionInput) (domain.Invocation, error)
			})
			if !ok {
				return errors.New("transactional integration decision adapter is unavailable")
			}
			_, err := decisionStore.DecideIntegrationApproval(context.Background(), input)
			return err
		}
	}
	badDecision := decision
	badDecision.ApprovalBindingHash = strings.Repeat("0", 64)
	if _, err := store.ConsumeInteractionCapabilityWithMutation(context.Background(), consume, decide(badDecision)); !errors.Is(err, domain.ErrApprovalBinding) {
		t.Fatalf("atomic decision rollback error=%v", err)
	}
	capability, err := store.CheckInteractionCapability(context.Background(), consume)
	if err != nil || capability.State != securityrepo.CapabilityStateUnused {
		t.Fatalf("callback capability was not rolled back: state=%s err=%v", capability.State, err)
	}
	if _, err := store.ConsumeInteractionCapabilityWithMutation(context.Background(), consume, decide(decision)); err != nil {
		t.Fatalf("atomic decision retry: %v", err)
	}
	var capabilityState, approvalState, invocationState string
	if err := fixture.pool.QueryRow(context.Background(), `
select capability.status, approval.state, invocation.state
from matter_codex_interaction_capabilities capability
join matter_codex_approval_requests approval on approval.public_id = $1
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
where capability.token_hash = $2
`, result.ApprovalID, issue.TokenHash).Scan(&capabilityState, &approvalState, &invocationState); err != nil {
		t.Fatalf("read atomic approval state: %v", err)
	}
	if capabilityState != "consumed" || approvalState != "approved" || invocationState != "approved" || fixture.executionCount(t) != 0 {
		t.Fatalf("atomic callback=%q approval=%q invocation=%q receipts=%d", capabilityState, approvalState, invocationState, fixture.executionCount(t))
	}
}

func TestIntegrationServerSideAdmissionAndAtomicHumanCallback(t *testing.T) {
	for _, decision := range []domain.ApprovalDecision{domain.ApprovalDecisionApprove, domain.ApprovalDecisionReject} {
		t.Run(string(decision), func(t *testing.T) {
			fixture := newPostgresFixture(t, "callback_"+string(decision))
			result := fixture.request(t, "restart:test:callback:"+string(decision)+":0001")
			input := fixture.decisionInput(t, result, decision)
			verifier := &authoritativeActorVerifier{human: true}
			store := adminpostgres.NewRepository(fixture.pool)
			allowed, err := store.AdmitInteractionResource(context.Background(), securityrepo.InteractionResourceAdmissionInput{
				ActionKey: "mattermost.callback.action", Operation: "action;kind=integration_approval;action=" + string(decision),
				ResourceType: "integration_approval", ResourceID: result.ApprovalID,
				ActorUserID: input.ActorUserID, ChannelID: input.ChannelID, PostID: input.PostID,
				Installation: domain.InstallationScope, Workspace: fixture.session.WorkspaceScope, Session: fixture.session.SessionKey,
			})
			if err != nil || !allowed {
				t.Fatalf("server-side AdmitInteractionResource() allowed=%t error=%v", allowed, err)
			}
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
				Repository: store,
				Admission: statusservice.NewServerSideInteractionAdmission(
					domain.InstallationScope, verifier, store,
				),
				ActorVerifier: verifier,
			})
			card := integrationApprovalCallbackCard(fixture, result, input, decision)
			if err := security.SealCardPending(context.Background(), &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
				t.Fatalf("SealCardPending() error=%v", err)
			}
			if err := security.ActivateCard(context.Background(), card); err != nil {
				t.Fatalf("ActivateCard() error=%v", err)
			}
			callback := statusservice.ActionCallback{
				Context: card.Actions[0].Context, UserID: input.ActorUserID,
				ChannelID: input.ChannelID, PostID: input.PostID,
			}
			interaction, err := security.AuthenticateActionAtomic(context.Background(), callback, func(interaction statusservice.AuthenticatedInteraction, transactional adminrepo.Repository) error {
				decisionStore, ok := transactional.(interface {
					DecideIntegrationApproval(context.Context, domain.ApprovalDecisionInput) (domain.Invocation, error)
				})
				if !ok {
					return errors.New("transactional integration decision adapter is unavailable")
				}
				input.ActorUserID = interaction.Actor.UserID
				input.ActorUserName = interaction.Actor.UserName
				_, err := decisionStore.DecideIntegrationApproval(context.Background(), input)
				return err
			})
			if err != nil {
				t.Fatalf("authoritative callback unexpectedly became denied/indeterminate: %v", err)
			}
			if interaction.ResourceID != result.ApprovalID || interaction.Actor.UserID != "direct-human" {
				t.Fatalf("authenticated interaction=%+v", interaction)
			}
			var capabilityState, approvalState, invocationState string
			if err := fixture.pool.QueryRow(context.Background(), `
select capability.status, approval.state, invocation.state
from matter_codex_interaction_capabilities capability
join matter_codex_approval_requests approval on approval.public_id = $1
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
where capability.resource_type = 'integration_approval'
	and capability.resource_id = approval.public_id
`, result.ApprovalID).Scan(&capabilityState, &approvalState, &invocationState); err != nil {
				t.Fatalf("read authoritative callback state: %v", err)
			}
			expectedState := "approved"
			if decision == domain.ApprovalDecisionReject {
				expectedState = "rejected"
			}
			if capabilityState != "consumed" || approvalState != expectedState || invocationState != expectedState || fixture.executionCount(t) != 0 {
				t.Fatalf("callback capability=%q approval=%q invocation=%q receipts=%d", capabilityState, approvalState, invocationState, fixture.executionCount(t))
			}
		})
	}
}

func TestIntegrationExternalUnprojectedBotCallbackFailsClosed(t *testing.T) {
	fixture := newPostgresFixture(t, "external_bot_callback")
	result := fixture.request(t, "restart:test:external-bot:0001")
	input := fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	verifier := &authoritativeActorVerifier{human: true}
	store := adminpostgres.NewRepository(fixture.pool)
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Repository: store,
		Admission: statusservice.NewServerSideInteractionAdmission(
			domain.InstallationScope, verifier, store,
		),
		ActorVerifier: verifier,
	})
	card := integrationApprovalCallbackCard(fixture, result, input, domain.ApprovalDecisionApprove)
	if err := security.SealCardPending(context.Background(), &card, card.Interaction.Actor, card.Interaction.Scope); err != nil {
		t.Fatalf("SealCardPending() error=%v", err)
	}
	if err := security.ActivateCard(context.Background(), card); err != nil {
		t.Fatalf("initial human activation error=%v", err)
	}
	var localBotIdentities int
	if err := fixture.pool.QueryRow(context.Background(), `
select count(*) from matter_codex_mattermost_bot_identities where mattermost_user_id = 'direct-human'
`).Scan(&localBotIdentities); err != nil || localBotIdentities != 0 {
		t.Fatalf("external actor unexpectedly projected into local bot table: count=%d error=%v", localBotIdentities, err)
	}
	verifier.human = false
	callback := statusservice.ActionCallback{
		Context: card.Actions[0].Context, UserID: input.ActorUserID,
		ChannelID: input.ChannelID, PostID: input.PostID,
	}
	if _, err := security.AuthenticateActionAtomic(context.Background(), callback, func(statusservice.AuthenticatedInteraction, adminrepo.Repository) error {
		t.Fatal("external bot reached atomic approval mutation")
		return nil
	}); !errors.Is(err, statusservice.ErrInteractionAdmissionDenied) {
		t.Fatalf("external bot callback error=%v", err)
	}
	token, _ := card.Actions[0].Context["capability"].(string)
	tokenHash := sha256.Sum256([]byte(token))
	var capabilityState, approvalState, invocationState string
	if err := fixture.pool.QueryRow(context.Background(), `
select capability.status, approval.state, invocation.state
from matter_codex_interaction_capabilities capability
join matter_codex_approval_requests approval on approval.public_id = $1
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
where capability.token_hash = $2
`, result.ApprovalID, tokenHash[:]).Scan(&capabilityState, &approvalState, &invocationState); err != nil {
		t.Fatalf("read denied bot callback state: %v", err)
	}
	if capabilityState != "unused" || approvalState != "pending" || invocationState != "pending" || fixture.executionCount(t) != 0 {
		t.Fatalf("denied bot capability=%q approval=%q invocation=%q receipts=%d", capabilityState, approvalState, invocationState, fixture.executionCount(t))
	}
}

func integrationApprovalCallbackCard(fixture *postgresFixture, result domain.ToolResult, input domain.ApprovalDecisionInput, decision domain.ApprovalDecision) statusservice.MattermostCard {
	return statusservice.MattermostCard{
		ChannelID: input.ChannelID, PostID: input.PostID,
		Actions: []statusservice.MattermostCardAction{{
			ID: string(decision), Context: map[string]any{
				"kind": "integration_approval", "action": string(decision),
				"resource_type": "integration_approval", "resource_id": result.ApprovalID,
				"approval_binding_sha256": input.ApprovalBindingHash,
			},
		}},
		Interaction: statusservice.MattermostCardInteraction{
			Actor: statusservice.AuthenticatedActor{UserID: input.ActorUserID, UserName: input.ActorUserName},
			Scope: statusservice.InteractionScope{
				Installation: domain.InstallationScope, Workspace: fixture.session.WorkspaceScope, Session: fixture.session.SessionKey,
			},
		},
	}
}

func TestIntegrationFreshAuthorizationAndNegativeMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*postgresFixture, domain.ToolResult)
	}{
		{"grant revoked", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_grants set enabled = false, revision = revision + 1`)
			if err != nil {
				t.Fatalf("revoke grant: %v", err)
			}
		}},
		{"grant expired", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_grants set expires_at = now() - interval '1 second', revision = revision + 1`)
			if err != nil {
				t.Fatalf("expire grant: %v", err)
			}
		}},
		{"connection disabled", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_connections set status = 'disabled', revision = revision + 1`)
			if err != nil {
				t.Fatalf("disable connection: %v", err)
			}
		}},
		{"connection revision changed", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_connections set revision = revision + 1`)
			if err != nil {
				t.Fatalf("revise connection: %v", err)
			}
		}},
		{"capability disabled", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_capabilities set status = 'disabled', revision = revision + 1`)
			if err != nil {
				t.Fatalf("disable capability: %v", err)
			}
		}},
		{"session token ref changed", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_agent_sessions set token_secret_ref = 'rotated-session-secret'`)
			if err != nil {
				t.Fatalf("rotate token reference: %v", err)
			}
		}},
		{"session root changed", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_agent_sessions set mattermost_root_post_id = 'foreign-root'`)
			if err != nil {
				t.Fatalf("rebind session root: %v", err)
			}
		}},
		{"session expired", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_agent_sessions set expires_at = now() - interval '1 second'`)
			if err != nil {
				t.Fatalf("expire session: %v", err)
			}
		}},
		{"session blocked", func(fixture *postgresFixture, _ domain.ToolResult) {
			_, err := fixture.pool.Exec(context.Background(), `update matter_codex_agent_sessions set status = 'blocked'`)
			if err != nil {
				t.Fatalf("block session: %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPostgresFixture(t, "fresh_"+strings.ReplaceAll(test.name, " ", "_"))
			result := fixture.request(t, "restart:test:fresh:0001")
			fixture.decide(t, result, domain.ApprovalDecisionApprove)
			test.mutate(fixture, result)
			worker := domain.NewWorker(domain.WorkerConfig{
				Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader), WorkerID: "worker-fresh",
			})
			worked, err := worker.RunOnce(context.Background())
			if !worked || !errors.Is(err, domain.ErrAuthorizationChanged) || fixture.executionCount(t) != 0 {
				t.Fatalf("fresh auth worked=%v err=%v receipts=%d", worked, err, fixture.executionCount(t))
			}
			var state, reason string
			if err := fixture.pool.QueryRow(context.Background(), `select state, reason_code from matter_codex_tool_invocations where public_id = $1`, result.InvocationID).Scan(&state, &reason); err != nil {
				t.Fatalf("read cancelled invocation: %v", err)
			}
			if state != string(domain.InvocationStatusCancelled) || reason != "execution.authorization_changed" {
				t.Fatalf("fresh auth terminal state=%q reason=%q", state, reason)
			}
		})
	}

	fixture := newPostgresFixture(t, "negative")
	result := fixture.request(t, "restart:test:negative:0001")
	wrong := fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	wrong.ActorUserID = "another-human"
	if _, err := fixture.service.DecideApproval(context.Background(), wrong); !errors.Is(err, domain.ErrApprovalActor) {
		t.Fatalf("self/foreign approval error=%v", err)
	}
	wrong = fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	wrong.ChannelID = "foreign-channel"
	if _, err := fixture.service.DecideApproval(context.Background(), wrong); !errors.Is(err, domain.ErrApprovalBinding) {
		t.Fatalf("foreign channel error=%v", err)
	}
	wrong = fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)
	wrong.ApprovalBindingHash = strings.Repeat("0", 64)
	if _, err := fixture.service.DecideApproval(context.Background(), wrong); !errors.Is(err, domain.ErrApprovalBinding) {
		t.Fatalf("tampered approval hash error=%v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_mattermost_bot_identities(
	project_id, role_id, username, display_name, mattermost_user_id, token_secret_ref, status
) values ($1, $2, 'late-approval-bot', 'Late approval bot', 'direct-human', 'bot-secret-ref', 'active')
`, fixture.session.ProjectID, fixture.session.RoleID); err != nil {
		t.Fatalf("seed late approval bot identity: %v", err)
	}
	if _, err := fixture.service.DecideApproval(context.Background(), fixture.decisionInput(t, result, domain.ApprovalDecisionApprove)); !errors.Is(err, domain.ErrApprovalTerminal) {
		t.Fatalf("late bot approval error=%v", err)
	}
	if _, err := fixture.service.RestartWorkload(context.Background(), "guessed-session", "session-bearer", fixture.input("restart:test:guessed:0001")); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("guessed session error=%v", err)
	}
	if _, err := fixture.service.RestartWorkload(context.Background(), fixture.session.SessionKey, "guessed-bearer", fixture.input("restart:test:guessed:0002")); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("guessed bearer error=%v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `update matter_codex_tool_invocations set arguments = '{"namespace":"tampered","workload_kind":"Deployment","workload_name":"bot-service"}' where public_id = $1`, result.InvocationID); err == nil {
		t.Fatal("immutable invocation arguments were modified")
	}
	if _, err := fixture.pool.Exec(context.Background(), `update matter_codex_approval_requests set approval_binding_sha256 = decode($2, 'hex') where public_id = $1`, result.ApprovalID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("immutable approval binding was modified")
	}
	if _, err := fixture.pool.Exec(context.Background(), `update matter_codex_integration_connections set credential_ref = $1`, "synthetic-credential-material-must-not-fit-reference"); err == nil {
		t.Fatal("connection accepted a credential value")
	}
}

func TestIntegrationRequestScopeAndIdempotencyNegativeMatrix(t *testing.T) {
	fixture := newPostgresFixture(t, "request_negative")
	result := fixture.request(t, "restart:test:binding-conflict:0001")

	for _, test := range []struct {
		name    string
		session func(domain.SessionContext) domain.SessionContext
		input   func(domain.RestartWorkloadInput) domain.RestartWorkloadInput
		err     error
	}{
		{"foreign subject", func(value domain.SessionContext) domain.SessionContext { value.SubjectRef = "999999"; return value }, nil, domain.ErrUnauthorized},
		{"foreign workspace", func(value domain.SessionContext) domain.SessionContext { value.WorkspaceScope = "999999"; return value }, nil, domain.ErrUnauthorized},
		{"foreign session", func(value domain.SessionContext) domain.SessionContext {
			value.SessionKey = "foreign-session"
			return value
		}, nil, domain.ErrUnauthorized},
		{"foreign connection", nil, func(value domain.RestartWorkloadInput) domain.RestartWorkloadInput {
			value.Connection = "recording-foreign"
			return value
		}, domain.ErrUnauthorized},
		{"target outside constraints", nil, func(value domain.RestartWorkloadInput) domain.RestartWorkloadInput {
			value.WorkloadName = "outside-grant"
			return value
		}, domain.ErrUnauthorized},
		{"empty target", nil, func(value domain.RestartWorkloadInput) domain.RestartWorkloadInput {
			value.WorkloadName = ""
			return value
		}, domain.ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := fixture.session
			if test.session != nil {
				session = test.session(session)
			}
			input := fixture.input("restart:test:negative-scope:" + strings.ReplaceAll(test.name, " ", "-"))
			if test.input != nil {
				input = test.input(input)
			}
			service := domain.NewService(domain.ServiceConfig{
				Repository: fixture.repo, Admission: sessionAdmissionStub{session: session}, CardPublisher: fixture.publisher,
			})
			_, err := service.RestartWorkload(context.Background(), session.SessionKey, "session-bearer", input)
			if !errors.Is(err, test.err) {
				t.Fatalf("negative request error=%v want=%v", err, test.err)
			}
		})
	}

	fixture.seedConnectionGrant(t, "recording-secondary", "bot-service")
	secondary := fixture.input("restart:test:binding-conflict:0001")
	secondary.Connection = "recording-secondary"
	if _, err := fixture.service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", secondary); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("connection binding conflict error=%v", err)
	}
	fixture.seedTargetGrant(t, "grant-recording-main-other", "bot-service-v2")
	otherTarget := fixture.input("restart:test:binding-conflict:0001")
	otherTarget.WorkloadName = "bot-service-v2"
	if _, err := fixture.service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", otherTarget); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("arguments binding conflict error=%v", err)
	}
	var invocations, approvals int
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_tool_invocations`).Scan(&invocations); err != nil {
		t.Fatalf("count invocations after conflicts: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_approval_requests`).Scan(&approvals); err != nil {
		t.Fatalf("count approvals after conflicts: %v", err)
	}
	if result.Status != domain.InvocationStatusPending || invocations != 1 || approvals != 1 || fixture.executionCount(t) != 0 {
		t.Fatalf("conflict evidence result=%+v invocations=%d approvals=%d receipts=%d", result, invocations, approvals, fixture.executionCount(t))
	}
}

func TestIntegrationSyntheticOnlyCanaryProductionProjections(t *testing.T) {
	fixture := newPostgresFixture(t, "synthetic_canary")
	rawCanary := `synthetic-only-issue93:"credential/value+20260721`
	referenceCanary := "synthetic-issue93-session-reference-20260721"
	fixture.session.SessionTokenSecretRef = referenceCanary
	if _, err := fixture.pool.Exec(context.Background(), `
update matter_codex_agent_sessions set token_secret_ref = $2 where id = $1
`, fixture.session.SessionID, referenceCanary); err != nil {
		t.Fatalf("seed synthetic-only credential reference: %v", err)
	}
	service := domain.NewService(domain.ServiceConfig{
		Repository:    fixture.repo,
		Admission:     syntheticCanaryAdmission{session: fixture.session, token: rawCanary},
		CardPublisher: fixture.publisher,
	})
	result, err := service.RestartWorkload(context.Background(), fixture.session.SessionKey, rawCanary, fixture.input("restart:test:synthetic-canary:0001"))
	if err != nil {
		t.Fatalf("RestartWorkload() synthetic-only boundary error=%v", err)
	}
	fixture.publisher.mu.Lock()
	if len(fixture.publisher.deliveries) != 1 {
		fixture.publisher.mu.Unlock()
		t.Fatalf("synthetic-only deliveries=%d", len(fixture.publisher.deliveries))
	}
	delivery := fixture.publisher.deliveries[0]
	fixture.publisher.mu.Unlock()

	var storedReference string
	if err := fixture.pool.QueryRow(context.Background(), `
select session_token_secret_ref from matter_codex_tool_invocations where public_id = $1
`, result.InvocationID).Scan(&storedReference); err != nil || storedReference != referenceCanary {
		t.Fatalf("synthetic reference did not cross authoritative source boundary: ref=%q error=%v", storedReference, err)
	}
	capture := &approvalCardCapture{}
	cardPublisher := statusservice.NewIntegrationApprovalPublisher(nil, capture, "https://bot.invalid/mattermost/actions/agents")
	if _, err := cardPublisher.EnsureApprovalCard(context.Background(), delivery); err != nil {
		t.Fatalf("render production Mattermost approval card: %v", err)
	}
	var auditProjection string
	if err := fixture.pool.QueryRow(context.Background(), `
select coalesce(string_agg(safe_metadata::text || summary || reason_code, E'\n'), '')
from matter_codex_audit_events where event_type like 'integration.%'
`).Scan(&auditProjection); err != nil {
		t.Fatalf("read production audit projection: %v", err)
	}
	projections, err := json.Marshal(map[string]any{
		"mcp_result":         result,
		"mcp_error":          domain.ReasonCode(errors.New(rawCanary)),
		"mattermost_card":    capture.card,
		"mattermost_payload": delivery,
		"audit":              auditProjection,
		"log_error_reason":   domain.ReasonCode(fmt.Errorf("synthetic executor: %s", rawCanary)),
	})
	if err != nil {
		t.Fatalf("encode production projections: %v", err)
	}
	assertSyntheticCanariesAbsent(t, string(projections), rawCanary, referenceCanary)
	if strings.Contains(string(projections), "credential_ref") || strings.Contains(string(projections), "session_token_secret_ref") {
		t.Fatalf("credential reference field leaked into outward production projection: %s", projections)
	}
}

type syntheticCanaryAdmission struct {
	session domain.SessionContext
	token   string
}

func (admission syntheticCanaryAdmission) AuthorizeIntegrationSession(_ context.Context, sessionKey string, token string) (domain.SessionContext, error) {
	if sessionKey != admission.session.SessionKey || token != admission.token {
		return domain.SessionContext{}, domain.ErrUnauthorized
	}
	return admission.session, nil
}

func assertSyntheticCanariesAbsent(t *testing.T, projection string, canaries ...string) {
	t.Helper()
	for _, canary := range canaries {
		jsonValue, err := json.Marshal(canary)
		if err != nil {
			t.Fatalf("encode synthetic canary: %v", err)
		}
		representations := []string{
			canary,
			string(jsonValue[1 : len(jsonValue)-1]),
			base64.StdEncoding.EncodeToString([]byte(canary)),
			base64.RawStdEncoding.EncodeToString([]byte(canary)),
			base64.URLEncoding.EncodeToString([]byte(canary)),
			base64.RawURLEncoding.EncodeToString([]byte(canary)),
		}
		for _, representation := range representations {
			if representation != "" && strings.Contains(projection, representation) {
				t.Fatalf("synthetic-only canary representation leaked: %q", representation)
			}
		}
	}
}

func TestIntegrationExpiredApprovalIsTerminalWithoutReceipt(t *testing.T) {
	fixture := newPostgresFixture(t, "approval_expired")
	now := time.Now().UTC()
	service := domain.NewService(domain.ServiceConfig{
		Repository: fixture.repo, Admission: sessionAdmissionStub{session: fixture.session}, CardPublisher: fixture.publisher,
		Now: func() time.Time { return now }, ApprovalTTL: time.Minute,
	})
	result, err := service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", fixture.input("restart:test:expired:0001"))
	if err != nil {
		t.Fatalf("request expiring approval: %v", err)
	}
	now = now.Add(2 * time.Minute)
	replay, err := service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", fixture.input("restart:test:expired:0001"))
	if err != nil {
		t.Fatalf("replay expiring approval: %v", err)
	}
	if replay.Status != domain.InvocationStatusExpired || replay.ReasonCode != "approval.expired" {
		t.Fatalf("expired replay=%+v", replay)
	}
	var approvalState, invocationState string
	if err := fixture.pool.QueryRow(context.Background(), `
select approval.state, invocation.state
from matter_codex_approval_requests approval
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
where approval.public_id = $1
`, result.ApprovalID).Scan(&approvalState, &invocationState); err != nil {
		t.Fatalf("read expired approval: %v", err)
	}
	if approvalState != "expired" || invocationState != "expired" || fixture.executionCount(t) != 0 {
		t.Fatalf("expired approval=%q invocation=%q receipts=%d", approvalState, invocationState, fixture.executionCount(t))
	}
	fixture.publisher.mu.Lock()
	deliveries := len(fixture.publisher.deliveries)
	fixture.publisher.mu.Unlock()
	if deliveries != 1 {
		t.Fatalf("passive expiry repeated approval card: deliveries=%d", deliveries)
	}
	var expiryAudits int
	if err := fixture.pool.QueryRow(context.Background(), `
select count(*) from matter_codex_audit_events
where event_type = 'integration.approval.decided' and reason_code = 'approval.expired'
	and safe_metadata ->> 'approval_id' = $1
`, result.ApprovalID).Scan(&expiryAudits); err != nil || expiryAudits != 1 {
		t.Fatalf("passive expiry audit count=%d error=%v", expiryAudits, err)
	}
}

func newPostgresFixture(t *testing.T, label string) *postgresFixture {
	t.Helper()
	dsn := testsupport.IsolatedSchemaDSN(t, "integration_"+label)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate integration fixture: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open integration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	var projectID, roleID, chatID, sessionID, turnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Integration proof', $1) returning id`, "integration-"+label).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'integration-worker', 'worker') returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'integration-channel', 'Integration', 'integration') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, status, token_secret_ref, ttl_seconds, expires_at
) values ('integration-session', $1, $2, $3, 'thread', 'integration-channel', 'integration-root', 'running', 'integration-session-secret', 3600, now() + interval '1 hour')
returning id
`, projectID, chatID, roleID).Scan(&sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(
	session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id,
	user_id, user_name, message, status, started_at
) values ($1, 'integration-run', 'integration-channel', 'integration-root', 'integration-post', 'direct-human', 'owner', 'test', 'running', now())
returning id
`, sessionID).Scan(&turnID); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = 'integration-run' where id = $1`, sessionID, turnID); err != nil {
		t.Fatalf("activate turn: %v", err)
	}
	var capabilityID int64
	if err := pool.QueryRow(ctx, `select id from matter_codex_integration_capabilities where capability_key = 'deployment.restart_workload' and version = 1`).Scan(&capabilityID); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	var connectionID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_integration_connections(
	public_id, capability_id, installation_scope, workspace_scope, status, revision
) values ('recording-main', $1, 'single-installation', $2, 'active', 1)
returning id
`, capabilityID, fmt.Sprint(projectID)).Scan(&connectionID); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_integration_grants(
	public_id, connection_id, capability_id, subject_kind, subject_ref,
	installation_scope, workspace_scope, session_scope,
	allowed_namespace, allowed_workload_kind, allowed_workload_name,
	enabled, valid_from, expires_at, revision
) values (
	'grant-recording-main', $1, $2, 'agent_role', $3,
	'single-installation', $4, 'integration-session',
	'mattermost', 'Deployment', 'bot-service', true, now() - interval '1 minute', now() + interval '1 hour', 1
)
`, connectionID, capabilityID, fmt.Sprint(roleID), fmt.Sprint(projectID)); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	session := domain.SessionContext{
		SessionID: sessionID, SessionKey: "integration-session", TurnID: turnID,
		ProjectID: projectID, ChatID: chatID, RoleID: roleID,
		SubjectKind: domain.SubjectKindAgentRole, SubjectRef: fmt.Sprint(roleID),
		InstallationScope: domain.InstallationScope, WorkspaceScope: fmt.Sprint(projectID),
		MattermostChannelID: "integration-channel", MattermostRootPostID: "integration-root",
		ApproverUserID: "direct-human", ApproverUserName: "owner",
		SessionTokenSecretRef: "integration-session-secret",
	}
	repo := repository.NewRepository(pool)
	publisher := &approvalPublisherStub{}
	service := domain.NewService(domain.ServiceConfig{
		Repository: repo, Admission: sessionAdmissionStub{session: session}, CardPublisher: publisher,
	})
	return &postgresFixture{pool: pool, repo: repo, service: service, session: session, publisher: publisher}
}

func (fixture *postgresFixture) input(key string) domain.RestartWorkloadInput {
	return domain.RestartWorkloadInput{
		Connection: "recording-main", Namespace: "mattermost", WorkloadKind: "Deployment",
		WorkloadName: "bot-service", IdempotencyKey: key,
	}
}

func (fixture *postgresFixture) seedConnectionGrant(t *testing.T, connectionPublicID string, workloadName string) {
	t.Helper()
	var capabilityID int64
	if err := fixture.pool.QueryRow(context.Background(), `select id from matter_codex_integration_capabilities where capability_key = 'deployment.restart_workload'`).Scan(&capabilityID); err != nil {
		t.Fatalf("read capability for additional connection: %v", err)
	}
	var connectionID int64
	if err := fixture.pool.QueryRow(context.Background(), `
insert into matter_codex_integration_connections(
	public_id, capability_id, installation_scope, workspace_scope, status, revision
) values ($1, $2, 'single-installation', $3, 'active', 1)
returning id
`, connectionPublicID, capabilityID, fixture.session.WorkspaceScope).Scan(&connectionID); err != nil {
		t.Fatalf("seed additional connection: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_integration_grants(
	public_id, connection_id, capability_id, subject_kind, subject_ref,
	installation_scope, workspace_scope, session_scope,
	allowed_namespace, allowed_workload_kind, allowed_workload_name,
	enabled, valid_from, expires_at, revision
) values ($1, $2, $3, 'agent_role', $4, 'single-installation', $5, $6,
	'mattermost', 'Deployment', $7, true, now() - interval '1 minute', now() + interval '1 hour', 1)
`, "grant-"+connectionPublicID, connectionID, capabilityID, fixture.session.SubjectRef, fixture.session.WorkspaceScope, fixture.session.SessionKey, workloadName); err != nil {
		t.Fatalf("seed additional connection grant: %v", err)
	}
}

func (fixture *postgresFixture) seedTargetGrant(t *testing.T, grantPublicID string, workloadName string) {
	t.Helper()
	if _, err := fixture.pool.Exec(context.Background(), `
insert into matter_codex_integration_grants(
	public_id, connection_id, capability_id, subject_kind, subject_ref,
	installation_scope, workspace_scope, session_scope,
	allowed_namespace, allowed_workload_kind, allowed_workload_name,
	enabled, valid_from, expires_at, revision
)
select $1, connection.id, connection.capability_id, 'agent_role', $2,
	'single-installation', $3, $4, 'mattermost', 'Deployment', $5,
	true, now() - interval '1 minute', now() + interval '1 hour', 1
from matter_codex_integration_connections connection
where connection.public_id = 'recording-main'
`, grantPublicID, fixture.session.SubjectRef, fixture.session.WorkspaceScope, fixture.session.SessionKey, workloadName); err != nil {
		t.Fatalf("seed additional target grant: %v", err)
	}
}

func (fixture *postgresFixture) request(t *testing.T, key string) domain.ToolResult {
	t.Helper()
	result, err := fixture.service.RestartWorkload(context.Background(), fixture.session.SessionKey, "session-bearer", fixture.input(key))
	if err != nil {
		t.Fatalf("RestartWorkload(%s): %v", key, err)
	}
	return result
}

func (fixture *postgresFixture) decisionInput(t *testing.T, result domain.ToolResult, decision domain.ApprovalDecision) domain.ApprovalDecisionInput {
	t.Helper()
	var binding, postID string
	if err := fixture.pool.QueryRow(context.Background(), `
select encode(approval_binding_sha256, 'hex'), mattermost_post_id
from matter_codex_approval_requests where public_id = $1
`, result.ApprovalID).Scan(&binding, &postID); err != nil {
		t.Fatalf("read approval binding: %v", err)
	}
	return domain.ApprovalDecisionInput{
		ApprovalPublicID: result.ApprovalID, ApprovalBindingHash: binding, Decision: decision,
		ActorUserID: "direct-human", ActorUserName: "owner", ChannelID: "integration-channel", PostID: postID,
	}
}

func (fixture *postgresFixture) decide(t *testing.T, result domain.ToolResult, decision domain.ApprovalDecision) {
	t.Helper()
	if _, err := fixture.service.DecideApproval(context.Background(), fixture.decisionInput(t, result, decision)); err != nil {
		t.Fatalf("DecideApproval(%s): %v", decision, err)
	}
}

func (fixture *postgresFixture) concurrentDecision(t *testing.T, result domain.ToolResult, decision domain.ApprovalDecision) {
	t.Helper()
	input := fixture.decisionInput(t, result, decision)
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.service.DecideApproval(context.Background(), input)
			errorsCh <- err
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent approval: %v", err)
		}
	}
}

func (fixture *postgresFixture) concurrentWorkers(t *testing.T, count int) {
	t.Helper()
	errorsCh := make(chan error, count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func(workerIndex int) {
			defer wait.Done()
			worker := domain.NewWorker(domain.WorkerConfig{
				Repository: fixture.repo, Executor: recording.New(fixture.repo, nil, rand.Reader),
				WorkerID: fmt.Sprintf("worker-%d", workerIndex),
			})
			_, err := worker.RunOnce(context.Background())
			errorsCh <- err
		}(index)
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent worker: %v", err)
		}
	}
}

func (fixture *postgresFixture) executionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from matter_codex_integration_test_executions`).Scan(&count); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	return count
}
