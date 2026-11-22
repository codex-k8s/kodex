package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
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

// queryKubernetesPFPreflightTestEvidence reads durable preflight facts in the disposable fixture without changing owner authority.
// queryKubernetesPFPreflightTestEvidence читает durable preflight facts в disposable fixture без изменения owner authority.
//
//go:embed testdata/sql/kubernetes_pf__preflight_test_evidence.sql
var queryKubernetesPFPreflightTestEvidence string

// kubernetesPortForwardOwnerFixture retains only actual public launch/approval and owner-assigned private bindings; it never inserts a synthetic PF row.
// Native handshake facts are synthetic; actual pinned TLS/SPDY is independently qualified in the broker suite.
// kubernetesPortForwardOwnerFixture хранит actual public launch/approval и owner-assigned private bindings; synthetic PF rows не вставляются.
// Native handshake facts синтетические; actual pinned TLS/SPDY отдельно квалифицируется broker suite.
type kubernetesPortForwardOwnerFixture struct {
	setup                  kubernetesSetupOwnerFixture
	service                *platformservice.Service
	connection             entity.IntegrationConnection
	agentRef, stream       string
	binding                value.ManagedKubernetesBinding
	broker, gateway, actor value.Principal
	plan                   entity.ManagedKubernetesPlan
	snapshot               entity.ManagedKubernetesOperationSnapshot
	restoreOwner           func()
}

// newKubernetesPortForwardOwnerFixture registers the selected operator protocol, demotes the original actor before launch and traverses real current-grant claim/plan/Gate paths.
// openParent=false preserves a genuine unattempted plan for independently derived profile rejection without fabricating an effect or stream.
// newKubernetesPortForwardOwnerFixture регистрирует selected operator protocol, понижает original actor до launch и проходит real current-grant claim/plan/Gate paths.
// openParent=false сохраняет genuine unattempted plan для independently derived profile rejection без fabricated effect либо stream.
func newKubernetesPortForwardOwnerFixture(t *testing.T, ctx context.Context, protocol managedkubernetes.PrivilegedProtocol, openParent bool) kubernetesPortForwardOwnerFixture {
	t.Helper()
	fixture, service, connection, bindingMetadata := kubernetesPersonalHealthFixture(t, ctx)

	operatorCandidate := cloneKubernetesOperatorComponent(t, fixture.candidate)
	operatorCandidate.Revision++
	operatorCandidate.ProducedAt = time.Now().UTC()
	// A protocol change is a new immutable pin; the original accepted pin remains unchanged.
	// Изменение protocol создаёт новый immutable pin; исходный accepted pin не меняется.
	pfNode := operatorCandidate.Nodes[0]
	pfNode.Transport.Ref = "knp_synthetic_pf_profile"
	pfNode.Transport.Generation++
	pfNode.Transport.Protocol = protocol
	operatorCandidate.Nodes = append(operatorCandidate.Nodes, pfNode)
	fixture.repository = newSyntheticRuntimeComponentRepository(t, ctx, fixture.pool, func(repository *Repository) {
		if repository.ConfigureKubernetesOperator(operatorCandidate) != nil {
			t.Fatal("configure actual PF operator revision")
		}
	})
	if err := fixture.repository.RegisterKubernetesOperator(ctx); err != nil {
		t.Fatal("register actual PF operator revision", err)
	}
	fixture.candidate = operatorCandidate
	var serviceErr error
	service, serviceErr = platformservice.New(fixture.repository)
	if serviceErr != nil {
		t.Fatal("construct PF owner service", serviceErr)
	}
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, bindingMetadata)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-pf-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable private input", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-pf-agent", "Private input owner")
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.port-forward", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	// Approval belongs to a separately scoped project approver; execution never regains management or Gate authority.
	// Решение принимает отдельно scoped project approver; execution не получает обратно management либо Gate authority.
	approver, approverRef := previewAuthorityActor(t, ctx, repository, service, fixture.transport, "kube-pf-approver", []string{"project.view", "run.view", "gate.resolve"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	execute(command.AddPlatformMembership, fixture.transport, "approver-platform", nil, command.PlatformMembershipInput{UserRef: approverRef, Role: "MEMBER", Active: true})
	execute(command.AddMembership, fixture.transport, "approver-project", nil, command.MembershipInput{ProjectRef: project.Ref, UserRef: approverRef, Permissions: []string{"VIEW", "RESOLVE_GATES"}, Active: true})
	restoreOwner := limitKubernetesInitiatorToExecutionPermissions(t, ctx, fixture, service, connection.Ref)
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Forward the approved Pod ports"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-pf-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 {
		t.Fatal("claim input runtime")
	}
	node := stringMap(runtime[0], "nodeRef")
	uid := "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
	payload := map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "uid": uid, "resourceVersion": "17", "options": map[string]any{"ports": []map[string]any{{"port": 18080, "targetPort": 8080}, {"port": 19090, "targetPort": 9090}}, "timeoutSeconds": 120}}
	resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": node, "connection_ref": connection.Ref, "capability_key": "kubernetes.port-forward", "idempotency_key": "kube-pf-invocation"}, payload)
	if err != nil {
		t.Fatal("resolve input invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
			gateway.Permission = operation
		}
	}
	claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, gateway, "kube-pf-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
	if err != nil || len(claims) != 1 {
		t.Fatal("claim input invocation", err)
	}
	claim := claims[0]
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "kube-pf-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	if binding.InvocationRef != stringMap(resolved, "invocationRef") {
		t.Fatal("claim selected another input invocation")
	}
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal("resolve PF original owner snapshot", err)
	}
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: uid, ResourceVersion: "17", Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "synthetic-node", Container: "main"}}
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal("hash PF child proposal", err)
	}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
	if err != nil || len(plan.Children) != 1 {
		t.Fatal("prepare input child", err)
	}
	child := plan.Children[0]
	base := kubernetesPortForwardOwnerFixture{setup: fixture, service: service, connection: connection, agentRef: agent.Ref, binding: binding, broker: broker, gateway: gateway, actor: fixture.transport, plan: plan, snapshot: snapshot, restoreOwner: restoreOwner}
	if !openParent {
		return base
	}
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	var waiting *platformservice.KubernetesApprovalRequiredError
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.As(err, &waiting) {
		t.Fatal("input child did not require exact approval", err)
	}
	approver.Permission = "platform.query.owner-gates.get"
	gate, err := service.GetOwnerGate(ctx, approver, waiting.GateRef)
	if err != nil {
		t.Fatal("read approved PF child Gate", err)
	}
	execute(command.ResolveOwnerGate, approver, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
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
		t.Fatal("hash PF native handshake", err)
	}
	broker.Permission = "platform.runtime.kubernetes.stream.open"
	if _, err := repository.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: stream, EffectRef: admission.Ref, EffectGeneration: admission.Generation, HandshakeReceipt: handshake, HandshakeReceiptDigest: hsDigest}); err != nil {
		t.Fatal("record input handshake", err)
	}
	base.stream = stream
	return base
}

