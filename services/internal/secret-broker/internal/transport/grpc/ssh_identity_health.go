package grpc

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sshIdentityHealthProtocol is the single synchronous broker health port; it neither exposes a key nor accepts a target/command from the gateway.
// sshIdentityHealthProtocol является единственным synchronous broker health port; key не выдаётся, target/command от gateway не принимаются.
type sshIdentityHealthProtocol interface {
	// Probe owns admission, private read, joined SSH probe and bounded original owner completion.
	// Probe владеет admission, private read, joined SSH probe и bounded original owner completion.
	Probe(context.Context, managedssh.Binding) (managedssh.HealthReceipt, error)
}

// WithSSHIdentityHealth wires the optional purpose service during trusted composition; absence remains closed at the RPC boundary.
// WithSSHIdentityHealth подключает optional purpose service при trusted composition; отсутствие сохраняет closed RPC boundary.
func WithSSHIdentityHealth(service sshIdentityHealthProtocol) Option {
	return func(server *Server) { server.sshHealth = service }
}

// ProbeSSHIdentity verifies exact transport/method authority before interpreting the original metadata-only test claim; raw diagnostics never leave this boundary.
// ProbeSSHIdentity проверяет exact transport/method authority до original metadata-only test claim; raw diagnostics не покидают boundary.
func (server *Server) ProbeSSHIdentity(ctx context.Context, request *sb.ProbeSSHIdentityRequest) (*sb.ProbeSSHIdentityResponse, error) {
	method := sb.ManagedSSHService_ProbeSSHIdentity_FullMethodName
	const operation = "ssh.health.probe"
	const rejected = "SSH identity health rejected"
	if admission, ok := serviceidentity.FromContext(ctx); ok {
		if admission.RPCProfile != transportprofile.TrustedCluster || admission.Peer.SPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway" || admission.TargetSPIFFEID != secretBrokerSPIFFEID || admission.FullMethod != method || admission.OperationID != operation || admission.Permission != operation || admission.ProjectRequired {
			return nil, status.Error(codes.PermissionDenied, rejected)
		}
	} else if _, _, err := verifiedProjectionAuthority(ctx, "integration-gateway", "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", method, operation); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || request.Binding == nil || len(request.Binding.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, rejected)
	}
	b := request.Binding
	binding := managedssh.Binding{WorkloadInstance: b.WorkloadInstance, ConnectionTestRef: b.ConnectionTestRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest}
	if binding.Validate() != nil {
		return nil, status.Error(codes.InvalidArgument, rejected)
	}
	if server.sshHealth == nil {
		return nil, status.Error(codes.Unavailable, rejected)
	}
	receipt, err := server.sshHealth.Probe(ctx, binding)
	if err != nil {
		return nil, status.Error(codes.Unavailable, rejected)
	}
	if receipt.TestRef != binding.ConnectionTestRef || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED" && receipt.State != "CANCELLED") || receipt.Applied == (receipt.State == "CANCELLED") {
		return nil, status.Error(codes.Unavailable, rejected)
	}
	return &sb.ProbeSSHIdentityResponse{TestRef: receipt.TestRef, State: receipt.State, Applied: receipt.Applied}, nil
}
