package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestGitLabScopedProjectResolution proves the configured boundary before any typed project operation; no test contacts a provider or authorizes a model tool.
// TestGitLabScopedProjectResolution проверяет configured boundary перед typed project operation; тесты не обращаются к провайдеру и не выдают полномочия model tool.
func TestGitLabScopedProjectResolution(t *testing.T) {
	for _, fixture := range []struct {
		name, kind, root, nested, selected, group, project string
		wantCalls                                          int
		allowed                                            bool
	}{
		{name: "project", kind: "PROJECT", root: "team/service", selected: "team/service", wantCalls: 1, allowed: true},
		{name: "group", kind: "GROUP", root: "team", selected: "team/service", wantCalls: 2, allowed: true},
		{name: "subgroup-enabled", kind: "GROUP", root: "team", nested: "true", selected: "team/nested/service", wantCalls: 2, allowed: true},
		{name: "instance", kind: "INSTANCE", selected: "other/service", wantCalls: 1, allowed: true},
		{name: "foreign-project", kind: "PROJECT", root: "team/service", selected: "other/service"},
		{name: "similar-prefix", kind: "GROUP", root: "team", selected: "team-other/service"},
		{name: "unselected-subgroup", kind: "GROUP", root: "team", selected: "team/nested/service"},
		{name: "escaped-selector", kind: "INSTANCE", selected: "team%2Fservice"},
		{name: "absolute-selector", kind: "INSTANCE", selected: "https://example.invalid/team/service"},
		{name: "missing-selector", kind: "GROUP", root: "team"},
		{name: "renamed-group", kind: "GROUP", root: "team", selected: "team/service", group: `{"id":10,"full_path":"other"}`, wantCalls: 1},
		{name: "missing-group-id", kind: "GROUP", root: "team", selected: "team/service", group: `{"full_path":"team"}`, wantCalls: 1},
		{name: "shared-foreign-project", kind: "GROUP", root: "team", selected: "team/service", project: `{"id":20,"path_with_namespace":"other/service","namespace":{"id":10,"full_path":"other","kind":"group"}}`, wantCalls: 2},
		{name: "foreign-namespace", kind: "GROUP", root: "team", selected: "team/service", project: `{"id":20,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"other","kind":"group"}}`, wantCalls: 2},
		{name: "group-id-mismatch", kind: "GROUP", root: "team", selected: "team/service", project: `{"id":20,"path_with_namespace":"team/service","namespace":{"id":11,"full_path":"team","kind":"group"}}`, wantCalls: 2},
		{name: "user-cannot-be-group", kind: "GROUP", root: "team", selected: "team/service", project: `{"id":20,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"team","kind":"user"}}`, wantCalls: 2},
		{name: "project-id-replaced", kind: "INSTANCE", selected: "team/service", project: `{"id":21,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"team","kind":"group"}}`, wantCalls: 1},
		{name: "missing-namespace", kind: "INSTANCE", selected: "team/service", project: `{"id":20,"path_with_namespace":"team/service"}`, wantCalls: 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			adapter := legacyGitLabTestAdapter(t)
			definition := adapter.definitions["gitlab"]
			capability, _ := definition.Capability("gitlab.project.metadata.read")
			request := invocationRequest(t, definition, capability.Key, map[string]any{}, testCredential(t, adapter, "synthetic-scope-key"))
			configuration := map[string]string{"base_url": "https://git.example.invalid", "scope_kind": fixture.kind}
			if fixture.kind == "PROJECT" {
				configuration["project_path"] = fixture.root
			}
			if fixture.kind == "GROUP" {
				configuration["group_path"] = fixture.root
			}
			if fixture.nested != "" {
				configuration["include_subgroups"] = fixture.nested
			}
			before := make(map[string]string, len(configuration))
			for key, value := range configuration {
				before[key] = value
			}
			input, _ := json.Marshal(map[string]any{"project_path": fixture.selected, "project_id": 20, "merge_request_iid": 7, "body": "synthetic"})
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Scheme+"://"+r.URL.Host != configuration["base_url"] || r.Header.Get("Authorization") != "Bearer synthetic-scope-key" {
					t.Fatal("scope resolver changed origin, credential or HTTP method")
				}
				body := fixture.project
				if strings.HasPrefix(r.URL.Path, "/api/v4/groups/") {
					if calls != 1 || fixture.kind != "GROUP" {
						t.Fatal("scope resolver fetched an unconfigured group")
					}
					body = fixture.group
					if body == "" {
						body = `{"id":10,"full_path":"team"}`
					}
				} else {
					if r.URL.Path != "/api/v4/projects/"+fixture.selected {
						t.Fatal("scope resolver selected a different project")
					}
					if body == "" {
						namespace := fixture.selected[:strings.LastIndexByte(fixture.selected, '/')]
						raw, _ := json.Marshal(map[string]any{"id": 20, "path_with_namespace": fixture.selected, "namespace": map[string]any{"id": 10, "full_path": namespace, "kind": "group"}})
						body = string(raw)
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			selected, providerInput, err := adapter.resolveGitLabScopedProject(t.Context(), request, capability, configuration, input)
			if (err == nil) != fixture.allowed || calls != fixture.wantCalls {
				t.Fatalf("scope admission mismatch: allowed=%t calls=%d error=%v", err == nil, calls, err)
			}
			if !reflect.DeepEqual(configuration, before) || request.InputDigest == "" {
				t.Fatal("scope resolver mutated shared configuration or receipt input")
			}
			if fixture.allowed {
				var projected map[string]any
				if selected["project_path"] != fixture.selected || json.Unmarshal(providerInput, &projected) != nil || len(projected) != 2 || projected["body"] != "synthetic" || projected["merge_request_iid"] != float64(7) {
					t.Fatal("scope resolver did not preserve the exact typed handler input")
				}
			}
		})
	}
}
