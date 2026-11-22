package kubernetesoperation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// streamPlanFixture joins existing synthetic owner ports and captures completion without target effects.
// streamPlanFixture объединяет synthetic owner ports и фиксирует completion без target effects.
type streamPlanFixture struct {
	*resourcePlanFixture
	*activeOwnerFixture
	*streamResolverFixture
	grant             value.KubernetesStreamGrant
	opens             int
	openErr           error
	completions       chan targetrepo.EffectCompletion
	cleanup           *targetrepo.StreamCleanupReceipt
	cleanupErr        error
	cleanupCalls      int
	completionReceipt func(targetrepo.EffectReceipt) targetrepo.EffectReceipt
}

// OpenKubernetesStream records one already-observed handshake and can lose only its owner acknowledgement.
// OpenKubernetesStream фиксирует наблюдённый handshake и может потерять только owner acknowledgement.
func (owner *streamPlanFixture) OpenKubernetesStream(_ context.Context, _ managedkubernetes.Binding, input targetrepo.StreamOpening) (value.KubernetesStreamGrant, error) {
	owner.opens++
	if input.HandshakeReceipt.Validate() != nil {
		return value.KubernetesStreamGrant{}, targetrepo.ErrOperationUnavailable
	}
	return owner.grant, owner.openErr
}

// CompleteKubernetesEffect copies exact submitted bytes before the caller clears private buffers.
// CompleteKubernetesEffect копирует submitted bytes до очистки private buffers вызывающим кодом.
func (owner *streamPlanFixture) CompleteKubernetesEffect(ctx context.Context, _ managedkubernetes.Binding, input targetrepo.EffectCompletion) (targetrepo.EffectReceipt, error) {
	if ctx.Err() != nil {
		return targetrepo.EffectReceipt{}, ctx.Err()
	}
	input.ResultJSON = bytes.Clone(input.ResultJSON)
	owner.completions <- input
	digest := resourceResultDigest(input.ResultJSON)
	receipt := targetrepo.EffectReceipt{State: input.Outcome, ResultDigest: digest, SubmissionDigest: digest, Applied: true}
	if owner.completionReceipt != nil {
		receipt = owner.completionReceipt(receipt)
	}
	return receipt, nil
}

// streamPreparationFixture records preparation and opening while real ActiveStream workers own its transport.
// streamPreparationFixture фиксирует preparation/opening; transport принадлежит реальным ActiveStream workers.
type streamPreparationFixture struct {
	pin                                       targetrepo.PodStreamPreparationBinding
	target                                    *streamTargetFixture
	transport                                 *openedStreamFixture
	observations, preparations, opens, closes int
	openErr                                   error
}

// ObserveKubernetesPod proposes observed coordinates without an invented owner-selected NodeUID.
// ObserveKubernetesPod предлагает observed coordinates без выдуманного owner-selected NodeUID.
func (factory *streamPreparationFixture) ObserveKubernetesPod(context.Context, value.KubernetesOperationSnapshot) (managedkubernetes.ChildCandidate, error) {
	factory.observations++
	candidate := factory.pin.Observation
	witness := *candidate.PodWitness
	witness.NodeUID = ""
	candidate.PodWitness = &witness
	return candidate, nil
}

// PrepareKubernetesPodStream counts private preparation without creating an external effect.
// PrepareKubernetesPodStream учитывает private preparation без создания external effect.
func (factory *streamPreparationFixture) PrepareKubernetesPodStream(context.Context, value.KubernetesOperationSnapshot, targetrepo.OperationChild) (targetrepo.PodStreamPreparation, error) {
	factory.preparations++
	return factory, nil
}

// Binding supplies the same exact observation independently of owner admission.
// Binding предоставляет ту же exact observation независимо от owner admission.
func (factory *streamPreparationFixture) Binding() (targetrepo.PodStreamPreparationBinding, bool) {
	return factory.pin, true
}

// TargetAccess exposes fresh synthetic target eligibility, never credential material.
// TargetAccess предоставляет fresh synthetic target eligibility без credential material.
func (factory *streamPreparationFixture) TargetAccess() targetrepo.StreamTargetAccess {
	return factory.target
}

