package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

func (server *Server) ReviewSkillBundleDraft(ctx context.Context, request *controlplanev1.ReviewSkillBundleDraftRequest) (*controlplanev1.ReviewSkillBundleDraftResponse, error) {
	name, ok := controlplanev1.SkillReviewDecision_name[int32(request.GetDecision())]
	if !ok || request.GetDecision() == controlplanev1.SkillReviewDecision_SKILL_REVIEW_DECISION_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "invalid skill review decision")
	}
	decision := strings.TrimPrefix(name, "SKILL_REVIEW_DECISION_")
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ReviewSkillBundleDraft_FullMethodName, command.ReviewSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedDigest: request.GetExpectedDigest(), Decision: decision, Comment: request.GetComment()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReviewSkillBundleDraftResponse{Bundle: bundle}, nil
}

func (server *Server) PublishSkillBundleDraft(ctx context.Context, request *controlplanev1.PublishSkillBundleDraftRequest) (*controlplanev1.PublishSkillBundleDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishSkillBundleDraft_FullMethodName, command.PublishSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedDigest: request.GetExpectedDigest()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishSkillBundleDraftResponse{Bundle: bundle}, nil
}

func (server *Server) DiscardSkillBundleDraft(ctx context.Context, request *controlplanev1.DiscardSkillBundleDraftRequest) (*controlplanev1.DiscardSkillBundleDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DiscardSkillBundleDraft_FullMethodName, command.DiscardSkillBundleDraft, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedDigest: request.GetExpectedDigest()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DiscardSkillBundleDraftResponse{Bundle: bundle}, nil
}

func (server *Server) ArchiveSkillBundle(ctx context.Context, request *controlplanev1.ArchiveSkillBundleRequest) (*controlplanev1.ArchiveSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveSkillBundle_FullMethodName, command.ArchiveSkillBundle, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveSkillBundleResponse{Bundle: bundle}, nil
}

func (server *Server) RestoreSkillBundle(ctx context.Context, request *controlplanev1.RestoreSkillBundleRequest) (*controlplanev1.RestoreSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RestoreSkillBundle_FullMethodName, command.RestoreSkillBundle, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RestoreSkillBundleResponse{Bundle: bundle}, nil
}

func (server *Server) PurgeSkillBundle(ctx context.Context, request *controlplanev1.PurgeSkillBundleRequest) (*controlplanev1.PurgeSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PurgeSkillBundle_FullMethodName, command.PurgeSkillBundle, request.GetMutation(), command.SkillBundleInput{BundleRef: request.GetBundleRef()})
	if err != nil {
		return nil, err
	}
	bundle, err := castSkillBundle(result.SkillBundle)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PurgeSkillBundleResponse{Bundle: bundle}, nil
}
