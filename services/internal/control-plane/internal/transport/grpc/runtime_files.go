package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func executionFilesContext(value *cp.ExecutionFileContext) query.ExecutionFileContext {
	return query.ExecutionFileContext{LeaseRef: value.GetLeaseRef(), Fence: value.GetFence(), Generation: value.GetGeneration(),
		CatalogRef: value.GetCatalogRef(), CatalogDigest: value.GetCatalogDigest(), Purpose: strings.TrimPrefix(value.GetPurpose().String(), "RUNTIME_FILE_PURPOSE_")}
}

func executionFilesRef(value *cp.ExecutionFileRef) query.ExecutionFileRef {
	return query.ExecutionFileRef{EntryRef: value.GetEntryRef(), ArtifactRef: value.GetArtifactRef(), Revision: value.GetRevision(), Digest: value.GetDigest()}
}

func executionFileDescriptor(value entity.ExecutionFileDescriptor) *cp.ExecutionFileDescriptor {
	return &cp.ExecutionFileDescriptor{EntryRef: value.EntryRef, ArtifactRef: value.ArtifactRef, Revision: value.Revision, Version: value.Version,
		Digest: value.Digest, Name: value.Name, MediaType: value.MediaType, SizeBytes: value.SizeBytes,
		Purpose:    cp.RuntimeFilePurpose(cp.RuntimeFilePurpose_value["RUNTIME_FILE_PURPOSE_"+value.Purpose]),
		ProjectRef: value.ProjectRef, RunRef: value.RunRef, Source: value.Source, SourceRef: value.SourceRef, SourceRevisionRef: value.SourceRevisionRef}
}

func (server *Server) SearchExecutionFiles(ctx context.Context, request *cp.SearchExecutionFilesRequest) (*cp.SearchExecutionFilesResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_SearchExecutionFiles_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.SearchExecutionFiles(ctx, p, executionFilesContext(request.GetContext()), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.SearchExecutionFilesResponse{Catalog: runtimeFileCatalogProto(result.Catalog), Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.Next}}
	for _, file := range result.Items {
		response.Items = append(response.Items, executionFileDescriptor(file))
	}
	return response, nil
}

func (server *Server) GetExecutionFileManifest(ctx context.Context, request *cp.GetExecutionFileManifestRequest) (*cp.GetExecutionFileManifestResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_GetExecutionFileManifest_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetExecutionFileManifest(ctx, p, executionFilesContext(request.GetContext()), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetExecutionFileManifestResponse{Catalog: runtimeFileCatalogProto(result.Catalog), Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.Next}}
	for _, file := range result.Items {
		response.Items = append(response.Items, executionFileDescriptor(file))
	}
	return response, nil
}

func (server *Server) GetExecutionFileMetadata(ctx context.Context, request *cp.GetExecutionFileMetadataRequest) (*cp.GetExecutionFileMetadataResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_GetExecutionFileMetadata_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetExecutionFileMetadata(ctx, p, executionFilesContext(request.GetContext()), executionFilesRef(request.GetFile()))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetExecutionFileMetadataResponse{Catalog: runtimeFileCatalogProto(result.Catalog), File: executionFileDescriptor(result.File)}, nil
}

func (server *Server) PreviewExecutionFile(ctx context.Context, request *cp.PreviewExecutionFileRequest) (*cp.PreviewExecutionFileResponse, error) {
	p, err := principal(ctx, cp.RuntimeWorkService_PreviewExecutionFile_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PreviewExecutionFile(ctx, p, executionFilesContext(request.GetContext()), executionFilesRef(request.GetFile()), request.GetMaximumBytes())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.PreviewExecutionFileResponse{Catalog: runtimeFileCatalogProto(result.Metadata.Catalog), File: executionFileDescriptor(result.Metadata.File),
		Text: result.Text, Truncated: result.Truncated, PreviewDigest: result.Digest}, nil
}
