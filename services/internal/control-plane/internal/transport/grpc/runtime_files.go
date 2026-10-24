package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// executionFilesContext copies lease and catalog pins and strips the file-purpose enum prefix
// without assigning authority from request fields.
// executionFilesContext копирует привязки lease и каталога и удаляет префикс enum цели файла без
// назначения полномочий полями запроса.
func executionFilesContext(value *cp.ExecutionFileContext) query.ExecutionFileContext {
	return query.ExecutionFileContext{LeaseRef: value.GetLeaseRef(), Fence: value.GetFence(), Generation: value.GetGeneration(),
		CatalogRef: value.GetCatalogRef(), CatalogDigest: value.GetCatalogDigest(), Purpose: strings.TrimPrefix(value.GetPurpose().String(), "RUNTIME_FILE_PURPOSE_")}
}

// executionFilesRef copies entry, artifact, revision and digest locators for subsequent validation
// by the domain owner.
// executionFilesRef копирует ссылки элемента и артефакта, ревизию и digest для последующей
// проверки доменным владельцем.
func executionFilesRef(value *cp.ExecutionFileRef) query.ExecutionFileRef {
	return query.ExecutionFileRef{EntryRef: value.GetEntryRef(), ArtifactRef: value.GetArtifactRef(), Revision: value.GetRevision(), Digest: value.GetDigest()}
}

// executionFileDescriptor projects owner file metadata, purpose and source/project/run pins
// without fetching file contents.
// executionFileDescriptor преобразует метаданные файла владельца, цель и привязки источника,
// проекта и запуска без получения содержимого.
func executionFileDescriptor(value entity.ExecutionFileDescriptor) *cp.ExecutionFileDescriptor {
	return &cp.ExecutionFileDescriptor{EntryRef: value.EntryRef, ArtifactRef: value.ArtifactRef, Revision: value.Revision, Version: value.Version,
		Digest: value.Digest, Name: value.Name, MediaType: value.MediaType, SizeBytes: value.SizeBytes,
		Purpose:    cp.RuntimeFilePurpose(cp.RuntimeFilePurpose_value["RUNTIME_FILE_PURPOSE_"+value.Purpose]),
		ProjectRef: value.ProjectRef, RunRef: value.RunRef, Source: value.Source, SourceRef: value.SourceRef, SourceRevisionRef: value.SourceRevisionRef}
}

// SearchExecutionFiles passes verified principal, execution context, query and page to the domain
// and returns catalog, file descriptors and total.
// SearchExecutionFiles передаёт проверенный principal, контекст исполнения, запрос и страницу
// домену и возвращает каталог, описатели файлов и общее число.
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

// GetExecutionFileManifest reads a paginated execution manifest under the verified principal and
// preserves its catalog and continuation pins.
// GetExecutionFileManifest читает страницу манифеста исполнения с проверенным principal и
// сохраняет привязки каталога и продолжения.
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

// GetExecutionFileMetadata reads one exact file reference within the supplied execution context
// and projects the authorized catalog and descriptor.
// GetExecutionFileMetadata читает точную ссылку файла в переданном контексте исполнения и
// преобразует авторизованные каталог и описатель.
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

// PreviewExecutionFile passes the requested byte bound to the authorized preview and returns text,
// truncation and preview digest with metadata.
// PreviewExecutionFile передаёт запрошенный предел байтов авторизованному просмотру и возвращает
// текст, усечение и digest просмотра с метаданными.
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
