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

// TestKubernetesInputOwnerComponent crosses public input and private one-attempt delivery using real
// launch/claim/Prepare/approval/stream ownership and versioned local storage. Native handshake metadata
// is synthetic: this proves owner durability and admission, not live TLS, signed policy108 or native writes.
// TestKubernetesInputOwnerComponent проходит public input и private one-attempt delivery через actual
// launch/claim/Prepare/approval/stream ownership и versioned local storage. Native handshake metadata
// синтетические: это проверка owner durability/admission, не live TLS, signed policy108 либо native writes.
func TestKubernetesInputOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
	defer cancel()
	fixture, service, connection, bindingMetadata := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, bindingMetadata)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-input-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable private input", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-input-agent", "Private input owner")
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.exec", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Write a reviewed private stdin frame"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-input-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 {
		t.Fatal("claim input runtime")
	}
	node := stringMap(runtime[0], "nodeRef")
	uid := "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
	payload := map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "uid": uid, "resourceVersion": "17", "options": map[string]any{"container": "main", "command": []string{"cat"}, "stdin": true, "tty": true, "timeoutSeconds": 120}}
	resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": node, "connection_ref": connection.Ref, "capability_key": "kubernetes.exec", "idempotency_key": "kube-input-invocation"}, payload)
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
	actor.Permission = "platform.integrations.kubernetes.input.list"
	page, err := service.ListKubernetesStreamInputs(ctx, actor, query.KubernetesStreamInputList{RunRef: run.Ref, NodeRef: node})
	if err != nil || len(page.Items) != 1 || page.Items[0].StreamRef != stream {
		t.Fatal("discover exact stream inputs", err)
	}
	actor.Permission = "platform.integrations.kubernetes.input.get"
	read := query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream}
	view, err := service.GetKubernetesStreamInput(ctx, actor, read)
	if err != nil {
		t.Fatal("get input view", err)
	}
	bytes := []byte("PRIVATE_STDIN_SENTINEL")
	sum := sha256.Sum256(bytes)
	submit := command.KubernetesStreamInputSubmit{InvocationRef: binding.InvocationRef, StreamRef: stream, Channel: "STDIN", Payload: bytes, PayloadDigest: hex.EncodeToString(sum[:])}
	mutation := value.Mutation{IdempotencyKey: "kube-input-source-1", ExpectedVersion: &view.View.Version}
	actor.Permission = "platform.integrations.kubernetes.input.submit"
	queued, err := service.SubmitKubernetesStreamInput(ctx, actor, mutation, submit)
	if err != nil || queued.Submission == nil || queued.Submission.State != enum.KubernetesInputQueued {
		t.Fatal("queue exact private input", err)
	}
	source := gateway
	source.Permission = "platform.runtime.kubernetes.input.claim"
	delivered, err := service.ClaimManagedKubernetesInputFrame(ctx, source, binding, stream)
	if err != nil || delivered.Disposition != enum.KubernetesInputFrameDeliver || delivered.Frame == nil || string(delivered.Frame.Payload) != string(bytes) {
		t.Fatal("claim exact private input", err)
	}
	defer clear(delivered.Frame.Payload)
	// Lost Claim reply must never reconstruct the bytes, even though the stored object still exists.
	// Потерянный Claim reply не восстанавливает bytes, хотя stored object существует.
	again, err := service.ClaimManagedKubernetesInputFrame(ctx, source, binding, stream)
	if err != nil || again.Disposition != enum.KubernetesInputFrameUnknown || again.Frame != nil {
		t.Fatal("input claim replayed private bytes", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	frame := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: delivered.Frame.Sequence, Channel: submit.Channel, PayloadDigest: submit.PayloadDigest, ByteCount: uint32(len(bytes))}
	foreign := frame
	foreign.PayloadDigest = strings.Repeat("f", 64)
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign input tuple admitted", err)
	}
	credit, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, frame)
	if err != nil || credit.Disposition != enum.KubernetesFrameSend || credit.ExpiresAt.After(delivered.Frame.DeliveryExpiresAt) {
		t.Fatal("input stage deadline not bound to native admission", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: frame.Sequence, PayloadDigest: frame.PayloadDigest}); err != nil {
		t.Fatal("input exact native ACK", err)
	}
	actor.Permission = "platform.integrations.kubernetes.input.get"
	read.SubmissionIdempotencyKey = mutation.IdempotencyKey
	confirmed, err := service.GetKubernetesStreamInput(ctx, actor, read)
	if err != nil || confirmed.Submission == nil || confirmed.Submission.State != enum.KubernetesInputAcknowledged || confirmed.View.AcknowledgedSequence != 1 {
		t.Fatal("lost Submit ACK exact metadata readback", err)
	}
	if confirmed.View.Pending != nil {
		t.Fatal("acknowledged input remained pending")
	}
	// Only the test cursor simulates a prior retained history; the next frame still crosses actual
	// Submit/Claim/Begin/Complete. No large allocation or fake result/history acceptance is used.
	// Только test cursor моделирует prior retained history; следующий frame проходит actual
	// Submit/Claim/Begin/Complete. Large allocation либо fake result/history acceptance не используются.
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.kubernetes_execution_streams SET input_sequence=4096 WHERE ref=$1`, stream); err != nil {
		t.Fatal("install retained input cursor", err)
	}
	// deliverInput exercises the same public/private ledger for typed resize and explicit stdin EOF.
	// deliverInput проверяет тот же public/private ledger для typed resize и explicit stdin EOF.
	deliverInput := func(channel string, payload []byte, eof, acknowledge bool, key string) entity.KubernetesStreamInputReadback {
		actor.Permission = "platform.integrations.kubernetes.input.get"
		currentRead, readErr := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream})
		if readErr != nil {
			t.Fatal(readErr)
		}
		sum := sha256.Sum256(payload)
		message := command.KubernetesStreamInputSubmit{InvocationRef: binding.InvocationRef, StreamRef: stream, Channel: channel, Payload: payload, PayloadDigest: hex.EncodeToString(sum[:]), EndOfInput: eof}
		actor.Permission = "platform.integrations.kubernetes.input.submit"
		submitted, submitErr := service.SubmitKubernetesStreamInput(ctx, actor, value.Mutation{IdempotencyKey: key, ExpectedVersion: &currentRead.View.Version}, message)
		if submitErr != nil || submitted.Submission == nil || submitted.Submission.State != enum.KubernetesInputQueued {
			t.Fatal("submit typed input source", channel, submitErr)
		}
		delivery, deliveryErr := service.ClaimManagedKubernetesInputFrame(ctx, source, binding, stream)
		if deliveryErr != nil || delivery.Frame == nil {
			t.Fatal("claim typed input source", channel, deliveryErr)
		}
		defer clear(delivery.Frame.Payload)
		if acknowledge {
			broker.Permission = "platform.runtime.kubernetes.frame.begin"
			begin := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: delivery.Frame.Sequence, Channel: channel, PayloadDigest: message.PayloadDigest, ByteCount: uint32(len(payload)), EndOfInput: eof}
			granted, beginErr := repository.BeginManagedKubernetesFrame(ctx, broker, binding, begin)
			if beginErr != nil || granted.Disposition != enum.KubernetesFrameSend {
				t.Fatal("admit typed staged input", channel, beginErr)
			}
			broker.Permission = "platform.runtime.kubernetes.frame.complete"
			if _, completeErr := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: begin.Sequence, PayloadDigest: begin.PayloadDigest}); completeErr != nil {
				t.Fatal("complete typed staged input", completeErr)
			}
		}
		actor.Permission = "platform.integrations.kubernetes.input.get"
		final, finalErr := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream, SubmissionIdempotencyKey: key})
		if finalErr != nil {
			t.Fatal(finalErr)
		}
		return final
	}
	resized := deliverInput("TERMINAL_RESIZE", []byte(`{"columns":80,"rows":24}`), false, true, "kube-input-resize-2")
	if resized.Submission.State != enum.KubernetesInputAcknowledged || resized.Submission.Sequence != 4097 {
		t.Fatal("resize receipt lost")
	}
	eof := deliverInput("STDIN", nil, true, true, "kube-input-eof-3")
	if len(eof.View.ClosedInputChannels) != 1 || eof.View.ClosedInputChannels[0] != "STDIN" {
		t.Fatal("stdin EOF did not close exact input channel")
	}
	pending := deliverInput("TERMINAL_RESIZE", []byte(`{"columns":100,"rows":30}`), false, false, "kube-input-resize-4")
	if pending.Submission.State != enum.KubernetesInputClaimed {
		t.Fatal("pending claim fact missing")
	}
	confirmed = pending
	actor.Permission = "platform.integrations.kubernetes.input.stop"
	stopped, err := service.StopKubernetesStream(ctx, actor, value.Mutation{IdempotencyKey: "kube-input-stop-1", ExpectedVersion: &confirmed.View.Version}, command.KubernetesStreamInputStop{InvocationRef: binding.InvocationRef, StreamRef: stream})
	if err != nil || stopped.State == enum.KubernetesStreamActive {
		t.Fatal("public exact stop did not close graph", err)
	}
	if stopped.Version <= confirmed.View.Version {
		t.Fatal("closure did not advance public input OCC")
	}
	actor.Permission = "platform.integrations.kubernetes.input.get"
	closed, err := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream, SubmissionIdempotencyKey: "kube-input-resize-4"})
	if err != nil || closed.Submission == nil || closed.Submission.State != enum.KubernetesInputUnknown {
		t.Fatal("closure erased claim uncertainty", err)
	}
}
