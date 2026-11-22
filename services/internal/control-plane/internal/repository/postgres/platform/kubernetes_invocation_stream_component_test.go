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
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// newKubernetesInvocationStreamResultFixture opens an actual owner-created approved stream whose
// native handshake is synthetic. It proves plan/effect/terminal aggregation, not target execution.
// newKubernetesInvocationStreamResultFixture открывает actual owner-created approved stream с
// synthetic native handshake. Проверяет plan/effect/terminal aggregation, не target execution.
func newKubernetesInvocationStreamResultFixture(t *testing.T, ctx context.Context) (*Repository, *platformservice.Service, value.Principal, value.Principal, value.ManagedKubernetesBinding, entity.ManagedKubernetesPlan, entity.ManagedKubernetesEffectAdmission, managedkubernetes.HandshakeReceipt) {
	t.Helper()
	repository, service, broker, gateway, binding, plan, admission, handshake, _ := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
	return repository, service, broker, gateway, binding, plan, admission, handshake
}

// newKubernetesInvocationStreamResultFixtureWithActor retains the real original public transport
// principal for artifact/history checks, without deriving a fake user from a worker principal.
// newKubernetesInvocationStreamResultFixtureWithActor сохраняет real original public transport
// principal для artifact/history checks без создания fake user из worker principal.
func newKubernetesInvocationStreamResultFixtureWithActor(t *testing.T, ctx context.Context) (*Repository, *platformservice.Service, value.Principal, value.Principal, value.ManagedKubernetesBinding, entity.ManagedKubernetesPlan, entity.ManagedKubernetesEffectAdmission, managedkubernetes.HandshakeReceipt, value.Principal) {
	t.Helper()
	repository, service, broker, gateway, binding, plan, snapshot, actor := newKubernetesApprovedPlanFixture(t, ctx)
	child := plan.Children[0]
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}

	admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
	if err != nil || admission.Stream == nil {
		t.Fatal("begin input stream", err)
	}
	stream := admission.Stream.Ref
	bindingDigest, _ := managedkubernetes.BindingDigest(binding)
	pin := child.NodeTransport
	if pin == nil {
		t.Fatal("missing owner-enriched node transport")
	}
	handshake := managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: bindingDigest, ChildInputDigest: child.InputDigest, EffectRef: admission.Ref, EffectGeneration: admission.Generation, StreamRef: stream, StreamGeneration: binding.Generation, Operation: child.Operation, TargetRef: snapshot.Target.Ref, TargetRevision: snapshot.Target.Revision}, TransportRef: pin.Ref, TransportGeneration: pin.Generation, Protocol: string(pin.Protocol), HTTPStatus: 101, NegotiatedProtocol: pin.Protocol.WireProtocol(), PeerSPKISHA256: pin.ServerPublicKeySHA256}
	hsDigest, err := handshake.Digest()
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.stream.open"
	if _, err := repository.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: stream, EffectRef: admission.Ref, EffectGeneration: admission.Generation, HandshakeReceipt: handshake, HandshakeReceiptDigest: hsDigest}); err != nil {
		t.Fatal("record input handshake", err)
	}
	return repository, service, broker, gateway, binding, plan, admission, handshake, actor
}

// newKubernetesApprovedPlanFixture creates the real claimed and approved owner plan before any effect intent, allowing native tests to bound authority without editing stored plans.
// newKubernetesApprovedPlanFixture создаёт real claimed и approved owner plan до effect intent, чтобы native tests ограничивали authority без изменения stored plan.
func newKubernetesApprovedPlanFixture(t *testing.T, ctx context.Context) (*Repository, *platformservice.Service, value.Principal, value.Principal, value.ManagedKubernetesBinding, entity.ManagedKubernetesPlan, entity.ManagedKubernetesOperationSnapshot, value.Principal) {
	t.Helper()
	return newKubernetesApprovedOperationPlanFixture(t, ctx, "kubernetes.exec")
}

