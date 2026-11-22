package callback

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// readSSHInvocationResult selects the fixed SSH reader only from the original pinned grant and forwards the original runtime lease; absence or mismatch never falls back to generic reads.
// readSSHInvocationResult выбирает fixed SSH reader лишь из original pinned grant и передаёт original runtime lease; отсутствие либо mismatch не включают generic reads.
func (s *Server) readSSHInvocationResult(ctx context.Context, input runtimecontract.RunnerInput, grant runtimecontract.RunnerIntegrationGrant, inv string) (*cp.GetIntegrationInvocationResponse, error) {
	contract, known := integrationpackage.CredentialResourcePermissionContractForProfile(grant.DefinitionKey, grant.CapabilityKey, integrationpackage.CredentialResourcePermissionSSHScopedV1)
	if !known || contract.Operation != grant.Operation || s.control.SSHOperations == nil {
		return nil, status.Error(codes.Unavailable, "SSH result reader unavailable")
	}
	result, err := s.control.SSHOperations.ReadManagedSSHInvocation(ctx, &cp.ReadManagedSSHInvocationRequest{InvocationRef: inv,
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, RuntimeInputDigest: input.InputDigest})
	if err != nil {
		return nil, err
	}
	return sshInvocationResult(inv, result)
}

// sshInvocationResult validates the complete owner envelope before the existing synchronous invoke/poll loop consumes it; a result is never a fresh execution admission.
// sshInvocationResult проверяет полный owner envelope до existing synchronous invoke/poll loop; result не является fresh execution admission.
func sshInvocationResult(inv string, v *cp.ReadManagedSSHInvocationResponse) (*cp.GetIntegrationInvocationResponse, error) {
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.Unavailable, runtimeResultProjectionInvalid)
	}
	return runtimeInvocationResult(inv, v.InvocationRef, strings.TrimPrefix(v.State.String(), "MANAGED_SSH_INVOCATION_STATE_"),
		v.ResultSummary, v.SafeErrorCode, v.EffectReceiptRef, v.ResultDigest, v.WaitingForGate)
}
