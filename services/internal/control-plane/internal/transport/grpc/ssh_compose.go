package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

// castSSHComposeBinding maps locators only; authority remains in the verified transport context.
// castSSHComposeBinding переносит только locators; authority остаётся в verified transport context.
func castSSHComposeBinding(input *cp.SSHComposeLeaseBinding) platformrepo.SSHComposeLeaseBinding {
	return platformrepo.SSHComposeLeaseBinding{WorkloadInstance: input.GetWorkloadInstance(), InvocationRef: input.GetInvocationRef(), ConnectionTestRef: input.GetConnectionTestRef(),
		LeaseRef: input.GetLeaseRef(), Fence: input.GetFence(), Generation: input.GetGeneration()}
}

// ClaimSSHComposeInvocations exposes the independently bounded protected worker lane.
// ClaimSSHComposeInvocations предоставляет отдельно ограниченную защищённую worker очередь.
func (server *Server) ClaimSSHComposeInvocations(ctx context.Context, request *cp.ClaimSSHComposeInvocationsRequest) (*cp.ClaimSSHComposeInvocationsResponse, error) {
	principal, err := principal(ctx, cp.RuntimeWorkService_ClaimSSHComposeInvocations_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimSSHComposeInvocations(ctx, principal, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ClaimSSHComposeInvocationsResponse{}
	for _, item := range items {
		response.Claims = append(response.Claims, CastIntegrationInvocationClaim(item))
	}
	return response, nil
}

// RenewSSHComposeInvocation returns only a committed renewal and its owner-signed permit.
// RenewSSHComposeInvocation возвращает лишь committed renewal и owner-signed permit.
func (server *Server) RenewSSHComposeInvocation(ctx context.Context, request *cp.RenewSSHComposeInvocationRequest) (*cp.RenewSSHComposeInvocationResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_RenewSSHComposeInvocation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.RenewSSHComposeInvocation(ctx, p, castSSHComposeBinding(request.GetBinding()))
	if err != nil {
		return nil, transportError(err)
	}
	permit, _ := result["permit"].([]byte)
	return &cp.RenewSSHComposeInvocationResponse{Lease: castLease(result), Permit: permit}, nil
}

// ResolveSSHComposeCredentialProjection returns metadata only after owner rechecks the complete lease graph.
// ResolveSSHComposeCredentialProjection возвращает metadata лишь после owner проверки полного lease graph.
func (server *Server) ResolveSSHComposeCredentialProjection(ctx context.Context, request *cp.ResolveSSHComposeCredentialProjectionRequest) (*cp.ResolveSSHComposeCredentialProjectionResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretWorkService_ResolveSSHComposeCredentialProjection_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ResolveSSHComposeCredentialProjection(ctx, p, castSSHComposeBinding(request.GetBinding()))
	if err != nil {
		return nil, transportError(err)
	}
	descriptor, ok := result["descriptor"].(entity.RuntimeSecretRevisionDescriptor)
	expires, valid := result["expiresAt"].(time.Time)
	if !ok || !valid {
		return nil, transportError(errs.ErrUnavailable)
	}
	permit, _ := result["permit"].([]byte)
	return &cp.ResolveSSHComposeCredentialProjectionResponse{SecretRevision: castRuntimeSecretRevisionDescriptor(descriptor), ExpiresAt: timestamppb.New(expires), TargetRef: mapString(result, "targetRef"), SecretRef: mapString(result, "secretRef"), Permit: permit}, nil
}
