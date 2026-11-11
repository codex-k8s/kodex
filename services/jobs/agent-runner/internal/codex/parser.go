package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

const (
	// maximumJSONLLineBytes bounds selected semantic command/diff/result strings, not the larger transport frame.
	// maximumJSONLLineBytes ограничивает отдельные смысловые строки command/diff/result, а не больший транспортный кадр.
	maximumJSONLLineBytes = 1 << 20
	// maximumJSONLMessages limits accepted JSONL event count independently of frame size or tool-call quotas.
	// maximumJSONLMessages ограничивает число JSONL событий отдельно от размера кадров и квот вызовов инструментов.
	maximumJSONLMessages = 100_000
	// maximumFinalBytes caps parsed agent text in UTF-8 bytes; published activity has its own tighter limit.
	// maximumFinalBytes ограничивает разобранный agent text в UTF-8 байтах; публичное событие имеет отдельный меньший лимит.
	maximumFinalBytes = 480_000
	// maximumDiagnosticBytes bounds parsed provider diagnostics without making their original text safe to publish.
	// maximumDiagnosticBytes ограничивает разобранную диагностику провайдера, не делая её исходный текст безопасным для публикации.
	maximumDiagnosticBytes = 16 << 10
	// maximumResponseReceipts caps the in-memory response-ID replay fence for one protocol execution.
	// maximumResponseReceipts ограничивает хранимые response-ID квитанции защиты от повторов одного исполнения.
	maximumResponseReceipts = 10_000
)

// UsageCompleteness aliases the shared wire quality without changing the broker enum layout.
// UsageCompleteness использует общее качество wire без изменения enum broker.
type UsageCompleteness = runtimecontract.UsageCompleteness

const (
	// UsageUnknown preserves the zero-valued optional broker field for missing observations.
	// UsageUnknown сохраняет нулевое необязательное broker-поле для отсутствующих наблюдений.
	UsageUnknown = runtimecontract.UsageUnknown
	// UsagePartial preserves confirmed subtotals without asserting complete measurement.
	// UsagePartial сохраняет подтверждённые подытоги без утверждения полноты измерений.
	UsagePartial = runtimecontract.UsagePartial
	// UsageComplete preserves explicit numeric zero and complete terminal observations.
	// UsageComplete сохраняет явный числовой ноль и полные terminal-наблюдения.
	UsageComplete = runtimecontract.UsageComplete
)

// Result carries the public outcome, safe native facts, measured usage and archive metadata by value.
// Private capture/diagnostic pointers establish additional provenance that arbitrary exported fields or
// JSON cannot recreate; public metadata alone is not proof of a completed or captured execution.
// Result переносит публичный исход, безопасные native факты, измеренный usage и метаданные архива.
// Приватные capture/diagnostic указатели дают дополнительное происхождение, не восстанавливаемое из
// публичных полей или JSON; одни метаданные не доказывают завершение или захват исполнения.
type Result struct {
	FinalMessage       string
	Outcome            string
	FailureCode        string
	Usage              runtimecontract.TokenUsage
	UsageCompleteness  UsageCompleteness                `json:"UsageCompleteness,omitempty"`
	ToolCalls          []runtimecontract.NativeToolCall `json:"ToolCalls,omitempty"`
	providerDiagnostic *runtimecontract.ProviderFailureDiagnostic
	workspaceJoin      *workspaceJoinProof
	NativeSession      *runtimecontract.NativeSessionCheckpoint `json:"NativeSession,omitempty"`
	nativeCapture      *nativeCaptureProof
}

// ProviderDiagnostic returns a copy only of the private diagnostic matching the exact input binding;
// it cannot manufacture provenance from caller-assigned public Result fields or decoded JSON.
// ProviderDiagnostic возвращает копию лишь приватной диагностики с точной привязкой к input;
// из публичных полей Result или декодированного JSON происхождение не создаётся.
func (result Result) ProviderDiagnostic(input model.Input) *runtimecontract.ProviderFailureDiagnostic {
	if result.providerDiagnostic == nil || !result.providerDiagnostic.Matches(input) {
		return nil
	}
	value := *result.providerDiagnostic
	return &value
}

