package controlplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestFrameAdapterRejectsUntrustedPortForwardRetention validates the new protected metadata without accepting malformed or cross-direction snapshots.
// TestFrameAdapterRejectsUntrustedPortForwardRetention проверяет protected metadata без приёма malformed либо cross-direction snapshots.
func TestFrameAdapterRejectsUntrustedPortForwardRetention(t *testing.T) {
	for _, mode := range []string{"valid", "nil", "unknown_field", "ref", "generation", "slot", "duplicate", "output", "stdin", "unknown", "ack"} {
		t.Run(mode, func(t *testing.T) {
			owner, client, binding, frame := frameAdapterFixture()
			frame.Channel = "PORT_FORWARD"
			item := &cp.ManagedKubernetesPortForwardRetainedConnection{ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 18)), Generation: 3, Slot: 0}
			client.begin.PortForwardRetention = []*cp.ManagedKubernetesPortForwardRetainedConnection{item}
			switch mode {
			case "nil":
				client.begin.PortForwardRetention[0] = nil
			case "unknown_field":
				item.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
			case "ref":
				item.ConnectionRef = "inv_foreign"
			case "generation":
				item.Generation = 0
			case "slot":
				item.Slot = 16
			case "duplicate":
				client.begin.PortForwardRetention = append(client.begin.PortForwardRetention, item)
			case "output":
				frame.Direction = "OUTPUT"
			case "stdin":
				frame.Channel = "STDIN"
			case "unknown":
				client.begin.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_UNKNOWN
			case "ack":
				client.begin.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_ACKNOWLEDGED
			}
			got, err := owner.BeginKubernetesFrame(t.Context(), binding, frame)
			if (err == nil) != (mode == "valid") {
				t.Fatal("invalid retention crossed protected adapter", mode)
			}
			if err == nil {
				if got.PortForwardRetention == nil || len(got.PortForwardRetention.Connections) != 1 {
					t.Fatal("exact owner snapshot lost")
				}
				item.Generation++
				if got.PortForwardRetention.Connections[0].Generation != 3 {
					t.Fatal("adapter retained borrowed protobuf tuple")
				}
			}
		})
	}
}

// frameClientFixture substitutes only the protected RPC transport, retaining generated wire types.
// frameClientFixture заменяет только protected RPC transport, сохраняя generated wire types.
type frameClientFixture struct {
	cp.ManagedKubernetesOwnerServiceClient
	begin                     *cp.BeginManagedKubernetesFrameResponse
	complete                  *cp.CompleteManagedKubernetesFrameResponse
	err                       error
	beginRequest              *cp.BeginManagedKubernetesFrameRequest
	completeRequest           *cp.CompleteManagedKubernetesFrameRequest
	beginCalls, completeCalls int
}

// BeginManagedKubernetesFrame captures one request without emulating owner authority or persistence.
// BeginManagedKubernetesFrame фиксирует один request без имитации owner authority либо persistence.
func (client *frameClientFixture) BeginManagedKubernetesFrame(_ context.Context, request *cp.BeginManagedKubernetesFrameRequest, _ ...grpc.CallOption) (*cp.BeginManagedKubernetesFrameResponse, error) {
	client.beginCalls++
	client.beginRequest = request
	return client.begin, client.err
}

// CompleteManagedKubernetesFrame returns a controlled receipt, including uncertainty and schema failures.
// CompleteManagedKubernetesFrame возвращает управляемый receipt, включая uncertainty и schema failures.
func (client *frameClientFixture) CompleteManagedKubernetesFrame(_ context.Context, request *cp.CompleteManagedKubernetesFrameRequest, _ ...grpc.CallOption) (*cp.CompleteManagedKubernetesFrameResponse, error) {
	client.completeCalls++
	client.completeRequest = request
	return client.complete, client.err
}

