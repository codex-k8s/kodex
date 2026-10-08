package integrationpackage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubDirectoryListSchemaPinsAndOffsetBounds(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	if definition.Metadata.Version != "5.0.0" {
		t.Fatal("directory wire change lacks a new exact package version")
	}
	if _, ok := ResolveShippedRevision(definition, "4.0.0", definition.Digest); ok {
		t.Fatal("old unpaged package was silently reinterpreted")
	}
	capability, ok := definition.Capability("github.repository.content.list")
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" || capability.Execution.Idempotency != "READ_ONLY" ||
		capability.ResourceScope.Kind != "GITHUB_REPOSITORY" {
		t.Fatal("directory pagination changed authority or effect policy")
	}
	for _, input := range []string{
		`{"ref":"` + strings.Repeat("a", 40) + `"}`,
		`{"path":"src","ref":"` + strings.Repeat("a", 40) + `","cursor":0,"limit":1}`,
		`{"ref":"` + strings.Repeat("a", 40) + `","cursor":999,"limit":50,"expected_catalog_digest":"sha256:` + strings.Repeat("b", 64) + `"}`,
	} {
		if _, err := capability.ValidateInput([]byte(input)); err != nil {
			t.Fatal("bounded offset input rejected")
		}
	}
	for _, input := range []string{
		`{}`, `{"ref":"` + strings.Repeat("a", 40) + `","cursor":1000}`, `{"ref":"` + strings.Repeat("a", 40) + `","limit":51}`,
		`{"ref":"` + strings.Repeat("a", 40) + `","limit":0}`, `{"ref":"` + strings.Repeat("a", 41) + `"}`,
	} {
		if _, err := capability.ValidateInput([]byte(input)); err == nil {
			t.Fatal("schema allowed missing pins or out-of-bounds page")
		}
	}
	schema, err := capability.InputSchema()
	var decoded struct {
		Properties map[string]struct{ Minimum, Maximum int }
		Required   []string
	}
	if err != nil || json.Unmarshal(schema, &decoded) != nil || decoded.Properties["cursor"].Minimum != 0 || decoded.Properties["cursor"].Maximum != 999 ||
		decoded.Properties["limit"].Maximum != 50 || len(decoded.Required) != 1 || decoded.Required[0] != "ref" {
		t.Fatal("native generated schema lost immutable pin or offset semantics")
	}
	valid := map[string]any{"items": "[]", "path": "", "commit_sha": strings.Repeat("a", 40), "catalog_digest": "sha256:" + strings.Repeat("b", 64),
		"count": 0, "total_count": 999, "cursor": 999, "eof": true}
	encoded, _ := json.Marshal(valid)
	if _, err := capability.ValidateOutput(encoded); err != nil {
		t.Fatal("exact root catalog EOF rejected")
	}
	for _, field := range []string{"total_count", "cursor", "count", "next_cursor"} {
		before, present := valid[field]
		valid[field] = 1000
		encoded, _ = json.Marshal(valid)
		if _, err := capability.ValidateOutput(encoded); err == nil {
			t.Fatal("output exceeded catalog/page bound")
		}
		if present {
			valid[field] = before
		} else {
			delete(valid, field)
		}
	}
	if _, err := capability.ValidateOutput([]byte(`{"items":"[]","count":0}`)); err == nil {
		t.Fatal("legacy unpinned full-index response accepted")
	}
	t.Logf("canonical GitHub directory package version=%s digest=%s", definition.Metadata.Version, definition.Digest)
}
