package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castProjectPurgeRecovery projects project presence, snapshot digest, recovery counts, resolution
// availability and owner version pins without performing cleanup.
// castProjectPurgeRecovery преобразует наличие проекта, digest снимка, счётчики восстановления,
// доступность решения и версии владельца без выполнения очистки.
func castProjectPurgeRecovery(item entity.ProjectPurgeRecovery) *cp.ProjectPurgeRecovery {
	return &cp.ProjectPurgeRecovery{ProjectRef: item.ProjectRef, Version: item.Version, SnapshotDigest: item.SnapshotDigest,
		State: item.State, UnknownContentCount: item.UnknownContentCount, CleanupPendingCount: item.CleanupPendingCount,
		ProjectPresent: item.ProjectPresent, CanResolve: item.CanResolve, ResolutionRef: item.ResolutionRef, ProjectVersion: item.ProjectVersion}
}

// GetProjectPurgeRecovery reads purge recovery under the verified query principal and projects the
// owner snapshot.
// GetProjectPurgeRecovery читает восстановление окончательного удаления с проверенным query
// principal и преобразует снимок владельца.
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

// ResolveProjectPurgeRecovery passes snapshot digest and explicit unknown-outcome acknowledgment
// through the dedicated resolution command, requiring a recovery result.
// ResolveProjectPurgeRecovery передаёт digest снимка и явное признание неизвестного исхода
// отдельной команде решения, требуя результат восстановления.
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
