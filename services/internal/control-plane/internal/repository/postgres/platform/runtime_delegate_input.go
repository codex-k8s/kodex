package platform

import (
	"bytes"
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// Исходные поля — данные exact Workflow root/version, не источник полномочий.
// Ошибка сохранённого snapshot отличается от недопустимого caller input.
func workflowDelegateInput(rootInput, workflowSpec []byte, additional map[string]any) (map[string]any, error) {
	var version entity.WorkflowVersion
	var original map[string]any
	if json.Unmarshal(workflowSpec, &version) != nil || !validWorkflowVersion(version) ||
		json.Unmarshal(rootInput, &original) != nil || !validBoundedRunInput(original) ||
		!validWorkflowRunInput(version.Inputs, original) {
		return nil, errs.ErrUnavailable
	}
	if !validBoundedRunInput(additional) {
		return nil, errs.ErrInvalid
	}
	merged := make(map[string]any, len(original)+len(additional))
	for key, value := range original {
		merged[key] = value
	}
	for key, value := range additional {
		if previous, exists := original[key]; exists {
			before, beforeErr := json.Marshal(previous)
			after, afterErr := json.Marshal(value)
			if beforeErr != nil || afterErr != nil || !bytes.Equal(before, after) {
				return nil, errs.ErrInvalid
			}
			continue
		}
		merged[key] = value
	}
	if !validBoundedRunInput(merged) {
		return nil, errs.ErrInvalid
	}
	return merged, nil
}
