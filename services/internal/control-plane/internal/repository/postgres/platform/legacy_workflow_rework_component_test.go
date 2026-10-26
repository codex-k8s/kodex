package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
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
	root, child, node, snapshot                           string
	active, callbacks, revisions, continues, events, runs int64
}

// TestLegacyWorkflowReworkComponent executes a real self-delegated step twice and requires distinct immutable delivery after owner rework.
// TestLegacyWorkflowReworkComponent дважды исполняет настоящий self-step и требует новую immutable доставку после доработки владельцем.
func TestLegacyWorkflowReworkComponent(t *testing.T) {
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
	draft := entity.WorkflowVersion{Name: "Доработка собственного материала", CoordinatorAgentRef: agent.Ref, Concurrency: 1, TimeoutSeconds: 180, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "material", Position: 1, Name: "Материал", AgentRef: agent.Ref, Instructions: "Подготовить небольшой материал для владельца.", ExpectedResult: "Проверенный материал", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}}}}
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
		case "first-complete":
			stage = "INITIAL_CHILD"
		case "second-complete":
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
	complete("coordinator-wait", initial[0], "Ожидаю отдельный результат собственного этапа.", "")
	firstClaims := claim("first-child")
	if len(firstClaims) != 1 || stringMap(firstClaims[0], "runRef") != child.Ref {
		t.Fatal("first delegated self-step was not claimed")
	}
	first := firstClaims[0]
	firstResult := complete("first-complete", first, "Первый материал требует уточнения.", "original.md")
	if len(firstResult.CreatedRefs) != 1 {
		t.Fatal("first native result artifact is missing")
	}
	read := func() legacyWorkflowReworkProof {
		t.Helper()
		var p legacyWorkflowReworkProof
		if err := pool.QueryRow(ctx, queryLegacyWorkflowReworkProof, pgx.StrictNamedArgs{"root_ref": root.Ref, "child_ref": child.Ref, "node_ref": stringMap(first, "nodeRef")}).Scan(&p.root, &p.child, &p.node, &p.active, &p.callbacks, &p.snapshot, &p.revisions, &p.continues, &p.events, &p.runs); err != nil {
			t.Fatal("read legacy immutable rework proof", completionOutcomeClass(err))
		}
		return p
	}
	before := read()
	if before.child != "SUCCEEDED" || before.node != "SUCCEEDED" || before.callbacks != 1 || before.continues != 1 || !strings.Contains(before.snapshot, firstResult.CreatedRefs[0]) {
		t.Fatal("native original callback did not create the expected queued parent boundary")
	}
	currentRoot, err := service.GetRun(ctx, owner, root.Ref)
	if err != nil || len(currentRoot.GateRefs) != 1 {
		t.Fatal("first result gate is missing")
	}
	gate, err := service.GetOwnerGate(ctx, owner, currentRoot.GateRefs[0])
	if err != nil || gate.State != "OPEN" {
		t.Fatal("first result gate is not OPEN")
	}
	feedback := "Уточнить итог; сохранить оба исходных материала."
	resolution := command.GateResolutionInput{GateRef: gate.Ref, Decision: "REQUEST_CHANGES", Comment: feedback}
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
	if after.child != "SUCCEEDED" || after.node != "SUCCEEDED" || after.snapshot != before.snapshot || after.revisions != before.revisions || after.callbacks != 1 || after.runs != before.runs+1 {
		t.Fatal("owner rework rewrote terminal origin instead of creating a distinct physical attempt")
	}
	second := complete("second-complete", reworked[0], "Исправленный материал готов к принятию.", "reworked.md")
	if len(second.CreatedRefs) != 1 || second.CreatedRefs[0] == firstResult.CreatedRefs[0] {
		t.Fatal("reworked result did not preserve distinct native artifact identity")
	}
	if work := claim("before-new-approval"); len(work) != 0 {
		t.Fatal("coordinator consumed a stale or unapproved rework callback")
	}
	currentRoot, err = service.GetRun(ctx, owner, root.Ref)
	if err != nil || currentRoot.State != "WAITING_HUMAN" || len(currentRoot.GateRefs) != 1 {
		t.Fatal("new physical attempt did not retain its own result gate")
	}
	acceptedGate, err := service.GetOwnerGate(ctx, owner, currentRoot.GateRefs[0])
	if err != nil || acceptedGate.Ref == gate.Ref || acceptedGate.State != "OPEN" {
		t.Fatal("new physical attempt reused the first gate")
	}
	execute(owner, command.ResolveOwnerGate, "approve-reworked", &acceptedGate.Version, command.GateResolutionInput{GateRef: acceptedGate.Ref, Decision: "APPROVE"})
	callback := claim("accepted-parent")
	if len(callback) != 1 || stringMap(callback[0], "runRef") != root.Ref || !strings.Contains(stringMap(callback[0], "task"), second.CreatedRefs[0]) || !strings.Contains(stringMap(callback[0], "task"), "Исправленный материал") {
		t.Fatal("fresh coordinator did not receive the exact newly accepted callback")
	}
	complete("final-parent", callback[0], "Принят исправленный материал с сохранённой историей.", "")
	final := read()
	if final.root != "SUCCEEDED" || final.active != 0 || final.callbacks != 2 || final.snapshot != before.snapshot || final.revisions != before.revisions {
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
