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

// validEvidenceSubjects requires each opted-in REVIEW/QA to name a direct predecessor with
// exactly one published PR predicate; REVIEW-only evidence cannot silently become a PR source.
// validEvidenceSubjects требует direct predecessor с ровно одним published PR для opt-in
// REVIEW/QA; REVIEW-only evidence не становится источником PR неявно.
func validEvidenceSubjects(version entity.WorkflowVersion) bool {
	if version.ResultEvidenceProfile != GitHubReviewQAV1 {
		return true
	}
	counts := map[string]int{}
	for _, step := range version.Steps {
		for _, requirement := range step.ResultRequirements {
			if requirement.Kind == "PULL_REQUEST" {
				counts[step.Key]++
			}
		}
	}
	for _, step := range version.Steps {
		for _, requirement := range step.ResultRequirements {
			if (requirement.Kind == "REVIEW" || requirement.Kind == "QA") && counts[requirement.SubjectStepKey] != 1 {
				return false
			}
		}
	}
	return true
}
