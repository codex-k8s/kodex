package httptransport

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ListRoleEnvironments reads owner-visible environment templates and maps them to an explicitly allocated public page; it does not build templates or validate every returned enum itself.
// ListRoleEnvironments читает видимые шаблоны окружений владельца и переносит их в явно созданную публичную страницу; шаблоны не собирает и каждый enum ответа сам не проверяет.
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

// ListRoleImageRecipes binds project context and validates bounded search/state/role/page input, then checks owner count, unique scoped references, lineage/source visibility and advancing cursor before public output. The read does not admit or promote images.
// ListRoleImageRecipes привязывает проект и проверяет поиск, state/role/page, затем сверяет счётчик, уникальные ссылки области, lineage/source visibility и продвижение cursor до выдачи. Чтение образы не допускает и не продвигает.
func (server *Server) ListRoleImageRecipes(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListRoleImageRecipesParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	query, state := stringValue(parameters.Query), ""
	if parameters.State != nil {
		state = string(*parameters.State)
	}
	if !validSearchText(query, 0, 128) || len(query) > 128 || state != "" && state != "ACTIVE" && state != "ARCHIVED" ||
		parameters.RoleDefinitionRef != nil && !effectiveCapabilityRef(*parameters.RoleDefinitionRef) ||
		parameters.PageSize != nil && (*parameters.PageSize < 1 || *parameters.PageSize > 100) ||
		parameters.PageToken != nil && !boundedModelText(*parameters.PageToken, 512) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.RoleImages.ListRoleImageRecipes(request.Context(), &controlplanev1.ListRoleImageRecipesRequest{
		ProjectRef: projectRef, RoleDefinitionRef: stringValue(parameters.RoleDefinitionRef),
		Page: page(parameters.PageSize, parameters.PageToken), Query: query, State: state,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeRoleImageRecipePage(writer, response, parameters.PageSize, parameters.PageToken, func(recipe *controlplanev1.RoleImageRecipe) bool {
		return recipe.GetProjectRef() == projectRef
	})
}

// GetRoleImageRecipe requires a returned recipe in the route project, validates lineage/source, builds and optional risk history, then maps detail and admission failure with version ETag. Project mismatch is NOT_FOUND; this endpoint's local check does not independently compare recipeRef to the returned recipe ref.
// GetRoleImageRecipe требует рецепт в проекте маршрута, проверяет lineage/source, builds и необязательную risk history, затем проецирует detail и admission failure с ETag версии. Чужой проект даёт NOT_FOUND; локальная проверка отдельно не сравнивает recipeRef со ссылкой ответа.
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
	writeRoleImageRecipeDetail(writer, response, nil, validImageRiskHistory)
}

// writeRoleImageDetail finishes an already scoped detail after its caller has set the recipe
// ETag. It validates admission-failure metadata against the same recipe/builds, then writes the
// public failure and HTTP200 body, or the existing HTTP502 problem. It neither authorizes a
// resource, mutates the prepared collections nor performs image promotion or admission.
// writeRoleImageDetail завершает уже проверенное представление после установки ETag рецепта
// вызывающим кодом. Проверяет metadata ошибки допуска по тем же рецепту/сборкам и выдаёт
// публичную ошибку и HTTP200 либо прежнюю проблему HTTP502. Полномочия на ресурс не назначает,
// подготовленные коллекции не меняет, продвижение образа и допуск не выполняет.
func writeRoleImageDetail(writer http.ResponseWriter, result generated.RoleImageRecipeDetail, admissionFailure *controlplanev1.RoleImageAdmissionFailure,
	recipe *controlplanev1.RoleImageRecipe, builds []*controlplanev1.ImageBuild,
) {
	failure, validFailure := publicRoleImageAdmissionFailure(admissionFailure, recipe, builds)
	if !validFailure {
		writeLocalProblem(writer, http.StatusBadGateway, invalidUpstreamResponseCode, false)
		return
	}
	result.AdmissionFailure = failure
	writeJSON(writer, http.StatusOK, result)
}

// validRoleImageLineage accepts absent lineage or the exact shipped-baseline no-pin case, otherwise requiring known management/origin, bounded source text and valid configuration/revision pins. It checks metadata consistency, not source signatures or actor authority.
// validRoleImageLineage принимает отсутствие lineage либо точный shipped-baseline без pins, иначе требует известные management/origin, ограниченный source text и допустимые configuration/revision pins. Проверяет metadata, а не подписи исходников и полномочия actor.
func validRoleImageLineage(lineage *controlplanev1.RoleImageManagedLineage) bool {
	if lineage == nil {
		return true
	}
	if !generated.RoleImageManagedLineageManagedBy(lineage.GetManagedBy()).Valid() || !generated.RoleImageManagedLineageOrigin(lineage.GetOrigin()).Valid() ||
		!validSearchText(lineage.GetSourceRef(), 0, 1024) || !validSearchText(lineage.GetSourceRevision(), 0, 256) {
		return false
	}
	if lineage.GetConfigurationRef() == "" && lineage.GetRevisionRef() == "" && lineage.GetRevision() == 0 {
		return lineage.GetManagedBy() == "SHIPPED" && lineage.GetOrigin() == "BASELINE"
	}
	return effectiveCapabilityRef(lineage.GetConfigurationRef()) && effectiveCapabilityRef(lineage.GetRevisionRef()) && validManagedVersion(lineage.GetRevision())
}

// validRoleImageReceipt checks returned project and optional exact recipe ref, lineage/source, build source/configuration reference and optional image risk history, emitting bad gateway on failure. Organization-specific scope binding is added by its separate wrapper; this predicate does not execute supply-chain admission.
// validRoleImageReceipt сверяет проект и необязательную точную ссылку рецепта, lineage/source, source/configuration reference сборки и необязательную risk history, выдавая bad gateway при отказе. Организационную область добавляет отдельная обёртка; predicate supply-chain admission не выполняет.
func validRoleImageReceipt(writer http.ResponseWriter, response *controlplanev1.ManageRoleImageRecipeResponse, projectRef, recipeRef string) bool {
	recipe := response.GetRecipe()
	if artifact := response.GetImageArtifact(); artifact != nil && !validImageRiskHistory(artifact) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return false
	}
	if recipe == nil || recipe.GetProjectRef() != projectRef || recipeRef != "" && recipe.GetRef() != recipeRef ||
		!validRoleImageLineage(recipe.GetManagedLineage()) || !validRoleImageSource(recipe) || !validRoleImageBuildSource(response.GetImageBuild()) || response.GetImageBuild().GetConfigurationRevisionRef() != "" && !effectiveCapabilityRef(response.GetImageBuild().GetConfigurationRevisionRef()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return false
	}
	return true
}

