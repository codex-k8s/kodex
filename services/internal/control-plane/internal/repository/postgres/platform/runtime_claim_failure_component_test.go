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

// queryRuntimeClaimFixtureAdvanceAccount embeds fixture account-version advancement by ref to
// stale the captured catalog dependency tuple. The component arrangement is disposable and does
// not rotate any external provider credential.
// queryRuntimeClaimFixtureAdvanceAccount встраивает fixture account-version advancement по ref для
// устаревания captured catalog dependency tuple. Component arrangement disposable и не ротирует
// внешний provider credential.
//
//go:embed testdata/sql/runtime_claim_advance_account.sql
var queryRuntimeClaimFixtureAdvanceAccount string

// queryRuntimeClaimFixtureGraphCounts embeds exact-run counts of open leases/turns and runtime
// revisions after candidate failure. Zero counts verify owner rollback/terminal cleanup, not Pod
// termination or external provider cancellation.
// queryRuntimeClaimFixtureGraphCounts встраивает exact-run counts открытых leases/turns и runtime
// revisions после candidate failure. Нули проверяют owner rollback/terminal cleanup, а не Pod
// termination и external provider cancellation.
//
//go:embed testdata/sql/runtime_claim_failed_graph_counts.sql
var queryRuntimeClaimFixtureGraphCounts string

// queryRuntimeClaimFixtureAddSiblings embeds synthetic PLANNED/RUNNING sibling nodes under the
// fixture root so failure must close the whole graph rather than only the selected node. It is
// test arrangement SQL without request-derived authority.
// queryRuntimeClaimFixtureAddSiblings встраивает synthetic PLANNED/RUNNING sibling nodes под
// fixture root, чтобы failure закрывал весь граф, а не только выбранный node. Это test arrangement
// SQL без authority из запроса.
//
//go:embed testdata/sql/runtime_claim_add_siblings.sql
var queryRuntimeClaimFixtureAddSiblings string

// testRuntimeCandidateIsolation runs mixed healthy/stale and all-stale claim scenarios
// sequentially against the caller fixture. It delegates complete assertions to the batch helper
// and must not be parallelized with its temporary global audit-query substitution.
// testRuntimeCandidateIsolation последовательно запускает mixed healthy/stale и all-stale claim
// scenarios на caller fixture. Передаёт полные assertions batch helper; не должен выполняться
// параллельно с его временной global audit-query substitution.
func testRuntimeCandidateIsolation(t *testing.T, ctx context.Context, repository *Repository) {
	for _, allStale := range []bool{false, true} {
		name := "mixed"
		if allStale {
			name = "all-stale"
		}
		t.Run(name, func(t *testing.T) { testRuntimeCandidateIsolationBatch(t, ctx, repository, allStale, name) })
	}
}

// queryRuntimeClaimReceiptCounts embeds audit, idempotency receipt and root-event counts for the
// exact fixture run/claim key. It supports atomic owner-fact and lost-ACK replay assertions
// without itself changing those facts.
// queryRuntimeClaimReceiptCounts встраивает audit, idempotency receipt и root-event counts точных
// fixture run/claim key. Поддерживает atomic owner-fact и lost-ACK replay assertions, сам не меняя
// эти факты.
//
//go:embed testdata/sql/runtime_claim_receipt_counts.sql
var queryRuntimeClaimReceiptCounts string

// queryRuntimeClaimAuditUnavailable embeds a deliberate division-by-zero failure substituted for
// the audit write only inside a sequential disposable test. It exercises rollback after real graph
// changes rather than merely classifying a fabricated Go error.
// queryRuntimeClaimAuditUnavailable встраивает deliberate division-by-zero failure, подставляемый
// вместо audit write лишь внутри sequential disposable test. Проверяет rollback после настоящих
// graph changes, а не только классификацию вымышленной Go error.
//
//go:embed testdata/sql/runtime_claim_audit_unavailable.sql
var queryRuntimeClaimAuditUnavailable string

// queryRuntimeClaimExpireTerminalLease embeds revival of a fixture terminal lease into an already
// expired CLAIMED shape to exercise expiry-only maintenance accounting. This is a deliberately
// invalid arrangement, not a supported runtime lifecycle operation.
// queryRuntimeClaimExpireTerminalLease встраивает перевод fixture terminal lease в уже expired
// CLAIMED shape для проверки expiry-only maintenance accounting. Это намеренно неверный
// arrangement, а не поддержанная runtime lifecycle operation.
//
//go:embed testdata/sql/runtime_claim_expire_terminal_lease.sql
var queryRuntimeClaimExpireTerminalLease string

// testRuntimeCandidateIsolationBatch makes one captured dependency stale and checks its complete
// graph fails without rolling back a healthy sibling claim. The all-stale case injects/restores
// real audit SQL failure, verifies rollback and atomic receipt/events, lost-ACK replay, idle
// no-receipt and expiry-only accounting. It also closes active projection proofs. Cleanup reseeds
// fixture catalog through WithoutCancel; the sequential PG test does not execute a provider or
// Pod.
// testRuntimeCandidateIsolationBatch делает captured dependency устаревшей и проверяет failure
// полного графа без rollback healthy sibling claim. All-stale case подставляет и восстанавливает
// real audit SQL failure, проверяет rollback и atomic receipt/events, lost-ACK replay, idle
// no-receipt и expiry-only accounting. Также закрывает active projection proofs. Cleanup заново
// заполняет fixture catalog через WithoutCancel; sequential PG test не исполняет provider и Pod.
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
			// Меняем возможности выбранной модели: добавление посторонней модели
			// намеренно не отзывает server-owned capability pin gpt-5.
			observation.Models[0].DefaultReasoningEffort = "low"
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
	verifyTrustedProjectionTerminal := testTrustedRuntimeProjection(t, ctx, repository, healthyLease)
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
	verifyTrustedProjectionTerminal()
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
