package grpc

import (
	"context"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// RoleImageServer exposes specialized image supply-chain RPCs over one domain service; transport
// principal resolution precedes every operation and does not delegate authority to request fields.
// RoleImageServer предоставляет специализированные RPC цепочки образов через один доменный сервис;
// разрешение principal предшествует каждой операции и не передаёт полномочия полям запроса.
type RoleImageServer struct {
	controlplanev1.UnimplementedRoleImageServiceServer
	service *roleimageservice.Service
}

// NewRoleImageServer rejects a nil domain service and constructs only the RPC adapter; it starts no
// listener or background work.
// NewRoleImageServer отклоняет nil доменного сервиса и создаёт только RPC-адаптер; listener и фоновую
// работу не запускает.
func NewRoleImageServer(service *roleimageservice.Service) (*RoleImageServer, error) {
	if service == nil {
		return nil, errors.New("role image service is required")
	}
	return &RoleImageServer{service: service}, nil
}

// castRoleImageRecipeInput copies source/build pins and selected keys into protobuf, allocating
// ordered platform/package/tool entries. It does not resolve references, check digests or admit the
// source as executable.
// castRoleImageRecipeInput копирует привязки исходника и сборки и выбранные ключи в protobuf, создавая
// упорядоченные записи платформ, пакетов и инструментов. Ссылки не разрешает, хеши не проверяет и
// исходник к исполнению не допускает.
func castRoleImageRecipeInput(input entity.RoleImageRecipeInput) *controlplanev1.RoleImageRecipeInput {
	result := &controlplanev1.RoleImageRecipeInput{
		BaseImageReference: input.BaseImageReference,
		BaseImageDigest:    input.BaseImageDigest,
		SourceRef:          input.SourceRef,
		SourceRevision:     input.SourceRevision,
		SourceSha256:       input.SourceSHA256,
		ContextRef:         input.ContextRef,
		ContextSha256:      input.ContextSHA256,
		BuilderSha256:      input.BuilderSHA256,
		FrontendSha256:     input.FrontendSHA256,
		InstallationBlock:  input.InstallationBlock,
		ToolchainSha256:    input.ToolchainSHA256,
		EnvironmentKey:     input.EnvironmentKey,
		PackageKeys:        append([]string(nil), input.PackageKeys...),
		ToolKeys:           append([]string(nil), input.ToolKeys...),
		Dockerfile:         input.Dockerfile,
	}
	for _, item := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	for _, item := range input.Packages {
		result.Packages = append(result.Packages, &controlplanev1.RoleImagePackage{
			Manager: item.Manager, Name: item.Name, Version: item.Version,
			Digest: item.Digest, SourceRef: item.SourceRef,
		})
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RoleImageTool{
			Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, Sha256: item.SHA256,
		})
	}
	return result
}

// castRoleImageRecipe projects domain recipe metadata, optional managed lineage and selected
// environment source into protobuf with copied key/action slices. Source eligibility and redaction
// belong to the domain read, not this caster.
// castRoleImageRecipe переносит метаданные рецепта, необязательное управляемое происхождение и
// выбранный исходник окружения в protobuf с копиями ключей и действий. Допустимость исходника и
// скрытие данных принадлежат доменному чтению, не caster.
func castRoleImageRecipe(input entity.RoleImageRecipe) *controlplanev1.RoleImageRecipe {
	var lineage *controlplanev1.RoleImageManagedLineage
	if input.ManagedLineage != nil {
		value := input.ManagedLineage
		lineage = &controlplanev1.RoleImageManagedLineage{ConfigurationRef: value.ConfigurationRef, RevisionRef: value.RevisionRef, Revision: value.Revision, ManagedBy: value.ManagedBy, SourceRef: value.SourceRef, SourceRevision: value.SourceRevision, Origin: value.Origin}
	}
	return &controlplanev1.RoleImageRecipe{
		ScopeKind: roleImageScopeKind(input.ScopeKind), OrganizationRef: input.OrganizationRef,
		SourceAvailable: input.SourceAvailable,
		ManagedLineage:  lineage,
		Ref:             input.Ref, Version: input.Version, ProjectRef: input.ProjectRef, RoleDefinitionRef: input.RoleDefinitionRef,
		Name: input.Name, State: input.State,
		Generation: input.Generation, SpecSha256: input.SpecSHA256,
		PolicyRevision: input.PolicyRevision, PolicySha256: input.PolicySHA256,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:   input.RoleRuntimeContractSHA256,
		ActiveImageArtifactRef:      input.ActiveImageArtifactRef,
		PromotedImageReference:      input.PromotedImageReference,
		CreatedAt:                   timestamp(input.CreatedAt), UpdatedAt: timestamp(input.UpdatedAt),
		NextActions: append([]string(nil), input.NextActions...),
		Environment: &controlplanev1.RoleEnvironmentSelection{
			EnvironmentKey:    input.Input.EnvironmentKey,
			PackageKeys:       append([]string(nil), input.Input.PackageKeys...),
			ToolKeys:          append([]string(nil), input.Input.ToolKeys...),
			InstallationBlock: input.Input.InstallationBlock,
			Dockerfile:        input.Input.Dockerfile,
		},
	}
}

