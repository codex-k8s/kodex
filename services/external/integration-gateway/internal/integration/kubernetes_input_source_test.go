package integration

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesInputOwnerFixture observes the exact one-attempt protected Claim boundary.
// kubernetesInputOwnerFixture наблюдает exact one-attempt защищённую границу Claim.
type kubernetesInputOwnerFixture struct {
	cp.ManagedKubernetesOwnerServiceClient
	calls int
	claim func(context.Context, *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error)
}

// ClaimManagedKubernetesInputFrame records attempts without implementing retry or storage authority.
// ClaimManagedKubernetesInputFrame считает attempts без реализации retry либо storage authority.
func (f *kubernetesInputOwnerFixture) ClaimManagedKubernetesInputFrame(ctx context.Context, in *cp.ClaimManagedKubernetesInputFrameRequest, _ ...grpc.CallOption) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
	f.calls++
	return f.claim(ctx, in)
}

// gatewayInputFrame constructs a synthetic owner-pinned private frame, never a public upload grant.
// gatewayInputFrame создаёт synthetic owner-pinned private frame, не public upload grant.
func gatewayInputFrame(t *testing.T, b *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) *cp.ManagedKubernetesInputFrame {
	t.Helper()
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: b.WorkloadInstance, InvocationRef: b.InvocationRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	wire := gatewayStreamFrame(1, d.InputChannels[0], "private-input")
	return &cp.ManagedKubernetesInputFrame{Ref: "kif_fixture_input", InvocationRef: b.InvocationRef, StreamRef: d.StreamRef, Generation: b.Generation, Sequence: 1, Channel: "STDIN", PayloadDigest: wire.PayloadDigest, ByteCount: uint32(len(wire.Payload)), Payload: wire.Payload, BindingDigest: digest, DeliveryExpiresAt: timestamppb.New(time.Now().Add(2 * time.Second))}
}

// TestKubernetesInputSourceTransfersOwnedDeadline verifies original scope, retained source deadline and independent payload ownership.
// TestKubernetesInputSourceTransfersOwnedDeadline проверяет original scope, сохранение source deadline и независимое владение payload.
func TestKubernetesInputSourceTransfersOwnedDeadline(t *testing.T) {
	b, d := gatewayStreamPins()
	frame := gatewayInputFrame(t, b, d)
	wanted := bytes.Clone(frame.Payload)
	expected, _ := kubernetesOwnerBinding(b)
	owner := &kubernetesInputOwnerFixture{claim: func(ctx context.Context, in *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(d.ExpiresAt.AsTime()) || deadline.After(time.Now().Add(kubernetesStreamCallTimeout)) || !proto.Equal(in.Binding, expected) || in.StreamRef != d.StreamRef {
			t.Fatal("claim changed original scope or lost deadline")
		}
		in.Binding.Fence = "client-mutated"
		return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER, Frame: frame}, nil
	}}
	delivery, err := NewKubernetesInputSource(owner)(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(delivery.Frame.Payload)
	if owner.calls != 1 || !bytes.Equal(delivery.Frame.Payload, wanted) || !bytes.Equal(frame.Payload, make([]byte, len(wanted))) || !delivery.DeliveryExpiresAt.Equal(frame.DeliveryExpiresAt.AsTime()) || delivery.Frame.DeliveryOrdinal != 0 {
		t.Fatal("claim replayed, lost deadline or retained owner bytes")
	}
	if strings.Contains(fmt.Sprintf("%#v", delivery), "private-input") {
		t.Fatal("diagnostics disclosed bytes")
	}
	if NewKubernetesInputSource(nil) != nil {
		t.Fatal("missing owner advertised input source")
	}
}

