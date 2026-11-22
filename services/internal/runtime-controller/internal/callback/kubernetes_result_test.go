package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
	"time"
)

// kubernetesResultClient records the exact runtime proof; it never simulates database authorization.
// kubernetesResultClient записывает точное доказательство runtime, не имитируя авторизацию БД.
type kubernetesResultClient struct {
	cp.ManagedKubernetesOwnerServiceClient
	request  *cp.ReadManagedKubernetesInvocationRequest
	response *cp.ReadManagedKubernetesInvocationResponse
}

// ReadManagedKubernetesInvocation clones the proof and returns a configured closed projection for callback testing.
// ReadManagedKubernetesInvocation копирует доказательство и возвращает заданную проекцию для проверки callback.
func (c *kubernetesResultClient) ReadManagedKubernetesInvocation(_ context.Context, q *cp.ReadManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.ReadManagedKubernetesInvocationResponse, error) {
	c.request = proto.Clone(q).(*cp.ReadManagedKubernetesInvocationRequest)
	return c.response, nil
}

// TestKubernetesRuntimeResultRouting proves a child Gate uses exact execution pins without a generic reader fallback.
// TestKubernetesRuntimeResultRouting проверяет точные execution pins дочернего Gate без fallback на общий reader.
func TestKubernetesRuntimeResultRouting(t *testing.T) {
	generic := &integrationResultClient{invocation: "inv_fixture_runtime"}
	private := &kubernetesResultClient{response: &cp.ReadManagedKubernetesInvocationResponse{InvocationRef: generic.invocation, State: cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_RUNNING, WaitingForGate: true}}
	s := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: generic, KubernetesOperations: private}}
	grant := integrationGrantFixture()
	grant.DefinitionKey = integrationpackage.KubernetesDefinitionKey
	grant.CapabilityKey = "kubernetes.identity"
	grant.Operation = grant.CapabilityKey
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "nod_fixture", LeaseRef: "lea_fixture", LeaseFence: "private-synthetic-fence", LeaseGeneration: 3, InputDigest: strings.Repeat("a", 64), IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	got, err := s.invoke(t.Context(), input, integrationArguments(grant, "x"), json.RawMessage(`"call"`))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(integrationToolResult)
	if !ok || !result.OwnerDecisionRequired || result.OK || result.ErrorCode != "INTEGRATION_APPROVAL_PENDING" {
		t.Fatal("child gate was not pending")
	}
	if len(generic.reads) != 0 || private.request == nil || private.request.LeaseRef != input.LeaseRef || private.request.Fence != input.LeaseFence || private.request.Generation != input.LeaseGeneration || private.request.RuntimeInputDigest != input.InputDigest || private.request.InvocationRef != generic.invocation {
		t.Fatal("runtime origin proof was replaced")
	}
	s.control.KubernetesOperations = nil
	if _, err := s.readInvocationResult(t.Context(), input, grant, generic.invocation); err == nil || len(generic.reads) != 0 {
		t.Fatal("missing private reader fell back")
	}
}

// TestKubernetesRuntimeResultRejectsFalseSuccess checks mixed-origin, unknown, corrupt and contradictory owner responses.
// TestKubernetesRuntimeResultRejectsFalseSuccess проверяет чужие, неизвестные, повреждённые и противоречивые ответы владельца.
func TestKubernetesRuntimeResultRejectsFalseSuccess(t *testing.T) {
	digest := sha256.Sum256([]byte("safe result"))
	base := &cp.ReadManagedKubernetesInvocationResponse{InvocationRef: "inv_fixture_runtime", State: cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_SUCCEEDED, ResultSummary: "safe result", EffectReceiptRef: "erc_fixture_receipt", ResultDigest: hex.EncodeToString(digest[:])}
	if _, err := kubernetesInvocationResult(base.InvocationRef, base); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*cp.ReadManagedKubernetesInvocationResponse){
		"foreign invocation": func(v *cp.ReadManagedKubernetesInvocationResponse) { v.InvocationRef = "inv_foreign_runtime" },
		"unknown state":      func(v *cp.ReadManagedKubernetesInvocationResponse) { v.State = 999 },
		"unknown wire":       func(v *cp.ReadManagedKubernetesInvocationResponse) { v.ProtoReflect().SetUnknown([]byte{0x78, 0x01}) },
		"corrupt body":       func(v *cp.ReadManagedKubernetesInvocationResponse) { v.ResultSummary += "changed" },
		"missing receipt":    func(v *cp.ReadManagedKubernetesInvocationResponse) { v.EffectReceiptRef = "" },
		"terminal gate":      func(v *cp.ReadManagedKubernetesInvocationResponse) { v.WaitingForGate = true },
		"failure receipt": func(v *cp.ReadManagedKubernetesInvocationResponse) {
			v.State = cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_FAILED
		},
		"raw diagnostic": func(v *cp.ReadManagedKubernetesInvocationResponse) { v.SafeErrorCode = "private error text" },
		"oversize":       func(v *cp.ReadManagedKubernetesInvocationResponse) { v.ResultSummary = strings.Repeat("x", (64<<10)+1) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			v := proto.Clone(base).(*cp.ReadManagedKubernetesInvocationResponse)
			mutate(v)
			if _, err := kubernetesInvocationResult(base.InvocationRef, v); err == nil {
				t.Fatal("invalid owner projection accepted")
			}
		})
	}
}