// frameAdapterFixture supplies synthetic exact lineage and no payload material.
// frameAdapterFixture задаёт synthetic exact lineage без payload material.
func frameAdapterFixture() (*Owner, *frameClientFixture, managedkubernetes.Binding, value.KubernetesFrameMetadata) {
	client := &frameClientFixture{begin: &cp.BeginManagedKubernetesFrameResponse{Disposition: cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_SEND, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, complete: &cp.CompleteManagedKubernetesFrameResponse{AcknowledgedSequence: 3}}
	owner := &Owner{client: &controlplaneclient.Client{KubernetesOperations: client}}
	binding := managedkubernetes.Binding{InvocationRef: "inv_synthetic", WorkloadInstance: "worker_synthetic", LeaseRef: "lease_synthetic", Fence: "fence_synthetic", Generation: 7, InputDigest: strings.Repeat("a", 64)}
	frame := value.KubernetesFrameMetadata{StreamRef: "stream_synthetic", Direction: "INPUT", Channel: "STDIN", PayloadDigest: strings.Repeat("b", 64), Sequence: 3, ByteCount: 12, EndOfInput: true}
	return owner, client, binding, frame
}

// TestFrameAdapterPreservesLineage verifies exact owner pins and one RPC per transition.
// TestFrameAdapterPreservesLineage проверяет exact owner pins и один RPC на переход.
func TestFrameAdapterPreservesLineage(t *testing.T) {
	owner, client, binding, frame := frameAdapterFixture()
	admission, err := owner.BeginKubernetesFrame(t.Context(), binding, frame)
	if err != nil || admission.Disposition != "SEND" {
		t.Fatal("valid admission failed")
	}
	request := client.beginRequest
	if request.GetBinding().GetFence() != binding.Fence || request.GetBinding().GetWorkloadInstance() != binding.WorkloadInstance || request.GetBinding().GetGeneration() != binding.Generation || request.GetBinding().GetInputDigest() != binding.InputDigest || request.GetBinding().GetLeaseRef() != binding.LeaseRef || request.GetBinding().GetInvocationRef() != binding.InvocationRef || request.GetBinding().GetConnectionTestRef() != "" || request.GetStreamRef() != frame.StreamRef || request.GetSequence() != frame.Sequence || request.GetChannel() != frame.Channel || request.GetPayloadDigest() != frame.PayloadDigest || request.GetByteCount() != frame.ByteCount || !request.GetEndOfInput() {
		t.Fatal("owner lineage or frame intent changed")
	}
	ack, err := owner.CompleteKubernetesFrame(t.Context(), binding, frame)
	if err != nil || ack != frame.Sequence || client.beginCalls != 1 || client.completeCalls != 1 {
		t.Fatal("receipt failed or RPC retried")
	}
	if client.completeRequest.GetPayloadDigest() != frame.PayloadDigest || client.completeRequest.GetStreamRef() != frame.StreamRef || client.completeRequest.GetBinding().GetFence() != binding.Fence {
		t.Fatal("completion pins changed")
	}
}

// TestFrameAdapterClosedReplies rejects incomplete authority without synthesizing send credit.
// TestFrameAdapterClosedReplies отклоняет неполные полномочия без создания send credit.
func TestFrameAdapterClosedReplies(t *testing.T) {
	for _, scenario := range []string{"nil", "lost", "enum", "unknown-field", "missing-time", "invalid-time", "nested-unknown", "mismatch-ack", "complete-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, frame := frameAdapterFixture()
			switch scenario {
			case "nil":
				client.begin = nil
			case "lost":
				client.err = errors.New("synthetic transport failure")
			case "enum":
				client.begin.Disposition = 99
			case "unknown-field":
				client.begin.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
			case "missing-time":
				client.begin.ExpiresAt = nil
			case "invalid-time":
				client.begin.ExpiresAt = &timestamppb.Timestamp{Seconds: 253402300800}
			case "nested-unknown":
				client.begin.ExpiresAt.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
			case "mismatch-ack":
				client.complete.AcknowledgedSequence++
			case "complete-unknown":
				client.complete.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
			}
			var err error
			if strings.HasPrefix(scenario, "complete-") || scenario == "mismatch-ack" {
				_, err = owner.CompleteKubernetesFrame(t.Context(), binding, frame)
			} else {
				_, err = owner.BeginKubernetesFrame(t.Context(), binding, frame)
			}
			if !errors.Is(err, targetrepo.ErrFrameUnavailable) || client.beginCalls+client.completeCalls != 1 {
				t.Fatal("invalid reply accepted or RPC retried")
			}
		})
	}
}

// TestFrameAdapterNoCreditStatusesPreserved distinguishes receipt replay from an ambiguous send.
// TestFrameAdapterNoCreditStatusesPreserved различает receipt replay и ambiguous send.
func TestFrameAdapterNoCreditStatusesPreserved(t *testing.T) {
	for name, disposition := range map[string]cp.ManagedKubernetesFrameDisposition{"UNKNOWN": cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_UNKNOWN, "ACKNOWLEDGED": cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_ACKNOWLEDGED} {
		t.Run(name, func(t *testing.T) {
			owner, client, binding, frame := frameAdapterFixture()
			client.begin.Disposition = disposition
			client.begin.ExpiresAt = nil
			result, err := owner.BeginKubernetesFrame(t.Context(), binding, frame)
			if err != nil || result.Disposition != name || !result.ExpiresAt.IsZero() {
				t.Fatal("receipt-only status received send authority")
			}
		})
	}
}

