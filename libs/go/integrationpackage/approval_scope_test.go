package integrationpackage

import "testing"

// approvalScopeFixture builds a closed nested schema for exact pointer and value-binding tests.
// approvalScopeFixture создаёт закрытую вложенную схему для проверки точных путей и привязки значений.
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

// TestApprovalScopeMatchesSelectedTypedParameters keeps digest stable across order and unrelated fields but not selected values.
// TestApprovalScopeMatchesSelectedTypedParameters сохраняет дайджест при смене порядка и посторонних полей, но не выбранных значений.
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

// TestApprovalScopeRejectsUnknownMissingAndMalformedPaths rejects undeclared, duplicate, malformed, and absent selected fields.
// TestApprovalScopeRejectsUnknownMissingAndMalformedPaths отклоняет необъявленные, повторные, неверные и отсутствующие выбранные поля.
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
