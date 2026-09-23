package callback

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const maximumAssistantDiscoveredSchemas = 4
const maximumAssistantCatalogAgents = 20

func configurationCatalogTool(input runtimecontract.RunnerInput) map[string]any {
	return map[string]any{
		"name":        "get_configuration_catalog",
		"description": "Discover server-owned context and permitted operation types. Pass operation_types=[] for a compact index, then request up to four exact schemas needed for the current task. Agents are returned in pages of at most 20; use agent_query and agent_offset to find a target, and do not infer absence until pages are exhausted. Names are display data; use only exact opaque refs in plans.",
		"inputSchema": objectSchema(nil, map[string]any{
			"operation_types": map[string]any{"type": "array", "maxItems": maximumAssistantDiscoveredSchemas,
				"uniqueItems": true, "items": map[string]any{"type": "string", "enum": assistantOperationTypes(input)}},
			"agent_query":  stringSchema(0, 80),
			"agent_offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 128},
		}),
		"outputSchema": objectSchema([]string{"current_project_ref", "agents"}, map[string]any{
			"current_project_ref": map[string]any{"type": "string"}, "agents": map[string]any{"type": "array", "maxItems": maximumAssistantCatalogAgents, "items": map[string]any{"type": "object"}},
			"agent_total":       map[string]any{"type": "integer", "minimum": 0},
			"agent_next_offset": map[string]any{"type": "integer", "minimum": 0},
			"context":           map[string]any{"type": "object"},
			"operation_types":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"operation_schemas": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		}),
	}
}

func assistantMetadataTool() map[string]any {
	return map[string]any{
		"name": "propose_assistant_metadata", "description": "Propose a concise conversation title. The control-plane owns the accepted title and never overwrites a user-edited title.",
		"inputSchema": objectSchema([]string{"title"}, map[string]any{"title": stringSchema(1, 160)}),
		"outputSchema": objectSchema([]string{"ok", "conversation_ref", "title_revision"}, map[string]any{
			"ok": map[string]any{"type": "boolean"}, "conversation_ref": opaqueRefSchema(), "title_revision": map[string]any{"type": "integer", "minimum": 1},
		}),
	}
}

func runMetadataTool() map[string]any {
	return map[string]any{
		"name": "propose_run_metadata", "description": "Propose a concise server-owned Run title and bounded current activity summary. Do not include secrets, raw tool output, or user payloads.",
		"inputSchema": objectSchema([]string{"title", "activity_summary"}, map[string]any{
			"title": stringSchema(0, 240), "activity_summary": stringSchema(0, 500),
		}),
		"outputSchema": objectSchema([]string{"ok", "run_ref"}, map[string]any{"ok": map[string]any{"type": "boolean"}, "run_ref": opaqueRefSchema()}),
	}
}

