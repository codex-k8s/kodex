package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// gatewayStreamBrokerFixture executes the real consumer RPC boundary with synthetic owned replies.
// gatewayStreamBrokerFixture исполняет real consumer RPC boundary с synthetic owned replies.
type gatewayStreamBrokerFixture struct {
	sb.ManagedKubernetesServiceClient
	exchanges, cancels atomic.Int64
	exchange           func(context.Context, *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error)
	cancel             func(context.Context, *sb.CancelManagedKubernetesOperationRequest) (*sb.CancelManagedKubernetesOperationResponse, error)
}

// ExchangeManagedKubernetesStream records each real attempt without implementing any hidden retry.
// ExchangeManagedKubernetesStream фиксирует каждый real attempt без hidden retry.
func (f *gatewayStreamBrokerFixture) ExchangeManagedKubernetesStream(ctx context.Context, in *sb.ExchangeManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	f.exchanges.Add(1)
	return f.exchange(ctx, in)
}

// CancelManagedKubernetesOperation exposes ordering and immutable cleanup pins to the test.
// CancelManagedKubernetesOperation открывает тесту ordering и immutable cleanup pins.
func (f *gatewayStreamBrokerFixture) CancelManagedKubernetesOperation(ctx context.Context, in *sb.CancelManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.CancelManagedKubernetesOperationResponse, error) {
	f.cancels.Add(1)
	return f.cancel(ctx, in)
}

// gatewayStreamPins supplies only synthetic original-owner metadata and bounded channels.
// gatewayStreamPins выдаёт только synthetic original-owner metadata и bounded channels.
func gatewayStreamPins() (*sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor) {
	binding := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "private_fixture_fence", Generation: 7, InputDigest: strings.Repeat("a", 64)}
	stream := &sb.ManagedKubernetesStreamDescriptor{StreamRef: "stream_fixture", InvocationRef: binding.InvocationRef, Generation: binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 32, MaximumTotalBytes: 128, InputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN}, OutputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}}
	return binding, stream
}

// gatewayStreamFrame hashes independent private fixture bytes with the supplied direction channel.
// gatewayStreamFrame хеширует independent private fixture bytes с переданным channel направления.
func gatewayStreamFrame(sequence uint64, channel sb.ManagedKubernetesChannel, payload string) *sb.ManagedKubernetesFrame {
	digest := sha256.Sum256([]byte(payload))
	return &sb.ManagedKubernetesFrame{Sequence: sequence, Channel: channel, Payload: []byte(payload), PayloadDigest: hex.EncodeToString(digest[:])}
}

// gatewayStreamReply creates one freshly credited active output frame, not a target or owner proof.
// gatewayStreamReply создаёт один freshly credited active output frame, не target либо owner proof.
func gatewayStreamReply() *sb.ExchangeManagedKubernetesStreamResponse {
	reply := &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING, Output: []*sb.ManagedKubernetesFrame{gatewayStreamFrame(1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT, "useful-output")}, AuthorizedUntil: timestamppb.New(time.Now().Add(time.Second))}
	reply.Output[0].DeliveryOrdinal = 1
	return reply
}

// gatewayStreamClosure is a synthetic authoritative confirmation callback, never a clock-only production implementation.
// gatewayStreamClosure — synthetic callback authoritative confirmation, не clock-only production implementation.
func gatewayStreamClosure(context.Context, *sb.ManagedKubernetesInvocationBinding, string) error {
	return nil
}