// TestKubernetesInputSourceRejectsMalformedDelivery rejects untrusted owner replies before any native frame escapes and clears every received payload.
// TestKubernetesInputSourceRejectsMalformedDelivery отклоняет недоверенные owner replies до выдачи native frame и очищает каждый payload.
func TestKubernetesInputSourceRejectsMalformedDelivery(t *testing.T) {
	cases := map[string]func(*cp.ManagedKubernetesInputFrame){
		"invocation":   func(f *cp.ManagedKubernetesInputFrame) { f.InvocationRef = "foreign" },
		"stream":       func(f *cp.ManagedKubernetesInputFrame) { f.StreamRef = "foreign" },
		"generation":   func(f *cp.ManagedKubernetesInputFrame) { f.Generation++ },
		"binding":      func(f *cp.ManagedKubernetesInputFrame) { f.BindingDigest = strings.Repeat("b", 64) },
		"ref":          func(f *cp.ManagedKubernetesInputFrame) { f.Ref = "foreign" },
		"sequence":     func(f *cp.ManagedKubernetesInputFrame) { f.Sequence = 0 },
		"count":        func(f *cp.ManagedKubernetesInputFrame) { f.ByteCount++ },
		"hash":         func(f *cp.ManagedKubernetesInputFrame) { f.PayloadDigest = strings.Repeat("b", 64) },
		"channel":      func(f *cp.ManagedKubernetesInputFrame) { f.Channel = "STDOUT" },
		"unregistered": func(f *cp.ManagedKubernetesInputFrame) { f.Channel = "FILE_CONTENT" },
		"unknown":      func(f *cp.ManagedKubernetesInputFrame) { f.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1}) },
		"expired": func(f *cp.ManagedKubernetesInputFrame) {
			f.DeliveryExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		"long credit": func(f *cp.ManagedKubernetesInputFrame) {
			f.DeliveryExpiresAt = timestamppb.New(time.Now().Add(31 * time.Second))
		},
		"nil credit": func(f *cp.ManagedKubernetesInputFrame) { f.DeliveryExpiresAt = nil },
		"unknown credit": func(f *cp.ManagedKubernetesInputFrame) {
			f.DeliveryExpiresAt.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
		},
		"bad credit": func(f *cp.ManagedKubernetesInputFrame) { f.DeliveryExpiresAt.Nanos = -1 },
		"fake EOF":   func(f *cp.ManagedKubernetesInputFrame) { f.EndOfInput = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := gatewayStreamPins()
			frame := gatewayInputFrame(t, b, d)
			change(frame)
			owner := &kubernetesInputOwnerFixture{claim: func(context.Context, *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
				return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER, Frame: frame}, nil
			}}
			delivery, err := ClaimKubernetesStreamInput(context.Background(), b, d, owner)
			if delivery != nil || err == nil || owner.calls != 1 || !bytes.Equal(frame.Payload, make([]byte, len(frame.Payload))) {
				t.Fatal("malformed claim escaped or retained bytes")
			}
		})
	}
}

// TestKubernetesInputSourceNoRetryOrFabricatedEOF distinguishes empty polling from uncertainty, closed state and malformed positive acknowledgements.
// TestKubernetesInputSourceNoRetryOrFabricatedEOF различает пустой poll, uncertainty, closed state и malformed positive acknowledgements.
func TestKubernetesInputSourceNoRetryOrFabricatedEOF(t *testing.T) {
	for _, mode := range []string{"none", "unknown", "closed", "nil", "unknown response", "unknown disposition", "none with bytes", "lost ACK", "cancel before", "cancel during", "late positive"} {
		t.Run(mode, func(t *testing.T) {
			b, d := gatewayStreamPins()
			frame := gatewayInputFrame(t, b, d)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel before" {
				cancel()
			}
			owner := &kubernetesInputOwnerFixture{claim: func(context.Context, *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
				reply := &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_NONE}
				switch mode {
				case "unknown":
					reply.Disposition = cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_UNKNOWN
				case "closed":
					reply.Disposition = cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_CLOSED
				case "nil":
					return nil, nil
				case "unknown response":
					reply.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
				case "unknown disposition":
					reply.Disposition = 999
				case "none with bytes":
					reply.Frame = frame
				case "lost ACK":
					return &cp.ClaimManagedKubernetesInputFrameResponse{Frame: frame}, status.Error(codes.Unavailable, "PRIVATE")
				case "cancel during":
					cancel()
					return &cp.ClaimManagedKubernetesInputFrameResponse{Frame: frame}, status.Error(codes.Canceled, "PRIVATE")
				case "late positive":
					cancel()
					reply.Disposition = cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER
					reply.Frame = frame
				}
				return reply, nil
			}}
			delivery, err := ClaimKubernetesStreamInput(ctx, b, d, owner)
			if delivery != nil || (err == nil) != (mode == "none") || owner.calls > 1 || mode == "cancel before" && owner.calls != 0 {
				t.Fatal("uncertain claim retried or absence became EOF")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw owner error escaped")
			}
			if mode == "none with bytes" || mode == "lost ACK" || mode == "cancel during" || mode == "late positive" {
				if !bytes.Equal(frame.Payload, make([]byte, len(frame.Payload))) {
					t.Fatal("error retained private bytes")
				}
			}
		})
	}
}

