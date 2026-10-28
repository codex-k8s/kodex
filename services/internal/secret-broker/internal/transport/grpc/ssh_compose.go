package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

// sshComposeProjectionOwner resolves only fenced SSH descriptors; it cannot read arbitrary secrets.
// sshComposeProjectionOwner разрешает только fenced SSH descriptors, не arbitrary secrets.
type sshComposeProjectionOwner interface {
	// ResolveSSHComposeCredentialProjection obtains fresh owner eligibility for exact worker binding.
	// ResolveSSHComposeCredentialProjection получает fresh owner eligibility exact worker binding.
	ResolveSSHComposeCredentialProjection(context.Context, *cp.ResolveSSHComposeCredentialProjectionRequest) (*cp.ResolveSSHComposeCredentialProjectionResponse, error)
}

// ProjectSSHComposeCredential releases one key copy only to the verified integration worker's live lease.
// ProjectSSHComposeCredential выдаёт одну key copy только live lease проверенного integration worker.
func (server *Server) ProjectSSHComposeCredential(ctx context.Context, request *sb.ProjectSSHComposeCredentialRequest) (*sb.ProjectSSHComposeCredentialResponse, error) {
	method := sb.RuntimeCredentialProjectionService_ProjectSSHComposeCredential_FullMethodName
	const operation = "platform.ssh-compose.credential.project"
	if admission, ok := serviceidentity.FromContext(ctx); ok {
		if admission.RPCProfile != transportprofile.TrustedCluster || admission.Peer.SPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway" || admission.TargetSPIFFEID != secretBrokerSPIFFEID || admission.FullMethod != method || admission.OperationID != operation || admission.Permission != operation || admission.ProjectRequired {
			return nil, status.Error(codes.PermissionDenied, "SSH credential projection rejected")
		}
	} else if _, _, err := verifiedProjectionAuthority(ctx, "integration-gateway", "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", method, operation); err != nil {
		return nil, err
	}
	owner, ok := server.owner.(sshComposeProjectionOwner)
	if !ok {
		return nil, status.Error(codes.Unavailable, "SSH credential owner unavailable")
	}
	resolved, err := owner.ResolveSSHComposeCredentialProjection(ctx, &cp.ResolveSSHComposeCredentialProjectionRequest{Binding: &cp.SSHComposeLeaseBinding{
		WorkloadInstance: request.GetWorkloadInstance(), InvocationRef: request.GetInvocationRef(), ConnectionTestRef: request.GetConnectionTestRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
	}})
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	descriptor := resolved.GetSecretRevision()
	now := time.Now()
	if descriptor == nil || descriptor.GetRevision() < 1 || resolved.GetTargetRef() == "" || len(resolved.GetPermit()) == 0 || len(resolved.GetPermit()) > 16384 || resolved.GetExpiresAt() == nil || resolved.GetExpiresAt().CheckValid() != nil ||
		!resolved.GetExpiresAt().AsTime().After(now) || resolved.GetExpiresAt().AsTime().After(now.Add(35*time.Second)) {
		return nil, status.Error(codes.FailedPrecondition, "SSH credential descriptor rejected")
	}
	_, raw, err := server.store.ReadExactValue(ctx, kubernetesstore.ExactDescriptor{Namespace: descriptor.GetNamespace(), Name: descriptor.GetSecretName(), Key: descriptor.GetSecretKey(),
		SecretRef: resolved.GetSecretRef(), Revision: descriptor.GetRevision(), UID: descriptor.GetSecretUid(), ResourceVersion: descriptor.GetSecretResourceVersion(), ContentSHA256: descriptor.GetContentSha256()})
	if err != nil {
		return nil, projectionStorageError(err)
	}
	defer clear(raw)
	if len(raw) < 1 || len(raw) > 16384 || !resolved.GetExpiresAt().AsTime().After(time.Now()) {
		return nil, status.Error(codes.FailedPrecondition, "SSH credential materialization rejected")
	}
	return &sb.ProjectSSHComposeCredentialResponse{PrivateKey: append([]byte(nil), raw...), ExpiresAt: resolved.GetExpiresAt(), TargetRef: resolved.GetTargetRef(), Permit: resolved.GetPermit()}, nil
}