// castRoleEnvironment projects catalog availability, localization keys and template metadata, copying
// software keys and platform entries without deciding catalog admission.
// castRoleEnvironment переносит доступность каталога, ключи локализации и метаданные шаблона, копируя
// ключи программ и платформы без решения о допуске каталога.
func castRoleEnvironment(input roleimageservice.Environment) *controlplanev1.RoleEnvironment {
	result := &controlplanev1.RoleEnvironment{
		Key: input.Key, NameMessageKey: input.NameMessageKey,
		DescriptionMessageKey: input.DescriptionMessageKey,
		SoftwareMessageKeys:   append([]string(nil), input.SoftwareMessageKeys...),
		Recommended:           input.Recommended, Available: input.Available,
		UnavailableMessageKey:     input.UnavailableMessageKey,
		CustomInstallationAllowed: input.CustomInstallationAllowed,
		DockerfileTemplate:        input.DockerfileTemplate,
	}
	for _, item := range input.Input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	return result
}

// domainRoleEnvironmentSelection maps nil to an empty selection or copies selected package/tool keys
// and installation source; it does not validate that the selection is allowed.
// domainRoleEnvironmentSelection превращает nil в пустой выбор либо копирует ключи пакетов и
// инструментов и исходник установки; допустимость выбора не проверяет.
func domainRoleEnvironmentSelection(input *controlplanev1.RoleEnvironmentSelection) entity.RoleEnvironmentSelection {
	if input == nil {
		return entity.RoleEnvironmentSelection{}
	}
	return entity.RoleEnvironmentSelection{
		EnvironmentKey:    input.GetEnvironmentKey(),
		PackageKeys:       append([]string(nil), input.GetPackageKeys()...),
		ToolKeys:          append([]string(nil), input.GetToolKeys()...),
		InstallationBlock: input.GetInstallationBlock(),
		Dockerfile:        input.GetDockerfile(),
	}
}

// imageBuildStage maps an exact stage suffix through the generated enum table, returning UNSPECIFIED
// for unknown values instead of inventing a live or terminal state.
// imageBuildStage переводит точный суффикс этапа через generated enum table, возвращая UNSPECIFIED для
// неизвестного значения вместо придуманного активного или конечного состояния.
func imageBuildStage(stage string) controlplanev1.ImageBuildStage {
	value, exists := controlplanev1.ImageBuildStage_value["IMAGE_BUILD_STAGE_"+stage]
	if !exists {
		return controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED
	}
	return controlplanev1.ImageBuildStage(value)
}

// castImageBuild projects build identity, version/attempt/fence, stage/progress, immutable pins,
// diagnostics and optional lease expiry without renewing or validating the lease.
// castImageBuild переносит личность сборки, версию, попытку, fence, этап, прогресс, неизменяемые
// привязки, диагностику и необязательный срок lease, не продлевая и не проверяя lease.
func castImageBuild(input entity.ImageBuild) *controlplanev1.ImageBuild {
	result := &controlplanev1.ImageBuild{
		ProjectRef: input.ProjectRef,
		ScopeKind:  roleImageScopeKind(input.ScopeKind), OrganizationRef: input.OrganizationRef,
		SourceAvailable:          input.SourceAvailable,
		ConfigurationRevisionRef: input.ConfigurationRevisionRef,
		Ref:                      input.Ref, Version: input.Version, RecipeRef: input.RecipeRef,
		RecipeVersion: input.RecipeVersion, RecipeGeneration: input.RecipeGeneration,
		SpecSha256: input.SpecSHA256, Attempt: input.Attempt, Fence: input.Fence,
		Stage: imageBuildStage(input.Stage), ProgressPercent: input.ProgressPercent,
		StagingReference: input.StagingReference, ManifestDigest: input.ManifestDigest,
		ProvenanceSha256: input.ProvenanceSHA256, ImmutableBuildSha256: input.ImmutableBuildSHA256,
		SafeErrorCode: input.SafeErrorCode, DiagnosticCode: input.DiagnosticCode,
		DiagnosticSummary: input.DiagnosticSummary, CreatedAt: timestamp(input.CreatedAt),
		UpdatedAt: timestamp(input.UpdatedAt), Dockerfile: input.Dockerfile,
	}
	if input.LeaseExpiresAt != nil {
		result.LeaseExpiresAt = timestamp(*input.LeaseExpiresAt)
	}
	return result
}

// castRoleImageAdmissionFailure preserves nil or projects the exact failed attempt's
// artifact/recipe/build and scope/version/error fields; it does not convert failure into an admission
// verdict.
// castRoleImageAdmissionFailure сохраняет nil либо переносит точные поля артефакта, рецепта, сборки,
// области, версии и ошибки неудавшейся попытки; ошибку в вердикт допуска не превращает.
func castRoleImageAdmissionFailure(input *entity.RoleImageAdmissionFailure) *controlplanev1.RoleImageAdmissionFailure {
	if input == nil {
		return nil
	}
	return &controlplanev1.RoleImageAdmissionFailure{ImageArtifactRef: input.ImageArtifactRef,
		Version: input.Version, RecipeRef: input.RecipeRef, RecipeGeneration: input.RecipeGeneration,
		BuildRef: input.BuildRef, BuildAttempt: input.BuildAttempt, ScopeKind: roleImageScopeKind(input.ScopeKind),
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, State: input.State, ErrorCode: input.ErrorCode}
}

