package codex

import (
	"encoding/json"
	"unicode/utf8"
)

// closedAccountReadFailure classifies only exact static messages from the pinned direct account/read internal-error profile after method/code and bounded-envelope checks. Unrelated calls return NONE and malformed or unrecognized content returns UNKNOWN; raw message/data never becomes a returned diagnostic or authority decision.
// closedAccountReadFailure классифицирует лишь точные статические сообщения pinned direct account/read internal-error profile после method/code и bounded-envelope checks. Чужие вызовы дают NONE, malformed или неизвестные данные — UNKNOWN; сырые message/data не становятся возвращённой диагностикой или authority decision.
// Историческая привязка: rust-v0.160.0, codex-rs/app-server/src/request_processors/account_processor/workspace_routing.rs:57–104,157–168; session-only variants не относятся к direct read_account(None).
func closedAccountReadFailure(method string, code int64, raw json.RawMessage) string {
	if method != "account/read" || code != -32603 {
		return "NONE"
	}
	fields, valid := decodeAccountRPCErrorFields(raw, code)
	if !valid {
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

// decodeAccountRPCErrorFields checks the common bounded UTF-8 error envelope, exact code/message schema and expected numeric RPC code. Method correlation, data eligibility and message interpretation remain with each caller; raw fields stay private to diagnostic classification.
// decodeAccountRPCErrorFields проверяет общий ограниченный UTF-8 error envelope, точную code/message schema и ожидаемый numeric RPC code. Method correlation, допустимость data и толкование message остаются у вызывающих; сырые поля остаются приватными для diagnostic classification.
func decodeAccountRPCErrorFields(raw json.RawMessage, expectedCode int64) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > maximumDiagnosticBytes || !utf8.Valid(raw) {
		return nil, false
	}
	fields, err := decodeObject(raw, schema([]string{"code", "message"}, "code", "message", "data"))
	if err != nil {
		return nil, false
	}
	var code int64
	if strictDecode(fields["code"], &code) != nil || code != expectedCode {
		return nil, false
	}
	return fields, true
}