// newGatewayStream constructs a consumer using explicit accepted in-memory fixture output and cleanup ports.
// newGatewayStream создаёт consumer с явными accepted in-memory fixture output и cleanup ports.
func newGatewayStream(t *testing.T, broker *gatewayStreamBrokerFixture, sink KubernetesStreamOutputSink) *KubernetesStreamConsumer {
	t.Helper()
	binding, stream := gatewayStreamPins()
	consumer, err := NewKubernetesStreamConsumer(binding, stream, "kubernetes.exec", broker, sink, gatewayStreamClosure)
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

// TestKubernetesStreamAcceptBeforeAck preserves actual output and immutable binding before the next exact ACK.
// TestKubernetesStreamAcceptBeforeAck сохраняет actual output и immutable binding до следующего exact ACK.
func TestKubernetesStreamAcceptBeforeAck(t *testing.T) {
	binding, descriptor := gatewayStreamPins()
	original := proto.Clone(binding)
	broker := &gatewayStreamBrokerFixture{}
	var accepted strings.Builder
	sink := func(ctx context.Context, originalBinding *sb.ManagedKubernetesInvocationBinding, originalDescriptor *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
		defer clear(out.Payload)
		if ctx.Err() != nil || out.StreamRef != "stream_fixture" || out.Sequence != 1 || out.DeliveryOrdinal != 1 || out.BindingDigest == "" || !out.AuthorizedUntil.After(time.Now()) || broker.exchanges.Load() != 1 {
			t.Fatal("output sink lost original scope or acceptance order")
		}
		if !proto.Equal(originalBinding, original) || originalDescriptor.OutputChannels[0] != sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT {
			t.Fatal("sink lost immutable original owner metadata")
		}
		originalBinding.Fence = "sink mutation"
		originalDescriptor.OutputChannels[0] = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR
		accepted.Write(out.Payload)
		return nil
	}
	consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, sink, gatewayStreamClosure)
	if err != nil {
		t.Fatal(err)
	}
	binding.Fence = "changed"
	descriptor.OutputChannels[0] = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR
	broker.exchange = func(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
		if !proto.Equal(request.Binding, original) || request.StreamRef != "stream_fixture" {
			t.Fatal("original binding changed")
		}
		if broker.exchanges.Load() == 1 {
			if request.AcknowledgedOutputSequence != 0 {
				t.Fatal("output acknowledged before sink acceptance")
			}
			return gatewayStreamReply(), nil
		}
		if request.AcknowledgedOutputSequence != 1 || accepted.String() != "useful-output" {
			t.Fatal("accepted output was discarded or not acknowledged")
		}
		return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, ReasonCode: "REMOTE_EXIT"}, nil
	}
	if _, err := consumer.Exchange(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	progress, err := consumer.Exchange(t.Context(), nil)
	if err != nil || progress.RemoteOutcome != sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED || progress.AcceptedOutput != 1 {
		t.Fatalf("terminal receipt rejected: %v", err)
	}
	if _, err := consumer.Exchange(t.Context(), nil); err == nil || broker.exchanges.Load() != 2 {
		t.Fatal("terminal stream repeated an exchange")
	}
	if strings.Contains(fmt.Sprintf("%#v %#v", consumer, KubernetesStreamOutput{Payload: []byte("PRIVATE")}), "PRIVATE") || strings.Contains(fmt.Sprintf("%#v", consumer), "private_fixture_fence") {
		t.Fatal("diagnostics exposed private bytes")
	}
}

// TestKubernetesStreamRejectsUnsafeDescriptors closes missing callback and foreign/ambiguous descriptor paths before I/O.
// TestKubernetesStreamRejectsUnsafeDescriptors закрывает missing callback и foreign/ambiguous descriptors до I/O.
func TestKubernetesStreamRejectsUnsafeDescriptors(t *testing.T) {
	for _, variant := range []string{"sink", "closure", "invocation", "generation", "deadline", "future", "timestamp-unknown", "descriptor-unknown", "duplicate-channel", "input-direction", "output-direction", "unknown-channel", "budget", "frame-budget", "health"} {
		t.Run(variant, func(t *testing.T) {
			binding, stream := gatewayStreamPins()
			sink := KubernetesStreamOutputSink(func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				clear(out.Payload)
				return nil
			})
			closure := KubernetesStreamClosure(gatewayStreamClosure)
			switch variant {
			case "sink":
				sink = nil
			case "closure":
				closure = nil
			case "invocation":
				stream.InvocationRef = "foreign"
			case "generation":
				stream.Generation++
			case "deadline":
				stream.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "future":
				stream.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
			case "timestamp-unknown":
				stream.ExpiresAt.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "descriptor-unknown":
				stream.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "duplicate-channel":
				stream.OutputChannels = append(stream.OutputChannels, stream.OutputChannels[0])
			case "input-direction":
				stream.InputChannels = []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}
			case "output-direction":
				stream.OutputChannels = []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN}
			case "unknown-channel":
				stream.OutputChannels[0] = 999
			case "budget":
				stream.MaximumTotalBytes = 1
			case "frame-budget":
				stream.MaximumFrameBytes = 32769
			case "health":
				binding.ConnectionTestRef = "test_fixture"
			}
			broker := &gatewayStreamBrokerFixture{}
			if _, err := NewKubernetesStreamConsumer(binding, stream, "kubernetes.exec", broker, sink, closure); err == nil || broker.exchanges.Load() != 0 {
				t.Fatal("unsafe descriptor admitted")
			}
		})
	}
}

