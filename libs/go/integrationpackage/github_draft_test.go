package integrationpackage

import (
	"encoding/json"
	"testing"
)

func TestGitHubPullRequestDraftInputIsOptionalAndClosed(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	create, ok := definitions["github"].Capability("github.pull_request.create")
	if !ok {
		t.Fatal("pull request create capability is missing")
	}
	github := definitions["github"]
	if github.Metadata.Version != "3.1.0" || !github.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP) || !github.RequiresConnectionCredential() {
		t.Fatal("draft field changed the executable package or credential boundary")
	}
	if _, ok := ResolveShippedRevision(github, "2.3.1", github.Digest); ok {
		t.Fatal("draft field admitted a stale shipped package version")
	}
	if _, ok := ResolveShippedRevision(github, github.Metadata.Version, "stale-digest"); ok {
		t.Fatal("draft field admitted a stale shipped package digest")
	}
	for _, raw := range []string{
		`{"title":"Title","head":"feature","base":"main"}`,
		`{"title":"Title","head":"feature","base":"main","draft":false}`,
		`{"title":"Title","head":"feature","base":"main","draft":true}`,
	} {
		if _, err := create.ValidateInput([]byte(raw)); err != nil {
			t.Fatalf("valid draft input rejected: %v", err)
		}
	}
	for _, raw := range []string{
		`{"title":"Title","head":"feature","base":"main","draft":"true"}`,
		`{"title":"Title","head":"feature","base":"main","draft":1}`,
		`{"title":"Title","head":"feature","base":"main","draft":null}`,
		`{"title":"Title","head":"feature","base":"main","draft":{}}`,
		`{"title":"Title","head":"feature","base":"main","draft":[]}`,
		`{"title":"Title","head":"feature","base":"main","draft":true,"draft":false}`,
		`{"title":"Title","head":"feature","base":"main","is_draft":true}`,
		`{"title":"Title","head":"feature","base":"main","draft":true,"actor":"owner"}`,
	} {
		if _, err := create.ValidateInput([]byte(raw)); err == nil {
			t.Fatal("invalid draft input accepted")
		}
	}
	schema, err := create.InputSchema()
	if err != nil {
		t.Fatal(err)
	}
	schemaDigest, err := create.InputSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("canonical GitHub package version=%s digest=%s create_schema_digest=%s", definitions["github"].Metadata.Version, definitions["github"].Digest, schemaDigest)
	var value struct {
		Properties           map[string]struct{ Type string } `json:"properties"`
		Required             []string                         `json:"required"`
		AdditionalProperties bool                             `json:"additionalProperties"`
	}
	if json.Unmarshal(schema, &value) != nil || value.Properties["draft"].Type != "boolean" || value.AdditionalProperties {
		t.Fatal("draft input schema is not closed and typed")
	}
	for _, key := range value.Required {
		if key == "draft" {
			t.Fatal("draft became required")
		}
	}
	update, _ := definitions["github"].Capability("github.pull_request.update")
	if _, err := update.ValidateInput([]byte(`{"pull_request_number":3,"draft":true}`)); err == nil {
		t.Fatal("create-only draft field expanded update authority")
	}
}
