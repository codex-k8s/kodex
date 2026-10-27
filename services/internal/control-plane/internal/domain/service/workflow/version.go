package workflow

import (
	"encoding/json"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// MaximumServerTextBytes is the existing aggregate UTF-8 text budget of the server profile, independent of individual Unicode field limits.
// MaximumServerTextBytes — прежний суммарный UTF-8 бюджет текста серверного профиля, независимый от отдельных Unicode-ограничений полей.
const MaximumServerTextBytes = 8192

// OwnerApprovedUpstreamV1 is an opt-in immutable recovery policy, never a model capability.
// OwnerApprovedUpstreamV1 — opt-in immutable recovery policy, не capability модели.
const OwnerApprovedUpstreamV1 = "OWNER_APPROVED_UPSTREAM_V1"

// ServerTextBytes counts decoded workflow name, purpose, instructions and completion criteria plus each step's name, instructions and expected result. Keys, input metadata and result predicates are excluded; the count grants no authority and does not validate a workflow.
// ServerTextBytes считает байты декодированных имени, цели, инструкций и критерия завершения процесса плюс имени, инструкций и ожидаемого результата каждого шага. Ключи, метаданные входов и условия результатов исключены; подсчёт не выдаёт полномочия и не валидирует процесс.
func ServerTextBytes(version entity.WorkflowVersion) int {
	total := len(version.Name) + len(version.Purpose) + len(version.Instructions) + len(version.CompletionCriteria)
	for _, step := range version.Steps {
		total += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
	}
	return total
}

// ValidServerVersion validates the bounded server profile and independent REVIEW assignments.
// Existing generic workflow shape checks and fresh database admission remain mandatory; this
// predicate neither grants capabilities nor proves the absence of required child bindings.
// ValidServerVersion проверяет ограниченный серверный профиль и независимые назначения REVIEW.
// Прежняя проверка общей формы и свежий допуск БД остаются обязательными; предикат не выдаёт
// возможностей и не доказывает отсутствие привязок обязательного дочернего процесса.
func ValidServerVersion(version entity.WorkflowVersion) bool {
	if version.ExecutionMode != ServerDAGV1 || (version.ReworkPolicy != "" && version.ReworkPolicy != OwnerApprovedUpstreamV1) || len(version.Steps) < 1 || len(version.Steps) > MaximumSteps || version.Concurrency < 1 || version.Concurrency > 2 {
		return false
	}
	raw, err := json.Marshal(version)
	if err != nil || len(raw) > 1<<20 {
		return false
	}
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
	}
	if ServerTextBytes(version) > MaximumServerTextBytes {
		return false
	}
	reviewers := map[string]string{}
	for _, step := range version.Steps {
		for _, requirement := range step.ResultRequirements {
			if requirement.Kind != "REVIEW" && requirement.Kind != "ARTIFACT_REVIEW" {
				continue
			}
			if requirement.Kind == "ARTIFACT_REVIEW" {
				if version.ReworkPolicy != OwnerApprovedUpstreamV1 || !artifactReviewSubjectExists(version.Steps, requirement) {
					return false
				}
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

// artifactReviewSubjectExists resolves a declared output from the exact published prerequisite.
// It does not select files, trust model references or infer an author from an agent role.
// artifactReviewSubjectExists разрешает declared output exact published prerequisite.
// Функция не выбирает файлы, не доверяет model refs и не выводит автора из agent role.
func artifactReviewSubjectExists(steps []entity.WorkflowStep, requirement entity.WorkflowResultRequirement) bool {
	for _, step := range steps {
		if step.Key != requirement.SubjectStepKey {
			continue
		}
		for _, result := range step.ResultRequirements {
			if result.Key == requirement.SubjectResultKey && result.Kind == "ARTIFACT" {
				return true
			}
		}
	}
	return false
}
