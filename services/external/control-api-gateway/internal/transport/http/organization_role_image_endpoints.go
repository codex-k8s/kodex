package httptransport

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

func validOrganizationRoleImageRecipe(recipe *controlplanev1.RoleImageRecipe) bool {
	return recipe != nil && recipe.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		validRuntimeResourceScope(generated.RuntimeResourceScopeKindORGANIZATION, recipe.GetOrganizationRef(), recipe.GetProjectRef())
}

func validOrganizationRoleImageBuild(build *controlplanev1.ImageBuild, recipe *controlplanev1.RoleImageRecipe) bool {
	return build == nil || build.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		build.GetProjectRef() == "" && build.GetOrganizationRef() == recipe.GetOrganizationRef() && build.GetRecipeRef() == recipe.GetRef()
}

func validOrganizationRoleImageArtifact(artifact *controlplanev1.ImageArtifact, recipe *controlplanev1.RoleImageRecipe) bool {
	return artifact == nil || artifact.GetScopeKind() == controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION &&
		artifact.GetProjectRef() == "" && artifact.GetOrganizationRef() == recipe.GetOrganizationRef() && artifact.GetRecipeRef() == recipe.GetRef()
}

func validOrganizationRoleImageReceipt(writer http.ResponseWriter, response *controlplanev1.ManageOrganizationRoleImageRecipeResponse, recipeRef string) bool {
	if response == nil || !validOrganizationRoleImageRecipe(response.GetRecipe()) || !validOrganizationRoleImageBuild(response.GetImageBuild(), response.GetRecipe()) ||
		!validOrganizationRoleImageArtifact(response.GetImageArtifact(), response.GetRecipe()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return false
	}
	return validRoleImageReceipt(writer, &controlplanev1.ManageRoleImageRecipeResponse{Recipe: response.GetRecipe(), ImageBuild: response.GetImageBuild(), ImageArtifact: response.GetImageArtifact(), Reused: response.GetReused()}, "", recipeRef)
}

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

func (server *Server) PromoteSystemRoleImage(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.PromoteSystemRoleImageParams) {
	body, ok := decodeJSON[generated.RoleImagePromotionInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
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
	if response == nil || response.GetTotal() < int64(len(response.GetRecipes())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetRecipes()) > 100 ||
		parameters.PageSize != nil && len(response.GetRecipes()) > *parameters.PageSize {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RoleImageRecipePage{Items: make([]generated.RoleImageRecipe, 0, len(response.GetRecipes())), Total: response.GetTotal()}
	seen := make(map[string]bool, len(response.GetRecipes()))
	for _, recipe := range response.GetRecipes() {
		if recipe == nil || !validOrganizationRoleImageRecipe(recipe) || !effectiveCapabilityRef(recipe.GetRef()) || seen[recipe.GetRef()] ||
			!validRoleImageLineage(recipe.GetManagedLineage()) || !validRoleImageSource(recipe) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[recipe.GetRef()] = true
		result.Items = append(result.Items, publicRoleImageRecipe(recipe))
	}
	if token := response.GetPage().GetNextPageToken(); token != "" {
		if !boundedModelText(token, 512) || token == stringValue(parameters.PageToken) || len(result.Items) == 0 {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.NextPageToken = &token
	}
	writeJSON(writer, http.StatusOK, result)
}

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
	if !validRoleImageLineage(response.GetRecipe().GetManagedLineage()) || !validRoleImageSource(response.GetRecipe()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RoleImageRecipeDetail{
		Recipe: publicRoleImageRecipe(response.GetRecipe()),
		Builds: make([]generated.RoleImageBuild, 0, len(response.GetBuilds())),
	}
	for _, build := range response.GetBuilds() {
		if build == nil || !validOrganizationRoleImageBuild(build, response.GetRecipe()) || !validRoleImageBuildSource(build) || build.GetConfigurationRevisionRef() != "" && !effectiveCapabilityRef(build.GetConfigurationRevisionRef()) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.Builds = append(result.Builds, publicRoleImageBuild(build))
	}
	if response.GetActiveArtifact() != nil {
		if !validOrganizationRoleImageArtifact(response.GetActiveArtifact(), response.GetRecipe()) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		artifact := publicRoleImageArtifact(response.GetActiveArtifact())
		result.ActiveArtifact = &artifact
	}
	if response.GetPromotionCandidate() != nil {
		if !validOrganizationRoleImageArtifact(response.GetPromotionCandidate(), response.GetRecipe()) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		artifact := publicRoleImageArtifact(response.GetPromotionCandidate())
		result.PromotionCandidate = &artifact
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	failure, validFailure := publicRoleImageAdmissionFailure(response.GetAdmissionFailure(), response.GetRecipe(), response.GetBuilds())
	if !validFailure {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result.AdmissionFailure = failure
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) CreateSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, parameters generated.CreateSystemRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeUpdateInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
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

func (server *Server) UpdateSystemRoleImageRecipe(writer http.ResponseWriter, request *http.Request, recipeRef generated.RecipeRef, parameters generated.UpdateSystemRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeUpdateInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
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
