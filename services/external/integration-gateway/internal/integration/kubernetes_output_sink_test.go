package integration

import (
	"bytes"
	"context"
	"errors"
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

// kubernetesOutputOwnerFixture captures actual typed owner calls without simulating private storage authority.
// kubernetesOutputOwnerFixture фиксирует реальные typed owner calls, не имитируя полномочия private storage.
type kubernetesOutputOwnerFixture struct {
	cp.ManagedKubernetesOwnerServiceClient
	accepts, reads int
	accept         func(context.Context, *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error)
	read           func(context.Context, *cp.GetManagedKubernetesOutputFrameRequest) (*cp.GetManagedKubernetesOutputFrameResponse, error)
}

// AcceptManagedKubernetesOutputFrame counts each payload submission so tests detect forbidden retries.
// AcceptManagedKubernetesOutputFrame считает каждую передачу payload, обнаруживая запрещённые retries.
func (f *kubernetesOutputOwnerFixture) AcceptManagedKubernetesOutputFrame(ctx context.Context, in *cp.AcceptManagedKubernetesOutputFrameRequest, _ ...grpc.CallOption) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
	f.accepts++
	return f.accept(ctx, in)
}

// GetManagedKubernetesOutputFrame exposes the exact metadata reconciliation request separately from payload transmission.
// GetManagedKubernetesOutputFrame открывает exact metadata reconciliation request отдельно от передачи payload.
func (f *kubernetesOutputOwnerFixture) GetManagedKubernetesOutputFrame(ctx context.Context, in *cp.GetManagedKubernetesOutputFrameRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesOutputFrameResponse, error) {
	f.reads++
	return f.read(ctx, in)
}

// kubernetesOutputSinkFixture supplies synthetic private output and a matching immutable deadline receipt.
// kubernetesOutputSinkFixture создаёт synthetic private output и квитанцию с соответствующим immutable deadline.
func kubernetesOutputSinkFixture(t *testing.T) (*sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor, KubernetesStreamOutput, *cp.ManagedKubernetesOutputFrameReceipt) {
	t.Helper()
	binding, descriptor := gatewayStreamPins()
	frame := gatewayStreamFrame(1, descriptor.OutputChannels[0], "accepted-private-output")
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	output := KubernetesStreamOutput{InvocationRef: binding.InvocationRef, StreamRef: descriptor.StreamRef, BindingDigest: digest, Generation: binding.Generation, Sequence: frame.Sequence, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, DeliveryOrdinal: 1, Payload: bytes.Clone(frame.Payload), AuthorizedUntil: time.Now().Add(2 * time.Second).Truncate(time.Microsecond)}
	receipt := &cp.ManagedKubernetesOutputFrameReceipt{Ref: "kof_receipt_fixture", InvocationRef: output.InvocationRef, StreamRef: output.StreamRef, Generation: output.Generation, Sequence: output.Sequence, Channel: "STDOUT", PayloadDigest: output.PayloadDigest, ByteCount: uint32(len(output.Payload)), DeliveryOrdinal: output.DeliveryOrdinal, State: cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_ACCEPTED, AuthorizedUntil: timestamppb.New(output.AuthorizedUntil), BindingDigest: output.BindingDigest}
	return binding, descriptor, output, receipt
}

// TestKubernetesOutputSinkAcceptsOwnedBytes verifies exact bytes, original scope, finite deadline and clearing after accepted staging.
// TestKubernetesOutputSinkAcceptsOwnedBytes проверяет exact bytes, original scope, конечный deadline и очистку после accepted staging.
func TestKubernetesOutputSinkAcceptsOwnedBytes(t *testing.T) {
	binding, descriptor, output, receipt := kubernetesOutputSinkFixture(t)
	originalWire, _ := kubernetesOwnerBinding(binding)
	wanted := bytes.Clone(output.Payload)
	var submitted []byte
	owner := &kubernetesOutputOwnerFixture{}
	owner.accept = func(ctx context.Context, in *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(output.AuthorizedUntil) || !proto.Equal(in.Binding, originalWire) || in.StreamRef != output.StreamRef || in.Sequence != output.Sequence || in.Channel != "STDOUT" || in.ByteCount != uint32(len(wanted)) || in.PayloadDigest != output.PayloadDigest || in.DeliveryOrdinal != 1 || !bytes.Equal(in.Payload, wanted) {
			t.Fatal("owner request lost original output pins")
		}
		submitted = in.Payload
		return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
	}
	if err := NewKubernetesOutputSink(owner)(context.Background(), binding, descriptor, output); err != nil {
		t.Fatal(err)
	}
	if owner.accepts != 1 || owner.reads != 0 || !bytes.Equal(output.Payload, make([]byte, len(wanted))) || !bytes.Equal(submitted, make([]byte, len(wanted))) {
		t.Fatal("acceptance retried or retained owned bytes")
	}
	if NewKubernetesOutputSink(nil) != nil {
		t.Fatal("missing owner advertised output sink")
	}
}

