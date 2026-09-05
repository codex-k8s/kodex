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
	items, total, next, err := server.service.ListVFSNodes(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), ResourceRef: request.GetPath(), Query: request.GetQuery(), State: request.GetLifecycleState(), VFSKinds: vfsKinds(request.GetKinds()), Page: page(request.GetPage())})
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
	items, total, next, err := server.service.SearchVFS(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), ResourceRef: request.GetPath(), Query: request.GetQuery(), State: request.GetLifecycleState(), VFSKinds: vfsKinds(request.GetKinds()), Page: page(request.GetPage())})
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
		Digest: value.Digest, ModifiedAt: timestamp(value.ModifiedAt), Version: value.Version, RevisionRef: value.RevisionRef, Revision: value.Revision,
		LifecycleState: value.LifecycleState, ScanState: value.ScanState, ResourceKind: value.ResourceKind,
		Selectable: value.Selectable, SelectionReason: value.SelectionReason, NextActions: value.NextActions}
}

func vfsKinds(input []controlplanev1.VFSNodeKind) []string {
	result := make([]string, 0, len(input))
	for _, kind := range input {
		name := kind.String()
		const prefix = "VFS_NODE_KIND_"
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			name = name[len(prefix):]
		}
		result = append(result, name)
	}
	return result
}
