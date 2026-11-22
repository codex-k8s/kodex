package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesExecutionInputOwner replaces only protected Claim RPC transport while retaining real source validation and cleanup adapters.
// kubernetesExecutionInputOwner заменяет только transport защищённого Claim RPC, сохраняя real source validation и cleanup adapters.
type kubernetesExecutionInputOwner struct {
	*kubernetesExecutionOwner
	claims int
	source []byte
	until  time.Time
	mode   string
}

// ClaimManagedKubernetesInputFrame returns a single synthetic staged tuple or explicit uncertainty, never a callback-selected frame.
// ClaimManagedKubernetesInputFrame возвращает single synthetic staged tuple либо explicit uncertainty, не callback-selected frame.
func (owner *kubernetesExecutionInputOwner) ClaimManagedKubernetesInputFrame(ctx context.Context, request *cp.ClaimManagedKubernetesInputFrameRequest, _ ...grpc.CallOption) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
	owner.claims++
	if owner.mode == "readonly" {
		owner.t.Fatal("read-only descriptor polled input")
	}
	if owner.claims != 1 || request.StreamRef != "stream_fixture" || request.Binding.Fence != owner.binding.Fence || request.Binding.Generation != owner.binding.Generation || request.Binding.InvocationRef != owner.binding.InvocationRef {
		owner.t.Fatal("Claim retried or changed original binding")
	}
	if _, ok := ctx.Deadline(); !ok {
		owner.t.Fatal("Claim lost finite budget")
	}
	if owner.mode == "lost" {
		return nil, status.Error(codes.Unavailable, "PRIVATE")
	}
	if owner.mode == "cancel" {
		owner.stop()
		return nil, status.Error(codes.Canceled, "PRIVATE")
	}
	if owner.mode == "unknown" {
		return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_UNKNOWN}, nil
	}
	raw := []byte("protected-input")
	eof := false
	if owner.mode == "EOF" {
		raw = nil
		eof = true
	}
	digest := sha256.Sum256(raw)
	owner.source = raw
	owner.until = time.Now().Add(2 * time.Second)
	if owner.mode == "expired" {
		owner.until = time.Now().Add(-time.Second)
	}
	if owner.mode == "deadline" {
		owner.until = time.Now().Add(150 * time.Millisecond)
	}
	frame := &cp.ManagedKubernetesInputFrame{Ref: "kif_fixture_input", InvocationRef: owner.binding.InvocationRef, StreamRef: request.StreamRef, Generation: owner.binding.Generation, Sequence: 1, Channel: "STDIN", PayloadDigest: hex.EncodeToString(digest[:]), ByteCount: uint32(len(raw)), Payload: raw, EndOfInput: eof, BindingDigest: owner.initial.BindingDigest, DeliveryExpiresAt: timestamppb.New(owner.until)}
	if owner.mode == "foreign" {
		frame.InvocationRef = "inv_foreign"
	}
	return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER, Frame: frame}, nil
}

// kubernetesExecutionInputBroker exposes exact native input and caller deadlines through the existing broker interface.
// kubernetesExecutionInputBroker открывает exact native input и caller deadlines через existing broker interface.
type kubernetesExecutionInputBroker struct {
	*kubernetesExecutionBroker
	inputOwner *kubernetesExecutionInputOwner
	sent       []byte
}

// ExecuteManagedKubernetesOperation supplies one started descriptor with owner-registered directional channels.
// ExecuteManagedKubernetesOperation выдаёт один started descriptor с owner-registered directional channels.
func (broker *kubernetesExecutionInputBroker) ExecuteManagedKubernetesOperation(_ context.Context, request *sb.ExecuteManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	broker.calls++
	if broker.calls != 1 || !proto.Equal(request.Binding, broker.owner.binding) {
		broker.owner.t.Fatal("effect replayed or changed binding")
	}
	descriptor := &sb.ManagedKubernetesStreamDescriptor{StreamRef: "stream_fixture", InvocationRef: request.Binding.InvocationRef, Generation: request.Binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 1024, MaximumTotalBytes: 4096, InputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN}, OutputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}}
	if broker.inputOwner.mode == "readonly" {
		descriptor.InputChannels = nil
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Stream{Stream: descriptor}}, nil
}