// TestKubernetesOutputSinkRejectsBeforeRPC checks malformed local scope and bytes without sending payload or retaining ownership.
// TestKubernetesOutputSinkRejectsBeforeRPC проверяет неверные local scope и bytes без передачи payload либо удержания ownership.
func TestKubernetesOutputSinkRejectsBeforeRPC(t *testing.T) {
	cases := map[string]func(*sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor, *KubernetesStreamOutput){
		"fence": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			b.Fence = ""
		},
		"health": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			b.ConnectionTestRef = "test_fixture"
		},
		"unknown binding": func(b *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			b.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		"unknown descriptor": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			d.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		"foreign stream": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.StreamRef = "other_stream"
		},
		"foreign invocation": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.InvocationRef = "other_invocation"
		},
		"generation": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Generation++
		},
		"binding digest": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.BindingDigest = strings.Repeat("b", 64)
		},
		"payload digest": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Payload[0] ^= 1
		},
		"empty": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Payload = nil
		},
		"oversized": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			d.MaximumFrameBytes = 1
		},
		"total budget": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			d.MaximumTotalBytes = 1
		},
		"channel": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Channel = sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN
		},
		"duplicate channel": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor, _ *KubernetesStreamOutput) {
			d.OutputChannels = append(d.OutputChannels, d.OutputChannels[0])
		},
		"sequence zero": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Sequence = 0
		},
		"sequence overflow": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.Sequence = managedkubernetes.MaximumFrameSequence + 1
		},
		"ordinal zero": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.DeliveryOrdinal = 0
		},
		"ordinal overflow": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.DeliveryOrdinal = 5
		},
		"expired": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.AuthorizedUntil = time.Now().Add(-time.Second)
		},
		"overcredit": func(_ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			o.AuthorizedUntil = time.Now().Add(6 * time.Second)
		},
		"hard deadline": func(_ *sb.ManagedKubernetesInvocationBinding, d *sb.ManagedKubernetesStreamDescriptor, o *KubernetesStreamOutput) {
			d.ExpiresAt = timestamppb.New(o.AuthorizedUntil.Add(-time.Second))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			binding, descriptor, output, _ := kubernetesOutputSinkFixture(t)
			mutate(binding, descriptor, &output)
			owner := &kubernetesOutputOwnerFixture{}
			err := AcceptKubernetesStreamOutput(context.Background(), binding, descriptor, output, owner)
			if err == nil || owner.accepts != 0 || owner.reads != 0 || !bytes.Equal(output.Payload, make([]byte, len(output.Payload))) {
				t.Fatal("invalid candidate crossed owner boundary or retained payload")
			}
		})
	}
}