// Open counts a single handshake and separates request cancellation from transport lifetime.
// Open учитывает handshake и отделяет request cancellation от transport lifetime.
func (factory *streamPreparationFixture) Open(_ context.Context, attempt context.Context, _ managedkubernetes.Binding, _ targetrepo.EffectAdmission) (targetrepo.OpenedPodStream, managedkubernetes.HandshakeReceipt, error) {
	factory.opens++
	if attempt.Err() != nil {
		return nil, managedkubernetes.HandshakeReceipt{}, attempt.Err()
	}
	if factory.openErr != nil {
		return nil, managedkubernetes.HandshakeReceipt{}, factory.openErr
	}
	return factory.transport, factory.transport.handshake, nil
}

// Close records preparation cleanup without closing a transferred transport.
// Close фиксирует preparation cleanup без закрытия переданного transport.
func (factory *streamPreparationFixture) Close() { factory.closes++ }

// openedStreamFixture adds immutable handshake metadata to the existing blocked-reader transport fixture.
// openedStreamFixture дополняет blocked-reader transport fixture неизменяемыми handshake metadata.
type openedStreamFixture struct {
	*activeTransportFixture
	handshake managedkubernetes.HandshakeReceipt
}

// HandshakeReceipt returns only a witness bound to the supplied exact origin.
// HandshakeReceipt возвращает witness только для указанного exact origin.
func (transport *openedStreamFixture) HandshakeReceipt(origin managedkubernetes.StreamReceiptOrigin) (managedkubernetes.HandshakeReceipt, error) {
	if origin != transport.handshake.Origin {
		return managedkubernetes.HandshakeReceipt{}, targetrepo.ErrOperationUnavailable
	}
	return transport.handshake, nil
}

// streamOperationsFixture composes the production orchestrator and stream workers around synthetic owner/native boundaries.
// It cannot prove a live owner transaction, Kubernetes handshake, RPC wiring or installed execution.
// streamOperationsFixture собирает production orchestrator/workers вокруг synthetic owner/native boundaries.
// Fixture не доказывает live owner transaction, Kubernetes handshake, RPC wiring либо установленное выполнение.
func streamOperationsFixture(t *testing.T) (*StreamOperations, *streamPlanFixture, *streamPreparationFixture, managedkubernetes.Binding) {
	t.Helper()
	_, resolver, target, origin, grant := currentAccessFixture(t)
	snapshot := resolver.snapshot
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: snapshot.Input.Namespace, Name: snapshot.Input.Name, UID: snapshot.Input.UID, ResourceVersion: snapshot.Input.ResourceVersion}
	observation := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: address, ObservedResourceDigest: strings.Repeat("1", 64), RegisteredSchemaDigest: strings.Repeat("2", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "node-example", NodeUID: "11111111-1111-4111-8111-111111111111", Container: "main"}}
	child := targetrepo.OperationChild{Ref: "child_synthetic", Operation: snapshot.Operation, InputDigest: resourceResultDigest(snapshot.NormalizedInput), State: "READY", Resource: address, Input: snapshot.Input, NormalizedInput: snapshot.NormalizedInput, Observation: &observation,
		NodeTransport: &value.KubernetesNodeTransport{Ref: "transport_synthetic", TargetRef: snapshot.Target.ID, TargetRevision: int64(snapshot.Target.Revision), NodeName: observation.PodWitness.NodeName, NodeUID: observation.PodWitness.NodeUID, Protocol: managedkubernetes.WebSocketV5, ExpiresAt: time.Now().Add(time.Minute)}}
	plan := targetrepo.OperationPlan{Ref: snapshot.PlanRef, Digest: snapshot.PlanDigest, Revision: snapshot.PlanRevision, Children: []targetrepo.OperationChild{child}}
	grant.ExpiresAt = time.Now().Add(10 * time.Second)
	admission := targetrepo.EffectAdmission{EffectRef: origin.EffectRef, EffectGeneration: origin.EffectGeneration, ChildInputDigest: child.InputDigest, ExpiresAt: time.Now().Add(3 * time.Second), Stream: &grant}
	base := &resourceOwnerFixture{snapshot: snapshot, binding: resolver.binding, admission: admission}
	owner := &streamPlanFixture{resourcePlanFixture: &resourcePlanFixture{resourceOwnerFixture: base, plan: plan}, activeOwnerFixture: &activeOwnerFixture{expires: grant.ExpiresAt, closed: make(chan targetrepo.StreamClosing, 8)}, streamResolverFixture: resolver, grant: grant, completions: make(chan targetrepo.EffectCompletion, 8)}
	origin.ChildInputDigest = child.InputDigest
	handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: child.NodeTransport.Ref, TransportGeneration: 1, Protocol: string(managedkubernetes.WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("d", 64)}
	transport := &openedStreamFixture{activeTransportFixture: &activeTransportFixture{reads: make(chan streamReadEvent, 4), closed: make(chan struct{})}, handshake: handshake}
	factory := &streamPreparationFixture{pin: targetrepo.PodStreamPreparationBinding{SnapshotDigest: snapshot.Digest, ChildRef: child.Ref, Operation: child.Operation, InputDigest: child.InputDigest, TargetDigest: snapshot.Target.MetadataDigest(), SubjectDigest: snapshot.SubjectDigest, Observation: observation, HardDeadline: grant.ExpiresAt}, target: target, transport: transport}
	service, err := NewStreamOperations(t.Context(), owner, factory, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Error("stream fixture failed shutdown", err)
		}
	})
	return service, owner, factory, resolver.binding
}

