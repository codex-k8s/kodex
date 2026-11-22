package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesStreamOutputWitnessReachesAcceptedSink preserves each finite owner ordinal without inferring it from local sequences or retries.
// TestKubernetesStreamOutputWitnessReachesAcceptedSink сохраняет каждый finite owner ordinal без вывода из local sequences либо retries.
func TestKubernetesStreamOutputWitnessReachesAcceptedSink(t *testing.T) {
	for _, ordinal := range []uint32{1, 2, 3, 4} {
		t.Run(strconv.FormatUint(uint64(ordinal), 10), func(t *testing.T) {
			broker := &gatewayStreamBrokerFixture{}
			var accepted string
			broker.exchange = func(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				if broker.exchanges.Load() == 1 {
					if request.AcknowledgedOutputSequence != 0 {
						t.Fatal("output witness acknowledged before accepted sink")
					}
					reply := gatewayStreamReply()
					reply.Output[0].DeliveryOrdinal = ordinal
					encoded, err := proto.Marshal(reply)
					if err != nil {
						t.Fatal(err)
					}
					decoded := &sb.ExchangeManagedKubernetesStreamResponse{}
					if err := proto.Unmarshal(encoded, decoded); err != nil {
						t.Fatal(err)
					}
					return decoded, nil
				}
				if request.AcknowledgedOutputSequence != 1 || accepted != "useful-output" {
					t.Fatal("output ACK lost accepted bytes or exact sequence")
				}
				return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, ReasonCode: "REMOTE_EXIT"}, nil
			}
			sink := func(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, output KubernetesStreamOutput) error {
				defer clear(output.Payload)
				if ctx.Err() != nil || output.DeliveryOrdinal != ordinal || output.Sequence != 1 || output.StreamRef != descriptor.StreamRef || output.InvocationRef != binding.InvocationRef || !output.AuthorizedUntil.After(time.Now()) {
					t.Fatal("accepted sink lost exact output witness")
				}
				accepted = string(output.Payload)
				return nil
			}
			consumer := newGatewayStream(t, broker, sink)
			if _, err := consumer.Exchange(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := consumer.Exchange(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if broker.exchanges.Load() != 2 {
				t.Fatal("witness handling repeated an exchange")
			}
		})
	}
}

// TestKubernetesStreamInputCannotCarryOutputOrdinal rejects forged output credit before the first broker call and keeps a valid zero-ordinal input usable.
// TestKubernetesStreamInputCannotCarryOutputOrdinal отклоняет forged output credit до broker call и сохраняет рабочий INPUT с нулевым ordinal.
func TestKubernetesStreamInputCannotCarryOutputOrdinal(t *testing.T) {
	for _, ordinal := range []uint32{1, 4, 5, ^uint32(0)} {
		t.Run(strconv.FormatUint(uint64(ordinal), 10), func(t *testing.T) {
			broker := &gatewayStreamBrokerFixture{}
			broker.exchange = func(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				if request.Input == nil || request.Input.DeliveryOrdinal != 0 {
					t.Fatal("INPUT forwarded output credit")
				}
				return &sb.ExchangeManagedKubernetesStreamResponse{AcknowledgedInputSequence: 1, State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING}, nil
			}
			consumer := newGatewayStream(t, broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, output KubernetesStreamOutput) error {
				clear(output.Payload)
				t.Fatal("input-only exchange emitted output")
				return nil
			})
			frame := gatewayStreamFrame(1, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, "owned-input")
			frame.DeliveryOrdinal = ordinal
			if _, err := consumer.Exchange(t.Context(), frame); err == nil || broker.exchanges.Load() != 0 {
				t.Fatal("nonzero INPUT ordinal reached broker")
			}
			frame.DeliveryOrdinal = 0
			if _, err := consumer.Exchange(t.Context(), frame); err != nil || broker.exchanges.Load() != 1 {
				t.Fatal("valid INPUT failed after closed pre-dispatch refusal")
			}
		})
	}
}