// input reads the current public OCC version through the original actor; it never derives execution authority from a known locator.
// input читает current public OCC version исходным actor; известный locator не становится execution authority.
func (fixture kubernetesPortForwardOwnerFixture) input(t *testing.T, ctx context.Context) entity.KubernetesStreamInputView {
	t.Helper()
	actor := fixture.actor
	actor.Permission = "platform.integrations.kubernetes.input.get"
	read, err := fixture.service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream})
	if err != nil {
		t.Fatal("read current PF input", err)
	}
	return read.View
}

// get reads one original connection without promoting CLOSED to a native join.
// get читает original connection без повышения CLOSED до native join.
func (fixture kubernetesPortForwardOwnerFixture) get(t *testing.T, ctx context.Context, ref string) entity.KubernetesPortForwardConnectionReadback {
	t.Helper()
	actor := fixture.actor
	actor.Permission = "platform.integrations.kubernetes.port-forward.get"
	read, err := fixture.service.GetKubernetesPortForwardConnection(ctx, actor, query.KubernetesPortForwardConnectionRead{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream, ConnectionRef: ref})
	if err != nil {
		t.Fatal("read exact PF connection", err)
	}
	return read
}

// open traverses public Open and retains the assigned incarnation before any native admission.
// open проходит public Open и сохраняет assigned incarnation до native admission.
func (fixture kubernetesPortForwardOwnerFixture) open(t *testing.T, ctx context.Context, key string, slot uint32) entity.KubernetesPortForwardConnectionReadback {
	t.Helper()
	view := fixture.input(t, ctx)
	actor := fixture.actor
	actor.Permission = "platform.integrations.kubernetes.port-forward.open"
	read, err := fixture.service.OpenKubernetesPortForwardConnection(ctx, actor, value.Mutation{IdempotencyKey: key, ExpectedVersion: &view.Version}, command.KubernetesPortForwardConnectionOpen{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream, Slot: slot})
	if err != nil || read.Submission == nil || read.Submission.State != enum.KubernetesInputQueued || read.Connection.State != enum.KubernetesPortForwardConnectionPrepared {
		t.Fatal("queue owner-assigned PF OPEN", err)
	}
	return read
}

