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
)

// ListMemoryRecords authenticates the exact query method, validates an optional resource state and projects paged memory records; the domain service owns project and agent eligibility.
// ListMemoryRecords проверяет вызов точного метода чтения, допустимость необязательного состояния и проецирует страницы записей памяти; доступность проекта и агента определяет доменный сервис.
func (server *Server) ListMemoryRecords(ctx context.Context, request *controlplanev1.ListMemoryRecordsRequest) (*controlplanev1.ListMemoryRecordsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListMemoryRecords_FullMethodName)
	if err != nil {
		return nil, err
	}
	state, ok := domainContextResourceState(request.GetState())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid memory state")
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

// ListMemoryRecordRevisions authenticates the exact history query and returns paged revisions of the selected record without altering retention or visibility.
// ListMemoryRecordRevisions проверяет вызов точного метода истории и возвращает страницы ревизий выбранной записи, не меняя срок хранения и видимость.
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

// domainMemorySpecification requires a valid retention timestamp and copies submitted fields into the domain input; it does not establish source provenance or permission.
// domainMemorySpecification требует корректное время окончания хранения и переносит поля запроса в доменный ввод; происхождение источника и полномочия не устанавливает.
func domainMemorySpecification(input *controlplanev1.MemoryRecordSpecification) (entity.MemoryRecordSpecification, error) {
	if input == nil || input.GetRetentionUntil() == nil || input.GetRetentionUntil().CheckValid() != nil {
		return entity.MemoryRecordSpecification{}, errs.ErrInvalid
	}
	return entity.MemoryRecordSpecification{Title: input.GetTitle(), Summary: input.GetSummary(), SourceRunRef: input.GetSourceRunRef(), RetentionUntil: input.GetRetentionUntil().AsTime().UTC()}, nil
}

// castContextProvenance projects stored actor, source revision and digest with creation time into a gRPC view without independently verifying that provenance.
// castContextProvenance проецирует сохранённые автора, ревизию источника и дайджест со временем создания в gRPC-представление без самостоятельной проверки происхождения.
func castContextProvenance(input entity.ContextProvenance) *controlplanev1.ContextProvenance {
	return &controlplanev1.ContextProvenance{ActorRef: input.ActorRef, SourceKind: input.SourceKind, SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, Digest: input.Digest, CreatedAt: timestamppb.New(input.CreatedAt)}
}

// castMemoryRevision preserves a nil revision and otherwise projects its version, digest, provenance, retention and redaction metadata into the wire type.
// castMemoryRevision сохраняет отсутствие ревизии и иначе проецирует её номер, дайджест, происхождение, срок хранения и отметку редактирования в транспортный тип.
func castMemoryRevision(input *entity.MemoryRecordRevision) *controlplanev1.MemoryRecordRevision {
	if input == nil {
		return nil
	}
	return &controlplanev1.MemoryRecordRevision{Ref: input.Ref, Revision: input.Revision, Title: input.Title, Summary: input.Summary, Digest: input.Digest,
		ParentRevisionRef: input.ParentRevisionRef, Provenance: castContextProvenance(input.Provenance), RetentionUntil: timestamppb.New(input.RetentionUntil), Redacted: input.Redacted}
}

// castMemoryRecord rejects an incomplete record or unknown state before projecting its exact ref, version, scope, current revision and timestamps; it does not authorize the reader.
// castMemoryRecord отклоняет неполную запись или неизвестное состояние до проекции точных ссылки, версии, области, текущей ревизии и времени; доступ читателя не проверяет.
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

// GetMemoryRecord authenticates the single-record query, asks the owner service for the requested ref and fails closed if the returned record cannot be projected.
// GetMemoryRecord проверяет вызов чтения одной записи, запрашивает ссылку у сервиса-владельца и закрыто отказывает, если результат нельзя проецировать.
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

// CreateMemoryRecord validates the submitted retention time and sends project, agent and specification with mutation context to the distinct owner command before projecting its record.
// CreateMemoryRecord проверяет срок хранения из запроса и передаёт проект, агента и спецификацию с контекстом изменения отдельной команде владельца, затем проецирует запись.
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

// ReviseMemoryRecord validates replacement specification fields and sends the exact record ref plus mutation context to the owner revision command; it does not replace the stored revision locally.
// ReviseMemoryRecord проверяет поля новой спецификации и передаёт точную ссылку записи с контекстом изменения команде ревизии владельца; локально сохранённую ревизию не подменяет.
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

// ArchiveMemoryRecord delegates the exact record ref and mutation to the archive command and projects its returned record; archival policy remains with the owner service.
// ArchiveMemoryRecord передаёт точную ссылку записи и mutation команде архивации и проецирует её результат; правила архивации остаются у сервиса-владельца.
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

// RestoreMemoryRecord delegates the exact record ref and mutation to the separate restore command and projects its result without inferring recoverability from the request.
// RestoreMemoryRecord передаёт точную ссылку записи и mutation отдельной команде восстановления и проецирует результат, не считая запрос доказательством возможности восстановления.
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

// PurgeMemoryRecord delegates the exact record ref and mutation to the distinct irreversible owner command and projects its returned record; this transport does not erase storage itself.
// PurgeMemoryRecord передаёт точную ссылку записи и mutation отдельной необратимой команде владельца и проецирует результат; сам транспорт хранилище не очищает.
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