// newKubernetesApprovedOperationPlanFixture builds only the existing exec or replace owner protocol before first dispatch; every package, grant, runtime claim and human child approval remains real.
// newKubernetesApprovedOperationPlanFixture строит лишь existing exec либо replace owner protocol до first dispatch; package, grant, runtime claim и human child approval настоящие.
func newKubernetesApprovedOperationPlanFixture(t *testing.T, ctx context.Context, operation string) (*Repository, *platformservice.Service, value.Principal, value.Principal, value.ManagedKubernetesBinding, entity.ManagedKubernetesPlan, entity.ManagedKubernetesOperationSnapshot, value.Principal) {
	t.Helper()
	if operation != "kubernetes.exec" && operation != "kubernetes.replace" {
		t.Fatal("unsupported bounded owner fixture operation")
	}
	fixture, service, connection, bindingMetadata := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, bindingMetadata)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-input-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable private input", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-input-agent", "Private input owner")
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: operation, ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Write a reviewed private stdin frame"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-input-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 {
		t.Fatal("claim input runtime")
	}
	node := stringMap(runtime[0], "nodeRef")
	uid := "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
	payload := map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "uid": uid, "resourceVersion": "17", "options": map[string]any{"container": "main", "command": []string{"cat"}, "stdin": true, "tty": true, "timeoutSeconds": 120}}
	if operation == "kubernetes.replace" {
		payload = map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": "review", "name": "settings", "uid": uid, "resourceVersion": "17", "body": map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "settings", "namespace": "review", "uid": uid, "resourceVersion": "17"}, "data": map[string]any{"mode": "safe"}}}
	}
	resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": node, "connection_ref": connection.Ref, "capability_key": operation, "idempotency_key": "kube-input-invocation"}, payload)
	if err != nil {
		t.Fatal("resolve input invocation", err)
	}
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
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "kube-input-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	if binding.InvocationRef != stringMap(resolved, "invocationRef") {
		t.Fatal("claim selected another input invocation")
	}
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal(err)
	}
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: uid, ResourceVersion: "17", Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "synthetic-node", Container: "main"}}
	if operation == "kubernetes.replace" {
		candidate.Resource.Resource = "configmaps"
		candidate.Resource.Name = "settings"
		candidate.PodWitness = nil
	}
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
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	var waiting *platformservice.KubernetesApprovalRequiredError
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.As(err, &waiting) {
		t.Fatal("input child did not require exact approval", err)
	}
	actor := fixture.transport
	actor.Permission = "platform.query.owner-gates.get"
	gate, err := service.GetOwnerGate(ctx, actor, waiting.GateRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ResolveOwnerGate, fixture.transport, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	return repository, service, broker, gateway, binding, plan, snapshot, fixture.transport
}