// admitInput consumes one actual protected stage and verifies its canonical connection/action before Begin; bytes are cleared after metadata admission.
// admitInput потребляет actual protected stage и сверяет canonical connection/action перед Begin; bytes очищаются после metadata admission.
func (fixture kubernetesPortForwardOwnerFixture) admitInput(t *testing.T, ctx context.Context, ref string, action managedkubernetes.PortForwardConnectionAction) command.ManagedKubernetesBeginFrame {
	t.Helper()
	source := fixture.gateway
	source.Permission = "platform.runtime.kubernetes.input.claim"
	delivery, err := fixture.service.ClaimManagedKubernetesInputFrame(ctx, source, fixture.binding, fixture.stream)
	if err != nil || delivery.Disposition != enum.KubernetesInputFrameDeliver || delivery.Frame == nil {
		t.Fatal("claim canonical PF action", err)
	}
	defer clear(delivery.Frame.Payload)
	frame, err := managedkubernetes.DecodePortForwardConnectionFrame(delivery.Frame.Payload)
	if err != nil || frame.ConnectionRef != ref || frame.Action != action || frame.NativeOrdinal != 0 {
		t.Fatal("private stage changed owner PF coordinates", err)
	}
	broker := fixture.broker
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	input := command.ManagedKubernetesBeginFrame{StreamRef: fixture.stream, Direction: enum.KubernetesFrameInput, Sequence: delivery.Frame.Sequence, Channel: "PORT_FORWARD", PayloadDigest: delivery.Frame.PayloadDigest, ByteCount: delivery.Frame.ByteCount}
	admission, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, input)
	if err != nil || admission.Disposition != enum.KubernetesFrameSend || admission.ExpiresAt.After(delivery.Frame.DeliveryExpiresAt) {
		t.Fatal("admit exact staged PF action", err)
	}
	if action == managedkubernetes.PortForwardConnectionOpen {
		opening := managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: frame.ConnectionRef, Generation: frame.Generation, Slot: frame.Slot}
		if admission.PortForwardRetention == nil || admission.PortForwardRetention.Validate(managedkubernetes.MaximumForwardPorts, &opening) != nil {
			t.Fatal("actual owner OPEN omitted exact native retention snapshot")
		}
	} else if admission.PortForwardRetention != nil {
		t.Fatal("non-OPEN action received retention authority")
	}
	return input
}

// ackInput records the exact native acknowledgement without a second Claim or payload replay.
// ackInput фиксирует exact native acknowledgement без второго Claim либо payload replay.
func (fixture kubernetesPortForwardOwnerFixture) ackInput(t *testing.T, ctx context.Context, input command.ManagedKubernetesBeginFrame) {
	t.Helper()
	broker := fixture.broker
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := fixture.setup.repository.CompleteManagedKubernetesFrame(ctx, broker, fixture.binding, command.ManagedKubernetesCompleteFrame{StreamRef: fixture.stream, Direction: enum.KubernetesFrameInput, Sequence: input.Sequence, PayloadDigest: input.PayloadDigest}); err != nil {
		t.Fatal("acknowledge exact PF native action", err)
	}
}

// output derives the private witness and whole-envelope hash from native-emitted coordinates, never caller-selected target authority.
// output выводит private witness и whole-envelope hash из native-emitted coordinates, не caller-selected target authority.
func (fixture kubernetesPortForwardOwnerFixture) output(t *testing.T, connection entity.KubernetesPortForwardConnection, sequence, ordinal uint64, body string) (command.ManagedKubernetesBeginFrame, []byte) {
	t.Helper()
	raw, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{ConnectionRef: connection.Ref, Generation: uint64(connection.Generation), Slot: byte(connection.Slot), NativeOrdinal: ordinal, Action: managedkubernetes.PortForwardConnectionData, Body: []byte(body)})
	if err != nil {
		t.Fatal(err)
	}
	witness, err := managedkubernetes.PortForwardOutputWitness(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return command.ManagedKubernetesBeginFrame{StreamRef: fixture.stream, Direction: enum.KubernetesFrameOutput, Sequence: sequence, Channel: "PORT_FORWARD", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(raw)), NativeAuthorizedUntil: time.Now().UTC().Add(3 * time.Second), PortForward: &witness}, raw
}