// TestKubernetesOutputSinkReceiptMustBeExact rejects positive malformed acknowledgements without a reconciliation read or output ACK.
// TestKubernetesOutputSinkReceiptMustBeExact отклоняет положительные malformed acknowledgements без reconciliation read либо output ACK.
func TestKubernetesOutputSinkReceiptMustBeExact(t *testing.T) {
	cases := map[string]func(*cp.ManagedKubernetesOutputFrameReceipt){
		"foreign ref kind": func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.Ref = "other_receipt" },
		"ref":              func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.Ref = "" },
		"invocation":       func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.InvocationRef = "foreign" },
		"stream":           func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.StreamRef = "foreign" },
		"generation":       func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.Generation++ },
		"sequence":         func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.Sequence++ },
		"channel":          func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.Channel = "STDERR" },
		"digest":           func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.PayloadDigest = strings.Repeat("b", 64) },
		"bytes":            func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.ByteCount++ },
		"ordinal":          func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.DeliveryOrdinal++ },
		"binding":          func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.BindingDigest = strings.Repeat("b", 64) },
		"state":            func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.State = cp.ManagedKubernetesOutputFrameState(999) },
		"missing expiry":   func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.AuthorizedUntil = nil },
		"earlier expiry": func(r *cp.ManagedKubernetesOutputFrameReceipt) {
			r.AuthorizedUntil = timestamppb.New(r.AuthorizedUntil.AsTime().Add(-time.Microsecond))
		},
		"later expiry": func(r *cp.ManagedKubernetesOutputFrameReceipt) {
			r.AuthorizedUntil = timestamppb.New(r.AuthorizedUntil.AsTime().Add(time.Microsecond))
		},
		"unknown expiry": func(r *cp.ManagedKubernetesOutputFrameReceipt) {
			r.AuthorizedUntil.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		"invalid expiry":  func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.AuthorizedUntil.Nanos = -1 },
		"unknown receipt": func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) },
	}
	for _, state := range []int32{0, 1, 2, 4, 5, 6} {
		cases[fmt.Sprintf("nonaccepted %d", state)] = func(r *cp.ManagedKubernetesOutputFrameReceipt) { r.State = cp.ManagedKubernetesOutputFrameState(state) }
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			binding, descriptor, output, receipt := kubernetesOutputSinkFixture(t)
			mutate(receipt)
			owner := &kubernetesOutputOwnerFixture{accept: func(context.Context, *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
				return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
			}}
			if err := AcceptKubernetesStreamOutput(context.Background(), binding, descriptor, output, owner); err == nil || owner.accepts != 1 || owner.reads != 0 {
				t.Fatal("mismatched acceptance admitted or reconciled")
			}
		})
	}
	for _, shape := range []string{"nil response", "nil receipt", "unknown response"} {
		t.Run(shape, func(t *testing.T) {
			binding, descriptor, output, receipt := kubernetesOutputSinkFixture(t)
			owner := &kubernetesOutputOwnerFixture{accept: func(context.Context, *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
				if shape == "nil response" {
					return nil, nil
				}
				reply := &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}
				if shape == "nil receipt" {
					reply.Receipt = nil
				} else {
					reply.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
				}
				return reply, nil
			}}
			if AcceptKubernetesStreamOutput(context.Background(), binding, descriptor, output, owner) == nil || owner.reads != 0 {
				t.Fatal("malformed positive reply allowed reconciliation")
			}
		})
	}
}

// TestKubernetesOutputSinkUncertainAcceptanceReadsOnce preserves original pins and deadline even if the first request object was mutated by the client.
// TestKubernetesOutputSinkUncertainAcceptanceReadsOnce сохраняет исходные pins и deadline, даже если client изменил первый request object.
func TestKubernetesOutputSinkUncertainAcceptanceReadsOnce(t *testing.T) {
	for _, state := range []string{"accepted", "rejected", "wrong ordinal", "read unavailable", "expired"} {
		t.Run(state, func(t *testing.T) {
			binding, descriptor, output, receipt := kubernetesOutputSinkFixture(t)
			if state == "expired" {
				output.AuthorizedUntil = time.Now().Add(100 * time.Millisecond)
				receipt.AuthorizedUntil = timestamppb.New(output.AuthorizedUntil)
			}
			wire, _ := kubernetesOwnerBinding(binding)
			owner := &kubernetesOutputOwnerFixture{}
			owner.accept = func(_ context.Context, in *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
				in.Binding.Fence = "mutated"
				return nil, status.Error(codes.Unavailable, "PRIVATE lost acknowledgement")
			}
			owner.read = func(ctx context.Context, in *cp.GetManagedKubernetesOutputFrameRequest) (*cp.GetManagedKubernetesOutputFrameResponse, error) {
				deadline, ok := ctx.Deadline()
				if !ok || !deadline.Equal(output.AuthorizedUntil) || !proto.Equal(in.Binding, wire) || in.StreamRef != output.StreamRef || in.Sequence != output.Sequence || in.DeliveryOrdinal != output.DeliveryOrdinal {
					t.Fatal("recovery changed original scope or deadline")
				}
				switch state {
				case "rejected":
					receipt.State = cp.ManagedKubernetesOutputFrameState(5)
				case "wrong ordinal":
					receipt.DeliveryOrdinal++
				case "read unavailable":
					return nil, errors.New("PRIVATE read error")
				case "expired":
					<-ctx.Done()
				}
				return &cp.GetManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
			}
			err := AcceptKubernetesStreamOutput(context.Background(), binding, descriptor, output, owner)
			if (err == nil) != (state == "accepted") || owner.accepts != 1 || owner.reads != 1 {
				t.Fatal("uncertain acceptance replayed or admitted without exact receipt")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw owner diagnostic escaped")
			}
		})
	}
}

