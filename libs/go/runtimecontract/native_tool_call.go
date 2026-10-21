package runtimecontract

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"unicode/utf8"
)

const (
	// NativeToolKindShell identifies a bounded shell-activity projection without command or output text.
	// NativeToolKindShell обозначает ограниченную проекцию shell-действия без текста команды и вывода.
	NativeToolKindShell = "CODEX_SHELL"
	// NativeToolKindFileChange identifies summarized file edits with bounded path hints.
	// NativeToolKindFileChange обозначает сводку изменений файлов с ограниченными подсказками путей.
	NativeToolKindFileChange = "CODEX_FILE_CHANGE"
	// NativeToolKindWebSearch identifies a summarized native search/open/find action.
	// NativeToolKindWebSearch обозначает сводное нативное действие поиска, открытия или поиска на странице.
	NativeToolKindWebSearch = "CODEX_WEB_SEARCH"
	// NativeToolKindDynamicTool identifies an external tool call through safe namespace/name metadata.
	// NativeToolKindDynamicTool обозначает внешний вызов через безопасные метаданные namespace и имени.
	NativeToolKindDynamicTool = "CODEX_DYNAMIC_TOOL"
	// NativeToolKindImageView identifies an image-view observation with a bounded path field.
	// NativeToolKindImageView обозначает наблюдение просмотра изображения с ограниченным полем пути.
	NativeToolKindImageView = "CODEX_IMAGE_VIEW"
	// NativeToolKindImageGeneration identifies an image-generation observation without the generation prompt.
	// NativeToolKindImageGeneration обозначает наблюдение генерации изображения без исходного prompt.
	NativeToolKindImageGeneration = "CODEX_IMAGE_GENERATION"
	// NativeToolKindSleep identifies an observed wait with a bounded requested duration.
	// NativeToolKindSleep обозначает наблюдаемое ожидание с ограниченной запрошенной длительностью.
	NativeToolKindSleep = "CODEX_SLEEP"

	// NativeToolStateSucceeded marks a terminal successful tool observation.
	// NativeToolStateSucceeded обозначает завершающее успешное наблюдение инструмента.
	NativeToolStateSucceeded = "SUCCEEDED"
	// NativeToolStateFailed marks a terminal failed tool observation.
	// NativeToolStateFailed обозначает завершающее неуспешное наблюдение инструмента.
	NativeToolStateFailed = "FAILED"
	// NativeToolStateRunning marks the initial revision before any result or duration is published.
	// NativeToolStateRunning обозначает начальную версию до публикации результата и длительности.
	NativeToolStateRunning = "RUNNING"
	// NativeToolStateCancelled marks a terminal cancellation with the closed cancellation result.
	// NativeToolStateCancelled обозначает завершающую отмену с закрытым результатом отмены.
	NativeToolStateCancelled = "CANCELLED"

	// NativeToolResultCompleted identifies a completed result without exposing provider output.
	// NativeToolResultCompleted обозначает завершённый результат без раскрытия вывода провайдера.
	NativeToolResultCompleted = "COMPLETED"
	// NativeToolResultFailed identifies a failed result without copying raw error details.
	// NativeToolResultFailed обозначает ошибку результата без копирования сырых подробностей.
	NativeToolResultFailed = "FAILED"
	// NativeToolResultDeclined identifies a declined operation through a closed result value.
	// NativeToolResultDeclined обозначает отклонённую операцию закрытым значением результата.
	NativeToolResultDeclined = "DECLINED"

	// MaximumNativeToolCalls caps the number of observed native items admitted by a runtime turn.
	// MaximumNativeToolCalls ограничивает количество наблюдаемых нативных элементов хода runtime.
	MaximumNativeToolCalls = 2_048
)

// nativeToolCallIDPattern accepts bounded provider item identities using a closed ASCII alphabet.
// nativeToolCallIDPattern допускает ограниченные идентификаторы элементов провайдера из закрытого ASCII-набора.
var nativeToolCallIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// NativeToolCall projects the lifecycle of a native provider item using closed metadata fields;
// raw commands, output, diffs, URLs, queries, prompts, and provider results have no fields here.
// NativeToolCall проецирует lifecycle нативного элемента провайдера закрытыми полями метаданных;
// для сырых команд, вывода, diff, URL, запросов, prompt и результатов провайдера полей здесь нет.
type NativeToolCall struct {
	CallID         string         `json:"call_id"`
	Revision       int64          `json:"revision"`
	Kind           string         `json:"kind"`
	State          string         `json:"state"`
	DurationMS     int64          `json:"duration_ms"`
	SafeParameters map[string]any `json:"safe_parameters"`
	SafeResult     string         `json:"safe_result"`
}

// RunnerNativeToolCallRequest pins one native activity observation to the immutable runtime revision digest.
// RunnerNativeToolCallRequest закрепляет наблюдение нативного действия за дайджестом неизменной версии runtime.
type RunnerNativeToolCallRequest struct {
	RuntimeRevisionDigest string `json:"runtime_revision_digest"`
	NativeToolCall
}

