package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ListVFSNodes(ctx context.Context, request *controlplanev1.ListVFSNodesRequest) (*controlplanev1.ListVFSNodesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListVFSNodes_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.ListVFSNodes(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), ResourceRef: request.GetPath(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListVFSNodesResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Nodes = append(response.Nodes, castVFSNode(item))
	}
	return response, nil
}
func (server *Server) SearchVFS(ctx context.Context, request *controlplanev1.SearchVFSRequest) (*controlplanev1.SearchVFSResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_SearchVFS_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.SearchVFS(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.SearchVFSResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Nodes = append(response.Nodes, castVFSNode(item))
	}
	return response, nil
}
func castVFSNode(value entity.VFSNode) *controlplanev1.VFSNode {
	return &controlplanev1.VFSNode{Ref: value.Ref, Path: value.Path, ParentPath: value.ParentPath, Name: value.Name,
		Kind: controlplanev1.VFSNodeKind(controlplanev1.VFSNodeKind_value["VFS_NODE_KIND_"+value.Kind]), Directory: value.Directory,
		ProjectRef: value.ProjectRef, EntityRef: value.EntityRef, RunRef: value.RunRef, SizeBytes: value.SizeBytes,
		Digest: value.Digest, ModifiedAt: timestamp(value.ModifiedAt)}
}
