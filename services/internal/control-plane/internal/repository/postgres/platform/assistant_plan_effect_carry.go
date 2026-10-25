package platform

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// carryAssistantPlanBindingPins shallow-copies Input, Parameters, Before and After into nonnil
// top-level maps and replaces only keys already present in each map with supplied pins. Nested
// values remain shared; unrelated operation fields and the original maps are untouched. The caller
// must supply pins from a previous canonical BIND of the same agent in the current owner
// transaction. It does not validate pin provenance, create missing keys, persist an immutable plan
// or authorize a later command.
// carryAssistantPlanBindingPins поверхностно копирует Input, Parameters, Before и After в
// ненулевые внешние maps и заменяет заданными pins только уже имеющиеся в каждой map keys.
// Вложенные значения остаются общими; прочие поля operation и исходные maps не меняются.
// Вызывающий обязан передавать pins предыдущего canonical BIND того же agent в текущей
// owner-транзакции. Не проверяет provenance pins, не создаёт отсутствующие keys, не сохраняет
// immutable plan и не авторизует следующую команду.
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
