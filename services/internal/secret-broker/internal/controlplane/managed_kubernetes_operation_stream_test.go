package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// streamClientFixture substitutes only RPC responses, never the stream transport or owner ledger.
// streamClientFixture заменяет только RPC responses, не stream transport либо owner ledger.
type streamClientFixture struct {
	frameClientFixture
	opening       *cp.OpenManagedKubernetesStreamResponse
	closing       *cp.CloseManagedKubernetesStreamResponse
	openRequest   *cp.OpenManagedKubernetesStreamRequest
	closeRequest  *cp.CloseManagedKubernetesStreamRequest
	opens, closes int
}

// OpenManagedKubernetesStream captures one exact first-handshake receipt without reopening a connection.
// OpenManagedKubernetesStream фиксирует exact first-handshake receipt без повторного открытия connection.
func (client *streamClientFixture) OpenManagedKubernetesStream(_ context.Context, request *cp.OpenManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*cp.OpenManagedKubernetesStreamResponse, error) {
	client.opens++
	client.openRequest = request
	return client.opening, client.err
}

// CloseManagedKubernetesStream returns independent owner state so tests cannot equate intent with fact.
// CloseManagedKubernetesStream возвращает независимое owner state, отделяя intent от fact.
func (client *streamClientFixture) CloseManagedKubernetesStream(_ context.Context, request *cp.CloseManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*cp.CloseManagedKubernetesStreamResponse, error) {
	client.closes++
	client.closeRequest = request
	return client.closing, client.err
}

// streamAdapterFixture supplies finite prepared credit and a synthetic handshake digest.
// streamAdapterFixture задаёт конечный prepared credit и synthetic handshake digest.
func streamAdapterFixture(t *testing.T) (*Owner, *streamClientFixture, managedkubernetes.Binding, targetrepo.StreamOpening) {
	t.Helper()
	owner, base, binding, _ := frameAdapterFixture()
	client := &streamClientFixture{frameClientFixture: *base, opening: &cp.OpenManagedKubernetesStreamResponse{Stream: &cp.ManagedKubernetesOwnerStreamGrant{StreamRef: "stream_synthetic", Generation: binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 1024, MaximumTotalBytes: 8192, InputChannels: []string{"STDIN"}, OutputChannels: []string{"STDOUT"}}}, closing: &cp.CloseManagedKubernetesStreamResponse{State: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_REVOKED, RemoteOutcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN}}
	owner.client.KubernetesOperations = client
	grant, valid := kubernetesStreamGrantFromProto(client.opening.Stream)
	if !valid {
		t.Fatal("fixture grant invalid")
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	receipt := managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: bindingDigest, ChildInputDigest: strings.Repeat("b", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: grant.Ref, StreamGeneration: binding.Generation, Operation: "kubernetes.exec", TargetRef: "target_synthetic", TargetRevision: 1}, TransportRef: "pin_synthetic", TransportGeneration: 1, Protocol: string(managedkubernetes.WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("c", 64)}
	digest, err := receipt.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return owner, client, binding, targetrepo.StreamOpening{EffectRef: "effect_synthetic", EffectGeneration: 1, HandshakeReceiptDigest: digest, HandshakeReceipt: receipt, Prepared: grant}
}

// streamClosingFixture attaches a recomputable observed receipt to the exact requested terminal state.
// streamClosingFixture прикрепляет recomputable observed receipt к exact requested terminal state.
func streamClosingFixture(t *testing.T, opening targetrepo.StreamOpening, succeeded bool) targetrepo.StreamClosing {
	t.Helper()
	receipt := managedkubernetes.TerminalReceipt{Origin: opening.HandshakeReceipt.Origin, HandshakeDigest: opening.HandshakeReceiptDigest, Protocol: opening.HandshakeReceipt.Protocol, TransportJoined: true, LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "TRANSPORT_LOST"}
	if succeeded {
		zero := int32(0)
		receipt.ExitCode = &zero
		receipt.LocalState = "COMPLETED"
		receipt.RemoteOutcome = "SUCCEEDED"
		receipt.ReasonCode = "REMOTE_EXIT"
	}
	digest, err := receipt.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return targetrepo.StreamClosing{StreamRef: opening.Prepared.Ref, ExpectedState: "ACTIVE", TerminalState: receipt.LocalState, RemoteOutcome: receipt.RemoteOutcome, TransportJoined: true, ObservedExitCode: receipt.ExitCode, TerminalReceiptDigest: digest, TerminalReceipt: receipt}
}

// TestStreamOpenPreservesPreparedCredit rejects widened limits and foreign stream bindings.
// TestStreamOpenPreservesPreparedCredit отклоняет расширенные limits и чужие stream bindings.
func TestStreamOpenPreservesPreparedCredit(t *testing.T) {
	for _, scenario := range []string{"exact", "lost", "ref", "generation", "deadline", "frame-limit", "total-limit", "channel", "duplicate-channel", "unknown-field", "nested-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, input := streamAdapterFixture(t)
			switch scenario {
			case "lost":
				client.err = errors.New("synthetic lost handshake receipt")
			case "ref":
				client.opening.Stream.StreamRef = "foreign_stream"
			case "generation":
				client.opening.Stream.Generation++
			case "deadline":
				client.opening.Stream.ExpiresAt = timestamppb.New(input.Prepared.ExpiresAt.Add(time.Second))
			case "frame-limit":
				client.opening.Stream.MaximumFrameBytes++
			case "total-limit":
				client.opening.Stream.MaximumTotalBytes++
			case "channel":
				client.opening.Stream.OutputChannels = []string{"STDERR"}
			case "duplicate-channel":
				client.opening.Stream.OutputChannels = []string{"STDOUT", "STDOUT"}
			case "unknown-field":
				client.opening.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "nested-unknown":
				client.opening.Stream.ProtoReflect().SetUnknown([]byte{0x78, 1})
			}
			grant, err := owner.OpenKubernetesStream(t.Context(), binding, input)
			if client.opens != 1 {
				t.Fatal("handshake receipt retried")
			}
			if scenario != "exact" {
				if err == nil || grant.Ref != "" {
					t.Fatal("changed or uncertain grant accepted")
				}
				return
			}
			if err != nil || grant.Ref != input.Prepared.Ref || client.openRequest.GetBinding().GetFence() != binding.Fence || client.openRequest.GetEffectGeneration() != input.EffectGeneration || client.openRequest.GetHandshakeReceiptDigest() != input.HandshakeReceiptDigest {
				t.Fatal("exact handshake pins changed")
			}
			if client.openRequest.GetHandshakeReceipt().GetOrigin().GetBindingDigest() != input.HandshakeReceipt.Origin.BindingDigest || client.openRequest.GetHandshakeReceipt().GetPeerSpkiSha256() != input.HandshakeReceipt.PeerSPKISHA256 {
				t.Fatal("typed handshake evidence missing")
			}
			client.opening.Stream.OutputChannels[0] = "STDERR"
			if grant.OutputChannels[0] != "STDOUT" {
				t.Fatal("RPC buffer aliases immutable grant")
			}
		})
	}
}