// TestFrameAdapterOutputWitnessCarriesExactDeadline binds generated owner requests and replies to the native delivery witness.
// TestFrameAdapterOutputWitnessCarriesExactDeadline связывает generated owner requests/replies с native delivery witness.
func TestFrameAdapterOutputWitnessCarriesExactDeadline(t *testing.T) {
	owner, client, binding, frame := frameAdapterFixture()
	frame.Direction, frame.Channel, frame.EndOfInput = "OUTPUT", "STDOUT", false
	frame.NativeAuthorizedUntil = time.Now().Add(4 * time.Second)
	client.begin.ExpiresAt = timestamppb.New(frame.NativeAuthorizedUntil.Add(-time.Second))
	client.begin.DeliveryOrdinal = 2
	got, err := owner.BeginKubernetesFrame(t.Context(), binding, frame)
	if err != nil || got.DeliveryOrdinal != 2 || !got.ExpiresAt.Equal(client.begin.ExpiresAt.AsTime()) || client.beginCalls != 1 || client.beginRequest.NativeAuthorizedUntil == nil || !client.beginRequest.NativeAuthorizedUntil.AsTime().Equal(frame.NativeAuthorizedUntil) {
		t.Fatal("native witness or durable ordinal changed across owner RPC")
	}
}

// TestFrameAdapterRejectsUnboundDeliveryCredit closes invalid direction, deadline and ordinal combinations before output becomes usable.
// TestFrameAdapterRejectsUnboundDeliveryCredit закрыто отклоняет неверные direction/deadline/ordinal до использования output.
func TestFrameAdapterRejectsUnboundDeliveryCredit(t *testing.T) {
	for _, scenario := range []string{"missing-native", "expired-native", "input-native", "input-ordinal", "zero-ordinal", "overflow-ordinal", "expired-credit", "beyond-native", "beyond-five-seconds", "ack-ordinal", "unknown-expiry"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding, frame := frameAdapterFixture()
			frame.Direction, frame.Channel, frame.EndOfInput = "OUTPUT", "STDOUT", false
			frame.NativeAuthorizedUntil = time.Now().Add(4 * time.Second)
			client.begin.ExpiresAt = timestamppb.New(time.Now().Add(2 * time.Second))
			client.begin.DeliveryOrdinal = 1
			calls := 1
			switch scenario {
			case "missing-native":
				frame.NativeAuthorizedUntil = time.Time{}
				calls = 0
			case "expired-native":
				frame.NativeAuthorizedUntil = time.Now().Add(-time.Second)
				calls = 0
			case "input-native":
				frame.Direction = "INPUT"
				calls = 0
			case "input-ordinal":
				frame.Direction = "INPUT"
				frame.NativeAuthorizedUntil = time.Time{}
			case "zero-ordinal":
				client.begin.DeliveryOrdinal = 0
			case "overflow-ordinal":
				client.begin.DeliveryOrdinal = managedkubernetes.MaximumOutputDeliveries + 1
			case "expired-credit":
				client.begin.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "beyond-native":
				client.begin.ExpiresAt = timestamppb.New(frame.NativeAuthorizedUntil.Add(time.Second))
			case "beyond-five-seconds":
				frame.NativeAuthorizedUntil = time.Now().Add(time.Minute)
				client.begin.ExpiresAt = timestamppb.New(time.Now().Add(10 * time.Second))
			case "ack-ordinal":
				client.begin.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_ACKNOWLEDGED
				client.begin.ExpiresAt = nil
			case "unknown-expiry":
				client.begin.Disposition = cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_UNKNOWN
				client.begin.DeliveryOrdinal = 0
			}
			result, err := owner.BeginKubernetesFrame(t.Context(), binding, frame)
			if !errors.Is(err, targetrepo.ErrFrameUnavailable) || result != (value.KubernetesFrameAdmission{}) || client.beginCalls != calls {
				t.Fatal("invalid delivery credit admitted or owner unexpectedly called")
			}
		})
	}
}
