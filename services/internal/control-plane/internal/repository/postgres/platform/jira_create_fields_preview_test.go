package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestJiraCreateFieldsGatePreviewRetainsExactIntent proves the actual owner preview consumer
// displays every added scalar and the full alias JSON under the same canonical input digest.
// TestJiraCreateFieldsGatePreviewRetainsExactIntent проверяет настоящую проекцию подтверждения:
// все добавленные значения и полный alias JSON показаны с прежним canonical input digest.
func TestJiraCreateFieldsGatePreviewRetainsExactIntent(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"summary": "Synthetic create", "fields_json": `{"platform":"test","hours":0,"epic":"DEMO-42"}`, "reporter_self": true}
	raw, _ := json.Marshal(input)
	digest := sha256.Sum256(raw)
	preview, err := integrationGatePreview(definitions, "jira-server-dc", "jira.issue.create", "jira.issue.create", raw, hex.EncodeToString(digest[:]))
	if err != nil || preview["contentComplete"] != true {
		t.Fatal("explicit intent hidden", err)
	}
	seen := map[string]bool{}
	for _, item := range preview["fields"].([]any) {
		field := item.(map[string]any)
		key := field["key"].(string)
		if field["opaque"] != false || field["value"] != input[key] {
			t.Fatal("gate changed approved field", key)
		}
		seen[key] = true
	}
	if len(seen) != len(input) {
		t.Fatal("gate omitted explicit intent")
	}
	input["fields_json"] = `{"platform":"other","hours":0,"epic":"DEMO-42"}`
	changed, _ := json.Marshal(input)
	if _, err := integrationGatePreview(definitions, "jira-server-dc", "jira.issue.create", "jira.issue.create", changed, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("changed input retained prior approval digest")
	}
}
