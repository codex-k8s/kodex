package callback

// workflowResultRequirementsSchema describes editable predicates only, with the same four-result
// bound as the owner. Neither evidence receipts nor owner authority fields can be configured here.
// workflowResultRequirementsSchema описывает только редактируемые условия с owner-пределом четыре
// результата. Receipts доказательств и поля полномочий здесь не задаются.
func workflowResultRequirementsSchema() map[string]any {
	fields := map[string]any{
		"key": workflowStepKeySchema(), "fileName": stringSchema(1, 160), "mediaType": stringSchema(1, 160),
		"repository": stringSchema(1, 200), "subjectStepKey": workflowStepKeySchema(), "targetRef": stringSchema(1, 96),
	}
	branches := make([]map[string]any, 0, 6)
	for _, kind := range []string{"ARTIFACT", "PULL_REQUEST", "REVIEW", "QA", "INTEGRATION_RECEIPT", "DEPLOYMENT"} {
		keys := []string{"key", "kind"}
		switch kind {
		case "ARTIFACT":
			keys = append(keys, "fileName", "mediaType")
		case "PULL_REQUEST":
			keys = append(keys, "repository")
		case "REVIEW", "QA":
			keys = append(keys, "subjectStepKey")
		case "INTEGRATION_RECEIPT", "DEPLOYMENT":
			keys = append(keys, "repository", "subjectStepKey", "operation")
			if kind == "DEPLOYMENT" {
				keys = append(keys, "targetRef")
			}
		}
		properties := make(map[string]any, len(keys))
		for _, key := range keys {
			properties[key] = fields[key]
		}
		properties["kind"] = enumSchema(kind)
		if kind == "INTEGRATION_RECEIPT" {
			properties["operation"] = enumSchema("github.pull_request.merge")
		}
		if kind == "DEPLOYMENT" {
			properties["operation"] = enumSchema("ssh.compose.deploy")
		}
		branches = append(branches, objectSchema(keys, properties))
	}
	return map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": map[string]any{"oneOf": branches}}
}

// workflowStepKeySchema advertises a stable logical key rather than a globally authorized resource.
// workflowStepKeySchema описывает устойчивый логический ключ, а не ресурс с глобальными полномочиями.
func workflowStepKeySchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 96, "pattern": `^[A-Za-z0-9_\-]+$`}
}

// workflowServerBounds applies the bounded first profile only when server mode is explicitly
// selected. Sparse updates without mode still require final validation against owner readback.
// workflowServerBounds применяет ограничения первого профиля только при явном серверном режиме.
// Частичное изменение без режима всё равно проверяется по итоговому owner readback.
func workflowServerBounds() []map[string]any {
	return []map[string]any{{
		"if": map[string]any{"required": []string{"executionMode"}, "properties": map[string]any{"executionMode": enumSchema("SERVER_DAG_V1")}},
		"then": map[string]any{"properties": map[string]any{
			"maxConcurrency": map[string]any{"maximum": 2},
			"steps":          map[string]any{"maxItems": 12, "items": map[string]any{"required": []string{"key", "dependsOn", "resultRequirements"}, "properties": map[string]any{"key": workflowStepKeySchema()}}},
		}},
	}}
}