func configurationCatalog(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if !input.SystemAssistant || !onlyKeys(arguments, "operation_types", "agent_query", "agent_offset") {
		return nil, errors.New("configuration catalog is not available")
	}
	agentQuery := ""
	if raw, supplied := arguments["agent_query"]; supplied {
		query, ok := raw.(string)
		if !ok || utf8.RuneCountInString(query) > 80 {
			return nil, errors.New("configuration catalog agent query is invalid")
		}
		agentQuery = strings.ToLower(strings.TrimSpace(query))
	}
	agentOffset := 0
	if raw, supplied := arguments["agent_offset"]; supplied {
		switch value := raw.(type) {
		case int:
			agentOffset = value
		case float64:
			if value < 0 || value > 128 || value != float64(int(value)) {
				return nil, errors.New("configuration catalog agent offset is invalid")
			}
			agentOffset = int(value)
		default:
			return nil, errors.New("configuration catalog agent offset is invalid")
		}
		if agentOffset < 0 || agentOffset > 128 {
			return nil, errors.New("configuration catalog agent offset is invalid")
		}
	}
	schemas := assistantPlanOperationSchemas(input)
	operationTypes := assistantOperationTypesFromSchemas(schemas)
	if raw, selected := arguments["operation_types"]; selected {
		requested, ok := raw.([]any)
		if !ok || len(requested) > maximumAssistantDiscoveredSchemas {
			return nil, errors.New("configuration catalog selection is invalid")
		}
		allowed := make(map[string]struct{}, len(operationTypes))
		for _, kind := range operationTypes {
			allowed[kind] = struct{}{}
		}
		selectedTypes := make(map[string]struct{}, len(requested))
		for _, value := range requested {
			kind, ok := value.(string)
			if !ok {
				return nil, errors.New("configuration catalog selection is invalid")
			}
			if _, permitted := allowed[kind]; !permitted {
				return nil, errors.New("configuration catalog selection is invalid")
			}
			if _, duplicate := selectedTypes[kind]; duplicate {
				return nil, errors.New("configuration catalog selection is invalid")
			}
			selectedTypes[kind] = struct{}{}
		}
		filtered := make([]map[string]any, 0, len(selectedTypes))
		for _, schema := range schemas {
			kind := assistantSchemaType(schema)
			if _, requested := selectedTypes[kind]; requested {
				filtered = append(filtered, schema)
			}
		}
		schemas = filtered
	}
	targets := append([]runtimecontract.RunnerDelegationTarget(nil), input.DelegationTargets...)
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].Name == targets[right].Name {
			return targets[left].Ref < targets[right].Ref
		}
		return targets[left].Name < targets[right].Name
	})
	matching := make([]runtimecontract.RunnerDelegationTarget, 0, len(targets))
	for _, target := range targets {
		if agentQuery != "" && !strings.Contains(strings.ToLower(target.Name+" "+target.Purpose+" "+target.RoleDescription), agentQuery) {
			continue
		}
		matching = append(matching, target)
	}
	end := agentOffset + maximumAssistantCatalogAgents
	if end > len(matching) {
		end = len(matching)
	}
	agents := make([]map[string]string, 0, maximumAssistantCatalogAgents)
	if agentOffset < len(matching) {
		for _, target := range matching[agentOffset:end] {
			agents = append(agents, map[string]string{
				"ref": target.Ref, "name": target.Name, "purpose": truncateRunes(target.Purpose, 240),
				"role_description": truncateRunes(target.RoleDescription, 240),
			})
		}
	}
	context := map[string]any{"route": "", "entity_kind": "", "entity_ref": "", "entity_name": "", "allowed_operations": []string{}}
	if input.AssistantContext != nil {
		context = map[string]any{"route": input.AssistantContext.Route, "entity_kind": input.AssistantContext.EntityKind,
			"entity_ref": input.AssistantContext.EntityRef, "entity_name": input.AssistantContext.EntityName,
			"entity_version": input.AssistantContext.EntityVersion, "allowed_operations": input.AssistantContext.AllowedOperations}
	}
	result := map[string]any{
		"current_project_ref": input.ProjectRef,
		"agents":              agents,
		"agent_total":         len(matching),
		"context":             context,
		"operation_types":     operationTypes,
		"operation_schemas":   schemas,
	}
	if end < len(matching) {
		result["agent_next_offset"] = end
	}
	return result, nil
}

func assistantPlanTool(input runtimecontract.RunnerInput) map[string]any {
	return map[string]any{
		"name":        "propose_configuration_plan",
		"description": "Propose an editable Kodex draft for explicit user approval. First request the exact schema for each operation type from get_configuration_catalog. This compact envelope does not grant fields or authority; control-plane validates every specialized operation. This tool never applies a plan.",
		"inputSchema": objectSchema([]string{"summary", "operations"}, map[string]any{
			"summary": stringSchema(1, 2000),
			"operations": map[string]any{"type": "array", "minItems": 1, "maxItems": 32,
				"items": objectSchema([]string{"type", "title", "summary", "parameters"}, map[string]any{
					"type": enumSchema(assistantOperationTypes(input)...), "title": stringSchema(1, 200),
					"summary":         stringSchema(1, 500),
					"parameters":      map[string]any{"type": "object", "maxProperties": 100},
					"action":          enumSchema("CREATE", "UPDATE", "ARCHIVE", "EXECUTE"),
					"target":          map[string]any{"type": "object", "maxProperties": 4},
					"before":          map[string]any{"type": "object", "maxProperties": 100},
					"after":           map[string]any{"type": "object", "maxProperties": 100},
					"expectedVersion": map[string]any{"type": "integer", "minimum": 1},
					"selected":        map[string]any{"type": "boolean"},
				})},
		}),
		"outputSchema": objectSchema([]string{"ok", "plan_ref", "plan_version", "plan_revision", "conversation_ref"}, map[string]any{
			"ok": map[string]any{"type": "boolean"}, "plan_ref": opaqueRefSchema(), "plan_version": map[string]any{"type": "integer", "minimum": 1},
			"plan_revision": map[string]any{"type": "integer", "minimum": 1}, "conversation_ref": opaqueRefSchema(),
		}),
	}
}

