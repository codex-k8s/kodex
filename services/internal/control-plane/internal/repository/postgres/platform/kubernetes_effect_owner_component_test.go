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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	ownergrpc "github.com/codex-k8s/kodex/services/internal/control-plane/internal/transport/grpc"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestKubernetesEffectOwnerComponent exercises actual launch, grants, claims and all direct effect
// owner transitions without target I/O. It proves storage/admission semantics, not target TLS/RBAC,
// signed policy108, public child Gate decisions, composite expansion or installed runtime readiness.
// TestKubernetesEffectOwnerComponent проверяет actual launch/grants/claims и direct effect owner
// transitions без target I/O. Доказывает storage/admission, не target TLS/RBAC, signed policy108,
// public child Gate decisions, composite expansion либо installed runtime readiness.
func TestKubernetesEffectOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-effect-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable Kubernetes effects", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-effect-agent", "Kubernetes effect reader")
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "get-grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.get", ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "review"}).Connection
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "replace-grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.replace", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "logs-grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.logs", ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "review"}).Connection
	restoreOwner := limitKubernetesInitiatorToExecutionPermissions(t, ctx, fixture, service, connection.Ref)
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Observe one selected disposable ConfigMap"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-effect-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 || stringMap(runtime[0], "runRef") != run.Ref {
		t.Fatal("claim actual effect runtime revision")
	}
	var writePinned bool
	if repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(coalesce(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
      JOIN control_plane.run_nodes node ON node.ref=$1
      WHERE revision.node_id=node.id AND pin->>'connectionRef'=$2 AND pin->>'capabilityKey'='kubernetes.replace'
      AND revision.generation=(SELECT max(current.generation) FROM control_plane.runtime_revisions current WHERE current.node_id=node.id))`, stringMap(runtime[0], "nodeRef"), connection.Ref).Scan(&writePinned) != nil || !writePinned {
		t.Fatal("view-only initiator RuntimeRevision dropped assigned Kubernetes WRITE grant")
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	resolvedGateway, err := repository.ResolvePrincipal(ctx, gateway)
	if err != nil {
		t.Fatal(err)
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.claim"
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "configmaps", Namespace: "review", Name: "settings", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17", Namespaced: true}
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
	// claimEffect keeps only actual owner-assigned invocation and claim metadata in the fixture.
	// claimEffect хранит только actual owner-assigned invocation/claim metadata в fixture.
	claimEffect := func(operation, key string, payload map[string]any) value.ManagedKubernetesBinding {
		resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": key}, payload)
		if err != nil || stringMap(resolved, "state") != "READY" {
			t.Fatal("resolve effect without an umbrella Gate", operation, err, stringMap(resolved, "state"))
		}
		capacityCaller := gateway
		for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
			if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
				capacityCaller.Permission = operation
			}
		}
		zero, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, capacityCaller, "kube-effect-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Ordinary: 1})
		if err != nil || len(zero) != 0 {
			t.Fatal("zero Kubernetes slots committed a lease", err)
		}
		if _, err = service.ClaimIntegrationInvocationsWithCapacity(ctx, capacityCaller, "kube-effect-gateway", 2, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1, SSH: 1}); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("unready SSH capacity was silently ignored", err)
		}
		var untouched bool
		if repository.pool.QueryRow(ctx, "SELECT state='READY' AND generation=0 AND lease_ref IS NULL FROM control_plane.integration_invocations WHERE ref=$1", stringMap(resolved, "invocationRef")).Scan(&untouched) != nil || !untouched {
			t.Fatal("excluded or unsupported capacity changed original intent")
		}

		claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, capacityCaller, "kube-effect-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
		if err != nil {
			t.Fatal(err)
		}
		for _, claim := range claims {
			if stringMap(claim, "invocationRef") == stringMap(resolved, "invocationRef") {
				mapped := ownergrpc.CastIntegrationInvocationClaim(claim)
				if mapped.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY || mapped.GetBrokerIdentityPurpose() != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET || mapped.GetBrokerIdentityRef() != binding.BrokerIdentity.Ref || mapped.GetBrokerIdentityRevision() != binding.BrokerIdentity.Revision || mapped.GetCredentialRevision() != nil {
					t.Fatal("committed Runtime claim lost exact BI metadata")
				}

				return value.ManagedKubernetesBinding{WorkloadInstance: "kube-effect-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
			}
		}
		t.Fatal("effect was not discovered by dedicated claim", operation)
		return value.ManagedKubernetesBinding{}
	}
	input := map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": "review", "name": "settings"}
	lease := claimEffect("kubernetes.get", "kube-effect-read", input)
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, lease)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	prepare := command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, lease, prepare)
	if err != nil || len(plan.Children) != 1 || plan.Children[0].Observation == nil || *plan.Children[0].Observation != candidate {
		t.Fatal("persist original child observation", err)
	}
	assertKubernetesOwnerPlanWire(t, ctx, plan)
	prepare.Candidates = nil
	recovered, err := repository.PrepareManagedKubernetesChildren(ctx, broker, lease, prepare)
	if err != nil || recovered.Ref != plan.Ref || recovered.Digest != plan.Digest || string(recovered.Children[0].NormalizedInputJSON) != string(snapshot.NormalizedInputJSON) {
		t.Fatal("stored plan recovery changed source or input", err)
	}
	assertKubernetesPlanObservationIntegrity(t, ctx, fixture, broker, lease, prepare, plan.Children[0].Ref, candidate)
	child := plan.Children[0]
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	first, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening)
	if err != nil || first.ReconcileOnly || first.Generation != 1 || first.Stream != nil {
		t.Fatal("admit exact first resource attempt", err)
	}
	repeated, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening)
	if err != nil || !repeated.ReconcileOnly || repeated.Ref != first.Ref || repeated.AcceptedResult != nil {
		t.Fatal("uncertain attempt was replayable", err)
	}
	raw := []byte(`{"metadata":{"name":"settings"}}`)
	digest := sha256.Sum256(raw)
	completion := command.ManagedKubernetesCompleteEffect{EffectRef: first.Ref, EffectGeneration: 1, ChildInputDigest: child.InputDigest, Outcome: enum.KubernetesEffectSucceeded, ResultJSON: raw, ResultDigest: hex.EncodeToString(digest[:])}
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	broken, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, queryKubernetesCompletionRecord, pgtype.TimestamptzOID)
	if _, err := broken.CompleteManagedKubernetesEffect(ctx, broker, lease, completion); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
		t.Fatal("completion Scan failure became authority loss", err)
	}
	accepted, err := repository.CompleteManagedKubernetesEffect(ctx, broker, lease, completion)
	if err != nil || !accepted.Applied || accepted.State != "SUCCEEDED" || accepted.SubmissionDigest != completion.ResultDigest || accepted.ResultDigest != completion.ResultDigest || !accepted.AuthorizedUntil.After(time.Now()) {
		t.Fatal("accept original safe result", err)
	}
	if receipt, err := repository.CompleteManagedKubernetesEffect(ctx, broker, lease, completion); err != nil || !receipt.Applied {
		t.Fatal("exact receipt replay failed", err)
	}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	cached, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening)
	if err != nil || !cached.ReconcileOnly || cached.AcceptedResult == nil || string(cached.AcceptedResult.ResultJSON) != string(raw) {
		t.Fatal("accepted recovery lost safe original result", err)
	}
	changed := completion
	changed.Outcome = enum.KubernetesEffectUnknown
	changed.ReasonCode = managedkubernetes.ReasonTransportUnknown
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	if _, err := repository.CompleteManagedKubernetesEffect(ctx, broker, lease, changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("receipt replay changed original outcome", err)
	}
	outerRead := assertKubernetesOuterCompletion(t, ctx, repository, service, gateway, broker, claimEffect, candidate)
	restoreOwner()
	var getGrant string
	for _, grant := range connection.Grants {
		if grant.CapabilityKey == "kubernetes.get" && grant.TargetRef == agent.Ref {
			getGrant = grant.Ref
		}
	}
	if getGrant == "" {
		t.Fatal("exact read grant locator missing")
	}
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "revoke-read", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.get", ApprovalPolicy: "NONE", Enabled: false, KubernetesNamespace: "review", KubernetesGrantRef: getGrant}).Connection
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, lease, opening); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("revoked grant released cached read result", err)
	}
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	if _, err := repository.CompleteManagedKubernetesEffect(ctx, broker, lease, completion); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("revocation rewrote accepted receipt as discarded output", err)
	}
	assertKubernetesTerminalReadbackAfterRevocation(t, ctx, service, gateway, outerRead)
	for _, mode := range []string{"HANDSHAKE_FAILED", "LOST_OPEN_ACK", "PREPARED_OWNER_LOST", "ACTIVE_OWNER_LOST"} {
		assertKubernetesPreparedStreamFailure(t, ctx, repository, broker, claimEffect, mode)
	}
	// pendingEach proves per-child Gate waits under the original lease with zero external attempts.
	// pendingEach проверяет per-child Gate под original lease без external attempts.
	replacement := map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": "review", "name": "settings", "uid": address.UID, "resourceVersion": address.ResourceVersion, "body": map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "review", "uid": address.UID, "resourceVersion": address.ResourceVersion}, "data": map[string]any{"mode": "safe"}}}
	each := claimEffect("kubernetes.replace", "kube-effect-replace", replacement)
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err = repository.ResolveManagedKubernetesOperation(ctx, broker, each)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err = managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err = repository.PrepareManagedKubernetesChildren(ctx, broker, each, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
	if err != nil {
		t.Fatal(err)
	}
	opening = command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: 1, PlanDigest: plan.Digest, ChildRef: plan.Children[0].Ref, ChildInputDigest: plan.Children[0].InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	var waiting *platformservice.KubernetesApprovalRequiredError
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, each, opening); !errors.As(err, &waiting) || waiting.ChildRef != opening.ChildRef {
		t.Fatal("EACH did not wait for exact child Gate", err)
	}
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, each, opening); !errors.As(err, &waiting) {
		t.Fatal("pending child Gate was not recoverable under same lease", err)
	}
	if _, readErr := repository.GetIntegrationInvocation(ctx, resolvedGateway, each.InvocationRef); !errors.Is(readErr, errs.ErrNotFound) {
		t.Fatal("organization locator exposed Kubernetes state/result", readErr)
	}
	assertKubernetesOwnerUnrecordedClosure(t, ctx, service, gateway, each, enum.KubernetesInvocationRunning)
	var effects, gates int
	if repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.kubernetes_execution_effects effect JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id WHERE child.ref=$1`, opening.ChildRef).Scan(&effects) != nil || effects != 0 {
		t.Fatal("pending Gate allocated an external attempt")
	}
	if repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.owner_gates gate JOIN control_plane.kubernetes_execution_children child ON child.id=gate.kubernetes_child_id WHERE child.ref=$1`, opening.ChildRef).Scan(&gates) != nil || gates != 1 {
		t.Fatal("pending Gate duplicated immutable child approval")
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.renew"
	if renewed, err := repository.RenewManagedKubernetesInvocation(ctx, resolvedGateway, each); err != nil || renewed.Ref != each.LeaseRef || renewed.Generation != each.Generation {
		t.Fatal("pending Gate lost original lease renewal", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, each.InvocationRef); err != nil {
		t.Fatal(err)
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.claim"
	if claims, err := repository.ClaimManagedKubernetesInvocations(ctx, resolvedGateway, "kube-effect-after-expiry", 1); err != nil || len(claims) != 0 {
		t.Fatal("expired Gate reissued an external attempt", err)
	}
	var state string
	if repository.pool.QueryRow(ctx, `SELECT state FROM control_plane.integration_invocations WHERE ref=$1`, each.InvocationRef).Scan(&state) != nil || state != "FAILED" {
		t.Fatal("no-attempt expiry invented an unknown external effect", state)
	}
	assertKubernetesOwnerUnrecordedClosure(t, ctx, service, gateway, each, enum.KubernetesInvocationFailed)
	assertKubernetesWorkerCancellation(t, ctx, repository, service, gateway, broker, claimEffect, replacement, candidate)
}