// imageAdmissionVerdict resolves the exact verdict suffix through generated values and returns
// UNSPECIFIED for unknown input; absence is not CLEAN or accepted.
// imageAdmissionVerdict разрешает точный суффикс вердикта через generated values и возвращает
// UNSPECIFIED для неизвестного ввода; отсутствие не означает CLEAN или принятие.
func imageAdmissionVerdict(verdict string) controlplanev1.ImageAdmissionVerdict {
	value, exists := controlplanev1.ImageAdmissionVerdict_value["IMAGE_ADMISSION_VERDICT_"+verdict]
	if !exists {
		return controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_UNSPECIFIED
	}
	return controlplanev1.ImageAdmissionVerdict(value)
}

// imagePromotionState resolves a promotion-state suffix through the generated table with an
// UNSPECIFIED fallback, without performing a promotion transition.
// imagePromotionState разрешает суффикс состояния продвижения через generated table с запасным
// UNSPECIFIED, не выполняя переход продвижения.
func imagePromotionState(state string) controlplanev1.ImagePromotionState {
	value, exists := controlplanev1.ImagePromotionState_value["IMAGE_PROMOTION_STATE_"+state]
	if !exists {
		return controlplanev1.ImagePromotionState_IMAGE_PROMOTION_STATE_UNSPECIFIED
	}
	return controlplanev1.ImagePromotionState(value)
}

// castImageArtifact projects immutable provenance/admission/promotion pins, platforms, tools and
// optional evidence views. An empty verdict becomes PENDING only for a pending/claimed admission
// attempt; the caster does not validate evidence, sign or promote an image.
// castImageArtifact переносит неизменяемые привязки происхождения, допуска и продвижения, платформы,
// инструменты и необязательные представления свидетельств. Пустой вердикт становится PENDING только
// для ожидающей либо захваченной попытки допуска; caster свидетельства не проверяет, образ не
// подписывает и не продвигает.
func castImageArtifact(input entity.ImageArtifact) *controlplanev1.ImageArtifact {
	result := &controlplanev1.ImageArtifact{
		ProjectRef: input.ProjectRef,
		ScopeKind:  roleImageScopeKind(input.ScopeKind), OrganizationRef: input.OrganizationRef,
		Ref: input.Ref, Version: input.Version, RecipeRef: input.RecipeRef,
		RecipeVersion: input.RecipeVersion, RecipeGeneration: input.RecipeGeneration,
		SpecSha256: input.SpecSHA256, BuildRef: input.BuildRef, BuildVersion: input.BuildVersion,
		BuildAttempt: input.BuildAttempt, StagingReference: input.StagingReference,
		ManifestDigest: input.ManifestDigest, ImmutableBuildSha256: input.ImmutableBuildSHA256,
		ProvenanceSha256: input.ProvenanceSHA256, BaseImageDigest: input.BaseImageDigest,
		SourceSha256: input.SourceSHA256, ContextSha256: input.ContextSHA256,
		BuilderSha256: input.BuilderSHA256, FrontendSha256: input.FrontendSHA256,
		ToolchainSha256: input.ToolchainSHA256, PolicyRevision: input.PolicyRevision,
		PolicySha256: input.PolicySHA256, SbomSha256: input.SBOMSHA256,
		VulnerabilityEvidenceSha256: input.VulnerabilityEvidenceSHA256,
		AdmissionVerdict:            imageAdmissionVerdict(input.AdmissionVerdict),
		PromotionState:              imagePromotionState(input.PromotionState),
		PromotionRequested:          input.PromotionRequested,
		SignatureIdentity:           input.SignatureIdentity, SignatureSha256: input.SignatureSHA256,
		AdmissionRevision:                 input.AdmissionRevision,
		AdmissionReceiptSha256:            input.AdmissionReceiptSHA256,
		AdmissionReceiptOciManifestDigest: input.AdmissionReceiptOCIManifestDigest,
		PromotedReference:                 input.PromotedReference,
		PromotionReadbackSha256:           input.PromotionReadbackSHA256,
		RoleRuntimeContractRevision:       input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:         input.RoleRuntimeContractSHA256,
		CreatedAt:                         timestamp(input.CreatedAt), UpdatedAt: timestamp(input.UpdatedAt),
	}
	if input.PromotedAt != nil {
		result.PromotedAt = timestamp(*input.PromotedAt)
	}
	for _, item := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	for _, item := range input.DeclaredTools {
		result.DeclaredTools = append(result.DeclaredTools, &controlplanev1.RoleImageTool{Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, Sha256: item.SHA256})
	}
	result.VerifiedToolInventory = castImageToolInventory(input.ToolInventory, input.ToolInventorySHA256)
	result.AdmissionAttempt = castImageAdmissionAttempt(input.AdmissionAttempt)
	if input.AdmissionVerdict == "" && input.AdmissionAttempt != nil && (input.AdmissionAttempt.State == "PENDING" || input.AdmissionAttempt.State == "CLAIMED") {
		result.AdmissionVerdict = controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_PENDING
	}
	result.RiskDecision = castImageRiskDecision(input.RiskDecision)
	return result
}

