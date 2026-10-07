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
	maximumAssistantConfigurationPageBytes   = 4096
	assistantConfigurationPageInvalidMessage = "assistant configuration page is invalid"
)

var errAssistantConfigurationPage = errors.New(assistantConfigurationPageInvalidMessage)

type assistantConfigurationPageRequest struct {
	offset  int64
	maximum int64
	digest  string
}

// Координаты страницы не передаются владельцу и не изменяют его полный read.
func parseAssistantConfigurationPage(selector map[string]any, kind string) (assistantConfigurationPageRequest, error) {
	page := assistantConfigurationPageRequest{maximum: maximumAssistantConfigurationPageBytes}
	if !onlyKeys(selector, "kind", "assistant_ref", "query", "offset", "account_ref", "runtime_profile_ref", "entity_kind", "entity_ref", "configuration_offset_bytes", "maximum_bytes", "configuration_sha256") {
		return page, errAssistantConfigurationPage
	}
	for _, key := range []string{"configuration_offset_bytes", "maximum_bytes", "configuration_sha256"} {
		if _, supplied := selector[key]; supplied && kind != "WORKFLOW_CONFIGURATION" && kind != "AGENT_CONFIGURATION" {
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

// Вызывается только после полного owner/caster read; исходная модель не меняется.
func pageAssistantConfigurationSnapshot(configuration map[string]any, page assistantConfigurationPageRequest) (map[string]any, error) {
	if page.offset < 0 || page.offset > maximumAssistantCurrentConfigurationBytes || page.maximum < utf8.UTFMax || page.maximum > maximumAssistantConfigurationPageBytes || page.offset > 0 && page.digest == "" || page.digest != "" && !validAssistantCatalogDigest(page.digest) {
		return nil, errAssistantConfigurationPage
	}
	key := ""
	switch configuration["kind"] {
	case "WORKFLOW_CONFIGURATION":
		key = "workflow_configuration"
	case "AGENT_CONFIGURATION":
		key = "agent_configuration"
	default:
		return nil, errAssistantConfigurationPage
	}
	other := "agent_configuration"
	if key == other {
		other = "workflow_configuration"
	}
	if _, mixed := configuration[other]; mixed {
		return nil, errAssistantConfigurationPage
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

// json.Marshal заменяет недопустимый UTF-8; до сериализации это закрыто запрещено.
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
