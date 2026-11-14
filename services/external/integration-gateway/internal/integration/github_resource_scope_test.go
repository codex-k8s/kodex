package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// TestGitHubScopedDispatchRechecksIdentityMembershipAndCanonicalRepository refuses foreign public resources, renamed or replaced IDs and revoked write permission before any mutation.
// TestGitHubScopedDispatchRechecksIdentityMembershipAndCanonicalRepository отклоняет чужие публичные ресурсы, переименованные либо заменённые ID и отозванное право записи до изменения.
func TestGitHubScopedDispatchRechecksIdentityMembershipAndCanonicalRepository(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		membership     bool
		repositoryID   int64
		repositoryName string
		push, archived bool
		want           bool
	}{{"permitted", true, 17, "team/repo", true, false, true}, {"unlisted_public", false, 17, "team/repo", true, false, false}, {"replaced_id", true, 18, "team/repo", true, false, false}, {"renamed", true, 17, "team/renamed", true, false, false}, {"collaboration_without_push", true, 17, "team/repo", false, false, true}, {"code_write_revoked", true, 17, "team/repo", false, false, false}, {"archived", true, 17, "team/repo", true, true, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			mutations := 0
			calls := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				if r.Method != http.MethodGet {
					mutations++
					http.Error(w, "forbidden", 403)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user":
					_, _ = w.Write([]byte(`{"id":7,"login":"synthetic"}`))
				case "/user/repos":
					if r.URL.Query().Get("affiliation") != "owner,collaborator,organization_member" {
						t.Error("discovery widened affiliations")
					}
					if scenario.membership {
						_, _ = w.Write([]byte(`[{"id":17,"name":"repo","full_name":"team/repo","owner":{"id":9,"login":"team","type":"Organization"},"permissions":{"pull":true,"push":true}}]`))
					} else {
						_, _ = w.Write([]byte(`[]`))
					}
				case "/repos/team/repo":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": scenario.repositoryID, "name": strings.Split(scenario.repositoryName, "/")[1], "full_name": scenario.repositoryName, "owner": map[string]any{"id": 9, "login": "team", "type": "Organization"}, "permissions": map[string]bool{"pull": true, "push": scenario.push}, "archived": scenario.archived})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := github.NewClient(server.Client())
			client.BaseURL, _ = url.Parse(server.URL + "/")
			capability := integrationpackage.Capability{Operation: "github.issue.create", Risk: "WRITE", Execution: integrationpackage.Execution{MaxAttempts: 1}}
			if scenario.name == "code_write_revoked" {
				capability.Operation = "github.branch.create"
			}
			configuration, raw, err := resolveGitHubScopedRepository(context.Background(), client, Request{PersonalCredentialAdmission: true, ResourcePermissionProfile: integrationpackage.CredentialResourcePermissionGitHubScopedV1}, capability, map[string]string{"scope_kind": "ALL_ACCESSIBLE"}, []byte(`{"repository_id":17,"repository_full_name":"team/repo","title":"synthetic"}`))
			if (err == nil) != scenario.want {
				t.Fatalf("admitted=%v want=%v: %v", err == nil, scenario.want, err)
			}
			if scenario.want && (configuration["owner"] != "team" || configuration["repository"] != "repo" || string(raw) != `{"title":"synthetic"}`) {
				t.Fatal("typed dispatch lost derived resource or native input")
			}
			if mutations != 0 || len(calls) < 2 || calls[0] != "/user" || calls[1] != "/user/repos" {
				t.Fatal("authorization did not use current authenticated provider reads")
			}
		})
	}
}

// TestGitHubScopedSelectionRejectsUnconfiguredRepositoryBeforeHTTP ensures a caller selector cannot widen an explicit selected scope, even with an otherwise valid provider ID.
// TestGitHubScopedSelectionRejectsUnconfiguredRepositoryBeforeHTTP проверяет, что caller selector не расширяет selected scope даже при существующем provider ID.
func TestGitHubScopedSelectionRejectsUnconfiguredRepositoryBeforeHTTP(t *testing.T) {
	_, _, err := resolveGitHubScopedRepository(context.Background(), nil, Request{}, integrationpackage.Capability{}, map[string]string{"scope_kind": "SELECTED", "repositories": `[{"id":17,"full_name":"team/repo"}]`}, []byte(`{"repository_id":18,"repository_full_name":"other/repo"}`))
	if err == nil {
		t.Fatal("foreign selected repository admitted")
	}
}

// TestGitHubServiceIdentityRequiresAuthenticatedLogin rejects a caller-chosen service label unless it matches fresh provider identity, before any repository metadata or mutation.
// TestGitHubServiceIdentityRequiresAuthenticatedLogin отклоняет выбранную caller service label без совпадения с текущей identity провайдера до чтения metadata репозитория либо изменения.
func TestGitHubServiceIdentityRequiresAuthenticatedLogin(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/user" {
			t.Error("mismatched service identity reached repository API")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7,"login":"actual-service"}`))
	}))
	defer server.Close()
	client := github.NewClient(server.Client())
	client.BaseURL, _ = url.Parse(server.URL + "/")
	capability := integrationpackage.Capability{Operation: "github.issue.create", Risk: "WRITE", Execution: integrationpackage.Execution{MaxAttempts: 1}}
	_, _, err := resolveGitHubScopedRepository(t.Context(), client, Request{}, capability, map[string]string{"scope_kind": "ALL_ACCESSIBLE", "service_identity": "different-service"}, []byte(`{"repository_id":17,"repository_full_name":"team/repo","title":"synthetic"}`))
	if err == nil || calls != 1 {
		t.Fatal("unproven service identity passed provider boundary", err)
	}
}

// TestGitHubScopedOperationPermissionMapping preserves useful read-member collaboration while keeping code/merge/action writes and unknown operations closed without push.
// TestGitHubScopedOperationPermissionMapping сохраняет полезный collaboration subset участнику с чтением и закрывает code/merge/action writes и неизвестные операции без push.
func TestGitHubScopedOperationPermissionMapping(t *testing.T) {
	for _, test := range []struct {
		operation, risk string
		allowed         bool
	}{{"github.issue.create", "WRITE", true}, {"github.issue.update", "WRITE", true}, {"github.issue.comment.delete", "DESTRUCTIVE", true}, {"github.pull_request.review.create", "WRITE", true}, {"github.pull_request.create", "WRITE", true}, {"github.pull_request.ready", "WRITE", true}, {"github.branch.create", "WRITE", false}, {"github.repository.content.update", "WRITE", false}, {"github.repository.bundle.publish", "WRITE", false}, {"github.pull_request.merge", "DESTRUCTIVE", false}, {"github.actions.run.rerun", "SENSITIVE", false}, {"github.synthetic.write", "WRITE", false}, {"github.issue.create", "READ", false}} {
		capability := integrationpackage.Capability{Operation: test.operation, Risk: test.risk}
		view := githubScopedRepository{CanRead: true}
		if gitHubScopedOperationAccess(capability, view) != test.allowed {
			t.Fatal("operation permission class changed", test.operation)
		}
		if test.risk != "READ" {
			view.Archived = true
			if gitHubScopedOperationAccess(capability, view) {
				t.Fatal("archived repository acquired write access")
			}
		}
	}
}