// TestStreamCloseOwnerRevocationWins prevents local join or requested completion from inventing success.
// TestStreamCloseOwnerRevocationWins исключает выдуманный success из local join либо requested completion.
func TestStreamCloseOwnerRevocationWins(t *testing.T) {
	owner, client, binding, opening := streamAdapterFixture(t)
	input := streamClosingFixture(t, opening, true)
	client.closing.RemoteOutcome = cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED
	receipt, err := owner.CloseKubernetesStream(t.Context(), binding, input)
	if err != nil || receipt.State != "REVOKED" || receipt.RemoteOutcome != "SUCCEEDED" || client.closes != 1 {
		t.Fatal("owner revocation replaced by local success")
	}
	if client.closeRequest.GetBinding().GetFence() != binding.Fence || !client.closeRequest.GetTransportJoined() || client.closeRequest.ObservedExitCode == nil || client.closeRequest.GetTerminalReceiptDigest() != input.TerminalReceiptDigest {
		t.Fatal("terminal receipt lost provenance")
	}
	if client.closeRequest.GetTerminalReceipt().GetOrigin().GetBindingDigest() != input.TerminalReceipt.Origin.BindingDigest || client.closeRequest.GetTerminalReceipt().ExitCode == nil || client.closeRequest.GetTerminalReceipt().GetExitCode() != 0 || client.closeRequest.GetTerminalReceipt().GetHandshakeDigest() != opening.HandshakeReceiptDigest {
		t.Fatal("typed terminal receipt lost zero exit or origin")
	}
}

