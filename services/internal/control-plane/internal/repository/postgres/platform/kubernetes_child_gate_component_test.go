package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesChildGateAction retains actual owner-assigned identifiers in a disposable native scenario.
// kubernetesChildGateAction хранит actual owner-assigned identifiers в disposable native scenario.
type kubernetesChildGateAction struct {
	binding value.ManagedKubernetesBinding
	opening command.ManagedKubernetesBeginEffect
	gate    entity.OwnerGate
}

// TestKubernetesChildGateOwnerComponent follows public grants/launch, dedicated claim and actual
// per-child Gate through owner Get/Resolve and subsequent Begin. Target transport and signed108 are
// intentionally outside this PostgreSQL test; the public local profile never contacts a live cluster.
// TestKubernetesChildGateOwnerComponent проводит public grants/launch, dedicated claim и actual
// per-child Gate через owner Get/Resolve и последующий Begin. Target transport и signed108 остаются
// вне PostgreSQL test; public local profile не обращается к live cluster.
func TestKubernetesChildGateOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-child-gate-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable child approval", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-child-gate-agent", "Child approval actor")
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.patch", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	approver, approverRef := previewAuthorityActor(t, ctx, repository, service, fixture.transport, "kube-child-approver", []string{"project.view", "run.view", "gate.resolve"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	execute(command.AddPlatformMembership, fixture.transport, "approver-platform", nil, command.PlatformMembershipInput{UserRef: approverRef, Role: "MEMBER", Active: true})
	execute(command.AddMembership, fixture.transport, "approver-project", nil, command.MembershipInput{ProjectRef: project.Ref, UserRef: approverRef, Permissions: []string{"VIEW", "RESOLVE_GATES"}, Active: true})
	viewer, viewerRef := previewAuthorityActor(t, ctx, repository, service, fixture.transport, "kube-child-viewer", []string{"project.view", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	execute(command.AddPlatformMembership, fixture.transport, "viewer-platform", nil, command.PlatformMembershipInput{UserRef: viewerRef, Role: "MEMBER", Active: true})
	execute(command.AddMembership, fixture.transport, "viewer-project", nil, command.MembershipInput{ProjectRef: project.Ref, UserRef: viewerRef, Permissions: []string{"VIEW"}, Active: true})
	restore := limitKubernetesInitiatorToExecutionPermissions(t, ctx, fixture, service, connection.Ref)
	defer restore()
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
	action := prepare("approve")
	viewer.Permission = "platform.query.owner-gates.get"
	hidden, err := service.GetOwnerGate(ctx, viewer, action.gate.Ref)
	if err != nil || hidden.KubernetesChild == nil || len(hidden.KubernetesChild.Preview.Changes) != 0 || len(hidden.KubernetesChild.Preview.Options) != 0 || len(hidden.KubernetesChild.Preview.Limitations) != 1 || hidden.KubernetesChild.Preview.Limitations[0] != "NOT_AUTHORIZED" {
		t.Fatal("viewer without resolve read submitted values", err)
	}
	wrong := action.gate.Version + 1
	if _, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: approver, Mutation: value.Mutation{IdempotencyKey: "kube-child-stale", ExpectedVersion: &wrong}, Payload: command.GateResolutionInput{GateRef: action.gate.Ref, Decision: "APPROVE"}}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("stale Gate OCC accepted", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: viewer, Mutation: value.Mutation{IdempotencyKey: "kube-child-viewer", ExpectedVersion: &action.gate.Version}, Payload: command.GateResolutionInput{GateRef: action.gate.Ref, Decision: "APPROVE"}}); err == nil {
		t.Fatal("viewer approved child")
	}
	approved := execute(command.ResolveOwnerGate, approver, "approve", &action.gate.Version, command.GateResolutionInput{GateRef: action.gate.Ref, Decision: "APPROVE"})
	if approved.Gate == nil || approved.Gate.State != "APPROVED" {
		t.Fatal("approval missing readback")
	}
	repeated := execute(command.ResolveOwnerGate, approver, "approve", &action.gate.Version, command.GateResolutionInput{GateRef: action.gate.Ref, Decision: "APPROVE"})
	if repeated.Gate == nil || repeated.Gate.Version != approved.Gate.Version {
		t.Fatal("exact decision replay changed Gate")
	}
	// Authorized actor event replay must not upgrade the immutable event disclosure channel.
	// Event replay авторизованного actor не повышает disclosure immutable event channel.
	approver.Permission = "platform.query.run-events.list"
	events, _, _, err := service.ListRunEvents(ctx, approver, query.Filter{ResourceRef: run.Ref, Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal("read child Gate events", err)
	}
	eventGates := 0
	for _, event := range events {
		if gate := event.Delta.Gate; gate != nil && gate.Ref == action.gate.Ref {
			eventGates++
			if gate.KubernetesChild == nil || len(gate.KubernetesChild.Preview.Options) != 0 || len(gate.KubernetesChild.Preview.Changes) != 0 || gate.KubernetesChild.Preview.SecretMaterialRef != "" {
				t.Fatal("actor event read disclosed standalone Gate values")
			}
		}
	}
	if eventGates == 0 {
		t.Fatal("child Gate event not delivered")
	}
	var state, leaseRef string
	var generation, effects int64
	if repository.pool.QueryRow(ctx, `SELECT state,lease_ref,generation FROM control_plane.integration_invocations WHERE ref=$1`, action.binding.InvocationRef).Scan(&state, &leaseRef, &generation) != nil || state != "RUNNING" || leaseRef != action.binding.LeaseRef || generation != action.binding.Generation {
		t.Fatal("approval replaced original running claim")
	}
	if repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.kubernetes_execution_effects effect JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id WHERE child.ref=$1`, action.opening.ChildRef).Scan(&effects) != nil || effects != 0 {
		t.Fatal("approval allocated effect credit")
	}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	effect, err := repository.BeginManagedKubernetesEffect(ctx, broker, action.binding, action.opening)
	if err != nil || effect.ReconcileOnly || effect.Ref == "" {
		t.Fatal("original claim cannot use approved child", err)
	}
	for _, decision := range []string{"REJECT", "CANCEL"} {
		pending := prepare(strings.ToLower(decision))
		outcome := execute(command.ResolveOwnerGate, approver, strings.ToLower(decision), &pending.gate.Version, command.GateResolutionInput{GateRef: pending.gate.Ref, Decision: decision})
		want := "REJECTED"
		if decision == "CANCEL" {
			want = "CANCELLED"
		}
		if outcome.Gate == nil || outcome.Gate.State != want || outcome.Run == nil || outcome.Run.State == "CANCELLED" {
			t.Fatal("child refusal cancelled root or lost decision")
		}
		if repository.pool.QueryRow(ctx, `SELECT state,coalesce(lease_ref,''),generation FROM control_plane.integration_invocations WHERE ref=$1`, pending.binding.InvocationRef).Scan(&state, &leaseRef, &generation) != nil || state != want || leaseRef != "" {
			t.Fatal("child refusal retained running claim")
		}
		broker.Permission = "platform.runtime.kubernetes.effect.begin"
		if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, pending.binding, pending.opening); err == nil {
			t.Fatal("refused child executed")
		}
	}
	expired := prepare("expired")
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, expired.binding.InvocationRef); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: approver, Mutation: value.Mutation{IdempotencyKey: "kube-child-expired", ExpectedVersion: &expired.gate.Version}, Payload: command.GateResolutionInput{GateRef: expired.gate.Ref, Decision: "APPROVE"}}); err == nil {
		t.Fatal("expired original claim gained approval")
	}
}
