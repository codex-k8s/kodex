package platform

import (
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strconv"
	"strings"
)

// promptIntegrationScope projects callable grants filtered by the effective capability set into
// fresh nonnil descriptor/item slices and a comma-separated key summary, retaining order and
// duplicates. It copies selected scalar metadata and parses grantVersion with ignored errors, so
// malformed text yields the existing ParseInt fallback result. It performs no live grant lookup,
// pin validation or invocation authorization.
// promptIntegrationScope проецирует callable grants, отфильтрованные effective capability set, в
// новые ненулевые descriptor/item slices и summary keys через запятую с сохранением порядка и
// повторов. Копирует выбранные scalar metadata и разбирает grantVersion с игнорированием ошибки,
// поэтому неверный текст сохраняет прежний fallback результат ParseInt. Не читает live grants, не
// проверяет pins и не авторизует invocation.
func promptIntegrationScope(grants []map[string]string, effective []string) map[string]any {
	items := make([]any, 0, len(grants))
	keys := make([]string, 0, len(grants))
	for _, grant := range filterIntegrationGrants(grants, effective) {
		version, _ := strconv.ParseInt(grant["grantVersion"], 10, 64)
		items = append(items, map[string]any{"ref": grant["ref"], "version": version, "approvalPolicy": grant["approvalPolicy"], "capability": grant["capabilityKey"], "name": grant["capabilityName"], "description": grant["capabilityDescription"]})
		keys = append(keys, grant["capabilityKey"])
	}
	return map[string]any{"items": items, "summary": strings.Join(keys, ", ")}
}

// narrowPromptIntegrationScope intersects user, agent/connection, workflow and Human Gate
// capability sets and replaces only the structured integrations projection with matching existing
// descriptor maps. Malformed item shapes are skipped; order, duplicate keys and descriptor aliases
// remain. Callers must initialize StructuredVariables. Narrowing display metadata neither updates
// grants nor validates a command or descriptor version.
// narrowPromptIntegrationScope пересекает user, agent/connection, workflow и Human Gate capability
// sets и заменяет только structured integrations projection совпадающими существующими descriptor
// maps. Неверные item shapes пропускаются; порядок, повторные keys и descriptor aliases
// сохраняются. Вызывающий обязан инициализировать StructuredVariables. Сужение display metadata не
// меняет grants и не проверяет команду или descriptor version.
func narrowPromptIntegrationScope(snapshot *entity.PromptMaterializationSnapshot) {
	effective := promptservice.Intersection(snapshot.UserCapabilities, promptservice.Union(snapshot.AgentCapabilities, snapshot.ConnectionCapabilities), snapshot.WorkflowCapabilities, snapshot.HumanGateCapabilities)
	scope, _ := snapshot.StructuredVariables["integrations"].(map[string]any)
	items, _ := scope["items"].([]any)
	selected := make([]any, 0, len(items))
	keys := make([]string, 0, len(items))
	for _, item := range items {
		descriptor, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := descriptor["capability"].(string)
		if contains(effective, key) {
			selected = append(selected, descriptor)
			keys = append(keys, key)
		}
	}
	snapshot.StructuredVariables["integrations"] = map[string]any{"items": selected, "summary": strings.Join(keys, ", ")}
}