// WithWorkspaceDiagnostic attaches bounded runner-owned completion diagnostics to
// the exact execution tuple; invalid pins/reasons cannot produce a diagnostic.
// WithWorkspaceDiagnostic прикрепляет ограниченную диагностику завершения runner
// к точной попытке; неверные pins или причины не создают диагностическую запись.
func (result Result) WithWorkspaceDiagnostic(input model.Input, stage, reason string) Result {
	result.providerDiagnostic = bindProviderDiagnostic(input, runtimecontract.ProviderFailureDiagnostic{
		Kind: "TERMINAL_FAILURE", Stage: stage, Class: "WORKSPACE", Detail: reason,
		Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE", TerminalCode: "RUNTIME_ARTIFACT_INVALID",
	})
	return result
}

// objectSchema describes allowed and required immediate object keys; value types and nested schemas
// are the consuming parser's responsibility, not implied by required-key presence.
// objectSchema описывает допустимые и обязательные ключи текущего объекта; типы значений и вложенные
// схемы проверяет потребитель, а не само наличие обязательного ключа.
type objectSchema struct {
	allowed  map[string]struct{}
	required map[string]struct{}
}

// schema builds independent key sets for a closed object shape without validating payload values.
// schema создаёт независимые наборы ключей закрытого объекта без проверки значений payload.
func schema(required []string, allowed ...string) objectSchema {
	value := objectSchema{allowed: make(map[string]struct{}, len(allowed)), required: make(map[string]struct{}, len(required))}
	for _, field := range allowed {
		value.allowed[field] = struct{}{}
	}
	for _, field := range required {
		value.required[field] = struct{}{}
	}
	return value
}

// tokenUsageFailureReason names internal accounting/parser failures; arbitrary strings require
// normalization before becoming closed diagnostics.
// tokenUsageFailureReason задаёт внутренние ошибки учёта и разбора; произвольная строка требует
// нормализации до использования в закрытой диагностике.
type tokenUsageFailureReason string