// validRoleImageSource requires an environment, a nonblank Dockerfile when source is available, and no Dockerfile/installation block when unavailable. This visibility consistency check is not Dockerfile safety analysis.
// validRoleImageSource требует environment и непустой Dockerfile при доступном source, а при недоступном — отсутствие Dockerfile и installation block. Это согласованность видимости, а не анализ безопасности Dockerfile.
func validRoleImageSource(recipe *controlplanev1.RoleImageRecipe) bool {
	environment := recipe.GetEnvironment()
	if recipe == nil || environment == nil {
		return false
	}
	if recipe.GetSourceAvailable() {
		return strings.TrimSpace(environment.GetDockerfile()) != ""
	}
	return environment.GetDockerfile() == "" && environment.GetInstallationBlock() == ""
}

// validRoleImageBuildSource permits no optional build, otherwise requires nonblank source only when marked available and an empty Dockerfile when unavailable.
// validRoleImageBuildSource допускает отсутствие необязательной сборки, иначе требует непустой source только при доступности и пустой Dockerfile при недоступности.
func validRoleImageBuildSource(build *controlplanev1.ImageBuild) bool {
	if build == nil {
		return true
	}
	if build.GetSourceAvailable() {
		return strings.TrimSpace(build.GetDockerfile()) != ""
	}
	return build.GetDockerfile() == ""
}

// CreateRoleImageRecipe binds project context and maps environment selection into the dedicated CREATE action with idempotency, then checks the returned receipt before ETag/HTTP201. It does not directly run a build or accept vulnerability risk.
// CreateRoleImageRecipe привязывает проект и преобразует environment selection в отдельный CREATE с идемпотентностью, затем проверяет receipt до ETag/HTTP201. Сборку напрямую не запускает и риск уязвимостей не принимает.
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
	if !validRoleImageReceipt(writer, response, projectRef, "") {
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusCreated, publicRoleImageRecipe(response.GetRecipe()))
}

