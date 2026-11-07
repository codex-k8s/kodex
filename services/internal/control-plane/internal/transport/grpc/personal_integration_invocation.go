package grpc

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BeginPersonalIntegrationInvocation returns descriptor metadata only for a fresh committed admission.
// Repeated, revoked or stale leases return an empty denial, never an ambiguous partial permit.
// BeginPersonalIntegrationInvocation выдаёт descriptor metadata лишь для свежего committed допуска.
// Повторные, отозванные и устаревшие leases возвращают пустой отказ, не partial permit.
func (server *Server) BeginPersonalIntegrationInvocation(ctx context.Context, request *cp.BeginPersonalIntegrationInvocationRequest) (*cp.BeginPersonalIntegrationInvocationResponse, error) {
	principal, err := principal(ctx, cp.PersonalIntegrationCredentialService_BeginPersonalIntegrationInvocation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.BeginPersonalIntegrationInvocation(ctx, principal, command.PersonalIntegrationInvocationAdmission{
		InvocationRef: request.GetInvocationRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), WorkloadInstance: request.GetWorkloadInstance(),
	})
	if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrConflict) {
		return &cp.BeginPersonalIntegrationInvocationResponse{}, nil
	}
	if err != nil {
		return nil, transportError(err)
	}
	if result.Credential == nil || result.Credential.Ref == "" || result.Credential.Revision < 1 || !result.Credential.ValidDescriptor() || result.Credential.CreatedAt.IsZero() {
		return nil, status.Error(codes.Unavailable, "personal invocation credential result is unavailable")
	}
	mode := cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE
	if result.RecoveryReadOnly {
		mode = cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
	}
	profile := castIntegrationResourcePermissionProfile(result.ResourcePermissionProfile)
	if profile < 0 {
		return nil, status.Error(codes.Unavailable, "personal invocation resource permission profile is unavailable")
	}
	return &cp.BeginPersonalIntegrationInvocationResponse{Admitted: true, CredentialRevision: castIntegrationCredential(*result.Credential), WorkMode: mode, ResourcePermissionProfile: profile}, nil
}