// TestStreamOperationsGateAndLostAcknowledgement prevents replay and separates pending consent from native opening.
// TestStreamOperationsGateAndLostAcknowledgement запрещает replay и отделяет pending consent от native opening.
func TestStreamOperationsGateAndLostAcknowledgement(t *testing.T) {
	for _, variant := range []string{"gate", "lost-begin", "lost-open", "failed-handshake", "changed-handshake"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, factory, binding := streamOperationsFixture(t)
			switch variant {
			case "gate":
				owner.admission = targetrepo.EffectAdmission{GateRef: "gate_synthetic"}
			case "lost-begin":
				owner.beginErr = errors.New("synthetic lost begin ACK")
			case "lost-open":
				owner.openErr = errors.New("synthetic lost open ACK")
			case "failed-handshake":
				factory.openErr = errors.New("synthetic handshake refused")
			case "changed-handshake":
				factory.transport.handshake.Origin.EffectRef = "effect_foreign"
			}
			result, err := service.Execute(t.Context(), binding)
			if variant == "gate" {
				if err != nil || result.Effect.GateRef == "" || factory.opens != 0 || owner.opens != 0 {
					t.Fatal("gate opened native stream")
				}
				return
			}
			if err == nil || result.Stream != nil || owner.begins != 1 {
				t.Fatal("uncertain opening fabricated descriptor or repeated Begin")
			}
			if variant == "lost-begin" {
				if factory.opens != 0 || len(owner.closed) != 0 {
					t.Fatal("lost Begin acknowledgement caused external action")
				}
				return
			}
			if factory.opens != 1 || len(owner.closed) != 1 || len(owner.completions) != 1 {
				t.Fatal("failed opening missing exact terminal completion")
			}
			closed := <-owner.closed
			if closed.RemoteOutcome != "UNKNOWN" || !closed.TransportJoined {
				t.Fatal("uncertain opening fabricated remote exit")
			}
			if (closed.ObservedHandshake != nil) != (variant == "lost-open") {
				t.Fatal("late handshake evidence lost or invented")
			}
		})
	}
}

