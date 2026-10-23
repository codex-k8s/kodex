package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListSkillBundles authenticates the exact list method, maps the optional shared resource-state filter and projects paged bundles; project and agent eligibility remain with the owner service.
// ListSkillBundles проверяет вызов точного метода списка, преобразует необязательный общий фильтр состояния и проецирует страницы пакетов; доступность проекта и агента остаётся у сервиса-владельца.
func (server *Server) ListSkillBundles(ctx context.Context, request *controlplanev1.ListSkillBundlesRequest) (*controlplanev1.ListSkillBundlesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListSkillBundles_FullMethodName)
	if err != nil {
		return nil, err
	}
	state, ok := domainContextResourceState(request.GetState())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid skill state")
	}
	items, total, next, err := server.service.ListSkillBundles(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), ResourceRef: request.GetAgentRef(), Query: request.GetQuery(), State: state, Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListSkillBundlesResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		bundle, err := castSkillBundle(&item)
		if err != nil {
			return nil, err
		}
		response.Bundles = append(response.Bundles, bundle)
	}
	return response, nil
}

// ListSkillBundleRevisions authenticates the history query and projects paged revisions of one bundle, preserving caster errors rather than returning partial history.
// ListSkillBundleRevisions проверяет вызов истории и проецирует страницы ревизий одного пакета, сохраняя ошибки преобразования вместо частичной выдачи истории.
func (server *Server) ListSkillBundleRevisions(ctx context.Context, request *controlplanev1.ListSkillBundleRevisionsRequest) (*controlplanev1.ListSkillBundleRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListSkillBundleRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, total, next, err := server.service.ListSkillBundleRevisions(ctx, p, request.GetBundleRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListSkillBundleRevisionsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		revision, err := castSkillRevision(&item)
		if err != nil {
			return nil, err
		}
		response.Revisions = append(response.Revisions, revision)
	}
	return response, nil
}

// domainSkillSpecification copies requested skill metadata and each path/artifact revision into domain input without treating submitted file refs as admitted content.
// domainSkillSpecification переносит метаданные навыка и каждую ссылку пути и версии артефакта в доменный ввод, не считая переданные ссылки допущенным содержимым.
func domainSkillSpecification(input *controlplanev1.SkillBundleSpecification) entity.SkillBundleSpecification {
	result := entity.SkillBundleSpecification{Name: input.GetName(), Description: input.GetDescription()}
	for _, file := range input.GetFiles() {
		result.Files = append(result.Files, entity.SkillBundleFile{Path: file.GetPath(), ArtifactRef: file.GetArtifactRef(), ArtifactRevision: file.GetArtifactRevision()})
	}
	return result
}

// ValidateSkillBundleDraft sends exact bundle/revision/digest pins to the owner validation command and projects its result; the transport does not perform scanning itself.
// ValidateSkillBundleDraft передаёт точные пакет, ревизию и дайджест команде проверки владельца и проецирует результат; транспорт самостоятельно не сканирует файлы.
func (server *Server) ValidateSkillBundleDraft(ctx context.Context, request *controlplanev1.ValidateSkillBundleDraftRequest) (*controlplanev1.ValidateSkillBundleDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateSkillBundleDraft_FullMethodName, command.ValidateSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedDigest: request.GetExpectedDigest()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateSkillBundleDraftResponse{Bundle: bundle}, nil
}

// castSkillRevision preserves an absent revision, rejects unknown lifecycle or scan states and projects exact provenance, diagnostics and artifact file descriptors.
// castSkillRevision сохраняет отсутствие ревизии, отклоняет неизвестные состояния жизненного цикла и сканирования и проецирует точные происхождение, диагностику и описатели файлов.
func castSkillRevision(input *entity.SkillBundleRevision) (*controlplanev1.SkillBundleRevision, error) {
	if input == nil {
		return nil, nil
	}
	state, ok := controlplanev1.SkillRevisionState_value["SKILL_REVISION_STATE_"+input.State]
	scan, scanOK := controlplanev1.SkillScanState_value["SKILL_SCAN_STATE_"+input.ScanState]
	if !ok || !scanOK || state == 0 || scan == 0 || input.Ref == "" || input.Revision < 1 {
		return nil, status.Error(codes.Internal, "skill revision result is invalid")
	}
	result := &controlplanev1.SkillBundleRevision{Ref: input.Ref, Revision: input.Revision, State: controlplanev1.SkillRevisionState(state), Name: input.Name, Description: input.Description, Digest: input.Digest, ParentRevisionRef: input.ParentRevisionRef,
		Provenance: castContextProvenance(input.Provenance), ScanState: controlplanev1.SkillScanState(scan), ScanEngine: input.ScanEngine, ScanDigest: input.ScanDigest, ReviewedBy: input.ReviewedBy, Diagnostics: input.Diagnostics}
	if input.ScannedAt != nil {
		result.ScannedAt = timestamppb.New(*input.ScannedAt)
	}
	if input.ReviewedAt != nil {
		result.ReviewedAt = timestamppb.New(*input.ReviewedAt)
	}
	for _, file := range input.Files {
		result.Files = append(result.Files, &controlplanev1.SkillBundleFile{Path: file.Path, ArtifactRef: file.ArtifactRef, ArtifactRevision: file.ArtifactRevision, Digest: file.Digest, SizeBytes: file.SizeBytes})
	}
	return result, nil
}

