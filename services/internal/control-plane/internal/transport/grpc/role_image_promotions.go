package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func (server *Server) PromoteRoleImage(ctx context.Context, request *controlplanev1.PromoteRoleImageRequest) (*controlplanev1.PromoteRoleImageResponse, error) {
	principal, err := principal(ctx, controlplanev1.PlatformCommandService_PromoteRoleImage_FullMethodName)
	if err != nil {
		return nil, err
	}
	commandMutation := mutation(request.GetMutation())
	if commandMutation.IdempotencyKey == "" {
		commandMutation.IdempotencyKey = "rpc-" + principal.CorrelationRef
		if len(commandMutation.IdempotencyKey) > 128 {
			commandMutation.IdempotencyKey = commandMutation.IdempotencyKey[:128]
		}
	}
	receipt, err := server.roleImages.Promote(ctx, roleimagerepository.PromotionRequestInput{
		Principal: principal, Mutation: commandMutation, RecipeRef: request.GetRecipeRef(),
		ArtifactRef: request.GetImageArtifactRef(), ExpectedProvenanceSHA256: request.GetExpectedProvenanceSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.PromoteRoleImageResponse{Receipt: castRoleImagePromotionReceipt(receipt)}, nil
}

func castRoleImagePromotionReceipt(input entity.RoleImagePromotionReceipt) *controlplanev1.RoleImagePromotionReceipt {
	return &controlplanev1.RoleImagePromotionReceipt{
		Ref: input.Ref, RecipeRef: input.RecipeRef, ImageArtifactRef: input.ImageArtifactRef,
		ProvenanceSha256: input.ProvenanceSHA256, ManifestDigest: input.ManifestDigest,
		ReceiptSha256: input.ReceiptSHA256, State: input.State, CreatedAt: timestamp(input.CreatedAt),
	}
}