// castRoleImageBuildInput maps the domain-owned build snapshot and exact source/policy/runtime pins,
// reusing existing recipe-input casters for platform/package/tool lists; it does not recompute their
// digests.
// castRoleImageBuildInput переносит принадлежащий домену снимок сборки и точные привязки исходника,
// политики и runtime, переиспользуя прежние recipe-input casters для платформ, пакетов и инструментов;
// хеши не пересчитывает.
func castRoleImageBuildInput(input entity.RoleImageBuildInput) *controlplanev1.RoleImageBuildInput {
	return &controlplanev1.RoleImageBuildInput{
		ProjectRef: input.ProjectRef,
		ScopeKind:  roleImageScopeKind(input.ScopeKind), OrganizationRef: input.OrganizationRef,
		RecipeRef: input.RecipeRef, RecipeVersion: input.RecipeVersion,
		RecipeGeneration: input.RecipeGeneration, SpecSha256: input.SpecSHA256,
		BaseImageReference: input.BaseImageReference, BaseImageDigest: input.BaseImageDigest,
		SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, SourceSha256: input.SourceSHA256,
		ContextRef: input.ContextRef, ContextSha256: input.ContextSHA256,
		BuilderSha256: input.BuilderSHA256, FrontendSha256: input.FrontendSHA256,
		Platforms:         castRoleImageRecipeInput(entity.RoleImageRecipeInput{Platforms: input.Platforms}).GetPlatforms(),
		Packages:          castRoleImageRecipeInput(entity.RoleImageRecipeInput{Packages: input.Packages}).GetPackages(),
		Tools:             castRoleImageRecipeInput(entity.RoleImageRecipeInput{Tools: input.Tools}).GetTools(),
		InstallationBlock: input.InstallationBlock, ToolchainSha256: input.ToolchainSHA256,
		PolicyRevision: input.PolicyRevision, PolicySha256: input.PolicySHA256,
		ImmutableBuildSha256:        input.ImmutableBuildSHA256,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:   input.RoleRuntimeContractSHA256,
		Dockerfile:                  input.Dockerfile,
	}
}

// roleImagePrincipal delegates context-derived principal and exact full-method checking to the shared
// transport boundary; it never builds a principal from image request fields.
// roleImagePrincipal передаёт получение principal из контекста и проверку точного полного метода общей
// транспортной границе; из полей запроса образа principal не создаёт.
func roleImagePrincipal(ctx context.Context, method string) (value.Principal, error) {
	return principal(ctx, method)
}

// roleImageLeaseInput assembles the resolved principal with exact build/version/attempt/fence/token
// and idempotency inputs; domain code still validates ownership and active lease binding.
// roleImageLeaseInput собирает разрешённый principal с точными вводами сборки, версии, попытки, fence,
// токена и идемпотентности; владение и действующую привязку lease по-прежнему проверяет домен.
func roleImageLeaseInput(principal value.Principal, key, ref, token string, version, fence uint64, attempt uint32) roleimagerepository.BuildLeaseInput {
	return roleimagerepository.BuildLeaseInput{
		Principal: principal, IdempotencyKey: key, BuildRef: ref, LeaseToken: token,
		ExpectedVersion: version, ExpectedAttempt: attempt, ExpectedFence: fence,
	}
}

// roleImageAction strips the generated recipe-action prefix only; it does not admit unknown numeric
// enum values as supported domain actions.
// roleImageAction лишь удаляет generated prefix действия рецепта; неизвестные числовые enum в
// разрешённые доменные действия не превращает.
func roleImageAction(action controlplanev1.RoleImageRecipeAction) string {
	return strings.TrimPrefix(action.String(), "ROLE_IMAGE_RECIPE_ACTION_")
}

