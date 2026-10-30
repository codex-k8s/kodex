package callback

// workflowResultRequirementsSchema describes editable predicates only, with the same four-result
// bound as the owner. Neither evidence receipts nor owner authority fields can be configured here.
// workflowResultRequirementsSchema описывает только редактируемые условия с owner-пределом четыре
// результата. Receipts доказательств и поля полномочий здесь не задаются.
func workflowResultRequirementsSchema() map[string]any {
	fields := map[string]any{
		"key": workflowStepKeySchema(), "fileName": stringSchema(1, 160), "mediaType": stringSchema(1, 160),
		"evidenceKey": workflowStepKeySchema(), "repository": stringSchema(1, 200), "subjectStepKey": workflowStepKeySchema(), "subjectResultKey": workflowStepKeySchema(), "targetRef": stringSchema(1, 96),
		"branch": map[string]any{"type": "string", "minLength": 1, "maxLength": 255, "pattern": `^[A-Za-z0-9][A-Za-z0-9_./-]{0,254}$`},
	}
	branches := make([]map[string]any, 0, 9)
	for _, kind := range []string{"ARTIFACT", "ARTIFACT_REVIEW", "REPOSITORY_FILE", "PR_COMMENT", "PULL_REQUEST", "REVIEW", "QA", "INTEGRATION_RECEIPT", "DEPLOYMENT"} {
		keys := []string{"key", "kind"}
		switch kind {
		case "ARTIFACT":
			keys = append(keys, "fileName", "mediaType")
		case "ARTIFACT_REVIEW":
			keys = append(keys, "subjectStepKey", "subjectResultKey")
		case "PULL_REQUEST":
			keys = append(keys, "repository")
		case "REPOSITORY_FILE":
			keys = append(keys, "repository", "targetRef", "branch", "fileName")
		case "PR_COMMENT":
			keys = append(keys, "repository", "targetRef")
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
		if kind == "QA" {
			properties["evidenceKey"] = fields["evidenceKey"]
		}
		if kind == "REPOSITORY_FILE" {
			properties["fileName"] = stringSchema(1, 1024)
		}
		if kind == "REPOSITORY_FILE" || kind == "PR_COMMENT" || kind == "PULL_REQUEST" {
			properties["targetRef"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 10, "pattern": `^[1-9][0-9]{0,9}$`}
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
	}, {
		"if": map[string]any{"required": []string{"resultEvidenceProfile"}, "properties": map[string]any{"resultEvidenceProfile": enumSchema("GITHUB_REVIEW_QA_V1", "GITHUB_PDLC_V1")}},
		"then": map[string]any{"properties": map[string]any{
			"executionMode": enumSchema("SERVER_DAG_V1"),
			"steps": map[string]any{"items": map[string]any{"properties": map[string]any{
				"resultRequirements": map[string]any{"items": map[string]any{"if": map[string]any{"required": []string{"kind"}, "properties": map[string]any{"kind": enumSchema("QA")}}, "then": map[string]any{"required": []string{"evidenceKey"}}}},
			}}},
		}},
	}, {
		"if": map[string]any{"required": []string{"resultEvidenceProfile"}, "properties": map[string]any{"resultEvidenceProfile": enumSchema("", "GITHUB_REVIEW_QA_V1")}},
		"then": map[string]any{"properties": map[string]any{"steps": map[string]any{"items": map[string]any{"properties": map[string]any{
			"resultRequirements": map[string]any{"items": workflowLegacyAuthorResultsSchema()},
		}}}}},
	}, {
		"if": map[string]any{"required": []string{"resultEvidenceProfile"}, "properties": map[string]any{"resultEvidenceProfile": enumSchema("GITHUB_PDLC_V1")}},
		"then": map[string]any{"properties": map[string]any{"steps": map[string]any{"items": map[string]any{"properties": map[string]any{
			"resultRequirements": map[string]any{"items": map[string]any{"if": map[string]any{"required": []string{"kind"}, "properties": map[string]any{"kind": enumSchema("PULL_REQUEST")}}, "then": map[string]any{"required": []string{"targetRef"}}}},
		}}}}},
	}}
}

// workflowLegacyAuthorResultsSchema excludes native author selectors from explicit old profiles.
// It remains separate from sparse-update inference: omitted profile is resolved by owner readback.
// workflowLegacyAuthorResultsSchema исключает native author selectors из явных прежних профилей.
// Отсутствующий profile при sparse update отдельно разрешает owner по своему readback.
func workflowLegacyAuthorResultsSchema() map[string]any {
	return map[string]any{"not": map[string]any{"anyOf": []map[string]any{
		{"required": []string{"kind"}, "properties": map[string]any{"kind": enumSchema("REPOSITORY_FILE", "PR_COMMENT")}},
		{"required": []string{"branch"}},
		{"required": []string{"kind", "targetRef"}, "properties": map[string]any{"kind": enumSchema("PULL_REQUEST")}},
	}}}
}

// workflowCreationBounds rejects server-only step fields in legacy creation, matching the
// owner parser. Updates retain separate rules because omitted mode can refer to an existing DAG.
// workflowCreationBounds запрещает серверные поля шага при создании прежнего процесса,
// как и owner parser. При изменении отсутствие режима может обозначать существующий DAG.
func workflowCreationBounds() []map[string]any {
	bounds := workflowServerBounds()
	bounds[0]["else"] = map[string]any{"properties": map[string]any{
		"steps": map[string]any{"items": map[string]any{"not": map[string]any{"anyOf": []map[string]any{
			{"required": []string{"key"}}, {"required": []string{"dependsOn"}}, {"required": []string{"resultRequirements"}},
		}}}},
	}}
	bounds = append(bounds, map[string]any{
		"if":   map[string]any{"required": []string{"resultEvidenceProfile"}},
		"then": map[string]any{"required": []string{"executionMode"}, "properties": map[string]any{"executionMode": enumSchema("SERVER_DAG_V1")}},
	})
	bounds = append(bounds, map[string]any{
		"if": map[string]any{"not": map[string]any{"required": []string{"resultEvidenceProfile"}}},
		"then": map[string]any{"properties": map[string]any{"steps": map[string]any{"items": map[string]any{"properties": map[string]any{
			"resultRequirements": map[string]any{"items": map[string]any{"allOf": []map[string]any{workflowLegacyAuthorResultsSchema(), {"not": map[string]any{"required": []string{"evidenceKey"}}}}}},
		}}}}},
	})
	return bounds
}