// TestKubernetesStreamInvalidReplyNeverReachesSink checks whole-response validation and permanent uncertainty without replay.
// TestKubernetesStreamInvalidReplyNeverReachesSink проверяет whole-response validation и permanent uncertainty без replay.
func TestKubernetesStreamInvalidReplyNeverReachesSink(t *testing.T) {
	for _, variant := range []string{"nil", "unknown", "input-ack", "sequence", "digest", "channel", "frame-unknown", "empty", "oversize", "frames", "eof", "credit-missing", "credit-expired", "credit-future", "credit-unknown", "credit-empty", "state", "outcome", "reason", "terminal-output", "combined-bytes", "sequence-overflow", "ordinal-zero", "ordinal-excessive", "ordinal-overflow"} {
		t.Run(variant, func(t *testing.T) {
			reply := gatewayStreamReply()
			broker := &gatewayStreamBrokerFixture{}
			var accepts int
			consumer := newGatewayStream(t, broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				accepts++
				clear(out.Payload)
				return nil
			})
			switch variant {
			case "nil":
				reply = nil
			case "unknown":
				reply.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "input-ack":
				reply.AcknowledgedInputSequence = 1
			case "sequence":
				reply.Output[0].Sequence = 2
			case "digest":
				reply.Output[0].PayloadDigest = strings.Repeat("0", 64)
			case "channel":
				reply.Output[0].Channel = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR
			case "frame-unknown":
				reply.Output[0].ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "empty":
				reply.Output[0] = gatewayStreamFrame(1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT, "")
				reply.Output[0].DeliveryOrdinal = 1
			case "oversize":
				reply.Output[0] = gatewayStreamFrame(1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT, strings.Repeat("x", 33))
				reply.Output[0].DeliveryOrdinal = 1
			case "frames":
				reply.Output = append(reply.Output, proto.Clone(reply.Output[0]).(*sb.ManagedKubernetesFrame))
			case "eof":
				reply.Output[0].EndOfInput = true
			case "credit-missing":
				reply.AuthorizedUntil = nil
			case "credit-expired":
				reply.AuthorizedUntil = timestamppb.New(time.Now().Add(-time.Second))
			case "credit-future":
				reply.AuthorizedUntil = timestamppb.New(time.Now().Add(time.Minute))
			case "credit-unknown":
				reply.AuthorizedUntil.ProtoReflect().SetUnknown([]byte{0x78, 1})
			case "credit-empty":
				reply.Output = nil
			case "state":
				reply.State = 999
			case "outcome":
				reply.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
			case "reason":
				reply.ReasonCode = "PRIVATE_PROVIDER_MESSAGE"
			case "terminal-output":
				reply.State = sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED
				reply.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
				reply.ReasonCode = "REMOTE_EXIT"
			case "ordinal-zero":
				reply.Output[0].DeliveryOrdinal = 0
			case "ordinal-excessive":
				reply.Output[0].DeliveryOrdinal = 5
			case "ordinal-overflow":
				reply.Output[0].DeliveryOrdinal = ^uint32(0)
			case "combined-bytes":
				consumer.totalBytes = consumer.descriptor.MaximumTotalBytes - 1
			case "sequence-overflow":
				reply.Output[0].Sequence = managedkubernetes.MaximumFrameSequence + 1
			}
			broker.exchange = func(context.Context, *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				return reply, nil
			}
			if _, err := consumer.Exchange(t.Context(), nil); err == nil || accepts != 0 {
				t.Fatal("invalid reply reached accepted-output sink")
			}
			if _, err := consumer.Exchange(t.Context(), nil); err == nil || broker.exchanges.Load() != 1 {
				t.Fatal("uncertain stream was retried")
			}
		})
	}
}

// TestKubernetesStreamInputUncertaintyAndEOF never resends uncertain input and closes only the acknowledged input channel.
// TestKubernetesStreamInputUncertaintyAndEOF не повторяет uncertain input и закрывает только acknowledged input channel.
func TestKubernetesStreamInputUncertaintyAndEOF(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			broker := &gatewayStreamBrokerFixture{}
			consumer := newGatewayStream(t, broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				clear(out.Payload)
				return nil
			})
			broker.exchange = func(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				if request.Input == nil || request.Input.Sequence != 1 || !request.Input.EndOfInput {
					t.Fatal("input changed")
				}
				if uncertain {
					return nil, errors.New("PRIVATE_FAILURE")
				}
				return &sb.ExchangeManagedKubernetesStreamResponse{AcknowledgedInputSequence: 1, State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING}, nil
			}
			input := gatewayStreamFrame(1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, "")
			input.EndOfInput = true
			_, err := consumer.Exchange(t.Context(), input)
			if (err != nil) != uncertain {
				t.Fatalf("unexpected input receipt: %v", err)
			}
			next := gatewayStreamFrame(2, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, "new")
			if _, err := consumer.Exchange(t.Context(), next); err == nil || broker.exchanges.Load() != 1 {
				t.Fatal("closed or uncertain input repeated")
			}
			if uncertain {
				var unknown *UnknownOutcomeError
				if !errors.As(err, &unknown) {
					t.Fatal("uncertain input did not retain UNKNOWN")
				}
			}
		})
	}
}

