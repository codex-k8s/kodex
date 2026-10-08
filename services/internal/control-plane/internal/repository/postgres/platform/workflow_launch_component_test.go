package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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

//go:embed testdata/sql/runtime_deadline_proof.sql
var queryRuntimeDeadlineProof string

//go:embed testdata/sql/runtime_deadline_archive_proof.sql
var queryRuntimeDeadlineArchiveProof string

//go:embed testdata/sql/runtime_deadline_reset_denied.sql
var queryRuntimeDeadlineResetDenied string

//go:embed testdata/sql/runtime_delegate_input_missing.sql
var queryRuntimeDelegateInputMissing string

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
	// Настроенная Agent-пара Context7 не входит в delegate-only scope coordinator.
	// Для такого execution нельзя требовать probe или добавлять managed MCP profile.
	context7 := *execute(command.CreateConnection, owner, "context7-create", command.ConnectionInput{DefinitionKey: "context7", Name: "Coordinator Context7", PublicConfiguration: map[string]any{"base_url": "https://mcp.context7.com"}}, nil).Connection
	if _, err := pool.Exec(ctx, `WITH credential AS (
		INSERT INTO control_plane.integration_credential_revisions
		(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
		SELECT 'icr_workflow_'||ref,organization_id,id,1,'kodex-system/synthetic#api_key',
		'60000000-0000-4000-8000-000000000001'::uuid,'1',repeat('a',64),created_by
		FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
		UPDATE control_plane.integration_connections c SET credential_revision_id=credential.id,
		state='CONNECTED',masked_credentials_state='CONFIGURED',version=version+1
		FROM credential WHERE c.id=credential.connection_id`, context7.Ref); err != nil {
		t.Fatal("configure coordinator synthetic credential metadata")
	}
	context7, err = service.GetIntegrationConnection(ctx, owner, context7.Ref)
	if err != nil {
		t.Fatal("read coordinator Context7 fixture")
	}
	for _, capability := range []string{"context7.library.resolve", "context7.docs.query"} {
		context7 = *execute(command.ChangeIntegrationGrant, owner, "context7-"+capability, command.IntegrationGrantInput{ConnectionRef: context7.Ref, CapabilityKey: capability, AgentRef: coordinator.Ref, ApprovalPolicy: "NONE", Enabled: true}, &context7.Version).Connection
	}
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
	observedClocks := map[string]runtimecontract.RuntimeExecutionClock{}
	claim := func(key, run string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, worker, key, command.LeaseInput{WorkloadInstance: "workflow-launch-fixture", Limit: 1}, nil).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run {
			t.Fatalf("claim %s: expected %s, got %v", key, run, items)
		}
		deadline, decodeErr := runtimecontract.DecodeRuntimeExecutionDeadline(items[0]["executionDeadline"])
		if decodeErr != nil {
			t.Fatal("invalid immutable clock in claim")
		}
		if deadline != nil {
			for _, clock := range deadline.Clocks {
				if previous, found := observedClocks[clock.RunRef]; found && previous != clock {
					t.Fatal("continuation, nested delegation or reclaim reset a durable clock")
				}
				observedClocks[clock.RunRef] = clock
			}
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
		result := execute(command.LaunchWorkflowExecution, launcher, key, command.LaunchWorkflowInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), WorkflowRef: selectedWorkflow, Task: "Complete the exact published workflow."}, nil)
		parent, err := service.GetRun(ctx, owner, result.Run.ParentRunRef)
		if err != nil {
			t.Fatal("workflow parent owner read denied", err)
		}
		_, graph, err := service.GetRunGraphSnapshot(ctx, owner, parent.RootRunRef)
		if err != nil {
			t.Fatal("manager to workflow snapshot denied", err)
		}
		found := false
		for _, run := range graph.Runs {
			if run.Ref == result.Run.Ref {
				found = run.RootRunRef == result.Run.Ref && run.ParentRunRef == result.Run.ParentRunRef
			}
		}
		if !found {
			t.Fatal("canonical cross-root workflow snapshot absent")
		}
		return result
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
	t.Run("workflow-input-inheritance", func(t *testing.T) {
		inputDraft := draft
		inputDraft.Name = "Immutable input workflow"
		inputDraft.Inputs = []entity.WorkflowInputField{
			{Key: "field-001", Label: "Issue", Type: "TEXT", Required: true},
			{Key: "field-002", Label: "Business", Type: "TEXT", Required: true},
			{Key: "field-003", Label: "Repository", Type: "TEXT", Required: true},
			{Key: "field-004", Label: "Constraints", Type: "TEXT", Required: true},
		}
		inputDraft.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
		second := inputDraft.Steps[0]
		second.Key, second.Position, second.Name = "second", 2, "Second"
		second.DependsOn = []string{"step"}
		inputDraft.Steps = append(inputDraft.Steps, second)
		publish := func(key string, specification entity.WorkflowVersion) *entity.Workflow {
			t.Helper()
			wf := execute(command.CreateWorkflow, owner, key+"-create", command.WorkflowInput{ProjectRef: project.Ref, Name: specification.Name, Purpose: specification.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &specification}, nil).Workflow
			wf = execute(command.ValidateWorkflow, owner, key+"-validate", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
			return execute(command.PublishWorkflow, owner, key+"-publish", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		}
		wf := publish("input", inputDraft)
		original := map[string]any{"field-001": "Issue1796", "field-002": strings.Repeat("business ", 30), "field-003": "Repository", "field-004": "Exact owner constraints"}
		root := execute(command.LaunchRun, owner, "input-root", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: wf.Ref}, Task: "Preserve original fields", Input: original}, nil).Run
		defer cancelRun("input-cleanup", root.Ref)
		current := claim("input-coordinator", root.Ref)
		delegate := func(key, step string, additions map[string]any) (command.Result, error) {
			return service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "workflow-input-" + key}, Payload: command.DelegateInput{LeaseRef: stringMap(current, "leaseRef"), Fence: stringMap(current, "fence"), Generation: runtimeRevisionMapInt64(current, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: step, Task: "Read exact input", Input: additions}})
		}
		_, before, err := service.GetRunGraph(ctx, owner, root.Ref)
		if err != nil {
			t.Fatal(err)
		}
		tooMany := map[string]any{}
		for index := 0; index < 99; index++ {
			tooMany[string(rune('A'+index))] = true
		}
		for key, additions := range map[string]map[string]any{
			"collision": {"field-001": "Another Issue"},
			"type":      {"field-001": false},
			"null":      {"field-001": nil},
			"keys":      tooMany,
			"bytes":     {"handoff": strings.Repeat("x", 65450)},
		} {
			if _, err := delegate(key, "step", additions); !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("%s did not reject invalid merged input: %v", key, err)
			}
		}
		_, after, err := service.GetRunGraph(ctx, owner, root.Ref)
		if err != nil || len(before.Nodes) != len(after.Nodes) || len(before.Edges) != len(after.Edges) {
			t.Fatal("invalid input changed execution graph")
		}
		originalQuery := queryRuntimeDelegateexecutionSelectRunsId
		queryRuntimeDelegateexecutionSelectRunsId = queryRuntimeClaimAuditUnavailable
		_, unavailableErr := delegate("sql-failure", "step", nil)
		queryRuntimeDelegateexecutionSelectRunsId = originalQuery
		if !errors.Is(unavailableErr, errs.ErrUnavailable) {
			t.Fatalf("SQL failure did not remain unavailable: %v", unavailableErr)
		}
		queryRuntimeDelegateexecutionSelectRunsId = queryRuntimeDelegateInputMissing
		_, missingErr := delegate("missing-root", "step", nil)
		queryRuntimeDelegateexecutionSelectRunsId = originalQuery
		if !errors.Is(missingErr, errs.ErrUnavailable) {
			t.Fatalf("missing root did not remain unavailable: %v", missingErr)
		}
		_, after, err = service.GetRunGraph(ctx, owner, root.Ref)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if err != nil || string(beforeJSON) != string(afterJSON) {
			t.Fatal("rejected merge or unavailable source changed graph")
		}
		child, err := delegate("first", "step", map[string]any{"field-001": original["field-001"], "handoff": "Read predecessor"})
		if err != nil {
			t.Fatal(err)
		}
		complete("input-first-coordinator-complete", current, true)
		childLease := claim("input-first-child", child.Run.Ref)
		assertInput := func(lease map[string]any, expected map[string]any) {
			t.Helper()
			actual, err := json.Marshal(lease["input"])
			want, marshalErr := json.Marshal(expected)
			if err != nil || marshalErr != nil || string(actual) != string(want) {
				t.Fatalf("immutable child input mismatch: got %s, want %s", actual, want)
			}
			digest, digestErr := runtimecontract.RuntimeBoundedInputDigest(expected)
			if digestErr != nil || stringMap(lease, "inputDigest") != digest {
				t.Fatal("claim digest does not bind merged input")
			}
			for _, value := range expected {
				if text, ok := value.(string); ok && !strings.Contains(stringMap(lease, "instructions"), text) {
					t.Fatal("materialized INPUT omitted inherited value")
				}
			}
		}
		merged := map[string]any{}
		for key, val := range original {
			merged[key] = val
		}
		merged["handoff"] = "Read predecessor"
		assertInput(childLease, merged)
		complete("input-first-child-complete", childLease, true)
		current = claim("input-first-callback", root.Ref)
		child, err = delegate("second", "second", nil)
		if err != nil {
			t.Fatal(err)
		}
		complete("input-second-coordinator-complete", current, true)
		childLease = claim("input-second-child", child.Run.Ref)
		assertInput(childLease, original)
		// Вложенный Workflow выбирает собственный root/version, не внешний input.
		innerDraft := inputDraft
		innerDraft.Name = "Nested immutable input"
		innerDraft.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
		inner := publish("input-inner", innerDraft)
		innerInput := map[string]any{"field-001": "Inner Issue", "field-002": "Inner business", "field-003": "Inner repository", "field-004": "Inner constraints"}
		nested := execute(command.LaunchWorkflowExecution, launcher, "input-inner-launch", command.LaunchWorkflowInput{LeaseRef: stringMap(childLease, "leaseRef"), Fence: stringMap(childLease, "fence"), Generation: runtimeRevisionMapInt64(childLease, "generation"), WorkflowRef: inner.Ref, Task: "Nested workflow", Input: innerInput}, nil)
		complete("input-second-child-waits", childLease, true)
		current = claim("input-inner-coordinator", nested.Run.Ref)
		assertInput(current, innerInput)
		child, err = delegate("inner", "step", nil)
		if err != nil {
			t.Fatal(err)
		}
		complete("input-inner-coordinator-complete", current, true)
		assertInput(claim("input-inner-child", child.Run.Ref), innerInput)
	})
	t.Run("durable-workflow-deadline", func(t *testing.T) {
		short := draft
		short.TimeoutSeconds = 1
		wf := execute(command.CreateWorkflow, owner, "timeout-create", command.WorkflowInput{ProjectRef: project.Ref, Name: "Clock", Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &short}, nil).Workflow
		wf = execute(command.ValidateWorkflow, owner, "timeout-validate", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		wf = execute(command.PublishWorkflow, owner, "timeout-publish", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		for _, operation := range []command.Kind{command.RenewExecution, command.CompleteExecution, command.DelegateExecution} {
			key := "timeout-" + string(operation)
			root := execute(command.LaunchRun, owner, key+"-root", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: wf.Ref}, Task: "Bounded clock"}, nil).Run
			// Даже очередь дольше configured срока не запускает часы.
			if operation == command.RenewExecution {
				time.Sleep(1100 * time.Millisecond)
			}
			lease := claim(key+"-claim", root.Ref)
			deadline, err := runtimecontract.DecodeRuntimeExecutionDeadline(lease["executionDeadline"])
			if err != nil || deadline == nil || len(deadline.Clocks) != 1 || deadline.Clocks[0].TimeoutSeconds != 1 {
				t.Fatalf("exact first claim clock missing: %v", err)
			}
			initial := deadline.Clocks[0]
			renewed := execute(command.RenewExecution, worker, key+"-initial-renew", command.LeaseInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}, nil)
			if stringMap(renewed.Runtime, "leaseRef") != stringMap(lease, "leaseRef") {
				t.Fatal("renew changed lease")
			}
			time.Sleep(time.Until(initial.DeadlineAt) + 100*time.Millisecond)
			var payload any = command.LeaseInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}
			if operation == command.CompleteExecution {
				payload = command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Late success must not win", Usage: turnUsageFixture(), CodexSessionID: "00000000-0000-4000-8000-000000000001", ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/08/rollout-2026-10-08T00-00-00-00000000-0000-4000-8000-000000000001.jsonl", ArchiveSHA256: strings.Repeat("d", 64), ArchiveSizeBytes: 26276}
			}
			if operation == command.DelegateExecution {
				payload = command.DelegateInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step", Task: "Must not create child"}
			}
			_, err = service.Execute(ctx, command.Command{Kind: operation, Principal: worker, Mutation: value.Mutation{IdempotencyKey: key + "-late"}, Payload: payload})
			if !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("late %s not denied: %v", operation, err)
			}
			current := read(root.Ref)
			if current.State != "FAILED" || current.SafeErrorCode != "RUNTIME_TIMEOUT" {
				t.Fatalf("timeout not committed: %s/%s", current.State, current.SafeErrorCode)
			}
			if operation == command.CompleteExecution {
				var archiveHash string
				var archiveSize int64
				if current.Usage.TotalTokens != turnUsageFixture().TotalTokens || pool.QueryRow(ctx, queryRuntimeDeadlineArchiveProof, pgx.StrictNamedArgs{"run_ref": root.Ref}).Scan(&archiveHash, &archiveSize) != nil || archiveHash != strings.Repeat("d", 64) || archiveSize != 26276 {
					t.Fatal("late complete lost already measured usage or verified archive pins")
				}
			}
			var started, ends time.Time
			var seconds int32
			var step string
			var leases, nodes int64
			var eligible bool
			if pool.QueryRow(ctx, queryRuntimeDeadlineProof, pgx.StrictNamedArgs{"run_ref": root.Ref}).Scan(&started, &ends, &seconds, &step, &leases, &nodes, &eligible) != nil || leases != 0 || nodes != 0 || eligible || !started.Equal(initial.StartedAt) || !ends.Equal(initial.DeadlineAt) {
				t.Fatal("timeout left active graph or reset clock")
			}
		}
	})
	t.Run("step-clock-includes-human-gate-and-waiting", func(t *testing.T) {
		for _, gatedStep := range []bool{false, true} {
			name := map[bool]string{false: "waiting", true: "gate"}[gatedStep]
			short := draft
			short.TimeoutSeconds = 60
			short.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
			short.Steps[0].TimeoutSeconds = 1
			short.Steps[0].HumanGateAfter = gatedStep
			if gatedStep {
				short.Steps[0].GateDecisions = []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}
			}
			wf := execute(command.CreateWorkflow, owner, "step-clock-"+name+"-create", command.WorkflowInput{ProjectRef: project.Ref, Name: "Step clock " + name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &short}, nil).Workflow
			wf = execute(command.ValidateWorkflow, owner, "step-clock-"+name+"-validate", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
			wf = execute(command.PublishWorkflow, owner, "step-clock-"+name+"-publish", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
			root := execute(command.LaunchRun, owner, "step-clock-"+name+"-root", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: wf.Ref}, Task: "Exact step wall clock"}, nil).Run
			origin := claim("step-clock-"+name+"-coordinator", root.Ref)
			originClock, _ := runtimecontract.DecodeRuntimeExecutionDeadline(origin["executionDeadline"])
			child := execute(command.DelegateExecution, worker, "step-clock-"+name+"-delegate", command.DelegateInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step", Task: "Exact stage"}, nil).Run
			complete("step-clock-"+name+"-coordinator-complete", origin, true)
			stage := claim("step-clock-"+name+"-stage", child.Ref)
			deadline, err := runtimecontract.DecodeRuntimeExecutionDeadline(stage["executionDeadline"])
			if err != nil || deadline == nil || len(deadline.Clocks) != 2 {
				t.Fatal("stage did not inherit root and own immutable clocks")
			}
			var stepClock runtimecontract.RuntimeExecutionClock
			for _, clock := range deadline.Clocks {
				if clock.RunRef == root.Ref && (!clock.StartedAt.Equal(originClock.Clocks[0].StartedAt) || !clock.DeadlineAt.Equal(originClock.Clocks[0].DeadlineAt)) {
					t.Fatal("root clock reset on stage claim")
				}
				if clock.RunRef == child.Ref {
					stepClock = clock
				}
			}
			if stepClock.StepKey != "step" || stepClock.TimeoutSeconds != 1 || !deadline.EffectiveDeadlineAt.Equal(stepClock.DeadlineAt) {
				t.Fatal("step timeout not pinned")
			}
			var gate entity.OwnerGate
			if gatedStep {
				complete("step-clock-"+name+"-stage-complete", stage, true)
				current := read(root.Ref)
				if len(current.GateRefs) != 1 {
					t.Fatal("step owner gate missing")
				}
				gate, err = service.GetOwnerGate(ctx, owner, current.GateRefs[0])
				if err != nil || gate.State != "OPEN" {
					t.Fatal("step gate not open")
				}
			}
			time.Sleep(time.Until(stepClock.DeadlineAt) + 100*time.Millisecond)
			if gatedStep {
				_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "step-clock-" + name + "-late-approve", ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Late approval must not resume"}})
				if !errors.Is(err, errs.ErrForbidden) {
					t.Fatalf("expired gate approved: %v", err)
				}
			} else {
				items := execute(command.ClaimExecution, worker, "step-clock-waiting-poll", command.LeaseInput{WorkloadInstance: "workflow-launch-fixture", Limit: 1}, nil).RuntimeItems
				if len(items) != 0 {
					t.Fatal("expired waiting graph created a new claim")
				}
			}
			current := read(root.Ref)
			if current.State != "FAILED" || current.SafeErrorCode != "RUNTIME_TIMEOUT" {
				t.Fatal("step expiry did not terminalize complete owner graph")
			}
			if gatedStep {
				latest, e := service.GetOwnerGate(ctx, owner, gate.Ref)
				if e != nil || latest.State != "CANCELLED" {
					t.Fatal("timeout left owner gate open")
				}
			}
		}
	})
	t.Run("reclaim-and-owner-retry-clock", func(t *testing.T) {
		root := execute(command.LaunchRun, owner, "reclaim-clock-root", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Exact reclaim and retry"}, nil).Run
		initial := claim("reclaim-clock-initial", root.Ref)
		if _, err := pool.Exec(ctx, queryRuntimeDeadlineResetDenied, pgx.StrictNamedArgs{"run_ref": root.Ref}); err == nil {
			t.Fatal("DB accepted reset of owner first-start")
		}
		var previousExpiry time.Time
		if pool.QueryRow(ctx, queryAssistantCurrentConfigurationExpire, stringMap(initial, "leaseRef")).Scan(&previousExpiry) != nil {
			t.Fatal("exact synthetic lease expiry fixture failed")
		}
		reclaimed := claim("reclaim-clock-again", root.Ref)
		if runtimeRevisionMapInt64(reclaimed, "generation") <= runtimeRevisionMapInt64(initial, "generation") || stringMap(reclaimed, "leaseRef") == stringMap(initial, "leaseRef") {
			t.Fatal("reclaim did not issue fresh fenced lease")
		}
		_, err := service.Execute(ctx, command.Command{Kind: command.RenewExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "reclaim-clock-old-renew"}, Payload: command.LeaseInput{LeaseRef: stringMap(initial, "leaseRef"), Fence: stringMap(initial, "fence"), Generation: runtimeRevisionMapInt64(initial, "generation")}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("old reclaimed fence renewed")
		}
		cancelRun("reclaim-clock-cancel", root.Ref)
		cancelled := read(root.Ref)
		retry := execute(command.RetryRun, owner, "reclaim-clock-retry", command.RunCommandInput{RunRef: root.Ref}, &cancelled.Version).Run
		if retry.Ref == root.Ref {
			t.Fatal("retry reused immutable owner clock")
		}
		retried := claim("reclaim-clock-retry-first", retry.Ref)
		oldDeadline, _ := runtimecontract.DecodeRuntimeExecutionDeadline(initial["executionDeadline"])
		newDeadline, _ := runtimecontract.DecodeRuntimeExecutionDeadline(retried["executionDeadline"])
		if newDeadline == nil || len(newDeadline.Clocks) != 1 || !newDeadline.Clocks[0].StartedAt.After(oldDeadline.Clocks[0].StartedAt) {
			t.Fatal("owner retry did not start a new clock")
		}
		cancelRun("reclaim-clock-retry-cleanup", retry.Ref)
	})
	t.Run("ordinary-sequential-delegation-callback", func(t *testing.T) {
		// Обычный Manager продолжает задачу после callback, сохраняя только
		// текущую capability. Каждая делегация принадлежит свежей attempt.
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "ordinary-manager", "Ordinary manager")
		agent = *execute(command.ChangeAgentCapability, owner, "ordinary-manager-cap", command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &agent.Version).Agent
		parent := execute(command.LaunchRun, owner, "ordinary-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Delegate review, then delegate response.", Input: map[string]any{"root-only": "Must not inherit"}}, nil).Run
		initial := claim("ordinary-initial-claim", parent.Ref)
		current := initial
		for index, stage := range []string{"review", "response"} {
			targets, ok := current["delegationTargets"].([]map[string]string)
			found := false
			for _, target := range targets {
				found = found || target["ref"] == specialist.Ref
			}
			if !ok || !found || !strings.Contains(strings.Join(runtimecontract.RuntimeMCPToolNames(runtimecontract.RunnerInput{DelegationTargets: runtimeRevisionDelegationTargets(current["delegationTargets"])}), ","), "delegate_agent") {
				t.Fatalf("ordinary %s lost its delegation catalog", stage)
			}
			delegated := execute(command.DelegateExecution, worker, "ordinary-"+stage+"-delegate", command.DelegateInput{LeaseRef: stringMap(current, "leaseRef"), Fence: stringMap(current, "fence"), Generation: runtimeRevisionMapInt64(current, "generation"), TargetAgentRef: specialist.Ref, Task: "Complete bounded " + stage, Input: map[string]any{"child-only": stage}}, nil)
			child := delegated.Run
			bound := false
			for _, node := range delegated.Graph.Nodes {
				bound = bound || (node.RunRef == child.Ref && node.ParentNodeRef == stringMap(current, "nodeRef") && node.Type == "AGENT_EXECUTION")
			}
			if !bound {
				t.Fatal("ordinary child lost its current server-owned parent node")
			}
			complete("ordinary-"+stage+"-parent-complete", current, true)
			childLease := claim("ordinary-"+stage+"-child-claim", child.Ref)
			childInput, ok := childLease["input"].(map[string]any)
			if !ok || len(childInput) != 1 || childInput["child-only"] != stage {
				t.Fatal("ordinary delegation changed its payload-only input")
			}
			complete("ordinary-"+stage+"-child-complete", childLease, true)
			if index == 0 {
				current = claim("ordinary-review-callback", parent.Ref)
				if stringMap(current, "sessionRef") != stringMap(initial, "sessionRef") || stringMap(current, "nodeRef") == stringMap(initial, "nodeRef") || stringMap(current, "runtimeRevisionRef") == stringMap(initial, "runtimeRevisionRef") {
					t.Fatal("ordinary callback did not receive a fresh execution in the same session")
				}
				if _, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "ordinary-stale-delegate"}, Payload: command.DelegateInput{LeaseRef: stringMap(initial, "leaseRef"), Fence: stringMap(initial, "fence"), Generation: runtimeRevisionMapInt64(initial, "generation"), TargetAgentRef: specialist.Ref, Task: "Reject stale attempt"}}); err == nil {
					t.Fatal("completed initial lease delegated a new child")
				}
			}
		}
		agent = *execute(command.ChangeAgentCapability, owner, "ordinary-manager-revoke", command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.run.delegate", Enabled: false}, &agent.Version).Agent
		last := claim("ordinary-response-callback", parent.Ref)
		if targets, _ := last["delegationTargets"].([]map[string]string); len(targets) != 0 {
			t.Fatal("revoked ordinary callback retained delegation targets")
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "ordinary-revoked-delegate"}, Payload: command.DelegateInput{LeaseRef: stringMap(last, "leaseRef"), Fence: stringMap(last, "fence"), Generation: runtimeRevisionMapInt64(last, "generation"), TargetAgentRef: specialist.Ref, Task: "Reject revoked capability"}}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("revoked callback delegation was not forbidden: %v", err)
		}
		completed := complete("ordinary-last-complete", last, true)
		if completed.Run == nil || completed.Run.State != "SUCCEEDED" {
			t.Fatal("ordinary sequential delegation did not complete")
		}
	})
	t.Run("two-step-authoritative-callback", func(t *testing.T) {
		specialist = *execute(command.ChangeAgentCapability, owner, "two-artifact-cap", command.AgentBindingInput{AgentRef: specialist.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}, &specialist.Version).Agent
		twoStep := draft
		twoStep.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
		twoStep.Steps[0].Key = "step001"
		twoStep.Steps[0].RequiredCapabilityKeys = []string{"platform.run.launch", runtimecontract.ArtifactCapability}
		second := twoStep.Steps[0]
		second.Key, second.Position, second.Name = "step002", 2, "Documentation"
		twoStep.Steps = append(twoStep.Steps, second)
		wf := execute(command.CreateWorkflow, owner, "two-create", command.WorkflowInput{ProjectRef: project.Ref, Name: "Two steps", Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &twoStep}, nil).Workflow
		wf = execute(command.ValidateWorkflow, owner, "two-validate", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		wf = execute(command.PublishWorkflow, owner, "two-publish", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		selectedWorkflow = wf.Ref
		parent := execute(command.LaunchRun, owner, "two-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Complete both steps once."}, nil).Run
		origin := claim("two-parent-claim", parent.Ref)
		child := launch("two-launch", origin)
		complete("two-parent-complete", origin, true)
		coord := claim("two-coord-claim", child.Run.Ref)
		first := execute(command.DelegateExecution, worker, "two-first", command.DelegateInput{LeaseRef: stringMap(coord, "leaseRef"), Fence: stringMap(coord, "fence"), Generation: runtimeRevisionMapInt64(coord, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step001", Task: "Produce authoritative first result."}, nil).Run
		complete("two-coord-complete", coord, true)
		firstLease := claim("two-first-claim", first.Ref)
		body := []byte("Verified architecture result")
		digest := sha256.Sum256(body)
		firstInput := command.CompleteExecutionInput{LeaseRef: stringMap(firstLease, "leaseRef"), Fence: stringMap(firstLease, "fence"), Generation: runtimeRevisionMapInt64(firstLease, "generation"), Success: true, ResultSummary: "Authoritative architecture completed", Usage: turnUsageFixture(), Artifacts: []command.CompletedArtifact{{FileName: "architecture.txt", MediaType: "text/plain", SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(body)), Content: body}}}
		firstResult := execute(command.CompleteExecution, worker, "two-first-complete", firstInput, nil)
		callback := claim("two-first-callback", child.Run.Ref)
		task := stringMap(callback, "task")
		if !strings.Contains(task, first.Ref) || !strings.Contains(task, "Authoritative architecture completed") || !strings.Contains(task, "step002") {
			t.Fatal("current callback task lacks authoritative result and next step")
		}
		if len(firstResult.Run.ArtifactRefs) != 1 || !strings.Contains(task, firstResult.Run.ArtifactRefs[0]) {
			t.Fatal("current callback task lacks authoritative child artifact ref")
		}
		next := execute(command.DelegateExecution, worker, "two-second", command.DelegateInput{LeaseRef: stringMap(callback, "leaseRef"), Fence: stringMap(callback, "fence"), Generation: runtimeRevisionMapInt64(callback, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step002", Task: "Use the first result for documentation."}, nil).Run
		complete("two-first-callback-complete", callback, true)
		secondLease := claim("two-second-claim", next.Ref)
		complete("two-second-complete", secondLease, true)
		complete("two-second-complete", secondLease, true)
		complete("two-second-callback-complete", claim("two-second-callback", child.Run.Ref), true)
		complete("two-parent-callback-complete", claim("two-parent-callback", parent.Ref), true)
		if read(child.Run.Ref).State != "SUCCEEDED" || read(parent.Ref).State != "SUCCEEDED" {
			t.Fatal("two-step workflow did not complete")
		}
		if len(execute(command.ClaimExecution, worker, "two-no-repeat", command.LeaseInput{WorkloadInstance: "workflow-launch-fixture", Limit: 1}, nil).RuntimeItems) != 0 {
			t.Fatal("completed callback was repeated")
		}
		// Незавершённый step002 сам по себе не является новым callback.
		idleParent := execute(command.LaunchRun, owner, "idle-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Do not repeat empty continuations."}, nil).Run
		idleOrigin := claim("idle-parent-claim", idleParent.Ref)
		idleWF := launch("idle-launch", idleOrigin)
		complete("idle-parent-complete", idleOrigin, true)
		idleCoord := claim("idle-coord-claim", idleWF.Run.Ref)
		idleChild := execute(command.DelegateExecution, worker, "idle-first", command.DelegateInput{LeaseRef: stringMap(idleCoord, "leaseRef"), Fence: stringMap(idleCoord, "fence"), Generation: runtimeRevisionMapInt64(idleCoord, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step001", Task: "Complete one child."}, nil).Run
		complete("idle-coord-complete", idleCoord, true)
		complete("idle-first-complete", claim("idle-first-claim", idleChild.Ref), true)
		complete("idle-callback-complete", claim("idle-callback", idleWF.Run.Ref), true)
		if read(idleWF.Run.Ref).State != "FAILED" || read(idleWF.Run.Ref).SafeErrorCode != "RUNTIME_WORKFLOW_INCOMPLETE" {
			t.Fatal("missing published step did not terminate incomplete workflow")
		}
		complete("idle-parent-callback-complete", claim("idle-parent-callback", idleParent.Ref), true)
		if len(execute(command.ClaimExecution, worker, "idle-no-repeat", command.LeaseInput{WorkloadInstance: "workflow-launch-fixture", Limit: 1}, nil).RuntimeItems) != 0 {
			t.Fatal("missing workflow step generated another callback without new receipt")
		}
	})
	t.Run("coordinator-step-materialization", func(t *testing.T) {
		selfDraft := draft
		selfDraft.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
		selfStep := draft.Steps[0]
		selfStep.Key, selfStep.Position, selfStep.AgentRef = "self-finalize", 2, coordinator.Ref
		selfDraft.Steps = append(selfDraft.Steps, selfStep)
		wf := execute(command.CreateWorkflow, owner, "self-create", command.WorkflowInput{ProjectRef: project.Ref, Name: "Explicit coordinator step", Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &selfDraft}, nil).Workflow
		wf = execute(command.ValidateWorkflow, owner, "self-validate", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		wf = execute(command.PublishWorkflow, owner, "self-publish", command.WorkflowInput{Ref: wf.Ref}, &wf.Version).Workflow
		selectedWorkflow = wf.Ref
		parent := execute(command.LaunchRun, owner, "self-parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Complete explicit coordinator step once."}, nil).Run
		origin := claim("self-parent-claim", parent.Ref)
		child := launch("self-launch", origin)
		complete("self-parent-complete", origin, true)
		coord := claim("self-coord-claim", child.Run.Ref)
		first := execute(command.DelegateExecution, worker, "self-first", command.DelegateInput{LeaseRef: stringMap(coord, "leaseRef"), Fence: stringMap(coord, "fence"), Generation: runtimeRevisionMapInt64(coord, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "step", Task: "Complete first step."}, nil).Run
		complete("self-coord-complete", coord, true)
		complete("self-first-complete", claim("self-first-claim", first.Ref), true)
		callback := claim("self-first-callback", child.Run.Ref)
		if !strings.Contains(stringMap(callback, "task"), "self-finalize") {
			t.Fatal("unmaterialized published coordinator step was hidden")
		}
		self := execute(command.DelegateExecution, worker, "self-final", command.DelegateInput{LeaseRef: stringMap(callback, "leaseRef"), Fence: stringMap(callback, "fence"), Generation: runtimeRevisionMapInt64(callback, "generation"), TargetAgentRef: coordinator.Ref, WorkflowStepKey: "self-finalize", Task: "Complete published final step."}, nil).Run
		complete("self-first-callback-complete", callback, true)
		complete("self-final-complete", claim("self-final-claim", self.Ref), true)
		finalCallback := claim("self-final-callback", child.Run.Ref)
		var snapshot struct {
			RemainingStepKeys []string `json:"remainingStepKeys"`
		}
		parts := strings.SplitN(stringMap(finalCallback, "task"), "\n\n", 2)
		if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &snapshot) != nil || len(snapshot.RemainingStepKeys) != 0 {
			t.Fatal("materialized coordinator step was proposed again")
		}
		complete("self-final-callback-complete", finalCallback, true)
		complete("self-parent-callback-complete", claim("self-parent-callback", parent.Ref), true)
		if read(child.Run.Ref).State != "SUCCEEDED" || read(parent.Ref).State != "SUCCEEDED" {
			t.Fatal("explicit coordinator step did not complete exactly once")
		}
	})
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
			capabilities, ok := childLease["capabilities"].([]string)
			if !ok || len(capabilities) != 1 || capabilities[0] != "platform.run.delegate" || len(runtimeRevisionGrants(childLease["integrationGrants"])) != 0 {
				t.Fatal("coordinator adopted capabilities outside published execution scope")
			}
			profiles, ok := childLease["managedMCPProfiles"].([]runtimecontract.ManagedMCPProfile)
			if !ok || len(profiles) != 0 {
				t.Fatal("coordinator adopted an excluded managed MCP profile")
			}
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