// Validate requires a canonical runtime revision digest and a valid closed activity projection.
// Validate требует канонический дайджест версии runtime и корректную закрытую проекцию действия.
func (request RunnerNativeToolCallRequest) Validate() error {
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) ||
		request.NativeToolCall.Validate() != nil {
		return errors.New("runner native tool call is invalid")
	}
	return nil
}

// Validate checks item identity, lifecycle revision, duration, kind-specific metadata, and a 3 KiB
// encoded parameter budget that leaves room for server-added correlation fields.
// Validate проверяет идентичность, версию lifecycle, длительность, видовые метаданные и бюджет
// параметров 3 KiB, оставляющий место для серверных полей корреляции.
func (call NativeToolCall) Validate() error {
	if !nativeToolCallIDPattern.MatchString(call.CallID) ||
		!nativeToolKind(call.Kind) ||
		!validNativeToolLifecycle(call) ||
		call.DurationMS < 0 || call.DurationMS > 86_400_000 ||
		!validNativeToolParameters(call.Kind, call.SafeParameters) {
		return errors.New("native tool call is invalid")
	}
	encoded, err := json.Marshal(call.SafeParameters)
	// Оставляем бюджет для серверно добавляемого correlation metadata внутри
	// существующего лимита RunToolCall.safe_parameters в 4 KiB.
	if err != nil || len(encoded) > 3<<10 || !utf8.Valid(encoded) {
		return errors.New("runner native tool call is invalid")
	}
	return nil
}

// validNativeToolLifecycle separates revision-1 running items from revision-2 terminal outcomes.
// validNativeToolLifecycle разделяет выполняемые элементы версии 1 и завершающие исходы версии 2.
func validNativeToolLifecycle(call NativeToolCall) bool {
	if call.State == NativeToolStateRunning {
		return call.Revision == 1 && call.SafeResult == "" && call.DurationMS == 0
	}
	if call.Revision != 2 {
		return false
	}
	if call.State == NativeToolStateCancelled {
		return call.SafeResult == "CANCELLED"
	}
	return (call.State == NativeToolStateSucceeded || call.State == NativeToolStateFailed) && nativeToolResult(call.SafeResult)
}

// nativeToolKind closes the allowed activity kinds instead of accepting arbitrary provider item types.
// nativeToolKind задаёт закрытый набор действий вместо произвольных видов элементов провайдера.
func nativeToolKind(value string) bool {
	switch value {
	case NativeToolKindShell, NativeToolKindFileChange, NativeToolKindWebSearch,
		NativeToolKindDynamicTool, NativeToolKindImageView, NativeToolKindImageGeneration, NativeToolKindSleep:
		return true
	default:
		return false
	}
}

// nativeToolResult accepts only the bounded terminal result enumeration used by success/failure states.
// nativeToolResult принимает только закрытые завершающие результаты состояний успеха и ошибки.
func nativeToolResult(value string) bool {
	return value == NativeToolResultCompleted || value == NativeToolResultFailed || value == NativeToolResultDeclined
}

// validNativeToolParameters enforces exact per-kind keys and bounded typed values without allowing
// extra provider fields; upstream producers remain responsible for constructing the safe projection.
// validNativeToolParameters требует точные ключи вида и ограниченные типизированные значения без
// лишних полей провайдера; построение безопасной проекции остаётся обязанностью производителя.
func validNativeToolParameters(kind string, values map[string]any) bool {
	if values == nil || len(values) > 8 {
		return false
	}
	switch kind {
	case NativeToolKindShell:
		return exactParameterKeys(values, "action_count", "action_kinds", "cwd_scope", "exit_code", "source") &&
			boundedInteger(values["action_count"], 0, 256) && stringSetValue(values["action_kinds"], 4, "READ", "LIST_FILES", "SEARCH", "UNKNOWN") &&
			closedString(values["cwd_scope"], "WORKSPACE", "OUTSIDE_WORKSPACE") &&
			closedString(values["exit_code"], "ZERO", "NONZERO", "UNAVAILABLE") &&
			closedString(values["source"], "AGENT", "USER_SHELL", "UNIFIED_EXEC_STARTUP", "UNIFIED_EXEC_INTERACTION", "UNKNOWN")
	case NativeToolKindFileChange:
		return exactParameterKeys(values, "change_count", "change_kinds", "paths", "paths_truncated") &&
			boundedInteger(values["change_count"], 0, 10_000) && stringSetValue(values["change_kinds"], 3, "ADD", "UPDATE", "DELETE") &&
			boundedStringList(values["paths"], 20, 240) && booleanValue(values["paths_truncated"])
	case NativeToolKindWebSearch:
		return exactParameterKeys(values, "action", "query_count") &&
			closedString(values["action"], "SEARCH", "OPEN_PAGE", "FIND_IN_PAGE", "OTHER", "UNSPECIFIED") &&
			boundedInteger(values["query_count"], 0, 100)
	case NativeToolKindDynamicTool:
		return exactParameterKeys(values, "argument_count", "argument_shape", "namespace", "tool") &&
			boundedInteger(values["argument_count"], 0, 10_000) &&
			closedString(values["argument_shape"], "OBJECT", "ARRAY", "SCALAR", "NULL") &&
			boundedSafeLabel(values["namespace"], 128) && boundedSafeLabel(values["tool"], 128)
	case NativeToolKindImageView:
		return exactParameterKeys(values, "path") && boundedPath(values["path"])
	case NativeToolKindImageGeneration:
		return exactParameterKeys(values, "output_available", "path") && booleanValue(values["output_available"]) && boundedPath(values["path"])
	case NativeToolKindSleep:
		return exactParameterKeys(values, "requested_duration_ms") && boundedInteger(values["requested_duration_ms"], 0, 86_400_000)
	default:
		return false
	}
}

