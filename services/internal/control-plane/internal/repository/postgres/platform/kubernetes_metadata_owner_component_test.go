package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesMetadataOwnerComponent exercises real grants, runtime claims, addressless plan persistence, RPC decoding and effect receipts in disposable PostgreSQL; it does not replace native TLS or installed acceptance.
// TestKubernetesMetadataOwnerComponent проверяет настоящие grants, runtime claims, сохранение безадресного plan, RPC decoding и receipts в disposable PostgreSQL; не заменяет native TLS и установленную приёмку.
func TestKubernetesMetadataOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-metadata-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable Kubernetes metadata", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-metadata-agent", "Kubernetes metadata reader")
	operations := []string{"kubernetes.version", "kubernetes.identity", "kubernetes.help", "kubernetes.config.metadata"}
	for _, operation := range operations {
		connection = *execute(command.ChangeIntegrationGrant, fixture.transport, operation, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "@cluster"}).Connection
	}
	restoreOwner := limitKubernetesInitiatorToExecutionPermissions(t, ctx, fixture, service, connection.Ref)
	defer restoreOwner()
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Read selected disposable cluster metadata"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-metadata-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 || stringMap(runtime[0], "runRef") != run.Ref {
		t.Fatal("claim actual metadata runtime revision")
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
			gateway.Permission = operation
		}
	}
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": operation}, map[string]any{})
			if err != nil || stringMap(resolved, "state") != "READY" {
				t.Fatal("resolve original metadata invocation", err)
			}
			claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, gateway, "kube-metadata-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
			if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(resolved, "invocationRef") {
				t.Fatal("claim exact metadata invocation", err)
			}
			claim := claims[0]
			lease := value.ManagedKubernetesBinding{WorkloadInstance: "kube-metadata-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
			broker.Permission = "platform.runtime.kubernetes.resolve"
			snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, lease)
			if err != nil || snapshot.Resource != (value.ManagedKubernetesResourceAddress{}) || snapshot.Discovery != nil {
				t.Fatal("resolve addressless metadata snapshot", err)
			}
			proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, nil)
			if err != nil {
				t.Fatal(err)
			}
			prepare := command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal}
			broker.Permission = "platform.runtime.kubernetes.children.prepare"
			plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, lease, prepare)
			if err != nil || len(plan.Children) != 1 || plan.Children[0].Observation != nil || plan.Children[0].Resource != (managedkubernetes.ResourceAddress{}) || plan.Children[0].Operation != operation {
				t.Fatal("persist addressless owner child", err)
			}
			assertKubernetesOwnerPlanWire(t, ctx, plan)
			recovered, err := repository.PrepareManagedKubernetesChildren(ctx, broker, lease, prepare)
			if err != nil || recovered.Ref != plan.Ref || recovered.Digest != plan.Digest || recovered.Children[0].Observation != nil {
				t.Fatal("readback changed immutable addressless plan", err)
			}
			assertKubernetesOwnerPlanWire(t, ctx, recovered)
			child := plan.Children[0]
			opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
			broker.Permission = "platform.runtime.kubernetes.effect.begin"
			admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening)
			if err != nil || admission.ReconcileOnly || admission.Generation != 1 || admission.Stream != nil {
				t.Fatal("admit exact metadata effect", err)
			}
			raw := []byte(`{"syntheticMetadata":true}`)
			digest := sha256.Sum256(raw)
			completion := command.ManagedKubernetesCompleteEffect{EffectRef: admission.Ref, EffectGeneration: admission.Generation, ChildInputDigest: child.InputDigest, Outcome: enum.KubernetesEffectSucceeded, ResultJSON: raw, ResultDigest: hex.EncodeToString(digest[:])}
			broker.Permission = "platform.runtime.kubernetes.effect.complete"
			receipt, err := repository.CompleteManagedKubernetesEffect(ctx, broker, lease, completion)
			if err != nil || !receipt.Applied || receipt.ResultDigest != completion.ResultDigest {
				t.Fatal("accept metadata receipt", err)
			}
			broker.Permission = "platform.runtime.kubernetes.effect.begin"
			cached, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening)
			if err != nil || !cached.ReconcileOnly || cached.AcceptedResult == nil || string(cached.AcceptedResult.ResultJSON) != string(raw) {
				t.Fatal("metadata result recovery replayed effect or lost original bytes", err)
			}
		})
	}
}