// TestStreamOperationsSurvivesUnaryCancellation checks retained delivery, exact binding and terminal completion.
// TestStreamOperationsSurvivesUnaryCancellation проверяет retained delivery, exact binding и terminal completion.
func TestStreamOperationsSurvivesUnaryCancellation(t *testing.T) {
	service, owner, factory, binding := streamOperationsFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	result, err := service.Execute(ctx, binding)
	cancel()
	if err != nil || result.Stream == nil {
		t.Fatal("stream failed to open", err)
	}
	ref := result.Stream.Ref
	foreign := binding
	foreign.Generation++
	if _, err := service.Exchange(t.Context(), foreign, ref, 0, nil); err == nil {
		t.Fatal("foreign binding received stream")
	}
	factory.transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{Channel: "STDOUT", Payload: []byte("synthetic-output")}}
	first, err := service.Exchange(t.Context(), binding, ref, 0, nil)
	if err != nil || len(first.Output) != 1 {
		t.Fatal("unary cancellation killed owned stream")
	}
	second, err := service.Exchange(t.Context(), binding, ref, 0, nil)
	if err != nil || len(second.Output) != 1 || !bytes.Equal(first.Output[0].Payload, second.Output[0].Payload) {
		t.Fatal("lost unary response discarded retained output")
	}
	exit := int32(0)
	factory.transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
	terminal, err := service.Exchange(t.Context(), binding, ref, first.Output[0].Sequence, nil)
	if err != nil || terminal.RemoteOutcome != "SUCCEEDED" {
		t.Fatal("native terminal evidence lost")
	}
	select {
	case completion := <-owner.completions:
		if completion.Outcome != "SUCCEEDED" || completion.StreamRef != ref {
			t.Fatal("wrong child effect completed")
		}
	case <-time.After(time.Second):
		t.Fatal("native terminal never completed owner effect")
	}
	if factory.opens != 1 || owner.opens != 1 {
		t.Fatal("stream handshake replayed")
	}
}

// TestStreamOperationsHistoricalReadNeverReopens keeps owner recovery independent of private preparation.
// TestStreamOperationsHistoricalReadNeverReopens отделяет owner recovery от private preparation.
func TestStreamOperationsHistoricalReadNeverReopens(t *testing.T) {
	for _, variant := range []string{"pending", "accepted", "foreign-input"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, factory, binding := streamOperationsFixture(t)
			owner.plan.Children[0].State = "ATTEMPTING"
			owner.admission.ReconcileOnly = true
			owner.admission.Stream = nil
			if variant == "accepted" {
				accepted, err := managedkubernetes.BuildStreamEffectResult(failedOpenedStream(factory.transport.handshake))
				if err != nil {
					t.Fatal(err)
				}
				owner.admission.AcceptedResult = &targetrepo.AcceptedEffectResult{Outcome: accepted.Outcome, ReasonCode: accepted.ReasonCode, ResultJSON: accepted.ResultJSON, ResultDigest: accepted.ResultDigest}
			}
			if variant == "foreign-input" {
				owner.admission.ChildInputDigest = strings.Repeat("9", 64)
			}
			result, err := service.Execute(t.Context(), binding)
			if factory.observations != 0 || factory.preparations != 0 || factory.opens != 0 || owner.opens != 0 {
				t.Fatal("historical stream reopened or loaded private material")
			}
			if variant == "foreign-input" {
				if err == nil {
					t.Fatal("foreign historical effect accepted")
				}
				return
			}
			if err != nil || !result.Effect.ReconcileOnly || result.Stream != nil {
				t.Fatal("recovery manufactured fresh stream credit", err)
			}
			if (result.Effect.Result != nil) != (variant == "accepted") {
				t.Fatal("accepted result lost or invented")
			}
		})
	}
}

// ResolveKubernetesStreamCleanup models only an owner-issued irreversible closure receipt, never a new grant.
// ResolveKubernetesStreamCleanup моделирует лишь owner-issued irreversible closure receipt без нового grant.
func (owner *streamPlanFixture) ResolveKubernetesStreamCleanup(_ context.Context, binding managedkubernetes.Binding, ref string) (targetrepo.StreamCleanupReceipt, error) {
	owner.cleanupCalls++
	if binding != owner.resourceOwnerFixture.binding || ref != owner.grant.Ref || owner.cleanup == nil {
		return targetrepo.StreamCleanupReceipt{}, targetrepo.ErrOperationUnavailable
	}
	return *owner.cleanup, owner.cleanupErr
}
