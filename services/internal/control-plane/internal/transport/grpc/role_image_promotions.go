package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// PromoteRoleImage resolves the exact principal and delegates promotion pins to the domain. If the
// mutation key is absent it preserves the existing rpc-correlation fallback capped at 128 bytes; this
// does not promise stable caller-selected retry identity.
// PromoteRoleImage разрешает точный principal и передаёт привязки продвижения домену. При отсутствии
// ключа изменения сохраняет прежний rpc-correlation fallback до 128 байт; устойчивый выбранный
// клиентом идентификатор повторов этим не обещается.
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

// castRoleImagePromotionReceipt projects the domain receipt's recipe/artifact,
// provenance/manifest/receipt hashes, state and creation time without creating or verifying a
// promotion receipt.
// castRoleImagePromotionReceipt переносит рецепт, артефакт, хеши происхождения, manifest и receipt,
// состояние и время доменного результата, не создавая и не проверяя receipt продвижения.
func castRoleImagePromotionReceipt(input entity.RoleImagePromotionReceipt) *controlplanev1.RoleImagePromotionReceipt {
	return &controlplanev1.RoleImagePromotionReceipt{
		Ref: input.Ref, RecipeRef: input.RecipeRef, ImageArtifactRef: input.ImageArtifactRef,
		ProvenanceSha256: input.ProvenanceSHA256, ManifestDigest: input.ManifestDigest,
		ReceiptSha256: input.ReceiptSHA256, State: input.State, CreatedAt: timestamp(input.CreatedAt),
	}
}