// TestKubernetesStreamSinkFailureOrExpiryWithholdsAck prevents accepted-output promotion after sink error or credit expiry.
// TestKubernetesStreamSinkFailureOrExpiryWithholdsAck запрещает accepted-output promotion после sink error либо credit expiry.
func TestKubernetesStreamSinkFailureOrExpiryWithholdsAck(t *testing.T) {
	for _, variant := range []string{"error", "slow", "ack-expired"} {
		t.Run(variant, func(t *testing.T) {
			broker := &gatewayStreamBrokerFixture{}
			reply := gatewayStreamReply()
			reply.AuthorizedUntil = timestamppb.New(time.Now().Add(50 * time.Millisecond))
			broker.exchange = func(context.Context, *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				return reply, nil
			}
			sink := func(ctx context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				defer clear(out.Payload)
				if variant == "slow" {
					<-ctx.Done()
					return nil
				}
				if variant == "error" {
					return errors.New("PRIVATE_SINK_ERROR")
				}
				return nil
			}
			consumer := newGatewayStream(t, broker, sink)
			_, err := consumer.Exchange(t.Context(), nil)
			if variant == "ack-expired" {
				if err != nil {
					t.Fatal(err)
				}
				wait, cancel := context.WithDeadline(t.Context(), reply.AuthorizedUntil.AsTime())
				defer cancel()
				<-wait.Done()
			} else if err == nil || consumer.acceptedOutput != 0 {
				t.Fatal("failed or expired sink became accepted")
			}
			if _, err := consumer.Exchange(t.Context(), nil); err == nil || broker.exchanges.Load() != 1 {
				t.Fatal("invalid sink credit reached ACK RPC")
			}
		})
	}
}

// TestKubernetesStreamTerminalReasonsCannotPromoteSuccess rejects terminal reasons that contradict a claimed completed outcome.
// TestKubernetesStreamTerminalReasonsCannotPromoteSuccess отклоняет terminal reasons, противоречащие completed outcome.
func TestKubernetesStreamTerminalReasonsCannotPromoteSuccess(t *testing.T) {
	for _, fixture := range []struct {
		outcome sb.ManagedKubernetesRemoteOutcome
		reason  string
		valid   bool
	}{
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, "REMOTE_EXIT", true},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, "CLEAN_EOF", true},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED, "REMOTE_FAILURE", true},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, "REMOTE_FAILURE", false},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, "CANCELED", false},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, "TRANSPORT_LOST", false},
		{sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED, "CLEAN_EOF", false},
	} {
		if validKubernetesStreamState(sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, fixture.outcome, fixture.reason) != fixture.valid {
			t.Fatal("completed outcome contradicted terminal reason")
		}
	}
	if !validKubernetesStreamCleanup(&sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, TransportJoined: true}) {
		t.Fatal("separate completed cleanup fact was rejected")
	}
}

// TestKubernetesStreamSequenceDoesNotLimitUsefulLifetime reuses retained owner cursors with one tiny
// next frame, preserving byte budgets without allocating a large history or permitting sequence gaps.
// TestKubernetesStreamSequenceDoesNotLimitUsefulLifetime использует retained owner cursors с одним
// tiny next frame, сохраняя byte budgets без large history allocation либо sequence gaps.
func TestKubernetesStreamSequenceDoesNotLimitUsefulLifetime(t *testing.T) {
	for _, previous := range []uint64{4096, 1 << 20} {
		reply := gatewayStreamReply()
		reply.AcknowledgedInputSequence = previous + 1
		reply.Output[0].Sequence = previous + 1
		broker := &gatewayStreamBrokerFixture{exchange: func(context.Context, *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
			return reply, nil
		}}
		accepted := 0
		consumer := newGatewayStream(t, broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
			accepted++
			clear(out.Payload)
			return nil
		})
		consumer.acknowledgedInput = previous
		consumer.acceptedOutput = previous
		consumer.totalBytes = 2 * previous
		consumer.descriptor.MaximumTotalBytes = 16 << 20
		input := gatewayStreamFrame(previous+1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, "x")
		progress, err := consumer.Exchange(t.Context(), input)
		if err != nil || accepted != 1 || progress.AcknowledgedInput != previous+1 || progress.AcceptedOutput != previous+1 {
			t.Fatal("useful frame rejected by lifetime count", previous, err)
		}
	}
}
