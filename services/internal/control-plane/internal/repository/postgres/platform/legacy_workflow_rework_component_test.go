package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryLegacyWorkflowReworkProof reads immutable first-delivery bytes and bounded graph counts without preparing business rows.
// queryLegacyWorkflowReworkProof читает неизменяемый первый callback и счётчики графа без подготовки бизнес-состояний.
//
//go:embed testdata/sql/legacy_workflow_rework_proof.sql
var queryLegacyWorkflowReworkProof string

// legacyWorkflowReworkProof keeps the original terminal tuple and delivery body comparable across owner decisions and replay.
// legacyWorkflowReworkProof сохраняет исходный terminal tuple и callback для сравнения после решений и replay.
type legacyWorkflowReworkProof struct {
	root, child, node, snapshot                                          string
	active, callbacks, revisions, continues, events, runs, cancellations int64
}

// TestLegacyWorkflowReworkComponent executes a real self-delegated step twice and requires distinct immutable delivery after owner rework.
// TestLegacyWorkflowReworkComponent дважды исполняет настоящий self-step и требует новую immutable доставку после доработки владельцем.
func TestLegacyWorkflowReworkComponent(t *testing.T) {
	for _, decision := range []string{"APPROVE", "REJECT", "CANCEL", "CANCEL_RUNNING", "PARENT_BUSY", "LEASE_EXPIRY", "READ_EFFECT_FIRST", "READ_ORIGIN_FIRST", "READ_OVERSIZE", "READ_SOURCE_DELETED", "READ_REVOKED", "MULTIPLE_GATES"} {
		t.Run(decision, func(t *testing.T) { runLegacyWorkflowReworkScenario(t, decision) })
	}
}

