package codex

// safeAccountReadFailure rechecks the closed reason vocabulary at the logging boundary, preserving known reasons/NONE/UNKNOWN and collapsing every arbitrary string to UNKNOWN without trimming or guessing from text.
// safeAccountReadFailure повторно сверяет закрытый словарь причин на logging boundary, сохраняя известные причины/NONE/UNKNOWN и заменяя произвольные строки на UNKNOWN без обрезки и угадывания по тексту.
func safeAccountReadFailure(reason string) string {
	switch reason {
	case "REQUIREMENTS_LOAD", "MISSING_ACCOUNT_ID", "DISCOVERY_CANCELLED", "ACCOUNT_CHANGED",
		"DISCOVERY_UNAUTHORIZED", "DISCOVERY_FAILED", "MISSING_WORKSPACE", "DUPLICATE_WORKSPACE",
		"REQUIREMENTS_RELOAD", "CONFIGURATION_CHANGED", "SHUTDOWN", "DISCOVERY_TIMEOUT",
		"MISSING_BACKEND_ORIGIN", "INVALID_ROUTING_OVERRIDE", "BACKEND_IS_NOT_ORIGIN",
		"BACKEND_CONFLICT", "INVALID_BACKEND_URL", "INVALID_BACKEND_ORIGIN", "NONE", "UNKNOWN":
		return reason
	default:
		return "UNKNOWN"
	}
}
