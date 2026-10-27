package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetRuntimeRevisionDiff reads the authorized revision comparison and preserves optional previous
// identity and component values in the protobuf diff.
// GetRuntimeRevisionDiff читает авторизованное сравнение ревизий и сохраняет необязательные
// прежнюю identity и значения компонентов в Protobuf diff.
func (server *Server) GetRuntimeRevisionDiff(ctx context.Context, request *controlplanev1.GetRuntimeRevisionDiffRequest) (*controlplanev1.GetRuntimeRevisionDiffResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetRuntimeRevisionDiff_FullMethodName, func(p value.Principal) (*controlplanev1.GetRuntimeRevisionDiffResponse, error) {
		result, err := server.service.GetRuntimeRevisionDiff(ctx, p, request.GetRunRef(), request.GetCurrentRevisionRef())
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.GetRuntimeRevisionDiffResponse{Current: castPublicRuntimeRevisionIdentity(&result.Current), Previous: castPublicRuntimeRevisionIdentity(result.Previous)}
		for _, change := range result.Changes {
			response.Changes = append(response.Changes, &controlplanev1.RuntimeRevisionDiffChange{
				Component: controlplanev1.RuntimeRevisionDiffComponent(controlplanev1.RuntimeRevisionDiffComponent_value["RUNTIME_REVISION_DIFF_COMPONENT_"+change.Component]),
				Previous:  castRuntimeRevisionDiffValue(change.Previous), Current: castRuntimeRevisionDiffValue(change.Current),
			})
		}
		return response, nil

	})
}

// castPublicRuntimeRevisionIdentity preserves absent identity and projects
// run/session/turn/attempt, revision digest and creation time from the public read model.
// castPublicRuntimeRevisionIdentity сохраняет отсутствие identity и преобразует запуск, сессию,
// ход, попытку, digest ревизии и время из публичной модели чтения.
func castPublicRuntimeRevisionIdentity(item *entity.PublicRuntimeRevisionIdentity) *controlplanev1.PublicRuntimeRevisionIdentity {
	if item == nil {
		return nil
	}
	return &controlplanev1.PublicRuntimeRevisionIdentity{Ref: item.Ref, Version: item.Version, RunRef: item.RunRef, SessionRef: item.SessionRef,
		TurnRef: item.TurnRef, Attempt: item.Attempt, RevisionDigest: item.RevisionDigest, CreatedAt: timestamp(item.CreatedAt)}
}

// castRuntimeRevisionDiffValue preserves an absent comparison value and otherwise copies its
// reference, version, digest and revision.
// castRuntimeRevisionDiffValue сохраняет отсутствие значения сравнения, иначе копирует ссылку,
// версию, digest и ревизию.
func castRuntimeRevisionDiffValue(item *entity.RuntimeRevisionDiffValue) *controlplanev1.RuntimeRevisionDiffValue {
	if item == nil {
		return nil
	}
	return &controlplanev1.RuntimeRevisionDiffValue{Ref: item.Ref, Version: item.Version, Digest: item.Digest, Revision: item.Revision}
}
