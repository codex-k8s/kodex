package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesExecutionOwner observes the real runner's terminal/cleanup calls without emulating SQL admission.
// kubernetesExecutionOwner наблюдает terminal/cleanup calls настоящего runner без имитации SQL admission.
type kubernetesExecutionOwner struct {
	*kubernetesCompletionOwner
	cancels                 int
	variant                 string
	stop                    context.CancelFunc
	outputCalls, outputGets int
	outputUntil             time.Time
	outputReceipt           *cp.ManagedKubernetesOutputFrameReceipt
}

// AcceptManagedKubernetesOutputFrame supplies an exact RPC receipt and records the single payload transmission.
// AcceptManagedKubernetesOutputFrame возвращает exact RPC receipt и учитывает единственную передачу payload.
func (owner *kubernetesExecutionOwner) AcceptManagedKubernetesOutputFrame(ctx context.Context, request *cp.AcceptManagedKubernetesOutputFrameRequest, _ ...grpc.CallOption) (*cp.AcceptManagedKubernetesOutputFrameResponse, error) {
	owner.outputCalls++
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(owner.outputUntil) || request.Binding.InvocationRef != owner.binding.InvocationRef || request.Binding.Fence != owner.binding.Fence || request.Binding.Generation != owner.binding.Generation || request.StreamRef != "stream_fixture" || request.Sequence != 1 || request.DeliveryOrdinal != 1 || request.Channel != "STDOUT" || string(request.Payload) != "actual-stream-output" || request.ByteCount != uint32(len(request.Payload)) {
		owner.t.Fatal("output coordinates, deadline or bytes changed")
	}
	digest := sha256.Sum256(request.Payload)
	if request.PayloadDigest != hex.EncodeToString(digest[:]) {
		owner.t.Fatal("output digest changed")
	}
	owner.outputReceipt = &cp.ManagedKubernetesOutputFrameReceipt{Ref: "kof_fixture", InvocationRef: owner.binding.InvocationRef, StreamRef: request.StreamRef, Generation: owner.binding.Generation, Sequence: request.Sequence, Channel: request.Channel, PayloadDigest: request.PayloadDigest, ByteCount: request.ByteCount, DeliveryOrdinal: request.DeliveryOrdinal, BindingDigest: owner.initial.BindingDigest, AuthorizedUntil: timestamppb.New(owner.outputUntil), State: cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_ACCEPTED}
	if owner.variant == "stream-rejected" {
		owner.outputReceipt.State = cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_REJECTED
	}
	if owner.variant == "stream-unknown" {
		owner.outputReceipt.State = cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_UNKNOWN
	}
	if owner.variant == "cancel-stream" {
		owner.stop()
	}
	if owner.variant == "stream-lost" || owner.variant == "stream-unknown" {
		return nil, status.Error(codes.Unavailable, "synthetic lost acknowledgement")
	}
	return &cp.AcceptManagedKubernetesOutputFrameResponse{Receipt: proto.Clone(owner.outputReceipt).(*cp.ManagedKubernetesOutputFrameReceipt)}, nil
}

// GetManagedKubernetesOutputFrame reconciles metadata once without retransmitting bytes or extending credit.
// GetManagedKubernetesOutputFrame сверяет metadata один раз без повторной передачи bytes либо продления credit.
func (owner *kubernetesExecutionOwner) GetManagedKubernetesOutputFrame(ctx context.Context, request *cp.GetManagedKubernetesOutputFrameRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesOutputFrameResponse, error) {
	owner.outputGets++
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(owner.outputUntil) || owner.outputCalls != 1 || request.Binding.InvocationRef != owner.binding.InvocationRef || request.Binding.Fence != owner.binding.Fence || request.StreamRef != "stream_fixture" || request.Sequence != 1 || request.DeliveryOrdinal != 1 {
		owner.t.Fatal("reconciliation changed original coordinates or credit")
	}
	return &cp.GetManagedKubernetesOutputFrameResponse{Receipt: proto.Clone(owner.outputReceipt).(*cp.ManagedKubernetesOutputFrameReceipt)}, nil
}

