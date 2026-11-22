package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// kubernetesOperationFixture retains the exact binding and returns only synthetic domain results.
// kubernetesOperationFixture сохраняет exact binding и возвращает только synthetic domain results.
type kubernetesOperationFixture struct {
	result  targetrepo.ResourceExecution
	binding managedkubernetes.Binding
	calls   int
	err     error
}

// Execute exposes invocation count so invalid transport input cannot silently reach the owner lane.
// Execute учитывает invocation count, чтобы invalid transport input не достигал owner lane незаметно.
func (fixture *kubernetesOperationFixture) Execute(_ context.Context, binding managedkubernetes.Binding) (targetrepo.OperationExecution, error) {
	fixture.calls++
	fixture.binding = binding
	return targetrepo.OperationExecution{Effect: fixture.result}, fixture.err
}

// kubernetesOperationRPCFixture builds an invocation with no actor, target or credential selector.
// kubernetesOperationRPCFixture строит invocation без actor, target либо credential selector.
func kubernetesOperationRPCFixture() (*sb.ExecuteManagedKubernetesOperationRequest, *kubernetesOperationFixture) {
	raw := []byte(`{"safe":true}`)
	digest := sha256.Sum256(raw)
	return &sb.ExecuteManagedKubernetesOperationRequest{Binding: &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_synthetic", InvocationRef: "inv_synthetic", LeaseRef: "lease_synthetic", Fence: "fence_synthetic", Generation: 7, InputDigest: strings.Repeat("a", 64)}},
		&kubernetesOperationFixture{result: targetrepo.ResourceExecution{PlanRef: "plan_synthetic", ChildRef: "child_synthetic", EffectRef: "effect_synthetic", EffectGeneration: 3, AuthorizedUntil: time.Now().Add(10 * time.Second), Result: &targetrepo.AcceptedEffectResult{Outcome: "SUCCEEDED", ResultJSON: raw, ResultDigest: hex.EncodeToString(digest[:])}}}
}

// invokeKubernetesOperation applies the actual trusted-profile interceptor before the RPC handler.
// invokeKubernetesOperation применяет actual trusted-profile interceptor перед RPC handler.
func invokeKubernetesOperation(t *testing.T, server *Server, request *sb.ExecuteManagedKubernetesOperationRequest, rawMetadata bool, caller, operation string) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	t.Helper()
	method := sb.ManagedKubernetesService_ExecuteManagedKubernetesOperation_FullMethodName
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller))
	if rawMetadata {
		return server.ExecuteManagedKubernetesOperation(ctx, request)
	}
	authorizer, err := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.ServiceActor}})
	if err != nil {
		t.Fatal(err)
	}
	var response *sb.ExecuteManagedKubernetesOperationResponse
	_, err = authorizer.UnaryServerInterceptor()(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		var handlerErr error
		response, handlerErr = server.ExecuteManagedKubernetesOperation(ctx, request)
		return response, handlerErr
	})
	return response, err
}

// TestManagedKubernetesResourceRPCBoundary rejects forged routing and preserves exact owner generations.
// TestManagedKubernetesResourceRPCBoundary отвергает forged routing и сохраняет exact owner generations.
func TestManagedKubernetesResourceRPCBoundary(t *testing.T) {
	for _, variant := range []string{"success", "raw-metadata", "wrong-caller", "wrong-operation", "unknown-request", "unknown-binding", "both-bindings", "private-error", "unwired"} {
		t.Run(variant, func(t *testing.T) {
			request, fixture := kubernetesOperationRPCFixture()
			server := &Server{}
			WithKubernetesOperations(fixture)(server)
			caller, operation := "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", "kubernetes.execute"
			want := codes.OK
			switch variant {
			case "raw-metadata":
				want = codes.PermissionDenied
			case "wrong-caller":
				caller, want = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller", codes.PermissionDenied
			case "wrong-operation":
				operation, want = "kubernetes.cancel", codes.PermissionDenied
			case "unknown-request":
				request.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
				want = codes.InvalidArgument
			case "unknown-binding":
				request.Binding.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
				want = codes.InvalidArgument
			case "both-bindings":
				request.Binding.ConnectionTestRef = "test_synthetic"
				want = codes.InvalidArgument
			case "private-error":
				fixture.err = errors.New("PRIVATE_SYNTHETIC_ERROR")
				want = codes.Unavailable
			case "unwired":
				server.kubernetesOperations = nil
				want = codes.Unavailable
			}
			response, err := invokeKubernetesOperation(t, server, request, variant == "raw-metadata", caller, operation)
			if status.Code(err) != want || strings.Contains(status.Convert(err).Message(), "PRIVATE_SYNTHETIC") {
				t.Fatalf("unexpected RPC boundary: %s", status.Code(err))
			}
			if variant != "success" && variant != "private-error" {
				if fixture.calls != 0 || response != nil {
					t.Fatal("unadmitted RPC reached consumer")
				}
				return
			}
			if fixture.calls != 1 || fixture.binding.Fence != request.Binding.Fence || fixture.binding.InputDigest != request.Binding.InputDigest || fixture.binding.Generation != request.Binding.Generation {
				t.Fatal("original binding changed")
			}
			if !bytes.Equal(fixture.result.Result.ResultJSON, make([]byte, len(fixture.result.Result.ResultJSON))) {
				t.Fatal("domain result bytes not cleared")
			}
			if variant == "success" {
				completed := response.GetCompleted()
				if completed == nil || string(completed.ResultJson) != `{"safe":true}` || completed.EffectGeneration != 3 || completed.InvocationGeneration != 7 || completed.AuthorizedUntil == nil {
					t.Fatal("result lost owner generation or delivery deadline")
				}
			}
		})
	}
}

