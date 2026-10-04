package callback

import (
	"errors"
	"slices"
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
		"description": "Discover refs/schemas; omit operation_types for index. assistant_configuration_catalog excludes other selectors. MODELS requires account_ref. CURRENT_CONFIGURATION uses your agent_ref: instructions, settings, environment/tools/network, template variables. current_configuration is fresh; execution_snapshot is turn-pinned.",
		"inputSchema": objectSchema(nil, map[string]any{
			"operation_types": map[string]any{"type": "array", "maxItems": maximumAssistantDiscoveredSchemas,
				"uniqueItems": true, "items": map[string]any{"type": "string", "enum": assistantOperationTypes(input)}},
			"agent_query":                     stringSchema(0, 80),
			"agent_offset":                    map[string]any{"type": "integer", "minimum": 0, "maximum": 128},
			"definition_query":                stringSchema(0, 80),
			"definition_offset":               map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
			"assistant_configuration_catalog": assistantConfigurationCatalogInputSchema(input),
		}),
		"outputSchema": objectSchema([]string{"current_project_ref", "agents"}, map[string]any{
			"current_project_ref": map[string]any{"type": "string"}, "agents": map[string]any{"type": "array", "maxItems": maximumAssistantCatalogAgents, "items": map[string]any{"type": "object"}},
			"agent_total":                     map[string]any{"type": "integer", "minimum": 0},
			"agent_next_offset":               map[string]any{"type": "integer", "minimum": 0},
			"context":                         map[string]any{"type": "object"},
			"current_runtime":                 assistantCurrentRuntimeSchema(),
			"operation_types":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"operation_schemas":               map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"integration_definitions":         map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"type": "object"}},
			"definition_next_offset":          map[string]any{"type": "integer", "minimum": 0},
			"assistant_configuration_catalog": map[string]any{"type": "object", "maxProperties": 10},
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
	if !input.IsAssistant() || !onlyKeys(arguments, "operation_types", "agent_query", "agent_offset", "definition_query", "definition_offset", "assistant_configuration_catalog") {
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
	allSchemas := assistantPlanOperationSchemas(input)
	operationTypes := assistantOperationTypesFromSchemas(allSchemas)
	schemas := make([]map[string]any, 0, maximumAssistantDiscoveredSchemas)
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
		for _, schema := range allSchemas {
			kind := assistantSchemaType(schema)
			if _, requested := selectedTypes[kind]; requested {
				schemas = append(schemas, schema)
			}
		}
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
			purpose, roleDescription := truncateRunes(target.Purpose, 240), truncateRunes(target.RoleDescription, 240)
			if input.AssistantContext != nil && input.AssistantContext.EntityKind == "AGENT" && input.AssistantContext.EntityRef == target.Ref {
				purpose, roleDescription = target.Purpose, target.RoleDescription
			}
			agents = append(agents, map[string]string{
				"ref": target.Ref, "name": target.Name, "purpose": purpose,
				"role_description": roleDescription,
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
		"current_runtime":     assistantCurrentRuntime(input),
	}
	if end < len(matching) {
		result["agent_next_offset"] = end
	}
	return result, nil
}

func assistantPlanTool(input runtimecontract.RunnerInput) map[string]any {
	return map[string]any{
		"name":        "propose_configuration_plan",
		"description": "Propose an editable draft for explicit user approval. Read exact schemas from get_configuration_catalog first. Control-plane validates each specialized operation; this tool never applies a plan or grants authority.",
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

func environmentPublicValuesSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 128,
		"description": "Non-secret environment values only. Names must not start with KODEX_, CODEX_, OPENAI_, OTEL_, AWS_, AZURE_, GOOGLE_, or KUBERNETES_. Credentials and tokens must use a protected Secret form and secretBindings.",
		"items": objectSchema([]string{"name", "value"}, map[string]any{
			"name":  map[string]any{"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,126}$"},
			"value": stringSchema(0, 8192),
		}),
	}
}

func environmentPublicValueUpdatesSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 128,
		"description": "Sparse upserts for non-secret environment values. Names must not start with KODEX_, CODEX_, OPENAI_, OTEL_, AWS_, AZURE_, GOOGLE_, or KUBERNETES_. Use this when the current complete value list is not exposed; the server merges entries into its authoritative snapshot.",
		"items": objectSchema([]string{"name", "value"}, map[string]any{
			"name":  map[string]any{"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,126}$"},
			"value": stringSchema(0, 8192),
		}),
	}
}

func environmentPublicValueRemovalsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 128, "uniqueItems": true,
		"description": "Names of non-secret environment values to remove from the server-owned current list.",
		"items":       map[string]any{"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,126}$"},
	}
}

func environmentSecretBindingsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 128,
		"description": "References to already created project Secrets; never include plaintext values.",
		"items": objectSchema([]string{"name", "secretRef"}, map[string]any{
			"name":      map[string]any{"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,126}$"},
			"secretRef": map[string]any{"type": "string", "pattern": "^sec_[A-Za-z0-9_-]{4,92}$"},
			"revision":  map[string]any{"type": "integer", "minimum": 0},
		}),
	}
}

func environmentToolsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 128,
		"description": "Complete selected tool list from the exact promoted image. The owner reviews each command before publishing the environment draft.",
		"items": objectSchema([]string{"name", "command", "description"}, map[string]any{
			"name":        stringSchema(1, 160),
			"command":     map[string]any{"type": "string", "maxLength": 160, "pattern": "^[A-Za-z0-9][A-Za-z0-9._+-]*$"},
			"description": stringSchema(1, 500), "usageHint": stringSchema(0, 500),
		}),
	}
}

func environmentPolicySchema() map[string]any {
	resources := objectSchema([]string{"cpuRequestMilli", "cpuLimitMilli", "memoryRequestMib", "memoryLimitMib", "ephemeralStorageRequestMib", "ephemeralStorageLimitMib"}, map[string]any{
		"cpuRequestMilli":            map[string]any{"type": "integer", "minimum": 100, "maximum": 8000},
		"cpuLimitMilli":              map[string]any{"type": "integer", "minimum": 100, "maximum": 16000},
		"memoryRequestMib":           map[string]any{"type": "integer", "minimum": 128, "maximum": 32768},
		"memoryLimitMib":             map[string]any{"type": "integer", "minimum": 128, "maximum": 65536},
		"ephemeralStorageRequestMib": map[string]any{"type": "integer", "minimum": 256, "maximum": 20480},
		"ephemeralStorageLimitMib":   map[string]any{"type": "integer", "minimum": 256, "maximum": 102400},
	})
	webAccessRule := objectSchema([]string{"domainPattern", "protocol", "port", "httpMethods"}, map[string]any{
		"domainPattern": map[string]any{
			"type": "string", "minLength": 3, "maxLength": 253,
			"pattern": `^(?:\*\*\.|\*\.)?[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`,
		},
		"protocol": enumSchema("HTTPS"),
		"port":     map[string]any{"type": "integer", "const": 443},
		"httpMethods": map[string]any{"type": "array", "minItems": 1, "maxItems": 7, "uniqueItems": true,
			"items": enumSchema("GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE")},
	})
	webAccess := objectSchema([]string{"mode", "rules"}, map[string]any{
		"mode":  enumSchema("NONE", "ALLOWLIST_READ_ONLY", "ALLOWLIST_FULL", "FULL_PUBLIC"),
		"rules": map[string]any{"type": "array", "maxItems": 64, "items": webAccessRule},
	})
	return objectSchema([]string{"resources", "volumes", "networkDestinations", "webAccess", "kubernetesAccess"}, map[string]any{
		"resources": resources,
		"volumes": map[string]any{"type": "array", "maxItems": 16, "items": objectSchema([]string{"name", "kind", "sizeMib"}, map[string]any{
			"name": stringSchema(1, 32), "kind": enumSchema("EPHEMERAL_DISK", "EPHEMERAL_MEMORY"),
			"sizeMib": map[string]any{"type": "integer", "minimum": 16, "maximum": 10240},
		})},
		"networkDestinations": map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "uniqueItems": true,
			"items": enumSchema("DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK")},
		"webAccess":        webAccess,
		"kubernetesAccess": enumSchema("NONE"),
	})
}

func environmentSecretSuggestionsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 8,
		"description": "Safe metadata for owner-only Secret forms. Never include credential values or pretend that the Secret already exists.",
		"items": objectSchema([]string{"name", "valueType", "sourceHelp"}, map[string]any{
			"name": stringSchema(1, 120), "description": stringSchema(0, 1000),
			"valueType": enumSchema("STRING", "JSON", "BINARY"), "sourceHelp": stringSchema(1, 1000),
		}),
	}
}

