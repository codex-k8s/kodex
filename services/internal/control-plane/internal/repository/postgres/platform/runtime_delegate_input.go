package platform

import (
	"bytes"
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowDelegateInput decodes and validates the stored workflow version and original bounded
// input, mapping corrupt owner snapshots to Unavailable before checking additional input. It
// shallow-copies original values, accepts duplicate keys only when JSON encodings match and adds
// new keys before rechecking the merged budget; caller conflicts are Invalid. Additional keys are
// budget-checked rather than revalidated against the workflow schema here. Nested values remain
// shared and data never becomes authority.
// workflowDelegateInput декодирует и проверяет сохранённые workflow version и исходный bounded
// input, отображая повреждение owner snapshots в Unavailable до проверки additional input.
// Поверхностно копирует исходные значения, принимает повторный key только при совпадении
// JSON-кодирования и добавляет новые keys до повторной проверки общего бюджета; caller conflicts
// дают Invalid. Новые keys здесь проверяются по бюджету, а не повторно по workflow schema.
// Вложенные значения остаются общими, данные не становятся полномочиями.
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
