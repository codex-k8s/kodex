package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
)

func (server *Server) ListMemoryRecords(ctx context.Context, request *controlplanev1.ListMemoryRecordsRequest) (*controlplanev1.ListMemoryRecordsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListMemoryRecords_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_UNSPECIFIED {
		name, ok := controlplanev1.ContextResourceState_name[int32(request.GetState())]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid memory state")
		}
		state = strings.TrimPrefix(name, "CONTEXT_RESOURCE_STATE_")
	}
	items, total, next, err := server.service.ListMemoryRecords(ctx, p, query.Filter{
		ProjectRef: request.GetProjectRef(), ResourceRef: request.GetAgentRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListMemoryRecordsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		record, err := castMemoryRecord(&item)
		if err != nil {
			return nil, err
		}
		response.Records = append(response.Records, record)
	}
	return response, nil
}

func (server *Server) ListMemoryRecordRevisions(ctx context.Context, request *controlplanev1.ListMemoryRecordRevisionsRequest) (*controlplanev1.ListMemoryRecordRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListMemoryRecordRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.ListMemoryRecordRevisions(ctx, p, request.GetRecordRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListMemoryRecordRevisionsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, castMemoryRevision(&item))
	}
	return response, nil
}

func domainMemorySpecification(input *controlplanev1.MemoryRecordSpecification) (entity.MemoryRecordSpecification, error) {
	if input == nil || input.GetRetentionUntil() == nil || input.GetRetentionUntil().CheckValid() != nil {
		return entity.MemoryRecordSpecification{}, errs.ErrInvalid
	}
	return entity.MemoryRecordSpecification{Title: input.GetTitle(), Summary: input.GetSummary(), SourceRunRef: input.GetSourceRunRef(), RetentionUntil: input.GetRetentionUntil().AsTime().UTC()}, nil
}
func castContextProvenance(input entity.ContextProvenance) *controlplanev1.ContextProvenance {
	return &controlplanev1.ContextProvenance{ActorRef: input.ActorRef, SourceKind: input.SourceKind, SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, Digest: input.Digest, CreatedAt: timestamppb.New(input.CreatedAt)}
}
func castMemoryRevision(input *entity.MemoryRecordRevision) *controlplanev1.MemoryRecordRevision {
	if input == nil {
		return nil
	}
	return &controlplanev1.MemoryRecordRevision{Ref: input.Ref, Revision: input.Revision, Title: input.Title, Summary: input.Summary, Digest: input.Digest,
		ParentRevisionRef: input.ParentRevisionRef, Provenance: castContextProvenance(input.Provenance), RetentionUntil: timestamppb.New(input.RetentionUntil), Redacted: input.Redacted}
}
func castMemoryRecord(input *entity.KodexMemoryRecord) (*controlplanev1.KodexMemoryRecord, error) {
	if input == nil || input.Ref == "" || input.Version < 1 || input.CurrentRevision == nil {
		return nil, status.Error(codes.Internal, "memory record result is incomplete")
	}
	state, ok := controlplanev1.ContextResourceState_value["CONTEXT_RESOURCE_STATE_"+input.State]
	if !ok || state == 0 {
		return nil, status.Error(codes.Internal, "memory record state is invalid")
	}
	return &controlplanev1.KodexMemoryRecord{Ref: input.Ref, Version: input.Version, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
		State: controlplanev1.ContextResourceState(state), CurrentRevision: castMemoryRevision(input.CurrentRevision),
		CreatedAt: timestamppb.New(input.CreatedAt), UpdatedAt: timestamppb.New(input.UpdatedAt)}, nil
}
func (server *Server) GetMemoryRecord(ctx context.Context, request *controlplanev1.GetMemoryRecordRequest) (*controlplanev1.GetMemoryRecordResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetMemoryRecord_FullMethodName)
	if err != nil {
		return nil, err
	}
	record, err := server.service.GetMemoryRecord(ctx, p, request.GetRecordRef())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := castMemoryRecord(&record)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.GetMemoryRecordResponse{Record: result}, nil
}

func (server *Server) CreateMemoryRecord(ctx context.Context, request *controlplanev1.CreateMemoryRecordRequest) (*controlplanev1.CreateMemoryRecordResponse, error) {
	specification, err := domainMemorySpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(err)
	}
	payload := command.MemoryRecordInput{ProjectRef: request.GetProjectRef(), AgentRef: request.GetAgentRef(), Specification: specification}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateMemoryRecord_FullMethodName, command.CreateMemoryRecord, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	record, err := castMemoryRecord(result.MemoryRecord)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateMemoryRecordResponse{Record: record}, nil
}

func (server *Server) ReviseMemoryRecord(ctx context.Context, request *controlplanev1.ReviseMemoryRecordRequest) (*controlplanev1.ReviseMemoryRecordResponse, error) {
	specification, err := domainMemorySpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(err)
	}
	payload := command.MemoryRecordInput{RecordRef: request.GetRecordRef(), Specification: specification}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ReviseMemoryRecord_FullMethodName, command.ReviseMemoryRecord, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	record, err := castMemoryRecord(result.MemoryRecord)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReviseMemoryRecordResponse{Record: record}, nil
}

func (server *Server) ArchiveMemoryRecord(ctx context.Context, request *controlplanev1.ArchiveMemoryRecordRequest) (*controlplanev1.ArchiveMemoryRecordResponse, error) {

	payload := command.MemoryRecordInput{RecordRef: request.GetRecordRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveMemoryRecord_FullMethodName, command.ArchiveMemoryRecord, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	record, err := castMemoryRecord(result.MemoryRecord)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveMemoryRecordResponse{Record: record}, nil
}

func (server *Server) RestoreMemoryRecord(ctx context.Context, request *controlplanev1.RestoreMemoryRecordRequest) (*controlplanev1.RestoreMemoryRecordResponse, error) {

	payload := command.MemoryRecordInput{RecordRef: request.GetRecordRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RestoreMemoryRecord_FullMethodName, command.RestoreMemoryRecord, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	record, err := castMemoryRecord(result.MemoryRecord)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RestoreMemoryRecordResponse{Record: record}, nil
}

func (server *Server) PurgeMemoryRecord(ctx context.Context, request *controlplanev1.PurgeMemoryRecordRequest) (*controlplanev1.PurgeMemoryRecordResponse, error) {

	payload := command.MemoryRecordInput{RecordRef: request.GetRecordRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PurgeMemoryRecord_FullMethodName, command.PurgeMemoryRecord, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	record, err := castMemoryRecord(result.MemoryRecord)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PurgeMemoryRecordResponse{Record: record}, nil
}
