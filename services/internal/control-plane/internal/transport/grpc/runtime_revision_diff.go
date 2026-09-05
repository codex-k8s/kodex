package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func (server *Server) GetRuntimeRevisionDiff(ctx context.Context, request *controlplanev1.GetRuntimeRevisionDiffRequest) (*controlplanev1.GetRuntimeRevisionDiffResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeRevisionDiff_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRuntimeRevisionDiff(ctx, p, request.GetRunRef(), request.GetCurrentRevisionRef())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetRuntimeRevisionDiffResponse{Current: castPublicRuntimeRevisionIdentity(&result.Current), Previous: castPublicRuntimeRevisionIdentity(result.Previous)}
	for _, change := range result.Changes {
		response.Changes = append(response.Changes, &controlplanev1.RuntimeRevisionDiffChange{
			Component: controlplanev1.RuntimeRevisionDiffComponent(controlplanev1.RuntimeRevisionDiffComponent_value["RUNTIME_REVISION_DIFF_COMPONENT_"+change.Component]),
			Previous:  castRuntimeRevisionDiffValue(change.Previous), Current: castRuntimeRevisionDiffValue(change.Current),
		})
	}
	return response, nil
}

func castPublicRuntimeRevisionIdentity(item *entity.PublicRuntimeRevisionIdentity) *controlplanev1.PublicRuntimeRevisionIdentity {
	if item == nil {
		return nil
	}
	return &controlplanev1.PublicRuntimeRevisionIdentity{Ref: item.Ref, Version: item.Version, RunRef: item.RunRef, SessionRef: item.SessionRef,
		TurnRef: item.TurnRef, Attempt: item.Attempt, RevisionDigest: item.RevisionDigest, CreatedAt: timestamp(item.CreatedAt)}
}

func castRuntimeRevisionDiffValue(item *entity.RuntimeRevisionDiffValue) *controlplanev1.RuntimeRevisionDiffValue {
	if item == nil {
		return nil
	}
	return &controlplanev1.RuntimeRevisionDiffValue{Ref: item.Ref, Version: item.Version, Digest: item.Digest, Revision: item.Revision}
}
