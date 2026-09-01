package runtimecontract

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"unicode/utf8"
)

const (
	NativeToolKindShell           = "CODEX_SHELL"
	NativeToolKindFileChange      = "CODEX_FILE_CHANGE"
	NativeToolKindWebSearch       = "CODEX_WEB_SEARCH"
	NativeToolKindDynamicTool     = "CODEX_DYNAMIC_TOOL"
	NativeToolKindImageView       = "CODEX_IMAGE_VIEW"
	NativeToolKindImageGeneration = "CODEX_IMAGE_GENERATION"
	NativeToolKindSleep           = "CODEX_SLEEP"

	NativeToolStateSucceeded = "SUCCEEDED"
	NativeToolStateFailed    = "FAILED"

	NativeToolResultCompleted = "COMPLETED"
	NativeToolResultFailed    = "FAILED"
	NativeToolResultDeclined  = "DECLINED"

	MaximumNativeToolCalls = 2_048
)

var nativeToolCallIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// NativeToolCall — безопасная проекция одного terminal native tool item Codex.
// Сырые команды, вывод, diff, URL, query, prompts и provider result в этот
// контракт не входят.
type NativeToolCall struct {
	CallID         string         `json:"call_id"`
	Kind           string         `json:"kind"`
	State          string         `json:"state"`
	DurationMS     int64          `json:"duration_ms"`
	SafeParameters map[string]any `json:"safe_parameters"`
	SafeResult     string         `json:"safe_result"`
}

type RunnerNativeToolCallRequest struct {
	RuntimeRevisionDigest string `json:"runtime_revision_digest"`
	NativeToolCall
}

func (request RunnerNativeToolCallRequest) Validate() error {
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) ||
		request.NativeToolCall.Validate() != nil {
		return errors.New("runner native tool call is invalid")
	}
	return nil
}

func (call NativeToolCall) Validate() error {
	if !nativeToolCallIDPattern.MatchString(call.CallID) ||
		!nativeToolKind(call.Kind) ||
		(call.State != NativeToolStateSucceeded && call.State != NativeToolStateFailed) ||
		call.DurationMS < 0 || call.DurationMS > 86_400_000 ||
		!nativeToolResult(call.SafeResult) || !validNativeToolParameters(call.Kind, call.SafeParameters) {
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

func nativeToolKind(value string) bool {
	switch value {
	case NativeToolKindShell, NativeToolKindFileChange, NativeToolKindWebSearch,
		NativeToolKindDynamicTool, NativeToolKindImageView, NativeToolKindImageGeneration, NativeToolKindSleep:
		return true
	default:
		return false
	}
}

func nativeToolResult(value string) bool {
	return value == NativeToolResultCompleted || value == NativeToolResultFailed || value == NativeToolResultDeclined
}

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

func booleanValue(value any) bool {
	_, ok := value.(bool)
	return ok
}

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

func boundedPath(value any) bool {
	text, ok := value.(string)
	return ok && len(text) <= 240 && utf8.ValidString(text)
}