// GetManagedKubernetesInvocation returns the current fixture state, without fabricating terminal progress merely because it was polled twice.
// GetManagedKubernetesInvocation возвращает текущее fixture state без выдуманного terminal progress после второго опроса.
func (owner *kubernetesExecutionOwner) GetManagedKubernetesInvocation(_ context.Context, request *cp.GetManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesInvocationResponse, error) {
	if request.Binding.Fence != owner.binding.Fence || request.Binding.InvocationRef != owner.binding.InvocationRef {
		owner.t.Fatal("read changed binding")
	}
	owner.reads++
	receipt := owner.initial
	if owner.completes > 0 {
		receipt = owner.response
	}
	return &cp.GetManagedKubernetesInvocationResponse{Receipt: proto.Clone(receipt).(*cp.ManagedKubernetesInvocationReceipt)}, nil
}

// RenewManagedKubernetesInvocation keeps the same synthetic lease during approval waiting; it creates no real authority.
// RenewManagedKubernetesInvocation сохраняет ту же synthetic lease при ожидании approval и не создаёт настоящие полномочия.
func (owner *kubernetesExecutionOwner) RenewManagedKubernetesInvocation(_ context.Context, request *cp.RenewManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.RenewManagedKubernetesInvocationResponse, error) {
	if request.Binding.Fence != owner.binding.Fence || request.Binding.Generation != owner.binding.Generation {
		owner.t.Fatal("renew changed binding")
	}
	return &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: owner.binding.LeaseRef, Generation: owner.binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(20 * time.Second))}, nil
}

// CancelManagedKubernetesInvocation requires an independent deadline and preserves a previously recorded result.
// CancelManagedKubernetesInvocation требует independent deadline и сохраняет ранее записанный результат.
func (owner *kubernetesExecutionOwner) CancelManagedKubernetesInvocation(ctx context.Context, request *cp.CancelManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.CancelManagedKubernetesInvocationResponse, error) {
	owner.cancels++
	if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil || request.Binding.Fence != owner.binding.Fence {
		owner.t.Fatal("cleanup lost its original binding or deadline")
	}
	receipt := proto.Clone(owner.response).(*cp.ManagedKubernetesInvocationReceipt)
	if owner.completes == 0 || receipt.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
		receipt.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
		receipt.CompletionRecorded = false
		receipt.ReceiptRef = ""
		receipt.ResultDigest = ""
	}
	return &cp.CancelManagedKubernetesInvocationResponse{Receipt: receipt}, nil
}

// kubernetesExecutionBroker replaces only RPC transport, retaining real adapters, stream validation and renewal ownership.
// kubernetesExecutionBroker заменяет лишь RPC transport, сохраняя реальные adapters, stream validation и renewal ownership.
type kubernetesExecutionBroker struct {
	sb.ManagedKubernetesServiceClient
	owner                     *kubernetesExecutionOwner
	stop                      context.CancelFunc
	variant                   string
	calls, exchanges, cancels int
}

// ExecuteManagedKubernetesOperation supplies explicit pending details or one started result and detects duplicate execution.
// ExecuteManagedKubernetesOperation выдаёт explicit pending details либо один started result и выявляет duplicate execution.
func (broker *kubernetesExecutionBroker) ExecuteManagedKubernetesOperation(_ context.Context, request *sb.ExecuteManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	broker.calls++
	if !proto.Equal(request.Binding, broker.owner.binding) {
		broker.owner.t.Fatal("execution changed binding")
	}
	if broker.variant == "gate" && broker.calls <= 10 || broker.variant == "reconcile" {
		reason := "KUBERNETES_APPROVAL_REQUIRED"
		metadata := map[string]string{"planRef": broker.owner.initial.PlanRef, "childRef": "child_fixture", "gateRef": "gate_fixture"}
		if broker.variant == "reconcile" {
			reason = "KUBERNETES_RECONCILIATION_REQUIRED"
			delete(metadata, "gateRef")
			metadata["effectRef"] = "effect_fixture"
			metadata["effectGeneration"] = "1"
		}
		pending, err := status.New(codes.FailedPrecondition, "synthetic pending").WithDetails(&errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: reason, Metadata: metadata})
		if err != nil {
			broker.owner.t.Fatal(err)
		}
		return nil, pending.Err()
	}
	if strings.HasPrefix(broker.variant, "stream") || strings.HasPrefix(broker.variant, "cancel-") {
		return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Stream{Stream: &sb.ManagedKubernetesStreamDescriptor{StreamRef: "stream_fixture", InvocationRef: request.Binding.InvocationRef, Generation: request.Binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 1024, MaximumTotalBytes: 4096, OutputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}}}}, nil
	}
	raw := []byte(`{"metadata":"safe"}`)
	digest := sha256.Sum256(raw)
	response := &sb.ManagedKubernetesSafeResult{EffectRef: "effect_fixture", EffectGeneration: 1, InvocationGeneration: request.Binding.Generation, ResultJson: raw, ResultDigest: hex.EncodeToString(digest[:]), AuthorizedUntil: timestamppb.New(time.Now().Add(20 * time.Second)), RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED}
	if broker.variant == "malformed" {
		response.ResultDigest = strings.Repeat("0", 64)
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: response}}, nil
}

