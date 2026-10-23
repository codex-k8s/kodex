// Package roleimage реализует invariants supply-chain образов ролей.
package roleimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	// permissionListRecipes names the gateway permission for reading project recipe collections.
	// permissionListRecipes задаёт разрешение шлюза на чтение проектных списков рецептов.
	permissionListRecipes = "platform.role-images.recipes.list"
	// permissionListEnvironments names the gateway permission for reading the approved build environment catalog.
	// permissionListEnvironments задаёт разрешение шлюза на чтение утверждённого каталога окружений сборки.
	permissionListEnvironments = "platform.role-images.environments.list"
	// permissionGetRecipe names the gateway permission for reading one project recipe.
	// permissionGetRecipe задаёт разрешение шлюза на чтение одного проектного рецепта.
	permissionGetRecipe = "platform.role-images.recipes.get"
	// permissionManageRecipe names the gateway permission for project recipe lifecycle commands.
	// permissionManageRecipe задаёт разрешение шлюза на команды жизненного цикла проектного рецепта.
	permissionManageRecipe = "platform.role-images.recipes.manage"
	// permissionClaimBuild restricts build claims to the role-image-builder workload together with the caller check.
	// permissionClaimBuild ограничивает получение сборок workload role-image-builder совместно с проверкой вызывающего.
	permissionClaimBuild = "platform.role-images.builds.claim"
	// permissionRenewBuild selects build lease renewal authority independently of claim and completion.
	// permissionRenewBuild выбирает полномочие продления lease сборки отдельно от получения и завершения.
	permissionRenewBuild = "platform.role-images.builds.renew"
	// permissionProgressBuild selects the permission for reporting nonterminal build progress.
	// permissionProgressBuild задаёт разрешение на сообщение промежуточного прогресса сборки.
	permissionProgressBuild = "platform.role-images.builds.progress"
	// permissionCompleteBuild selects build completion authority; it does not grant admission or promotion.
	// permissionCompleteBuild задаёт полномочие завершения сборки, не выдавая допуск или публикацию образа.
	permissionCompleteBuild = "platform.role-images.builds.complete"
	// permissionFailBuild selects the builder permission for reporting a failed attempt under its lease.
	// permissionFailBuild задаёт разрешение сборщика на сообщение отказа попытки под её lease.
	permissionFailBuild = "platform.role-images.builds.fail"
	// permissionGetSupplyWork names the controller permission for inspecting pending supply-chain work.
	// permissionGetSupplyWork задаёт разрешение контроллера на чтение наличия задач цепочки поставки.
	permissionGetSupplyWork = "platform.role-images.supply-work.get"
	// permissionClaimAdmission names the image-admission workload permission for claiming verification work.
	// permissionClaimAdmission задаёт разрешение workload image-admission на получение задачи проверки.
	permissionClaimAdmission = "platform.role-images.admission.claim"
	// permissionRecordAdmission names the image-admission workload permission for recording a bounded verdict and evidence.
	// permissionRecordAdmission задаёт разрешение workload image-admission на запись ограниченного вердикта и свидетельств.
	permissionRecordAdmission = "platform.role-images.admission.record"
	// permissionRequestPromotion names the gateway permission for requesting project image promotion, distinct from worker authorization.
	// permissionRequestPromotion задаёт разрешение шлюза на запрос публикации проектного образа отдельно от авторизации worker.
	permissionRequestPromotion = "platform.command.role-images.promote"
	// permissionClaimPromotion names the image-promotion worker permission for claiming a promotion task.
	// permissionClaimPromotion задаёт разрешение worker image-promotion на получение задачи публикации.
	permissionClaimPromotion = "platform.role-images.promotion.claim"
	// permissionAuthorizePromotion selects the worker permission for obtaining exact promotion authorization after claim.
	// permissionAuthorizePromotion задаёт разрешение worker на получение точной авторизации публикации после claim.
	permissionAuthorizePromotion = "platform.role-images.promotion.authorize"
	// permissionCompletePromotion selects the worker permission for committing promotion with readback evidence.
	// permissionCompletePromotion задаёт разрешение worker на фиксацию публикации со свидетельством readback.
	permissionCompletePromotion = "platform.role-images.promotion.complete"
	// permissionListOrganizationRecipes names organization recipe listing authority, separate from the project collection.
	// permissionListOrganizationRecipes задаёт полномочие чтения организационных рецептов отдельно от проектного списка.
	permissionListOrganizationRecipes = "platform.organization.role-images.recipes.list"
	// permissionGetOrganizationRecipe names the permission for reading one organization-owned recipe.
	// permissionGetOrganizationRecipe задаёт разрешение чтения одного рецепта организации.
	permissionGetOrganizationRecipe = "platform.organization.role-images.recipes.get"
	// permissionManageOrganizationRecipe names organization recipe lifecycle authority and never reuses project scope identifiers.
	// permissionManageOrganizationRecipe задаёт полномочие жизненного цикла рецепта организации без переиспользования проектных идентификаторов области.
	permissionManageOrganizationRecipe = "platform.organization.role-images.recipes.manage"
	// permissionRequestOrganizationPromotion names the gateway permission for requesting organization-wide image promotion.
	// permissionRequestOrganizationPromotion задаёт разрешение шлюза на запрос публикации образа общей области организации.
	permissionRequestOrganizationPromotion = "platform.command.organization.role-images.promote"
)