// ExchangeManagedKubernetesStream verifies the retained source deadline and a single exact tuple before acknowledging local delivery.
// ExchangeManagedKubernetesStream проверяет retained source deadline и single exact tuple перед acknowledgement local delivery.
func (broker *kubernetesExecutionInputBroker) ExchangeManagedKubernetesStream(ctx context.Context, request *sb.ExchangeManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	broker.exchanges++
	if broker.exchanges != 1 {
		broker.owner.t.Fatal("uncertain input replayed")
	}
	acknowledged := uint64(0)
	if broker.inputOwner.mode == "readonly" {
		if request.Input != nil {
			broker.owner.t.Fatal("read-only stream fabricated input")
		}
	} else {
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(broker.inputOwner.until) || request.Input == nil || request.Input.Sequence != 1 || request.Input.DeliveryOrdinal != 0 || request.Input.EndOfInput != (broker.inputOwner.mode == "EOF") {
			broker.owner.t.Fatal("source deadline or exact tuple lost")
		}
		broker.sent = request.Input.Payload
		expected := "protected-input"
		if broker.inputOwner.mode == "EOF" {
			expected = ""
		}
		if string(broker.sent) != expected {
			broker.owner.t.Fatal("staged input bytes changed")
		}
		if broker.inputOwner.mode == "deadline" {
			<-ctx.Done()
			return nil, status.Error(codes.DeadlineExceeded, "PRIVATE")
		}
		acknowledged = 1
	}
	return &sb.ExchangeManagedKubernetesStreamResponse{AcknowledgedInputSequence: acknowledged, State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, ReasonCode: "CLEAN_EOF"}, nil
}

// TestKubernetesExecutionUsesProtectedInputFactory exercises actual Claim source, native Exchange deadline, owner closure and positive broker cleanup join.
// No test-selected input callback participates; absent/uncertain stages never become a second Claim or native replay.
// TestKubernetesExecutionUsesProtectedInputFactory проверяет actual Claim source, native Exchange deadline, owner closure и positive broker cleanup join.
// Test-selected input callback отсутствует; missing/uncertain stages не становятся вторым Claim либо native replay.
func TestKubernetesExecutionUsesProtectedInputFactory(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: 10 * time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"input", "EOF", "readonly", "unknown", "lost", "foreign", "expired", "deadline", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			operation := "kubernetes.exec"
			if mode == "readonly" {
				operation = "kubernetes.logs"
			}
			input := kubernetesExecutionInput(t, definitions[integrationpackage.KubernetesDefinitionKey], operation)
			base := &kubernetesExecutionOwner{kubernetesCompletionOwner: kubernetesCompletionFixture(t)}
			base.binding = input.binding
			digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: input.binding.WorkloadInstance, InvocationRef: input.binding.InvocationRef, LeaseRef: input.binding.LeaseRef, Fence: input.binding.Fence, Generation: input.binding.Generation, InputDigest: input.binding.InputDigest})
			if err != nil {
				t.Fatal(err)
			}
			for _, receipt := range []*cp.ManagedKubernetesInvocationReceipt{base.initial, base.after, base.response} {
				receipt.InputDigest = input.binding.InputDigest
				receipt.BindingDigest = digest
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base.stop = cancel
			owner := &kubernetesExecutionInputOwner{kubernetesExecutionOwner: base, mode: mode}
			broker := &kubernetesExecutionInputBroker{kubernetesExecutionBroker: &kubernetesExecutionBroker{owner: base}, inputOwner: owner}
			execution, err := newKubernetesExecution(adapter, owner, broker, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = execution.run(ctx, input)
			failed := mode == "unknown" || mode == "lost" || mode == "foreign" || mode == "expired" || mode == "deadline"
			if (err != nil) != failed || owner.claims > 1 || base.cancels != 1 || broker.cancels != 1 {
				t.Fatalf("input runner outcome or cleanup mismatch: mode=%s err=%v", mode, err)
			}
			expectedClaims := 1
			if mode == "readonly" {
				expectedClaims = 0
			}
			if owner.claims != expectedClaims {
				t.Fatal("input source skipped or read-only source polled")
			}
			expectedExchanges := 1
			if mode == "unknown" || mode == "lost" || mode == "foreign" || mode == "expired" || mode == "cancel" {
				expectedExchanges = 0
			}
			if broker.exchanges != expectedExchanges {
				t.Fatal("uncertain stage reached native Exchange")
			}
			if !bytes.Equal(owner.source, make([]byte, len(owner.source))) || !bytes.Equal(broker.sent, make([]byte, len(broker.sent))) {
				t.Fatal("runner retained owned private bytes")
			}
		})
	}
}
