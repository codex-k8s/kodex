package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

// ReviewSkillBundleDraft rejects an unknown decision enum and sends exact bundle/revision/digest pins plus the review comment to the owner command before projecting its result.
// ReviewSkillBundleDraft отклоняет неизвестное решение и передаёт точные пакет, ревизию и дайджест вместе с комментарием команде владельца до проекции результата.
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

// PublishSkillBundleDraft delegates publication with pinned bundle, revision and digest to the distinct owner command; the transport does not infer admission from the request.
// PublishSkillBundleDraft передаёт публикацию с закреплёнными пакетом, ревизией и дайджестом отдельной команде владельца; транспорт не считает запрос доказательством допуска.
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

// DiscardSkillBundleDraft delegates the exact draft revision and digest to the owner discard command and projects the returned bundle without locally changing draft state.
// DiscardSkillBundleDraft передаёт точные ревизию черновика и дайджест команде отказа владельца и проецирует пакет без локального изменения состояния черновика.
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

// ArchiveSkillBundle delegates the exact bundle ref and mutation to the owner archive command and projects its result; lifecycle eligibility is checked by the owner.
// ArchiveSkillBundle передаёт точную ссылку пакета и mutation команде архивации владельца и проецирует результат; допустимость перехода проверяет владелец.
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

// RestoreSkillBundle delegates the exact bundle ref and mutation to the separate owner restore command; the transport does not assume the archived revision is admissible.
// RestoreSkillBundle передаёт точную ссылку пакета и mutation отдельной команде восстановления владельца; транспорт не предполагает допуск архивной ревизии.
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

// PurgeSkillBundle delegates the exact bundle ref and mutation to the distinct owner purge command and projects its result without directly deleting artifacts here.
// PurgeSkillBundle передаёт точную ссылку пакета и mutation отдельной команде необратимой очистки владельца и проецирует результат без прямого удаления артефактов здесь.
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
