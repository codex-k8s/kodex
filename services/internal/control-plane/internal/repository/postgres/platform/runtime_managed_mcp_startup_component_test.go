package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/managed_mcp_startup_fixture.sql
var queryManagedMCPStartupFixture string

//go:embed testdata/sql/managed_mcp_startup_success.sql
var queryManagedMCPStartupSuccess string

//go:embed testdata/sql/managed_mcp_startup_counts.sql
var queryManagedMCPStartupCounts string

//go:embed testdata/sql/managed_mcp_startup_expired.sql
var queryManagedMCPStartupExpired string

//go:embed testdata/sql/managed_mcp_startup_failure.sql
var queryManagedMCPStartupFailure string

//go:embed testdata/sql/managed_mcp_startup_change_credential.sql
var queryManagedMCPStartupChangeCredential string

//go:embed testdata/sql/managed_mcp_startup_history.sql
var queryManagedMCPStartupHistory string

// Полный native claim → durable DUE → gateway probe → fresh runtime claim.
// Отдельная migrated disposable БД; upstream и provider не вызываются.
func TestManagedMCPStartupRecoveryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated startup recovery PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.runs.launch", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	controller := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	gateway := principal("integration-gateway", "platform.runtime.integration-tests.claim", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "startup-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("synthetic startup command %s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Startup recovery fixture", Language: "en"}).Project
	type fixture struct {
		connection entity.IntegrationConnection
		agent      entity.Agent
		run        entity.Run
	}
	prepare := func(key string, success bool) fixture {
		t.Helper()
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "startup-agent-"+key, "Startup "+key)
		c := *execute(command.CreateConnection, "connection-"+key, nil, command.ConnectionInput{DefinitionKey: "context7", Name: "Startup " + key, PublicConfiguration: map[string]any{"base_url": "https://mcp.context7.com"}}).Connection
		if _, err := pool.Exec(ctx, queryManagedMCPStartupFixture, pgx.StrictNamedArgs{"connection_ref": c.Ref}); err != nil {
			t.Fatal("prepare synthetic startup credential")
		}
		c, err = service.GetIntegrationConnection(ctx, owner, c.Ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, capability := range []string{"context7.library.resolve", "context7.docs.query"} {
			c = *execute(command.ChangeIntegrationGrant, key+capability, &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: capability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
		}
		if success {
			if _, err := pool.Exec(ctx, queryManagedMCPStartupSuccess, pgx.StrictNamedArgs{"connection_ref": c.Ref, "test_ref": "tst_startup_" + key}); err != nil {
				t.Fatal("prepare stale immutable success")
			}
		}
		run := *execute(command.LaunchRun, "run-"+key, nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Synthetic bounded startup task"}).Run
		return fixture{connection: c, agent: agent, run: run}
	}
	claim := func(key string) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: controller, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: "startup-fixture", Limit: 32}})
		if err != nil {
			t.Fatal("native startup claim failed", err)
		}
		return result
	}
	counts := func(f fixture, key string) (int, int, int, int, int, int, string) {
		t.Helper()
		var tasks, due, revisions, leases, audit, receipts int
		var created string
		if err := pool.QueryRow(ctx, queryManagedMCPStartupCounts, pgx.StrictNamedArgs{"connection_ref": f.connection.Ref, "run_ref": f.run.Ref, "idempotency_key": key}).Scan(&tasks, &due, &revisions, &leases, &audit, &receipts, &created); err != nil {
			t.Fatal("read exact startup counts")
		}
		return tasks, due, revisions, leases, audit, receipts, created
	}
	stale := prepare("success", true)
	var priorReceipt string
	if err := pool.QueryRow(ctx, queryManagedMCPStartupHistory, pgx.StrictNamedArgs{"test_ref": "tst_startup_success"}).Scan(&priorReceipt); err != nil {
		t.Fatal("read immutable prior receipt")
	}
	func() {
		original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
		queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
		defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
		_, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: controller, Mutation: value.Mutation{IdempotencyKey: "startup-audit-failure"}, Payload: command.LeaseInput{WorkloadInstance: "startup-fixture", Limit: 32}})
		if !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("startup queue audit failure was suppressed")
		}
	}()
	initialTasks, initialDue, initialRevisions, initialLeases, initialAudit, initialReceipts, _ := counts(stale, "startup-audit-failure")
	if initialTasks != 0 || initialDue != 0 || initialRevisions != 0 || initialLeases != 0 || initialAudit != 0 || initialReceipts != 0 {
		t.Fatal("audit rollback leaked startup task or candidate effects")
	}
	if len(claim("startup-first").RuntimeItems) != 0 {
		t.Fatal("stale proof granted runtime work")
	}
	tasks, due, revisions, leases, audit, receipts, created := counts(stale, "startup-first")
	if tasks != 1 || due != 1 || revisions != 0 || leases != 0 || audit != 1 || receipts != 1 || created == "" {
		t.Fatalf("startup DUE/rollback/receipt mismatch %d/%d/%d/%d/%d/%d", tasks, due, revisions, leases, audit, receipts)
	}
	current, err := service.GetRun(ctx, owner, stale.run.Ref)
	if err != nil || (current.State != "QUEUED" && current.State != "RUNNING") {
		t.Fatal("stale dependency terminalized run")
	}
	claim("startup-first")
	claim("startup-pending")
	tasks, due, revisions, leases, audit, receipts, repeated := counts(stale, "startup-first")
	if tasks != 1 || due != 1 || revisions != 0 || leases != 0 || audit != 1 || receipts != 1 || repeated != created {
		t.Fatal("replay/poll reset cycle or leaked candidate effects")
	}
	if _, err := service.ClaimIntegrationConnectionTests(ctx, controller, "wrong-startup-probe", 32); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("startup producer gained probe claimant authority")
	}
	claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "startup-probe", 32)
	if err != nil {
		t.Fatal("native gateway probe claim failed")
	}
	var probe map[string]any
	for _, item := range claims {
		if stringMap(item, "connectionRef") == stale.connection.Ref {
			probe = item
		}
	}
	if probe == nil {
		t.Fatal("server-created DUE did not reach sole gateway claimant")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "startup-probe-complete"}, Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(probe, "testRef"), LeaseRef: stringMap(probe, "leaseRef"), Fence: stringMap(probe, "fence"), Generation: probe["generation"].(int64), Success: true}})
	if err != nil {
		t.Fatal("synthetic native probe completion failed", err)
	}
	result := claim("startup-after-probe")
	if len(result.RuntimeItems) != 1 || stringMap(result.RuntimeItems[0], "runRef") != stale.run.Ref {
		t.Fatal("fresh probe did not recover exact native candidate")
	}
	if stringMap(result.RuntimeItems[0], "runtimeRevisionRef") == "" {
		t.Fatal("fresh claim lost immutable RuntimeRevision")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: controller, Mutation: value.Mutation{IdempotencyKey: "startup-complete"}, Payload: command.CompleteExecutionInput{LeaseRef: stringMap(result.RuntimeItems[0], "leaseRef"), Fence: stringMap(result.RuntimeItems[0], "fence"), Generation: runtimeRevisionMapInt64(result.RuntimeItems[0], "generation"), Success: true, ResultSummary: "Synthetic completion", Usage: turnUsageFixture()}})
	if err != nil {
		t.Fatal("finish recovered native attempt", err)
	}
	var historicalReceipt string
	if err := pool.QueryRow(ctx, queryManagedMCPStartupHistory, pgx.StrictNamedArgs{"test_ref": "tst_startup_success"}).Scan(&historicalReceipt); err != nil || historicalReceipt != priorReceipt {
		t.Fatal("startup recovery rewrote immutable prior receipt")
	}
	cold := prepare("cold", false)
	claim("startup-cold")
	current, err = service.GetRun(ctx, owner, cold.run.Ref)
	if err != nil || current.State != "FAILED" {
		t.Fatal("cold missing proof stopped failing closed")
	}
	tasks, _, _, _, _, _, _ = counts(cold, "startup-cold")
	if tasks != 0 {
		t.Fatal("cold proof created startup probe")
	}
	expired := prepare("expired", true)
	if _, err := pool.Exec(ctx, queryManagedMCPStartupExpired, pgx.StrictNamedArgs{"connection_ref": expired.connection.Ref, "test_ref": "tst_startup_expired_due"}); err != nil {
		t.Fatal("prepare expired cycle")
	}
	claim("startup-expired")
	current, err = service.GetRun(ctx, owner, expired.run.Ref)
	if err != nil || current.State != "FAILED" {
		t.Fatal("expired cycle became new startup budget")
	}
	tasks, due, revisions, leases, _, _, _ = counts(expired, "startup-expired")
	if tasks != 1 || due != 1 || revisions != 0 || leases != 0 {
		t.Fatal("expired cycle reset or leaked runtime authority")
	}
	for _, scenario := range []string{"latest-failure", "credential-drift", "partial-grants"} {
		f := prepare(scenario, true)
		switch scenario {
		case "latest-failure":
			if _, err := pool.Exec(ctx, queryManagedMCPStartupFailure, pgx.StrictNamedArgs{"connection_ref": f.connection.Ref, "test_ref": "tst_startup_latest_failed"}); err != nil {
				t.Fatal("prepare latest failed outcome")
			}
		case "credential-drift":
			if _, err := pool.Exec(ctx, queryManagedMCPStartupChangeCredential, pgx.StrictNamedArgs{"connection_ref": f.connection.Ref}); err != nil {
				t.Fatal("prepare changed credential pin")
			}
		case "partial-grants":
			execute(command.ChangeIntegrationGrant, "partial-grants-disable", &f.connection.Version, command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, CapabilityKey: "context7.docs.query", AgentRef: f.agent.Ref, ApprovalPolicy: "NONE", Enabled: false})
		}
		claim("startup-" + scenario)
		current, err = service.GetRun(ctx, owner, f.run.Ref)
		if err != nil || current.State != "FAILED" {
			t.Fatal("closed startup guard was hidden", scenario)
		}
		tasks, _, revisions, leases, _, _, _ = counts(f, "startup-"+scenario)
		if tasks != 0 || revisions != 0 || leases != 0 {
			t.Fatal("invalid startup created readiness or runtime authority", scenario)
		}
	}
	cancelled := prepare("cancelled", true)
	claim("startup-cancelled")
	current, err = service.GetRun(ctx, owner, cancelled.run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, "cancel-root", &current.Version, command.RunCommandInput{RunRef: current.Ref, Reason: "Synthetic fixture cleanup"})
	claim("startup-after-cancel")
	current, err = service.GetRun(ctx, owner, cancelled.run.Ref)
	if err != nil || current.State != "CANCELLED" {
		t.Fatal("startup maintenance revived cancelled root")
	}
	tasks, due, revisions, leases, _, _, _ = counts(cancelled, "startup-cancelled")
	if tasks != 1 || due != 1 || revisions != 0 || leases != 0 {
		t.Fatal("root cancel corrupted independent connection maintenance")
	}
	for _, capability := range []string{"context7.library.resolve", "context7.docs.query"} {
		cancelled.connection = *execute(command.ChangeIntegrationGrant, "cancelled-revoke-"+capability, &cancelled.connection.Version, command.IntegrationGrantInput{ConnectionRef: cancelled.connection.Ref, CapabilityKey: capability, AgentRef: cancelled.agent.Ref, ApprovalPolicy: "NONE", Enabled: false}).Connection
	}
	cancelled.connection = *execute(command.SetConnectionEnabled, "disable-cancelled-connection", &cancelled.connection.Version, command.ConnectionInput{Ref: cancelled.connection.Ref, Enabled: false}).Connection
	execute(command.DeleteConnection, "delete-cancelled-connection", &cancelled.connection.Version, command.ConnectionInput{Ref: cancelled.connection.Ref})
	tasks, due, revisions, leases, _, _, _ = counts(cancelled, "startup-cancelled")
	if tasks != 1 || due != 0 || revisions != 0 || leases != 0 {
		t.Fatal("connection delete retained startup probe authority")
	}
}
