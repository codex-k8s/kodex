package httptransport

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListRoleEnvironments(writer http.ResponseWriter, request *http.Request) {
	response, err := server.control.RoleImages.ListRoleEnvironments(request.Context(), &controlplanev1.ListRoleEnvironmentsRequest{})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	result := generated.RoleEnvironmentPage{Items: make([]generated.RoleEnvironment, 0, len(response.GetEnvironments()))}
	for _, environment := range response.GetEnvironments() {
		result.Items = append(result.Items, publicRoleEnvironment(environment))
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) ListRoleImageRecipes(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListRoleImageRecipesParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ListRoleImageRecipes(request.Context(), &controlplanev1.ListRoleImageRecipesRequest{
		ProjectRef: projectRef, RoleDefinitionRef: stringValue(parameters.RoleDefinitionRef),
		Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	result := generated.RoleImageRecipePage{Items: make([]generated.RoleImageRecipe, 0, len(response.GetRecipes()))}
	for _, recipe := range response.GetRecipes() {
		result.Items = append(result.Items, publicRoleImageRecipe(recipe))
	}
	if token := response.GetPage().GetNextPageToken(); token != "" {
		result.NextPageToken = &token
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) GetRoleImageRecipe(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef) {
	response, err := server.control.RoleImages.GetRoleImageRecipe(request.Context(), &controlplanev1.GetRoleImageRecipeRequest{RecipeRef: recipeRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if response.GetRecipe() == nil || response.GetRecipe().GetProjectRef() != projectRef {
		writeLocalProblem(writer, http.StatusNotFound, "NOT_FOUND", false)
		return
	}
	result := generated.RoleImageRecipeDetail{
		Recipe: publicRoleImageRecipe(response.GetRecipe()),
		Builds: make([]generated.RoleImageBuild, 0, len(response.GetBuilds())),
	}
	for _, build := range response.GetBuilds() {
		result.Builds = append(result.Builds, publicRoleImageBuild(build))
	}
	if response.GetActiveArtifact() != nil {
		artifact := publicRoleImageArtifact(response.GetActiveArtifact())
		result.ActiveArtifact = &artifact
	}
	if response.GetPromotionCandidate() != nil {
		artifact := publicRoleImageArtifact(response.GetPromotionCandidate())
		result.PromotionCandidate = &artifact
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) CreateRoleImageRecipe(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeCreateInput](writer, request)
	if !ok {
		return
	}
	request, ok = withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageRoleImageRecipe(request.Context(), &controlplanev1.ManageRoleImageRecipeRequest{
		Mutation: mutation, Action: controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_CREATE,
		ProjectRef: projectRef, RoleDefinitionRef: body.RoleDefinitionRef, Name: body.Name,
		Environment: roleEnvironmentSelection(body.Environment),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusCreated, publicRoleImageRecipe(response.GetRecipe()))
}

func (server *Server) UpdateRoleImageRecipe(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, parameters generated.UpdateRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeUpdateInput](writer, request)
	if !ok {
		return
	}
	request, ok = withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageRoleImageRecipe(request.Context(), &controlplanev1.ManageRoleImageRecipeRequest{
		Mutation: mutation, Action: controlplanev1.RoleImageRecipeAction_ROLE_IMAGE_RECIPE_ACTION_UPDATE,
		ProjectRef: projectRef, RecipeRef: recipeRef, Name: body.Name,
		Environment: roleEnvironmentSelection(body.Environment),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusOK, publicRoleImageRecipe(response.GetRecipe()))
}

func (server *Server) CommandRoleImageRecipe(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, parameters generated.CommandRoleImageRecipeParams) {
	body, ok := decodeJSON[generated.RoleImageRecipeCommand](writer, request)
	if !ok {
		return
	}
	action, ok := roleImageRecipeAction(body.Action)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	request, ok = withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.RoleImages.ManageRoleImageRecipe(request.Context(), &controlplanev1.ManageRoleImageRecipeRequest{
		Mutation: mutation, Action: action, ProjectRef: projectRef, RecipeRef: recipeRef,
	})
	if err != nil {
		writeRPCProblem(writer, err)
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

func roleEnvironmentSelection(input generated.RoleEnvironmentSelection) *controlplanev1.RoleEnvironmentSelection {
	result := &controlplanev1.RoleEnvironmentSelection{EnvironmentKey: input.EnvironmentKey, Dockerfile: input.Dockerfile}
	if input.PackageKeys != nil {
		result.PackageKeys = append([]string(nil), (*input.PackageKeys)...)
	}
	if input.ToolKeys != nil {
		result.ToolKeys = append([]string(nil), (*input.ToolKeys)...)
	}
	if input.InstallationBlock != nil {
		result.InstallationBlock = *input.InstallationBlock
	}
	return result
}

func publicRoleEnvironment(input *controlplanev1.RoleEnvironment) generated.RoleEnvironment {
	result := generated.RoleEnvironment{
		Key: input.GetKey(), NameMessageKey: input.GetNameMessageKey(),
		DescriptionMessageKey: input.GetDescriptionMessageKey(),
		SoftwareMessageKeys:   append([]string(nil), input.GetSoftwareMessageKeys()...),
		Recommended:           input.GetRecommended(), Available: input.GetAvailable(),
		CustomInstallationAllowed: input.GetCustomInstallationAllowed(),
		DockerfileTemplate:        input.GetDockerfileTemplate(),
		Platforms:                 make([]generated.RoleEnvironmentPlatform, 0, len(input.GetPlatforms())),
	}
	if value := input.GetUnavailableMessageKey(); value != "" {
		result.UnavailableMessageKey = &value
	}
	for _, platform := range input.GetPlatforms() {
		current := generated.RoleEnvironmentPlatform{
			Os:           generated.RoleEnvironmentPlatformOs(platform.GetOs()),
			Architecture: generated.RoleEnvironmentPlatformArchitecture(platform.GetArchitecture()),
		}
		if value := platform.GetVariant(); value != "" {
			current.Variant = &value
		}
		result.Platforms = append(result.Platforms, current)
	}
	return result
}

func publicRoleImageRecipe(input *controlplanev1.RoleImageRecipe) generated.RoleImageRecipe {
	result := generated.RoleImageRecipe{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), ProjectRef: input.GetProjectRef(),
		RoleDefinitionRef: input.GetRoleDefinitionRef(), Name: input.GetName(),
		State: generated.RoleImageRecipeState(input.GetState()), Generation: int64(input.GetGeneration()),
		PromotedImageReady: input.GetPromotedImageReference() != "",
		CreatedAt:          protoTime(input.GetCreatedAt()), UpdatedAt: protoTime(input.GetUpdatedAt()),
		NextActions: make([]generated.NextAction, 0, len(input.GetNextActions())),
	}
	if value := input.GetActiveImageArtifactRef(); value != "" {
		result.ActiveImageArtifactRef = &value
	}
	if value := input.GetPromotedImageReference(); value != "" {
		result.PromotedImageReference = &value
	}
	if environment := input.GetEnvironment(); environment != nil {
		result.Environment = generated.RoleEnvironmentSelection{EnvironmentKey: environment.GetEnvironmentKey(), Dockerfile: environment.GetDockerfile()}
		if values := environment.GetPackageKeys(); len(values) > 0 {
			copy := append([]string(nil), values...)
			result.Environment.PackageKeys = &copy
		}
		if values := environment.GetToolKeys(); len(values) > 0 {
			copy := append([]string(nil), values...)
			result.Environment.ToolKeys = &copy
		}
		if value := environment.GetInstallationBlock(); value != "" {
			result.Environment.InstallationBlock = &value
		}
	}
	for _, action := range input.GetNextActions() {
		result.NextActions = append(result.NextActions, generated.NextAction(action))
	}
	return result
}

func publicRoleImageBuild(input *controlplanev1.ImageBuild) generated.RoleImageBuild {
	result := generated.RoleImageBuild{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), RecipeRef: input.GetRecipeRef(),
		RecipeGeneration: int64(input.GetRecipeGeneration()), Dockerfile: input.GetDockerfile(),
		Attempt: int(input.GetAttempt()), Stage: generated.RoleImageBuildStage(strings.TrimPrefix(input.GetStage().String(), "IMAGE_BUILD_STAGE_")),
		ProgressPercent: int(input.GetProgressPercent()), CreatedAt: protoTime(input.GetCreatedAt()), UpdatedAt: protoTime(input.GetUpdatedAt()),
	}
	if value := input.GetSafeErrorCode(); value != "" {
		result.SafeErrorCode = &value
	}
	if value := input.GetDiagnosticCode(); value != "" {
		result.DiagnosticCode = &value
	}
	if value := input.GetDiagnosticSummary(); value != "" {
		result.DiagnosticSummary = &value
	}
	return result
}

func publicRoleImageArtifact(input *controlplanev1.ImageArtifact) generated.RoleImageArtifact {
	result := generated.RoleImageArtifact{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), RecipeRef: input.GetRecipeRef(),
		RecipeGeneration: int64(input.GetRecipeGeneration()), ManifestDigest: input.GetManifestDigest(),
		ProvenanceSha256: input.GetProvenanceSha256(),
		AdmissionVerdict: generated.RoleImageArtifactAdmissionVerdict(strings.TrimPrefix(input.GetAdmissionVerdict().String(), "IMAGE_ADMISSION_VERDICT_")),
		Tools:            make([]generated.RoleImageArtifactTool, 0, len(input.GetTools())),
	}
	if value := input.GetPromotedReference(); value != "" {
		result.PromotedReference = &value
	}
	if input.GetPromotedAt() != nil {
		value := generated.Timestamp(protoTime(input.GetPromotedAt()))
		result.PromotedAt = &value
	}
	if value := input.GetSbomSha256(); value != "" {
		result.SbomSha256 = &value
	}
	if value := input.GetVulnerabilityEvidenceSha256(); value != "" {
		result.VulnerabilityEvidenceSha256 = &value
	}
	for _, tool := range input.GetTools() {
		result.Tools = append(result.Tools, generated.RoleImageArtifactTool{Name: tool.GetName(), Version: tool.GetVersion()})
	}
	return result
}

func roleImageRecipeAction(input generated.RoleImageRecipeCommandAction) (controlplanev1.RoleImageRecipeAction, bool) {
	value, ok := controlplanev1.RoleImageRecipeAction_value["ROLE_IMAGE_RECIPE_ACTION_"+string(input)]
	return controlplanev1.RoleImageRecipeAction(value), ok
}

func protoTime(input interface{ AsTime() time.Time }) time.Time {
	if input == nil {
		return time.Time{}
	}
	return input.AsTime().UTC()
}

func setVersionETag(writer http.ResponseWriter, version uint64) {
	if version > 0 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", version))
	}
}
