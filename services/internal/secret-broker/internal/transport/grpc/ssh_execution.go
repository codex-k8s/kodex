package grpc

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sshExecutionRejected hides private owner, key and provider diagnostics on every execution RPC.
// sshExecutionRejected скрывает private owner, key и provider diagnostics каждого execution RPC.
const sshExecutionRejected = "SSH execution rejected"

// sshIdentityExecutionProtocol owns a single joined operation and a separate metadata-only reconciliation path; it cannot accept raw commands or key locators.
// sshIdentityExecutionProtocol владеет одной joined operation и отдельным metadata-only reconciliation path; raw commands и key locators не принимаются.
type sshIdentityExecutionProtocol interface {
	// Execute preserves original admission through finite dispatch and bounded completion after local transport join.
	// Execute сохраняет original admission через finite dispatch и bounded completion после local transport join.
	Execute(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error)
	// Get reads original metadata without reading key bytes, dispatching SSH or granting another attempt.
	// Get читает original metadata без key bytes, SSH dispatch либо новой attempt.
	Get(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error)
}

// WithSSHIdentityExecution binds the private purpose protocol during trusted composition; an absent implementation remains unavailable.
// WithSSHIdentityExecution связывает private purpose protocol в trusted composition; absent implementation остаётся unavailable.
func WithSSHIdentityExecution(service sshIdentityExecutionProtocol) Option {
	return func(server *Server) { server.sshExecution = service }
}

// verifySSHExecutionGateway checks exact transport identity, target, method and permission before parsing claim coordinates; mTLS alone grants no execution.
// verifySSHExecutionGateway проверяет exact transport identity, target, method и permission до claim coordinates; одного mTLS для execution недостаточно.
func verifySSHExecutionGateway(ctx context.Context, method, operation string) error {
	if admission, ok := serviceidentity.FromContext(ctx); ok {
		if admission.RPCProfile != transportprofile.TrustedCluster || admission.Peer.SPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway" || admission.TargetSPIFFEID != secretBrokerSPIFFEID || admission.FullMethod != method || admission.OperationID != operation || admission.Permission != operation || admission.ProjectRequired {
			return status.Error(codes.PermissionDenied, sshExecutionRejected)
		}
		return nil
	}
	_, _, err := verifiedProjectionAuthority(ctx, "integration-gateway", "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", method, operation)
	return err
}

// sshExecutionClaim decodes only a complete original invocation binding and rejects unknown nested wire fields, including health-claim substitution.
// sshExecutionClaim разбирает лишь complete original invocation binding и отвергает unknown nested wire fields, включая health-claim substitution.
func sshExecutionClaim(input *sb.ManagedSSHExecutionBinding) (managedssh.Binding, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedssh.Binding{}, false
	}
	binding := managedssh.Binding{WorkloadInstance: input.WorkloadInstance, InvocationRef: input.InvocationRef, LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, InputDigest: input.InputDigest}
	return binding, binding.Validate() == nil && binding.InvocationRef != ""
}

// sshExecutionResult projects only the same claim and a closed owner receipt; private output requires an applied recorded observation, while reconciliation stays metadata-only.
// sshExecutionResult выдаёт лишь тот же claim и closed owner receipt; private output требует applied recorded observation, reconciliation остаётся metadata-only.
func sshExecutionResult(binding managedssh.Binding, receipt managedssh.ExecutionReceipt, metadata bool) (*sb.ManagedSSHExecutionResult, error) {
	if receipt.Validate(time.Now(), !metadata) != nil || metadata && len(receipt.Observation.Result) != 0 {
		return nil, status.Error(codes.Unavailable, sshExecutionRejected)
	}
	return &sb.ManagedSSHExecutionResult{Binding: &sb.ManagedSSHExecutionBinding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}, AttemptRef: receipt.AttemptRef, SnapshotDigest: receipt.SnapshotDigest, State: receipt.State, Recorded: receipt.Recorded, Applied: receipt.Applied, OwnerState: receipt.OwnerState,
		Outcome: string(receipt.Observation.Outcome), ResultDigest: receipt.Observation.ResultDigest, ResultJson: receipt.Observation.Result}, nil
}

// ExecuteSSHIdentity accepts only the registered protected original claim and never exposes a generic shell, key or caller-selected target endpoint.
// ExecuteSSHIdentity принимает лишь registered protected original claim без generic shell, key либо caller-selected target endpoint.
func (server *Server) ExecuteSSHIdentity(ctx context.Context, request *sb.ExecuteSSHIdentityRequest) (*sb.ExecuteSSHIdentityResponse, error) {
	if err := verifySSHExecutionGateway(ctx, sb.ManagedSSHService_ExecuteSSHIdentity_FullMethodName, "ssh.execute"); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshExecutionRejected)
	}
	binding, valid := sshExecutionClaim(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshExecutionRejected)
	}
	if server.sshExecution == nil {
		return nil, status.Error(codes.Unavailable, sshExecutionRejected)
	}
	receipt, err := server.sshExecution.Execute(ctx, binding)
	if err != nil {
		return nil, status.Error(codes.Unavailable, sshExecutionRejected)
	}
	result, err := sshExecutionResult(binding, receipt, false)
	if err != nil {
		return nil, err
	}
	return &sb.ExecuteSSHIdentityResponse{Result: result}, nil
}

// ReconcileSSHIdentity reads original metadata after uncertain execution delivery and cannot call Begin, read a private key or send another SSH command.
// ReconcileSSHIdentity читает original metadata после uncertain execution delivery и не вызывает Begin, key read либо новый SSH command.
func (server *Server) ReconcileSSHIdentity(ctx context.Context, request *sb.ReconcileSSHIdentityRequest) (*sb.ReconcileSSHIdentityResponse, error) {
	if err := verifySSHExecutionGateway(ctx, sb.ManagedSSHService_ReconcileSSHIdentity_FullMethodName, "ssh.reconcile"); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshExecutionRejected)
	}
	binding, valid := sshExecutionClaim(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshExecutionRejected)
	}
	if server.sshExecution == nil {
		return nil, status.Error(codes.Unavailable, sshExecutionRejected)
	}
	receipt, err := server.sshExecution.Get(ctx, binding)
	if err != nil {
		return nil, status.Error(codes.Unavailable, sshExecutionRejected)
	}
	result, err := sshExecutionResult(binding, receipt, true)
	if err != nil {
		return nil, err
	}
	return &sb.ReconcileSSHIdentityResponse{Result: result}, nil
}