func assistantOperationTypes(input runtimecontract.RunnerInput) []string {
	return assistantOperationTypesFromSchemas(assistantPlanOperationSchemas(input))
}

func assistantOperationTypesFromSchemas(schemas []map[string]any) []string {
	result := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		result = append(result, assistantSchemaType(schema))
	}
	return result
}

func assistantSchemaType(schema map[string]any) string {
	return schema["properties"].(map[string]any)["type"].(map[string]any)["const"].(string)
}

func assistantPlanOperationSchemas(input runtimecontract.RunnerInput) []map[string]any {
	projectRef := opaqueRefSchema()
	agentRef := opaqueRefSchema()
	if input.ProjectRef != "" {
		projectRef = enumSchema(input.ProjectRef)
	}
	if len(input.DelegationTargets) != 0 {
		refs := make([]string, 0, len(input.DelegationTargets))
		for _, target := range input.DelegationTargets {
			refs = append(refs, target.Ref)
		}
		agentRef = enumSchema(refs...)
	}
	result := []map[string]any{
		assistantOperationSchema("CREATE_PROJECT", objectSchema([]string{"name", "purpose", "language"}, map[string]any{
			"name": stringSchema(1, 160), "purpose": stringSchema(1, 2000), "language": enumSchema("ru", "en"),
		})),
		assistantOperationSchema("UPDATE_PROJECT", projectUpdateInputSchema(projectRef)),
		assistantOperationSchema("CREATE_AGENT", objectSchema([]string{"projectRef", "name", "purpose", "roleDescription", "instructions"}, map[string]any{
			"projectRef": projectRef, "roleDefinitionRef": opaqueRefSchema(), "name": stringSchema(1, 160),
			"purpose": stringSchema(1, 2000), "roleDescription": stringSchema(1, 2000), "avatarUrl": stringSchema(0, 500),
			"runtimeRef": opaqueRefSchema(), "instructions": stringSchema(20, 65536),
			"capabilities": map[string]any{"type": "array", "maxItems": 3, "uniqueItems": true, "items": enumSchema("platform.artifact.manage", "platform.run.delegate", "platform.run.launch")},
		})),
		assistantOperationSchema("CREATE_RUNTIME_ENVIRONMENT_DRAFT", objectSchema([]string{"projectRef", "name"}, map[string]any{
			"projectRef": projectRef, "name": stringSchema(1, 120), "description": stringSchema(0, 1000),
			"imageArtifactRef": opaqueRefSchema(),
		})),
		assistantOperationSchema("CREATE_ROLE_IMAGE_RECIPE", objectSchema([]string{"projectRef", "agentRef", "name"}, map[string]any{
			"projectRef": projectRef, "agentRef": opaqueRefSchema(), "name": stringSchema(1, 160),
			"environmentKey": stringSchema(1, 96),
		})),
		assistantOperationSchema("ARCHIVE_AGENT", objectSchema(nil, map[string]any{})),
		assistantOperationSchema("CREATE_WORKFLOW", workflowInputSchema(projectRef, agentRef)),
		assistantOperationSchema("ARCHIVE_WORKFLOW", objectSchema(nil, map[string]any{})),
		assistantOperationSchema("CHANGE_CAPABILITY", objectSchema([]string{"agentRef", "capabilityKey", "enabled"}, map[string]any{
			"agentRef": agentRef, "capabilityKey": capabilityKeySchema(), "enabled": map[string]any{"type": "boolean"},
		})),
		assistantOperationSchema("CHANGE_INTEGRATION_GRANT", integrationGrantInputSchema()),
		assistantOperationSchema("CREATE_INTEGRATION_CONNECTION", objectSchema([]string{"definitionKey", "name", "publicConfiguration"}, map[string]any{
			"definitionKey": capabilityKeySchema(), "name": stringSchema(1, 160),
			"publicConfiguration": map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		})),
		assistantOperationSchema("TEST_INTEGRATION_CONNECTION", objectSchema([]string{"connectionRef"}, map[string]any{
			"connectionRef": opaqueRefSchema(),
		})),
		assistantOperationSchema("CREATE_SCHEDULE", scheduleInputSchema(projectRef, agentRef)),
		assistantOperationSchema("LAUNCH_RUN", runInputSchema(projectRef, agentRef)),
	}
	if input.AssistantContext == nil {
		return result
	}
	if input.AssistantContext.EntityKind == "AGENT" && input.AssistantContext.EntityRef != "" {
		result = append(result, assistantOperationSchema("UPDATE_AGENT", agentUpdateInputSchema(enumSchema(input.AssistantContext.EntityRef))))
	}
	if len(input.AssistantContext.AllowedOperations) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(input.AssistantContext.AllowedOperations))
	for _, operation := range input.AssistantContext.AllowedOperations {
		allowed[operation] = struct{}{}
	}
	filtered := make([]map[string]any, 0, len(result))
	for _, operation := range result {
		kind := operation["properties"].(map[string]any)["type"].(map[string]any)["const"].(string)
		if _, ok := allowed[kind]; ok {
			filtered = append(filtered, operation)
		}
	}
	return filtered
}

