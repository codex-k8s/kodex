package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/runtime_claim_advance_account.sql
var queryRuntimeClaimFixtureAdvanceAccount string

//go:embed testdata/sql/runtime_claim_failed_graph_counts.sql
var queryRuntimeClaimFixtureGraphCounts string

//go:embed testdata/sql/runtime_claim_add_siblings.sql
var queryRuntimeClaimFixtureAddSiblings string

func testRuntimeCandidateIsolation(t *testing.T, ctx context.Context, repository *Repository) {
	for _, allStale := range []bool{false, true} {
		name := "mixed"
		if allStale {
			name = "all-stale"
		}
		t.Run(name, func(t *testing.T) { testRuntimeCandidateIsolationBatch(t, ctx, repository, allStale, name) })
	}
}

//go:embed testdata/sql/runtime_claim_receipt_counts.sql
var queryRuntimeClaimReceiptCounts string

//go:embed testdata/sql/runtime_claim_audit_unavailable.sql
var queryRuntimeClaimAuditUnavailable string

//go:embed testdata/sql/runtime_claim_expire_terminal_lease.sql
var queryRuntimeClaimExpireTerminalLease string

func testRuntimeCandidateIsolationBatch(t *testing.T, ctx context.Context, repository *Repository, allStale bool, suffix string) {
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "claim-isolation-project-" + suffix}, Payload: command.ProjectInput{Name: "Claim isolation " + suffix, Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create claim isolation project: %v", err)
	}
	badAgent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "claim-isolation-stale-agent-"+suffix, "Stale catalog candidate")
	launch := func(agent entity.Agent, key string) entity.Run {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + suffix}, Payload: command.LaunchRunInput{
				ProjectRef: project.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Verify independent claim progress."}})
		if err != nil || result.Run == nil {
			t.Fatalf("launch isolation candidate: %v", err)
		}
		return *result.Run
	}
	badRun := launch(badAgent, "claim-isolation-stale-run")
	if _, err := repository.pool.Exec(ctx, queryRuntimeClaimFixtureAddSiblings, badRun.Ref); err != nil {
		t.Fatal(err)
	}
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, badAgent.Ref)
	if err != nil || len(configuration.Configuration.ProviderPolicy.AccountCandidates) == 0 {
		t.Fatalf("read candidate account: %v", err)
	}
	accountRef := configuration.Configuration.ProviderPolicy.AccountCandidates[0].AccountRef
	if _, err := repository.pool.Exec(ctx, queryRuntimeClaimFixtureAdvanceAccount, accountRef); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository, func(observation *platformrepo.ProviderModelCatalogObservation) {
		if observation.AccountRef == accountRef {
			// Меняем immutable catalog pin, сохраняя совместимость общего gpt-5 с другими fixtures.
			observation.Models = append(observation.Models, platformrepo.ProviderModelCatalogRecord{ID: "claim-fresh-model"})
		}
	})
	defer func() {
		cleanup := context.WithoutCancel(ctx)
		if _, err := repository.pool.Exec(cleanup, queryRuntimeClaimFixtureAdvanceAccount, accountRef); err != nil {
			t.Error(err)
			return
		}
		seedObservedCatalogFixture(t, cleanup, repository)
	}()
	healthyAgent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "claim-isolation-healthy-agent-"+suffix, "Healthy catalog candidate")
	var healthyRun entity.Run
	if !allStale {
		healthyRun = launch(healthyAgent, "claim-isolation-healthy-run")
	}
	claim := command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "claim-isolation-batch-" + suffix}, Payload: command.LeaseInput{WorkloadInstance: "claim-isolation-worker", Limit: 32}}
	if allStale {
		// Bootstrap не parallel: временный отказ actual SQL audit проверяет rollback
		// уже изменённого графа, а не одну классификацию ошибки helper.
		func() {
			original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
			queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
			defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
			if _, err := service.Execute(ctx, claim); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatalf("audit infrastructure failure was suppressed: %v", err)
			}
		}()
		readback, err := service.GetRun(ctx, owner, badRun.Ref)
		if err != nil || readback.State != badRun.State {
			t.Fatalf("failed transaction committed candidate graph: %s %v", readback.State, err)
		}
		var auditCount, receiptCount, eventCount int
		if err := repository.pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, badRun.Ref, claim.Mutation.IdempotencyKey).Scan(&auditCount, &receiptCount, &eventCount); err != nil || receiptCount != 0 {
			t.Fatalf("failed transaction persisted receipt: %v", err)
		}
	}
	claimed, err := service.Execute(ctx, claim)
	if err != nil {
		t.Fatalf("stale candidate rolled back claim batch: %v", err)
	}
	var audits, receipts, events int
	if err := repository.pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, badRun.Ref, claim.Mutation.IdempotencyKey).Scan(&audits, &receipts, &events); err != nil || audits < 1 || receipts != 1 || events < 1 {
		t.Fatalf("changed claim lacks atomic audit/receipt/events: %d/%d/%d %v", audits, receipts, events, err)
	}
	if allStale {
		if len(claimed.RuntimeItems) != 0 {
			t.Fatal("all-stale batch returned a claim")
		}
		healthyRun = launch(healthyAgent, "claim-isolation-later-healthy-run")
		replay, err := service.Execute(ctx, claim)
		if err != nil || len(replay.RuntimeItems) != 0 {
			t.Fatalf("lost-ACK replay claimed a new healthy run: %v", err)
		}
		var afterAudits, afterReceipts, afterEvents int
		if err := repository.pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, badRun.Ref, claim.Mutation.IdempotencyKey).Scan(&afterAudits, &afterReceipts, &afterEvents); err != nil || afterAudits != audits || afterReceipts != receipts || afterEvents != events {
			t.Fatalf("claim replay duplicated owner facts: %v", err)
		}
		claim.Mutation.IdempotencyKey += "-fresh"
		claimed, err = service.Execute(ctx, claim)
		if err != nil {
			t.Fatal(err)
		}
	}
	var healthyLease map[string]any
	for _, item := range claimed.RuntimeItems {
		if stringMap(item, "runRef") == badRun.Ref {
			t.Fatal("stale candidate received a runtime lease")
		}
		if stringMap(item, "runRef") == healthyRun.Ref {
			healthyLease = item
		}
	}
	if healthyLease == nil {
		t.Fatal("healthy candidate did not progress in the same batch")
	}
	testDeletingAccountRetainsExactActiveProjection(t, ctx, repository, stringMap(healthyLease, "leaseRef"))
	failed, graph, err := service.GetRunGraph(ctx, owner, badRun.Ref)
	if err != nil || failed.State != "FAILED" {
		t.Fatalf("stale candidate lacks terminal owner readback: %v", err)
	}
	for _, node := range graph.Nodes {
		if node.State == "PLANNED" || node.State == "QUEUED" || node.State == "RUNNING" || node.State == "WAITING" {
			t.Fatal("failed candidate retained active graph node")
		}
	}
	var openLeases, openTurns, revisions int
	err = repository.pool.QueryRow(ctx, queryRuntimeClaimFixtureGraphCounts, badRun.Ref).Scan(&openLeases, &openTurns, &revisions)
	if err != nil || openLeases != 0 || openTurns != 0 || revisions != 0 {
		t.Fatalf("candidate savepoint retained partial runtime state: %v", err)
	}
	completeClaimedExecution(t, ctx, service, worker, healthyLease, "claim-isolation-healthy-complete-"+suffix, false)
	if allStale {
		claim.Mutation.IdempotencyKey += "-idle"
		idle, err := service.Execute(ctx, claim)
		if err != nil || len(idle.RuntimeItems) != 0 {
			t.Fatalf("idle poll: %v", err)
		}
		if err := repository.pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, badRun.Ref, claim.Mutation.IdempotencyKey).Scan(&audits, &receipts, &events); err != nil || receipts != 0 {
			t.Fatalf("idle poll persisted receipt: %v", err)
		}
		if _, err := repository.pool.Exec(ctx, queryRuntimeClaimExpireTerminalLease, stringMap(healthyLease, "leaseRef")); err != nil {
			t.Fatal(err)
		}
		claim.Mutation.IdempotencyKey += "-expiry"
		expiry, err := service.Execute(ctx, claim)
		if err != nil || len(expiry.RuntimeItems) != 0 {
			t.Fatalf("expiry-only claim: %v", err)
		}
		if err := repository.pool.QueryRow(ctx, queryRuntimeClaimReceiptCounts, healthyRun.Ref, claim.Mutation.IdempotencyKey).Scan(&audits, &receipts, &events); err != nil || receipts != 1 || audits < 1 {
			t.Fatalf("expiry-only transition lost audit/receipt: %v", err)
		}
	}
}