// castSkillBundle requires a complete bundle identity/version and one of the closed public states before projecting current and draft revisions; it does not authorize access.
// castSkillBundle требует полные идентификатор и версию пакета и одно из закрытого набора публичных состояний до проекции текущей и черновой ревизий; доступ не проверяет.
func castSkillBundle(input *entity.SkillBundle) (*controlplanev1.SkillBundle, error) {
	if input == nil || input.Ref == "" || input.Version < 1 {
		return nil, status.Error(codes.Internal, "skill bundle result is incomplete")
	}
	state, ok := controlplanev1.ContextResourceState_value["CONTEXT_RESOURCE_STATE_"+input.State]
	if !ok || (input.State != "ACTIVE" && input.State != "ARCHIVED" && input.State != "PURGED") {
		return nil, status.Error(codes.Internal, "skill bundle state is invalid")
	}
	current, err := castSkillRevision(input.CurrentRevision)
	if err != nil {
		return nil, err
	}
	draft, err := castSkillRevision(input.DraftRevision)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SkillBundle{Ref: input.Ref, Version: input.Version, ProjectRef: input.ProjectRef, State: controlplanev1.ContextResourceState(state), CurrentRevision: current, DraftRevision: draft, CreatedAt: timestamppb.New(input.CreatedAt), UpdatedAt: timestamppb.New(input.UpdatedAt)}, nil
}

// GetSkillBundle authenticates the single-bundle query, requests the exact ref from the owner service and fails closed on an invalid projected bundle.
// GetSkillBundle проверяет вызов чтения одного пакета, запрашивает точную ссылку у сервиса-владельца и закрыто отказывает при недопустимой проекции пакета.
func (server *Server) GetSkillBundle(ctx context.Context, request *controlplanev1.GetSkillBundleRequest) (*controlplanev1.GetSkillBundleResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetSkillBundle_FullMethodName)
	if err != nil {
		return nil, err
	}
	bundle, err := server.service.GetSkillBundle(ctx, p, request.GetBundleRef())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := castSkillBundle(&bundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.GetSkillBundleResponse{Bundle: result}, nil
}

// CreateSkillBundleDraft passes project, optional bundle ref and specification with mutation context to the distinct owner create command, then projects the returned bundle.
// CreateSkillBundleDraft передаёт проект, необязательную ссылку пакета и спецификацию с контекстом изменения отдельной команде создания владельца, затем проецирует пакет.
func (server *Server) CreateSkillBundleDraft(ctx context.Context, request *controlplanev1.CreateSkillBundleDraftRequest) (*controlplanev1.CreateSkillBundleDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateSkillBundleDraft_FullMethodName, command.CreateSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{ProjectRef: request.GetProjectRef(), BundleRef: request.GetBundleRef(), Specification: domainSkillSpecification(request.GetSpecification())})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateSkillBundleDraftResponse{Bundle: bundle}, nil
}

// SaveSkillBundleDraft passes the exact bundle/revision refs and replacement specification with mutation context to the owner save command; this transport does not publish the draft.
// SaveSkillBundleDraft передаёт точные ссылки пакета и ревизии и новую спецификацию с контекстом изменения команде сохранения владельца; транспорт не публикует черновик.
func (server *Server) SaveSkillBundleDraft(ctx context.Context, request *controlplanev1.SaveSkillBundleDraftRequest) (*controlplanev1.SaveSkillBundleDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SaveSkillBundleDraft_FullMethodName, command.SaveSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), Specification: domainSkillSpecification(request.GetSpecification())})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SaveSkillBundleDraftResponse{Bundle: bundle}, nil
}
