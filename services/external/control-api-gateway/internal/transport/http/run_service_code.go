package httptransport

// Код назначается только служебному RunEvent до локализации summary.
// Содержимое сообщений, результатов инструментов и внешних эффектов не скрывается.
func projectRunEventServiceCode(value map[string]any) {
	for _, field := range []string{"message", "toolCall", "artifact", "artifactRef", "progress", "integrationInvocationRef"} {
		if _, present := value[field]; present {
			return
		}
	}
	if value["summary"] == "i18n:ROOT_PROCESS_COMPLETED" {
		projectRootCompletionServiceCode(value)
		return
	}
	switch value["type"] {
	case "RUN_EVENT_TYPE_RUN_STATE_CHANGED", "RUN_EVENT_TYPE_NODE_STATE_CHANGED", "RUN_EVENT_TYPE_TURN_PROGRESS", "RUN_EVENT_TYPE_TURN_COMPLETED":
	default:
		return
	}
	switch value["messageKind"] {
	case "RUN_EVENT_MESSAGE_KIND_STATE", "RUN_EVENT_MESSAGE_KIND_INTERMEDIATE_MESSAGE":
	default:
		return
	}
	state := value["runState"]
	if nodeState, present := value["nodeState"]; present {
		state = nodeState
	}
	if state != "RUN_STATE_CANCELLED" && state != "RUN_NODE_STATE_CANCELLED" {
		return
	}
	switch value["summary"] {
	case "i18n:RUN_CANCELLED":
		value["serviceCode"] = "RUN_CANCELLED"
	case "i18n:RUN_NODE_CANCELLED":
		value["serviceCode"] = "RUN_NODE_CANCELLED"
	case "i18n:ASSISTANT_TURN_CANCELLED":
		value["serviceCode"] = "ASSISTANT_TURN_CANCELLED"
	}
}

// Только каноническое служебное завершение, не любая ошибка корневого узла.
func projectRootCompletionServiceCode(value map[string]any) {
	for _, field := range []string{"gate", "gateRef", "incident", "edge", "edgeRef"} {
		if _, present := value[field]; present {
			return
		}
	}
	node, ok := value["node"].(map[string]any)
	if !ok || value["type"] != "RUN_EVENT_TYPE_NODE_STATE_CHANGED" ||
		value["messageKind"] != "RUN_EVENT_MESSAGE_KIND_STATE" ||
		node["type"] != "RUN_NODE_TYPE_ROOT_PROCESS" ||
		node["ref"] == nil || node["ref"] != value["nodeRef"] || node["state"] != value["nodeState"] {
		return
	}
	switch value["nodeState"] {
	case "RUN_NODE_STATE_SUCCEEDED", "RUN_NODE_STATE_FAILED", "RUN_NODE_STATE_CANCELLED":
		value["serviceCode"] = "ROOT_PROCESS_COMPLETED"
	}
}
