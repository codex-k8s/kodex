package gateway

import (
	"bytes"
	"encoding/json"
)

const maximumAccountsCheckShapeBytes = 1 << 20

// Проверяются только типы backend-client/src/types.rs из openai/codex
// rust-v0.160.0. Это диагностика decode, а не допуска, выбора workspace или
// пригодности routing. Значения, ключи аккаунтов и ошибки decoder не выдаются.
func classifyAccountsCheckShape(raw []byte) string {
	if len(raw) > maximumAccountsCheckShapeBytes {
		return "BOUND_EXCEEDED"
	}
	if !json.Valid(raw) {
		return "JSON_INVALID"
	}
	fields, ok := accountsCheckStruct(raw, []string{"accounts", "account_ordering", "default_account_id"}, 0)
	if !ok {
		return "TOP_TYPE_INVALID"
	}
	if ordering, exists := fields["account_ordering"]; exists {
		var entries []json.RawMessage
		if !accountsCheckArray(ordering, &entries) {
			return "ORDERING_INVALID"
		}
		for _, entry := range entries {
			if !accountsCheckString(entry) {
				return "ORDERING_INVALID"
			}
		}
	}
	if !accountsCheckOptionalString(fields, "default_account_id") {
		return "OPTIONAL_FIELD_TYPE_INVALID"
	}
	accounts, exists := fields["accounts"]
	if !exists {
		return "SCHEMA_OK_LIST"
	}
	accounts = bytes.TrimSpace(accounts)
	if len(accounts) == 0 {
		return "ACCOUNTS_TYPE_INVALID"
	}
	switch accounts[0] {
	case '[':
		var entries []json.RawMessage
		if !accountsCheckArray(accounts, &entries) {
			return "ACCOUNTS_TYPE_INVALID"
		}
		for _, entry := range entries {
			if outcome := accountsCheckListEntry(entry); outcome != "" {
				return outcome
			}
		}
		return "SCHEMA_OK_LIST"
	case '{':
		// HashMap десериализует каждый value до замены повторного key. Поэтому
		// проверяется и прежний value; account keys не сохраняются.
		decoder := json.NewDecoder(bytes.NewReader(accounts))
		_, _ = decoder.Token()
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return "JSON_INVALID"
			}
			var entry json.RawMessage
			if err := decoder.Decode(&entry); err != nil {
				return "JSON_INVALID"
			}
			if outcome := accountsCheckMapEntry(entry); outcome != "" {
				return outcome
			}
		}
		return "SCHEMA_OK_MAP"
	default:
		return "ACCOUNTS_TYPE_INVALID"
	}
}

func accountsCheckListEntry(raw json.RawMessage) string {
	fields, ok := accountsCheckStruct(raw, []string{"id", "plan_type", "workspace_backend_origin", "account_routing_override", "name", "profile_picture_url", "structure"}, 4)
	if !ok {
		return "ENTRY_TYPE_INVALID"
	}
	if !accountsCheckString(fields["id"]) {
		return "REQUIRED_FIELD_INVALID"
	}
	return accountsCheckOptionalFields(fields, "plan_type", "workspace_backend_origin", "account_routing_override", "name", "profile_picture_url")
}

func accountsCheckMapEntry(raw json.RawMessage) string {
	fields, ok := accountsCheckStruct(raw, []string{"account"}, 1)
	if !ok {
		return "ENTRY_TYPE_INVALID"
	}
	account, ok := accountsCheckStruct(fields["account"], []string{"account_id", "plan_type", "name", "profile_picture_url", "structure"}, 1)
	if !ok {
		return "REQUIRED_FIELD_INVALID"
	}
	return accountsCheckOptionalFields(account, "account_id", "plan_type", "name", "profile_picture_url")
}

func accountsCheckOptionalFields(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if !accountsCheckOptionalString(fields, name) {
			return "OPTIONAL_FIELD_TYPE_INVALID"
		}
	}
	// String с serde(default) допускает отсутствие, но не явный null.
	if structure, exists := fields["structure"]; exists && !accountsCheckString(structure) {
		return "OPTIONAL_FIELD_TYPE_INVALID"
	}
	return ""
}

func accountsCheckOptionalString(fields map[string]json.RawMessage, name string) bool {
	raw, exists := fields[name]
	return !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || accountsCheckString(raw)
}

func accountsCheckString(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"'
}

func accountsCheckArray(raw json.RawMessage, entries *[]json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, entries) == nil
}

// serde-derived struct допускает object и positional sequence. Неизвестные
// object-поля игнорируются, повтор известных полей и лишние sequence-поля — нет.
func accountsCheckStruct(raw json.RawMessage, names []string, minimumSequenceFields int) (map[string]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}
	fields := make(map[string]json.RawMessage, len(names))
	if raw[0] == '[' {
		var values []json.RawMessage
		if !accountsCheckArray(raw, &values) || len(values) < minimumSequenceFields || len(values) > len(names) {
			return nil, false
		}
		for index, value := range values {
			fields[names[index]] = value
		}
		return fields, true
	}
	if raw[0] != '{' {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return nil, false
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		name, ok := token.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		for _, known := range names {
			if name == known {
				if _, exists := fields[name]; exists {
					return nil, false
				}
				fields[name] = value
				break
			}
		}
	}
	return fields, true
}