// ExchangeManagedKubernetesStream emits one real frame then requires its accepted-output ACK before terminal state.
// ExchangeManagedKubernetesStream выдаёт один реальный frame и требует accepted-output ACK до terminal state.
func (broker *kubernetesExecutionBroker) ExchangeManagedKubernetesStream(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	broker.exchanges++
	if broker.variant == "cancel-exchange" && broker.exchanges == 2 {
		broker.stop()
		return nil, status.Error(codes.Canceled, "synthetic cancellation")
	}
	if request.Input != nil || !proto.Equal(request.Binding, broker.owner.binding) {
		broker.owner.t.Fatal("runner fabricated input or changed binding")
	}
	if broker.exchanges == 1 {
		broker.owner.outputUntil = time.Now().Add(time.Second).Truncate(time.Microsecond)
		payload := []byte("actual-stream-output")
		digest := sha256.Sum256(payload)
		return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING, AuthorizedUntil: timestamppb.New(broker.owner.outputUntil), Output: []*sb.ManagedKubernetesFrame{{Sequence: 1, DeliveryOrdinal: 1, Channel: sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT, Payload: payload, PayloadDigest: hex.EncodeToString(digest[:])}}}, nil
	}
	if request.AcknowledgedOutputSequence != 1 || broker.owner.outputCalls != 1 || broker.owner.outputReceipt.State != cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_ACCEPTED {
		broker.owner.t.Fatal("output was not accepted before ACK")
	}
	return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, ReasonCode: "CLEAN_EOF"}, nil
}

// CancelManagedKubernetesOperation proves actual cleanup ordering independently from terminal response.
// CancelManagedKubernetesOperation доказывает порядок cleanup независимо от terminal response.
func (broker *kubernetesExecutionBroker) CancelManagedKubernetesOperation(ctx context.Context, request *sb.CancelManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.CancelManagedKubernetesOperationResponse, error) {
	broker.cancels++
	if broker.owner.cancels != 1 || ctx.Err() != nil || request.StreamRef != "stream_fixture" || !proto.Equal(request.Binding, broker.owner.binding) {
		broker.owner.t.Fatal("broker cleanup preceded owner closure")
	}
	return &sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN, TransportJoined: true}, nil
}

// kubernetesExecutionInput builds genuine shipped-package pins for a synthetic claim and uses production input validation.
// kubernetesExecutionInput строит настоящие shipped-package pins synthetic claim и использует production input validation.
func kubernetesExecutionInput(t *testing.T, definition integrationpackage.Package, operation string) kubernetesActivityInput {
	t.Helper()
	capability, ok := definition.Capability(operation)
	if !ok {
		t.Fatal("missing capability")
	}
	input := map[string]any{"group": "apps", "version": "v1", "resource": "deployments", "namespace": "example", "name": "web", "uid": "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", "resourceVersion": "17"}
	if operation == "kubernetes.exec" {
		input["group"], input["resource"] = "", "pods"
		input["options"] = map[string]any{"container": "app", "command": []string{"cat"}, "stdin": true, "timeoutSeconds": 10}
	}
	if operation == "kubernetes.logs" {
		input["options"] = map[string]any{"timeoutSeconds": 10}
	}
	configuration := map[string]string{"target_ref": "target_fixture"}
	scope, err := capability.KubernetesResourceScope(configuration, "example")
	if err != nil {
		t.Fatal(err)
	}
	scopeBytes, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(scopeBytes)
	raw, _ := json.Marshal(input)
	canonical, err := capability.ValidateInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	encoded, _ := json.Marshal(definition)
	request := integration.Request{DefinitionPackage: encoded, DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, ConnectionRef: "connection_fixture", CapabilityKey: capability.Key, Operation: operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, ResourceKind: capability.ResourceScope.Kind, ResourceScope: scope, ResourceScopeDigest: hex.EncodeToString(scopeDigest[:]), Input: input, InputDigest: hex.EncodeToString(digest[:]), Configuration: map[string]any{"target_ref": "target_fixture"}, EffectKey: "effect_fixture", GrantRef: "grant_fixture", GrantVersion: 1, ResourcePermissionProfile: integrationpackage.CredentialResourcePermissionKubernetesScopedV1}
	binding := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 7, InputDigest: request.InputDigest}
	return kubernetesActivityInput{request: request, binding: binding, leaseUntil: time.Now().Add(20 * time.Second)}
}