func environmentRevisionInputSchema(environmentRef map[string]any, systemAssistantRef map[string]any) map[string]any {
	required := []string{"environmentRef"}
	properties := map[string]any{
		"environmentRef": environmentRef, "name": stringSchema(1, 120),
		"description": stringSchema(0, 1000), "imageArtifactRef": stringSchema(0, 96),
		"publicValues": environmentPublicValuesSchema(), "publicValueUpdates": environmentPublicValueUpdatesSchema(),
		"publicValueRemovals": environmentPublicValueRemovalsSchema(), "secretBindings": environmentSecretBindingsSchema(),
		"tools": environmentToolsSchema(), "policy": environmentPolicySchema(),
	}
	if systemAssistantRef != nil {
		required = append(required, "systemAssistantRef")
		properties["systemAssistantRef"] = systemAssistantRef
	}
	schema := objectSchema(required, properties)
	schema["anyOf"] = []map[string]any{
		{"required": []string{"name"}}, {"required": []string{"description"}},
		{"required": []string{"imageArtifactRef"}}, {"required": []string{"publicValues"}},
		{"required": []string{"publicValueUpdates"}}, {"required": []string{"publicValueRemovals"}},
		{"required": []string{"secretBindings"}}, {"required": []string{"tools"}}, {"required": []string{"policy"}},
	}
	return schema
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
			"name": stringSchema(1, 120), "purpose": stringSchema(1, 1000), "language": enumSchema("ru", "en"),
		})),
		assistantOperationSchema("CREATE_PROJECT_FILE", objectSchema([]string{"projectRef", "fileName", "mediaType", "content"}, map[string]any{
			"projectRef": projectRef, "fileName": stringSchema(1, 255),
			"mediaType":       enumSchema("text/plain", "text/markdown", "text/csv", "application/json", "image/png", "image/jpeg", "image/webp", "application/pdf"),
			"contentEncoding": enumSchema("UTF8", "BASE64"),
			"content":         stringSchema(0, ((1<<20)+2)/3*4),
		})),
		assistantOperationSchema("UPDATE_PROJECT", projectUpdateInputSchema(projectRef)),
		assistantOperationSchema("CREATE_AGENT", objectSchema([]string{"projectRef", "name", "purpose", "roleDescription", "instructions"}, map[string]any{
			"projectRef": projectRef, "roleDefinitionRef": opaqueRefSchema(), "name": stringSchema(1, 120),
			"purpose": stringSchema(1, 1000), "roleDescription": stringSchema(1, 1000), "avatarUrl": stringSchema(0, 500),
			"runtimeRef": opaqueRefSchema(), "instructions": assistantAgentInstructionsSchema(),
			"capabilities": map[string]any{"type": "array", "maxItems": 3, "uniqueItems": true, "items": assistantAgentCapabilitySchema()},
		})),
		assistantOperationSchema("CREATE_RUNTIME_ENVIRONMENT_DRAFT", objectSchema([]string{"projectRef", "name"}, map[string]any{
			"projectRef": projectRef, "name": stringSchema(1, 120), "description": stringSchema(0, 1000),
			"imageArtifactRef":  opaqueRefSchema(),
			"publicValues":      environmentPublicValuesSchema(),
			"secretBindings":    environmentSecretBindingsSchema(),
			"secretSuggestions": environmentSecretSuggestionsSchema(),
			"tools":             environmentToolsSchema(),
			"policy":            environmentPolicySchema(),
		})),
		assistantOperationSchema("CREATE_ROLE_IMAGE_RECIPE", objectSchema([]string{"projectRef", "agentRef", "name"}, map[string]any{
			"projectRef": projectRef, "agentRef": opaqueRefSchema(), "name": stringSchema(1, 160),
			"environmentKey": assistantRoleEnvironmentKeySchema(), "dockerfile": stringSchema(1, 65536),
		})),
		assistantOperationSchema("UPDATE_ROLE_IMAGE_RECIPE", objectSchema([]string{"recipeRef"}, map[string]any{
			"recipeRef": opaqueRefSchema(), "name": stringSchema(1, 160),
			"environmentKey": assistantRoleEnvironmentKeySchema(), "dockerfile": stringSchema(1, 65536),
		})),
		assistantOperationSchema("ARCHIVE_AGENT", objectSchema(nil, map[string]any{})),
		assistantOperationSchema("CREATE_WORKFLOW", workflowInputSchema(projectRef, agentRef)),
		assistantOperationSchema("ARCHIVE_WORKFLOW", objectSchema(nil, map[string]any{})),
		assistantOperationSchema("CHANGE_CAPABILITY", objectSchema([]string{"agentRef", "capabilityKey", "enabled"}, map[string]any{
			"agentRef": agentRef, "capabilityKey": assistantAgentCapabilitySchema(), "enabled": map[string]any{"type": "boolean"},
		})),
		assistantOperationSchema("CHANGE_INTEGRATION_GRANT", integrationGrantInputSchema(input.AssistantContext)),
		assistantOperationSchema("CREATE_INTEGRATION_CONNECTION", objectSchema([]string{"definitionKey", "name", "publicConfiguration"}, map[string]any{
			"definitionKey": capabilityKeySchema(), "name": stringSchema(1, 160),
			"publicConfiguration": map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		})),
		assistantOperationSchema("PUBLISH_INTEGRATION_DEFINITION", objectSchema([]string{"configurationRef", "revisionRef"}, map[string]any{
			"configurationRef": opaqueRefSchema(), "revisionRef": opaqueRefSchema(),
		})),
		assistantOperationSchema("TEST_INTEGRATION_CONNECTION", objectSchema([]string{"connectionRef"}, map[string]any{
			"connectionRef": opaqueRefSchema(),
		})),
		assistantOperationSchema("CREATE_SCHEDULE", scheduleInputSchema(projectRef, agentRef)),
		assistantOperationSchema("LAUNCH_RUN", runInputSchema(projectRef, agentRef)),
	}
	if input.IsSystemAssistant() {
		result = append(result, assistantOperationSchema("CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", objectSchema(
			[]string{"connectionRef", "capabilityKey", "enabled", "approvalPolicy"}, map[string]any{
				"connectionRef": opaqueRefSchema(), "capabilityKey": capabilityKeySchema(), "enabled": map[string]any{"type": "boolean"},
				"approvalPolicy":     enumSchema("NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"),
				"approvalScopePaths": map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": stringSchema(1, 200)},
			})))
		result = append(result, assistantOperationSchema("CREATE_PROJECT_ASSISTANT", objectSchema(
			[]string{"projectRef", "name", "purpose", "instructions"}, map[string]any{
				"projectRef": projectRef, "name": stringSchema(1, 120), "purpose": stringSchema(1, 1000),
				"instructions": assistantAgentInstructionsSchema(),
			})))
	}
	selfInstructionsOperation := input.IsSystemAssistant() && input.AgentRef != ""
	selfConfigurationOperation := input.IsAssistant() && input.AgentRef != ""
	if selfConfigurationOperation {
		configurationAgentRef := opaqueRefSchema()
		if input.AssistantScope == runtimecontract.AssistantScopeProject {
			configurationAgentRef = enumSchema(input.AgentRef)
		}
		result = append(result, assistantOperationSchema("PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", assistantRuntimeConfigurationSchema(configurationAgentRef)))
	}
	if selfInstructionsOperation {
		updateImageSchema := objectSchema([]string{"systemAssistantRef", "recipeRef"}, map[string]any{
			"systemAssistantRef": enumSchema(input.AgentRef), "recipeRef": opaqueRefSchema(),
			"name": stringSchema(1, 160), "environmentKey": assistantRoleEnvironmentKeySchema(), "dockerfile": stringSchema(1, 65536),
		})
		updateImageSchema["anyOf"] = []map[string]any{{"required": []string{"name"}}, {"required": []string{"environmentKey"}}, {"required": []string{"dockerfile"}}}
		result = append(result,
			assistantOperationSchema("CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", objectSchema(
				[]string{"systemAssistantRef", "name", "environmentKey", "dockerfile"}, map[string]any{
					"systemAssistantRef": enumSchema(input.AgentRef), "name": stringSchema(1, 160),
					"environmentKey": assistantRoleEnvironmentKeySchema(), "dockerfile": stringSchema(1, 65536),
				})),
			assistantOperationSchema("UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", updateImageSchema))
	}
	if selfInstructionsOperation {
		result = append(result, assistantOperationSchema("UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", objectSchema(
			[]string{"systemAssistantRef", "instructions"}, map[string]any{
				"systemAssistantRef": enumSchema(input.AgentRef),
				"instructions":       stringSchema(0, 20000),
			})))
	}
	selfEnvironmentOperation := input.IsSystemAssistant() && input.AgentRef != "" && input.RuntimeEnvironmentRef != ""
	if selfEnvironmentOperation {
		parameters := environmentRevisionInputSchema(enumSchema(input.RuntimeEnvironmentRef), enumSchema(input.AgentRef))
		if screen := input.AssistantContext; screen != nil && screen.EntityKind == "ENVIRONMENT" && screen.EntityRef != "" &&
			slices.Contains(screen.AllowedOperations, "PREPARE_RUNTIME_ENVIRONMENT_REVISION") {
			parameters["required"] = []string{"environmentRef"}
			parameters["properties"].(map[string]any)["environmentRef"] = enumSchema(input.RuntimeEnvironmentRef, screen.EntityRef)
			// Присутствие self locator отделяет собственную среду от точной экранной.
			parameters["allOf"] = []map[string]any{{"oneOf": []map[string]any{
				{"required": []string{"systemAssistantRef"}, "properties": map[string]any{"environmentRef": enumSchema(input.RuntimeEnvironmentRef)}},
				{"not": map[string]any{"required": []string{"systemAssistantRef"}}, "properties": map[string]any{"environmentRef": enumSchema(screen.EntityRef)}},
			}}}
		}
		result = append(result, assistantOperationSchema("PREPARE_RUNTIME_ENVIRONMENT_REVISION",
			parameters))
	}
	projectSelfOperation := input.AssistantScope == runtimecontract.AssistantScopeProject && input.AgentRef != ""
	if projectSelfOperation {
		result = append(result,
			assistantOperationSchema("UPDATE_AGENT", agentUpdateInputSchema(projectSelfTargetSchema(input, "UPDATE_AGENT", "AGENT", input.AgentRef))),
			assistantOperationSchema("CREATE_INSTRUCTION_DRAFT", objectSchema([]string{"agentRef", "instructions"}, map[string]any{
				"agentRef": projectSelfTargetSchema(input, "CREATE_INSTRUCTION_DRAFT", "AGENT", input.AgentRef), "instructions": assistantAgentInstructionsSchema(),
			})),
			assistantOperationSchema("BIND_AGENT_RUNTIME_ENVIRONMENT", objectSchema([]string{"agentRef", "environmentRef"}, map[string]any{
				"agentRef": projectSelfTargetSchema(input, "BIND_AGENT_RUNTIME_ENVIRONMENT", "AGENT", input.AgentRef), "environmentRef": opaqueRefSchema(),
			})))
	}
	projectSelfEnvironmentOperation := input.AssistantScope == runtimecontract.AssistantScopeProject && input.RuntimeEnvironmentRef != ""
	if projectSelfEnvironmentOperation {
		result = append(result, assistantOperationSchema("PREPARE_RUNTIME_ENVIRONMENT_REVISION",
			environmentRevisionInputSchema(projectSelfTargetSchema(input, "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "ENVIRONMENT", input.RuntimeEnvironmentRef), nil)))
	}
	if input.AssistantContext == nil {
		return withProjectAssistantOperations(input, result)
	}
	if input.AssistantContext.EntityKind == "AGENT" && input.AssistantContext.EntityRef != "" && !projectSelfOperation {
		result = append(result, assistantOperationSchema("UPDATE_AGENT", agentUpdateInputSchema(enumSchema(input.AssistantContext.EntityRef))))
		result = append(result, assistantOperationSchema("CREATE_INSTRUCTION_DRAFT", objectSchema(
			[]string{"agentRef", "instructions"}, map[string]any{
				"agentRef": enumSchema(input.AssistantContext.EntityRef), "instructions": assistantAgentInstructionsSchema(),
			})))
		result = append(result, assistantOperationSchema("BIND_AGENT_RUNTIME_ENVIRONMENT", objectSchema(
			[]string{"agentRef", "environmentRef"}, map[string]any{
				"agentRef": enumSchema(input.AssistantContext.EntityRef), "environmentRef": opaqueRefSchema(),
			})))
	}
	if input.AssistantContext.EntityKind == "WORKFLOW" && input.AssistantContext.EntityRef != "" {
		result = append(result, assistantOperationSchema("UPDATE_WORKFLOW", workflowUpdateInputSchema(input.AssistantContext.EntityRef)))
	}
	if input.AssistantContext.EntityKind == "ENVIRONMENT" && input.AssistantContext.EntityRef != "" && !projectSelfEnvironmentOperation && !selfEnvironmentOperation {
		result = append(result, assistantOperationSchema("PREPARE_RUNTIME_ENVIRONMENT_REVISION",
			environmentRevisionInputSchema(enumSchema(input.AssistantContext.EntityRef), nil)))
	}
	if input.AssistantContext.EntityKind == "INTEGRATION_CONNECTION" && input.AssistantContext.EntityRef != "" {
		result = append(result, assistantOperationSchema("UPDATE_INTEGRATION_CONNECTION", connectionUpdateInputSchema(input.AssistantContext.EntityRef)))
	}
	if input.AssistantContext.EntityKind == "SCHEDULE" && input.AssistantContext.EntityRef != "" {
		result = append(result, assistantOperationSchema("UPDATE_SCHEDULE", scheduleUpdateInputSchema(input.AssistantContext.EntityRef)))
	}
	if len(input.AssistantContext.AllowedOperations) == 0 && !projectSelfOperation && !projectSelfEnvironmentOperation && !selfInstructionsOperation && !selfConfigurationOperation {
		return nil
	}
	allowed := make(map[string]struct{}, len(input.AssistantContext.AllowedOperations))
	for _, operation := range input.AssistantContext.AllowedOperations {
		allowed[operation] = struct{}{}
	}
	filtered := make([]map[string]any, 0, len(result))
	for _, operation := range result {
		kind := operation["properties"].(map[string]any)["type"].(map[string]any)["const"].(string)
		if _, ok := allowed[kind]; ok ||
			selfInstructionsOperation && kind == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" ||
			selfEnvironmentOperation && kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ||
			selfInstructionsOperation && kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" ||
			selfInstructionsOperation && (kind == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE") ||
			selfConfigurationOperation && kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" ||
			projectSelfOperation && (kind == "UPDATE_AGENT" || kind == "CREATE_INSTRUCTION_DRAFT" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT") ||
			projectSelfEnvironmentOperation && kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			filtered = append(filtered, operation)
		}
	}
	return withProjectAssistantOperations(input, filtered)
}

// Locator помощника не заменяет authority: CP разрешает профиль и текущие pins.
// Обычная экранная команда и адресная команда помощника — непересекающиеся формы.
func withProjectAssistantOperations(input runtimecontract.RunnerInput, schemas []map[string]any) []map[string]any {
	if !input.IsAssistant() || input.AgentRef == "" {
		return schemas
	}
	for _, kind := range []string{"CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION"} {
		parameters := projectAssistantOperationParameters(input, kind)
		merged := false
		for _, schema := range schemas {
			properties := schema["properties"].(map[string]any)
			if properties["type"].(map[string]any)["const"] == kind {
				properties["parameters"] = map[string]any{"oneOf": []map[string]any{properties["parameters"].(map[string]any), parameters}}
				merged = true
				break
			}
		}
		if !merged {
			schemas = append(schemas, assistantOperationSchema(kind, parameters))
		}
	}
	return schemas
}

func projectAssistantOperationParameters(input runtimecontract.RunnerInput, kind string) map[string]any {
	ref := opaqueRefSchema()
	if input.AssistantScope == runtimecontract.AssistantScopeProject {
		ref = enumSchema(input.AgentRef)
	}
	var schema map[string]any
	switch kind {
	case "CREATE_INSTRUCTION_DRAFT":
		schema = objectSchema([]string{"projectAssistantRef", "instructions"}, map[string]any{
			"projectAssistantRef": ref, "instructions": assistantAgentInstructionsSchema(),
		})
	case "BIND_AGENT_RUNTIME_ENVIRONMENT":
		schema = objectSchema([]string{"projectAssistantRef", "environmentRef"}, map[string]any{
			"projectAssistantRef": ref, "environmentRef": opaqueRefSchema(),
		})
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		schema = environmentRevisionInputSchema(opaqueRefSchema(), nil)
		schema["properties"].(map[string]any)["projectAssistantRef"] = ref
		schema["required"] = []string{"projectAssistantRef"}
	default:
		return nil
	}
	schema["description"] = "Use the actual project assistant agent ref from ASSISTANTS, not a profile ref or ordinary employee. PROJECT can configure only itself; SYSTEM can configure an eligible project helper from any screen. Do not mix agentRef, systemAssistantRef, projectRef or owner fields. CP assigns versions and authority. For revision, environmentRef is optional; CP resolves the currently bound published environment and verifies any supplied locator."
	return schema
}

func assistantRuntimeConfigurationSchema(agentRef map[string]any) map[string]any {
	schema := objectSchema([]string{"agentRef", "runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode", "providerAccounts"}, map[string]any{
		"agentRef": agentRef, "runtimeProfileRef": assistantRuntimeProfileKeySchema(), "model": stringSchema(1, 128),
		"reasoningEffort": map[string]any{"type": "string", "minLength": 0, "maxLength": 64, "pattern": `^(?:[a-z][a-z0-9_-]{0,63})?$`}, "providerPolicyMode": enumSchema("FIXED", "LEAST_USED", "WEIGHTED"),
		"providerAccounts": map[string]any{"type": "array", "minItems": 1, "maxItems": 128,
			"items": objectSchema([]string{"accountRef", "weight"}, map[string]any{
				"accountRef": opaqueRefSchema(), "weight": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			})},
	})
	schema["description"] = "Prepare a reviewed assistant model configuration, not a TOML patch. Use exact refs from the server catalog. SYSTEM may configure itself or a project assistant; PROJECT may configure only itself. Empty reasoningEffort selects the eligible catalog default. FIXED requires one account; weights must be 1 unless policy is WEIGHTED. Control-plane verifies current catalog eligibility and assigns all versions and pins. Never include credentials or owner fields."
	return schema
}

func assistantCurrentRuntimeSchema() map[string]any {
	return map[string]any{"type": "object", "maxProperties": 5}
}

func assistantRuntimeProfileKeySchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9_-]{8,128}$", "maxLength": 128}
}

func assistantRoleEnvironmentKeySchema() map[string]any {
	return map[string]any{"type": "string", "pattern": assistantCatalogEnvironmentKeyPattern.String(), "maxLength": 100}
}

func assistantCurrentRuntime(input runtimecontract.RunnerInput) map[string]any {
	return map[string]any{"agent_ref": input.AgentRef, "runtime_profile_ref": input.RuntimeProfileRef,
		"provider_account_ref": input.ProviderAccountRef, "model": input.Model, "reasoning_effort": input.EffectiveReasoningEffort}
}

func projectSelfTargetSchema(input runtimecontract.RunnerInput, operation, kind, ownRef string) map[string]any {
	refs := []string{ownRef}
	if context := input.AssistantContext; context != nil && context.EntityKind == kind && context.EntityRef != "" && context.EntityRef != ownRef {
		for _, allowed := range context.AllowedOperations {
			if allowed == operation {
				refs = append(refs, context.EntityRef)
				break
			}
		}
	}
	return enumSchema(refs...)
}

func assistantAgentCapabilitySchema() map[string]any {
	return enumSchema("platform.artifact.manage", "platform.run.delegate", "platform.run.launch")
}

func assistantAgentInstructionsSchema() map[string]any {
	schema := stringSchema(20, 65536)
	schema["description"] = "Go template instructions. Stable scalar variables: {{ .organization.name }}, {{ .project.name }}, {{ .agent.name }}. Dynamic integrations: {{ range .integrations.items }} with .name, .description and .capability; include {{ else }} for the empty list and {{ end }}. Do not use i18n keys or index expressions."
	return schema
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

func connectionUpdateInputSchema(connectionRef string) map[string]any {
	schema := objectSchema([]string{"connectionRef"}, map[string]any{
		"connectionRef": enumSchema(connectionRef), "name": stringSchema(1, 160),
		"publicConfiguration": map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
	})
	schema["anyOf"] = []map[string]any{{"required": []string{"name"}}, {"required": []string{"publicConfiguration"}}}
	return schema
}

func workflowUpdateInputSchema(workflowRef string) map[string]any {
	graph := workflowInputSchema(opaqueRefSchema(), opaqueRefSchema())["properties"].(map[string]any)
	fields := graph["inputFields"].(map[string]any)
	field := fields["items"].(map[string]any)
	field["properties"].(map[string]any)["key"] = map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9_-]{0,79}$",
		"description": "Preserve the existing field key from the workflow readback; omit only for a new field."}
	steps := graph["steps"].(map[string]any)
	step := steps["items"].(map[string]any)
	step["properties"].(map[string]any)["key"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 96,
		"description": "Preserve the existing step key from the workflow readback; omit only for a new step."}
	schema := objectSchema([]string{"workflowRef"}, map[string]any{
		"workflowRef": enumSchema(workflowRef), "name": stringSchema(1, 160),
		"purpose": stringSchema(0, 2000), "coordinatorAgentRef": opaqueRefSchema(),
		"instructions":       stringSchema(0, 65536),
		"completionCriteria": stringSchema(0, 65536),
		"maxConcurrency":     map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"timeoutSeconds":     map[string]any{"type": "integer", "minimum": 1, "maximum": 604800},
		"inputFields":        fields, "steps": steps,
	})
	branches := make([]map[string]any, 0, 9)
	for _, field := range []string{"name", "purpose", "coordinatorAgentRef", "instructions", "completionCriteria", "maxConcurrency", "timeoutSeconds", "inputFields", "steps"} {
		branches = append(branches, map[string]any{"required": []string{field}})
	}
	schema["anyOf"] = branches
	return schema
}

func scheduleUpdateInputSchema(scheduleRef string) map[string]any {
	schema := objectSchema([]string{"scheduleRef"}, map[string]any{
		"scheduleRef": enumSchema(scheduleRef), "name": stringSchema(1, 160),
		"targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"preset":         enumSchema("HOURLY", "DAILY", "WEEKDAYS", "WEEKLY", "CUSTOM"),
		"cronExpression": stringSchema(0, 120), "timeOfDay": stringSchema(0, 5),
		"dayOfWeek": enumSchema("MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"),
		"timezone":  stringSchema(1, 80), "input": map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"automationText": stringSchema(1, 32768), "sessionPolicy": enumSchema("NEW_EACH_RUN", "CONTINUE_ONE"),
		"notificationPolicy": enumSchema("CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"),
	})
	branches := make([]map[string]any, 0, 12)
	for _, field := range []string{"name", "targetType", "targetRef", "preset", "cronExpression", "timeOfDay", "dayOfWeek", "timezone", "input", "automationText", "sessionPolicy", "notificationPolicy"} {
		branches = append(branches, map[string]any{"required": []string{field}})
	}
	schema["anyOf"] = branches
	return schema
}

func projectUpdateInputSchema(projectRef map[string]any) map[string]any {
	schema := objectSchema([]string{"projectRef"}, map[string]any{
		"projectRef": projectRef,
		"name":       stringSchema(1, 120),
		"purpose":    stringSchema(1, 1000),
		"language":   enumSchema("ru", "en"),
	})
	schema["anyOf"] = []map[string]any{
		{"required": []string{"name"}},
		{"required": []string{"purpose"}},
		{"required": []string{"language"}},
	}
	return schema
}

func integrationGrantInputSchema(context *runtimecontract.RunnerAssistantContext) map[string]any {
	properties := map[string]any{
		"connectionRef": opaqueRefSchema(), "capabilityKey": capabilityKeySchema(), "agentRef": opaqueRefSchema(), "workflowRef": opaqueRefSchema(),
		"enabled":            map[string]any{"type": "boolean"},
		"approvalPolicy":     enumSchema("NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"),
		"approvalScopePaths": map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": stringSchema(1, 200)},
	}
	if context != nil && context.EntityRef != "" {
		switch context.EntityKind {
		case "AGENT":
			properties["agentRef"] = enumSchema(context.EntityRef)
			delete(properties, "workflowRef")
			return objectSchema([]string{"connectionRef", "capabilityKey", "agentRef", "enabled", "approvalPolicy"}, properties)
		case "WORKFLOW":
			properties["workflowRef"] = enumSchema(context.EntityRef)
			delete(properties, "agentRef")
			return objectSchema([]string{"connectionRef", "capabilityKey", "workflowRef", "enabled", "approvalPolicy"}, properties)
		}
	}
	schema := objectSchema([]string{"connectionRef", "capabilityKey", "enabled", "approvalPolicy"}, properties)
	schema["oneOf"] = []map[string]any{
		{"required": []string{"agentRef"}, "not": map[string]any{"required": []string{"workflowRef"}}},
		{"required": []string{"workflowRef"}, "not": map[string]any{"required": []string{"agentRef"}}},
	}
	return schema
}

func assistantOperationSchema(kind string, parameters map[string]any) map[string]any {
	action := "CREATE"
	requiresVersion := false
	if kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT" || kind == "CREATE_INSTRUCTION_DRAFT" || kind == "UPDATE_WORKFLOW" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" || kind == "UPDATE_INTEGRATION_CONNECTION" || kind == "UPDATE_SCHEDULE" || kind == "CHANGE_CAPABILITY" || kind == "CHANGE_INTEGRATION_GRANT" || kind == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" || kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" || kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
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
	serverHydrated := assistantServerHydratedOperation(kind)
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
	schema := objectSchema([]string{"projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "timezone", "input", "automationText", "sessionPolicy", "notificationPolicy"}, map[string]any{
		"projectRef": projectRef, "name": stringSchema(1, 160), "targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"preset": enumSchema("HOURLY", "DAILY", "WEEKDAYS", "WEEKLY", "CUSTOM"), "cronExpression": stringSchema(0, 120), "timeOfDay": stringSchema(0, 5),
		"dayOfWeek": enumSchema("MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"), "timezone": stringSchema(1, 80),
		"input":              map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"automationText":     stringSchema(1, 32768),
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
