package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ListConfigOverlayRevisions(ctx context.Context, request *controlplanev1.ListConfigOverlayRevisionsRequest) (*controlplanev1.ListConfigOverlayRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListConfigOverlayRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.ListConfigOverlayRevisions(ctx, p, query.Filter{ResourceRef: request.GetAgentRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListConfigOverlayRevisionsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, castConfigOverlay(&item))
	}
	return response, nil
}
func (server *Server) GetConfigOverlayRevision(ctx context.Context, request *controlplanev1.GetConfigOverlayRevisionRequest) (*controlplanev1.GetConfigOverlayRevisionResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetConfigOverlayRevision_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetConfigOverlayRevision(ctx, p, request.GetAgentRef(), request.GetRevisionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetConfigOverlayRevisionResponse{Revision: castConfigOverlay(&item)}, nil
}