// TestManagedKubernetesResourceRPCReceipt rejects stale or contradictory completion and never turns a gate into success.
// TestManagedKubernetesResourceRPCReceipt отвергает stale/contradictory completion и не превращает gate в success.
func TestManagedKubernetesResourceRPCReceipt(t *testing.T) {
	for _, variant := range []string{"gate", "mixed-gate", "expired", "wide-credit", "digest", "unknown", "failed", "reconcile", "mixed-reconcile", "canceled"} {
		t.Run(variant, func(t *testing.T) {
			request, fixture := kubernetesOperationRPCFixture()
			switch variant {
			case "gate":
				fixture.result = targetrepo.ResourceExecution{GateRef: "gate_synthetic", PlanRef: "plan_synthetic", ChildRef: "child_synthetic"}
			case "mixed-gate":
				fixture.result.GateRef = "gate_synthetic"
			case "expired":
				fixture.result.AuthorizedUntil = time.Now().Add(-time.Second)
			case "wide-credit":
				fixture.result.AuthorizedUntil = time.Now().Add(time.Hour)
			case "digest":
				fixture.result.Result.ResultDigest = strings.Repeat("b", 64)
			case "unknown":
				fixture.result.Result.Outcome, fixture.result.Result.ReasonCode = "UNKNOWN", managedkubernetes.ReasonTransportUnknown
			case "failed":
				fixture.result.Result.Outcome, fixture.result.Result.ReasonCode = "FAILED", managedkubernetes.ReasonScopeDenied
			case "reconcile":
				fixture.result.ReconcileOnly, fixture.result.Result = true, nil
				fixture.result.AuthorizedUntil = time.Time{}
			case "mixed-reconcile":
				fixture.result.ReconcileOnly, fixture.result.Result = true, nil
			}
			binding := managedkubernetes.Binding{Generation: request.Binding.Generation}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if variant == "canceled" {
				cancel()
			}
			response, err := managedKubernetesResourceResponse(ctx, binding, fixture.result)
			switch variant {
			case "gate":
				if response != nil || status.Code(err) != codes.FailedPrecondition {
					t.Fatal("gate became completion")
				}
				details := status.Convert(err).Details()
				if len(details) != 1 {
					t.Fatal("typed gate missing")
				}
				info, ok := details[0].(*errdetails.ErrorInfo)
				if !ok || info.Domain != "kodex.control-plane" || info.Reason != "KUBERNETES_APPROVAL_REQUIRED" || info.Metadata["planRef"] != fixture.result.PlanRef || info.Metadata["childRef"] != fixture.result.ChildRef || info.Metadata["gateRef"] != fixture.result.GateRef || len(info.Metadata) != 3 {
					t.Fatal("gate pins changed")
				}
			case "reconcile":
				if response != nil || status.Code(err) != codes.FailedPrecondition {
					t.Fatal("reconciliation became completion")
				}
				details := status.Convert(err).Details()
				if len(details) != 1 {
					t.Fatal("typed reconciliation missing")
				}
				info, ok := details[0].(*errdetails.ErrorInfo)
				if !ok || info.Domain != "kodex.control-plane" || info.Reason != "KUBERNETES_RECONCILIATION_REQUIRED" || info.Metadata["planRef"] != fixture.result.PlanRef || info.Metadata["childRef"] != fixture.result.ChildRef || info.Metadata["effectRef"] != fixture.result.EffectRef || info.Metadata["effectGeneration"] != "3" || len(info.Metadata) != 4 {
					t.Fatal("reconciliation pins changed")
				}
			case "unknown", "failed":
				if err != nil || response.GetCompleted() == nil || response.GetCompleted().RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED {
					t.Fatal("remote failure promoted")
				}
			default:
				if err == nil || response != nil {
					t.Fatal("stale or contradictory result released")
				}
			}
		})
	}
}