var (
	// sha256Pattern accepts exactly 64 lowercase hexadecimal characters for raw SHA-256 evidence, without an algorithm prefix.
	// sha256Pattern допускает ровно 64 строчных шестнадцатеричных символа для SHA-256 свидетельства без префикса алгоритма.
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// manifestPattern accepts only the sha256-prefixed OCI digest form used by image and package manifests.
	// manifestPattern допускает только OCI digest с префиксом sha256 для манифестов образов и пакетов.
	manifestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	// namePattern bounds revision, package and tool identifiers to 256 ASCII characters from the supported name alphabet.
	// namePattern ограничивает идентификаторы ревизий, пакетов и инструментов 256 ASCII-символами поддерживаемого алфавита.
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@/~:-]{0,255}$`)
	// errorCodePattern accepts a closed-format uppercase diagnostic code of at most 64 characters; semantic code selection remains with the caller.
	// errorCodePattern допускает формат диагностического кода верхнего регистра длиной до 64 символов; смысловой выбор кода остаётся у вызывающего.
	errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	// signatureIDPattern bounds signing identity syntax to 256 ASCII characters; matching does not verify a signature or trust chain.
	// signatureIDPattern ограничивает синтаксис identity подписи 256 ASCII-символами; совпадение не проверяет подпись или цепочку доверия.
	signatureIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)
)

// Service coordinates recipe and image lifecycle validation with the authoritative repository and immutable environment catalog; it does not build or publish image bytes.
// Service согласует проверки жизненного цикла рецептов и образов с авторитетным repository и неизменяемым каталогом окружений; сам не собирает и не публикует байты образов.
type Service struct {
	repository repository.Repository
	catalog    *Catalog
}

// GetSupplyWorkAvailability Re-resolves the principal and requires the exact supply-work permission from image-admission-controller before reading availability from the repository. It does not claim work or mint a lease.
// GetSupplyWorkAvailability Повторно разрешает principal и требует точное разрешение supply-work от image-admission-controller перед чтением наличия работы из repository. Не получает задачу и не выдаёт lease.
func (service *Service) GetSupplyWorkAvailability(ctx context.Context, principal value.Principal) (repository.SupplyWorkAvailability, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return repository.SupplyWorkAvailability{}, err
	}
	if principal.Permission != permissionGetSupplyWork || principal.CallerWorkload != "image-admission-controller" {
		return repository.SupplyWorkAvailability{}, errs.ErrForbidden
	}
	return service.repository.GetSupplyWorkAvailability(ctx, principal)
}

// New Requires both repository and catalog dependencies and returns a service without starting workers or performing I/O.
// New Требует обе зависимости — repository и catalog — и возвращает сервис без запуска worker и операций ввода-вывода.
func New(repo repository.Repository, catalog *Catalog) (*Service, error) {
	if repo == nil || catalog == nil {
		return nil, errors.New("role image repository is required")
	}
	return &Service{repository: repo, catalog: catalog}, nil
}

// ValidateManagedRecipe Validates project/role reference syntax, display name and the complete recipe specification for managed UI/Git input. Resource existence and ownership are not established by this metadata check.
// ValidateManagedRecipe Проверяет синтаксис ссылок проекта и роли, название и полную спецификацию рецепта для managed UI/Git input. Эта проверка метаданных не устанавливает существование ресурса или владение им.
func ValidateManagedRecipe(projectRef, roleRef, name string, input entity.RoleImageRecipeInput) error {
	if !validRef(projectRef, "prj") || !validRef(roleRef, "role") || !validDisplayName(name) || validateRecipe(input) != nil {
		return errs.ErrInvalid
	}
	return nil
}

// ValidateManagedOrganizationRecipe Applies the same display-name and recipe invariants to organization-managed input without requiring project or role identifiers.
// ValidateManagedOrganizationRecipe Применяет те же инварианты названия и рецепта к управляемому input организации без требования идентификаторов проекта и роли.
func ValidateManagedOrganizationRecipe(name string, input entity.RoleImageRecipeInput) error {
	if !validDisplayName(name) || validateRecipe(input) != nil {
		return errs.ErrInvalid
	}
	return nil
}

// ListEnvironments Resolves the principal and requires the exact catalog-list permission from control-api-gateway before returning catalog environments. No repository query or mutation follows the authorization check.
// ListEnvironments Разрешает principal и требует точное разрешение чтения каталога от control-api-gateway перед выдачей окружений. После проверки полномочий запросов или изменений repository не выполняет.
func (service *Service) ListEnvironments(ctx context.Context, principal value.Principal) ([]Environment, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, err
	}
	if err := authorize(principal, permissionListEnvironments, "control-api-gateway"); err != nil {
		return nil, err
	}
	return service.catalog.List(), nil
}

// List Resolves gateway authority and validates the project reference before delegating recipe pagination to the repository. Authorization errors take precedence over malformed reference errors.
// List Разрешает полномочия шлюза и проверяет ссылку проекта перед передачей пагинации рецептов repository. Ошибка полномочий имеет приоритет над ошибкой формата ссылки.
func (service *Service) List(ctx context.Context, principal value.Principal, filter repository.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, "", 0, err
	}
	if err := authorize(principal, permissionListRecipes, "control-api-gateway"); err != nil || !validRef(filter.ProjectRef, "prj") {
		return nil, "", 0, firstError(err, errs.ErrInvalid)
	}
	return service.repository.List(ctx, principal, filter)
}

// Get Resolves gateway authority and validates the recipe reference before the project-scoped repository read, preserving authorization-error precedence.
// Get Разрешает полномочия шлюза и проверяет ссылку рецепта перед проектным чтением repository, сохраняя приоритет ошибки авторизации.
func (service *Service) Get(ctx context.Context, principal value.Principal, ref string) (repository.Detail, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return repository.Detail{}, err
	}
	if err := authorize(principal, permissionGetRecipe, "control-api-gateway"); err != nil || !validRef(ref, "imgrec") {
		return repository.Detail{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.Get(ctx, principal, ref)
}

// Manage Selects the project scope and permission for the shared recipe command validation path; payload fields cannot choose an organization scope.
// Manage Выбирает проектную область и разрешение для общего пути проверки команды рецепта; поля payload не могут выбрать область организации.
func (service *Service) Manage(ctx context.Context, input repository.ManageInput) (repository.ManageResult, error) {
	return service.manage(ctx, input, "PROJECT", permissionManageRecipe)
}

// ListOrganization Requires organization recipe-list authority and rejects project or role filters before the organization repository query.
// ListOrganization Требует полномочие чтения рецептов организации и отклоняет фильтры проекта или роли перед организационным запросом repository.
func (service *Service) ListOrganization(ctx context.Context, principal value.Principal, filter repository.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, "", 0, err
	}
	if err := authorize(principal, permissionListOrganizationRecipes, "control-api-gateway"); err != nil {
		return nil, "", 0, err
	}
	if filter.ProjectRef != "" || filter.RoleDefinitionRef != "" {
		return nil, "", 0, errs.ErrInvalid
	}
	return service.repository.ListOrganization(ctx, principal, filter)
}

// GetOrganization Requires the organization read permission, validates the recipe identifier and uses the organization-owned repository read rather than the project route.
// GetOrganization Требует организационное разрешение чтения, проверяет идентификатор рецепта и использует чтение repository организации вместо проектного пути.
func (service *Service) GetOrganization(ctx context.Context, principal value.Principal, ref string) (repository.Detail, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return repository.Detail{}, err
	}
	if err := authorize(principal, permissionGetOrganizationRecipe, "control-api-gateway"); err != nil {
		return repository.Detail{}, err
	}
	if !validRef(ref, "imgrec") {
		return repository.Detail{}, errs.ErrInvalid
	}
	return service.repository.GetOrganization(ctx, principal, ref)
}

// ListOrganizationRevisions Resolves the exact revision-list permission, validates the organization recipe reference and forwards page options unchanged to its repository read.
// ListOrganizationRevisions Разрешает точное полномочие списка ревизий, проверяет ссылку организационного рецепта и передаёт параметры страницы неизменёнными его repository.
func (service *Service) ListOrganizationRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.RoleImageRecipeRevision, string, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if err := authorize(principal, "platform.organization.role-images.recipe-revisions.list", "control-api-gateway"); err != nil {
		return nil, "", err
	}
	if !validRef(ref, "imgrec") {
		return nil, "", errs.ErrInvalid
	}
	return service.repository.ListOrganizationRevisions(ctx, principal, ref, page)
}

// ManageOrganization Selects organization scope and authority for shared command validation; project and role identifiers are rejected by that path.
// ManageOrganization Выбирает организационную область и полномочие для общей проверки команды; этот путь отклоняет идентификаторы проекта и роли.
func (service *Service) ManageOrganization(ctx context.Context, input repository.ManageInput) (repository.ManageResult, error) {
	return service.manage(ctx, input, "ORGANIZATION", permissionManageOrganizationRecipe)
}

// manage Re-resolves authority, normalizes the action, assigns trusted scope and operation, resolves create/update environment specifications and binds an intent digest. It enforces action-specific reference/version/field rules before invoking the matching project or organization repository command.
// manage Повторно разрешает полномочия, нормализует действие, назначает доверенные область и operation, разрешает окружение для create/update и связывает digest намерения. Проверяет правила ссылок, версий и полей конкретного действия перед соответствующей проектной или организационной командой repository.
func (service *Service) manage(ctx context.Context, input repository.ManageInput, scopeKind, permission string) (repository.ManageResult, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return repository.ManageResult{}, err
	}
	input.Principal = principal
	if err := authorize(input.Principal, permission, "control-api-gateway"); err != nil {
		return repository.ManageResult{}, err
	}
	input.Action = strings.ToUpper(strings.TrimSpace(input.Action))
	input.ScopeKind = scopeKind
	validProject := validRef(input.ProjectRef, "prj")
	validRole := validRef(input.RoleDefinitionRef, "role")
	if scopeKind == "ORGANIZATION" {
		if input.ProjectRef != "" || input.RoleDefinitionRef != "" {
			return repository.ManageResult{}, errs.ErrInvalid
		}
		validProject, validRole = true, true
	}
	if input.Action == "CREATE" || input.Action == "UPDATE" {
		input.Recipe, err = service.catalog.Resolve(input.Environment)
		if err != nil {
			return repository.ManageResult{}, err
		}
	}
	input.Mutation.Operation = "role-image-recipe." + strings.ToLower(input.Action)
	if scopeKind == "ORGANIZATION" {
		input.Mutation.Operation = "organization-role-image-recipe." + strings.ToLower(input.Action)
	}
	input.Mutation.IntentDigest = roleImageManageIntentDigest(input)
	if err := input.Mutation.Validate(); err != nil {
		return repository.ManageResult{}, errs.ErrInvalid
	}
	switch input.Action {
	case "CREATE":
		if !validProject || !validRole ||
			!validDisplayName(input.Name) || validateRecipe(input.Recipe) != nil || input.Mutation.ExpectedVersion != nil || input.RecipeRef != "" || input.BuildRef != "" {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	case "UPDATE":
		if !validProject || !validRef(input.RecipeRef, "imgrec") || input.RoleDefinitionRef != "" ||
			!validDisplayName(input.Name) || validateRecipe(input.Recipe) != nil || input.Mutation.ExpectedVersion == nil || input.BuildRef != "" {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	case "ARCHIVE", "RESTORE", "REQUEST_BUILD":
		if !validProject || !validRef(input.RecipeRef, "imgrec") ||
			input.Mutation.ExpectedVersion == nil || input.RoleDefinitionRef != "" || input.BuildRef != "" ||
			hasRecipeEnvironmentSelection(input.Environment) {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	case "CANCEL_BUILD":
		if !validProject || !validRef(input.RecipeRef, "imgrec") || !validRef(input.BuildRef, "imgbld") ||
			input.Mutation.ExpectedVersion == nil || input.RoleDefinitionRef != "" || input.Name != "" ||
			hasRecipeEnvironmentSelection(input.Environment) {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	default:
		return repository.ManageResult{}, errs.ErrInvalid
	}
	if scopeKind == "ORGANIZATION" {
		return service.repository.ManageOrganization(ctx, input)
	}
	return service.repository.Manage(ctx, input)
}

// hasRecipeEnvironmentSelection reports any supplied recipe environment field,
// including whitespace-only text, without resolving a catalog or normalizing input.
// Lifecycle-only commands use it to reject attempts to alter build configuration.
// hasRecipeEnvironmentSelection обнаруживает любое переданное поле окружения
// рецепта, включая строку из пробелов, без lookup каталога и нормализации input.
// Команды жизненного цикла отклоняют через него попытку менять настройки сборки.
func hasRecipeEnvironmentSelection(input entity.RoleEnvironmentSelection) bool {
	return input.EnvironmentKey != "" || len(input.PackageKeys) != 0 || len(input.ToolKeys) != 0 ||
		input.InstallationBlock != "" || input.Dockerfile != ""
}

// roleImageManageIntentDigest Hashes normalized recipe intent and adds BuildRef only for CANCEL_BUILD, so cancellation of another build cannot reuse the same mutation intent.
// roleImageManageIntentDigest Хеширует нормализованное намерение рецепта и добавляет BuildRef только для CANCEL_BUILD, чтобы отмена другой сборки не переиспользовала то же намерение изменения.
func roleImageManageIntentDigest(input repository.ManageInput) string {
	intent := struct {
		ScopeKind, Action, RecipeRef, ProjectRef, RoleDefinitionRef, Name string
		Recipe                                                            entity.RoleImageRecipeInput
	}{input.ScopeKind, input.Action, input.RecipeRef, input.ProjectRef, input.RoleDefinitionRef, input.Name, input.Recipe}
	if input.Action == "CANCEL_BUILD" {
		return digest(struct {
			Intent   any
			BuildRef string
		}{intent, input.BuildRef})
	}
	return digest(intent)
}

// ClaimBuild Resolves the exact builder identity and claim permission, validates its idempotency key, then delegates atomic work selection and lease creation to the repository.
// ClaimBuild Разрешает точную identity сборщика и разрешение claim, проверяет ключ идемпотентности, затем поручает repository атомарный выбор работы и создание lease.
func (service *Service) ClaimBuild(ctx context.Context, principal value.Principal, key string) (entity.ImageBuildClaim, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return entity.ImageBuildClaim{}, err
	}
	if err := authorizeKey(principal, permissionClaimBuild, "role-image-builder", key); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	return service.repository.ClaimBuild(ctx, principal, key)
}

// RenewBuild Re-resolves the principal and validates exact build version/attempt/fence/token under the renewal permission before repository lease renewal.
// RenewBuild Повторно разрешает principal и проверяет точные version/attempt/fence/token сборки под разрешением продления перед продлением lease в repository.
func (service *Service) RenewBuild(ctx context.Context, input repository.BuildLeaseInput) (entity.ImageBuildClaim, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageBuildClaim{}, err
	}
	input.Principal = principal
	if err := validateBuildLease(input, permissionRenewBuild); err != nil {
		return entity.ImageBuildClaim{}, err
	}
	return service.repository.RenewBuild(ctx, input)
}

// ReportBuildProgress Validates the builder lease and a recognized nonterminal stage with progress no greater than 99 before recording progress. Completion is intentionally a separate command.
// ReportBuildProgress Проверяет lease сборщика, известный промежуточный этап и прогресс не более 99 перед записью. Завершение намеренно остаётся отдельной командой.
func (service *Service) ReportBuildProgress(ctx context.Context, input repository.BuildProgressInput) (entity.ImageBuild, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageBuild{}, err
	}
	input.Principal = principal
	if err := validateBuildLease(input.BuildLeaseInput, permissionProgressBuild); err != nil ||
		!progressStage(input.Stage) || input.ProgressPercent > 99 {
		return entity.ImageBuild{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.ReportBuildProgress(ctx, input)
}

// CompleteBuild Requires completion authority and exact build lease pins, a staging reference bound to the manifest digest, and provenance/build hashes before recording build and artifact state.
// CompleteBuild Требует полномочие завершения и точные lease pins сборки, staging reference с digest манифеста и хеши provenance/build перед записью состояний сборки и артефакта.
func (service *Service) CompleteBuild(ctx context.Context, input repository.BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageBuild{}, entity.ImageArtifact{}, err
	}
	input.Principal = principal
	if err := validateBuildLease(input.BuildLeaseInput, permissionCompleteBuild); err != nil ||
		!validImageReference(input.StagingReference, input.ManifestDigest) || !manifestPattern.MatchString(input.ManifestDigest) ||
		!sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) {
		return entity.ImageBuild{}, entity.ImageArtifact{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.CompleteBuild(ctx, input)
}

// FailBuild Validates failure authority, exact build lease and bounded diagnostic syntax before recording the failed build attempt; it does not infer retry policy locally.
// FailBuild Проверяет полномочие отказа, точный lease сборки и ограниченный синтаксис диагностики перед записью отказа попытки; локально не выводит политику повтора.
func (service *Service) FailBuild(ctx context.Context, input repository.BuildFailureInput) (entity.ImageBuild, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageBuild{}, err
	}
	input.Principal = principal
	if err := validateBuildLease(input.BuildLeaseInput, permissionFailBuild); err != nil ||
		!errorCodePattern.MatchString(input.ErrorCode) || !validDiagnostic(input.DiagnosticCode, input.DiagnosticSummary) {
		return entity.ImageBuild{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.FailBuild(ctx, input)
}

// ClaimAdmission Authorizes only the image-admission workload with a valid claim key before delegating selection of pending verification work.
// ClaimAdmission Разрешает только workload image-admission с корректным ключом claim перед передачей выбора ожидающей проверки.
func (service *Service) ClaimAdmission(ctx context.Context, principal value.Principal, key string) (entity.ImageAdmissionClaim, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	if err := authorizeKey(principal, permissionClaimAdmission, "image-admission", key); err != nil {
		return entity.ImageAdmissionClaim{}, err
	}
	return service.repository.ClaimAdmission(ctx, principal, key)
}

// RecordAdmission Validates the admission worker and exact attempt/version/fence/claim, evidence hashes, signature metadata and closed verdict. An accepted result additionally requires a decoded tool inventory bound to the manifest and provenance before the repository owns the transition.
// RecordAdmission Проверяет worker допуска, точные attempt/version/fence/claim, хеши свидетельств, метаданные подписи и закрытый вердикт. Для принятого результата дополнительно нужен разобранный каталог инструментов с привязкой к манифесту и provenance; переход выполняет repository.
func (service *Service) RecordAdmission(ctx context.Context, input repository.AdmissionRecordInput) (entity.ImageArtifact, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	input.Principal = principal
	if err := authorizeKey(input.Principal, permissionRecordAdmission, "image-admission", input.IdempotencyKey); err != nil ||
		!validRef(input.ExpectedAdmissionAttemptRef, "imgadm") || input.ExpectedAdmissionAttempt == 0 ||
		input.VulnerabilityReportJSON == "" || !sha256Pattern.MatchString(input.VulnerabilityReportProjectionSHA256) ||
		!validRef(input.ArtifactRef, "imgart") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ClaimToken == "" ||
		!manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) ||
		!sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.SBOMSHA256) ||
		!sha256Pattern.MatchString(input.VulnerabilityEvidenceSHA256) || input.PolicyRevision == 0 ||
		!sha256Pattern.MatchString(input.PolicySHA256) || (input.Verdict != "ACCEPTED" && input.Verdict != "REJECTED") ||
		!signatureIDPattern.MatchString(input.SignatureIdentity) || !sha256Pattern.MatchString(input.SignatureSHA256) ||
		!sha256Pattern.MatchString(input.AdmissionReceiptSHA256) || !manifestPattern.MatchString(input.AdmissionReceiptOCIManifestDigest) {
		return entity.ImageArtifact{}, firstError(err, errs.ErrInvalid)
	}
	if input.ToolInventoryJSON != "" {
		inventory, err := runtimecontract.DecodeImageToolInventory([]byte(input.ToolInventoryJSON))
		if err != nil || runtimecontract.ImageInventorySHA256([]byte(input.ToolInventoryJSON)) != input.ToolInventorySHA256 || inventory.ImageDigest != input.ManifestDigest || inventory.ProvenanceSHA256 != input.ProvenanceSHA256 {
			return entity.ImageArtifact{}, errs.ErrInvalid
		}
	} else if input.Verdict == "ACCEPTED" || input.ToolInventorySHA256 != "" {
		return entity.ImageArtifact{}, errs.ErrInvalid
	}
	return service.repository.RecordAdmission(ctx, input)
}

// ClaimPromotion Requires image-promotion claim authority and a bounded idempotency key before asking the repository for promotion work.
// ClaimPromotion Требует полномочие claim от image-promotion и ограниченный ключ идемпотентности перед запросом работы публикации у repository.
func (service *Service) ClaimPromotion(ctx context.Context, principal value.Principal, key string) (entity.ImagePromotionClaim, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	if err := authorizeKey(principal, permissionClaimPromotion, "image-promotion", key); err != nil {
		return entity.ImagePromotionClaim{}, err
	}
	return service.repository.ClaimPromotion(ctx, principal, key)
}

// Promote Routes a project promotion request through the shared validator with server-selected project scope and permission.
// Promote Передаёт запрос проектной публикации общему валидатору с выбранными сервером проектной областью и разрешением.
func (service *Service) Promote(ctx context.Context, input repository.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	return service.requestPromotion(ctx, input, "PROJECT", permissionRequestPromotion)
}

// RequestOrganizationPromotion Routes an organization promotion request through the shared validator with its separate permission and scope.
// RequestOrganizationPromotion Передаёт запрос публикации организации общему валидатору с отдельными разрешением и областью.
func (service *Service) RequestOrganizationPromotion(ctx context.Context, input repository.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	return service.requestPromotion(ctx, input, "ORGANIZATION", permissionRequestOrganizationPromotion)
}

// requestPromotion Resolves gateway authority, assigns the scope-specific operation and hashes recipe/artifact/provenance/version intent. It requires expected version and valid reference/digest syntax before the matching repository request; actual promotion is not performed here.
// requestPromotion Разрешает полномочия шлюза, назначает operation для области и хеширует намерение recipe/artifact/provenance/version. Требует ожидаемую версию и корректные ссылки/digest перед соответствующим запросом repository; сама публикация здесь не выполняется.
func (service *Service) requestPromotion(ctx context.Context, input repository.PromotionRequestInput, scopeKind, permission string) (entity.RoleImagePromotionReceipt, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	input.Principal = principal
	if err := authorize(input.Principal, permission, "control-api-gateway"); err != nil {
		return entity.RoleImagePromotionReceipt{}, err
	}
	input.Mutation.Operation = "controlplane.promote_role_image"
	input.ScopeKind = scopeKind
	if scopeKind == "ORGANIZATION" {
		input.Mutation.Operation = "controlplane.promote_organization_role_image"
	}
	var expectedVersion int64
	if input.Mutation.ExpectedVersion != nil {
		expectedVersion = *input.Mutation.ExpectedVersion
	}
	input.Mutation.IntentDigest = digest(struct {
		ScopeKind, RecipeRef, ArtifactRef, ExpectedProvenanceSHA256 string
		ExpectedVersion                                             int64
	}{scopeKind, input.RecipeRef, input.ArtifactRef, input.ExpectedProvenanceSHA256, expectedVersion})
	if input.Mutation.ExpectedVersion == nil || input.Mutation.Validate() != nil ||
		!validRef(input.RecipeRef, "imgrec") || !validRef(input.ArtifactRef, "imgart") ||
		!sha256Pattern.MatchString(input.ExpectedProvenanceSHA256) {
		return entity.RoleImagePromotionReceipt{}, errs.ErrInvalid
	}
	if scopeKind == "ORGANIZATION" {
		return service.repository.RequestOrganizationPromotion(ctx, input)
	}
	return service.repository.RequestPromotion(ctx, input)
}

// AuthorizePromotion Validates the promotion worker, idempotency key, artifact version, claim token and manifest digest before requesting repository-owned publication authorization.
// AuthorizePromotion Проверяет worker публикации, ключ идемпотентности, версию артефакта, token claim и digest манифеста перед запросом авторизации публикации, принадлежащей repository.
func (service *Service) AuthorizePromotion(ctx context.Context, input repository.PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImagePromotionAuthorization{}, err
	}
	input.Principal = principal
	if err := authorizeKey(input.Principal, permissionAuthorizePromotion, "image-promotion", input.IdempotencyKey); err != nil ||
		!validRef(input.ArtifactRef, "imgart") || input.ExpectedVersion == 0 || input.PromotionClaim == "" ||
		!manifestPattern.MatchString(input.ManifestDigest) {
		return entity.ImagePromotionAuthorization{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.AuthorizePromotion(ctx, input)
}

// CompletePromotion Requires the promotion worker, artifact/version and authorization token plus a promoted reference pinned to the manifest and a readback hash before committing completion.
// CompletePromotion Требует worker публикации, artifact/version и token авторизации, а также promoted reference с точным манифестом и hash readback перед фиксацией завершения.
func (service *Service) CompletePromotion(ctx context.Context, input repository.PromotionCompleteInput) (entity.ImageArtifact, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	input.Principal = principal
	if err := authorizeKey(input.Principal, permissionCompletePromotion, "image-promotion", input.IdempotencyKey); err != nil ||
		!validRef(input.ArtifactRef, "imgart") || input.ExpectedVersion == 0 || input.AuthorizationToken == "" ||
		!manifestPattern.MatchString(input.ManifestDigest) || !validImageReference(input.PromotedReference, input.ManifestDigest) ||
		!sha256Pattern.MatchString(input.PromotionReadbackSHA256) {
		return entity.ImageArtifact{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.CompletePromotion(ctx, input)
}

// resolvePrincipal Rejects structurally invalid principals as unauthorized, then asks the repository to resolve current authority rather than trusting the input snapshot.
// resolvePrincipal Отклоняет структурно неверные principal как неавторизованные, затем просит repository разрешить текущие полномочия вместо доверия входному снимку.
func (service *Service) resolvePrincipal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrUnauthorized
	}
	return service.repository.ResolvePrincipal(ctx, principal)
}

// validateBuildLease Checks exact builder permission and idempotency first, then build reference, nonzero version/attempt/fence and a 32–512 byte lease token. Current lease ownership is still checked by the repository.
// validateBuildLease Сначала проверяет точное разрешение сборщика и идемпотентность, затем ссылку сборки, ненулевые version/attempt/fence и lease token длиной 32–512 байт. Текущее владение lease дополнительно проверяет repository.
func validateBuildLease(input repository.BuildLeaseInput, permission string) error {
	if err := authorizeKey(input.Principal, permission, "role-image-builder", input.IdempotencyKey); err != nil {
		return err
	}
	if !validRef(input.BuildRef, "imgbld") || input.ExpectedVersion == 0 || input.ExpectedAttempt == 0 ||
		input.ExpectedFence == 0 || len(input.LeaseToken) < 32 || len(input.LeaseToken) > 512 {
		return errs.ErrInvalid
	}
	return nil
}

// authorizeKey Preserves authorization-error precedence and then requires an unpadded idempotency key of 8–128 bytes; it does not itself claim or deduplicate work.
// authorizeKey Сохраняет приоритет ошибки полномочий и затем требует ключ идемпотентности длиной 8–128 байт без крайних пробелов; сам не получает и не дедуплицирует работу.
func authorizeKey(principal value.Principal, permission, workload, key string) error {
	if err := authorize(principal, permission, workload); err != nil {
		return err
	}
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key {
		return errs.ErrInvalid
	}
	return nil
}

// authorize Checks principal structure and exact workload/permission equality, distinguishing unauthorized from forbidden. Current principal resolution is performed separately by callers.
// authorize Проверяет структуру principal и точное совпадение workload/permission, различая отсутствие авторизации и запрет. Актуальное разрешение principal выполняют вызывающие отдельно.
func authorize(principal value.Principal, permission, workload string) error {
	if err := principal.Validate(); err != nil {
		return errs.ErrUnauthorized
	}
	if principal.CallerWorkload != workload || principal.Permission != permission {
		return errs.ErrForbidden
	}
	return nil
}

// validateRecipe Validates pinned base/source/context/toolchain inputs, bounded installation text, one supported Dockerfile base and Linux target platforms. Package and tool keys must match item counts and all platform/key lists must be sorted and unique; this does not perform image admission.
// validateRecipe Проверяет привязки base/source/context/toolchain, ограниченный текст установки, поддерживаемую базу Dockerfile и целевые Linux-платформы. Число ключей пакетов и инструментов должно совпадать с элементами, а списки платформ и ключей должны быть отсортированы и уникальны; допуск образа эта проверка не выполняет.
func validateRecipe(input entity.RoleImageRecipeInput) error {
	if !validRepository(input.BaseImageReference) || !manifestPattern.MatchString(input.BaseImageDigest) ||
		!validExternalRef(input.SourceRef) || !namePattern.MatchString(input.SourceRevision) ||
		!sha256Pattern.MatchString(input.SourceSHA256) || !validExternalRef(input.ContextRef) ||
		!sha256Pattern.MatchString(input.ContextSHA256) || !sha256Pattern.MatchString(input.BuilderSHA256) ||
		!sha256Pattern.MatchString(input.FrontendSHA256) || !sha256Pattern.MatchString(input.ToolchainSHA256) ||
		!validCatalogKey(input.EnvironmentKey) || len(input.PackageKeys) != len(input.Packages) ||
		len(input.ToolKeys) != len(input.Tools) || len(input.Platforms) == 0 || len(input.Platforms) > 8 ||
		len(input.Packages) > 256 || len(input.Tools) > 128 ||
		!utf8.ValidString(input.InstallationBlock) || len(input.InstallationBlock) > 64<<10 || strings.ContainsRune(input.InstallationBlock, 0) {
		return errs.ErrInvalid
	}
	if validateDockerfile(input.Dockerfile, input.BaseImageReference, input.BaseImageDigest) != nil {
		return errs.ErrInvalid
	}
	platformKeys := make([]string, 0, len(input.Platforms))
	for _, platform := range input.Platforms {
		if platform.OS != "linux" || (platform.Architecture != "amd64" && platform.Architecture != "arm64") ||
			(platform.Variant != "" && !namePattern.MatchString(platform.Variant)) {
			return errs.ErrInvalid
		}
		platformKeys = append(platformKeys, platform.OS+"/"+platform.Architecture+"/"+platform.Variant)
	}
	packageKeys := make([]string, 0, len(input.Packages))
	for index, item := range input.Packages {
		if !slices.Contains([]string{"apk", "apt", "dnf", "pip", "npm"}, item.Manager) ||
			!namePattern.MatchString(item.Name) || !namePattern.MatchString(item.Version) ||
			!manifestPattern.MatchString(item.Digest) || !validExternalRef(item.SourceRef) ||
			!validCatalogKey(input.PackageKeys[index]) {
			return errs.ErrInvalid
		}
		packageKeys = append(packageKeys, input.PackageKeys[index])
	}
	toolKeys := make([]string, 0, len(input.Tools))
	for index, item := range input.Tools {
		if !namePattern.MatchString(item.Name) || !namePattern.MatchString(item.Version) ||
			!validExternalRef(item.SourceRef) || !sha256Pattern.MatchString(item.SHA256) ||
			!validCatalogKey(input.ToolKeys[index]) {
			return errs.ErrInvalid
		}
		toolKeys = append(toolKeys, input.ToolKeys[index])
	}
	if !uniqueSorted(platformKeys) || !uniqueSorted(packageKeys) || !uniqueSorted(toolKeys) {
		return errs.ErrInvalid
	}
	return nil
}

// validateDockerfile Rejects empty, oversized or malformed text and syntax directives, requires one exact digest-pinned FROM, and rejects instructions preceding it. This line-oriented guard is not a complete Dockerfile parser or a substitute for build admission.
// validateDockerfile Отклоняет пустой, чрезмерный или некорректный текст и директивы syntax, требует один FROM с точным digest и запрещает инструкции перед ним. Построчная проверка не является полным парсером Dockerfile или заменой допуска сборки.
func validateDockerfile(input, baseReference, baseDigest string) error {
	if input == "" || !utf8.ValidString(input) || len(input) > 256<<10 || strings.ContainsAny(input, "\x00\r") {
		return errs.ErrInvalid
	}
	expectedBase := baseReference + "@" + baseDigest
	foundFrom := false
	for _, rawLine := range strings.Split(input, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "# syntax=") || strings.HasPrefix(lower, "#syntax=") {
			return errs.ErrInvalid
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], "FROM") {
			if foundFrom || len(fields) != 2 && len(fields) != 4 || fields[1] != expectedBase ||
				len(fields) == 4 && !strings.EqualFold(fields[2], "AS") || strings.HasSuffix(line, "\\") {
				return errs.ErrInvalid
			}
			foundFrom = true
			continue
		}
		if !foundFrom {
			return errs.ErrInvalid
		}
	}
	if !foundFrom {
		return errs.ErrInvalid
	}
	return nil
}

// validCatalogKey Accepts 1–100 bytes starting with a lowercase ASCII letter and containing only lowercase letters, digits, dots and hyphens; no trimming or normalization occurs.
// validCatalogKey Допускает 1–100 байт с начальной строчной ASCII-буквой и только строчными буквами, цифрами, точками и дефисами; обрезки пробелов и нормализации нет.
func validCatalogKey(input string) bool {
	if input == "" || len(input) > 100 || input[0] < 'a' || input[0] > 'z' {
		return false
	}
	for _, character := range input {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '-' {
			continue
		}
		return false
	}
	return true
}

// uniqueSorted Requires existing ascending order and rejects adjacent equal strings without reordering or copying the input; an empty list passes.
// uniqueSorted Требует исходный возрастающий порядок и отклоняет соседние равные строки, не переставляя и не копируя input; пустой список проходит.
func uniqueSorted(values []string) bool {
	if !slices.IsSorted(values) {
		return false
	}
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
}

// validDisplayName Checks a nonempty UTF-8 display name bounded to 160 bytes with no leading or trailing whitespace; internal whitespace remains unchanged.
// validDisplayName Проверяет непустое UTF-8 название длиной до 160 байт без крайних пробелов; внутренние пробелы остаются неизменными.
func validDisplayName(input string) bool {
	return strings.TrimSpace(input) == input && utf8.ValidString(input) && len(input) > 0 && len(input) <= 160
}

// validRef applies the shared opaque-reference grammar to the caller's exact resource prefix.
// Resource existence, scope, admission state, and permission remain separate owner-side checks.
// validRef применяет общую грамматику непрозрачных ссылок к точному префиксу ресурса вызывающего кода.
// Существование ресурса, область, состояние допуска и полномочия проверяются отдельно владельцем.
func validRef(input, prefix string) bool {
	return opaqueref.Valid(input, prefix)
}

// validRepository Checks a nonempty repository reference of at most 500 bytes with a slash and without a digest separator or whitespace; it does not resolve a registry or validate ownership.
// validRepository Проверяет непустую ссылку repository до 500 байт с косой чертой и без разделителя digest или пробелов; не разрешает registry и не проверяет владение.
func validRepository(input string) bool {
	return input != "" && len(input) <= 500 && !strings.ContainsAny(input, "@ \t\r\n") && strings.Contains(input, "/")
}

// validExternalRef Bounds a nonempty external reference to 1000 bytes and rejects whitespace; no URL scheme, network reachability or trust assertion is implied.
// validExternalRef Ограничивает непустую внешнюю ссылку 1000 байтами и запрещает пробелы; не подтверждает схему URL, сетевую доступность или доверие.
func validExternalRef(input string) bool {
	return input != "" && len(input) <= 1000 && !strings.ContainsAny(input, " \t\r\n")
}

// validImageReference Requires a bounded whitespace-free reference ending in the exact validated manifest digest, without making a registry read or proving signature/admission.
// validImageReference Требует ограниченную ссылку без пробелов с окончанием на точный проверенный digest манифеста, без чтения registry и доказательства подписи/допуска.
func validImageReference(reference, digest string) bool {
	return len(reference) <= 1000 && manifestPattern.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) && !strings.ContainsAny(reference, " \t\r\n")
}

// validDiagnostic Accepts either two absent diagnostic fields or a valid code with a nonempty single-line UTF-8 summary up to 256 bytes; this syntax check does not redact arbitrary content.
// validDiagnostic Допускает либо отсутствие обоих полей диагностики, либо корректный код с непустым однострочным UTF-8 описанием до 256 байт; проверка синтаксиса сама не скрывает произвольное содержимое.
func validDiagnostic(code, summary string) bool {
	return code == "" && summary == "" || errorCodePattern.MatchString(code) && utf8.ValidString(summary) &&
		len(summary) > 0 && len(summary) <= 256 && !strings.ContainsAny(summary, "\x00\r\n")
}

// progressStage Recognizes the fixed materialization-to-provenance build stage set and rejects terminal or arbitrary stage strings.
// progressStage Распознаёт фиксированный набор этапов сборки от materialization до provenance и отклоняет terminal или произвольные строки этапов.
func progressStage(stage string) bool {
	return slices.Contains([]string{"MATERIALIZATION", "CONTEXT_VALIDATION", "BASE_PULL", "SOLVING", "INSTALLATION", "TRUSTED_RUNTIME_FINALIZATION", "STAGING_PUSH", "PROVENANCE"}, stage)
}

// digest Hashes the JSON encoding of the supplied intent to lowercase SHA-256. Callers use JSON-marshalable structs; the existing implementation discards serialization errors rather than validating arbitrary values.
// digest Хеширует JSON-представление переданного намерения в строчный SHA-256. Вызывающие передают сериализуемые структуры; существующая реализация отбрасывает ошибку сериализации, не проверяя произвольные значения.
func digest(input any) string {
	encoded, _ := json.Marshal(input)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// firstError Returns the primary error when present and otherwise the fallback, preserving the declared validation precedence without joining errors.
// firstError Возвращает основную ошибку при её наличии, иначе резервную, сохраняя заданный приоритет проверок без объединения ошибок.
func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
