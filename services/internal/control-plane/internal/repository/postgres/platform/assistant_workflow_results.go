package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assistantWorkflowMode preserves omitted legacy policy and rejects malformed explicit modes.
// The selector does not publish the workflow or authorize execution.
// assistantWorkflowMode сохраняет отсутствие прежней политики и отклоняет неверный явный режим.
// Селектор не публикует процесс и не разрешает исполнение.
func assistantWorkflowMode(input map[string]any) (string, bool) {
	raw, exists := input["executionMode"]
	if !exists {
		return "", true
	}
	value, valid := raw.(string)
	return value, valid && (value == "MANAGER_DRIVEN" || value == workflowservice.ServerDAGV1)
}

// assistantWorkflowRequirements parses only editable string predicates, retaining exact values
// for domain validation. Unknown keys, nulls and empty supplied fields cannot disappear as defaults.
// assistantWorkflowRequirements разбирает только редактируемые строковые условия, сохраняя точные
// значения для доменной проверки. Неизвестные ключи, null и пустые поля не исчезают как defaults.
func assistantWorkflowRequirements(raw any) ([]entity.WorkflowResultRequirement, error) {
	values, ok := raw.([]any)
	if !ok || len(values) < 1 || len(values) > 4 {
		return nil, errs.ErrInvalid
	}
	result := make([]entity.WorkflowResultRequirement, 0, len(values))
	for _, raw := range values {
		value, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(value, "key", "kind", "fileName", "mediaType", "repository", "subjectStepKey", "subjectResultKey", "operation", "targetRef") || !hasAssistantFields(value, "key", "kind") {
			return nil, errs.ErrInvalid
		}
		fields := make(map[string]string, len(value))
		for key, raw := range value {
			text, ok := raw.(string)
			if !ok || text == "" {
				return nil, errs.ErrInvalid
			}
			fields[key] = text
		}
		result = append(result, entity.WorkflowResultRequirement{Key: fields["key"], Kind: fields["kind"], FileName: fields["fileName"], MediaType: fields["mediaType"], Repository: fields["repository"], SubjectStepKey: fields["subjectStepKey"], SubjectResultKey: fields["subjectResultKey"], Operation: fields["operation"], TargetRef: fields["targetRef"]})
	}
	return result, nil
}

// assistantWorkflowDependencies copies explicit predecessor keys without trimming or repairing
// malformed input; the domain subsequently verifies membership, order, uniqueness and syntax.
// assistantWorkflowDependencies копирует явные ключи предшественников без исправления ввода;
// домен затем проверяет принадлежность, порядок, уникальность и форму.
func assistantWorkflowDependencies(raw any) ([]string, bool) {
	var values []string
	switch raw := raw.(type) {
	case []string:
		values = append([]string{}, raw...)
	case []any:
		values = make([]string, 0, len(raw))
		for _, value := range raw {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			values = append(values, text)
		}
	default:
		return nil, false
	}
	return values, len(values) <= 8
}

// assistantWorkflowRequirementFields renders a fresh editable snapshot without evidence or
// authority fields; absent optional predicates remain absent across plan hydration.
// assistantWorkflowRequirementFields создаёт редактируемый снимок без evidence и полномочий;
// отсутствующие необязательные условия остаются отсутствующими при подготовке плана.
func assistantWorkflowRequirementFields(values []entity.WorkflowResultRequirement) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		fields := map[string]any{"key": value.Key, "kind": value.Kind}
		for key, text := range map[string]string{"fileName": value.FileName, "mediaType": value.MediaType, "repository": value.Repository, "subjectStepKey": value.SubjectStepKey, "subjectResultKey": value.SubjectResultKey, "operation": value.Operation, "targetRef": value.TargetRef} {
			if text != "" {
				fields[key] = text
			}
		}
		result = append(result, fields)
	}
	return result
}

// assistantWorkflowReworkPolicy retains an omitted legacy selector and rejects null/unknown policies.
// It grants no authority and is accepted only together with the published server profile.
// assistantWorkflowReworkPolicy сохраняет отсутствующий legacy selector и отклоняет null/unknown policy.
// Полномочия не выдаются; policy принимается лишь вместе с published server profile.
func assistantWorkflowReworkPolicy(input map[string]any) (string, bool) {
	raw, present := input["reworkPolicy"]
	if !present {
		return "", true
	}
	value, valid := raw.(string)
	mode, modeValid := assistantWorkflowMode(input)
	return value, valid && modeValid && (value == "" || mode == workflowservice.ServerDAGV1 && value == workflowservice.OwnerApprovedUpstreamV1)
}
