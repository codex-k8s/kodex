package integrationpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// jiraCreateFieldID admits custom fields only, excluding every built-in authority-bearing field.
// jiraCreateFieldID допускает только custom fields, исключая системные поля полномочий.
var jiraCreateFieldID = regexp.MustCompile(`^customfield_[1-9][0-9]{0,17}$`)

// errJiraCreateFields is deliberately constant; rejected configuration and tool input stay private.
// errJiraCreateFields намеренно постоянна; отклонённые настройки и ввод не раскрываются.
var errJiraCreateFields = errors.New("Jira create fields are invalid")

// JiraCreateField maps an operator-owned alias to one typed custom field, without defaults or URLs.
// JiraCreateField связывает alias оператора с одним типизированным custom field без defaults и URL.
type JiraCreateField struct {
	ID      string
	Type    string
	Options map[string]string
}

// ParseJiraCreateFields validates the optional bounded operator allowlist before connection storage
// and again at execution. Nested objects are closed, duplicate keys/IDs and system fields fail closed.
// ParseJiraCreateFields проверяет необязательный allowlist до сохранения подключения и исполнения.
// Вложенные объекты закрыты; повторные ключи/IDs и системные поля отклоняются.
func ParseJiraCreateFields(raw string) (map[string]JiraCreateField, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 8192 || !utf8.ValidString(raw) {
		return nil, errJiraCreateFields
	}
	entries, err := decodeJSONObject([]byte(raw))
	if err != nil || len(entries) == 0 || len(entries) > 16 {
		return nil, errJiraCreateFields
	}
	result, seen := map[string]JiraCreateField{}, map[string]bool{}
	for alias, encoded := range entries {
		fields, err := decodeJSONObject(encoded)
		var field JiraCreateField
		if err != nil || len(alias) > 48 || len(fields) < 2 || len(fields) > 3 || json.Unmarshal(fields["id"], &field.ID) != nil ||
			!jiraCreateFieldID.MatchString(field.ID) || seen[field.ID] || json.Unmarshal(fields["type"], &field.Type) != nil {
			return nil, errJiraCreateFields
		}
		for key := range fields {
			if key != "id" && key != "type" && key != "options" {
				return nil, errJiraCreateFields
			}
		}
		switch field.Type {
		case "STRING", "NUMBER", "BOOLEAN", "ISSUE_KEY":
			if _, exists := fields["options"]; exists {
				return nil, errJiraCreateFields
			}
		case "OPTION":
			options, err := decodeJSONObject(fields["options"])
			if err != nil || len(options) == 0 || len(options) > 64 {
				return nil, errJiraCreateFields
			}
			field.Options = map[string]string{}
			optionIDs := map[string]bool{}
			for name, value := range options {
				var id string
				if len(name) > 48 || json.Unmarshal(value, &id) != nil || len(id) > 18 || id == "" || id[0] == '0' {
					return nil, errJiraCreateFields
				}
				if _, err := strconv.ParseUint(id, 10, 64); err != nil || optionIDs[id] {
					return nil, errJiraCreateFields
				}
				field.Options[name], optionIDs[id] = id, true
			}
		default:
			return nil, errJiraCreateFields
		}
		result[alias], seen[field.ID] = field, true
	}
	return result, nil
}

// ParseJiraCreateValues preserves explicit scalar intent, including zero/false, for Human Gate
// and recovery. It never accepts nested JSON, null, coercions, duplicate aliases or oversized input.
// ParseJiraCreateValues сохраняет явные значения, включая zero/false, для Human Gate и recovery.
// Вложенный JSON, null, приведение типов, повторные aliases и чрезмерный ввод запрещены.
func ParseJiraCreateValues(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	if len(raw) > 2048 || !utf8.ValidString(raw) {
		return nil, errJiraCreateFields
	}
	fields, err := decodeJSONObject([]byte(raw))
	if err != nil || len(fields) > 16 {
		return nil, errJiraCreateFields
	}
	result := map[string]any{}
	for key, encoded := range fields {
		if len(key) > 48 {
			return nil, errJiraCreateFields
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil, errJiraCreateFields
		}
		switch typed := value.(type) {
		case string:
			if typed == "" || len(typed) > 512 {
				return nil, errJiraCreateFields
			}
		case bool:
		case json.Number:
			if len(typed.String()) > 128 {
				return nil, errJiraCreateFields
			}
			if index := strings.IndexAny(typed.String(), "eE"); index >= 0 {
				exponent, err := strconv.Atoi(typed.String()[index+1:])
				if err != nil || exponent < -64 || exponent > 64 {
					return nil, errJiraCreateFields
				}
			}
			number, err := typed.Float64()
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 1e9 {
				return nil, errJiraCreateFields
			}
		default:
			return nil, errJiraCreateFields
		}
		result[key] = value
	}
	return result, nil
}
