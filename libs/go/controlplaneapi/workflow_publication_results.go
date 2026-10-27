package controlplaneapi

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// workflowServerMode selects the closed second-version transport profile, not runtime authority.
	// workflowServerMode выбирает закрытый профиль второй версии, не предоставляя runtime полномочий.
	workflowServerMode = "SERVER_DAG_V1"
)

var (
	// workflowResultKey bounds shared step/result keys without permitting URLs or opaque selectors.
	// workflowResultKey ограничивает общие ключи этапов/результатов, исключая URL и произвольные селекторы.
	workflowResultKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
	// workflowResultRepository admits only owner/name syntax; actual repository eligibility is external.
	// workflowResultRepository допускает только owner/name; фактический допуск репозитория проверяется отдельно.
	workflowResultRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// validWorkflowPublicationMode preserves the exact legacy wire and bounds the new explicit profile.
// Historical snapshots are never interpreted as a new mode because they contain familiar fields.
// validWorkflowPublicationMode сохраняет точную старую передачу и ограничивает новый явный профиль.
// Исторический снимок не становится новым режимом только из-за наличия знакомых полей.
func validWorkflowPublicationMode(value WorkflowPublication) bool {
	switch value.Version {
	case 1:
		return value.ExecutionMode == "" && value.ResultEvidenceProfile == ""
	case 2, 3:
		if value.Version == 2 && value.ResultEvidenceProfile != "" || value.Version == 3 && value.ResultEvidenceProfile != "GITHUB_REVIEW_QA_V1" {
			return false
		}
		textBytes := len(value.Name) + len(value.Purpose) + len(value.Instructions) + len(value.CompletionCriteria)
		for _, step := range value.Steps {
			textBytes += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
		}
		return value.ExecutionMode == workflowServerMode && value.Concurrency <= 2 && len(value.Steps) <= 12 && textBytes <= 8192
	default:
		return false
	}
}

// validWorkflowPublicationServerStep checks the bounded structural contract before canonical
// encoding. Exact grants, assigned-agent independence and evidence acceptance belong to the owner.
// validWorkflowPublicationServerStep проверяет ограниченный структурный контракт до канонического
// кодирования. Точные grants, независимость сотрудников и приёмка доказательств принадлежат владельцу.
func validWorkflowPublicationServerStep(step WorkflowPublicationStep) bool {
	return validWorkflowPublicationServerStepForProfile(step, "")
}

// validWorkflowPublicationServerStepForProfile preserves old predicates and requires the explicit
// profile to bind QA to one same-step JSON report; syntax never proves capture or execution.
// validWorkflowPublicationServerStepForProfile сохраняет прежние условия и связывает QA с одним
// JSON report того же шага только явного профиля; синтаксис не доказывает capture либо execution.
func validWorkflowPublicationServerStepForProfile(step WorkflowPublicationStep, profile string) bool {
	if !workflowResultKey.MatchString(step.Key) || len(step.DependsOn) > 8 || len(step.ResultRequirements) < 1 || len(step.ResultRequirements) > 4 {
		return false
	}
	dependencies := map[string]bool{}
	for _, key := range step.DependsOn {
		if !workflowResultKey.MatchString(key) || dependencies[key] {
			return false
		}
		dependencies[key] = true
	}
	if slices.Contains(step.RequiredCapabilityKeys, "platform.run.delegate") || slices.Contains(step.RequiredCapabilityKeys, "platform.run.launch") {
		return false
	}
	seen := map[string]bool{}
	for _, requirement := range step.ResultRequirements {
		if requirement.EvidenceKey != "" && (profile != "GITHUB_REVIEW_QA_V1" || requirement.Kind != "QA") || profile == "GITHUB_REVIEW_QA_V1" && requirement.Kind == "QA" && !workflowResultKey.MatchString(requirement.EvidenceKey) {
			return false
		}
		if !workflowResultKey.MatchString(requirement.Key) || seen[requirement.Key] || len(requirement.Repository) > 200 || requirement.Kind != "DEPLOYMENT" && requirement.TargetRef != "" {
			return false
		}
		seen[requirement.Key] = true
		switch requirement.Kind {
		case "ARTIFACT":
			if requirement.FileName == "" || requirement.FileName == "." || requirement.FileName == ".." || requirement.FileName == "result.md" || requirement.FileName == "workspace-write-result.json" || len(requirement.FileName) > 160 || !utf8.ValidString(requirement.FileName) || strings.ContainsAny(requirement.FileName, "/\\\x00\r\n") || requirement.MediaType == "" || len(requirement.MediaType) > 160 || !utf8.ValidString(requirement.MediaType) || strings.ContainsAny(requirement.MediaType, "\x00\r\n") || requirement.Repository != "" || requirement.SubjectStepKey != "" || requirement.Operation != "" {
				return false
			}
		case "PULL_REQUEST":
			if !workflowResultRepository.MatchString(requirement.Repository) || requirement.FileName != "" || requirement.MediaType != "" || requirement.SubjectStepKey != "" || requirement.Operation != "" {
				return false
			}
		case "REVIEW", "QA":
			if !dependencies[requirement.SubjectStepKey] || requirement.FileName != "" || requirement.MediaType != "" || requirement.Repository != "" || requirement.Operation != "" {
				return false
			}
		case "INTEGRATION_RECEIPT":
			if !dependencies[requirement.SubjectStepKey] || !workflowResultRepository.MatchString(requirement.Repository) || requirement.Operation != "github.pull_request.merge" || requirement.FileName != "" || requirement.MediaType != "" {
				return false
			}
		case "DEPLOYMENT":
			if !dependencies[requirement.SubjectStepKey] || !workflowResultRepository.MatchString(requirement.Repository) || requirement.Operation != "ssh.compose.deploy" || !strings.HasPrefix(requirement.TargetRef, "ssh_") || len(requirement.TargetRef) > 96 || requirement.FileName != "" || requirement.MediaType != "" {
				return false
			}
		default:
			return false
		}
	}
	if profile == "GITHUB_REVIEW_QA_V1" {
		counts := map[string]int{}
		for _, requirement := range step.ResultRequirements {
			if requirement.Kind != "REVIEW" && requirement.Kind != "QA" {
				continue
			}
			counts[requirement.Kind]++
			if counts[requirement.Kind] > 1 {
				return false
			}
			if requirement.Kind == "QA" {
				found := false
				for _, report := range step.ResultRequirements {
					found = found || report.Key == requirement.EvidenceKey && report.Kind == "ARTIFACT" && report.MediaType == "application/json"
				}
				if !found {
					return false
				}
			}
		}
	}
	return true
}
