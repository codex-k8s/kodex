package workflow

import (
	"encoding/json"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ValidServerVersion validates the bounded server profile and independent REVIEW assignments.
// Existing generic workflow shape checks and fresh database admission remain mandatory; this
// predicate neither grants capabilities nor proves the absence of required child bindings.
// ValidServerVersion проверяет ограниченный серверный профиль и независимые назначения REVIEW.
// Прежняя проверка общей формы и свежий допуск БД остаются обязательными; предикат не выдаёт
// возможностей и не доказывает отсутствие привязок обязательного дочернего процесса.
func ValidServerVersion(version entity.WorkflowVersion) bool {
	if version.ExecutionMode != ServerDAGV1 || len(version.Steps) < 1 || len(version.Steps) > MaximumSteps || version.Concurrency < 1 || version.Concurrency > 2 {
		return false
	}
	raw, err := json.Marshal(version)
	if err != nil || len(raw) > 1<<20 {
		return false
	}
	textBytes := len(version.Name) + len(version.Purpose) + len(version.Instructions) + len(version.CompletionCriteria)
	agents := map[string]string{}
	for _, step := range version.Steps {
		if !resultKey.MatchString(step.Key) || agents[step.Key] != "" || step.AgentRef == "" || len(step.DependsOn) > 8 || slices.Contains(step.RequiredCapabilityKeys, "platform.run.delegate") || slices.Contains(step.RequiredCapabilityKeys, "platform.run.launch") || !ValidRequirements(step.ResultRequirements, step.DependsOn) {
			return false
		}
		seen := map[string]bool{}
		for _, dependency := range step.DependsOn {
			if agents[dependency] == "" || seen[dependency] {
				return false
			}
			seen[dependency] = true
		}
		agents[step.Key] = step.AgentRef
		textBytes += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
	}
	if textBytes > 8192 {
		return false
	}
	reviewers := map[string]string{}
	for _, step := range version.Steps {
		for _, requirement := range step.ResultRequirements {
			if requirement.Kind != "REVIEW" {
				continue
			}
			key := requirement.SubjectStepKey + ":" + step.AgentRef
			if agents[requirement.SubjectStepKey] == step.AgentRef || reviewers[key] != "" && reviewers[key] != step.Key {
				return false
			}
			reviewers[key] = step.Key
		}
	}
	return true
}