// TestStreamCloseCannotRewriteRemoteEvidence preserves observed exit independently of local revocation.
// TestStreamCloseCannotRewriteRemoteEvidence сохраняет observed exit независимо от local revocation.
func TestStreamCloseCannotRewriteRemoteEvidence(t *testing.T) {
	for _, observed := range []string{"SUCCEEDED", "FAILED", "UNKNOWN"} {
		for _, reply := range []string{"SUCCEEDED", "FAILED", "UNKNOWN"} {
			t.Run(observed+"/"+reply, func(t *testing.T) {
				owner, client, binding, opening := streamAdapterFixture(t)
				input := streamClosingFixture(t, opening, observed != "UNKNOWN")
				if observed == "FAILED" {
					one := int32(1)
					input.RemoteOutcome, input.TerminalReceipt.RemoteOutcome = observed, observed
					input.ObservedExitCode, input.TerminalReceipt.ExitCode = &one, &one
					var err error
					input.TerminalReceiptDigest, err = input.TerminalReceipt.Digest()
					if err != nil {
						t.Fatal(err)
					}
				}
				client.closing.RemoteOutcome, _ = kubernetesEffectOutcomeToProto(reply)
				receipt, err := owner.CloseKubernetesStream(t.Context(), binding, input)
				if observed == reply {
					if err != nil || receipt.State != "REVOKED" || receipt.RemoteOutcome != observed {
						t.Fatal("owner local revocation lost observed remote evidence")
					}
				} else if err == nil || receipt.State != "" || receipt.RemoteOutcome != "" {
					t.Fatal("close ACK rewrote remote evidence")
				}
				if client.closes != 1 {
					t.Fatal("close receipt retried")
				}
			})
		}
	}
}

// TestStreamCloseCannotPromoteIncompleteLocalDelivery preserves a known exit without claiming completed output delivery.
// TestStreamCloseCannotPromoteIncompleteLocalDelivery сохраняет known exit без выдуманного completed output delivery.
func TestStreamCloseCannotPromoteIncompleteLocalDelivery(t *testing.T) {
	for state, reason := range map[string]string{"LOST": "OUTPUT_LOST", "CANCELED": "CANCELED", "REVOKED": "REVOKED", "EXPIRED": "EXPIRED"} {
		t.Run(state, func(t *testing.T) {
			owner, client, binding, opening := streamAdapterFixture(t)
			input := streamClosingFixture(t, opening, true)
			input.TerminalState, input.TerminalReceipt.LocalState = state, state
			input.TerminalReceipt.ReasonCode = reason
			var err error
			input.TerminalReceiptDigest, err = input.TerminalReceipt.Digest()
			if err != nil {
				t.Fatal(err)
			}
			client.closing.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED
			client.closing.RemoteOutcome = cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED
			if receipt, err := owner.CloseKubernetesStream(t.Context(), binding, input); err == nil || receipt.State != "" || client.closes != 1 {
				t.Fatal("known remote exit promoted incomplete local delivery")
			}
		})
	}
}

// TestStreamCloseRejectsInventedExitAndNonterminalReadback keeps uncertain shutdown visibly uncertain.
// TestStreamCloseRejectsInventedExitAndNonterminalReadback сохраняет видимую uncertainty завершения.
func TestStreamCloseRejectsInventedExitAndNonterminalReadback(t *testing.T) {
	for _, scenario := range []string{"completed-unknown", "unjoined-completed", "wrong-exit", "exit-unknown", "active-reply", "completed-unknown-reply", "lost-reply"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, opening := streamAdapterFixture(t)
			input := streamClosingFixture(t, opening, false)
			switch scenario {
			case "completed-unknown":
				input.TerminalState = "COMPLETED"
			case "unjoined-completed":
				input.TerminalState = "COMPLETED"
				input.RemoteOutcome = "SUCCEEDED"
				input.TransportJoined = false
			case "wrong-exit":
				exit := int32(137)
				input.ObservedExitCode = &exit
				input.RemoteOutcome = "SUCCEEDED"
			case "exit-unknown":
				exit := int32(0)
				input.ObservedExitCode = &exit
			case "active-reply":
				client.closing.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE
			case "completed-unknown-reply":
				client.closing.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED
			case "lost-reply":
				client.err = errors.New("synthetic lost closure response")
			}
			receipt, err := owner.CloseKubernetesStream(t.Context(), binding, input)
			if err == nil || receipt.State != "" {
				t.Fatal("unproven terminal receipt accepted")
			}
			if !strings.HasSuffix(scenario, "reply") && client.closes != 0 {
				t.Fatal("invalid terminal intent sent to owner")
			}
		})
	}
}