// TestKubernetesExecutionComposesActualLifecycle exercises actual broker/owner adapters, stream sink ACK, approval wait, no reconciliation replay and cleanup join.
// TestKubernetesExecutionComposesActualLifecycle исполняет actual broker/owner adapters, stream sink ACK, approval wait, no reconciliation replay и cleanup join.
func TestKubernetesExecutionComposesActualLifecycle(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: 10 * time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"resource", "gate", "reconcile", "incomplete-plan", "malformed", "stream", "stream-lost", "stream-rejected", "stream-unknown", "cancel-stream", "stream-readonly-skip-input", "cancel-exchange", "before-start", "expired-lease"} {
		t.Run(variant, func(t *testing.T) {
			operation := "kubernetes.get"
			if strings.HasPrefix(variant, "stream") || strings.HasPrefix(variant, "cancel-") {
				operation = "kubernetes.logs"
			}
			input := kubernetesExecutionInput(t, definitions[integrationpackage.KubernetesDefinitionKey], operation)
			owner := &kubernetesExecutionOwner{kubernetesCompletionOwner: kubernetesCompletionFixture(t)}
			owner.binding = input.binding
			digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: input.binding.WorkloadInstance, InvocationRef: input.binding.InvocationRef, LeaseRef: input.binding.LeaseRef, Fence: input.binding.Fence, Generation: input.binding.Generation, InputDigest: input.binding.InputDigest})
			if err != nil {
				t.Fatal(err)
			}
			for _, receipt := range []*cp.ManagedKubernetesInvocationReceipt{owner.initial, owner.after, owner.response} {
				receipt.InputDigest = input.binding.InputDigest
				receipt.BindingDigest = digest
			}
			if variant == "incomplete-plan" {
				owner.response = proto.Clone(owner.initial).(*cp.ManagedKubernetesInvocationReceipt)
			}
			broker := &kubernetesExecutionBroker{owner: owner, variant: variant}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			broker.stop = cancel
			owner.stop, owner.variant = cancel, variant
			if variant == "before-start" {
				cancel()
			}
			if variant == "expired-lease" {
				input.leaseUntil = time.Now().Add(-time.Second)
			}
			execution, err := newKubernetesExecution(adapter, owner, broker, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = execution.run(ctx, input)
			expectedFailure := variant == "malformed" || variant == "incomplete-plan" || variant == "cancel-stream" || variant == "stream-rejected" || variant == "stream-unknown"
			if (err != nil) != expectedFailure || owner.cancels != 1 {
				t.Fatalf("unexpected execution outcome: variant=%s error=%v cancels=%d", variant, err, owner.cancels)
			}
			expectedCalls := 1
			if variant == "before-start" || variant == "expired-lease" {
				expectedCalls = 0
			}
			if variant == "gate" {
				expectedCalls = 11
			}
			if broker.calls != expectedCalls {
				t.Fatal("uncertain effect replayed or approval polling truncated")
			}
			expectedGets := 0
			if variant == "stream-lost" || variant == "stream-unknown" {
				expectedGets = 1
			}
			if owner.outputGets != expectedGets {
				t.Fatal("output reconciliation repeated or skipped")
			}
			if variant == "stream-rejected" || variant == "stream-unknown" || variant == "cancel-stream" {
				if broker.exchanges != 1 || owner.completes != 0 {
					t.Fatal("unaccepted output acknowledged or completed")
				}
			}
			if operation == "kubernetes.logs" && (owner.outputCalls != 1 || broker.cancels != 1) {
				t.Fatal("stream did not retain accepted bytes and positive cleanup join")
			}
		})
	}
}
