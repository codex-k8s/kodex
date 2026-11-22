package platform

import (
	"context"
	_ "embed"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryIntegrationPendingRootTestOrigin reads the server-owned root identifier from the original public run and tenant only for the disposable observer, never from a redacted claim or caller root identifier.
// queryIntegrationPendingRootTestOrigin читает server-owned root id по original public run и tenant лишь для disposable observer, не из redacted claim либо caller root id.
//
//go:embed testdata/sql/integration_pending_root__origin.sql
var queryIntegrationPendingRootTestOrigin string

// TestIntegrationPendingRootOwnerComponent completes a real origin while canonical KUBE/ordinary TOKEN work is READY or RUNNING, proving neither an approved Gate nor an ungated operation can prematurely finish the root. Public cancellation closes the same graph without provider effects or invented success.
// TestIntegrationPendingRootOwnerComponent завершает real origin при canonical KUBE/ordinary TOKEN work в READY либо RUNNING: approved Gate либо ungated operation не завершают root преждевременно. Public cancel закрывает тот же graph без provider effects и вымышленного success.
func TestIntegrationPendingRootOwnerComponent(t *testing.T) {
	for _, purpose := range []string{"kubernetes", "token"} {
		for _, running := range []bool{false, true} {
			phase := "READY"
			if running {
				phase = "RUNNING"
			}
			t.Run(purpose+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
				defer cancel()
				var repository *Repository
				var service *platformservice.Service
				var actor value.Principal
				var connection entity.IntegrationConnection
				capability, policy := "github.issue.create", "NONE"
				input := map[string]any{"title": "Synthetic pending work"}
				var paths []string
				if purpose == "kubernetes" {
					fixture, ownerService, kubeConnection, binding := kubernetesPersonalHealthFixture(t, ctx)
					authenticateKubernetesInvocationFixture(t, ctx, fixture, ownerService, kubeConnection, binding)
					repository, service, actor, connection = fixture.repository, ownerService, fixture.transport, kubeConnection
					capability, policy = "kubernetes.patch", "HUMAN_SCOPED"
					paths = []string{"/group", "/version", "/resource", "/namespace", "/name", "/uid"}
					input = map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web",
						"uid": "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", "resourceVersion": "17",
						"body":    map[string]any{"metadata": map[string]any{"labels": map[string]any{"review": "true"}}},
						"options": map[string]any{"patchType": "MERGE"}}
				} else {
					pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
					if err != nil {
						t.Fatal("open isolated pending TOKEN fixture", err)
					}
					defer pool.Close()
					repository = newSyntheticRuntimeComponentRepository(t, ctx, pool)
					service, err = platformservice.New(repository)
					if err != nil {
						t.Fatal("construct pending TOKEN fixture", err)
					}
					actor = resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
					created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: actor,
						Mutation: value.Mutation{IdempotencyKey: "pending-token-create"}, Payload: command.ConnectionInput{
							DefinitionKey: "github", Name: "Synthetic pending TOKEN", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}})
					if err != nil || created.Connection == nil {
						t.Fatal("create public pending TOKEN Connection", err)
					}
					connection = *created.Connection
					if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
						t.Fatal("prepare synthetic TOKEN descriptor", err)
					}
					reader := actor
					reader.Permission = "platform.query.integrations.get"
					connection, err = service.GetIntegrationConnection(ctx, reader, connection.Ref)
					if err != nil {
						t.Fatal("read current TOKEN source version", err)
					}
				}
				seedObservedCatalogFixture(t, ctx, repository)
				execute := newComponentCommandExecutor(t, ctx, service, "pending-root-")
				project := execute(command.CreateProject, actor, "project", nil, command.ProjectInput{Name: "Pending integration root", Language: "ru"}).Project
				agent := createLifecycleAgent(t, ctx, service, actor, project.Ref, "pending-root-agent", "Pending integration owner")
				grant := command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref,
					CapabilityKey: capability, ApprovalPolicy: policy, ApprovalScopePaths: paths, Enabled: true}
				if purpose == "kubernetes" {
					grant.KubernetesNamespace = "review"
				}
				connection = *execute(command.ChangeIntegrationGrant, actor, "grant", &connection.Version, grant).Connection
				run := execute(command.LaunchRun, actor, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
					Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Await the admitted asynchronous operation"}).Run
				worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
				leases := execute(command.ClaimExecution, worker, "origin", nil, command.LeaseInput{WorkloadInstance: "pending-origin", Limit: 1}).RuntimeItems
				if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
					t.Fatal("claim exact pending origin")
				}
				origin := leases[0]
				invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref,
					"node_ref": stringMap(origin, "nodeRef"), "connection_ref": connection.Ref, "capability_key": capability,
					"idempotency_key": "pending-canonical-invocation"}, input)
				if err != nil {
					t.Fatal("resolve canonical pending invocation", err)
				}
				if gateRef := stringMap(invocation, "gateRef"); gateRef != "" {
					reader := actor
					reader.Permission = "platform.query.owner-gates.get"
					gate, err := service.GetOwnerGate(ctx, reader, gateRef)
					if err != nil {
						t.Fatal("read pending origin Gate", err)
					}
					execute(command.ResolveOwnerGate, actor, "approve-before-complete", &gate.Version, command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE"})
				}
				if running {
					_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
					capacity := value.IntegrationInvocationDeliveryCapacity{Ordinary: 1}
					if purpose == "kubernetes" {
						capacity = value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1}
					}
					claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, gateway, "pending-gateway", 1, capacity)
					if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(invocation, "invocationRef") {
						t.Fatal("claim exact pending operation", err)
					}
				}
				execute(command.CompleteExecution, worker, "complete-origin", nil, command.CompleteExecutionInput{
					NativeSession: nativeCheckpointForClaimFixture(origin), LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"),
					Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, ResultSummary: "Admitted operation is still pending", Usage: turnUsageFixture()})
				reader := actor
				reader.Permission = "platform.query.runs.get"
				current, err := service.GetRun(ctx, reader, run.Ref)
				if err != nil || current.State != "RUNNING" {
					t.Fatal("pending canonical operation prematurely completed its root", err)
				}
				cancelled := execute(command.CancelRun, actor, "cancel-owned-graph", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Close disposable pending integration"}).Run
				if cancelled == nil || cancelled.State != "CANCELLED" {
					t.Fatal("public cancel did not close pending root")
				}
				var active, planned int
				resolvedReader, err := repository.ResolvePrincipal(ctx, reader)
				if err != nil {
					t.Fatal("resolve original pending root principal", err)
				}
				currentScope, err := repository.resolveScope(ctx, resolvedReader)
				if err != nil {
					t.Fatal("resolve original pending root observer", err)
				}
				var rootID string
				if err := repository.pool.QueryRow(ctx, queryIntegrationPendingRootTestOrigin, pgx.StrictNamedArgs{
					"organization_id": currentScope.organizationID, "run_ref": run.Ref}).Scan(&rootID); err != nil {
					t.Fatal("read exact original root for pending observer", err)
				}
				if err := repository.pool.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType, rootID).Scan(&active, &planned); err != nil || active != 0 {
					t.Fatal("terminal cancellation remained counted as pending work", err)
				}
			})
		}
	}
}