// TestStreamTypedReceiptTamperingNeverCallsOwner rejects mixed origin and receipt intent locally.
// TestStreamTypedReceiptTamperingNeverCallsOwner отклоняет mixed origin и receipt intent до owner RPC.
func TestStreamTypedReceiptTamperingNeverCallsOwner(t *testing.T) {
	for _, scenario := range []string{"handshake-missing", "handshake-digest", "binding", "effect", "stream", "generation", "terminal-missing", "terminal-digest", "terminal-binding", "terminal-join"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, input := streamAdapterFixture(t)
			closing := streamClosingFixture(t, input, true)
			switch scenario {
			case "handshake-missing":
				input.HandshakeReceipt = managedkubernetes.HandshakeReceipt{}
			case "handshake-digest":
				input.HandshakeReceiptDigest = strings.Repeat("f", 64)
			case "binding":
				input.HandshakeReceipt.Origin.BindingDigest = strings.Repeat("f", 64)
				input.HandshakeReceiptDigest, _ = input.HandshakeReceipt.Digest()
			case "effect":
				input.HandshakeReceipt.Origin.EffectRef = "foreign_effect"
				input.HandshakeReceiptDigest, _ = input.HandshakeReceipt.Digest()
			case "stream":
				input.HandshakeReceipt.Origin.StreamRef = "foreign_stream"
				input.HandshakeReceiptDigest, _ = input.HandshakeReceipt.Digest()
			case "generation":
				input.HandshakeReceipt.Origin.StreamGeneration++
				input.HandshakeReceiptDigest, _ = input.HandshakeReceipt.Digest()
			case "terminal-missing":
				closing.TerminalReceipt = managedkubernetes.TerminalReceipt{}
			case "terminal-digest":
				closing.TerminalReceiptDigest = strings.Repeat("f", 64)
			case "terminal-binding":
				closing.TerminalReceipt.Origin.BindingDigest = strings.Repeat("f", 64)
				closing.TerminalReceiptDigest, _ = closing.TerminalReceipt.Digest()
			case "terminal-join":
				closing.TransportJoined = false
			}
			var err error
			if strings.HasPrefix(scenario, "terminal-") {
				_, err = owner.CloseKubernetesStream(t.Context(), binding, closing)
			} else {
				_, err = owner.OpenKubernetesStream(t.Context(), binding, input)
			}
			if err == nil || client.opens != 0 || client.closes != 0 {
				t.Fatal("mixed private receipt reached owner")
			}
		})
	}
}

// TestStreamCloseLateHandshakeKeepsExactNativeWitness rejects swapped late facts before owner RPC.
// TestStreamCloseLateHandshakeKeepsExactNativeWitness отклоняет подмену late facts до owner RPC.
func TestStreamCloseLateHandshakeKeepsExactNativeWitness(t *testing.T) {
	for _, variant := range []string{"exact", "foreign-origin", "changed-peer", "malformed"} {
		t.Run(variant, func(t *testing.T) {
			owner, client, binding, opening := streamAdapterFixture(t)
			closing := streamClosingFixture(t, opening, false)
			witness := opening.HandshakeReceipt
			closing.ObservedHandshake = &witness
			switch variant {
			case "foreign-origin":
				witness.Origin.EffectRef = "effect_foreign"
			case "changed-peer":
				witness.PeerSPKISHA256 = strings.Repeat("e", 64)
			case "malformed":
				witness.HTTPStatus = 200
			}
			_, err := owner.CloseKubernetesStream(t.Context(), binding, closing)
			if variant != "exact" {
				if err == nil || client.closes != 0 {
					t.Fatal("foreign late witness reached owner")
				}
				return
			}
			if err != nil || client.closes != 1 || client.closeRequest.GetObservedHandshake().GetOrigin().GetEffectRef() != witness.Origin.EffectRef || client.closeRequest.GetObservedHandshake().GetPeerSpkiSha256() != witness.PeerSPKISHA256 {
				t.Fatal("late native evidence was dropped")
			}
		})
	}
}
