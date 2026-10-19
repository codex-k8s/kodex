package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func castProjectPurgeRecovery(item entity.ProjectPurgeRecovery) *cp.ProjectPurgeRecovery {
	return &cp.ProjectPurgeRecovery{ProjectRef: item.ProjectRef, Version: item.Version, SnapshotDigest: item.SnapshotDigest,
		State: item.State, UnknownContentCount: item.UnknownContentCount, CleanupPendingCount: item.CleanupPendingCount,
		ProjectPresent: item.ProjectPresent, CanResolve: item.CanResolve, ResolutionRef: item.ResolutionRef, ProjectVersion: item.ProjectVersion}
}

func (server *Server) GetProjectPurgeRecovery(ctx context.Context, request *cp.GetProjectPurgeRecoveryRequest) (*cp.GetProjectPurgeRecoveryResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetProjectPurgeRecovery_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetProjectPurgeRecovery(ctx, p, request.GetProjectRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetProjectPurgeRecoveryResponse{Recovery: castProjectPurgeRecovery(item)}, nil
}

func (server *Server) ResolveProjectPurgeRecovery(ctx context.Context, request *cp.ResolveProjectPurgeRecoveryRequest) (*cp.ResolveProjectPurgeRecoveryResponse, error) {
	result, err := execute(ctx, server.service, cp.PlatformCommandService_ResolveProjectPurgeRecovery_FullMethodName,
		command.ResolveProjectPurgeRecovery, request.GetMutation(), command.ProjectPurgeRecoveryInput{
			Ref: request.GetProjectRef(), SnapshotDigest: request.GetSnapshotDigest(), AcknowledgeUnknownOutcome: request.GetAcknowledgeUnknownOutcome()})
	if err != nil {
		return nil, err
	}
	if result.ProjectPurgeRecovery == nil {
		return nil, transportError(errs.ErrConflict)
	}
	return &cp.ResolveProjectPurgeRecoveryResponse{Recovery: castProjectPurgeRecovery(*result.ProjectPurgeRecovery)}, nil
}
