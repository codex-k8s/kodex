package platform

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// Pins поступают только из результата предыдущего canonical BIND этого же
// agent в текущей owner-транзакции. Persisted immutable plan не изменяется.
func carryAssistantPlanBindingPins(operation entity.AssistantPlanOperation, pins map[string]any) entity.AssistantPlanOperation {
	operation.Input = cloneAssistantFields(operation.Input)
	operation.Parameters = cloneAssistantFields(operation.Parameters)
	operation.Before = cloneAssistantFields(operation.Before)
	operation.After = cloneAssistantFields(operation.After)
	for _, fields := range []map[string]any{operation.Input, operation.Parameters, operation.Before, operation.After} {
		for key, value := range pins {
			if _, exists := fields[key]; exists {
				fields[key] = value
			}
		}
	}
	return operation
}