func agentUpdateInputSchema(agentRef map[string]any) map[string]any {
	schema := objectSchema([]string{"agentRef"}, map[string]any{
		"agentRef": agentRef, "name": stringSchema(1, 160), "purpose": stringSchema(1, 2000),
		"roleDescription": stringSchema(1, 2000),
	})
	schema["anyOf"] = []map[string]any{
		{"required": []string{"name"}}, {"required": []string{"purpose"}}, {"required": []string{"roleDescription"}},
	}
	return schema
}

func projectUpdateInputSchema(projectRef map[string]any) map[string]any {
	schema := objectSchema([]string{"projectRef"}, map[string]any{
		"projectRef": projectRef,
		"name":       stringSchema(1, 160),
		"purpose":    stringSchema(1, 2000),
		"language":   enumSchema("ru", "en"),
	})
	schema["anyOf"] = []map[string]any{
		{"required": []string{"name"}},
		{"required": []string{"purpose"}},
		{"required": []string{"language"}},
	}
	return schema
}

func integrationGrantInputSchema() map[string]any {
	schema := objectSchema([]string{"connectionRef", "capabilityKey", "enabled"}, map[string]any{
		"connectionRef": opaqueRefSchema(), "capabilityKey": capabilityKeySchema(), "agentRef": opaqueRefSchema(), "workflowRef": opaqueRefSchema(),
		"enabled": map[string]any{"type": "boolean"},
	})
	schema["oneOf"] = []map[string]any{
		{"required": []string{"agentRef"}, "not": map[string]any{"required": []string{"workflowRef"}}},
		{"required": []string{"workflowRef"}, "not": map[string]any{"required": []string{"agentRef"}}},
	}
	return schema
}

func assistantOperationSchema(kind string, parameters map[string]any) map[string]any {
	action := "CREATE"
	requiresVersion := false
	if kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT" || kind == "CHANGE_CAPABILITY" || kind == "CHANGE_INTEGRATION_GRANT" {
		action, requiresVersion = "UPDATE", true
	} else if kind == "ARCHIVE_AGENT" || kind == "ARCHIVE_WORKFLOW" {
		action, requiresVersion = "ARCHIVE", true
	} else if kind == "LAUNCH_RUN" || kind == "TEST_INTEGRATION_CONNECTION" {
		action = "EXECUTE"
		requiresVersion = kind == "TEST_INTEGRATION_CONNECTION"
	}
	targetRequired := []string{"kind", "name"}
	targetProperties := map[string]any{
		"kind": stringSchema(1, 80), "name": stringSchema(1, 300),
	}
	required := []string{"type", "action", "title", "summary", "target", "parameters", "selected"}
	if requiresVersion {
		targetRequired = append(targetRequired, "ref", "version")
		targetProperties["ref"] = opaqueRefSchema()
		targetProperties["version"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
		required = append(required, "expectedVersion")
	}
	before := map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true}
	after := map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true}
	if action == "CREATE" {
		before = objectSchema(nil, map[string]any{})
		after = parameters
	}
	serverHydrated := action == "CREATE" || kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT"
	if !serverHydrated {
		required = append(required, "before", "after")
	}
	if serverHydrated {
		required = []string{"type", "title", "summary", "parameters"}
	}
	properties := map[string]any{
		"type": map[string]any{"const": kind}, "action": map[string]any{"const": action}, "title": stringSchema(1, 200),
		"summary": stringSchema(1, 500), "target": objectSchema(targetRequired, targetProperties),
		"parameters": parameters, "before": before, "after": after, "selected": map[string]any{"const": true},
	}
	if requiresVersion {
		properties["expectedVersion"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
	}
	return objectSchema(required, properties)
}