// runLegacyWorkflowReworkScenario builds only native business states and retains exact first-result history across owner decisions and terminal cleanup.
// runLegacyWorkflowReworkScenario создаёт business states только native командами и сохраняет точную историю первого результата при решениях и terminal cleanup.
func runLegacyWorkflowReworkScenario(t *testing.T, decision string) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated legacy rework database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	execute := func(actor value.Principal, kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatal("native legacy rework command failed", string(kind), completionOutcomeClass(err))
		}
		return result
	}
	project := execute(owner, command.CreateProject, "project", nil, command.ProjectInput{Name: "Legacy immutable rework", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "legacy-rework-self", "Координатор собственного этапа")
	for _, capability := range []string{"platform.run.delegate", runtimecontract.ArtifactCapability} {
		agent = *execute(owner, command.ChangeAgentCapability, capability, &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: capability, Enabled: true}).Agent
	}
	readScenario := strings.HasPrefix(decision, "READ_")
	var connectionRef string
	var gateway value.Principal
	if readScenario {
		connection := execute(owner, command.CreateConnection, "read-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Native bounded rework read", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
		// Synthetic credential metadata enables the native connection owner path; no credential value or external network is used.
		// Synthetic credential metadata включает native connection owner path без credential value либо внешней сети.
		if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
			t.Fatal("prepare synthetic READ credential metadata")
		}
		fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal("read native READ connection")
		}
		execute(owner, command.ChangeIntegrationGrant, "read-grant", &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.issue.read", AgentRef: agent.Ref, Enabled: true, ApprovalPolicy: "NONE"})
		connectionRef = connection.Ref
		gateway = resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	}
	draft := entity.WorkflowVersion{Name: "Доработка собственного материала", CoordinatorAgentRef: agent.Ref, Concurrency: 1, TimeoutSeconds: 180, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "material", Position: 1, Name: "Материал", AgentRef: agent.Ref, Instructions: "Подготовить небольшой материал для владельца.", ExpectedResult: "Проверенный материал", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}}}}
	multipleGates := decision == "MULTIPLE_GATES"
	if multipleGates {
		draft.Concurrency = 2
		otherStep := draft.Steps[0]
		otherStep.Key = "independent"
		otherStep.Position = 2
		otherStep.Name = "Независимый материал"
		draft.Steps = append(draft.Steps, otherStep)
	}
	if readScenario {
		draft.Steps[0].RequiredCapabilityKeys = append(draft.Steps[0].RequiredCapabilityKeys, "github.issue.read")
	}
	workflow := execute(owner, command.CreateWorkflow, "create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, CoordinatorAgentRef: agent.Ref, Draft: &draft}).Workflow
	workflow = execute(owner, command.ValidateWorkflow, "validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = execute(owner, command.PublishWorkflow, "publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	root := execute(owner, command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Проверить исходный и доработанный материалы.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	claim := func(key string) []map[string]any {
		t.Helper()
		return execute(worker, command.ClaimExecution, key, nil, command.LeaseInput{WorkloadInstance: "legacy-owner-rework-fixture", Limit: 2}).RuntimeItems
	}
	complete := func(key string, lease map[string]any, summary, file string) command.Result {
		t.Helper()
		stage := "UNKNOWN"
		switch key {
		case "coordinator-wait":
			stage = "COORDINATOR"
		case "first-complete", "independent-complete":
			stage = "INITIAL_CHILD"
		case "second-complete", "read-ack":
			stage = "REWORK_CHILD"
		case "final-parent":
			stage = "PARENT_FINAL"
		default:
			t.Fatal("unsupported native completion stage")
		}
		t.Log("legacy_completion_stage", stage)
		payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: summary}
		if file != "" {
			if !contains(runtimeRevisionStringSlice(lease["capabilities"]), runtimecontract.ArtifactCapability) {
				t.Fatal("native artifact completion lacks its published runtime capability", stage)
			}
			payload.Artifacts = []command.CompletedArtifact{workflowCaptureProgressArtifact(file, []byte(summary+"\n"))}
		}
		return execute(worker, command.CompleteExecution, key, nil, payload)
	}
	initial := claim("initial")
	if len(initial) != 1 || stringMap(initial[0], "runRef") != root.Ref {
		t.Fatal("initial legacy coordinator was not claimed")
	}
	child := execute(worker, command.DelegateExecution, "delegate", nil, command.DelegateInput{LeaseRef: stringMap(initial[0], "leaseRef"), Fence: stringMap(initial[0], "fence"), Generation: runtimeRevisionMapInt64(initial[0], "generation"), TargetAgentRef: agent.Ref, WorkflowStepKey: "material", Task: "Создать первый материал."}).Run
	var independent *entity.Run
	if multipleGates {
		independent = execute(worker, command.DelegateExecution, "delegate-independent", nil, command.DelegateInput{LeaseRef: stringMap(initial[0], "leaseRef"), Fence: stringMap(initial[0], "fence"), Generation: runtimeRevisionMapInt64(initial[0], "generation"), TargetAgentRef: agent.Ref, WorkflowStepKey: "independent", Task: "Подготовить независимый материал."}).Run
	}
	if decision != "PARENT_BUSY" {
		complete("coordinator-wait", initial[0], "Ожидаю отдельный результат собственного этапа.", "")
	}
	firstClaims := claim("first-child")
	var independentClaim map[string]any
	if multipleGates && len(firstClaims) == 2 {
		for _, item := range firstClaims {
			if stringMap(item, "runRef") == independent.Ref {
				independentClaim = item
			}
		}
		for _, item := range firstClaims {
			if stringMap(item, "runRef") == child.Ref {
				firstClaims = []map[string]any{item}
				break
			}
		}
		if independentClaim == nil {
			t.Fatal("parallel independent step was not claimed")
		}
	}
	if len(firstClaims) != 1 || stringMap(firstClaims[0], "runRef") != child.Ref {
		t.Fatal("first delegated self-step was not claimed")
	}
	first := firstClaims[0]
	firstResult := complete("first-complete", first, "Первый материал требует уточнения.", "original.md")
	if len(firstResult.CreatedRefs) != 1 {
		t.Fatal("first native result artifact is missing")
	}
	callbackBase := int64(1)
	if multipleGates {
		complete("independent-complete", independentClaim, "Второй независимый материал принят отдельно.", "independent.md")
		callbackBase = 2
	}
	read := func() legacyWorkflowReworkProof {
		t.Helper()
		var p legacyWorkflowReworkProof
		if err := pool.QueryRow(ctx, queryLegacyWorkflowReworkProof, pgx.StrictNamedArgs{"root_ref": root.Ref, "child_ref": child.Ref, "node_ref": stringMap(first, "nodeRef")}).Scan(&p.root, &p.child, &p.node, &p.active, &p.callbacks, &p.snapshot, &p.revisions, &p.continues, &p.events, &p.runs, &p.cancellations); err != nil {
			t.Fatal("read legacy immutable rework proof", completionOutcomeClass(err))
		}
		return p
	}
	before := read()
	if before.child != "SUCCEEDED" || before.node != "SUCCEEDED" || before.callbacks != callbackBase || before.continues != map[bool]int64{true: 0, false: 1}[decision == "PARENT_BUSY"] || !strings.Contains(before.snapshot, firstResult.CreatedRefs[0]) {
		t.Fatal("native original callback did not create the expected queued parent boundary")
	}
	currentRoot, err := service.GetRun(ctx, owner, root.Ref)
	if err != nil || len(currentRoot.GateRefs) != int(callbackBase) {
		t.Fatal("first result gate is missing")
	}
	firstGateRef := currentRoot.GateRefs[0]
	var independentGate entity.OwnerGate
	if multipleGates {
		for _, ref := range currentRoot.GateRefs {
			candidate, err := service.GetOwnerGate(ctx, owner, ref)
			if err != nil {
				t.Fatal("read parallel native result gate")
			}
			if candidate.ContextSummary == "Первый материал требует уточнения." {
				firstGateRef = ref
			} else {
				independentGate = candidate
			}
		}
	}
	gate, err := service.GetOwnerGate(ctx, owner, firstGateRef)
	if err != nil || gate.State != "OPEN" {
		t.Fatal("first result gate is not OPEN")
	}
	feedback := "Уточнить итог; сохранить оба исходных материала."
	resolution := command.GateResolutionInput{GateRef: gate.Ref, Decision: "REQUEST_CHANGES", Comment: feedback}
	if decision == "PARENT_BUSY" {
		_, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-parent-busy", ExpectedVersion: &gate.Version}, Payload: resolution})
		if !errors.Is(err, errs.ErrLegacyReworkParentBusy) || read() != before {
			t.Fatal("live coordinator was cancelled or historical delivery replaced", completionOutcomeClass(err))
		}
		fresh, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal("read live coordinator root")
		}
		execute(owner, command.CancelRun, "cancel-busy", &fresh.Version, command.RunCommandInput{RunRef: root.Ref})
		if read().root != "CANCELLED" || read().active != 0 {
			t.Fatal("busy coordinator cleanup did not close full graph")
		}
		return
	}
	stale := gate.Version + 1
	_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-stale", ExpectedVersion: &stale}, Payload: resolution})
	if !errors.Is(err, errs.ErrVersionMismatch) || read() != before {
		t.Fatal("stale owner rework changed native graph", completionOutcomeClass(err))
	}
	other := execute(owner, command.CreateProject, "foreign-project", nil, command.ProjectInput{Name: "Foreign rework scope", Language: "en"}).Project
	foreign := owner
	foreign.ProjectRef = gateTestProjectID(t, ctx, repository, owner, other.Ref)
	_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: foreign, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-foreign", ExpectedVersion: &gate.Version}, Payload: resolution})
	if !errors.Is(err, errs.ErrNotFound) || read() != before {
		t.Fatal("foreign owner rework crossed project boundary", completionOutcomeClass(err))
	}
	execute(owner, command.ResolveOwnerGate, "request-changes", &gate.Version, resolution)
	after := read()
	execute(owner, command.ResolveOwnerGate, "request-changes", &gate.Version, resolution)
	if read() != after {
		t.Fatal("exact owner replay duplicated rework graph or changed immutable history")
	}
	reworked := claim("reworked-child")
	if len(reworked) != 1 || stringMap(reworked[0], "runRef") == child.Ref || stringMap(reworked[0], "runRef") == root.Ref || stringMap(reworked[0], "nodeRef") == stringMap(first, "nodeRef") || stringMap(reworked[0], "runtimeRevisionRef") == stringMap(first, "runtimeRevisionRef") || stringMap(reworked[0], "leaseRef") == stringMap(first, "leaseRef") || !strings.Contains(stringMap(reworked[0], "task"), feedback) {
		t.Fatal("owner rework did not claim one fresh child with exact saved feedback")
	}
	if after.child != "SUCCEEDED" || after.node != "SUCCEEDED" || after.snapshot != before.snapshot || after.revisions != before.revisions || after.callbacks != callbackBase || after.runs != before.runs+1 {
		t.Fatal("owner rework rewrote terminal origin instead of creating a distinct physical attempt")
	}
	_, graph, graphErr := service.GetRunGraphSnapshot(ctx, owner, root.Ref)
	if graphErr != nil || len(graph.Runs) != int(after.runs) {
		t.Fatal("owner graph rejected the fresh child retry lineage", completionOutcomeClass(graphErr))
	}
	if decision == "APPROVE" {
		frozen := read()
		for _, probe := range []string{"clear_scope", "rewrite_turn", "delete_node", "rewrite_retry", "delete_run"} {
			source, err := os.ReadFile("testdata/sql/legacy_rework_guard_" + probe + ".sql")
			if err != nil {
				t.Fatal("read owned lineage negative probe")
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin owned lineage negative probe")
			}
			_, mutationErr := tx.Exec(ctx, string(source), pgx.StrictNamedArgs{"node_ref": stringMap(reworked[0], "nodeRef")})
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			rollbackErr := tx.Rollback(cleanup)
			stop()
			if rollbackErr != nil {
				t.Fatal("rollback owned lineage negative probe")
			}
			var failure *pgconn.PgError
			if !errors.As(mutationErr, &failure) || failure.Code != "23514" || read() != frozen {
				t.Fatal("owned assignment mutation escaped its exact immutable guard", probe)
			}
		}
	}
	if decision == "LEASE_EXPIRY" {
		leaseBefore := reworked[0]
		timer := time.NewTimer(31 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("legacy rework expiry fixture deadline")
		case <-timer.C:
		}
		reworked = claim("expired-owned-claim")
		if len(reworked) != 1 || stringMap(reworked[0], "nodeRef") != stringMap(leaseBefore, "nodeRef") || stringMap(reworked[0], "leaseRef") == stringMap(leaseBefore, "leaseRef") || stringMap(reworked[0], "runtimeRevisionRef") == stringMap(leaseBefore, "runtimeRevisionRef") || runtimeRevisionMapInt64(reworked[0], "generation") <= runtimeRevisionMapInt64(leaseBefore, "generation") {
			t.Fatal("canonical owner rework expiry did not fence and reclaim same node")
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.RenewExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-expired-renew"}, Payload: command.LeaseInput{LeaseRef: stringMap(leaseBefore, "leaseRef"), Fence: stringMap(leaseBefore, "fence"), Generation: runtimeRevisionMapInt64(leaseBefore, "generation")}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("expired owner rework lease renewed", completionOutcomeClass(err))
		}
	}
	if decision == "CANCEL_RUNNING" {
		fresh, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal("read active owner rework root")
		}
		execute(owner, command.CancelRun, "cancel-running", &fresh.Version, command.RunCommandInput{RunRef: root.Ref})
		final := read()
		if final.root != "CANCELLED" || final.active != 0 || final.callbacks != callbackBase || final.snapshot != before.snapshot {
			t.Fatal("owner cancellation lost immutable first result or active cleanup")
		}
		if len(claim("cancelled-owned-claim")) != 0 || read() != final {
			t.Fatal("cancelled owner rework root resurrected")
		}
		return
	}
	var completeRead func()
	var readInvocation map[string]any
	if readScenario {
		invoke := func(key string) (map[string]any, error) {
			return service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": stringMap(reworked[0], "runRef"), "node_ref": stringMap(reworked[0], "nodeRef"), "connection_ref": connectionRef, "capability_key": "github.issue.read", "idempotency_key": key}, map[string]any{"issue_number": 1})
		}
		// allocation keeps one concurrent native result and its exact key; the owner serializes pending slots and retains the winner for replay.
		// allocation хранит один конкурентный native result и его точный key; owner сериализует pending slots и сохраняет winner для replay.
		type allocation struct {
			key    string
			result map[string]any
			err    error
		}
		started := make(chan struct{})
		outcomes := make(chan allocation, 2)
		var joined sync.WaitGroup
		for _, key := range []string{"legacy-rework-read-a", "legacy-rework-read-b"} {
			joined.Add(1)
			go func() {
				defer joined.Done()
				<-started
				result, err := invoke(key)
				outcomes <- allocation{key, result, err}
			}()
		}
		close(started)
		joined.Wait()
		close(outcomes)
		winner, denied := 0, 0
		winnerKey := ""
		for outcome := range outcomes {
			if outcome.err == nil {
				winner++
				readInvocation = outcome.result
				winnerKey = outcome.key
			} else if errors.Is(outcome.err, errs.ErrLegacyReworkScopeUnsupported) {
				denied++
			} else {
				t.Fatal("pending READ allocation failed outside closed slot contract", completionOutcomeClass(outcome.err))
			}
		}
		if winner != 1 || denied != 1 || stringMap(readInvocation, "state") != "READY" || stringMap(readInvocation, "gateRef") != "" {
			t.Fatal("canonical READ/NONE concurrent slot duplicated or manufactured a Gate")
		}
		duplicate, err := invoke(winnerKey)
		if err != nil || stringMap(duplicate, "invocationRef") != stringMap(readInvocation, "invocationRef") {
			t.Fatal("exact pending READ replay was not preserved", completionOutcomeClass(err))
		}
		if _, err = invoke("legacy-rework-read-second"); !errors.Is(err, errs.ErrLegacyReworkScopeUnsupported) {
			t.Fatal("second pending READ allocated another technical slot", completionOutcomeClass(err))
		}
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, "legacy-rework-read-gateway", 1)
		if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(readInvocation, "invocationRef") {
			t.Fatal("native READ worker did not claim exact invocation", completionOutcomeClass(err))
		}
		unexpected, err := service.ClaimIntegrationInvocations(ctx, gateway, "legacy-rework-read-no-second", 1)
		if err != nil || len(unexpected) != 0 {
			t.Fatal("concurrent READ slot left another committed invocation", completionOutcomeClass(err))
		}

		body := "Native READ evidence"
		if decision == "READ_OVERSIZE" {
			body = strings.Repeat("x", 34000)
		}
		raw, _ := json.Marshal(map[string]any{"number": 1, "title": "Native issue", "state": "open", "body": body})
		digest := sha256.Sum256(raw)
		payload := command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), Success: true, ResultSummary: string(raw), EffectKey: stringMap(work[0], "effectKey"), InputDigest: stringMap(work[0], "inputDigest"), ProviderEffectRef: "synthetic-github:issue:1", ResponseDigest: hex.EncodeToString(digest[:])}
		completeRead = func() {
			execute(gateway, command.CompleteIntegrationInvocation, "read-complete", nil, payload)
			execute(gateway, command.CompleteIntegrationInvocation, "read-complete", nil, payload)
		}
		if decision == "READ_EFFECT_FIRST" || decision == "READ_REVOKED" {
			completeRead()
		}
	}
	if decision == "READ_REVOKED" {
		freshConnection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
		if err != nil {
			t.Fatal("read current READ authority before revoke")
		}
		execute(owner, command.ChangeIntegrationGrant, "revoke-read", &freshConnection.Version, command.IntegrationGrantInput{ConnectionRef: connectionRef, CapabilityKey: "github.issue.read", AgentRef: agent.Ref, Enabled: false, ApprovalPolicy: "NONE"})
		frozen := read()
		_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-revoked-completion"}, Payload: command.CompleteExecutionInput{LeaseRef: stringMap(reworked[0], "leaseRef"), Fence: stringMap(reworked[0], "fence"), Generation: runtimeRevisionMapInt64(reworked[0], "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: "Не выдавать новый grant после отзыва READ."}})
		if !errors.Is(err, errs.ErrLegacyReworkDeliveryBlocked) || read() != frozen {
			t.Fatal("rejoin used historical READ grant as current authority", completionOutcomeClass(err))
		}
		fact, err := service.GetIntegrationInvocation(ctx, worker, stringMap(readInvocation, "invocationRef"))
		if err != nil || stringMap(fact, "state") != "SUCCEEDED" || stringMap(fact, "effectReceiptRef") == "" {
			t.Fatal("authority rejection rewrote prior READ fact")
		}
		fresh, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal("read authority-blocked rework root")
		}
		execute(owner, command.CancelRun, "cancel-revoked", &fresh.Version, command.RunCommandInput{RunRef: root.Ref})
		if read().root != "CANCELLED" || read().active != 0 || len(claim("revoked-no-claim")) != 0 {
			t.Fatal("revoked rejoin cleanup resurrected terminal authority")
		}
		return
	}
	second := complete("second-complete", reworked[0], "Исправленный материал готов к принятию.", "reworked.md")
	if len(second.CreatedRefs) != 1 || second.CreatedRefs[0] == firstResult.CreatedRefs[0] {
		t.Fatal("reworked result did not preserve distinct native artifact identity")
	}
	if readScenario {
		if decision != "READ_EFFECT_FIRST" {
			completeRead()
		}
		if decision == "READ_OVERSIZE" {
			settled, err := service.GetIntegrationInvocation(ctx, worker, stringMap(readInvocation, "invocationRef"))
			if err != nil || stringMap(settled, "state") != "SUCCEEDED" || stringMap(settled, "effectReceiptRef") == "" {
				t.Fatal("oversize READ presentation rolled back confirmed fact", completionOutcomeClass(err))
			}
			fresh, err := service.GetRun(ctx, owner, root.Ref)
			if err != nil || fresh.State != "FAILED" || fresh.SafeErrorCode != "LEGACY_REWORK_READ_LIMIT" {
				t.Fatal("oversize READ did not publish exact durable terminal cause", completionOutcomeClass(err))
			}
			final := read()
			if final.active != 0 || final.callbacks != callbackBase || final.snapshot != before.snapshot || len(claim("oversize-no-claim")) != 0 {
				t.Fatal("oversize READ renewed work or rewrote original result")
			}
			return
		}
		acknowledgements := claim("read-ack-claim")
		if len(acknowledgements) != 1 || stringMap(acknowledgements[0], "runRef") != stringMap(reworked[0], "runRef") || stringMap(acknowledgements[0], "nodeRef") == stringMap(reworked[0], "nodeRef") || !strings.Contains(stringMap(acknowledgements[0], "task"), stringMap(readInvocation, "invocationRef")) || !strings.Contains(stringMap(acknowledgements[0], "task"), "Native READ evidence") || strings.Contains(stringMap(acknowledgements[0], "task"), "The owner approved") {
			t.Fatal("READ completion failed exact immutable ACK without synthetic owner approval")
		}
		execution := runtimeFilesTestContext(t, acknowledgements[0], runtimecontract.FilePurposeRunResult)
		manifestReader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
		page, err := service.GetExecutionFileManifest(ctx, manifestReader, execution, query.Page{Size: 100})
		if err != nil {
			t.Fatal("read exact ACK file manifest", completionOutcomeClass(err))
		}
		found := false
		for _, file := range page.Items {
			if file.ArtifactRef == second.CreatedRefs[0] {
				found = true
				if file.Revision != 1 || file.Name != "reworked.md" {
					t.Fatal("ACK substituted early producer artifact")
				}
			}
		}
		if !found {
			t.Fatal("ACK file catalog omitted early owned producer")
		}
		bodyReader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
		download, err := service.ReadExecutionArtifact(ctx, bodyReader, execution.LeaseRef, execution.Fence, execution.Generation, second.CreatedRefs[0])
		if err != nil {
			t.Fatal("read immutable early rework artifact", completionOutcomeClass(err))
		}
		bytes, readErr := io.ReadAll(io.LimitReader(download.Reader, 1024))
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil || string(bytes) != "Исправленный материал готов к принятию.\n" {
			t.Fatal("ACK changed early producer bytes")
		}
		if decision == "READ_SOURCE_DELETED" {
			artifact, err := service.GetArtifact(ctx, owner, second.CreatedRefs[0])
			if err != nil {
				t.Fatal("read early artifact lifecycle")
			}
			impact, err := service.GetArtifactImpact(ctx, owner, artifact.Ref, "DELETE")
			if err != nil || !impact.Permitted {
				t.Fatal("native early artifact delete preview denied")
			}
			execute(owner, command.DeleteArtifact, "delete-early-result", &artifact.Version, command.ArtifactLifecycleInput{ArtifactRef: artifact.Ref, ImpactDigest: impact.Digest})
			frozen := read()
			_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-deleted-source-completion"}, Payload: command.CompleteExecutionInput{LeaseRef: execution.LeaseRef, Fence: execution.Fence, Generation: execution.Generation, Success: true, Usage: turnUsageFixture(), ResultSummary: "Завершение без подмены отсутствующего раннего результата."}})
			if !errors.Is(err, errs.ErrLegacyReworkDeliveryBlocked) || read() != frozen {
				t.Fatal("deleted early producer was silently omitted or partial callback committed", completionOutcomeClass(err))
			}
			fresh, err := service.GetRun(ctx, owner, root.Ref)
			if err != nil {
				t.Fatal("read blocked early-artifact root")
			}
			execute(owner, command.CancelRun, "cancel-missing-early", &fresh.Version, command.RunCommandInput{RunRef: root.Ref})
			if read().root != "CANCELLED" || read().active != 0 || read().snapshot != before.snapshot {
				t.Fatal("missing early result cleanup lost original immutable receipt")
			}
			return
		}
		complete("read-ack", acknowledgements[0], "Исправленный материал готов к принятию.", "")
	}
	if work := claim("before-new-approval"); len(work) != 0 {
		t.Fatal("coordinator consumed a stale or unapproved rework callback")
	}
	currentRoot, err = service.GetRun(ctx, owner, root.Ref)
	if err != nil || currentRoot.State != "WAITING_HUMAN" || len(currentRoot.GateRefs) != int(callbackBase) {
		t.Fatal("new physical attempt did not retain its own result gate")
	}
	acceptedGateRef := currentRoot.GateRefs[0]
	if multipleGates {
		for _, ref := range currentRoot.GateRefs {
			if ref != independentGate.Ref {
				acceptedGateRef = ref
			}
		}
	}
	acceptedGate, err := service.GetOwnerGate(ctx, owner, acceptedGateRef)
	if err != nil || acceptedGate.Ref == gate.Ref || acceptedGate.State != "OPEN" {
		t.Fatal("new physical attempt reused the first gate")
	}
	if decision == "REJECT" || decision == "CANCEL" {
		execute(owner, command.ResolveOwnerGate, "close-reworked", &acceptedGate.Version, command.GateResolutionInput{GateRef: acceptedGate.Ref, Decision: decision, Comment: "Закрыть без повторного исполнения."})
		final := read()
		want := map[string]string{"REJECT": "FAILED", "CANCEL": "CANCELLED"}[decision]
		if decision == "REJECT" && final.cancellations != 0 {
			t.Fatal("REJECT persisted a false cancellation event before FAILED")
		}
		if final.root != want || final.active != 0 || final.callbacks != callbackBase+1 || final.snapshot != before.snapshot || final.revisions != before.revisions {
			t.Fatal("owner terminal decision did not close exact rework graph", decision)
		}
		if len(claim("closed-owner-claim")) != 0 || read() != final {
			t.Fatal("terminal owner decision resumed legacy work")
		}
		return
	}
	acceptedFeedback := "Принято с условием сохранить обе версии материала."
	execute(owner, command.ResolveOwnerGate, "approve-reworked", &acceptedGate.Version, command.GateResolutionInput{GateRef: acceptedGate.Ref, Decision: "APPROVE", Comment: acceptedFeedback})
	if multipleGates {
		if len(claim("before-independent-approval")) != 0 {
			t.Fatal("one result approval bypassed another OPEN Gate")
		}
		stillOpen, err := service.GetOwnerGate(ctx, owner, independentGate.Ref)
		if err != nil || stillOpen.State != "OPEN" || stillOpen.Version != independentGate.Version {
			t.Fatal("other result gate changed after unrelated approval")
		}
		execute(owner, command.ResolveOwnerGate, "approve-independent", &independentGate.Version, command.GateResolutionInput{GateRef: independentGate.Ref, Decision: "APPROVE", Comment: "Принят независимый второй результат."})
	}
	callback := claim("accepted-parent")
	if len(callback) != 1 || stringMap(callback[0], "runRef") != root.Ref || !strings.Contains(stringMap(callback[0], "task"), second.CreatedRefs[0]) || !strings.Contains(stringMap(callback[0], "task"), "Исправленный материал") || !strings.Contains(stringMap(callback[0], "task"), feedback) || !strings.Contains(stringMap(callback[0], "task"), acceptedFeedback) || !strings.Contains(stringMap(callback[0], "task"), acceptedGate.Ref) {
		t.Fatal("fresh coordinator did not receive the exact newly accepted callback")
	}
	complete("final-parent", callback[0], "Принят исправленный материал с сохранённой историей.", "")
	final := read()
	if final.root != "SUCCEEDED" || final.active != 0 || final.callbacks != callbackBase+1 || final.snapshot != before.snapshot || final.revisions != before.revisions {
		t.Fatal("legacy rework did not finish with two immutable exact callback receipts")
	}
	if work := claim("terminal-claim"); len(work) != 0 {
		t.Fatal("terminal legacy root admitted another execution")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.RenewExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "legacy-rework-old-renew"}, Payload: command.LeaseInput{LeaseRef: stringMap(first, "leaseRef"), Fence: stringMap(first, "fence"), Generation: runtimeRevisionMapInt64(first, "generation")}})
	if !errors.Is(err, errs.ErrForbidden) || read() != final {
		t.Fatal("terminal old lease was renewed or changed accepted history", completionOutcomeClass(err))
	}
}
