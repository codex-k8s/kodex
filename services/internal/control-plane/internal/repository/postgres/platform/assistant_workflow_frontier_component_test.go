package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/assistant_workflow_frontier_pins.sql
var queryAssistantWorkflowFrontierPins string

func testAssistantWorkflowFrontier(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, agentRef string) {
	t.Helper()
	draft, _, updated := assistantWorkflowFrontierFixture(t)
	draft.CoordinatorAgentRef = agentRef
	for index := range draft.Steps {
		draft.Steps[index].AgentRef = agentRef
	}
	for _, raw := range updated {
		raw.(map[string]any)["agentRef"] = agentRef
	}
	execute := func(kind command.Kind, key string, payload any, version *int64) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "frontier-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return result
	}
	workflow := execute(command.CreateWorkflow, "create", command.WorkflowInput{ProjectRef: projectRef, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agentRef, Draft: &draft}, nil).Workflow
	workflow = execute(command.ValidateWorkflow, "validate-original", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, "publish-original", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	root := execute(command.LaunchRun, "original-root", command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Preserve original published graph", Input: map[string]any{"field-001": "Issue", "field-002": "Purpose", "field-003": "Repository", "field-004": "Constraints"}}, nil).Run
	defer func() {
		current, err := service.GetRun(ctx, owner, root.Ref)
		if err != nil {
			t.Error("read synthetic root for cleanup")
			return
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "frontier-cleanup", ExpectedVersion: &current.Version}, Payload: command.RunCommandInput{RunRef: root.Ref}}); err != nil {
			t.Error("cancel synthetic root")
		}
	}()
	oldRun, oldGraph, err := service.GetRunGraph(ctx, owner, root.Ref)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	readPins := func() (string, []byte, []byte) {
		t.Helper()
		var versionRef string
		var spec, input []byte
		if err := repository.pool.QueryRow(ctx, queryAssistantWorkflowFrontierPins, pgx.StrictNamedArgs{"organization_id": actorScope.organizationID, "run_ref": root.Ref}).Scan(&versionRef, &spec, &input); err != nil {
			t.Fatal("read exact historical workflow pins")
		}
		return versionRef, spec, input
	}
	oldVersionRef, oldSpec, oldInput := readPins()
	if oldVersionRef != workflow.Published.Ref {
		t.Fatal("root did not pin the original published workflow")
	}
	read := func(operation entity.AssistantPlanOperation, hydrate bool) (entity.AssistantPlanOperation, bool) {
		t.Helper()
		tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if hydrate {
			operation, err = repository.hydrateAssistantWorkflowOperation(ctx, tx, actorScope, projectRef, operation)
			if err != nil {
				t.Fatal(err)
			}
		}
		matches, err := repository.assistantWorkflowUpdateSnapshotMatches(ctx, tx, actorScope, projectRef, operation)
		if err != nil {
			t.Fatal(err)
		}
		return operation, matches
	}
	operation, matches := read(entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "frontier-update", Title: "Add architectural peers", Summary: "Preserve every original edge", Parameters: map[string]any{"workflowRef": workflow.Ref, "steps": updated}}, true)
	if !matches {
		t.Fatal("fresh normalized After rejected")
	}
	forged := operation
	forged.After = nil
	raw, _ := json.Marshal(operation.After)
	if json.Unmarshal(raw, &forged.After) != nil {
		t.Fatal("copy normalized After")
	}
	forged.After["draft"].(map[string]any)["Steps"].([]any)[0].(map[string]any)["DependsOn"] = []any{"step-039"}
	if _, matches := read(forged, false); matches {
		t.Fatal("forged After accepted")
	}
	operation, err = normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped.Principal, mapped.Mutation.IdempotencyKey = owner, "frontier-apply"
	applied, err := service.Execute(ctx, mapped)
	if err != nil || applied.Workflow == nil || applied.Workflow.Draft == nil {
		t.Fatalf("apply normalized workflow: %v", err)
	}
	assertAssistantWorkflowFrontier(t, draft, *applied.Workflow.Draft)
	if !assistantJSONEqual(operation.After["draft"], *applied.Workflow.Draft) {
		persisted := map[string]any{}
		raw, _ := json.Marshal(applied.Workflow.Draft)
		_ = json.Unmarshal(raw, &persisted)
		for key, expected := range operation.After["draft"].(map[string]any) {
			if !assistantJSONEqual(expected, persisted[key]) {
				t.Errorf("persisted draft field differs from normalized After: %s", key)
			}
		}
		t.Fatal("persisted draft differs from normalized After")
	}
	if _, matches := read(operation, false); matches {
		t.Fatal("stale snapshot remained eligible")
	}
	mapped.Mutation.IdempotencyKey = "frontier-stale-apply"
	if _, err := service.Execute(ctx, mapped); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("stale OCC did not reject")
	}
	workflow = execute(command.ValidateWorkflow, "validate-updated", command.WorkflowInput{Ref: workflow.Ref}, &applied.Workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, "publish-updated", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	assertAssistantWorkflowFrontier(t, draft, *workflow.Published)
	currentRun, currentGraph, err := service.GetRunGraph(ctx, owner, root.Ref)
	if err != nil {
		t.Fatalf("read historical graph: %v", err)
	}
	if !assistantJSONEqual(oldGraph, currentGraph) {
		t.Fatal("new publication mutated historical run graph")
	}
	if oldRun.Target.Type != currentRun.Target.Type || oldRun.Target.Ref != currentRun.Target.Ref {
		t.Fatal("new publication changed historical target locator")
	}
	currentVersionRef, currentSpec, currentInput := readPins()
	if currentVersionRef != oldVersionRef || string(currentSpec) != string(oldSpec) || string(currentInput) != string(oldInput) || workflow.Published.Ref == oldVersionRef {
		t.Fatal("new publication mutated historical workflow revision, DAG or original inputs")
	}
}
