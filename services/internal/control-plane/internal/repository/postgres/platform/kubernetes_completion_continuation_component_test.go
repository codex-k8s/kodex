package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesCompletionContinuationOwnerComponent proves exact child-approved terminal delivery after the origin turn ends.
// The target response is synthetic; approval, completion, replay, fresh continuation and result read use PostgreSQL owners.
// TestKubernetesCompletionContinuationOwnerComponent проверяет доставку результата согласованного дочернего действия после конца хода.
// Ответ цели синтетический; решение, завершение, повтор, новое продолжение и чтение проходят PostgreSQL-владельца.
func TestKubernetesCompletionContinuationOwnerComponent(t *testing.T) {
	for _, outcome := range []enum.ManagedKubernetesEffectOutcome{enum.KubernetesEffectSucceeded, enum.KubernetesEffectFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
			authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
			repository := fixture.repository
			seedObservedCatalogFixture(t, ctx, repository)
			execute := newComponentCommandExecutor(t, ctx, service, "kube-child-gate-")
			project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable child approval", Language: "ru"}).Project
			agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-child-gate-agent", "Child approval actor")
			connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.patch", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
			run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Apply a selected reviewed change"}).Run
			worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
			runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-child-runtime", Limit: 1}).RuntimeItems
			if len(runtime) != 1 || stringMap(runtime[0], "runRef") != run.Ref {
				t.Fatal("claim child runtime revision")
			}
			_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			gateway, err := repository.ResolvePrincipal(ctx, gateway)
			if err != nil {
				t.Fatal(err)
			}
			gateway.Permission = "platform.runtime.kubernetes.claim"
			broker := fixture.principal
			broker.CallerWorkload = "secret-broker"
			address := managedkubernetes.ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "review", Name: "web", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17", Namespaced: true}
			approver := fixture.transport
			// prepare uses actual Resolve/Claim/Prepare/Begin; only the target observation is synthetic.
			// prepare использует actual Resolve/Claim/Prepare/Begin; синтетична лишь target observation.
			prepare := func(key string) kubernetesChildGateAction {
				t.Helper()
				payload := map[string]any{"group": "apps", "version": "v1", "resource": "deployments", "namespace": "review", "name": "web", "uid": address.UID, "resourceVersion": "17", "body": []any{map[string]any{"op": "replace", "path": "/spec/replicas", "value": 0}}, "options": map[string]any{"patchType": "JSON"}}
				resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "kubernetes.patch", "idempotency_key": "kube-child-" + key}, payload)
				if err != nil || stringMap(resolved, "state") != "READY" {
					t.Fatal("resolve child without parent umbrella Gate", err, stringMap(resolved, "state"))
				}
				claims, err := repository.ClaimManagedKubernetesInvocations(ctx, gateway, "kube-child-gateway", 4)
				if err != nil {
					t.Fatal(err)
				}
				var action kubernetesChildGateAction
				for _, claim := range claims {
					if stringMap(claim, "invocationRef") == stringMap(resolved, "invocationRef") {
						action.binding = value.ManagedKubernetesBinding{WorkloadInstance: "kube-child-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
					}
				}
				if action.binding.InvocationRef == "" {
					t.Fatal("dedicated child claim missing")
				}
				broker.Permission = "platform.runtime.kubernetes.resolve"
				snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, action.binding)
				if err != nil {
					t.Fatal(err)
				}
				candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
				proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
				if err != nil {
					t.Fatal(err)
				}
				broker.Permission = "platform.runtime.kubernetes.children.prepare"
				plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, action.binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
				if err != nil || len(plan.Children) != 1 {
					t.Fatal("prepare child", err)
				}
				child := plan.Children[0]
				action.opening = command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
				broker.Permission = "platform.runtime.kubernetes.effect.begin"
				var waiting *platformservice.KubernetesApprovalRequiredError
				if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, action.binding, action.opening); !errors.As(err, &waiting) || waiting.ChildRef != child.Ref || waiting.PlanRef != plan.Ref {
					t.Fatal("open exact child Gate", err)
				}
				approver.Permission = "platform.query.owner-gates.get"
				action.gate, err = service.GetOwnerGate(ctx, approver, waiting.GateRef)
				if err != nil || action.gate.KubernetesChild == nil || !action.gate.KubernetesChild.Valid() {
					t.Fatal("read typed child Gate", err)
				}
				preview := action.gate.KubernetesChild.Preview
				if len(preview.Changes) != 1 || preview.Changes[0].Path != "/spec/replicas" || preview.Changes[0].ValueJSON != "0" || preview.Changes[0].Redacted {
					t.Fatal("approval hides submitted scale to zero")
				}
				return action
			}

			action := prepare("continuation")
			origin := runtime[0]
			runtimeReader := worker
			runtimeReader.Permission = "platform.runtime.kubernetes.invocation.read"
			proof := query.KubernetesRuntimeRead{InvocationRef: action.binding.InvocationRef, LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), RuntimeInputDigest: stringMap(origin, "inputDigest")}
			view, readErr := service.ReadKubernetesRuntimeInvocation(ctx, runtimeReader, proof)
			if readErr != nil || view.State != "RUNNING" || !view.WaitingForGate || view.EffectReceiptRef != "" {
				t.Fatal("runtime child gate result read", readErr)
			}
			for _, change := range []func(*query.KubernetesRuntimeRead){
				func(q *query.KubernetesRuntimeRead) { q.Fence += "wrong" },
				func(q *query.KubernetesRuntimeRead) { q.Generation++ },
				func(q *query.KubernetesRuntimeRead) { q.RuntimeInputDigest = strings.Repeat("f", 64) },
				func(q *query.KubernetesRuntimeRead) { q.InvocationRef = "inv_foreign_runtime" },
			} {
				bad := proof
				change(&bad)
				if _, err := service.ReadKubernetesRuntimeInvocation(ctx, runtimeReader, bad); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("runtime result accepted substituted proof", err)
				}
			}
			testKubernetesRuntimeExpiryDuringConnectionLock(t, ctx, repository, runtimeReader, proof, connection.Ref)
			execute(command.CompleteExecution, worker, "complete-origin", nil, command.CompleteExecutionInput{NativeSession: nativeCheckpointForClaimFixture(origin), LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, ResultSummary: "Waiting for exact child approval", Usage: turnUsageFixture()})
			if _, err := service.ReadKubernetesRuntimeInvocation(ctx, runtimeReader, proof); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("completed runtime retained result authority", err)
			}
			approved := execute(command.ResolveOwnerGate, approver, "approve", &action.gate.Version, command.GateResolutionInput{GateRef: action.gate.Ref, Decision: "APPROVE", Comment: "Only the selected child"}).Gate
			broker.Permission = "platform.runtime.kubernetes.effect.begin"
			effect, err := repository.BeginManagedKubernetesEffect(ctx, broker, action.binding, action.opening)
			if err != nil {
				t.Fatal("approved completed origin cannot begin", err)
			}
			raw := []byte(`{"metadata":{"name":"web"},"spec":{"replicas":0}}`)
			reason := ""
			want := "SUCCEEDED"
			if outcome == enum.KubernetesEffectFailed {
				raw = []byte(`{"reason":"VERSION_CONFLICT"}`)
				reason = managedkubernetes.ReasonConflict
				want = "FAILED"
			}
			sum := sha256.Sum256(raw)
			broker.Permission = "platform.runtime.kubernetes.effect.complete"
			if _, err = repository.CompleteManagedKubernetesEffect(ctx, broker, action.binding, command.ManagedKubernetesCompleteEffect{EffectRef: effect.Ref, EffectGeneration: effect.Generation, ChildInputDigest: action.opening.ChildInputDigest, Outcome: outcome, ResultJSON: raw, ResultDigest: hex.EncodeToString(sum[:]), ReasonCode: reason}); err != nil {
				t.Fatal("record child result", err)
			}
			_, _, completionCaller := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
				if method == cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName {
					completionCaller.Permission = operation
				}
			}
			finish := command.ManagedKubernetesCompleteInvocation{PlanRef: action.opening.PlanRef, PlanRevision: action.opening.PlanRevision, PlanDigest: action.opening.PlanDigest}
			result, err := service.CompleteManagedKubernetesInvocation(ctx, completionCaller, action.binding, finish)
			if err != nil || string(result.State) != want || !result.CompletionRecorded {
				t.Fatal("complete approved child", result.State, err)
			}
			replay, err := service.CompleteManagedKubernetesInvocation(ctx, completionCaller, action.binding, finish)
			if err != nil || replay != result {
				t.Fatal("lost completion ACK changed result", err)
			}
			actor := fixture.transport
			actor.Permission = "platform.query.run-graph.get"
			_, graph, err := service.GetRunGraph(ctx, actor, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, edge := range graph.Edges {
				if edge.Type == "CONTINUES" {
					count++
				}
			}
			if count != 1 {
				t.Fatal("completion/replay did not produce exactly one continuation", count)
			}
			resumed := execute(command.ClaimExecution, worker, "claim-result", nil, command.LeaseInput{WorkloadInstance: "kube-result-runtime", Limit: 1}).RuntimeItems
			if len(resumed) != 1 || stringMap(resumed[0], "sessionRef") != stringMap(origin, "sessionRef") || stringMap(resumed[0], "turnRef") == stringMap(origin, "turnRef") || stringMap(resumed[0], "runtimeRevisionRef") == stringMap(origin, "runtimeRevisionRef") {
				t.Fatal("result continuation lost session or fresh revision")
			}
			snapshot := decodeIntegrationContinuationTask(t, stringMap(resumed[0], "task"))
			if snapshot.State != want || snapshot.OwnerDecision.GateRef != action.gate.Ref || snapshot.OwnerDecision.GateVersion != approved.Version || snapshot.OwnerDecision.Decision != "APPROVE" || snapshot.OwnerDecision.Comment != "Only the selected child" {
				t.Fatal("result changed exact owner decision")
			}
			if want == "SUCCEEDED" && snapshot.ReceiptRef != result.ReceiptRef || want != "SUCCEEDED" && snapshot.ReceiptRef != "" {
				t.Fatal("completion evidence became false success proof")
			}
			reader := worker
			reader.Permission = "platform.runtime.execution.integration.outcomes.read"
			source := asJSON(snapshot)
			page, err := service.GetExecutionIntegrationOutcomes(ctx, reader, query.ExecutionIntegrationOutcomes{LeaseRef: stringMap(resumed[0], "leaseRef"), Fence: stringMap(resumed[0], "fence"), Generation: runtimeRevisionMapInt64(resumed[0], "generation"), SourceInvocationRef: action.binding.InvocationRef, SourceSHA256: workflowResultSHA(source), MaximumBytes: 4096})
			if err != nil || !page.EOF || page.Text != string(source) {
				t.Fatal("result reader lost exact terminal outcome", err)
			}
			actor.Permission = "platform.query.owner-gates.get"
			historical, err := service.GetOwnerGate(ctx, actor, action.gate.Ref)
			if err != nil || historical.Version != approved.Version || historical.State != "APPROVED" {
				t.Fatal("completion rewrote historical approval", err)
			}
		})
	}
}
