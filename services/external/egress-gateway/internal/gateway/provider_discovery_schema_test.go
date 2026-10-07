package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestClassifyAccountsCheckShape(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"empty", "", "JSON_INVALID"},
		{"whitespace", " \n ", "JSON_INVALID"},
		{"malformed", `{`, "JSON_INVALID"},
		{"html", `<html>private</html>`, "JSON_INVALID"},
		{"trailing", `{} {}`, "JSON_INVALID"},
		{"top_null", `null`, "TOP_TYPE_INVALID"},
		{"top_string", `"private"`, "TOP_TYPE_INVALID"},
		{"top_number", `12`, "TOP_TYPE_INVALID"},
		{"top_bool", `true`, "TOP_TYPE_INVALID"},
		{"defaults", `{}`, "SCHEMA_OK_LIST"},
		{"list_empty", `{"accounts":[]}`, "SCHEMA_OK_LIST"},
		{"map_empty", `{"accounts":{}}`, "SCHEMA_OK_MAP"},
		{"accounts_null", `{"accounts":null}`, "ACCOUNTS_TYPE_INVALID"},
		{"accounts_scalar", `{"accounts":"private"}`, "ACCOUNTS_TYPE_INVALID"},
		{"accounts_number", `{"accounts":1}`, "ACCOUNTS_TYPE_INVALID"},
		{"accounts_bool", `{"accounts":false}`, "ACCOUNTS_TYPE_INVALID"},
		{"list_entry_null", `{"accounts":[null]}`, "ENTRY_TYPE_INVALID"},
		{"list_entry_scalar", `{"accounts":["private"]}`, "ENTRY_TYPE_INVALID"},
		{"list_id_missing", `{"accounts":[{}]}`, "REQUIRED_FIELD_INVALID"},
		{"list_id_null", `{"accounts":[{"id":null}]}`, "REQUIRED_FIELD_INVALID"},
		{"list_id_number", `{"accounts":[{"id":42}]}`, "REQUIRED_FIELD_INVALID"},
		{"list_id_empty", `{"accounts":[{"id":""}]}`, "SCHEMA_OK_LIST"},
		{"list_minimal", `{"accounts":[{"id":"private"}]}`, "SCHEMA_OK_LIST"},
		{"list_null_options", `{"accounts":[{"id":"private","plan_type":null,"workspace_backend_origin":null,"account_routing_override":null,"name":null,"profile_picture_url":null}]}`, "SCHEMA_OK_LIST"},
		{"unknown_plan_string", `{"accounts":[{"id":"private","plan_type":"future_private_plan"}]}`, "SCHEMA_OK_LIST"},
		{"map_entry_null", `{"accounts":{"private":null}}`, "ENTRY_TYPE_INVALID"},
		{"map_entry_scalar", `{"accounts":{"private":9}}`, "ENTRY_TYPE_INVALID"},
		{"map_account_missing", `{"accounts":{"private":{}}}`, "REQUIRED_FIELD_INVALID"},
		{"map_account_null", `{"accounts":{"private":{"account":null}}}`, "REQUIRED_FIELD_INVALID"},
		{"map_account_scalar", `{"accounts":{"private":{"account":false}}}`, "REQUIRED_FIELD_INVALID"},
		{"map_minimal", `{"accounts":{"private":{"account":{}}}}`, "SCHEMA_OK_MAP"},
		{"map_missing_id", `{"accounts":{"private":{"account":{"plan_type":"promax"}}},"account_ordering":["private"]}`, "SCHEMA_OK_MAP"},
		{"map_null_id", `{"accounts":{"private":{"account":{"account_id":null}}}}`, "SCHEMA_OK_MAP"},
		{"map_wrong_id", `{"accounts":{"private":{"account":{"account_id":7}}}}`, "OPTIONAL_FIELD_TYPE_INVALID"},
		{"map_unselected_bad", `{"accounts":{"private":{"account":{"structure":null}}},"account_ordering":[]}`, "OPTIONAL_FIELD_TYPE_INVALID"},
		{"ordering_null", `{"account_ordering":null}`, "ORDERING_INVALID"},
		{"ordering_object", `{"account_ordering":{}}`, "ORDERING_INVALID"},
		{"ordering_bad_item", `{"account_ordering":["private",null]}`, "ORDERING_INVALID"},
		{"ordering_empty_strings", `{"account_ordering":["", "private", "private"]}`, "SCHEMA_OK_LIST"},
		{"default_null", `{"default_account_id":null}`, "SCHEMA_OK_LIST"},
		{"default_string", `{"default_account_id":"private"}`, "SCHEMA_OK_LIST"},
		{"default_bad", `{"default_account_id":[]}`, "OPTIONAL_FIELD_TYPE_INVALID"},
		{"list_unknown_fields", `{"accounts":[{"id":"private","account_id":{},"email":{"private":"value"}}],"unknown":false}`, "SCHEMA_OK_LIST"},
		{"map_unknown_routing_fields", `{"accounts":{"private":{"account":{"id":false,"workspace_backend_origin":{},"account_routing_override":[]}}}}`, "SCHEMA_OK_MAP"},
		{"top_known_duplicate", `{"accounts":[],"accounts":{}}`, "TOP_TYPE_INVALID"},
		{"entry_known_duplicate", `{"accounts":[{"id":"private","id":"private"}]}`, "ENTRY_TYPE_INVALID"},
		{"map_wrapper_duplicate", `{"accounts":{"private":{"account":{},"account":{}}}}`, "ENTRY_TYPE_INVALID"},
		{"unknown_duplicate", `{"unknown":1,"unknown":2}`, "SCHEMA_OK_LIST"},
		{"map_duplicate_key", `{"accounts":{"private":{"account":{}},"private":{"account":{}}}}`, "SCHEMA_OK_MAP"},
		{"map_duplicate_invalid_earlier", `{"accounts":{"private":null,"private":{"account":{}}}}`, "ENTRY_TYPE_INVALID"},
		{"sequence_top_defaults", `[]`, "SCHEMA_OK_LIST"},
		{"sequence_top_list", `[[], [], null]`, "SCHEMA_OK_LIST"},
		{"sequence_top_extra", `[[], [], null, false]`, "TOP_TYPE_INVALID"},
		{"sequence_list_entry", `{"accounts":[["private",null,null,null]]}`, "SCHEMA_OK_LIST"},
		{"sequence_list_short", `{"accounts":[["private"]]}`, "ENTRY_TYPE_INVALID"},
		{"sequence_map_entry", `{"accounts":{"private":[[null]]}}`, "SCHEMA_OK_MAP"},
		{"sequence_map_account_short", `{"accounts":{"private":{"account":[]}}}`, "REQUIRED_FIELD_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyAccountsCheckShape([]byte(test.body)); got != test.want {
				t.Fatalf("closed shape = %s, want %s", got, test.want)
			}
		})
	}
}

