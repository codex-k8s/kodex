package callback

func assistantPlanTool() map[string]any {
	return map[string]any{
		"name":        "propose_configuration_plan",
		"description": "Propose a bounded MatterCodex configuration plan for explicit user approval. This tool never applies the plan.",
		"inputSchema": objectSchema([]string{"summary", "operations"}, map[string]any{
			"summary": stringSchema(1, 2000),
			"operations": map[string]any{"type": "array", "minItems": 1, "maxItems": 32,
				"items": map[string]any{"oneOf": assistantPlanOperationSchemas()}},
		}),
	}
}

func assistantPlanOperationSchemas() []map[string]any {
	return []map[string]any{
		assistantOperationSchema("CREATE_PROJECT", objectSchema([]string{"name", "purpose", "language"}, map[string]any{
			"name": stringSchema(1, 160), "purpose": stringSchema(1, 2000), "language": enumSchema("ru", "en"),
		})),
		assistantOperationSchema("CREATE_AGENT", objectSchema([]string{"projectRef", "name", "purpose", "roleDescription", "instructions"}, map[string]any{
			"projectRef": opaqueRefSchema(), "roleDefinitionRef": opaqueRefSchema(), "name": stringSchema(1, 160),
			"purpose": stringSchema(1, 2000), "roleDescription": stringSchema(1, 2000), "avatarUrl": stringSchema(0, 500),
			"runtimeRef": opaqueRefSchema(), "instructions": stringSchema(20, 65536),
		})),
		assistantOperationSchema("CREATE_WORKFLOW", workflowInputSchema()),
		assistantOperationSchema("CHANGE_CAPABILITY", objectSchema([]string{"agentRef", "capabilityKey", "enabled", "expectedVersion"}, map[string]any{
			"agentRef": opaqueRefSchema(), "capabilityKey": capabilityKeySchema(), "enabled": map[string]any{"type": "boolean"},
			"expectedVersion": map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991},
		})),
		assistantOperationSchema("CHANGE_INTEGRATION_GRANT", integrationGrantInputSchema()),
		assistantOperationSchema("CREATE_INTEGRATION_CONNECTION", objectSchema([]string{"definitionKey", "name", "publicConfiguration"}, map[string]any{
			"definitionKey": capabilityKeySchema(), "name": stringSchema(1, 160),
			"publicConfiguration": map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		})),
		assistantOperationSchema("TEST_INTEGRATION_CONNECTION", objectSchema([]string{"connectionRef", "expectedVersion"}, map[string]any{
			"connectionRef": opaqueRefSchema(), "expectedVersion": map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991},
		})),
		assistantOperationSchema("CREATE_SCHEDULE", scheduleInputSchema()),
		assistantOperationSchema("LAUNCH_RUN", runInputSchema()),
	}
}

func integrationGrantInputSchema() map[string]any {
	schema := objectSchema([]string{"connectionRef", "capabilityKey", "enabled", "expectedVersion"}, map[string]any{
		"connectionRef": opaqueRefSchema(), "capabilityKey": capabilityKeySchema(), "agentRef": opaqueRefSchema(), "workflowRef": opaqueRefSchema(),
		"enabled": map[string]any{"type": "boolean"}, "expectedVersion": map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991},
	})
	schema["oneOf"] = []map[string]any{
		{"required": []string{"agentRef"}, "not": map[string]any{"required": []string{"workflowRef"}}},
		{"required": []string{"workflowRef"}, "not": map[string]any{"required": []string{"agentRef"}}},
	}
	return schema
}

func assistantOperationSchema(kind string, input map[string]any) map[string]any {
	return objectSchema([]string{"type", "summary", "input"}, map[string]any{
		"type": map[string]any{"const": kind}, "summary": stringSchema(1, 500), "input": input,
	})
}

func workflowInputSchema() map[string]any {
	inputField := objectSchema([]string{"label", "valueType", "required", "options"}, map[string]any{
		"label": stringSchema(1, 160), "description": stringSchema(0, 500),
		"valueType": enumSchema("TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT"),
		"required":  map[string]any{"type": "boolean"},
		"options":   map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": stringSchema(1, 160)},
	})
	step := objectSchema([]string{"name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys"}, map[string]any{
		"name": stringSchema(1, 160), "purpose": stringSchema(1, 1000), "agentRef": opaqueRefSchema(),
		"parallel": map[string]any{"type": "boolean"}, "parallelGroup": map[string]any{"type": "integer", "minimum": 0, "maximum": 50},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 86400}, "expectedResult": stringSchema(0, 1000),
		"humanGate": map[string]any{"type": "boolean"}, "gateDecisions": stringArraySchema(0, 4, []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}),
		"requiredCapabilityKeys": map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": capabilityKeySchema()},
	})
	return objectSchema([]string{"projectRef", "name", "purpose", "coordinatorAgentRef", "steps"}, map[string]any{
		"projectRef": opaqueRefSchema(), "name": stringSchema(1, 160), "purpose": stringSchema(1, 1000), "coordinatorAgentRef": opaqueRefSchema(),
		"inputFields":    map[string]any{"type": "array", "maxItems": 100, "items": inputField},
		"steps":          map[string]any{"type": "array", "minItems": 1, "maxItems": 200, "items": step},
		"maxConcurrency": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 604800}, "completionCriteria": stringSchema(0, 2000),
	})
}

func scheduleInputSchema() map[string]any {
	return objectSchema([]string{"projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "timezone", "input", "sessionPolicy", "notificationPolicy"}, map[string]any{
		"projectRef": opaqueRefSchema(), "name": stringSchema(1, 160), "targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"preset": stringSchema(1, 120), "timeOfDay": stringSchema(0, 5), "dayOfWeek": stringSchema(0, 9), "timezone": stringSchema(1, 80),
		"input":              map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"sessionPolicy":      enumSchema("NEW_EACH_RUN", "CONTINUE_ONE"),
		"notificationPolicy": enumSchema("CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"),
	})
}

func runInputSchema() map[string]any {
	return objectSchema([]string{"projectRef", "targetType", "targetRef", "title", "task", "input"}, map[string]any{
		"projectRef": opaqueRefSchema(), "targetType": enumSchema("AGENT", "WORKFLOW"), "targetRef": opaqueRefSchema(),
		"title": stringSchema(1, 240), "task": stringSchema(1, 32768), "sessionRef": opaqueRefSchema(),
		"input":        map[string]any{"type": "object", "maxProperties": 100, "additionalProperties": true},
		"artifactRefs": map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": opaqueRefSchema()},
	})
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
