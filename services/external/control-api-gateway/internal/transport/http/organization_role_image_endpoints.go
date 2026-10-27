package httptransport

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

// validOrganizationRoleImageRecipe requires nonnil ORGANIZATION scope and a consistent owner organization/empty-project resource scope; it does not derive organization identity from a client request.
// validOrganizationRoleImageRecipe требует ненулевой ORGANIZATION scope и согласованные организацию владельца и отсутствие проекта; организационную identity из клиентского запроса не выводит.
func validOrganizationRoleImageRecipe(recipe *controlplanev1.RoleImageRecipe) bool {
	return recipe != nil && recipe.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		validRuntimeResourceScope(generated.RuntimeResourceScopeKindORGANIZATION, recipe.GetOrganizationRef(), recipe.GetProjectRef())
}

// validOrganizationRoleImageBuild allows an absent optional build, otherwise binds organization scope, empty project, organization and recipe references to the supplied recipe; no image signature is checked here.
// validOrganizationRoleImageBuild допускает отсутствие сборки, иначе привязывает организационную область, пустой проект и organization/recipe к переданному рецепту; подпись образа не проверяет.
func validOrganizationRoleImageBuild(build *controlplanev1.ImageBuild, recipe *controlplanev1.RoleImageRecipe) bool {
	return build == nil || build.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		build.GetProjectRef() == "" && build.GetOrganizationRef() == recipe.GetOrganizationRef() && build.GetRecipeRef() == recipe.GetRef()
}

// validOrganizationRoleImageArtifact allows absence, otherwise requires valid risk history and exact organization/recipe with no project. These are owner-response consistency checks, not an operator's risk approval.
// validOrganizationRoleImageArtifact допускает отсутствие, иначе требует допустимую risk history и точные organization/recipe без проекта. Это согласованность ответа владельца, а не принятие риска оператором.
func validOrganizationRoleImageArtifact(artifact *controlplanev1.ImageArtifact, recipe *controlplanev1.RoleImageRecipe) bool {
	return artifact == nil || validImageRiskHistory(artifact) && artifact.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		artifact.GetProjectRef() == "" && artifact.GetOrganizationRef() == recipe.GetOrganizationRef() && artifact.GetRecipeRef() == recipe.GetRef()
}

// validOrganizationRoleImageReceipt first enforces organization recipe/build/artifact scope, then adapts the existing typed response into shared receipt validation with empty project and optional exact recipe ref. Bad upstream state fails closed rather than changing scope.
// validOrganizationRoleImageReceipt сначала проверяет организационные recipe/build/artifact, затем приспосабливает типизированный ответ к общей проверке receipt с пустым проектом и optional точным recipe ref. Неверный upstream закрыто отклоняется, область не меняется.
func validOrganizationRoleImageReceipt(writer http.ResponseWriter, response *controlplanev1.ManageOrganizationRoleImageRecipeResponse, recipeRef string) bool {
	if response == nil || !validOrganizationRoleImageRecipe(response.GetRecipe()) || !validOrganizationRoleImageBuild(response.GetImageBuild(), response.GetRecipe()) ||
		!validOrganizationRoleImageArtifact(response.GetImageArtifact(), response.GetRecipe()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return false
	}
	return validRoleImageReceipt(writer, &controlplanev1.ManageRoleImageRecipeResponse{Recipe: response.GetRecipe(), ImageBuild: response.GetImageBuild(), ImageArtifact: response.GetImageArtifact(), Reused: response.GetReused()}, "", recipeRef)
}

