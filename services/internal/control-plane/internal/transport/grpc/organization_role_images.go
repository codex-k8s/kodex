package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
)

// RequestOrganizationRoleImagePromotion resolves its exact organization-operation principal, forwards
// mutation and artifact/provenance pins to the domain and returns only the resulting promotion
// receipt.
// RequestOrganizationRoleImagePromotion разрешает principal точной организационной операции, передаёт
// домену изменение и привязки артефакта и происхождения и возвращает лишь полученный receipt
// продвижения.
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

// ListOrganizationRoleImageRecipeRevisions keeps exact organization-read authority and forwards
// recipe/page selection to the domain, projecting ordered revision provenance and cursor without
// resolving ownership from request fields.
// ListOrganizationRoleImageRecipeRevisions сохраняет полномочия точного организационного чтения и
// передаёт домену выбор рецепта и страницы, перенося порядок ревизий, происхождение и курсор без
// назначения владения из полей запроса.
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
	response.Revisions = castResponseItems(items, castRoleImageRecipeRevision)
	return response, nil
}
