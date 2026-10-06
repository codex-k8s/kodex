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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/workflow_launch_proof.sql
var queryWorkflowLaunchProof string

//go:embed testdata/sql/workflow_launch_purge_graph.sql
var queryWorkflowLaunchPurgeGraph string

//go:embed testdata/sql/workflow_launch_origin_diagnostics.sql
var queryWorkflowLaunchOriginDiagnostics string

func TestWorkflowLaunchComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
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
	var graphNodes, graphEdges int
	var graphNodeHash, graphEdgeHash string
	if pool.QueryRow(ctx, queryWorkflowLaunchPurgeGraph).Scan(&graphNodes, &graphNodeHash, &graphEdges, &graphEdgeHash) != nil {
		t.Fatal("purge graph unavailable")
	}
	t.Logf("purge graph: %d %s %d %s", graphNodes, graphNodeHash, graphEdges, graphEdgeHash)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	launcher := worker
	launcher.Permission = "platform.runtime.execution.workflow.launch"
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, payload any, version *int64) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "workflow-launch-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Required workflows", Language: "en"}, nil).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "workflow-launch-manager", "Manager")
	manager = *execute(command.ChangeAgentCapability, owner, "manager-cap", command.AgentBindingInput{AgentRef: manager.Ref, BindingRef: "platform.run.launch", Enabled: true}, &manager.Version).Agent
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "workflow-launch-coordinator", "Coordinator")
	coordinator = *execute(command.ChangeAgentCapability, owner, "coord-cap", command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &coordinator.Version).Agent
	coordinator = *execute(command.ChangeAgentCapability, owner, "coord-launch-cap", command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.launch", Enabled: true}, &coordinator.Version).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "workflow-launch-specialist", "Specialist")
	specialist = *execute(command.ChangeAgentCapability, owner, "specialist-launch-cap", command.AgentBindingInput{AgentRef: specialist.Ref, BindingRef: "platform.run.launch", Enabled: true}, &specialist.Version).Agent
	draft := entity.WorkflowVersion{Name: "Required workflow", Purpose: "Bounded lifecycle", CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Verified child result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "step", Position: 1, Name: "Step", AgentRef: specialist.Ref, Instructions: "Complete bounded step.", ExpectedResult: "Verified result", TimeoutSeconds: 900, RequiredCapabilityKeys: []string{"platform.run.launch"}}}}
	workflow := execute(command.CreateWorkflow, owner, "create", command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}, nil).Workflow
	workflow = execute(command.ValidateWorkflow, owner, "validate", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, owner, "publish", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	gatedDraft := draft
	gatedDraft.Name = "Required gated workflow"
	gatedDraft.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
	gatedDraft.Steps[0].HumanGateAfter = true
	gatedDraft.Steps[0].GateDecisions = []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}
	gated := execute(command.CreateWorkflow, owner, "gated-create", command.WorkflowInput{ProjectRef: project.Ref, Name: gatedDraft.Name, Purpose: gatedDraft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &gatedDraft}, nil).Workflow
	gated = execute(command.ValidateWorkflow, owner, "gated-validate", command.WorkflowInput{Ref: gated.Ref}, &gated.Version).Workflow
	gated = execute(command.PublishWorkflow, owner, "gated-publish", command.WorkflowInput{Ref: gated.Ref}, &gated.Version).Workflow
	selectedWorkflow := workflow.Ref
	claim := func(key, run string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, worker, key, command.LeaseInput{WorkloadInstance: "workflow-launch-fixture", Limit: 1}, nil).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run {
			t.Fatalf("claim %s: expected %s, got %v", key, run, items)
		}
		if strings.Contains(key, "callback") || strings.Contains(key, "continuation") {
			events, _, _, readErr := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run, Limit: 500})
			if readErr != nil {
				t.Fatal("read callback public events", readErr)
			}
			found := false
			for _, event := range events {
				if event.Delta.Execution == nil || event.Delta.Execution.NodeRef != stringMap(items[0], "nodeRef") || event.Delta.Message == nil || event.Delta.Message.Phase != "USER" {
					continue
				}
				if event.Delta.Message.Source.Origin == "CALLBACK_CONTINUATION" {
					found = true
					if event.Delta.Message.Text != callbackContinuationPublicText || event.Delta.Node == nil || event.Delta.Node.InputSummary != callbackContinuationPublicText {
						t.Fatal("callback public projection exposed runtime instruction")
					}
				}
			}
			if !found {
				t.Fatalf("canonical callback source is missing for %s", key)
			}
		}
		return items[0]
	}
	complete := func(key string, lease map[string]any, success bool) command.Result {
		code := ""
		if !success {
			code = "RUNTIME_UNAVAILABLE"
		}
		return execute(command.CompleteExecution, worker, key, command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: success, SafeErrorCode: code, ResultSummary: "Bounded fixture result", Usage: turnUsageFixture()}, nil)
	}
	launch := func(key string, lease map[string]any) command.Result {
		var diagnostics map[string]any
		if pool.QueryRow(ctx, queryWorkflowLaunchOriginDiagnostics, pgx.StrictNamedArgs{"lease_ref": stringMap(lease, "leaseRef")}).Scan(&diagnostics) == nil {
			t.Logf("launch %s origin states: %v", key, diagnostics)
		}
		return execute(command.LaunchWorkflowExecution, launcher, key, command.LaunchWorkflowInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), WorkflowRef: selectedWorkflow, Task: "Complete the exact published workflow."}, nil)
	}
	cancelRun := func(key, ref string) {
		t.Helper()
		run, err := service.GetRun(ctx, owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.CancelRun, owner, key, command.RunCommandInput{RunRef: ref}, &run.Version)
	}
	read := func(ref string) entity.Run {
		t.Helper()
		run, err := service.GetRun(ctx, owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	proof := func(child string) (string, string, string, bool, int64, int64) {
		t.Helper()
		var state, origin, root string
		var exact bool
		var leases, turns int64
		if err := pool.QueryRow(ctx, queryWorkflowLaunchProof, pgx.StrictNamedArgs{"child_ref": child}).Scan(&state, &origin, &root, &exact, &leases, &turns); err != nil {
			t.Fatal(err)
		}
		return state, origin, root, exact, leases, turns
	}
	for _, scenario := range []string{"parent-cancel", "owner-child-cancel", "parent-failure", "success", "nested-success", "early-nested-success", "owner-child-gate-reject"} {
		t.Run(scenario, func(t *testing.T) {
			selectedWorkflow = workflow.Ref
			if scenario == "owner-child-gate-reject" {
				selectedWorkflow = gated.Ref
			}
			parent := execute(command.LaunchRun, owner, scenario+"-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Launch one required workflow."}, nil).Run
			origin := claim(scenario+"-parent-claim", parent.Ref)
			child := launch(scenario+"-launch", origin)
			replay := launch(scenario+"-launch", origin)
			if replay.Run.Ref != child.Run.Ref {
				t.Fatal("replay duplicated child root")
			}
			duplicate := launch(scenario+"-dedup", origin)
			if duplicate.Run.Ref != child.Run.Ref {
				t.Fatal("new transport key duplicated same accepted intent")
			}
			state, parentRef, rootRef, exact, _, _ := proof(child.Run.Ref)
			if state != "OPEN" || parentRef != parent.Ref || rootRef != child.Run.Ref || !exact || child.Run.ParentRunRef != parent.Ref || child.Run.Source != "AGENT_DELEGATION" {
				t.Fatal("server-owned origin or own WF root mismatch")
			}
			childLease := claim(scenario+"-child-claim", child.Run.Ref)
			var grandchild *entity.Run
			var nestedParentLease, grandchildLease map[string]any
			if scenario == "parent-cancel" || scenario == "owner-child-cancel" || scenario == "nested-success" || scenario == "early-nested-success" || scenario == "owner-child-gate-reject" {
				// Coordinator имеет только materialized delegate capability; nested
				// launch выполняет реальный step с явно опубликованным required key.
				delegated := execute(command.DelegateExecution, worker, scenario+"-nested-step", command.DelegateInput{LeaseRef: stringMap(childLease, "leaseRef"), Fence: stringMap(childLease, "fence"), Generation: runtimeRevisionMapInt64(childLease, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step", Task: "Launch one nested required workflow."}, nil)
				complete(scenario+"-nested-coord-complete", childLease, true)
				nestedParentLease = claim(scenario+"-nested-step-claim", delegated.Run.Ref)
				selectedWorkflow = workflow.Ref
				grandchild = launch(scenario+"-nested", nestedParentLease).Run
				grandchildLease = claim(scenario+"-nested-claim", grandchild.Ref)
			}
			if scenario == "parent-cancel" {
				currentParent := read(parent.Ref)
				func() {
					original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
					queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
					defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
					_, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "workflow-launch-atomic-cancel", ExpectedVersion: &currentParent.Version}, Payload: command.RunCommandInput{RunRef: parent.Ref}})
					if !errors.Is(err, errs.ErrUnavailable) {
						t.Fatalf("audit failure: %v", err)
					}
				}()
				if state, _, _, _, leases, _ := proof(child.Run.Ref); state != "OPEN" || leases != 1 || read(parent.Ref).State != "RUNNING" {
					t.Fatal("audit failure committed partial required graph")
				}
				cancelRun(scenario+"-cancel", parent.Ref)
				state, _, _, _, leases, turns := proof(child.Run.Ref)
				if state != "CANCELLED" || read(child.Run.Ref).State != "CANCELLED" || leases != 0 || turns != 0 {
					t.Fatal("parent cancel did not atomically close required root")
				}
				if state, _, _, _, leases, turns := proof(grandchild.Ref); state != "CANCELLED" || leases != 0 || turns != 0 {
					t.Fatal("parent cancellation left nested required root")
				}
				cancelled := read(parent.Ref)
				retried := execute(command.RetryRun, owner, scenario+"-retry", command.RunCommandInput{RunRef: parent.Ref}, &cancelled.Version).Run
				retryChild := launch(scenario+"-retry-launch", claim(scenario+"-retry-claim", retried.Ref)).Run
				if retryChild.Ref == child.Run.Ref || retried.Ref == parent.Ref {
					t.Fatal("retry reused old required origin")
				}
				if oldState, oldParent, _, _, _, _ := proof(child.Run.Ref); oldState != "CANCELLED" || oldParent != parent.Ref {
					t.Fatal("retry rewrote old required origin")
				}
				cancelRun(scenario+"-retry-cleanup", retried.Ref)
				return
			}
			if scenario == "parent-failure" {
				complete(scenario+"-fail", origin, false)
				if state, _, _, _, leases, turns := proof(child.Run.Ref); state != "CANCELLED" || leases != 0 || turns != 0 {
					t.Fatal("parent failure retained required child authority")
				}
				return
			}
			complete(scenario+"-parent-complete", origin, true)
			if read(parent.Ref).State != "RUNNING" {
				t.Fatal("parent finished before required workflow")
			}
			if scenario == "nested-success" || scenario == "early-nested-success" || scenario == "owner-child-gate-reject" {
				if scenario == "nested-success" || scenario == "owner-child-gate-reject" {
					complete(scenario+"-nested-parent-complete", nestedParentLease, true)
				}
				delegated := execute(command.DelegateExecution, worker, scenario+"-grand-step", command.DelegateInput{LeaseRef: stringMap(grandchildLease, "leaseRef"), Fence: stringMap(grandchildLease, "fence"), Generation: runtimeRevisionMapInt64(grandchildLease, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step", Task: "Complete nested canonical step."}, nil)
				complete(scenario+"-grand-coord-complete", grandchildLease, true)
				complete(scenario+"-grand-step-complete", claim(scenario+"-grand-step-claim", delegated.Run.Ref), true)
				complete(scenario+"-grand-callback-complete", claim(scenario+"-grand-callback", grandchild.Ref), true)
				if scenario == "early-nested-success" {
					complete(scenario+"-nested-parent-complete", nestedParentLease, true)
				}
				stepContinuation := claim(scenario+"-step-continuation", stringMap(nestedParentLease, "runRef"))
				var diagnostics map[string]any
				if pool.QueryRow(ctx, queryWorkflowLaunchOriginDiagnostics, pgx.StrictNamedArgs{"lease_ref": stringMap(stepContinuation, "leaseRef")}).Scan(&diagnostics) != nil || diagnostics["workflowStepKey"] != "step" || diagnostics["revisionCap"] != true {
					t.Fatal("required continuation widened canonical workflow step scope")
				}
				complete(scenario+"-step-continuation-complete", stepContinuation, true)
				if scenario == "owner-child-gate-reject" {
					gatedRun := read(child.Run.Ref)
					if len(gatedRun.GateRefs) != 1 {
						t.Fatal("required result owner gate missing")
					}
					gate, err := service.GetOwnerGate(ctx, owner, gatedRun.GateRefs[0])
					if err != nil || gate.State != "OPEN" {
						t.Fatal("required result did not preserve final owner gate")
					}
					execute(command.ResolveOwnerGate, owner, scenario+"-reject", command.GateResolutionInput{GateRef: gate.Ref, Decision: "REJECT", Comment: "Bounded owner rejection."}, &gate.Version)
					if state, _, _, _, leases, turns := proof(child.Run.Ref); state != "FAILED" || leases != 0 || turns != 0 {
						t.Fatal("owner reject left required workflow authority")
					}
					complete(scenario+"-parent-reject-callback", claim(scenario+"-parent-reject-claim", parent.Ref), true)
					if read(parent.Ref).State != "FAILED" {
						t.Fatal("owner-rejected required result permitted fake parent success")
					}
					return
				}
				complete(scenario+"-outer-callback-complete", claim(scenario+"-outer-callback", child.Run.Ref), true)
				complete(scenario+"-root-callback-complete", claim(scenario+"-root-callback", parent.Ref), true)
				if read(parent.Ref).State != "SUCCEEDED" || read(child.Run.Ref).State != "SUCCEEDED" || read(grandchild.Ref).State != "SUCCEEDED" {
					t.Fatal("required nested callback graph did not finish")
				}
				return
			}
			if scenario == "owner-child-cancel" {
				cancelRun(scenario+"-child-cancel", child.Run.Ref)
				if state, _, _, _, leases, turns := proof(grandchild.Ref); state != "CANCELLED" || leases != 0 || turns != 0 {
					t.Fatal("owner workflow cancellation retained required subtree")
				}
				callback := claim(scenario+"-callback", parent.Ref)
				complete(scenario+"-callback-complete", callback, true)
				if read(parent.Ref).State != "FAILED" {
					t.Fatal("cancelled required child permitted fake success")
				}
				return
			}
			targets, _ := childLease["delegationTargets"].([]map[string]string)
			if len(targets) != 1 {
				t.Fatalf("canonical workflow step catalog missing: %T", childLease["delegationTargets"])
			}
			delegated := execute(command.DelegateExecution, worker, scenario+"-step", command.DelegateInput{LeaseRef: stringMap(childLease, "leaseRef"), Fence: stringMap(childLease, "fence"), Generation: runtimeRevisionMapInt64(childLease, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: targets[0]["workflowStepKey"], Task: "Complete canonical step."}, nil)
			complete(scenario+"-coord-complete", childLease, true)
			complete(scenario+"-step-complete", claim(scenario+"-step-claim", delegated.Run.Ref), true)
			complete(scenario+"-wf-callback-complete", claim(scenario+"-wf-callback", child.Run.Ref), true)
			if read(child.Run.Ref).State != "SUCCEEDED" {
				t.Fatal("workflow did not own its canonical completion")
			}
			complete(scenario+"-parent-callback-complete", claim(scenario+"-parent-callback", parent.Ref), true)
			if read(parent.Ref).State != "SUCCEEDED" {
				t.Fatal("successful required result did not unblock parent")
			}
		})
	}
	t.Run("current-capability-and-lease", func(t *testing.T) {
		parent := execute(command.LaunchRun, owner, "denied-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Revalidate exact current launch authority."}, nil).Run
		origin := claim("denied-claim", parent.Ref)
		toolInput := command.RunToolCallInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), CallRef: "tcl_launchproof01", Tool: "launch_workflow", CapabilityRef: "platform.run.launch", State: "RUNNING", Revision: 1, SafeParameters: map[string]any{"workflow_ref": workflow.Ref}}
		toolWorker := worker
		toolWorker.Permission = "platform.runtime.tool-call.record"
		activity := execute(command.RecordRunToolCall, toolWorker, "actual-native-tool", toolInput, nil)
		if activity.Event == nil || activity.Event.ToolCall == nil || activity.Event.Actor.Kind != "AGENT" || activity.Event.ToolCall.Tool != "launch_workflow" {
			t.Fatal("actual native activity consumer missing")
		}
		managerView, err := service.GetAgent(ctx, owner, manager.Ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.ChangeAgentCapability, owner, "revoke-launch", command.AgentBindingInput{AgentRef: manager.Ref, BindingRef: "platform.run.launch", Enabled: false}, &managerView.Version)
		toolInput.State = "SUCCEEDED"
		toolInput.Revision = 2
		if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolWorker, Mutation: value.Mutation{IdempotencyKey: "workflow-launch-revoked-tool"}, Payload: toolInput}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("current native capability guard: %v", err)
		}
		input := command.Command{Kind: command.LaunchWorkflowExecution, Principal: launcher, Mutation: value.Mutation{IdempotencyKey: "workflow-launch-denied"}, Payload: command.LaunchWorkflowInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), WorkflowRef: workflow.Ref, Task: "Denied launch."}}
		if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("revoked capability accepted: %v", err)
		}
		cancelRun("denied-cleanup", parent.Ref)
	})
}