const (
	// tokenUsageStructure marks a rejected object shape rather than a valid partial numeric observation.
	// tokenUsageStructure отмечает отклонённую структуру объекта, а не корректное частичное числовое наблюдение.
	tokenUsageStructure tokenUsageFailureReason = "TOKEN_USAGE_STRUCTURE"
	// tokenUsageRequiredMissing distinguishes an absent required counter from a present zero.
	// tokenUsageRequiredMissing отличает отсутствующий обязательный счётчик от присутствующего нуля.
	tokenUsageRequiredMissing tokenUsageFailureReason = "TOKEN_USAGE_REQUIRED_MISSING"
	// tokenUsageRequiredNull rejects explicit null instead of defaulting a required counter to zero.
	// tokenUsageRequiredNull отвергает явный null вместо обнуления обязательного счётчика.
	tokenUsageRequiredNull tokenUsageFailureReason = "TOKEN_USAGE_REQUIRED_NULL"
	// tokenUsageRequiredType marks a required counter or response identifier that fails its typed decoder.
	// tokenUsageRequiredType отмечает обязательный счётчик или response identifier, не прошедший typed decoder.
	tokenUsageRequiredType tokenUsageFailureReason = "TOKEN_USAGE_REQUIRED_TYPE"
	// tokenUsageOptionalNull rejects null where only omission gives the optional counter its default.
	// tokenUsageOptionalNull отвергает null там, где значение optional счётчика по умолчанию даёт лишь отсутствие поля.
	tokenUsageOptionalNull tokenUsageFailureReason = "TOKEN_USAGE_OPTIONAL_NULL"
	// tokenUsageOptionalType records invalid present optional counters, metadata amount or context-window types.
	// tokenUsageOptionalType фиксирует неверный тип присутствующего optional счётчика, metadata amount или context window.
	tokenUsageOptionalType tokenUsageFailureReason = "TOKEN_USAGE_OPTIONAL_TYPE"
	// tokenUsageNegative rejects negative counters/window sizes before accepting an observation.
	// tokenUsageNegative отвергает отрицательные счётчики или window size до принятия наблюдения.
	tokenUsageNegative tokenUsageFailureReason = "TOKEN_USAGE_NEGATIVE"
	// tokenUsageTotalArithmetic marks failed measured total arithmetic, not an approximate display estimate.
	// tokenUsageTotalArithmetic отмечает неверную арифметику измеренного total, а не приблизительную display оценку.
	tokenUsageTotalArithmetic tokenUsageFailureReason = "TOKEN_USAGE_TOTAL_ARITHMETIC"
	// tokenUsageCacheInputBound rejects a cached/write-cache component exceeding its input counter.
	// tokenUsageCacheInputBound отвергает cached/write-cache компонент выше счётчика input.
	tokenUsageCacheInputBound tokenUsageFailureReason = "TOKEN_USAGE_CACHE_INPUT_BOUND"
	// tokenUsageReasoningOutputBound rejects reasoning output exceeding the associated output count.
	// tokenUsageReasoningOutputBound отвергает reasoning output выше соответствующего output.
	tokenUsageReasoningOutputBound tokenUsageFailureReason = "TOKEN_USAGE_REASONING_OUTPUT_BOUND"
	// tokenUsageLastExceedsTotal marks a last display component above its cumulative counterpart.
	// tokenUsageLastExceedsTotal отмечает last display компонент выше соответствующего накопленного значения.
	tokenUsageLastExceedsTotal tokenUsageFailureReason = "TOKEN_USAGE_LAST_EXCEEDS_TOTAL"
	// tokenUsageReceiptConflict rejects reusing one response ID for a changed pinned payload.
	// tokenUsageReceiptConflict отвергает повторное использование response ID с изменившимся закреплённым payload.
	tokenUsageReceiptConflict tokenUsageFailureReason = "TOKEN_USAGE_RECEIPT_CONFLICT"
	// tokenUsageReceiptLimit fails closed when the bounded response-receipt ledger is full.
	// tokenUsageReceiptLimit даёт закрытый отказ при заполнении ограниченного реестра response receipts.
	tokenUsageReceiptLimit tokenUsageFailureReason = "TOKEN_USAGE_RECEIPT_LIMIT"
	// tokenUsageOverflow rejects int64 arithmetic overflow before committing new measured counters.
	// tokenUsageOverflow отвергает переполнение int64 до фиксации новых измеренных счётчиков.
	tokenUsageOverflow tokenUsageFailureReason = "TOKEN_USAGE_OVERFLOW"
)

// tokenUsageFailure carries only a reason with fixed Error text, never a counter value or provider JSON.
// The closed diagnostic selector still rejects unregistered reason strings.
// tokenUsageFailure переносит лишь причину с фиксированным Error без счётчика или provider JSON.
// Закрытая диагностическая проекция дополнительно отвергает неизвестные строки причины.
type tokenUsageFailure struct{ reason tokenUsageFailureReason }

// Error emits the constant token-usage failure message without dereferencing or formatting the reason.
// Error выдаёт постоянное сообщение отказа usage без разыменования или форматирования причины.
func (*tokenUsageFailure) Error() string { return "Codex app-server token usage is invalid" }

// safeTokenUsageFailureReason maps only registered reason values to public-safe labels, otherwise UNKNOWN.
// safeTokenUsageFailureReason выдаёт безопасные labels лишь зарегистрированным причинам, иначе UNKNOWN.
func safeTokenUsageFailureReason(reason tokenUsageFailureReason) string {
	switch reason {
	case tokenUsageStructure, tokenUsageRequiredMissing, tokenUsageRequiredNull, tokenUsageRequiredType,
		tokenUsageOptionalNull, tokenUsageOptionalType, tokenUsageNegative, tokenUsageTotalArithmetic,
		tokenUsageCacheInputBound, tokenUsageReasoningOutputBound, tokenUsageLastExceedsTotal,
		tokenUsageReceiptConflict, tokenUsageReceiptLimit, tokenUsageOverflow:
		return string(reason)
	default:
		return "UNKNOWN"
	}
}

