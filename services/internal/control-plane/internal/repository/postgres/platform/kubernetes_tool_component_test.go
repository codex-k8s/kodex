package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryKubernetesToolStopTestEvidence reads durable receipt, audit and closure facts in the disposable owner test only.
// queryKubernetesToolStopTestEvidence читает сохранённые receipt, audit и закрытие только в одноразовой owner-проверке.
//
//go:embed testdata/sql/kubernetes_tool__stop_test_evidence.sql
var queryKubernetesToolStopTestEvidence string

// TestKubernetesToolOwnerComponent proves public pre-stream closure through real owner launch/grant/claim/Gate transactions.
// Native resources are synthetic; this is PostgreSQL lifecycle evidence, not deployed native termination.
// TestKubernetesToolOwnerComponent проверяет публичное закрытие до потока через настоящие owner launch/grant/claim/Gate.
// Внешние ресурсы синтетические; это PostgreSQL lifecycle, не проверка остановки удалённого процесса.
func TestKubernetesToolOwnerComponent(t *testing.T) {
	for _, scenario := range []string{"ready", "parent_gate", "child_gate", "attempting", "parent_gate_completed", "child_gate_completed", "attempting_completed", "parent_gate_approved_completed", "child_gate_approved_completed"} {
		t.Run(scenario, func(t *testing.T) {
			completed := strings.HasSuffix(scenario, "_completed")
			phase := strings.TrimSuffix(strings.TrimSuffix(scenario, "_completed"), "_approved")
			approved := strings.Contains(scenario, "_approved_")
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			fixture, service, connection, bindingMetadata := kubernetesPersonalHealthFixture(t, ctx)
			authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, bindingMetadata)
			repository := fixture.repository
			seedObservedCatalogFixture(t, ctx, repository)
			execute := newComponentCommandExecutor(t, ctx, service, "kube-tool-stop-")
			project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable private input", Language: "ru"}).Project
			agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-input-agent", "Private input owner")
			capability, policy := "kubernetes.exec", "HUMAN_EACH_EFFECT"
			var paths []string
			if phase == "parent_gate" {
				capability, policy = "kubernetes.patch", "HUMAN_SCOPED"
				paths = []string{"/group", "/version", "/resource", "/namespace", "/name", "/uid"}
			}
			connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: capability, ApprovalPolicy: policy, ApprovalScopePaths: paths, Enabled: true, KubernetesNamespace: "review"}).Connection
			run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Write a reviewed private stdin frame"}).Run
			worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
			runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-input-runtime", Limit: 1}).RuntimeItems
			if len(runtime) != 1 {
				t.Fatal("claim input runtime")
			}
			node := stringMap(runtime[0], "nodeRef")
			uid := "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
			payload := map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "uid": uid, "resourceVersion": "17", "options": map[string]any{"container": "main", "command": []string{"cat"}, "stdin": true, "tty": true, "timeoutSeconds": 120}}
			if phase == "parent_gate" {
				payload["body"] = map[string]any{"metadata": map[string]any{"labels": map[string]any{"review": "true"}}}
				payload["options"] = map[string]any{"patchType": "MERGE"}
			}
			resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": node, "connection_ref": connection.Ref, "capability_key": capability, "idempotency_key": "kube-input-invocation"}, payload)
			if err != nil {
				t.Fatal("resolve input invocation", err)
			}

			inv := stringMap(resolved, "invocationRef")
			actor := fixture.transport
			var binding value.ManagedKubernetesBinding
			var broker value.Principal
			var opening command.ManagedKubernetesBeginEffect
			gateRef := stringMap(resolved, "gateRef")
			if phase == "child_gate" || phase == "attempting" {
				_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
				for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
					if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
						gateway.Permission = operation
					}
				}
				claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, gateway, "kube-input-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
				if err != nil || len(claims) != 1 {
					t.Fatal("claim input invocation", err)
				}
				claim := claims[0]
				binding = value.ManagedKubernetesBinding{WorkloadInstance: "kube-input-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
				if binding.InvocationRef != stringMap(resolved, "invocationRef") {
					t.Fatal("claim selected another input invocation")
				}
				broker = fixture.principal
				broker.CallerWorkload = "secret-broker"
				broker.Permission = "platform.runtime.kubernetes.resolve"
				snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
				if err != nil {
					t.Fatal(err)
				}
				candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: uid, ResourceVersion: "17", Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "synthetic-node", Container: "main"}}
				proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
				if err != nil {
					t.Fatal(err)
				}
				broker.Permission = "platform.runtime.kubernetes.children.prepare"
				plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
				if err != nil || len(plan.Children) != 1 {
					t.Fatal("prepare input child", err)
				}
				child := plan.Children[0]
				opening = command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
				broker.Permission = "platform.runtime.kubernetes.effect.begin"

				var waiting *platformservice.KubernetesApprovalRequiredError
				if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.As(err, &waiting) {
					t.Fatal("missing child Gate", err)
				}
				gateRef = waiting.GateRef
				if phase == "attempting" {
					actor.Permission = "platform.query.owner-gates.get"
					gate, err := service.GetOwnerGate(ctx, actor, gateRef)
					if err != nil {
						t.Fatal(err)
					}
					execute(command.ResolveOwnerGate, fixture.transport, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
					if admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); err != nil || admission.Stream == nil {
						t.Fatal("missing stream admission", err)
					}
				}
			}
			if completed {
				origin := runtime[0]
				execute(command.CompleteExecution, worker, "complete-origin", nil, command.CompleteExecutionInput{NativeSession: nativeCheckpointForClaimFixture(origin), LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, ResultSummary: "Waiting for owner", Usage: turnUsageFixture()})
			}
			if completed && phase == "child_gate" {
				_, _, gatewayRenew := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
				for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
					if method == cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName {
						gatewayRenew.Permission = operation
					}
				}
				if renewed, err := service.RenewManagedKubernetesInvocation(ctx, gatewayRenew, binding); err != nil || renewed.Ref != binding.LeaseRef || renewed.Generation != binding.Generation {
					t.Fatal("completed origin lost its existing pending-child claim", err)
				}
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				var waiting *platformservice.KubernetesApprovalRequiredError
				if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.As(err, &waiting) || waiting.GateRef != gateRef {
					t.Fatal("pending-child eligibility bypassed the exact Gate", err)
				}
			}
			var approvedVersion int64
			if approved {
				actor.Permission = "platform.query.owner-gates.get"
				gate, err := service.GetOwnerGate(ctx, actor, gateRef)
				if err != nil {
					t.Fatal(err)
				}
				approval := command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Original approval must remain historical"}
				if phase == "parent_gate" {
					approval.IntegrationApproval = &command.IntegrationApprovalSelection{DurationSeconds: 600, MaxEffects: 1, Lifetime: "ROOT_RUN"}
				}
				approvedVersion = execute(command.ResolveOwnerGate, fixture.transport, "approve-before-stop", &gate.Version, approval).Gate.Version
			}
			actor.Permission = "platform.integrations.kubernetes.tool.list"
			page, err := service.ListKubernetesTools(ctx, actor, query.KubernetesToolList{RunRef: run.Ref, NodeRef: node})
			if err != nil || len(page.Items) != 1 || page.Items[0].InvocationRef != inv {
				t.Fatal("missing exact pending tool", err)
			}
			actor.Permission = "platform.integrations.kubernetes.tool.get"
			view, err := service.GetKubernetesTool(ctx, actor, inv)
			if err != nil {
				t.Fatal("read public tool", err)
			}
			if view.WaitingForGate != ((phase == "parent_gate" || phase == "child_gate") && !approved) {
				t.Fatal("incorrect pending Gate phase")
			}
			if phase == "parent_gate" && !approved && view.State != "WAITING_APPROVAL" {
				t.Fatal("fixture did not reach parent approval")
			}
			actor.Permission = "platform.integrations.kubernetes.tool.stop"
			badVersion := view.Version + 1
			if _, err := service.StopKubernetesTool(ctx, actor, value.Mutation{IdempotencyKey: "tool-stop-bad-version", ExpectedVersion: &badVersion}, inv); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatal("stale Stop accepted", err)
			}
			mutation := value.Mutation{IdempotencyKey: "tool-stop-once", ExpectedVersion: &view.Version}
			foreign := actor
			foreign.ProjectRef = "prj_foreignsynthetic"
			if _, err := service.StopKubernetesTool(ctx, foreign, mutation, inv); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("foreign project Stop disclosed state", err)
			}
			stopped, err := service.StopKubernetesTool(ctx, actor, mutation, inv)
			if err != nil {
				t.Fatal("public tool Stop", err)
			}
			expected := "CANCELLED"
			if phase == "attempting" {
				expected = "UNKNOWN_OUTCOME"
			}
			if string(stopped.State) != expected || stopped.WaitingForGate || stopped.Version <= view.Version {
				t.Fatal("incorrect closure metadata", stopped.State)
			}
			replay, err := service.StopKubernetesTool(ctx, actor, mutation, inv)
			if err != nil || replay.Version != stopped.Version || replay.State != stopped.State {
				t.Fatal("lost Stop ACK changed closure", err)
			}
			current := fixture.current
			var audits, receipts int
			var closed bool
			if err := repository.pool.QueryRow(ctx, queryKubernetesToolStopTestEvidence, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": inv}).Scan(&audits, &receipts, &closed); err != nil || audits != 1 || receipts != 1 || !closed {
				t.Fatal("Stop receipt/audit/lease closure mismatch", err, audits, receipts, closed)
			}
			reused := mutation
			reused.ExpectedVersion = &stopped.Version
			if _, err := service.StopKubernetesTool(ctx, actor, reused, inv); !errors.Is(err, errs.ErrIdempotencyReuse) {
				t.Fatal("Stop key reused with new intent", err)
			}
			if gateRef != "" && phase != "attempting" {
				actor.Permission = "platform.query.owner-gates.get"
				gate, err := service.GetOwnerGate(ctx, actor, gateRef)
				wantState := "CANCELLED"
				if approved {
					wantState = "APPROVED"
				}
				if err != nil || gate.State != wantState || approved && (gate.Version != approvedVersion || gate.DecisionComment != "Original approval must remain historical") {
					t.Fatal("Stop changed historical approval or retained an open Gate", err)
				}
			}
			if phase == "child_gate" || phase == "attempting" {
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); err == nil {
					t.Fatal("Begin accepted after public closure")
				}
			}
			actor.Permission = "platform.query.runs.get"
			currentRun, err := service.GetRun(ctx, actor, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if currentRun.State == "CANCELLED" || currentRun.State == "WAITING_HUMAN" {
				t.Fatal("Stop cancelled run or retained its only Gate")
			}
			actor.Permission = "platform.query.run-graph.get"
			_, graph, err := service.GetRunGraph(ctx, actor, run.Ref)
			if err != nil {
				t.Fatal("read continuation graph", err)
			}
			continuations := 0
			for _, edge := range graph.Edges {
				if edge.Type == "CONTINUES" {
					continuations++
				}
			}
			wantContinuations := 0
			if completed && phase != "attempting" {
				wantContinuations = 1
			}
			if continuations != wantContinuations {
				t.Fatalf("Stop/replay continuation count: got %d want %d", continuations, wantContinuations)
			}
			if wantContinuations == 1 {
				resumed := execute(command.ClaimExecution, worker, "claim-continuation", nil, command.LeaseInput{WorkloadInstance: "kube-stop-continuation", Limit: 1}).RuntimeItems
				if len(resumed) != 1 || stringMap(resumed[0], "sessionRef") != stringMap(runtime[0], "sessionRef") || stringMap(resumed[0], "turnRef") == stringMap(runtime[0], "turnRef") || stringMap(resumed[0], "runtimeRevisionRef") == stringMap(runtime[0], "runtimeRevisionRef") {
					t.Fatal("Stop continuation lost exact session or fresh turn/revision")
				}
				snapshot := decodeIntegrationContinuationTask(t, stringMap(resumed[0], "task"))
				decision := "CANCEL"
				if approved {
					decision = "APPROVE"
				}
				if snapshot.State != "CANCELLED" || snapshot.OwnerDecision.GateRef != gateRef || snapshot.OwnerDecision.Decision != decision {
					t.Fatal("continuation changed exact historical decision or terminal outcome")
				}
				reader := worker
				reader.Permission = "platform.runtime.execution.integration.outcomes.read"
				source := asJSON(snapshot)
				page, err := service.GetExecutionIntegrationOutcomes(ctx, reader, query.ExecutionIntegrationOutcomes{LeaseRef: stringMap(resumed[0], "leaseRef"), Fence: stringMap(resumed[0], "fence"), Generation: runtimeRevisionMapInt64(resumed[0], "generation"), SourceInvocationRef: inv, SourceSHA256: workflowResultSHA(source), MaximumBytes: 4096})
				if err != nil || !page.EOF || page.Text != string(source) {
					t.Fatal("canonical outcome reader lost exact cancelled result", err)
				}
			}
			actor.Permission = "platform.integrations.kubernetes.tool.get"
			read, err := service.GetKubernetesTool(ctx, actor, inv)
			if err != nil || read.Version != stopped.Version {
				t.Fatal("authoritative Stop readback", err)
			}
		})
	}
}
