package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
)

func (server *RoleImageServer) RequestOrganizationRoleImagePromotion(ctx context.Context, request *controlplanev1.RequestOrganizationRoleImagePromotionRequest) (*controlplanev1.RequestOrganizationRoleImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_RequestOrganizationRoleImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.RequestOrganizationPromotion(ctx, roleimagerepository.PromotionRequestInput{
		Principal: p, Mutation: mutation(request.GetMutation()), RecipeRef: request.GetRecipeRef(),
		ArtifactRef: request.GetImageArtifactRef(), ExpectedProvenanceSHA256: request.GetExpectedProvenanceSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RequestOrganizationRoleImagePromotionResponse{Receipt: castRoleImagePromotionReceipt(result)}, nil
}

func (server *RoleImageServer) ListOrganizationRoleImageRecipeRevisions(ctx context.Context, request *controlplanev1.ListOrganizationRoleImageRecipeRevisionsRequest) (*controlplanev1.ListOrganizationRoleImageRecipeRevisionsResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListOrganizationRoleImageRecipeRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListOrganizationRevisions(ctx, p, request.GetRecipeRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListOrganizationRoleImageRecipeRevisionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, &controlplanev1.RoleImageRecipeRevision{
			Ref: item.Ref, RecipeRef: item.RecipeRef, Revision: int64(item.Revision), RecipeVersion: int64(item.RecipeVersion),
			RecipeGeneration: int64(item.RecipeGeneration), SpecSha256: item.SpecSHA256, ProvenanceSha256: item.ProvenanceSHA256,
			SourceSha256: item.SourceSHA256, ImmutableBuildSha256: item.ImmutableBuildSHA256, ImageArtifactRef: item.ImageArtifactRef,
			ManifestDigest: item.ManifestDigest, PromotedReference: item.PromotedReference, PromotionReceiptSha256: item.PromotionReceiptSHA256, CreatedAt: timestamp(item.CreatedAt),
		})
	}
	return response, nil
}
