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
	"strings"
)

func (server *Server) ListSkillBundles(ctx context.Context, request *controlplanev1.ListSkillBundlesRequest) (*controlplanev1.ListSkillBundlesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListSkillBundles_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_UNSPECIFIED {
		name, ok := controlplanev1.ContextResourceState_name[int32(request.GetState())]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid skill state")
		}
		state = strings.TrimPrefix(name, "CONTEXT_RESOURCE_STATE_")
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

func domainSkillSpecification(input *controlplanev1.SkillBundleSpecification) entity.SkillBundleSpecification {
	result := entity.SkillBundleSpecification{Name: input.GetName(), Description: input.GetDescription()}
	for _, file := range input.GetFiles() {
		result.Files = append(result.Files, entity.SkillBundleFile{Path: file.GetPath(), ArtifactRef: file.GetArtifactRef(), ArtifactRevision: file.GetArtifactRevision()})
	}
	return result
}

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
