package roleimage

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetVulnerabilityReport(ctx context.Context, p value.Principal, filter repo.VulnerabilityReportFilter) (entity.ImageVulnerabilityReport, error) {
	p, err := service.resolvePrincipal(ctx, p)
	if err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	permission := "platform.query.role-images.vulnerability-report.get"
	if filter.ScopeKind == "ORGANIZATION" {
		permission = "platform.query.organization.role-images.vulnerability-report.get"
	}
	if err := authorize(p, permission, "control-api-gateway"); err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	if p.ProjectRef != "" {
		return entity.ImageVulnerabilityReport{}, errs.ErrForbidden
	}
	if !validRiskScope(filter.ScopeKind, filter.ProjectRef) || !validRef(filter.RecipeRef, "imgrec") || !validRef(filter.ArtifactRef, "imgart") ||
		filter.Page.Size < 0 || filter.Page.Size > 100 || len(filter.Page.Token) > 512 ||
		!validRiskQuery(filter.PackageQuery) || !validRiskQuery(filter.AdvisoryQuery) ||
		filter.ExpectedReportSHA256 != "" && !sha256Pattern.MatchString(filter.ExpectedReportSHA256) ||
		filter.Severity != "" && !validRiskSeverity(filter.Severity) || filter.Page.Token != "" && filter.ExpectedReportSHA256 == "" {
		return entity.ImageVulnerabilityReport{}, errs.ErrInvalid
	}
	return service.repository.GetVulnerabilityReport(ctx, p, filter)
}

func (service *Service) DecideAdmissionRisk(ctx context.Context, input repo.AdmissionRiskInput) (entity.ImageAdmissionRiskResult, error) {
	p, err := service.resolvePrincipal(ctx, input.Principal)
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	input.Principal = p
	permission := "platform.command.role-images.risk.decide"
	if input.ScopeKind == "ORGANIZATION" {
		permission = "platform.command.organization.role-images.risk.decide"
	}
	if err := authorize(p, permission, "control-api-gateway"); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	if p.ProjectRef != "" {
		return entity.ImageAdmissionRiskResult{}, errs.ErrForbidden
	}
	input.Mutation.Operation = permission
	intent := input
	intent.Principal = value.Principal{}
	intent.Mutation.IntentDigest = ""
	input.Mutation.IntentDigest = digest(intent)
	for _, pin := range []uint64{input.ExpectedArtifactVersion, input.ExpectedAdmissionRevision, input.ExpectedRecipeVersion, input.ExpectedRecipeGeneration, input.PolicyRevision} {
		if pin > 9007199254740991 {
			return entity.ImageAdmissionRiskResult{}, errs.ErrInvalid
		}
	}
	if !validRiskScope(input.ScopeKind, input.ProjectRef) || !validRef(input.RecipeRef, "imgrec") || !validRef(input.ArtifactRef, "imgart") ||
		!validRef(input.ExpectedBuildRef, "imgbld") || input.ExpectedArtifactVersion == 0 || input.ExpectedAdmissionRevision == 0 ||
		input.ExpectedRecipeVersion == 0 || input.ExpectedRecipeGeneration == 0 || input.ExpectedBuildAttempt == 0 || input.ExpectedBuildAttempt > 10 || input.PolicyRevision == 0 ||
		!manifestPattern.MatchString(input.ManifestDigest) || !manifestPattern.MatchString(input.PriorEvidenceManifestDigest) ||
		!sha256Pattern.MatchString(input.VulnerabilityEvidenceSHA256) || !sha256Pattern.MatchString(input.ProjectionSHA256) ||
		!sha256Pattern.MatchString(input.PriorAdmissionReceiptSHA256) || !sha256Pattern.MatchString(input.PolicySHA256) ||
		(input.Action != "ACCEPT_RISK" && input.Action != "REJECT_RISK") || !validRiskReason(input.Reason) ||
		input.Mutation.ExpectedVersion == nil || uint64(*input.Mutation.ExpectedVersion) != input.ExpectedArtifactVersion || input.Mutation.Validate() != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrInvalid
	}
	return service.repository.DecideAdmissionRisk(ctx, input)
}

func validRiskScope(scope, project string) bool {
	return scope == "ORGANIZATION" && project == "" || scope == "PROJECT" && validRef(project, "prj")
}
func validRiskSeverity(value string) bool {
	switch value {
	case "CRITICAL", "HIGH", "MEDIUM", "LOW", "NEGLIGIBLE", "UNKNOWN":
		return true
	}
	return false
}
func validRiskQuery(value string) bool {
	return len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}
func validRiskReason(value string) bool {
	return len(value) > 0 && len(value) <= 2048 && strings.TrimSpace(value) == value && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}