func workflowInputSchema(projectRef, agentRef map[string]any) map[string]any {
	inputField := objectSchema([]string{"label", "valueType", "required", "options"}, map[string]any{
		"label": stringSchema(1, 160), "description": stringSchema(0, 500),
		"valueType": enumSchema("TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT"),
		"required":  map[string]any{"type": "boolean"},
		"options":   map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": stringSchema(1, 160)},
	})
	step := objectSchema([]string{"name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys"}, map[string]any{
		"name": stringSchema(1, 160), "purpose": stringSchema(1, 1000), "agentRef": agentRef,
		"parallel": map[string]any{"type": "boolean"}, "parallelGroup": map[string]any{"oneOf": []map[string]any{
			{"type": "integer", "minimum": 0, "maximum": 50}, stringSchema(1, 80),
		}},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 86400}, "expectedResult": stringSchema(0, 1000),
		"humanGate": map[string]any{"type": "boolean"}, "gateDecisions": stringArraySchema(0, 4, []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}),
		"requiredCapabilityKeys": map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": capabilityKeySchema()},
	})
	return objectSchema([]string{"projectRef", "name", "purpose", "coordinatorAgentRef", "steps"}, map[string]any{
		"projectRef": projectRef, "name": stringSchema(1, 160), "purpose": stringSchema(1, 1000), "coordinatorAgentRef": agentRef,
		"inputFields":    map[string]any{"type": "array", "maxItems": 100, "items": inputField},
		"steps":          map[string]any{"type": "array", "minItems": 1, "maxItems": 200, "items": step},
		"maxConcurrency": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 604800}, "completionCriteria": stringSchema(0, 2000),
	})
}

func scheduleInputSchema(projectRef, targetRef map[string]any) map[string]any {
	schema := objectSchema([]string{"projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "timezone", "input", "sessionPolicy", "notificationPolicy"}, map[string]any{
		"projectRef": projectRef, "name": stringSchema(1, 160), "targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"preset": stringSchema(1, 120), "timeOfDay": stringSchema(0, 5), "dayOfWeek": stringSchema(0, 9), "timezone": stringSchema(1, 80),
		"input":              map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"sessionPolicy":      enumSchema("NEW_EACH_RUN", "CONTINUE_ONE"),
		"notificationPolicy": enumSchema("CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"),
	})
	schema["oneOf"] = assistantExecutionTargetBranches(targetRef)
	return schema
}

func runInputSchema(projectRef, targetRef map[string]any) map[string]any {
	schema := objectSchema([]string{"projectRef", "targetType", "targetRef", "title", "task", "input"}, map[string]any{
		"projectRef": projectRef, "targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"title": stringSchema(1, 240), "task": stringSchema(1, 32768), "sessionRef": opaqueRefSchema(),
		"input":            map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"attachmentSetRef": opaqueRefSchema(),
	})
	schema["oneOf"] = assistantExecutionTargetBranches(targetRef)
	return schema
}

func assistantExecutionTargetBranches(agentRef map[string]any) []map[string]any {
	return []map[string]any{
		{"properties": map[string]any{"targetType": map[string]any{"const": "AGENT"}, "targetRef": agentRef}},
		{"properties": map[string]any{"targetType": map[string]any{"const": "WORKFLOW"}, "targetRef": opaqueRefSchema()}},
	}
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func stringSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "string", "minLength": minimum, "maxLength": maximum}
}

func opaqueRefSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9_-]{8,96}$", "maxLength": 96}
}

func capabilityKeySchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[a-z0-9][a-z0-9._-]{0,79}$", "maxLength": 80}
}

func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func stringArraySchema(minimum, maximum int, values []string) map[string]any {
	return map[string]any{"type": "array", "minItems": minimum, "maxItems": maximum, "uniqueItems": true,
		"items": map[string]any{"type": "string", "enum": values}}
}
