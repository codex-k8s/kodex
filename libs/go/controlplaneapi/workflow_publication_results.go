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
		return value.ExecutionMode == "" && value.ReworkPolicy == ""
	case 2:
		textBytes := len(value.Name) + len(value.Purpose) + len(value.Instructions) + len(value.CompletionCriteria)
		for _, step := range value.Steps {
			textBytes += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
		}
		return (value.ReworkPolicy == "" || value.ReworkPolicy == "OWNER_APPROVED_UPSTREAM_V1") && value.ExecutionMode == workflowServerMode && value.Concurrency <= 2 && len(value.Steps) <= 12 && textBytes <= 8192
	default:
		return false
	}
}

// validWorkflowPublicationServerStep checks the bounded structural contract before canonical
// encoding. Exact grants, assigned-agent independence and evidence acceptance belong to the owner.
// validWorkflowPublicationServerStep проверяет ограниченный структурный контракт до канонического
// кодирования. Точные grants, независимость сотрудников и приёмка доказательств принадлежат владельцу.
func validWorkflowPublicationServerStep(step WorkflowPublicationStep) bool {
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
		if !workflowResultKey.MatchString(requirement.Key) || seen[requirement.Key] || len(requirement.Repository) > 200 || requirement.Kind != "DEPLOYMENT" && requirement.TargetRef != "" || requirement.Kind != "ARTIFACT_REVIEW" && requirement.SubjectResultKey != "" {
			return false
		}
		seen[requirement.Key] = true
		switch requirement.Kind {
		case "ARTIFACT":
			if requirement.FileName == "" || requirement.FileName == "." || requirement.FileName == ".." || requirement.FileName == "result.md" || requirement.FileName == "workspace-write-result.json" || len(requirement.FileName) > 160 || !utf8.ValidString(requirement.FileName) || strings.ContainsAny(requirement.FileName, "/\\\x00\r\n") || requirement.MediaType == "" || len(requirement.MediaType) > 160 || !utf8.ValidString(requirement.MediaType) || strings.ContainsAny(requirement.MediaType, "\x00\r\n") || requirement.Repository != "" || requirement.SubjectStepKey != "" || requirement.Operation != "" {
				return false
			}
		case "ARTIFACT_REVIEW":
			if !dependencies[requirement.SubjectStepKey] || !workflowResultKey.MatchString(requirement.SubjectResultKey) || requirement.FileName != "" || requirement.MediaType != "" || requirement.Repository != "" || requirement.Operation != "" {
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
	return true
}

// validWorkflowPublicationReviews preserves exact published subject/result keys and independent
// reviewer identity. It checks configuration shape only, never evidence or execution grants.
// validWorkflowPublicationReviews сохраняет exact published subject/result keys и независимость
// reviewer identity. Проверяется лишь configuration shape, не evidence или execution grants.
func validWorkflowPublicationReviews(value WorkflowPublication, step WorkflowPublicationStep) bool {
	for _, requirement := range step.ResultRequirements {
		if requirement.Kind != "ARTIFACT_REVIEW" {
			continue
		}
		if value.ReworkPolicy != "OWNER_APPROVED_UPSTREAM_V1" {
			return false
		}
		found := false
		for _, subject := range value.Steps {
			if subject.Key != requirement.SubjectStepKey {
				continue
			}
			if subject.AgentRef == step.AgentRef {
				return false
			}
			for _, result := range subject.ResultRequirements {
				if result.Key == requirement.SubjectResultKey && result.Kind == "ARTIFACT" {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