// UpdateRoleImageRecipe forwards exact project/recipe, editable name/environment and mutation/version pins to the dedicated owner UPDATE, validating receipt before public projection without detaching managed content implicitly.
// UpdateRoleImageRecipe передаёт точные project/recipe, имя, environment и mutation/version pins отдельному UPDATE владельца и проверяет receipt до выдачи без неявного detach управляемого содержимого.
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
	if !validRoleImageReceipt(writer, response, projectRef, recipeRef) {
		return
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeJSON(writer, http.StatusOK, publicRoleImageRecipe(response.GetRecipe()))
}

// CommandRoleImageRecipe maps the selected action/build under project context and mutation pins to the owner, verifies receipt and projects recipe/build plus reused flag. Owner admission, cancellation and promotion transitions are not inferred from display fields.
// CommandRoleImageRecipe преобразует action/build с проектом и mutation pins в запрос владельцу, сверяет receipt и проецирует recipe/build и reused. Допуск, отмена и продвижение владельца из отображаемых полей не выводятся.
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
	buildRef := ""
	if body.BuildRef != nil {
		buildRef = *body.BuildRef
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
		BuildRef: buildRef,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	if !validRoleImageReceipt(writer, response, projectRef, recipeRef) {
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

// roleEnvironmentSelection copies optional package/tool lists and installation text into a fresh typed selection while retaining supplied Dockerfile content unchanged; it performs no execution, catalog lookup or admission.
// roleEnvironmentSelection копирует необязательные package/tool списки и installation text в новый типизированный selection, сохраняя Dockerfile без изменений; выполнение, поиск каталога и допуск не производятся.
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

// publicRoleEnvironment explicitly maps template metadata, availability hints and platform tuples, preserving optional variant/unavailable fields and copying software message keys. It assumes owner-projected inputs rather than validating templates.
// publicRoleEnvironment явно преобразует metadata шаблона, availability hints и platform tuples, сохраняя необязательные variant/unavailable и копируя software message keys. Предполагает проекцию владельца, а не проверяет шаблоны.
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

// publicRoleImageRecipe maps recipe scope/version/lineage and visible environment data, derives promoted readiness from a nonempty reference and preserves optional pins/actions. It omits internal build policy hashes and does not independently validate numeric conversions or ownership.
// publicRoleImageRecipe преобразует scope/version/lineage и видимый environment, выводит promoted readiness из непустой ссылки и сохраняет optional pins/actions. Внутренние build policy hashes не выдаёт, числовые casts и владение отдельно не проверяет.
func publicRoleImageRecipe(input *controlplanev1.RoleImageRecipe) generated.RoleImageRecipe {
	result := generated.RoleImageRecipe{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), ProjectRef: input.GetProjectRef(),
		ScopeKind: runtimeResourceScopeKind(input.GetScopeKind().String()), OrganizationRef: input.GetOrganizationRef(),
		RoleDefinitionRef: input.GetRoleDefinitionRef(), Name: input.GetName(),
		State: generated.RoleImageRecipeState(input.GetState()), Generation: int64(input.GetGeneration()),
		PromotedImageReady: input.GetPromotedImageReference() != "",
		SourceAvailable:    input.GetSourceAvailable(),
		CreatedAt:          protoTime(input.GetCreatedAt()), UpdatedAt: protoTime(input.GetUpdatedAt()),
		NextActions: make([]generated.NextAction, 0, len(input.GetNextActions())),
	}
	if lineage := input.GetManagedLineage(); lineage != nil {
		result.ManagedLineage = &generated.RoleImageManagedLineage{
			ManagedBy: generated.RoleImageManagedLineageManagedBy(lineage.GetManagedBy()), Origin: generated.RoleImageManagedLineageOrigin(lineage.GetOrigin()),
			SourceRef: lineage.GetSourceRef(), SourceRevision: lineage.GetSourceRevision(),
			ConfigurationRef: optionalManagedString(lineage.GetConfigurationRef()), RevisionRef: optionalManagedString(lineage.GetRevisionRef()),
		}
		if lineage.GetRevision() != 0 {
			revision := lineage.GetRevision()
			result.ManagedLineage.Revision = &revision
		}
	}
	if value := input.GetActiveImageArtifactRef(); value != "" {
		result.ActiveImageArtifactRef = &value
	}
	if value := input.GetPromotedImageReference(); value != "" {
		result.PromotedImageReference = &value
	}
	if environment := input.GetEnvironment(); environment != nil {
		result.Environment = generated.RoleEnvironmentView{EnvironmentKey: environment.GetEnvironmentKey(), Dockerfile: optionalManagedString(environment.GetDockerfile())}
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

// publicRoleImageBuild maps public build scope, generation, stage/progress, source availability, configuration pin and optional safe diagnostics. It does not verify the build digest or sanitize arbitrary input diagnostics by itself.
// publicRoleImageBuild преобразует scope, generation, stage/progress, доступность source, configuration pin и optional safe diagnostics. Digest сборки не проверяет и произвольную диагностику сам не очищает.
func publicRoleImageBuild(input *controlplanev1.ImageBuild) generated.RoleImageBuild {
	result := generated.RoleImageBuild{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), RecipeRef: input.GetRecipeRef(),
		ScopeKind: runtimeResourceScopeKind(input.GetScopeKind().String()), OrganizationRef: input.GetOrganizationRef(),
		RecipeGeneration: int64(input.GetRecipeGeneration()), Dockerfile: optionalManagedString(input.GetDockerfile()), SourceAvailable: input.GetSourceAvailable(),
		ProjectRef: input.GetProjectRef(),
		Attempt:    int(input.GetAttempt()), Stage: generated.RoleImageBuildStage(strings.TrimPrefix(input.GetStage().String(), "IMAGE_BUILD_STAGE_")),
		ProgressPercent: int(input.GetProgressPercent()), CreatedAt: protoTime(input.GetCreatedAt()), UpdatedAt: protoTime(input.GetUpdatedAt()),
		ConfigurationRevisionRef: optionalManagedString(input.GetConfigurationRevisionRef()),
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

// publicRoleImageArtifact maps admitted-artifact display metadata, promotion/SBOM/evidence references, declared/observed tools and convertible optional risk records. An optional risk conversion failure is omitted here; callers must validate history before mapping, and display data is not admission authority.
// publicRoleImageArtifact преобразует metadata файла образа, promotion/SBOM/evidence ссылки, declared/observed tools и преобразуемые optional risk records. Ошибочный optional risk conversion здесь пропускается; историю заранее проверяет caller, отображение не является полномочием допуска.
func publicRoleImageArtifact(input *controlplanev1.ImageArtifact) generated.RoleImageArtifact {
	result := generated.RoleImageArtifact{
		Ref: input.GetRef(), Version: int64(input.GetVersion()), RecipeRef: input.GetRecipeRef(),
		ScopeKind: runtimeResourceScopeKind(input.GetScopeKind().String()), OrganizationRef: input.GetOrganizationRef(),
		RecipeGeneration: int64(input.GetRecipeGeneration()), BuildRef: input.GetBuildRef(), ManifestDigest: input.GetManifestDigest(),
		ProjectRef:         input.GetProjectRef(),
		ProvenanceSha256:   input.GetProvenanceSha256(),
		AdmissionVerdict:   generated.RoleImageArtifactAdmissionVerdict(strings.TrimPrefix(input.GetAdmissionVerdict().String(), "IMAGE_ADMISSION_VERDICT_")),
		PromotionState:     generated.RoleImageArtifactPromotionState(strings.TrimPrefix(input.GetPromotionState().String(), "IMAGE_PROMOTION_STATE_")),
		PromotionRequested: input.GetPromotionRequested(),
		DeclaredTools:      make([]generated.RoleImageArtifactTool, 0, len(input.GetDeclaredTools())),
	}
	if value := input.GetPromotedReference(); value != "" {
		result.PromotedReference = &value
	}
	if value := input.GetPromotionReadbackSha256(); value != "" {
		result.PromotionReceiptSha256 = &value
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
	for _, tool := range input.GetDeclaredTools() {
		result.DeclaredTools = append(result.DeclaredTools, generated.RoleImageArtifactTool{Name: tool.GetName(), Version: tool.GetVersion()})
	}
	result.VerifiedToolInventory = publicImageToolInventory(input.GetVerifiedToolInventory())
	if input.GetAdmissionAttempt() != nil {
		value, ok := imageRiskPublic[generated.ImageAdmissionAttempt](input.GetAdmissionAttempt())
		if ok {
			result.AdmissionAttempt = &value
		}
	}
	if input.GetRiskDecision() != nil {
		value, ok := imageRiskPublic[generated.ImageAdmissionRiskDecision](input.GetRiskDecision())
		if ok {
			result.RiskDecision = &value
		}
	}
	return result
}

// publicImageToolInventory returns explicit UNAVAILABLE with an empty platform list unless owner metadata says VERIFIED, then copies digests/platform/tool observations into the public shape. This mapper does not itself verify signatures, digests, tool execution or readiness.
// publicImageToolInventory выдаёт явный UNAVAILABLE с пустыми платформами, если metadata владельца не VERIFIED, иначе копирует digests и наблюдения platform/tool. Сам mapper подписи, digests, выполнение инструментов и readiness не проверяет.
func publicImageToolInventory(input *controlplanev1.ImageToolInventory) generated.ImageToolInventory {
	result := generated.ImageToolInventory{Status: generated.ImageToolInventoryStatusUNAVAILABLE, Platforms: []generated.ImagePlatformToolInventory{}}
	if input == nil || input.GetStatus() != "VERIFIED" {
		return result
	}
	result.Status, result.Sha256, result.ImageDigest, result.ProvenanceSha256 = generated.ImageToolInventoryStatusVERIFIED, input.GetSha256(), input.GetImageDigest(), input.GetProvenanceSha256()
	for _, platform := range input.GetPlatforms() {
		item := generated.ImagePlatformToolInventory{Platform: generated.ImagePlatformToolInventoryPlatform(platform.GetPlatform()), PlatformDigest: platform.GetPlatformDigest(), ManifestSha256: platform.GetManifestSha256(), Tools: []generated.ImageToolObservation{}}
		for _, tool := range platform.GetTools() {
			item.Tools = append(item.Tools, generated.ImageToolObservation{Name: generated.ImageToolObservationName(tool.GetName()), Status: generated.ImageToolObservationStatus(tool.GetStatus()), Path: tool.GetPath(), Version: tool.GetVersion(), Sha256: tool.GetSha256(), Required: tool.GetRequired()})
		}
		result.Platforms = append(result.Platforms, item)
	}
	return result
}

// roleImageRecipeAction performs exact prefixed action enum lookup and reports whether the name exists; it does not apply action-specific permission or lifecycle policy.
// roleImageRecipeAction точно ищет action enum с префиксом и сообщает наличие имени; права и lifecycle конкретного действия не применяет.
func roleImageRecipeAction(input generated.RoleImageRecipeCommandAction) (controlplanev1.RoleImageRecipeAction, bool) {
	value, ok := controlplanev1.RoleImageRecipeAction_value["ROLE_IMAGE_RECIPE_ACTION_"+string(input)]
	return controlplanev1.RoleImageRecipeAction(value), ok
}

// protoTime returns zero for a nil interface, otherwise delegates AsTime and converts to UTC without CheckValid. Typed-nil receiver behavior remains that of the supplied implementation rather than a guaranteed zero result.
// protoTime возвращает ноль для nil interface, иначе вызывает AsTime и приводит к UTC без CheckValid. Поведение typed-nil определяется реализацией получателя, а не гарантированным нулём.
func protoTime(input interface{ AsTime() time.Time }) time.Time {
	if input == nil {
		return time.Time{}
	}
	return input.AsTime().UTC()
}

// setVersionETag writes a quoted decimal ETag only for positive uint64 versions, leaving any existing header untouched for zero; it does not validate domain version bounds.
// setVersionETag записывает десятичный ETag в кавычках только положительной uint64-версии, не меняя прежний заголовок для нуля; доменные пределы версии не проверяет.
func setVersionETag(writer http.ResponseWriter, version uint64) {
	if version > 0 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", version))
	}
}
