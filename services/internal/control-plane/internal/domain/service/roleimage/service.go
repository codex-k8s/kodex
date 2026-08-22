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

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	permissionListRecipes        = "platform.role-images.recipes.list"
	permissionListEnvironments   = "platform.role-images.environments.list"
	permissionGetRecipe          = "platform.role-images.recipes.get"
	permissionManageRecipe       = "platform.role-images.recipes.manage"
	permissionClaimBuild         = "platform.role-images.builds.claim"
	permissionRenewBuild         = "platform.role-images.builds.renew"
	permissionProgressBuild      = "platform.role-images.builds.progress"
	permissionCompleteBuild      = "platform.role-images.builds.complete"
	permissionFailBuild          = "platform.role-images.builds.fail"
	permissionClaimAdmission     = "platform.role-images.admission.claim"
	permissionRecordAdmission    = "platform.role-images.admission.record"
	permissionClaimPromotion     = "platform.role-images.promotion.claim"
	permissionAuthorizePromotion = "platform.role-images.promotion.authorize"
	permissionCompletePromotion  = "platform.role-images.promotion.complete"
)

var (
	sha256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	manifestPattern    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	namePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@/~:-]{0,255}$`)
	errorCodePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	signatureIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)
)

type Service struct {
	repository repository.Repository
	catalog    *Catalog
}

func New(repo repository.Repository, catalog *Catalog) (*Service, error) {
	if repo == nil || catalog == nil {
		return nil, errors.New("role image repository is required")
	}
	return &Service{repository: repo, catalog: catalog}, nil
}

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

func (service *Service) List(ctx context.Context, principal value.Principal, filter repository.Filter) ([]entity.RoleImageRecipe, string, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if err := authorize(principal, permissionListRecipes, "control-api-gateway"); err != nil || !validRef(filter.ProjectRef, "prj") {
		return nil, "", firstError(err, errs.ErrInvalid)
	}
	return service.repository.List(ctx, principal, filter)
}

func (service *Service) Get(ctx context.Context, principal value.Principal, ref string) (entity.RoleImageRecipe, []entity.ImageBuild, *entity.ImageArtifact, error) {
	principal, err := service.resolvePrincipal(ctx, principal)
	if err != nil {
		return entity.RoleImageRecipe{}, nil, nil, err
	}
	if err := authorize(principal, permissionGetRecipe, "control-api-gateway"); err != nil || !validRef(ref, "imgrec") {
		return entity.RoleImageRecipe{}, nil, nil, firstError(err, errs.ErrInvalid)
	}
	return service.repository.Get(ctx, principal, ref)
}

func (service *Service) Manage(ctx context.Context, input repository.ManageInput) (repository.ManageResult, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return repository.ManageResult{}, err
	}
	input.Principal = principal
	if err := authorize(input.Principal, permissionManageRecipe, "control-api-gateway"); err != nil {
		return repository.ManageResult{}, err
	}
	input.Action = strings.ToUpper(strings.TrimSpace(input.Action))
	if input.Action == "CREATE" || input.Action == "UPDATE" {
		input.Recipe, err = service.catalog.Resolve(input.Environment)
		if err != nil {
			return repository.ManageResult{}, err
		}
	}
	input.Mutation.Operation = "role-image-recipe." + strings.ToLower(input.Action)
	input.Mutation.IntentDigest = digest(struct {
		Action, RecipeRef, ProjectRef, RoleDefinitionRef, Name string
		Recipe                                                 entity.RoleImageRecipeInput
	}{input.Action, input.RecipeRef, input.ProjectRef, input.RoleDefinitionRef, input.Name, input.Recipe})
	if err := input.Mutation.Validate(); err != nil {
		return repository.ManageResult{}, errs.ErrInvalid
	}
	switch input.Action {
	case "CREATE":
		if !validRef(input.ProjectRef, "prj") || !validRef(input.RoleDefinitionRef, "role") ||
			!validDisplayName(input.Name) || validateRecipe(input.Recipe) != nil || input.Mutation.ExpectedVersion != nil || input.RecipeRef != "" {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	case "UPDATE":
		if !validRef(input.ProjectRef, "prj") || !validRef(input.RecipeRef, "imgrec") || input.RoleDefinitionRef != "" ||
			!validDisplayName(input.Name) || validateRecipe(input.Recipe) != nil || input.Mutation.ExpectedVersion == nil {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	case "ARCHIVE", "RESTORE", "REQUEST_BUILD":
		if !validRef(input.ProjectRef, "prj") || !validRef(input.RecipeRef, "imgrec") ||
			input.Mutation.ExpectedVersion == nil || input.RoleDefinitionRef != "" ||
			input.Environment.EnvironmentKey != "" || len(input.Environment.PackageKeys) != 0 ||
			len(input.Environment.ToolKeys) != 0 || input.Environment.InstallationBlock != "" {
			return repository.ManageResult{}, errs.ErrInvalid
		}
	default:
		return repository.ManageResult{}, errs.ErrInvalid
	}
	return service.repository.Manage(ctx, input)
}

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

func (service *Service) RecordAdmission(ctx context.Context, input repository.AdmissionRecordInput) (entity.ImageArtifact, error) {
	principal, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageArtifact{}, err
	}
	input.Principal = principal
	if err := authorizeKey(input.Principal, permissionRecordAdmission, "image-admission", input.IdempotencyKey); err != nil ||
		!validRef(input.ArtifactRef, "imgart") || input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ClaimToken == "" ||
		!manifestPattern.MatchString(input.ManifestDigest) || !sha256Pattern.MatchString(input.ImmutableBuildSHA256) ||
		!sha256Pattern.MatchString(input.ProvenanceSHA256) || !sha256Pattern.MatchString(input.SBOMSHA256) ||
		!sha256Pattern.MatchString(input.VulnerabilityEvidenceSHA256) || input.PolicyRevision == 0 ||
		!sha256Pattern.MatchString(input.PolicySHA256) || (input.Verdict != "ACCEPTED" && input.Verdict != "REJECTED") ||
		!signatureIDPattern.MatchString(input.SignatureIdentity) || !sha256Pattern.MatchString(input.SignatureSHA256) ||
		!sha256Pattern.MatchString(input.AdmissionReceiptSHA256) || !manifestPattern.MatchString(input.AdmissionReceiptOCIManifestDigest) {
		return entity.ImageArtifact{}, firstError(err, errs.ErrInvalid)
	}
	return service.repository.RecordAdmission(ctx, input)
}

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

func (service *Service) resolvePrincipal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrUnauthorized
	}
	return service.repository.ResolvePrincipal(ctx, principal)
}

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

func authorizeKey(principal value.Principal, permission, workload, key string) error {
	if err := authorize(principal, permission, workload); err != nil {
		return err
	}
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key {
		return errs.ErrInvalid
	}
	return nil
}

func authorize(principal value.Principal, permission, workload string) error {
	if err := principal.Validate(); err != nil {
		return errs.ErrUnauthorized
	}
	if principal.CallerWorkload != workload || principal.Permission != permission {
		return errs.ErrForbidden
	}
	return nil
}

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

func validDisplayName(input string) bool {
	return strings.TrimSpace(input) == input && utf8.ValidString(input) && len(input) > 0 && len(input) <= 160
}

func validRef(input, prefix string) bool {
	if len(input) < len(prefix)+9 || len(input) > 96 || !strings.HasPrefix(input, prefix+"_") {
		return false
	}
	for _, character := range input[len(prefix)+1:] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validRepository(input string) bool {
	return input != "" && len(input) <= 500 && !strings.ContainsAny(input, "@ \t\r\n") && strings.Contains(input, "/")
}

func validExternalRef(input string) bool {
	return input != "" && len(input) <= 1000 && !strings.ContainsAny(input, " \t\r\n")
}

func validImageReference(reference, digest string) bool {
	return len(reference) <= 1000 && manifestPattern.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) && !strings.ContainsAny(reference, " \t\r\n")
}

func validDiagnostic(code, summary string) bool {
	return code == "" && summary == "" || errorCodePattern.MatchString(code) && utf8.ValidString(summary) &&
		len(summary) > 0 && len(summary) <= 256 && !strings.ContainsAny(summary, "\x00\r\n")
}

func progressStage(stage string) bool {
	return slices.Contains([]string{"MATERIALIZATION", "CONTEXT_VALIDATION", "BASE_PULL", "SOLVING", "INSTALLATION", "TRUSTED_RUNTIME_FINALIZATION", "STAGING_PUSH", "PROVENANCE"}, stage)
}

func digest(input any) string {
	encoded, _ := json.Marshal(input)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
