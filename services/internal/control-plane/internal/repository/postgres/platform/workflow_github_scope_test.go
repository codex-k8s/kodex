package platform

import (
	"encoding/json"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"testing"
)

// TestWorkflowGitHubScopeReceiptProjection keeps personal scoped evidence parsers strict while rejecting repository substitution and missing immutable IDs.
// TestWorkflowGitHubScopeReceiptProjection сохраняет строгий разбор personal scoped evidence и отклоняет подмену репозитория либо отсутствие immutable ID.
func TestWorkflowGitHubScopeReceiptProjection(t *testing.T) {
	selected := []byte(`{"scope_kind":"SELECTED","repositories":"[{\"id\":7,\"full_name\":\"owner/repo\"}]"}`)
	for _, test := range []struct {
		name, input string
		valid       bool
	}{
		{"exact", `{"repository_id":7,"repository_full_name":"owner/repo","issue_number":1}`, true},
		{"different ID", `{"repository_id":8,"repository_full_name":"owner/repo","issue_number":1}`, false},
		{"renamed", `{"repository_id":7,"repository_full_name":"owner/renamed","issue_number":1}`, false},
		{"missing ID", `{"repository_full_name":"owner/repo","issue_number":1}`, false},
		{"foreign", `{"repository_id":7,"repository_full_name":"foreign/repo","issue_number":1}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope, input, valid := workflowGitHubScopeInput(selected, []byte(test.input))
			if valid != test.valid {
				t.Fatal("scope projection accepted substituted locator")
			}
			if valid {
				var values map[string]string
				var fields map[string]any
				if json.Unmarshal(scope, &values) != nil || json.Unmarshal(input, &fields) != nil || values["owner"] != "owner" || values["repository"] != "repo" || len(fields) != 1 {
					t.Fatal("scope projection changed native operation data")
				}
			}
		})
	}
}

// TestWorkflowGitHubFileIntentPinsRepository prevents equal file bytes in two repositories from sharing one semantic intent under ALL_ACCESSIBLE.
// TestWorkflowGitHubFileIntentPinsRepository исключает общий semantic intent одинаковых file bytes двух репозиториев в ALL_ACCESSIBLE.
func TestWorkflowGitHubFileIntentPinsRepository(t *testing.T) {
	input := map[string]any{"repository_id": 7, "repository_full_name": "owner/repo", "path": "a.go", "branch": "feature/a", "message": "test", "content_base64": "eA=="}
	a, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, "file", "github.repository.content.create", "scope", asJSON(input))
	if err != nil {
		t.Fatal(err)
	}
	input["repository_id"] = 8
	b, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, "file", "github.repository.content.create", "scope", asJSON(input))
	if err != nil || a == b {
		t.Fatal("repository ID omitted from semantic intent", err)
	}
	input["repository_id"] = 7
	input["repository_full_name"] = "owner/other"
	c, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, "file", "github.repository.content.create", "scope", asJSON(input))
	if err != nil || a == c {
		t.Fatal("repository name omitted from semantic intent", err)
	}
}