// decodeObject rejects duplicate/unknown immediate keys, missing required keys and trailing JSON,
// returning raw values for typed/nested validation. Required presence does not reject null by itself;
// errors may contain an untrusted key name and must be wrapped/classified before outward logging.
// decodeObject отвергает повторные или лишние текущие ключи, отсутствие обязательных и лишний JSON,
// возвращая raw values для typed/nested проверки. Наличие ключа само не запрещает null; ошибка может
// содержать недоверенное имя поля и требует обёртки или классификации перед внешним логом.
func decodeObject(raw []byte, expected objectSchema) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("JSON value is not an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok {
			return nil, errors.New("JSON object key is invalid")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("JSON object key is duplicated")
		}
		if _, allowed := expected.allowed[key]; !allowed {
			return nil, fmt.Errorf("JSON object field %q is not allowed", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || len(value) == 0 {
			return nil, errors.New("JSON object value is invalid")
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("JSON object is incomplete")
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	for required := range expected.required {
		if _, present := fields[required]; !present {
			return nil, fmt.Errorf("JSON object field %q is required", required)
		}
	}
	return fields, nil
}

// strictDecode requires one complete JSON value and disallows unknown struct fields, but ordinary
// encoding/json semantics still apply to nulls, maps and duplicate keys. Use explicit object/recursive
// checks where that boundary requires uniqueness or non-null values.
// strictDecode требует одно полное JSON значение и запрещает неизвестные struct fields, но сохраняет
// обычные правила encoding/json для null, map и duplicate keys. Требуемую границей уникальность
// или non-null обеспечивают отдельные object/recursive проверки.
func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

// ensureEOF requires the decoder's next value to be EOF, rejecting another value or malformed suffix.
// ensureEOF требует EOF следующим значением decoder, отвергая второй объект или повреждённый хвост.
func ensureEOF(decoder *json.Decoder) error {
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("JSON value has trailing data")
	}
	return nil
}

// decodeBoundedString accepts only a complete nonempty decoded UTF-8 string within the byte limit;
// it does not cap allocation before JSON decoding or interpret content as authority.
// decodeBoundedString принимает лишь полную непустую декодированную UTF-8 строку в байтовом лимите;
// она не ограничивает выделение до JSON decode и не трактует содержимое как полномочия.
func decodeBoundedString(raw []byte, maximum int) (string, error) {
	var value string
	if strictDecode(raw, &value) != nil || value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return "", errors.New("JSON string is invalid")
	}
	return value, nil
}

// CapacityFailure classifies only server_overloaded as capacity, never quota, policy, authentication
// or every provider failure that happens to have a retry-later presentation.
// CapacityFailure считает capacity лишь server_overloaded, не квоту, policy, auth или любой
// provider failure с отображением retry-later.
func CapacityFailure(code string) bool {
	return code == "server_overloaded"
}

// TerminalPresentation maps typed closed codes to fixed outcome/i18n/action triples; raw provider
// diagnostics never become user text or authority. Unknown codes remain failed with a safe fallback,
// and presentation alone is not the complete admission/retry policy.
// TerminalPresentation переводит закрытые typed codes в фиксированные outcome/i18n/action; исходная
// диагностика не становится текстом пользователя или полномочиями. Неизвестный code даёт безопасный
// failed fallback, а одно отображение не является полной admission/retry policy.
func TerminalPresentation(code string) (outcome, markdown, nextAction string) {
	switch code {
	case "unauthorized", "authentication_required", "authentication_expired":
		return "BLOCKED", "i18n:PROVIDER_AUTHENTICATION_REQUIRED", "REAUTH_DEVICE_CODE"
	case "usage_limit_exceeded":
		return "BLOCKED", "i18n:PROVIDER_USAGE_LIMIT_EXCEEDED", "CHECK_PROVIDER_QUOTA"
	case "server_overloaded":
		return "FAILED", "i18n:PROVIDER_OVERLOADED", "RETRY_LATER"
	case "cyber_policy", "policy_denied":
		return "BLOCKED", "i18n:PROVIDER_POLICY_DENIED", "REVIEW_POLICY"
	case "invalid_configuration", "stale_grant":
		return "BLOCKED", "i18n:RUNTIME_CONFIGURATION_STALE", "CREATE_FRESH_TURN"
	case "provider_error_info_invalid", "provider_interrupted", "":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "RETRY_FRESH_TURN"
	case "provider_other_error":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "RETRY_FRESH_TURN"
	case "provider_internal_error", "provider_transport_failure":
		return "FAILED", "i18n:PROVIDER_OVERLOADED", "RETRY_LATER"
	case "provider_bad_request", "provider_sandbox_error":
		return "BLOCKED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "REVIEW_CONFIGURATION"
	case "context_window_exceeded", "session_budget_exceeded":
		return "BLOCKED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "CREATE_FRESH_SESSION"
	case "thread_rollback_failed", "active_turn_not_steerable":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "CREATE_FRESH_TURN"
	default:
		return "FAILED", "i18n:PROVIDER_RESULT_UNKNOWN", "RETRY_FRESH_TURN"
	}
}

