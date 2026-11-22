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

// sshResultClient records the exact runtime proof; it never simulates database authorization.
// sshResultClient записывает точное доказательство runtime, не имитируя авторизацию БД.
type sshResultClient struct {
	cp.ManagedSSHOwnerServiceClient
	request  *cp.ReadManagedSSHInvocationRequest
	response *cp.ReadManagedSSHInvocationResponse
}

// ReadManagedSSHInvocation clones the proof and returns a configured closed projection for callback testing.
// ReadManagedSSHInvocation копирует доказательство и возвращает заданную проекцию для проверки callback.
func (c *sshResultClient) ReadManagedSSHInvocation(_ context.Context, q *cp.ReadManagedSSHInvocationRequest, _ ...grpc.CallOption) (*cp.ReadManagedSSHInvocationResponse, error) {
	c.request = proto.Clone(q).(*cp.ReadManagedSSHInvocationRequest)
	return c.response, nil
}

// TestSSHRuntimeResultRouting proves a child Gate uses exact execution pins without a generic reader fallback.
// TestSSHRuntimeResultRouting проверяет точные execution pins дочернего Gate без fallback на общий reader.
func TestSSHRuntimeResultRouting(t *testing.T) {
	generic := &integrationResultClient{invocation: "inv_fixture_runtime"}
	private := &sshResultClient{response: &cp.ReadManagedSSHInvocationResponse{InvocationRef: generic.invocation, State: cp.ManagedSSHInvocationState_MANAGED_SSH_INVOCATION_STATE_RUNNING, WaitingForGate: true}}
	s := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: generic, SSHOperations: private}}
	grant := integrationGrantFixture()
	grant.DefinitionKey = integrationpackage.SSHManagedDefinitionKey
	grant.CapabilityKey = "ssh.system.read"
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
	s.control.SSHOperations = nil
	if _, err := s.readInvocationResult(t.Context(), input, grant, generic.invocation); err == nil || len(generic.reads) != 0 {
		t.Fatal("missing private reader fell back")
	}
}

// TestSSHRuntimeResultRejectsFalseSuccess checks mixed-origin, unknown, corrupt and contradictory owner responses.
// TestSSHRuntimeResultRejectsFalseSuccess проверяет чужие, неизвестные, повреждённые и противоречивые ответы владельца.
func TestSSHRuntimeResultRejectsFalseSuccess(t *testing.T) {
	digest := sha256.Sum256([]byte("safe result"))
	base := &cp.ReadManagedSSHInvocationResponse{InvocationRef: "inv_fixture_runtime", State: cp.ManagedSSHInvocationState_MANAGED_SSH_INVOCATION_STATE_SUCCEEDED, ResultSummary: "safe result", EffectReceiptRef: "erc_fixture_receipt", ResultDigest: hex.EncodeToString(digest[:])}
	if _, err := sshInvocationResult(base.InvocationRef, base); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*cp.ReadManagedSSHInvocationResponse){
		"foreign invocation": func(v *cp.ReadManagedSSHInvocationResponse) { v.InvocationRef = "inv_foreign_runtime" },
		"unknown state":      func(v *cp.ReadManagedSSHInvocationResponse) { v.State = 999 },
		"unknown wire":       func(v *cp.ReadManagedSSHInvocationResponse) { v.ProtoReflect().SetUnknown([]byte{0x78, 0x01}) },
		"corrupt body":       func(v *cp.ReadManagedSSHInvocationResponse) { v.ResultSummary += "changed" },
		"missing receipt":    func(v *cp.ReadManagedSSHInvocationResponse) { v.EffectReceiptRef = "" },
		"terminal gate":      func(v *cp.ReadManagedSSHInvocationResponse) { v.WaitingForGate = true },
		"failure receipt": func(v *cp.ReadManagedSSHInvocationResponse) {
			v.State = cp.ManagedSSHInvocationState_MANAGED_SSH_INVOCATION_STATE_FAILED
		},
		"raw diagnostic": func(v *cp.ReadManagedSSHInvocationResponse) { v.SafeErrorCode = "private error text" },
		"oversize":       func(v *cp.ReadManagedSSHInvocationResponse) { v.ResultSummary = strings.Repeat("x", (64<<10)+1) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			v := proto.Clone(base).(*cp.ReadManagedSSHInvocationResponse)
			mutate(v)
			if _, err := sshInvocationResult(base.InvocationRef, v); err == nil {
				t.Fatal("invalid owner projection accepted")
			}
		})
	}
}