// ListSystemRoleImageRecipeRevisions uses the organization-owner RPC and requires a nonnil page of at most100 exact-recipe revisions with valid refs and positive revision/version before public pagination output.
// ListSystemRoleImageRecipeRevisions использует организационный RPC владельца и требует ненулевую страницу до100 ревизий точного рецепта с допустимыми refs и положительными revision/version до публичной выдачи.
func (server *Server) ListSystemRoleImageRecipeRevisions(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.ListSystemRoleImageRecipeRevisionsParams) {
	response, err := server.control.RoleImages.ListOrganizationRoleImageRecipeRevisions(request.Context(), &controlplanev1.ListOrganizationRoleImageRecipeRevisionsRequest{
		RecipeRef: recipeRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response == nil || len(response.GetRevisions()) > 100 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, revision := range response.GetRevisions() {
		if revision == nil || revision.GetRecipeRef() != recipeRef || !effectiveCapabilityRef(revision.GetRef()) || revision.GetRevision() < 1 || revision.GetRecipeVersion() < 1 {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeMessage(writer, http.StatusOK, response, "", "revisions")
}

// PromoteSystemRoleImage forwards mutation/version and artifact/provenance pins to owner promotion, then requires an exact recipe/artifact/provenance receipt before HTTP202. Acceptance is not proof the image was published or risk approved.
// PromoteSystemRoleImage передаёт mutation/version и artifact/provenance pins продвижению владельца, затем требует точный recipe/artifact/provenance receipt до HTTP202. Принятие не доказывает публикацию и подтверждение риска.
func (server *Server) PromoteSystemRoleImage(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.PromoteSystemRoleImageParams) {
	body, mutation, ok := decodeMutationBody[generated.RoleImagePromotionInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.RequestOrganizationRoleImagePromotion(request.Context(), &controlplanev1.RequestOrganizationRoleImagePromotionRequest{
		Mutation: mutation, RecipeRef: recipeRef, ImageArtifactRef: body.ImageArtifactRef, ExpectedProvenanceSha256: body.ExpectedProvenanceSha256,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response == nil || response.GetReceipt() == nil || response.GetReceipt().GetRecipeRef() != recipeRef || response.GetReceipt().GetImageArtifactRef() != body.ImageArtifactRef ||
		response.GetReceipt().GetProvenanceSha256() != body.ExpectedProvenanceSha256 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(writer, http.StatusAccepted, response, "receipt", "")
}

// ListSystemRoleImageRecipes validates bounded search/state/page input and owner counts, then requires unique organization-scoped recipes, consistent lineage/source and advancing bounded cursor. The request carries no caller-selected organization authority field.
// ListSystemRoleImageRecipes проверяет поиск, state/page и счётчики владельца, затем требует уникальные организационные рецепты, lineage/source и продвижение ограниченного cursor. В запросе нет клиентского поля полномочий организации.
func (server *Server) ListSystemRoleImageRecipes(writer http.ResponseWriter, request *http.Request, parameters generated.ListSystemRoleImageRecipesParams) {
	query, state := stringValue(parameters.Query), ""
	if parameters.State != nil {
		state = string(*parameters.State)
	}
	if !validSearchText(query, 0, 128) || len(query) > 128 || state != "" && state != "ACTIVE" && state != "ARCHIVED" ||
		parameters.PageSize != nil && (*parameters.PageSize < 1 || *parameters.PageSize > 100) ||
		parameters.PageToken != nil && !boundedModelText(*parameters.PageToken, 512) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.RoleImages.ListOrganizationRoleImageRecipes(request.Context(), &controlplanev1.ListOrganizationRoleImageRecipesRequest{

		Page: page(parameters.PageSize, parameters.PageToken), Query: query, State: state,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeRoleImageRecipePage(writer, response, parameters.PageSize, parameters.PageToken, validOrganizationRoleImageRecipe)
}

// GetSystemRoleImageRecipe requires the exact recipe and organization scope, validates related build/artifact ownership, lineage/source and risk history, then projects detail/admission failure with version ETag without performing admission itself.
// GetSystemRoleImageRecipe требует точный рецепт и организационную область, сверяет владение build/artifact, lineage/source и risk history, затем проецирует detail/admission failure с ETag без самостоятельного допуска.
func (server *Server) GetSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef) {
	response, err := server.control.RoleImages.GetOrganizationRoleImageRecipe(request.Context(), &controlplanev1.GetOrganizationRoleImageRecipeRequest{RecipeRef: recipeRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response.GetRecipe() == nil || response.GetRecipe().GetRef() != recipeRef || !validOrganizationRoleImageRecipe(response.GetRecipe()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeRoleImageRecipeDetail(writer, response,
		func(build *controlplanev1.ImageBuild) bool {
			return validOrganizationRoleImageBuild(build, response.GetRecipe())
		},
		func(artifact *controlplanev1.ImageArtifact) bool {
			return validOrganizationRoleImageArtifact(artifact, response.GetRecipe())
		},
	)
}

// CreateSystemRoleImageRecipe sends name/environment and create idempotency to the dedicated organization CREATE, validates scoped receipt and emits HTTP201 without assigning organization identity from the body.
// CreateSystemRoleImageRecipe передаёт name/environment и идемпотентный create отдельному организационному CREATE, проверяет receipt области и выдаёт HTTP201 без назначения организации из тела.
func (server *Server) CreateSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, parameters generated.CreateSystemRoleImageRecipeParams) {
	body, mutation, ok := decodeMutationBody[generated.RoleImageRecipeUpdateInput](writer, request, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageOrganizationRoleImageRecipe(request.Context(), &controlplanev1.ManageOrganizationRoleImageRecipeRequest{
		Mutation: mutation, Action: controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_CREATE,
		Name:        body.Name,
		Environment: roleEnvironmentSelection(body.Environment),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if !validOrganizationRoleImageReceipt(writer, response, "") {
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusCreated, publicRoleImageRecipe(response.GetRecipe()))
}

// UpdateSystemRoleImageRecipe maps editable name/environment, exact recipe and mutation/version to organization UPDATE, validates its scoped receipt and preserves ETag/public projection.
// UpdateSystemRoleImageRecipe преобразует name/environment, точный рецепт и mutation/version в организационный UPDATE, проверяет receipt области и сохраняет ETag и публичную проекцию.
func (server *Server) UpdateSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.UpdateSystemRoleImageRecipeParams) {
	body, mutation, ok := decodeMutationBody[generated.RoleImageRecipeUpdateInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageOrganizationRoleImageRecipe(request.Context(), &controlplanev1.ManageOrganizationRoleImageRecipeRequest{
		Mutation: mutation, Action: controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_UPDATE,
		RecipeRef: recipeRef, Name: body.Name,
		Environment: roleEnvironmentSelection(body.Environment),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if !validOrganizationRoleImageReceipt(writer, response, recipeRef) {
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusOK, publicRoleImageRecipe(response.GetRecipe()))
}

// CommandSystemRoleImageRecipe maps action/build and mutation pins to the dedicated organization command, enforces scoped receipt and returns recipe/optional build/reused metadata. It does not bypass owner lifecycle or convert a displayed admission hint into authority.
// CommandSystemRoleImageRecipe передаёт action/build и mutation pins отдельной организационной команде, требует receipt области и выдаёт recipe, optional build и reused. Lifecycle владельца не обходит, отображаемый hint допуска в полномочия не превращает.
func (server *Server) CommandSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.CommandSystemRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeCommand](writer, request)
	if !ok {
		return
	}
	action, ok := roleImageRecipeAction(body.Action)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	buildRef := ""
	if body.BuildRef != nil {
		buildRef = *body.BuildRef
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageOrganizationRoleImageRecipe(request.Context(), &controlplanev1.ManageOrganizationRoleImageRecipeRequest{
		Mutation: mutation, Action: action, RecipeRef: recipeRef,
		BuildRef: buildRef,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if !validOrganizationRoleImageReceipt(writer, response, recipeRef) {
		return
	}
	result := generated.RoleImageRecipeCommandReceipt{
		Recipe: publicRoleImageRecipe(response.GetRecipe()), Reused: response.GetReused(),
	}
	if response.GetImageBuild() != nil {
		build := publicRoleImageBuild(response.GetImageBuild())
		result.ImageBuild = &build
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusOK, result)
}