// TestKubernetesOutputSinkCancellationAndDenialNeverRenew checks definitive denials and cancelled or exhausted delivery budgets without retry.
// TestKubernetesOutputSinkCancellationAndDenialNeverRenew проверяет definitive denials и cancelled либо exhausted delivery budgets без retry.
func TestKubernetesOutputSinkCancellationAndDenialNeverRenew(t *testing.T) {
	for _, mode := range []string{"denied", "cancelled before", "cancelled during", "expired during"} {
		t.Run(mode, func(t *testing.T) {
			binding, descriptor, output, receipt := kubernetesOutputSinkFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled before" {
				cancel()
			}
			if mode == "expired during" {
				output.AuthorizedUntil = time.Now().Add(100 * time.Millisecond)
				receipt.AuthorizedUntil = timestamppb.New(output.AuthorizedUntil)
			}
			owner := &kubernetesOutputOwnerFixture{accept: func(ctx context.Context, _ *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
				switch mode {
				case "cancelled during":
					cancel()
					return nil, status.Error(codes.Canceled, "PRIVATE")
				case "expired during":
					<-ctx.Done()
					return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
				default:
					return nil, status.Error(codes.PermissionDenied, "PRIVATE")
				}
			}}
			err := AcceptKubernetesStreamOutput(ctx, binding, descriptor, output, owner)
			if err == nil || owner.reads != 0 || owner.accepts > 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("cancelled or denied output renewed or disclosed diagnostics")
			}
		})
	}
}

// TestKubernetesOutputSinkDrivesActualBrokerACK requires actual protected acceptance before the real consumer can acknowledge the output frame.
// TestKubernetesOutputSinkDrivesActualBrokerACK требует реального protected acceptance перед ACK output frame настоящим consumer.
func TestKubernetesOutputSinkDrivesActualBrokerACK(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(fmt.Sprint(accepted), func(t *testing.T) {
			binding, descriptor := gatewayStreamPins()
			reply := gatewayStreamReply()
			var staged []byte
			owner := &kubernetesOutputOwnerFixture{accept: func(_ context.Context, in *cp.AcceptManagedKubernetesOutputFrameRequest) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
				staged = bytes.Clone(in.Payload)
				digest, _ := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest})
				receipt := &cp.ManagedKubernetesOutputFrameReceipt{Ref: "kof_receipt_fixture", InvocationRef: binding.InvocationRef, StreamRef: in.StreamRef, Generation: binding.Generation, Sequence: in.Sequence, Channel: in.Channel, PayloadDigest: in.PayloadDigest, ByteCount: in.ByteCount, DeliveryOrdinal: in.DeliveryOrdinal, State: cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_ACCEPTED, AuthorizedUntil: proto.Clone(reply.AuthorizedUntil).(*timestamppb.Timestamp), BindingDigest: digest}
				if !accepted {
					receipt.State = cp.ManagedKubernetesOutputFrameState(2)
				}
				return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: receipt}, nil
			}}
			broker := &gatewayStreamBrokerFixture{}
			broker.exchange = func(_ context.Context, in *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
				if broker.exchanges.Load() == 1 {
					if in.AcknowledgedOutputSequence != 0 {
						t.Fatal("premature ACK")
					}
					return proto.Clone(reply).(*sb.ExchangeManagedKubernetesStreamResponse), nil
				}
				if !accepted || in.AcknowledgedOutputSequence != 1 || string(staged) != "useful-output" || owner.accepts != 1 {
					t.Fatal("ACK without exact durable sink acceptance")
				}
				return &sb.ExchangeManagedKubernetesStreamResponse{State: reply.State, RemoteOutcome: reply.RemoteOutcome}, nil
			}
			consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, NewKubernetesOutputSink(owner), gatewayStreamClosure)
			if err != nil {
				t.Fatal(err)
			}
			_, err = consumer.Exchange(context.Background(), nil)
			if (err == nil) != accepted {
				t.Fatal("consumer acceptance mismatch")
			}
			_, nextErr := consumer.Exchange(context.Background(), nil)
			if accepted && nextErr != nil || !accepted && (nextErr == nil || broker.exchanges.Load() != 1) {
				t.Fatal("rejected sink output acknowledged or accepted output not acknowledged")
			}
		})
	}
}