// BlockedFailure is the caller's closed blocked-code predicate, separate from CapacityFailure and
// not a general equivalence to the outcome string returned by TerminalPresentation.
// BlockedFailure задаёт закрытый blocked-code предикат вызывающего кода отдельно от CapacityFailure;
// это не универсальный эквивалент outcome строки из TerminalPresentation.
func BlockedFailure(code string) bool {
	switch code {
	case "unauthorized", "cyber_policy", "usage_limit_exceeded", "provider_error_info_invalid",
		"authentication_required", "authentication_expired", "policy_denied", "invalid_configuration", "stale_grant":
		return true
	default:
		return false
	}
}

// serverNotificationMethods enumerates known notification names; recognition permits outer-schema
// validation, not execution of nested payload, arbitrary methods or new capabilities.
// serverNotificationMethods перечисляет известные имена уведомлений; узнавание допускает проверку
// внешней схемы, не исполнение вложенного payload, произвольные методы или новые capabilities.
var serverNotificationMethods = stringSet(
	"error", "thread/started", "thread/status/changed", "thread/archived", "thread/deleted", "thread/unarchived",
	"thread/closed", "skills/changed", "thread/name/updated", "thread/goal/updated", "thread/goal/cleared",
	"thread/settings/updated", "thread/tokenUsage/updated", "turn/started", "hook/started", "turn/completed",
	"hook/completed", "turn/diff/updated", "turn/plan/updated", "item/started", "item/autoApprovalReview/started",
	"item/autoApprovalReview/completed", "item/completed", "item/agentMessage/delta", "item/plan/delta",
	"command/exec/outputDelta", "process/outputDelta", "process/exited", "item/commandExecution/outputDelta",
	"item/commandExecution/terminalInteraction", "item/fileChange/outputDelta", "item/fileChange/patchUpdated",
	"serverRequest/resolved", "item/mcpToolCall/progress", "mcpServer/oauthLogin/completed",
	"rawResponseItem/completed", "rawResponse/completed",
	"mcpServer/startupStatus/updated", "account/updated", "account/rateLimits/updated", "app/list/updated",
	"remoteControl/status/changed", "externalAgentConfig/import/progress", "externalAgentConfig/import/completed",
	"fs/changed", "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded", "item/reasoning/textDelta",
	"thread/compacted", "model/rerouted", "model/verification", "turn/moderationMetadata",
	"model/safetyBuffering/updated", "warning", "guardianWarning", "deprecationNotice", "configWarning",
	"fuzzyFileSearch/sessionUpdated", "fuzzyFileSearch/sessionCompleted", "thread/realtime/started",
	"thread/realtime/itemAdded", "thread/realtime/transcript/delta", "thread/realtime/transcript/done",
	"thread/realtime/outputAudio/delta", "thread/realtime/sdp", "thread/realtime/error", "thread/realtime/closed",
	"windows/worldWritableWarning", "windowsSandbox/setupCompleted", "account/login/completed",
)

// stringSet builds a private membership map from trusted registry literals, silently coalescing duplicates;
// it is not a validator for caller-owned lists or authorization grants.
// stringSet строит приватную карту принадлежности из доверенных literals, объединяя повторы;
// она не проверяет внешние списки и не выдаёт authorization grants.
func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
