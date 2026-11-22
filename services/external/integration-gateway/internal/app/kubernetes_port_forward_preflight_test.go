package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

// portForwardPreflightBroker returns only the actual broker's closed no-effect signal; any stream call is deliberately unimplemented.
// portForwardPreflightBroker возвращает лишь actual broker closed no-effect signal; любой stream call намеренно не реализован.
type portForwardPreflightBroker struct {
	sb.ManagedKubernetesServiceClient
	calls     int
	duplicate bool
}

// ExecuteManagedKubernetesOperation emits one typed failure without a completed effect or stream descriptor.
// ExecuteManagedKubernetesOperation выдаёт typed failure без completed effect либо stream descriptor.
func (broker *portForwardPreflightBroker) ExecuteManagedKubernetesOperation(context.Context, *sb.ExecuteManagedKubernetesOperationRequest, ...grpc.CallOption) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	broker.calls++
	info := &errdetails.ErrorInfo{Domain: "kodex.secret-broker", Reason: managedkubernetes.ReasonPortForwardProfileUnsupported}
	result, _ := status.New(codes.FailedPrecondition, "synthetic private diagnostic").WithDetails(info)
	if broker.duplicate {
		result, _ = result.WithDetails(info)
	}
	return nil, result.Err()
}

// portForwardPreflightOwner checks the exact new request branch before delegating existing strict original-plan fixture behavior.
// portForwardPreflightOwner проверяет exact new request branch до existing strict original-plan fixture behavior.
type portForwardPreflightOwner struct{ *kubernetesExecutionOwner }

// CompleteManagedKubernetesInvocation rejects an accidental generic completion that would lose the canonical owner preflight reason.
// CompleteManagedKubernetesInvocation отклоняет accidental generic completion, теряющий canonical owner preflight reason.
func (owner *portForwardPreflightOwner) CompleteManagedKubernetesInvocation(ctx context.Context, request *cp.CompleteManagedKubernetesInvocationRequest, options ...grpc.CallOption) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
	if request.PreflightRejection != cp.ManagedKubernetesPreflightRejection_MANAGED_KUBERNETES_PREFLIGHT_REJECTION_PORT_FORWARD_PROFILE_UNSUPPORTED {
		owner.t.Fatal("runner lost the closed preflight branch")
	}
	return owner.kubernetesCompletionOwner.CompleteManagedKubernetesInvocation(ctx, request, options...)
}

// TestPortForwardPreflightRunnerPersistsFailureWithoutDispatchReplay crosses actual request validation, typed broker failure, owner completion, lost ACK readback and independent cleanup.
// TestPortForwardPreflightRunnerPersistsFailureWithoutDispatchReplay проходит actual request validation, typed broker failure, owner completion, lost ACK readback и independent cleanup.
func TestPortForwardPreflightRunnerPersistsFailureWithoutDispatchReplay(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: 10 * time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"accepted", "lost", "malformed"} {
		t.Run(variant, func(t *testing.T) {
			definition := definitions[integrationpackage.KubernetesDefinitionKey]
			input := kubernetesExecutionInput(t, definition, "kubernetes.exec")
			capability, ok := definition.Capability("kubernetes.port-forward")
			if !ok {
				t.Fatal("missing port-forward capability")
			}
			input.request.Operation, input.request.CapabilityKey = capability.Operation, capability.Key
			input.request.Risk, input.request.ApprovalPolicy = capability.Risk, capability.ApprovalPolicy
			input.request.Input["options"] = map[string]any{"ports": []map[string]any{{"port": 18080, "targetPort": 8080}}, "timeoutSeconds": 10}
			raw, _ := json.Marshal(input.request.Input)
			canonical, err := capability.ValidateInput(raw)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(canonical)
			input.request.InputDigest = hex.EncodeToString(sum[:])
			input.binding.InputDigest = input.request.InputDigest
			base := &kubernetesExecutionOwner{kubernetesCompletionOwner: kubernetesCompletionFixture(t)}
			owner := &portForwardPreflightOwner{base}
			owner.binding = input.binding
			digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: input.binding.WorkloadInstance, InvocationRef: input.binding.InvocationRef, LeaseRef: input.binding.LeaseRef, Fence: input.binding.Fence, Generation: input.binding.Generation, InputDigest: input.binding.InputDigest})
			if err != nil {
				t.Fatal(err)
			}
			for _, receipt := range []*cp.ManagedKubernetesInvocationReceipt{owner.initial, owner.after, owner.response} {
				receipt.InputDigest = input.binding.InputDigest
				receipt.BindingDigest = digest
			}
			owner.after.State, owner.response.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED, cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED
			broker := &portForwardPreflightBroker{duplicate: variant == "malformed"}
			if variant == "lost" {
				owner.completeErr = status.Error(codes.Unavailable, "synthetic lost ACK")
			}
			execution, err := newKubernetesExecution(adapter, owner, broker, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = execution.execute(t.Context(), input)
			if (err != nil) != (variant == "malformed") || broker.calls != 1 || owner.cancels != 1 || owner.outputCalls != 0 {
				t.Fatal("preflight was replayed, lost or promoted to native output", err)
			}
			expectedComplete := 1
			if variant == "malformed" {
				expectedComplete = 0
			}
			if owner.completes != expectedComplete {
				t.Fatal("malformed/generic error reached preflight completion")
			}
			if variant == "lost" && owner.reads != 2 {
				t.Fatal("lost completion did not use one exact metadata read")
			}
		})
	}
}