// TestKubernetesInvocationStreamResultOwnerComponent preserves known remote truth while refusing
// outer success for LOST or unjoined streams, including zero output and an accepted partial prefix.
// TestKubernetesInvocationStreamResultOwnerComponent сохраняет known remote truth, запрещая
// outer success для LOST либо unjoined streams, включая zero output и accepted partial prefix.
func TestKubernetesInvocationStreamResultOwnerComponent(t *testing.T) {
	for _, test := range []struct {
		name, local, remote, reason string
		joined, output              bool
		want                        enum.ManagedKubernetesInvocationState
	}{
		{"lost_zero", "LOST", "SUCCEEDED", "TRANSPORT_LOST", true, false, enum.KubernetesInvocationUnknown},
		{"lost_prefix", "LOST", "SUCCEEDED", "OUTPUT_LOST", true, true, enum.KubernetesInvocationUnknown},
		{"unjoined", "LOST", "UNKNOWN", "TRANSPORT_LOST", false, false, enum.KubernetesInvocationUnknown},
		{"complete_zero", "COMPLETED", "SUCCEEDED", "REMOTE_EXIT", true, false, enum.KubernetesInvocationSucceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
			defer cancel()
			repository, service, broker, gateway, binding, plan, admission, handshake := newKubernetesInvocationStreamResultFixture(t, ctx)
			stream := admission.Stream.Ref
			if test.output {
				payload := []byte("accepted prefix")
				sum := sha256.Sum256(payload)
				frame := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: 1, Channel: "STDOUT", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(payload)), NativeAuthorizedUntil: time.Now().Add(4 * time.Second)}
				broker.Permission = "platform.runtime.kubernetes.frame.begin"
				credit, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, frame)
				if err != nil {
					t.Fatal("admit captured prefix", err)
				}
				gateway.Permission = "platform.runtime.kubernetes.output.accept"
				stage, err := service.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: stream, Sequence: 1, DeliveryOrdinal: credit.DeliveryOrdinal}, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, Payload: payload})
				if err != nil || stage.State != enum.KubernetesOutputAccepted {
					t.Fatal("durable partial prefix", err)
				}
				broker.Permission = "platform.runtime.kubernetes.frame.complete"
				if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: 1, PayloadDigest: frame.PayloadDigest}); err != nil {
					t.Fatal("ack captured prefix", err)
				}
			}
			hsDigest, err := handshake.Digest()
			if err != nil {
				t.Fatal(err)
			}
			terminal := managedkubernetes.TerminalReceipt{Origin: handshake.Origin, HandshakeDigest: hsDigest, Protocol: handshake.Protocol, LocalState: test.local, RemoteOutcome: test.remote, TransportJoined: test.joined, ReasonCode: test.reason}
			if test.remote == "SUCCEEDED" {
				zero := int32(0)
				terminal.ExitCode = &zero
			}
			terminalDigest, err := terminal.Digest()
			if err != nil {
				t.Fatal(err)
			}
			broker.Permission = "platform.runtime.kubernetes.stream.close"
			closed, err := repository.CloseManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesCloseStream{StreamRef: stream, ExpectedState: enum.KubernetesStreamActive, TerminalState: enum.ManagedKubernetesStreamState(test.local), RemoteOutcome: enum.ManagedKubernetesEffectOutcome(test.remote), TransportJoined: test.joined, ObservedExitCode: terminal.ExitCode, TerminalReceipt: terminal, TerminalReceiptDigest: terminalDigest})
			if err != nil || string(closed.State) != test.local || string(closed.RemoteOutcome) != test.remote {
				t.Fatal("store typed terminal truth", closed.State, err)
			}
			built, err := managedkubernetes.BuildStreamEffectResult(terminal)
			if err != nil {
				t.Fatal(err)
			}
			broker.Permission = "platform.runtime.kubernetes.effect.complete"
			accepted, err := repository.CompleteManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesCompleteEffect{EffectRef: admission.Ref, EffectGeneration: admission.Generation, ChildInputDigest: plan.Children[0].InputDigest, StreamRef: stream, Outcome: enum.ManagedKubernetesEffectOutcome(built.Outcome), ResultJSON: built.ResultJSON, ResultDigest: built.ResultDigest, ReasonCode: built.ReasonCode})
			if err != nil || !accepted.Applied {
				t.Fatal("store exact stream effect", err)
			}
			gateway.Permission = "platform.runtime.kubernetes.complete"
			completed, err := service.CompleteManagedKubernetesInvocation(ctx, gateway, binding, command.ManagedKubernetesCompleteInvocation{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest})
			if err != nil || completed.State != test.want || !completed.CompletionRecorded {
				t.Fatal("outer state promoted remote exit", completed.State, err)
			}
			var summary string
			if err := repository.pool.QueryRow(ctx, "SELECT result_summary::text FROM control_plane.integration_invocations WHERE ref=$1", binding.InvocationRef).Scan(&summary); err != nil {
				t.Fatal("read owner result", err)
			}
			if summary != string(built.ResultJSON) {
				t.Fatal("outer result erased remote/local facts")
			}
		})
	}
}
