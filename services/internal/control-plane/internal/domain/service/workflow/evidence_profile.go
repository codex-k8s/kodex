package workflow

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// GitHubReviewQAV1 selects explicit COMMENT receipts and captured QA reports; absence preserves
// the historical verdict/head-only profile and never authorizes an external write implicitly.
// GitHubReviewQAV1 выбирает явные COMMENT receipts и captured QA reports; отсутствие сохраняет
// прежний verdict/head-only профиль и не разрешает внешний WRITE неявно.
const GitHubReviewQAV1 = "GITHUB_REVIEW_QA_V1"

// validEvidenceRequirementRefs binds one QA report to a distinct same-step JSON ARTIFACT and
// permits at most one COMMENT and one aggregate issue slot per published step/business attempt.
// validEvidenceRequirementRefs связывает QA report с отдельным JSON ARTIFACT того же шага,
// допуская максимум один COMMENT и один агрегированный issue slot на шаг/business attempt.
func validEvidenceRequirementRefs(profile string, requirements []entity.WorkflowResultRequirement) bool {
	if profile == "" {
		return true
	}
	counts := map[string]int{}
	for _, requirement := range requirements {
		if requirement.Kind != "REVIEW" && requirement.Kind != "QA" {
			continue
		}
		counts[requirement.Kind]++
		if counts[requirement.Kind] > 1 {
			return false
		}
		if requirement.Kind != "QA" {
			continue
		}
		found := false
		for _, report := range requirements {
			found = found || report.Key == requirement.EvidenceKey && report.Kind == "ARTIFACT" && report.MediaType == "application/json"
		}
		if !found {
			return false
		}
	}
	return true
}
