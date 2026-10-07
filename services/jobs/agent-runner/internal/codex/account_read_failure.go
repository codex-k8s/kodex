package codex

import (
	"encoding/json"
	"unicode/utf8"
)

// closedAccountReadFailure классифицирует только статические ошибки прямого
// account/read закреплённого Codex 0.160.0. Источник: официальный tag
// rust-v0.160.0, codex-rs/app-server/src/request_processors/account_processor/
// workspace_routing.rs:57–104,157–168. Session-only variants недостижимы при
// read_account(None). Диагностика не назначает authority и не меняет outcome.
// Исходные message/data остаются только локальным входом и не возвращаются.
func closedAccountReadFailure(method string, code int64, raw json.RawMessage) string {
	if method != "account/read" || code != -32603 {
		return "NONE"
	}
	if len(raw) == 0 || len(raw) > maximumDiagnosticBytes || !utf8.Valid(raw) {
		return "UNKNOWN"
	}
	fields, err := decodeObject(raw, schema([]string{"code", "message"}, "code", "message", "data"))
	if err != nil {
		return "UNKNOWN"
	}
	var embeddedCode int64
	if strictDecode(fields["code"], &embeddedCode) != nil || embeddedCode != code {
		return "UNKNOWN"
	}
	message, err := decodeBoundedString(fields["message"], maximumDiagnosticBytes)
	if err != nil {
		return "UNKNOWN"
	}
	switch message {
	case "failed to load workspace requirements":
		return "REQUIREMENTS_LOAD"
	case "workspace routing requires a ChatGPT account id":
		return "MISSING_ACCOUNT_ID"
	case "workspace routing discovery cancelled":
		return "DISCOVERY_CANCELLED"
	case "account changed during workspace routing discovery":
		return "ACCOUNT_CHANGED"
	case "workspace routing discovery unauthorized (401)":
		return "DISCOVERY_UNAUTHORIZED"
	case "workspace routing discovery failed":
		return "DISCOVERY_FAILED"
	case "selected workspace missing from routing discovery":
		return "MISSING_WORKSPACE"
	case "duplicate workspace in routing discovery":
		return "DUPLICATE_WORKSPACE"
	case "failed to reload workspace requirements":
		return "REQUIREMENTS_RELOAD"
	case "configuration changed during workspace routing discovery; retry account/read":
		return "CONFIGURATION_CHANGED"
	case "workspace routing discovery cancelled during shutdown":
		return "SHUTDOWN"
	case "workspace routing discovery timed out":
		return "DISCOVERY_TIMEOUT"
	case "workspace routing discovery missing backend origin":
		return "MISSING_BACKEND_ORIGIN"
	case "workspace routing discovery has invalid account routing override":
		return "INVALID_ROUTING_OVERRIDE"
	case "workspace routing discovery must return an origin":
		return "BACKEND_IS_NOT_ORIGIN"
	case "required ChatGPT backend conflicts with workspace routing":
		return "BACKEND_CONFLICT"
	case "invalid workspace backend URL":
		return "INVALID_BACKEND_URL"
	case "workspace backend must use an HTTPS origin without credentials":
		return "INVALID_BACKEND_ORIGIN"
	default:
		return "UNKNOWN"
	}
}

// safeAccountReadFailure повторно ограничивает значение на logging boundary.
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
