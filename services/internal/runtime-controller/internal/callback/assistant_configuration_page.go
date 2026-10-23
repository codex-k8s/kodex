package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"unicode/utf8"
)

const (
	// maximumAssistantConfigurationPageBytes caps one raw configuration chunk at 16 KiB;
	// the encoded MCP envelope has a separate budget that may shrink a requested chunk.
	// maximumAssistantConfigurationPageBytes ограничивает raw configuration chunk 16 KiB;
	// у encoded MCP envelope отдельный budget, способный уменьшить chunk.
	maximumAssistantConfigurationPageBytes = 16 << 10
	// assistantConfigurationPageInvalidMessage is the fixed non-reflecting diagnostic
	// for malformed page coordinates, source shape, byte bounds or digest mismatch.
	// assistantConfigurationPageInvalidMessage — фиксированная диагностика без отражения
	// данных при неверных coordinates, source shape, byte bounds и digest mismatch.
	assistantConfigurationPageInvalidMessage = "assistant configuration page is invalid"
)

// errAssistantConfigurationPage provides one comparable failure for rejected page
// selection/projection without including source text, selectors or owner data.
// errAssistantConfigurationPage даёт сравнимую ошибку отказа page selection/projection
// без source text, selectors и owner data.
var errAssistantConfigurationPage = errors.New(assistantConfigurationPageInvalidMessage)

// assistantConfigurationPageRequest holds local byte offset/chunk limit and an
// optional expected source digest. It does not carry authority and is not sent
// to the owner, whose full eligible read remains independent of page selection.
// assistantConfigurationPageRequest хранит локальные byte offset/chunk limit и optional
// expected source digest. Authority не переносится; owner получает полный eligible
// read независимо от page selection, а не эти координаты.
type assistantConfigurationPageRequest struct {
	offset  int64
	maximum int64
	digest  string
}

// parseAssistantConfigurationPage accepts only known selector keys, permits page
// fields for four configuration read kinds and validates bounded integral coordinates.
// Nonzero continuation requires a canonical digest; defaults are offset zero/16 KiB.
// It does not resolve resources or change the owner's full read.
// parseAssistantConfigurationPage допускает известные selector keys, page fields
// лишь четырёх configuration read kinds и bounded integral coordinates. Продолжение
// с ненулевого offset требует canonical digest; defaults — zero/16 KiB.
// Ресурсы не разрешаются, полный owner read не меняется.
func parseAssistantConfigurationPage(selector map[string]any, kind string) (assistantConfigurationPageRequest, error) {
	page := assistantConfigurationPageRequest{maximum: maximumAssistantConfigurationPageBytes}
	if !onlyKeys(selector, "kind", "assistant_ref", "query", "offset", "account_ref", "runtime_profile_ref", "entity_kind", "entity_ref", "configuration_offset_bytes", "maximum_bytes", "configuration_sha256") {
		return page, errAssistantConfigurationPage
	}
	for _, key := range []string{"configuration_offset_bytes", "maximum_bytes", "configuration_sha256"} {
		if _, supplied := selector[key]; supplied && kind != "WORKFLOW_CONFIGURATION" && kind != "WORKFLOW_METADATA" && kind != "AGENT_CONFIGURATION" && kind != "AGENT_RUNTIME_CONFIGURATION" {
			return page, errAssistantConfigurationPage
		}
	}
	var ok bool
	if page.offset, ok = assistantConfigurationPageInteger(selector, "configuration_offset_bytes", 0, maximumAssistantCurrentConfigurationBytes); !ok {
		return page, errAssistantConfigurationPage
	}
	if page.maximum, ok = assistantConfigurationPageInteger(selector, "maximum_bytes", maximumAssistantConfigurationPageBytes, maximumAssistantConfigurationPageBytes); !ok || page.maximum < utf8.UTFMax {
		return page, errAssistantConfigurationPage
	}
	if raw, supplied := selector["configuration_sha256"]; supplied {
		page.digest, ok = raw.(string)
		if !ok || !validAssistantCatalogDigest(page.digest) {
			return page, errAssistantConfigurationPage
		}
	}
	if page.offset > 0 && page.digest == "" {
		return page, errAssistantConfigurationPage
	}
	return page, nil
}

// assistantConfigurationPageInteger accepts int, int64 or finite integral float64
// within zero through maximum; absent keys use fallback without revalidation.
// Other numeric representations, fractions and special float values are rejected.
// assistantConfigurationPageInteger принимает int,int64 или finite integral float64
// от нуля до maximum; отсутствующий key даёт fallback без перепроверки.
// Другие numeric representations, fractions и special float values отвергаются.
func assistantConfigurationPageInteger(selector map[string]any, key string, fallback, maximum int64) (int64, bool) {
	raw, supplied := selector[key]
	if !supplied {
		return fallback, true
	}
	var value int64
	switch number := raw.(type) {
	case int:
		value = int64(number)
	case int64:
		value = number
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > float64(maximum) || math.Trunc(number) != number {
			return 0, false
		}
		value = int64(number)
	default:
		return 0, false
	}
	return value, value >= 0 && value <= maximum
}

