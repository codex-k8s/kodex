package platform

import (
	"context"
	_ "embed"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkflowGateDecisionsProof observes only synthetic graph/receipt cardinalities and gate
// flags; it never rewrites immutable revisions or supplies execution authority.
// queryWorkflowGateDecisionsProof читает только synthetic graph/receipt cardinalities и флаги
// Gate, не переписывая immutable revisions и не предоставляя execution authority.
//
//go:embed testdata/sql/workflow_gate_decisions_proof.sql
var queryWorkflowGateDecisionsProof string

// workflowGateDecisionsProof compares owner-visible synthetic transitions without logging
// locators, payload, credentials or raw database errors.
// workflowGateDecisionsProof сравнивает synthetic owner transitions без вывода locators,
// payload, credentials либо сырых ошибок БД.
type workflowGateDecisionsProof struct {
	coordinatorGated                                                   bool
	stepGated, gates, nodes, turns, events, audit, receipts, callbacks int64
}

// TestWorkflowGateDecisionsComponent exercises actual claims and one completion per lease for
// pinned step decisions, self-step gates, incomplete failure, owner replay and terminal cancel.
// It does not invoke integrations, duplicate completion or the separately stopped PG paths.
// TestWorkflowGateDecisionsComponent проверяет actual claims и одно completion на lease для
// pinned step decisions, self-step gates, incomplete failure, owner replay и terminal cancel.
// Интеграции, duplicate completion и отдельно остановленные PG paths не вызываются.
func TestWorkflowGateDecisionsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated workflow gate database")
	}
	defer pool.Close()
	r, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	mutate := func(actor value.Principal, kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "gate-policy-" + key, ExpectedVersion: version}, Payload: payload})
	}
	execute := func(t *testing.T, actor value.Principal, kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := mutate(actor, kind, key, version, payload)
		if err != nil {
			t.Fatalf("workflow gate command %s failed: not_found=%t forbidden=%t invalid=%t conflict=%t unavailable=%t", kind, errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrForbidden), errors.Is(err, errs.ErrInvalid), errors.Is(err, errs.ErrConflict), errors.Is(err, errs.ErrUnavailable))
		}
		return result
	}
	project := execute(t, owner, command.CreateProject, "project", nil, command.ProjectInput{Name: "Workflow gate policies", Language: "ru"}).Project
	other := execute(t, owner, command.CreateProject, "other-project", nil, command.ProjectInput{Name: "Other gate scope", Language: "ru"}).Project
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "gate-policy-coordinator", "Координатор")
	coordinator = *execute(t, owner, command.ChangeAgentCapability, "delegate-cap", &coordinator.Version, command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "gate-policy-specialist", "Исполнитель")
	foreign := owner
	foreign.ProjectRef = gateTestProjectID(t, ctx, r, owner, other.Ref)
	subset := []string{"APPROVE", "REJECT", "CANCEL"}
	full := []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}
	for _, scenario := range []struct {
		name, decision                          string
		self, incomplete, republish, cancelRoot bool
	}{
		{name: "subset-approve", decision: "APPROVE", republish: true},
		{name: "subset-reject", decision: "REJECT"},
		{name: "subset-cancel", decision: "CANCEL"},
		{name: "full-request-changes", decision: "REQUEST_CHANGES"},
		{name: "self-step", decision: "APPROVE", self: true},
		{name: "self-incomplete", self: true, incomplete: true},
		{name: "root-cancel", cancelRoot: true},
	} {
		if !t.Run(scenario.name, func(t *testing.T) {
			key := scenario.name + "-"
			agent := specialist
			if scenario.self {
				agent = coordinator
			}
			decisions := subset
			if scenario.decision == "REQUEST_CHANGES" {
				decisions = full
			}
			draft := entity.WorkflowVersion{Name: "Точный Gate " + scenario.name, CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "review", Position: 1, Name: "Проверить результат", AgentRef: agent.Ref, Instructions: "Вернуть проверенный результат владельцу.", ExpectedResult: "Проверенный результат", TimeoutSeconds: 600, HumanGateAfter: true, GateDecisions: append([]string(nil), decisions...)}}}
			workflow := execute(t, owner, command.CreateWorkflow, key+"create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}).Workflow
			workflow = execute(t, owner, command.ValidateWorkflow, key+"validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
			workflow = execute(t, owner, command.PublishWorkflow, key+"publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
			run := execute(t, owner, command.LaunchRun, key+"launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Выполнить точный опубликованный этап.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
			t.Cleanup(func() {
				current, readErr := service.GetRun(ctx, owner, run.Ref)
				if readErr == nil && !contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, current.State) {
					if _, err := mutate(owner, command.CancelRun, key+"cleanup", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic fixture cleanup"}); err != nil {
						t.Error("close synthetic workflow root failed")
					}
				}
			})
			proof := func(receiptKey string) workflowGateDecisionsProof {
				t.Helper()
				var value workflowGateDecisionsProof
				if err := pool.QueryRow(ctx, queryWorkflowGateDecisionsProof, pgx.StrictNamedArgs{"root_ref": run.Ref, "receipt_key": "gate-policy-" + receiptKey}).Scan(&value.coordinatorGated, &value.stepGated, &value.gates, &value.nodes, &value.turns, &value.events, &value.audit, &value.receipts, &value.callbacks); err != nil {
					t.Fatal("read synthetic gate proof failed")
				}
				return value
			}
			initial := proof(key + "none")
			if initial.coordinatorGated || initial.stepGated != 1 || initial.gates != 0 {
				t.Fatalf("published gate flags changed: coordinator=%t step_count=%d gate_count=%d", initial.coordinatorGated, initial.stepGated, initial.gates)
			}
			claim := func(name, expectedRun string) map[string]any {
				t.Helper()
				items := execute(t, worker, command.ClaimExecution, key+name, nil, command.LeaseInput{WorkloadInstance: "workflow-gate-policy-fixture", Limit: 1}).RuntimeItems
				if len(items) != 1 || stringMap(items[0], "runRef") != expectedRun {
					t.Fatalf("expected exact workflow claim: count=%d", len(items))
				}
				return items[0]
			}
			complete := func(name string, lease map[string]any) command.Result {
				return execute(t, worker, command.CompleteExecution, key+name, nil, command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Verified synthetic result", Usage: turnUsageFixture()})
			}
			current := claim("coordinator", run.Ref)
			if scenario.incomplete {
				complete("incomplete", current)
				read, err := service.GetRun(ctx, owner, run.Ref)
				if err != nil || read.State != "FAILED" || read.SafeErrorCode != "RUNTIME_WORKFLOW_INCOMPLETE" || proof(key+"none").gates != 0 {
					t.Fatalf("unfinished self-step did not fail closed: error_nil=%t failed=%t incomplete=%t", err == nil, read.State == "FAILED", read.SafeErrorCode == "RUNTIME_WORKFLOW_INCOMPLETE")
				}
				_, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
				if err != nil {
					t.Fatal("read incomplete self-step graph failed")
				}
				closed := false
				for _, node := range graph.Nodes {
					if node.MaterializationState == "PLANNED" {
						closed = node.State == "CANCELLED"
					}
				}
				if !closed {
					t.Fatal("incomplete self-step left its mandatory planned node open")
				}
				return
			}
			child := execute(t, worker, command.DelegateExecution, key+"delegate", nil, command.DelegateInput{LeaseRef: stringMap(current, "leaseRef"), Fence: stringMap(current, "fence"), Generation: runtimeRevisionMapInt64(current, "generation"), TargetAgentRef: agent.Ref, WorkflowStepKey: "review", Task: "Выполнить проверку по закреплённой версии."}).Run
			complete("coordinator-done", current)
			childLease := claim("child", child.Ref)
			// Проба использует настоящий lease, затем портит только pins READ-запроса, не execution authority.
			func() {
				tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
				if err != nil {
					t.Fatal("begin exact gate policy proof")
				}
				defer func() { _ = tx.Rollback(ctx) }()
				resolved, err := r.ResolvePrincipal(ctx, worker)
				if err != nil {
					t.Fatal("resolve exact worker proof")
				}
				currentScope, err := r.resolveScope(ctx, resolved)
				if err != nil {
					t.Fatal("resolve exact worker scope")
				}
				verified, err := r.lease(ctx, tx, currentScope, command.LeaseInput{LeaseRef: stringMap(childLease, "leaseRef"), Fence: stringMap(childLease, "fence"), Generation: runtimeRevisionMapInt64(childLease, "generation")}, false)
				if err != nil {
					t.Fatal("verify actual child lease")
				}
				actual, err := r.executionStepGateDecisions(ctx, tx, currentScope, verified)
				if err != nil || !reflect.DeepEqual(actual, decisions) {
					t.Fatal("actual child revision cannot resolve its exact gate policy")
				}
				for _, field := range []string{"generation", "nodeID", "runtimeRevisionID", "runID", "rootRunID"} {
					mismatched := make(map[string]any, len(verified))
					for name, value := range verified {
						mismatched[name] = value
					}
					if field == "generation" {
						mismatched[field] = verified[field].(int64) + 1
					} else {
						mismatched[field] = "00000000-0000-4000-8000-000000000000"
					}
					if _, err := r.executionStepGateDecisions(ctx, tx, currentScope, mismatched); !errors.Is(err, errs.ErrConflict) {
						t.Fatalf("mismatched gate policy pin was accepted: field=%s", field)
					}
				}
			}()
			if scenario.republish {
				draft.Steps[0].GateDecisions = append([]string(nil), full...)
				workflow = execute(t, owner, command.UpdateWorkflow, key+"edit-new", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref, Draft: &draft}).Workflow
				workflow = execute(t, owner, command.ValidateWorkflow, key+"validate-new", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
				workflow = execute(t, owner, command.PublishWorkflow, key+"publish-new", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
				if workflow.Published.VersionNumber != 2 {
					t.Fatal("independent later publication missing")
				}
			}
			complete("child-done", childLease)
			root, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil || len(root.GateRefs) != 1 {
				t.Fatalf("exact result gate missing: error_nil=%t gate_count=%d", err == nil, len(root.GateRefs))
			}
			gate, err := service.GetOwnerGate(ctx, owner, root.GateRefs[0])
			if err != nil || gate.State != "OPEN" || !reflect.DeepEqual(gate.AllowedDecisions, decisions) || proof(key+"none").gates != 1 {
				t.Fatalf("result gate expanded published policy: error_nil=%t open=%t decisions_match=%t", err == nil, gate.State == "OPEN", reflect.DeepEqual(gate.AllowedDecisions, decisions))
			}
			if scenario.cancelRoot {
				execute(t, owner, command.CancelRun, key+"cancel-root", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Synthetic terminal gate"})
				closed, err := service.GetOwnerGate(ctx, owner, gate.Ref)
				if err != nil || closed.State != "CANCELLED" || !reflect.DeepEqual(closed.AllowedDecisions, decisions) {
					t.Fatal("root cancel changed the pinned gate policy or failed to close gate")
				}
				return
			}
			payload := command.GateResolutionInput{GateRef: gate.Ref, Decision: scenario.decision, Comment: "Проверенное решение synthetic fixture."}
			before := proof(key + "foreign")
			if _, err := mutate(foreign, command.ResolveOwnerGate, key+"foreign", &gate.Version, payload); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("foreign project gate decision escaped owner scope")
			}
			if proof(key+"foreign") != before {
				t.Fatal("foreign gate decision changed owner graph")
			}
			stale := gate.Version + 1
			before = proof(key + "stale")
			if _, err := mutate(owner, command.ResolveOwnerGate, key+"stale", &stale, payload); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatal("stale gate decision accepted")
			}
			if proof(key+"stale") != before {
				t.Fatal("stale gate decision changed owner graph")
			}
			if !contains(decisions, "REQUEST_CHANGES") {
				forbidden := payload
				forbidden.Decision = "REQUEST_CHANGES"
				before = proof(key + "excluded")
				if _, err := mutate(owner, command.ResolveOwnerGate, key+"excluded", &gate.Version, forbidden); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("excluded REQUEST_CHANGES was not forbidden")
				}
				if proof(key+"excluded") != before {
					t.Fatal("excluded decision created owner state or effects")
				}
			}
			beforeResolve := proof(key + "decision")
			execute(t, owner, command.ResolveOwnerGate, key+"decision", &gate.Version, payload)
			if scenario.decision == "REQUEST_CHANGES" && proof(key+"decision").turns != beforeResolve.turns+1 {
				t.Fatal("allowed change request did not create exactly one new turn")
			}
			resolved, err := service.GetOwnerGate(ctx, owner, gate.Ref)
			expected := map[string]string{"APPROVE": "APPROVED", "REJECT": "REJECTED", "CANCEL": "CANCELLED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[scenario.decision]
			if err != nil || resolved.State != expected || !reflect.DeepEqual(resolved.AllowedDecisions, decisions) {
				t.Fatal("allowed decision did not preserve the existing gate lifecycle")
			}
			before = proof(key + "decision")
			execute(t, owner, command.ResolveOwnerGate, key+"decision", &gate.Version, payload)
			if proof(key+"decision") != before {
				t.Fatal("exact owner decision replay duplicated graph, receipt or event")
			}
		}) {
			t.FailNow()
		}
	}
}