// ListRoleEnvironments resolves the exact RPC principal, asks the domain for its catalog view and
// preserves the returned environment order in protobuf.
// ListRoleEnvironments разрешает principal точного RPC, запрашивает доменное представление каталога и
// сохраняет порядок окружений в protobuf.
func (server *RoleImageServer) ListRoleEnvironments(ctx context.Context, _ *controlplanev1.ListRoleEnvironmentsRequest) (*controlplanev1.ListRoleEnvironmentsResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListRoleEnvironments_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListEnvironments(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleEnvironmentsResponse{}
	for _, item := range items {
		response.Environments = append(response.Environments, castRoleEnvironment(item))
	}
	return response, nil
}

// ListRoleImageRecipes resolves the exact project-list principal and forwards
// project/role/query/state/page filters to the domain before mapping recipes, cursor and total.
// ListRoleImageRecipes разрешает principal точного проектного списка и передаёт домену фильтры
// проекта, роли, запроса, состояния и страницы до преобразования рецептов, курсора и количества.
func (server *RoleImageServer) ListRoleImageRecipes(ctx context.Context, request *controlplanev1.ListRoleImageRecipesRequest) (*controlplanev1.ListRoleImageRecipesResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListRoleImageRecipes_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, nextPage, total, err := server.service.List(ctx, p, roleimagerepository.Filter{
		ProjectRef: request.GetProjectRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Page: page(request.GetPage()), Query: request.GetQuery(), State: request.GetState(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleImageRecipesResponse{Page: &controlplanev1.PageInfo{NextPageToken: nextPage}, Total: total}
	for _, item := range items {
		response.Recipes = append(response.Recipes, castRoleImageRecipe(item))
	}
	return response, nil
}

// GetRoleImageRecipe keeps the exact project-read principal and domain Get call, then maps the already
// resolved detail into the project-specific protobuf response.
// GetRoleImageRecipe сохраняет principal точного проектного чтения и доменный Get, затем преобразует
// уже разрешённые данные в проектный protobuf-ответ.
func (server *RoleImageServer) GetRoleImageRecipe(ctx context.Context, request *controlplanev1.GetRoleImageRecipeRequest) (*controlplanev1.GetRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_GetRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	detail, err := server.service.Get(ctx, p, request.GetRecipeRef())
	if err != nil {
		return nil, transportError(err)
	}
	response := castRoleImageRecipeDetail(detail)
	return &controlplanev1.GetRoleImageRecipeResponse{
		Recipe: response.Recipe, AdmissionFailure: response.AdmissionFailure,
		Builds: response.Builds, ActiveArtifact: response.ActiveArtifact, PromotionCandidate: response.PromotionCandidate,
	}, nil
}

// ManageRoleImageRecipe passes the exact project-management principal and typed
// mutation/action/selection to the domain, returning optional build/artifact and reuse metadata
// without performing generic transport-owned state changes.
// ManageRoleImageRecipe передаёт домену principal точного управления проектом и типизированные
// изменение, действие и выбор, возвращая необязательные сборку, артефакт и признак повтора без
// универсальных изменений состояния силами транспорта.
func (server *RoleImageServer) ManageRoleImageRecipe(ctx context.Context, request *controlplanev1.ManageRoleImageRecipeRequest) (*controlplanev1.ManageRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ManageRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Manage(ctx, roleimagerepository.ManageInput{
		Principal: p, Mutation: mutation(request.GetMutation()), Action: roleImageAction(request.GetAction()),
		RecipeRef: request.GetRecipeRef(), ProjectRef: request.GetProjectRef(), BuildRef: request.GetBuildRef(),
		RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(),
		Environment: domainRoleEnvironmentSelection(request.GetEnvironment()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ManageRoleImageRecipeResponse{Recipe: castRoleImageRecipe(result.Recipe), Reused: result.Reused}
	if result.Build != nil {
		response.ImageBuild = castImageBuild(*result.Build)
	}
	if result.Artifact != nil {
		response.ImageArtifact = castImageArtifact(*result.Artifact)
	}
	return response, nil
}

// ClaimImageBuild uses the exact worker operation principal and idempotency key, returning only the
// domain's claimed build/input, token, fence, authority generation and expiry.
// ClaimImageBuild использует principal точной операции worker и ключ идемпотентности, возвращая лишь
// захваченные доменом сборку, ввод, токен, fence, поколение полномочий и срок.
func (server *RoleImageServer) ClaimImageBuild(ctx context.Context, request *controlplanev1.ClaimImageBuildRequest) (*controlplanev1.ClaimImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimBuild(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImageBuildResponse{
		ImageBuild: castImageBuild(claim.Build), Input: castRoleImageBuildInput(claim.Input),
		LeaseToken: claim.LeaseToken, Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		LeaseExpiresAt: timestamp(claim.LeaseExpiresAt),
	}, nil
}

// RenewImageBuild submits the exact resolved-principal/build/version/attempt/fence/token tuple to the
// domain and maps its renewal result; request fields cannot mint a lease in transport.
// RenewImageBuild передаёт домену точный набор разрешённого principal, сборки, версии, попытки, fence
// и токена и переносит результат продления; поля запроса не выпускают lease в транспорте.
func (server *RoleImageServer) RenewImageBuild(ctx context.Context, request *controlplanev1.RenewImageBuildRequest) (*controlplanev1.RenewImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_RenewImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.RenewBuild(ctx, roleImageLeaseInput(p, request.GetIdempotencyKey(),
		request.GetImageBuildRef(), request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()))
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RenewImageBuildResponse{
		ImageBuild: castImageBuild(claim.Build), LeaseToken: claim.LeaseToken,
		LeaseExpiresAt: timestamp(claim.LeaseExpiresAt),
	}, nil
}

// ReportImageBuildProgress combines the verified RPC principal and exact lease tuple with
// stage/progress, leaving stage legality, concurrency and persistence to the domain.
// ReportImageBuildProgress соединяет проверенный principal RPC и точный набор lease с этапом и
// прогрессом, оставляя допустимость этапа, конкуренцию и сохранение домену.
func (server *RoleImageServer) ReportImageBuildProgress(ctx context.Context, request *controlplanev1.ReportImageBuildProgressRequest) (*controlplanev1.ReportImageBuildProgressResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ReportImageBuildProgress_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := roleimagerepository.BuildProgressInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		Stage:           strings.TrimPrefix(request.GetStage().String(), "IMAGE_BUILD_STAGE_"),
		ProgressPercent: request.GetProgressPercent(),
	}
	build, err := server.service.ReportBuildProgress(ctx, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ReportImageBuildProgressResponse{ImageBuild: castImageBuild(build)}, nil
}

// CompleteImageBuild forwards exact lease and staging/manifest/provenance/immutable-build pins to the
// completion command and maps its build/artifact result; build completion alone is not admission or
// promotion.
// CompleteImageBuild передаёт точные lease и привязки staging, manifest, происхождения и неизменяемой
// сборки команде завершения и переносит сборку с артефактом; завершение сборки само по себе не допуск
// и не продвижение.
func (server *RoleImageServer) CompleteImageBuild(ctx context.Context, request *controlplanev1.CompleteImageBuildRequest) (*controlplanev1.CompleteImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_CompleteImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	build, artifact, err := server.service.CompleteBuild(ctx, roleimagerepository.BuildCompletionInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		StagingReference: request.GetStagingReference(), ManifestDigest: request.GetManifestDigest(),
		ProvenanceSHA256: request.GetProvenanceSha256(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteImageBuildResponse{ImageBuild: castImageBuild(build), ImageArtifact: castImageArtifact(artifact)}, nil
}

// FailImageBuild binds the failure request to the resolved principal and exact build lease before
// delegating safe error/diagnostic fields to the domain failure command.
// FailImageBuild связывает запрос ошибки с разрешённым principal и точным lease сборки до передачи
// безопасных кодов и диагностики доменной команде отказа.
func (server *RoleImageServer) FailImageBuild(ctx context.Context, request *controlplanev1.FailImageBuildRequest) (*controlplanev1.FailImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_FailImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	build, err := server.service.FailBuild(ctx, roleimagerepository.BuildFailureInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		ErrorCode: request.GetErrorCode(), DiagnosticCode: request.GetDiagnosticCode(), DiagnosticSummary: request.GetDiagnosticSummary(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.FailImageBuildResponse{ImageBuild: castImageBuild(build)}, nil
}

// ClaimImageAdmission maps the domain-issued artifact claim, exact attempt, fence/authority
// generation, risk/source-evidence pins and expiry without accepting request-supplied ownership.
// ClaimImageAdmission переносит выданные доменом захват артефакта, точную попытку, fence, поколение
// полномочий, привязки риска и исходных свидетельств и срок без принятия владения из запроса.
func (server *RoleImageServer) ClaimImageAdmission(ctx context.Context, request *controlplanev1.ClaimImageAdmissionRequest) (*controlplanev1.ClaimImageAdmissionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImageAdmission_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimAdmission(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImageAdmissionResponse{
		ImageArtifact: castImageArtifact(claim.Artifact), ClaimToken: claim.ClaimToken,
		Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		ClaimExpiresAt:      timestamp(claim.ClaimExpiresAt),
		AdmissionAttemptRef: claim.AdmissionAttemptRef, AdmissionAttempt: claim.AdmissionAttempt,
		RiskAcceptanceJson: claim.RiskAcceptanceJSON, RiskAcceptanceSha256: claim.RiskAcceptanceSHA256,
		SourceAdmissionReceiptSha256: claim.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: claim.SourceEvidenceManifestDigest,
		SourceAdmissionRevision: claim.SourceAdmissionRevision,
	}, nil
}

// GetImageSupplyWorkAvailability resolves its exact operation principal and projects the domain's
// admission/promotion availability flags; this transport method does not issue a claim token.
// GetImageSupplyWorkAvailability разрешает principal точной операции и переносит доменные признаки
// доступности допуска и продвижения; транспортный метод не выдаёт claim token.
func (server *RoleImageServer) GetImageSupplyWorkAvailability(ctx context.Context, _ *controlplanev1.GetImageSupplyWorkAvailabilityRequest) (*controlplanev1.GetImageSupplyWorkAvailabilityResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_GetImageSupplyWorkAvailability_FullMethodName)
	if err != nil {
		return nil, err
	}
	availability, err := server.service.GetSupplyWorkAvailability(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetImageSupplyWorkAvailabilityResponse{
		AdmissionAvailable: availability.AdmissionAvailable,
		PromotionAvailable: availability.PromotionAvailable,
	}, nil
}

// RecordImageAdmission forwards the exact principal/claim/version/fence/attempt and immutable
// evidence, policy, inventory, vulnerability/risk and signature pins to the specialized domain record
// command; transport mapping does not itself accept evidence.
// RecordImageAdmission передаёт точные principal, claim, версию, fence, попытку и неизменяемые
// привязки свидетельств, политики, инструментов, уязвимостей, риска и подписи специализированной
// доменной команде; транспортное преобразование само свидетельства не принимает.
func (server *RoleImageServer) RecordImageAdmission(ctx context.Context, request *controlplanev1.RecordImageAdmissionRequest) (*controlplanev1.RecordImageAdmissionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_RecordImageAdmission_FullMethodName)
	if err != nil {
		return nil, err
	}
	artifact, err := server.service.RecordAdmission(ctx, roleimagerepository.AdmissionRecordInput{
		ExpectedAdmissionAttemptRef: request.GetExpectedAdmissionAttemptRef(), ExpectedAdmissionAttempt: request.GetExpectedAdmissionAttempt(),
		VulnerabilityReportJSON: request.GetVulnerabilityReportJson(), VulnerabilityReportProjectionSHA256: request.GetVulnerabilityReportProjectionSha256(), RiskAcceptanceSHA256: request.GetRiskAcceptanceSha256(),
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
		ClaimToken: request.GetClaimToken(), ManifestDigest: request.GetManifestDigest(),
		ImmutableBuildSHA256: request.GetImmutableBuildSha256(), ProvenanceSHA256: request.GetProvenanceSha256(),
		SBOMSHA256: request.GetSbomSha256(), VulnerabilityEvidenceSHA256: request.GetVulnerabilityEvidenceSha256(),
		PolicyRevision: request.GetPolicyRevision(), PolicySHA256: request.GetPolicySha256(),
		Verdict:           strings.TrimPrefix(request.GetVerdict().String(), "IMAGE_ADMISSION_VERDICT_"),
		SignatureIdentity: request.GetSignatureIdentity(), SignatureSHA256: request.GetSignatureSha256(),
		AdmissionReceiptSHA256:            request.GetAdmissionReceiptSha256(),
		AdmissionReceiptOCIManifestDigest: request.GetAdmissionReceiptOciManifestDigest(),
		ToolInventoryJSON:                 request.GetToolInventoryJson(), ToolInventorySHA256: request.GetToolInventorySha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RecordImageAdmissionResponse{ImageArtifact: castImageArtifact(artifact)}, nil
}

// FailImageAdmission carries exact attempt/claim/fence/authority and artifact/build/recipe/policy pins
// plus the failure code to the domain, then projects its admission-failure result.
// FailImageAdmission несёт точные привязки попытки, claim, fence, полномочий, артефакта, сборки,
// рецепта и политики с кодом ошибки домену, затем переносит результат ошибки допуска.
func (server *RoleImageServer) FailImageAdmission(ctx context.Context, request *controlplanev1.FailImageAdmissionRequest) (*controlplanev1.FailImageAdmissionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_FailImageAdmission_FullMethodName)
	if err != nil {
		return nil, err
	}
	failure, err := server.service.FailAdmission(ctx, roleimagerepository.AdmissionFailureInput{
		ExpectedAdmissionAttemptRef: request.GetExpectedAdmissionAttemptRef(), ExpectedAdmissionAttempt: request.GetExpectedAdmissionAttempt(),
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(), ClaimToken: request.GetClaimToken(),
		ExpectedAuthorityGeneration: request.GetExpectedAuthorityGeneration(), ManifestDigest: request.GetManifestDigest(),
		ImmutableBuildSHA256: request.GetImmutableBuildSha256(), ProvenanceSHA256: request.GetProvenanceSha256(),
		PolicyRevision: request.GetPolicyRevision(), PolicySHA256: request.GetPolicySha256(), BuildRef: request.GetBuildRef(),
		ExpectedBuildAttempt: request.GetExpectedBuildAttempt(), RecipeGeneration: request.GetRecipeGeneration(),
		SpecSHA256: request.GetSpecSha256(), ErrorCode: request.GetErrorCode(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.FailImageAdmissionResponse{AdmissionFailure: castRoleImageAdmissionFailure(&failure)}, nil
}

// ExpireImageAdmissionClaim submits the exact expected attempt, authority/fence/version and immutable
// claim/artifact/build/policy tuple to the domain expiry command; transport does not decide that a
// lease has expired.
// ExpireImageAdmissionClaim передаёт точную ожидаемую попытку, полномочия, fence, версию и
// неизменяемый набор claim, артефакта, сборки и политики команде истечения домена; транспорт сам
// истечение lease не определяет.
func (server *RoleImageServer) ExpireImageAdmissionClaim(ctx context.Context, request *controlplanev1.ExpireImageAdmissionClaimRequest) (*controlplanev1.ExpireImageAdmissionClaimResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ExpireImageAdmissionClaim_FullMethodName)
	if err != nil {
		return nil, err
	}
	failure, err := server.service.ExpireAdmission(ctx, roleimagerepository.AdmissionExpiryInput{ExpectedAdmissionAttemptRef: request.GetExpectedAdmissionAttemptRef(), ExpectedAdmissionAttempt: request.GetExpectedAdmissionAttempt(), Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(), ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(), ExpectedAuthorityGeneration: request.GetExpectedAuthorityGeneration(), ManifestDigest: request.GetManifestDigest(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(), ProvenanceSHA256: request.GetProvenanceSha256(), PolicyRevision: request.GetPolicyRevision(), PolicySHA256: request.GetPolicySha256(), BuildRef: request.GetBuildRef(), ExpectedBuildAttempt: request.GetExpectedBuildAttempt(), RecipeGeneration: request.GetRecipeGeneration(), SpecSHA256: request.GetSpecSha256()})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ExpireImageAdmissionClaimResponse{AdmissionFailure: castRoleImageAdmissionFailure(&failure)}, nil
}

// ClaimImagePromotion uses the resolved exact-operation principal to request a domain-owned promotion
// claim and maps its artifact, token, fence, authority generation and deadline.
// ClaimImagePromotion использует разрешённый principal точной операции для запроса доменного claim
// продвижения и переносит артефакт, токен, fence, поколение полномочий и срок.
func (server *RoleImageServer) ClaimImagePromotion(ctx context.Context, request *controlplanev1.ClaimImagePromotionRequest) (*controlplanev1.ClaimImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimPromotion(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImagePromotionResponse{
		ImageArtifact: castImageArtifact(claim.Artifact), PromotionClaim: claim.PromotionClaim,
		Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		ClaimExpiresAt: timestamp(claim.ClaimExpiresAt),
	}, nil
}

// AuthorizeImagePromotion submits the exact artifact version/claim/manifest and resolved principal to
// the domain authorization step; only its returned token/deadline are exposed for promotion.
// AuthorizeImagePromotion передаёт домену точные версию артефакта, claim, manifest и разрешённый
// principal для авторизации; для продвижения выдаются лишь возвращённые доменом токен и срок.
func (server *RoleImageServer) AuthorizeImagePromotion(ctx context.Context, request *controlplanev1.AuthorizeImagePromotionRequest) (*controlplanev1.AuthorizeImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_AuthorizeImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	authorization, err := server.service.AuthorizePromotion(ctx, roleimagerepository.PromotionAuthorizeInput{
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), PromotionClaim: request.GetPromotionClaim(),
		ManifestDigest: request.GetManifestDigest(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.AuthorizeImagePromotionResponse{
		ImageArtifact:          castImageArtifact(authorization.Artifact),
		AuthorizationToken:     authorization.AuthorizationToken,
		AuthorizationExpiresAt: timestamp(authorization.AuthorizationExpiresAt),
	}, nil
}

// CompleteImagePromotion binds the authorized token to exact artifact/version, promoted reference,
// manifest and readback digest in the domain completion command; it does not independently verify
// registry delivery.
// CompleteImagePromotion связывает разрешённый токен с точными артефактом, версией, promoted
// reference, manifest и хешем readback в доменной команде завершения; доставку registry независимо не
// проверяет.
func (server *RoleImageServer) CompleteImagePromotion(ctx context.Context, request *controlplanev1.CompleteImagePromotionRequest) (*controlplanev1.CompleteImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_CompleteImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	artifact, err := server.service.CompletePromotion(ctx, roleimagerepository.PromotionCompleteInput{
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), AuthorizationToken: request.GetAuthorizationToken(),
		PromotedReference: request.GetPromotedReference(), ManifestDigest: request.GetManifestDigest(),
		PromotionReadbackSHA256: request.GetPromotionReadbackSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteImagePromotionResponse{ImageArtifact: castImageArtifact(artifact)}, nil
}

// roleImageScopeKind maps only ORGANIZATION and PROJECT to known scope enums, preserving unknown scope
// as UNSPECIFIED rather than widening it.
// roleImageScopeKind переводит только ORGANIZATION и PROJECT в известные enum области, сохраняя
// неизвестное как UNSPECIFIED, а не расширяя область.
func roleImageScopeKind(kind string) controlplanev1.RuntimeResourceScopeKind {
	switch kind {
	case "ORGANIZATION":
		return controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	case "PROJECT":
		return controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
	default:
		return controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_UNSPECIFIED
	}
}

// ListOrganizationRoleImageRecipes retains its distinct organization-list principal and domain
// operation, forwarding only page/query/state and mapping the returned list/cursor/total.
// ListOrganizationRoleImageRecipes сохраняет отдельный principal организационного списка и доменную
// операцию, передавая лишь страницу, запрос и состояние и перенося список, курсор и количество.
func (server *RoleImageServer) ListOrganizationRoleImageRecipes(ctx context.Context, request *controlplanev1.ListOrganizationRoleImageRecipesRequest) (*controlplanev1.ListOrganizationRoleImageRecipesResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListOrganizationRoleImageRecipes_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, nextPage, total, err := server.service.ListOrganization(ctx, p, roleimagerepository.Filter{
		Page: page(request.GetPage()), Query: request.GetQuery(), State: request.GetState(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListOrganizationRoleImageRecipesResponse{Page: &controlplanev1.PageInfo{NextPageToken: nextPage}, Total: total}
	for _, item := range items {
		response.Recipes = append(response.Recipes, castRoleImageRecipe(item))
	}
	return response, nil
}

// GetOrganizationRoleImageRecipe retains the distinct organization-read principal and domain
// GetOrganization call, sharing only the pure resolved-detail projection with project reads.
// GetOrganizationRoleImageRecipe сохраняет отдельный principal организационного чтения и доменный
// GetOrganization, разделяя с проектным чтением лишь чистую проекцию уже разрешённых данных.
func (server *RoleImageServer) GetOrganizationRoleImageRecipe(ctx context.Context, request *controlplanev1.GetOrganizationRoleImageRecipeRequest) (*controlplanev1.GetOrganizationRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_GetOrganizationRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	detail, err := server.service.GetOrganization(ctx, p, request.GetRecipeRef())
	if err != nil {
		return nil, transportError(err)
	}
	response := castRoleImageRecipeDetail(detail)
	return &controlplanev1.GetOrganizationRoleImageRecipeResponse{
		Recipe: response.Recipe, AdmissionFailure: response.AdmissionFailure,
		Builds: response.Builds, ActiveArtifact: response.ActiveArtifact, PromotionCandidate: response.PromotionCandidate,
	}, nil
}

// ManageOrganizationRoleImageRecipe calls the distinct organization-management operation with resolved
// principal, mutation and recipe/build/selection fields, omitting caller-supplied project/role
// ownership.
// ManageOrganizationRoleImageRecipe вызывает отдельную операцию организационного управления с
// разрешённым principal, изменением и полями рецепта, сборки и выбора, не передавая владение проектом
// или ролью от вызывающего.
func (server *RoleImageServer) ManageOrganizationRoleImageRecipe(ctx context.Context, request *controlplanev1.ManageOrganizationRoleImageRecipeRequest) (*controlplanev1.ManageOrganizationRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ManageOrganizationRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ManageOrganization(ctx, roleimagerepository.ManageInput{
		Principal: p, Mutation: mutation(request.GetMutation()), Action: roleImageAction(request.GetAction()),
		RecipeRef: request.GetRecipeRef(), BuildRef: request.GetBuildRef(),
		Name:        request.GetName(),
		Environment: domainRoleEnvironmentSelection(request.GetEnvironment()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ManageOrganizationRoleImageRecipeResponse{Recipe: castRoleImageRecipe(result.Recipe), Reused: result.Reused}
	if result.Build != nil {
		response.ImageBuild = castImageBuild(*result.Build)
	}
	if result.Artifact != nil {
		response.ImageArtifact = castImageArtifact(*result.Artifact)
	}
	return response, nil
}