// TestKubernetesPortForwardOwnerComponent proves actual Open/stage/admit/ACK/Close ownership, retained output and deduplicated partial capture without synthetic PF rows.
// Native wire handshakes are synthetic and this test does not qualify live SPDY or artifact publication.
// TestKubernetesPortForwardOwnerComponent проверяет actual Open/stage/admit/ACK/Close ownership, retained output и deduplicated partial capture без synthetic PF rows.
// Native wire handshakes синтетические; live SPDY и artifact publication этим тестом не квалифицируются.
func TestKubernetesPortForwardOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	fixture := newKubernetesPortForwardOwnerFixture(t, ctx, managedkubernetes.PortForwardSPDYV1, true)
	actor := fixture.actor
	actor.Permission = "platform.integrations.kubernetes.port-forward.list"
	page, err := fixture.service.ListKubernetesPortForwardConnections(ctx, actor, query.KubernetesPortForwardConnectionList{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream})
	if err != nil || len(page.Items) != 0 || page.Profile.Protocol != string(managedkubernetes.PortForwardSPDYV1) || page.Profile.MaximumActiveConnections != 16 || len(page.Profile.Ports) != 2 || page.Profile.Ports[0].Port != 18080 || page.Profile.Ports[1].TargetPort != 9090 || page.Profile.TimeoutSeconds != 120 {
		t.Fatal("read exact immutable approved PF profile", err)
	}
	before := fixture.input(t, ctx)
	first := fixture.open(t, ctx, "pf-open-first", 0)
	actor.Permission = "platform.integrations.kubernetes.port-forward.open"
	replayed, err := fixture.service.OpenKubernetesPortForwardConnection(ctx, actor, value.Mutation{IdempotencyKey: "pf-open-first", ExpectedVersion: &before.Version}, command.KubernetesPortForwardConnectionOpen{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream, Slot: 0})
	if err != nil || replayed.Connection.Ref != first.Connection.Ref || replayed.Connection.Generation != first.Connection.Generation || replayed.Submission.Sequence != first.Submission.Sequence {
		t.Fatal("lost OPEN reply allocated a replacement incarnation", err)
	}
	if _, err := fixture.service.OpenKubernetesPortForwardConnection(ctx, actor, value.Mutation{IdempotencyKey: "pf-open-first", ExpectedVersion: &before.Version}, command.KubernetesPortForwardConnectionOpen{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream, Slot: 1}); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("OPEN key adopted another slot", err)
	}
	broker := fixture.broker
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	output, raw := fixture.output(t, first.Connection, 1, 1, "before-open-admission")
	if _, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, output); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("output intent substituted for native OPEN admission", err)
	}
	clear(raw)
	open := fixture.admitInput(t, ctx, first.Connection.Ref, managedkubernetes.PortForwardConnectionOpen)
	if current := fixture.get(t, ctx, first.Connection.Ref); current.Connection.State != enum.KubernetesPortForwardConnectionOpening || current.Connection.NativeJoined {
		t.Fatal("native OPEN admission became acknowledged/joined")
	}
	fixture.ackInput(t, ctx, open)
	first = fixture.get(t, ctx, first.Connection.Ref)
	second := fixture.open(t, ctx, "pf-open-second", 1)
	fixture.ackInput(t, ctx, fixture.admitInput(t, ctx, second.Connection.Ref, managedkubernetes.PortForwardConnectionOpen))
	second = fixture.get(t, ctx, second.Connection.Ref)
	if first.Connection.State != enum.KubernetesPortForwardConnectionOpen || second.Connection.State != enum.KubernetesPortForwardConnectionOpen {
		t.Fatal("OPEN ACK did not preserve distinct siblings")
	}
	// This frame is admitted before Close; its immutable bytes remain eligible for current protected delivery afterwards.
	// Frame допущен до Close; immutable bytes сохраняют право current protected delivery после него.
	output, raw = fixture.output(t, first.Connection, 1, 1, "retained-before-close")
	defer clear(raw)
	credit, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, output)
	if err != nil || credit.Disposition != enum.KubernetesFrameSend {
		t.Fatal("admit retained PF output", err)
	}
	view := fixture.input(t, ctx)
	actor.Permission = "platform.integrations.kubernetes.port-forward.close"
	closed, err := fixture.service.CloseKubernetesPortForwardConnection(ctx, actor, value.Mutation{IdempotencyKey: "pf-close-first", ExpectedVersion: &view.Version}, command.KubernetesPortForwardConnectionClose{InvocationRef: fixture.binding.InvocationRef, StreamRef: fixture.stream, ConnectionRef: first.Connection.Ref})
	if err != nil || closed.Connection.State != enum.KubernetesPortForwardConnectionClosed || closed.Connection.NativeJoined || closed.Submission == nil {
		t.Fatal("owner Close claimed native join or lost exact CLOSE intent", err)
	}
	gateway := fixture.gateway
	gateway.Permission = "platform.runtime.kubernetes.output.accept"
	receipt, err := fixture.service.AcceptManagedKubernetesOutputFrame(ctx, gateway, fixture.binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: fixture.stream, Sequence: output.Sequence, DeliveryOrdinal: credit.DeliveryOrdinal}, Channel: output.Channel, PayloadDigest: output.PayloadDigest, ByteCount: output.ByteCount, Payload: append([]byte(nil), raw...)})
	if err != nil || receipt.State != enum.KubernetesOutputAccepted || !receipt.AuthorizedUntil.Equal(credit.ExpiresAt) {
		t.Fatal("per-connection Close erased admitted exact output", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := fixture.setup.repository.CompleteManagedKubernetesFrame(ctx, broker, fixture.binding, command.ManagedKubernetesCompleteFrame{StreamRef: fixture.stream, Direction: enum.KubernetesFrameOutput, Sequence: output.Sequence, PayloadDigest: output.PayloadDigest}); err != nil {
		t.Fatal("ACK retained PF output", err)
	}
	fixture.ackInput(t, ctx, fixture.admitInput(t, ctx, first.Connection.Ref, managedkubernetes.PortForwardConnectionClose))
	if one, sibling := fixture.get(t, ctx, first.Connection.Ref), fixture.get(t, ctx, second.Connection.Ref); !one.Connection.NativeJoined || sibling.Connection.State != enum.KubernetesPortForwardConnectionOpen || sibling.Connection.NativeJoined {
		t.Fatal("one pair close changed sibling lifecycle or lacked native join receipt")
	}
	// A new native event read before owner Close becomes PARTIAL, with no frame/credit/ACK or silent byte-budget refund.
	// Новый native event, прочитанный до owner Close, становится PARTIAL без frame/credit/ACK и скрытого возврата byte budget.
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	dropped, droppedRaw := fixture.output(t, first.Connection, 2, 2, "unadmitted-after-close")
	defer clear(droppedRaw)
	for attempt := 0; attempt < 2; attempt++ {
		noCredit, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, dropped)
		if err != nil || noCredit.Disposition != enum.KubernetesFrameClosed || noCredit.DeliveryOrdinal != 0 || !noCredit.ExpiresAt.IsZero() {
			t.Fatal("closed native candidate minted output credit", err)
		}
	}
	changed := dropped
	changed.PayloadDigest = strings.Repeat("f", 64)
	if _, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("same native ordinal changed immutable loss tuple", err)
	}
	var losses, lossBytes, frames int64
	if err := fixture.setup.repository.pool.QueryRow(ctx, `SELECT count(*),coalesce(sum(loss.byte_count),0) FROM control_plane.kubernetes_capture_losses loss JOIN control_plane.kubernetes_execution_streams stream ON stream.id=loss.stream_id AND stream.organization_id=loss.organization_id WHERE stream.ref=$1`, fixture.stream).Scan(&losses, &lossBytes); err != nil || losses != 1 || lossBytes != int64(dropped.ByteCount) {
		t.Fatal("native loss dedup/quota omitted canonical envelope bytes", err)
	}
	if err := fixture.setup.repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.kubernetes_execution_frames frame JOIN control_plane.kubernetes_execution_streams stream ON stream.id=frame.stream_id AND stream.organization_id=frame.organization_id WHERE stream.ref=$1 AND frame.direction='OUTPUT'`, fixture.stream).Scan(&frames); err != nil || frames != 1 {
		t.Fatal("CLOSED created an execution frame or consumed provisional sequence", err)
	}
	current := fixture.get(t, ctx, second.Connection.Ref)
	good, goodRaw := fixture.output(t, current.Connection, 2, 1, "sibling-progress")
	defer clear(goodRaw)
	if granted, err := fixture.setup.repository.BeginManagedKubernetesFrame(ctx, broker, fixture.binding, good); err != nil || granted.Disposition != enum.KubernetesFrameSend {
		t.Fatal("closed candidate blocked sibling output sequence", err)
	}
}

// TestKubernetesPortForwardPreflightOwnerComponent persists the closed unsupported reason from an actual unattempted owner plan and proves no effect/stream was fabricated.
// TestKubernetesPortForwardPreflightOwnerComponent сохраняет closed unsupported reason из actual unattempted owner plan и доказывает отсутствие fabricated effect/stream.
func TestKubernetesPortForwardPreflightOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fixture := newKubernetesPortForwardOwnerFixture(t, ctx, managedkubernetes.PortForwardWebSocketV4, false)
	gateway := fixture.gateway
	gateway.Permission = ""
	for permission, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName {
			gateway.Permission = permission
		}
	}
	if gateway.Permission == "" {
		t.Fatal("production owner completion operation is not registered")
	}
	// Exact protected read checkpoints localize a refusal before completion without changing authority or native state.
	// Exact protected read checkpoints локализуют отказ до completion без изменения authority либо native state.
	readCaller := gateway
	for permission, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName {
			readCaller.Permission = permission
		}
	}
	before, err := fixture.service.GetManagedKubernetesInvocation(ctx, readCaller, fixture.binding)
	if err != nil || before.State != enum.KubernetesInvocationRunning || before.PlanDigest != fixture.plan.Digest {
		t.Fatal("preflight exact original claim metadata checkpoint", err)
	}
	broker := fixture.broker
	broker.Permission = "platform.runtime.kubernetes.resolve"
	fresh, err := fixture.setup.repository.ResolveManagedKubernetesOperation(ctx, broker, fixture.binding)
	if err != nil || fresh.Digest != fixture.snapshot.Digest {
		t.Fatal("preflight current original owner snapshot checkpoint", err)
	}
	// The service normalizes the verified proof actor/tenant IDs before the repository expects canonical refs.
	// Service нормализует verified proof actor/tenant IDs до repository, ожидающего canonical refs.
	receipt, err := fixture.service.CompleteManagedKubernetesInvocation(ctx, gateway, fixture.binding, command.ManagedKubernetesCompleteInvocation{PlanRef: fixture.plan.Ref, PlanRevision: fixture.plan.Revision, PlanDigest: fixture.plan.Digest, PreflightRejection: managedkubernetes.ReasonPortForwardProfileUnsupported})
	if err != nil || receipt.State != enum.KubernetesInvocationFailed || !receipt.CompletionRecorded {
		t.Fatal("persist owner-derived unsupported profile", err)
	}
	var reason string
	var attempts, streams int64
	if err := fixture.setup.repository.pool.QueryRow(ctx, queryKubernetesPFPreflightTestEvidence, fixture.binding.InvocationRef).Scan(&reason, &attempts, &streams); err != nil || reason != managedkubernetes.ReasonPortForwardProfileUnsupported || attempts != 0 || streams != 0 {
		t.Fatal("preflight rejection created native attempt or lost canonical reason", err)
	}
	actor := fixture.actor
	actor.Permission = "platform.integrations.kubernetes.tool.get"
	view, err := fixture.service.GetKubernetesTool(ctx, actor, fixture.binding.InvocationRef)
	if err != nil || view.SafeReasonCode != enum.KubernetesToolReasonPortForwardProfileUnsupported || view.State != "FAILED" {
		t.Fatal("no-stream Tool Get lost the persisted closed profile reason", err)
	}
	actor.Permission = "platform.integrations.kubernetes.tool.list"
	page, err := fixture.service.ListKubernetesTools(ctx, actor, query.KubernetesToolList{RunRef: view.RunRef, NodeRef: view.NodeRef, Page: query.Page{Size: 20}})
	if err != nil || len(page.Items) != 1 || page.Items[0].SafeReasonCode != view.SafeReasonCode {
		t.Fatal("no-stream Tool List lost exact owner reason", err)
	}

}