// exactParameterKeys requires every registered key and forbids any additional metadata field.
// exactParameterKeys требует каждый зарегистрированный ключ и запрещает дополнительные метаданные.
func exactParameterKeys(values map[string]any, expected ...string) bool {
	if len(values) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

// boundedInteger accepts supported Go/JSON numeric forms only when finite, integral, and within bounds.
// boundedInteger принимает поддерживаемые числовые формы Go и JSON лишь как конечные целые в пределах границ.
func boundedInteger(value any, minimum, maximum int64) bool {
	var number float64
	switch typed := value.(type) {
	case int:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint64:
		if typed > math.MaxInt64 {
			return false
		}
		number = float64(typed)
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return false
		}
		number = float64(parsed)
	default:
		return false
	}
	return !math.IsNaN(number) && !math.IsInf(number, 0) && math.Trunc(number) == number && number >= float64(minimum) && number <= float64(maximum)
}

// closedString requires a string equal to one explicit allowed value without coercion or normalization.
// closedString требует строку, равную явному допустимому значению, без преобразования или нормализации.
func closedString(value any, allowed ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if text == candidate {
			return true
		}
	}
	return false
}

// stringSetValue accepts a bounded duplicate-free array whose items belong to the closed string set.
// stringSetValue принимает ограниченный массив без повторов, элементы которого входят в закрытый набор строк.
func stringSetValue(value any, maximum int, allowed ...string) bool {
	items, ok := stringSlice(value)
	if !ok || len(items) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !closedString(item, allowed...) {
			return false
		}
		if _, duplicate := seen[item]; duplicate {
			return false
		}
		seen[item] = struct{}{}
	}
	return true
}

// boundedStringList permits an empty array but bounds every present nonempty UTF-8 item by bytes.
// boundedStringList допускает пустой массив, ограничивая каждый присутствующий непустой UTF-8 элемент по байтам.
func boundedStringList(value any, maximum, maximumLength int) bool {
	items, ok := stringSlice(value)
	if !ok || len(items) > maximum {
		return false
	}
	for _, item := range items {
		if item == "" || len(item) > maximumLength || !utf8.ValidString(item) {
			return false
		}
	}
	return true
}

// stringSlice accepts native or JSON-decoded string arrays without coercion, rejecting JSON null and non-string items.
// stringSlice принимает нативные или разобранные из JSON строковые массивы без преобразований, отклоняя JSON null и нестроковые элементы.
func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

// booleanValue checks the actual boolean type without interpreting text or numeric substitutes.
// booleanValue проверяет настоящий булев тип без трактовки строковых или числовых замен.
func booleanValue(value any) bool {
	_, ok := value.(bool)
	return ok
}

// boundedSafeLabel permits only bounded nonempty tool-label text using the explicit ASCII alphabet.
// boundedSafeLabel допускает лишь ограниченные непустые метки инструмента из явного ASCII-набора.
func boundedSafeLabel(value any, maximum int) bool {
	text, ok := value.(string)
	if !ok || text == "" || len(text) > maximum || !utf8.ValidString(text) {
		return false
	}
	for _, symbol := range text {
		if !(symbol >= 'a' && symbol <= 'z' || symbol >= 'A' && symbol <= 'Z' || symbol >= '0' && symbol <= '9' ||
			symbol == '.' || symbol == '_' || symbol == ':' || symbol == '-' || symbol == '/') {
			return false
		}
	}
	return true
}

// boundedPath checks the path-hint string type, UTF-8 validity, and byte bound, allowing an empty hint.
// boundedPath проверяет строковый тип подсказки пути, UTF-8 и предел байтов, допуская пустую подсказку.
func boundedPath(value any) bool {
	text, ok := value.(string)
	return ok && len(text) <= 240 && utf8.ValidString(text)
}
