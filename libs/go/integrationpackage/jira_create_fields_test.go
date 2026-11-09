package integrationpackage

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestJiraCreateFieldsRejectsAuthorityAndAmbiguity verifies the operator allowlist, including
// nested duplicate rejection, finite primitive inputs and absence of system-field/default injection.
// TestJiraCreateFieldsRejectsAuthorityAndAmbiguity проверяет allowlist, вложенные повторы,
// конечные primitive inputs и запрет подстановки системных полей или defaults.
func TestJiraCreateFieldsRejectsAuthorityAndAmbiguity(t *testing.T) {
	valid := `{"platform":{"id":"customfield_11001","type":"OPTION","options":{"test":"21001"}},"hours":{"id":"customfield_11002","type":"NUMBER"}}`
	if fields, err := ParseJiraCreateFields(valid); err != nil || len(fields) != 2 {
		t.Fatal("valid allowlist rejected", err)
	}
	for _, raw := range []string{
		`{}`, `null`, `{"x":{"id":"project","type":"STRING"}}`,
		`{"x":{"id":"customfield_1","type":"STRING","default":"hidden"}}`,
		`{"x":{"id":"customfield_1","id":"customfield_2","type":"STRING"}}`,
		`{"x":{"id":"customfield_1","type":"STRING"},"x":{"id":"customfield_2","type":"NUMBER"}}`,
		`{"x":{"id":"customfield_1","type":"STRING"},"y":{"id":"customfield_1","type":"STRING"}}`,
		`{"x":{"id":"customfield_1","type":"OPTION","options":{"a":"1","a":"2"}}}`,
		`{"x":{"id":"customfield_1","type":"OPTION","options":{"a":"1","b":"1"}}}`,
		`{"x":{"id":"customfield_1","type":"STRING","options":{}}}`,
		`{"x":{"id":"customfield_1","type":"ARRAY"}}`, strings.Repeat(" ", 8193),
	} {
		if _, err := ParseJiraCreateFields(raw); err == nil {
			t.Fatal("unsafe allowlist accepted", raw)
		}
	}
	values, err := ParseJiraCreateValues(`{"hours":0,"flag":false,"platform":"test"}`)
	if err != nil || values["hours"] != json.Number("0") || values["flag"] != false {
		t.Fatal("explicit zero/false lost", err)
	}
	for _, raw := range []string{`{"x":null}`, `{"x":[]}`, `{"x":{}}`, `{"x":0,"x":1}`, `{"x":1e999}`, `{"x":1e-999999999}`, `{"x":1000000001}`, `{"x":""}`, strings.Repeat(" ", 2049)} {
		if _, err := ParseJiraCreateValues(raw); err == nil {
			t.Fatal("unsafe input accepted", raw)
		}
	}
}

// TestJiraCreateFieldsRemainOptInAndHistoricallyPinned checks connection validation, closed tool
// schemas, immutable old pins, Cloud isolation and exact explicit input preservation before gating.
// TestJiraCreateFieldsRemainOptInAndHistoricallyPinned проверяет конфигурацию, закрытые schemas,
// старые pins, изоляцию Cloud и сохранение явного ввода до подтверждения.
func TestJiraCreateFieldsRemainOptInAndHistoricallyPinned(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["jira-server-dc"]
	previous, err := Parse(jiraServerOneCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	old, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest)
	if !ok || old.Digest != previous.Digest || ValidateExecutableRevision(previous, current) != nil {
		t.Fatal("old pinned definition rejected")
	}
	migration, err := os.ReadFile("../../../services/internal/control-plane/cmd/cli/migrations/20261107000100_jira_create_fields_compatibility.sql")
	if err != nil || !strings.Contains(string(migration), legacyPolicyPackage(t, "jira-server-dc", "1.1.0").Digest) || !strings.Contains(string(migration), old.Digest) {
		t.Fatal("SQL compatibility pins differ from executable packages")
	}
	if _, ok := ResolveShippedRevision(current, "1.0.0", current.Digest); ok {
		t.Fatal("new authority acquired through old version")
	}
	config := map[string]string{"base_url": "https://jira.example.invalid", "context_path": "/tracker", "auth_scheme": "BEARER", "project_key": "DEMO", "issue_type": "Task"}
	if current.ValidateConfiguration(config) != nil || previous.ValidateConfiguration(config) != nil {
		t.Fatal("legacy connection changed")
	}
	config["issue_create_fields"] = `{"hours":{"id":"customfield_11002","type":"NUMBER"}}`
	if current.ValidateConfiguration(config) != nil || previous.ValidateConfiguration(config) == nil {
		t.Fatal("configuration did not remain opt-in")
	}
	config["issue_create_fields"] = `{"hours":{"id":"project","type":"NUMBER"}}`
	if current.ValidateConfiguration(config) == nil {
		t.Fatal("system field admitted by connection owner")
	}
	capability, _ := current.Capability("jira.issue.create")
	raw := []byte(`{"summary":"Synthetic","fields_json":"{\"hours\":0}","reporter_self":true}`)
	canonical, err := capability.ValidateInput(raw)
	if err != nil || string(canonical) == "" {
		t.Fatal("new explicit input rejected", err)
	}
	oldCapability, _ := old.Capability("jira.issue.create")
	cloudCapability, _ := definitions["jira"].Capability("jira.issue.create")
	if _, err := oldCapability.ValidateInput(raw); err == nil {
		t.Fatal("old definition gained new inputs")
	}
	if _, err := cloudCapability.ValidateInput(raw); err == nil {
		t.Fatal("Server inputs reached Cloud")
	}
	if _, err := capability.ValidateInput([]byte(`{"summary":"Synthetic","fields_json":"{\"hours\":{}}"}`)); err == nil {
		t.Fatal("invalid nested intent reached gate")
	}
}
