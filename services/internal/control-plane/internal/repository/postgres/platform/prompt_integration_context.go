package platform

import (
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strconv"
	"strings"
)

func promptIntegrationScope(grants []map[string]string, effective []string) map[string]any {
	items := make([]any, 0, len(grants))
	keys := make([]string, 0, len(grants))
	for _, grant := range filterIntegrationGrants(grants, effective) {
		version, _ := strconv.ParseInt(grant["grantVersion"], 10, 64)
		items = append(items, map[string]any{"ref": grant["ref"], "version": version, "capability": grant["capabilityKey"], "name": grant["capabilityName"], "description": grant["capabilityDescription"]})
		keys = append(keys, grant["capabilityKey"])
	}
	return map[string]any{"items": items, "summary": strings.Join(keys, ", ")}
}

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