// pageAssistantConfigurationSnapshot runs after the full owner/caster read, verifies
// the selected source shape, marshaled-byte digest and continuation pin, then slices
// without splitting UTF-8 runes. Exact EOF is an empty final page; invalid offsets
// return no projection. New outer/inner maps omit full configuration but retain other
// nested references, leaving the source untouched rather than deeply copying it.
// pageAssistantConfigurationSnapshot вызывается после полного owner/caster read, проверяет
// source shape, digest сериализованных bytes и continuation pin, режет без разрыва
// UTF-8 runes. Точный EOF даёт пустую final page; неверный offset — без projection.
// Новые outer/inner maps не содержат полной configuration, остальные nested references
// общие; исходник не меняется, но deep copy не выполняется.
func pageAssistantConfigurationSnapshot(configuration map[string]any, page assistantConfigurationPageRequest) (map[string]any, error) {
	if page.offset < 0 || page.offset > maximumAssistantCurrentConfigurationBytes || page.maximum < utf8.UTFMax || page.maximum > maximumAssistantConfigurationPageBytes || page.offset > 0 && page.digest == "" || page.digest != "" && !validAssistantCatalogDigest(page.digest) {
		return nil, errAssistantConfigurationPage
	}
	key := ""
	switch configuration["kind"] {
	case "WORKFLOW_CONFIGURATION":
		key = "workflow_configuration"
	case "WORKFLOW_METADATA":
		key = "workflow_metadata"
	case "AGENT_CONFIGURATION":
		key = "agent_configuration"
	case "AGENT_RUNTIME_CONFIGURATION":
		key = "agent_runtime_configuration"
	default:
		return nil, errAssistantConfigurationPage
	}
	for _, other := range []string{"agent_configuration", "workflow_configuration", "workflow_metadata", "agent_runtime_configuration"} {
		if _, mixed := configuration[other]; mixed && other != key {
			return nil, errAssistantConfigurationPage
		}
	}
	inner, ok := configuration[key].(map[string]any)
	if !ok {
		return nil, errAssistantConfigurationPage
	}
	snapshot, ok := inner["configuration"].(map[string]any)
	digest, digestOK := inner["configuration_sha256"].(string)
	if !ok || !digestOK || !validAssistantCatalogDigest(digest) || !validAssistantConfigurationPageText(snapshot) {
		return nil, errAssistantConfigurationPage
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) == 0 || len(raw) > maximumAssistantCurrentConfigurationBytes || !utf8.Valid(raw) {
		return nil, errAssistantConfigurationPage
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest || page.digest != "" && page.digest != digest || page.offset > int64(len(raw)) {
		return nil, errAssistantConfigurationPage
	}
	end := min(page.offset+page.maximum, int64(len(raw)))
	if page.offset < int64(len(raw)) && !utf8.RuneStart(raw[page.offset]) {
		return nil, errAssistantConfigurationPage
	}
	for end < int64(len(raw)) && !utf8.RuneStart(raw[end]) {
		end--
	}
	part := raw[page.offset:end]
	if !utf8.Valid(part) || page.offset < int64(len(raw)) && len(part) == 0 {
		return nil, errAssistantConfigurationPage
	}
	chunk := sha256.Sum256(part)
	result := make(map[string]any, len(configuration))
	for field, value := range configuration {
		result[field] = value
	}
	projected := make(map[string]any, len(inner))
	for field, value := range inner {
		if field != "configuration" {
			projected[field] = value
		}
	}
	projected["configuration_page"] = map[string]any{"text": string(part), "size_bytes": len(raw), "offset_bytes": page.offset, "next_offset_bytes": end, "eof": end == int64(len(raw)), "page_sha256": hex.EncodeToString(chunk[:])}
	result[key] = projected
	return result, nil
}

// validAssistantConfigurationPageText recursively checks UTF-8 for strings, keys
// and values in map[string]any/[]any before JSON replacement could hide invalid bytes.
// Other dynamic types pass here and remain subject to subsequent marshal validation.
// validAssistantConfigurationPageText рекурсивно проверяет UTF-8 strings,keys/values
// map[string]any/[]any до JSON replacement невалидных bytes. Другие dynamic types
// здесь допускаются и остаются предметом последующей marshal validation.
func validAssistantConfigurationPageText(value any) bool {
	switch item := value.(type) {
	case string:
		return utf8.ValidString(item)
	case map[string]any:
		for key, nested := range item {
			if !utf8.ValidString(key) || !validAssistantConfigurationPageText(nested) {
				return false
			}
		}
	case []any:
		for _, nested := range item {
			if !validAssistantConfigurationPageText(nested) {
				return false
			}
		}
	}
	return true
}