// TestKubernetesInputSourceFeedsActualExchange keeps the original staged bytes and owner sequence through the real stream protocol and joined ACK.
// TestKubernetesInputSourceFeedsActualExchange сохраняет staged bytes и owner sequence через настоящий stream protocol и joined ACK.
func TestKubernetesInputSourceFeedsActualExchange(t *testing.T) {
	b, d := gatewayStreamPins()
	frame := gatewayInputFrame(t, b, d)
	wanted := bytes.Clone(frame.Payload)
	owner := &kubernetesInputOwnerFixture{claim: func(context.Context, *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
		return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER, Frame: frame}, nil
	}}
	delivery, err := NewKubernetesInputSource(owner)(context.Background(), b, d)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(delivery.Frame.Payload)
	broker := &gatewayStreamBrokerFixture{exchange: func(_ context.Context, in *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
		if in.Input == nil || !bytes.Equal(in.Input.Payload, wanted) || in.Input.Sequence != 1 || in.Input.DeliveryOrdinal != 0 {
			t.Fatal("native input changed staged tuple")
		}
		return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING, AcknowledgedInputSequence: 1}, nil
	}}
	consumer, err := NewKubernetesStreamConsumer(b, d, "kubernetes.exec", broker, func(context.Context, *sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor, KubernetesStreamOutput) error {
		return nil
	}, gatewayStreamClosure)
	if err != nil {
		t.Fatal(err)
	}
	call, cancel := context.WithDeadline(context.Background(), delivery.DeliveryExpiresAt)
	defer cancel()
	progress, err := consumer.Exchange(call, delivery.Frame)
	if err != nil || progress.AcknowledgedInput != 1 || owner.calls != 1 || broker.exchanges.Load() != 1 {
		t.Fatal("actual exchange did not acknowledge exact staged input")
	}
}

// TestKubernetesInputSourcePreflightBlocksIO rejects malformed descriptors and original bindings before any one-attempt Claim.
// TestKubernetesInputSourcePreflightBlocksIO отклоняет malformed descriptors и original bindings до любого one-attempt Claim.
func TestKubernetesInputSourcePreflightBlocksIO(t *testing.T) {
	cases := map[string]func(*sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor){
		"health": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor) {
			b.ConnectionTestRef = "test_fixture"
		},
		"fence": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor) { b.Fence = "" },
		"unknown binding": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor) {
			b.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
		},
		"unknown descriptor": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
		},
		"foreign invocation": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.InvocationRef = "foreign"
		},
		"generation": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.Generation++
		},
		"expired": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		"future": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.ExpiresAt = timestamppb.New(time.Now().Add(601 * time.Second))
		},
		"unknown expiry": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.ExpiresAt.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
		},
		"empty channels": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.InputChannels = nil
		},
		"duplicate channel": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.InputChannels = append(d.InputChannels, d.InputChannels[0])
		},
		"wrong direction": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.InputChannels = []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}
		},
		"frame ceiling": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.MaximumFrameBytes = 32769
		},
		"total ceiling": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor) {
			d.MaximumTotalBytes = 268435457
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := gatewayStreamPins()
			change(b, d)
			owner := &kubernetesInputOwnerFixture{}
			if result, err := ClaimKubernetesStreamInput(context.Background(), b, d, owner); result != nil || err == nil || owner.calls != 0 {
				t.Fatal("invalid preflight crossed Claim boundary")
			}
		})
	}
}

// TestKubernetesInputSourceKeepsTypedEOFResizeAndPrivateArtifact distinguishes public syntax from owner-only artifact bytes.
// TestKubernetesInputSourceKeepsTypedEOFResizeAndPrivateArtifact различает public syntax и owner-only artifact bytes.
func TestKubernetesInputSourceKeepsTypedEOFResizeAndPrivateArtifact(t *testing.T) {
	for _, kind := range []string{"EOF", "resize", "artifact"} {
		t.Run(kind, func(t *testing.T) {
			b, d := gatewayStreamPins()
			frame := gatewayInputFrame(t, b, d)
			channel := sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN
			payload := ""
			frame.EndOfInput = true
			if kind == "resize" {
				channel = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_TERMINAL_RESIZE
				payload = `{"columns":80,"rows":24}`
				frame.EndOfInput = false
			}
			if kind == "artifact" {
				channel = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_FILE_CONTENT
				payload = "private-artifact-bytes"
				frame.EndOfInput = false
			}
			d.InputChannels = []sb.ManagedKubernetesChannel{channel}
			wire := gatewayStreamFrame(1, channel, payload)
			frame.Channel = kubernetesOutputChannel(channel)
			frame.Payload = wire.Payload
			frame.PayloadDigest = wire.PayloadDigest
			frame.ByteCount = uint32(len(wire.Payload))
			owner := &kubernetesInputOwnerFixture{claim: func(context.Context, *cp.ClaimManagedKubernetesInputFrameRequest) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
				return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER, Frame: frame}, nil
			}}
			delivery, err := ClaimKubernetesStreamInput(context.Background(), b, d, owner)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(delivery.Frame.Payload)
			if delivery.Frame.EndOfInput != (kind == "EOF") || delivery.Frame.Channel != channel || string(delivery.Frame.Payload) != payload {
				t.Fatal("typed source changed EOF or payload")
			}
		})
	}
}
