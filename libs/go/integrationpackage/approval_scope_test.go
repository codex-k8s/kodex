package integrationpackage

import "testing"

func approvalScopeFixture() Capability {
	return Capability{OpenAPI: &OpenAPIHTTP{InputSchema: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
			"ticket": map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"id": map[string]any{"type": "integer"}}},
			"note": map[string]any{"type": "string"},
			"a/b":  map[string]any{"type": "boolean"},
		},
		"required": []any{"action", "ticket"},
	}}}
}

func TestApprovalScopeMatchesSelectedTypedParameters(t *testing.T) {
	capability := approvalScopeFixture()
	paths := []string{"/ticket/id", "/action"}
	first, err := capability.ResolveApprovalScope(paths, []byte(`{"action":"UPDATE","ticket":{"id":3},"note":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	same, err := capability.ResolveApprovalScope([]string{"/action", "/ticket/id"}, []byte(`{"note":"second","ticket":{"id":3},"action":"UPDATE"}`))
	if err != nil || first.Digest != same.Digest || len(first.Values) != 2 {
		t.Fatalf("scope match failed: %v", err)
	}
	changed, err := capability.ResolveApprovalScope(paths, []byte(`{"action":"UPDATE","ticket":{"id":4}}`))
	if err != nil || changed.Digest == first.Digest {
		t.Fatalf("changed selected value reused scope: %v", err)
	}
}

func TestApprovalScopeRejectsUnknownMissingAndMalformedPaths(t *testing.T) {
	capability := approvalScopeFixture()
	for _, paths := range [][]string{{}, {"/unknown"}, {"/ticket/id", "/ticket/id"}, {"ticket/id"}, {"/ticket/~2id"}, {"/ticket/id/part"}} {
		if err := capability.ValidateApprovalScopePaths(paths); err == nil {
			t.Fatalf("invalid scope paths accepted: %v", paths)
		}
	}
	if err := capability.ValidateApprovalScopePaths([]string{"/a~1b"}); err != nil {
		t.Fatalf("escaped property rejected: %v", err)
	}
	if _, err := capability.ResolveApprovalScope([]string{"/ticket/id"}, []byte(`{"action":"UPDATE","ticket":{}}`)); err == nil {
		t.Fatal("missing selected parameter accepted")
	}
}