func TestClassifyAccountsCheckShapeOptionalFieldTypes(t *testing.T) {
	for _, field := range []string{"plan_type", "workspace_backend_origin", "account_routing_override", "name", "profile_picture_url", "structure"} {
		for _, value := range []string{`false`, `123`, `{}`, `[]`} {
			body := `{"accounts":[{"id":"private","` + field + `":` + value + `}]}`
			if got := classifyAccountsCheckShape([]byte(body)); got != "OPTIONAL_FIELD_TYPE_INVALID" {
				t.Fatalf("known optional field must reject non-string: %s", got)
			}
		}
	}
	for _, field := range []string{"account_id", "plan_type", "name", "profile_picture_url", "structure"} {
		body := `{"accounts":{"private":{"account":{"` + field + `":false}}}}`
		if got := classifyAccountsCheckShape([]byte(body)); got != "OPTIONAL_FIELD_TYPE_INVALID" {
			t.Fatalf("known map optional field must reject non-string: %s", got)
		}
	}
	for _, body := range []string{`{"accounts":[{"id":"private","structure":null}]}`, `{"accounts":{"private":{"account":{"structure":null}}}}`} {
		if got := classifyAccountsCheckShape([]byte(body)); got != "OPTIONAL_FIELD_TYPE_INVALID" {
			t.Fatalf("structure must reject explicit null: %s", got)
		}
	}
}

func TestClassifyAccountsCheckShapeBoundAndPrivateValues(t *testing.T) {
	const prefix = `{"unknown":"`
	const suffix = `"}`
	bounded := append([]byte(prefix), bytes.Repeat([]byte("p"), maximumAccountsCheckShapeBytes-len(prefix)-len(suffix))...)
	bounded = append(bounded, suffix...)
	if got := classifyAccountsCheckShape(bounded); got != "SCHEMA_OK_LIST" {
		t.Fatalf("exact budget shape = %s", got)
	}
	if got := classifyAccountsCheckShape(append(bounded, ' ')); got != "BOUND_EXCEEDED" {
		t.Fatalf("over budget shape = %s", got)
	}
	private := "private-account-key-email@example.invalid"
	for _, body := range []string{
		`{"accounts":{"` + private + `":{"account":{"account_id":"` + private + `","name":"` + private + `","email":"` + private + `"}}},"account_ordering":["` + private + `"]}`,
		`{"accounts":{"` + private + `":{"account":{"account_id":{"` + private + `":"` + private + `"}}}}}`,
		`{"` + private + `":`,
	} {
		got := classifyAccountsCheckShape([]byte(body))
		switch got {
		case "BOUND_EXCEEDED", "JSON_INVALID", "TOP_TYPE_INVALID", "ACCOUNTS_TYPE_INVALID", "ENTRY_TYPE_INVALID", "REQUIRED_FIELD_INVALID", "OPTIONAL_FIELD_TYPE_INVALID", "ORDERING_INVALID", "SCHEMA_OK_LIST", "SCHEMA_OK_MAP":
		default:
			t.Fatal("shape is outside the closed diagnostic set")
		}
		if strings.Contains(got, private) || strings.Contains(got, "@") || strings.Contains(got, "account_id") {
			t.Fatal("shape leaked private input")
		}
	}
}
