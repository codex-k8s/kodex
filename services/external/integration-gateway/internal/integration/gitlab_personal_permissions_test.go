package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// gitLabPermissionResponse supplies versioned synthetic metadata for the authenticated token
// and one project. It records no live identity or credential and creates no external object.
// gitLabPermissionResponse задаёт versioned synthetic metadata authenticated token и одного
// проекта. Живые identity/credential не сохраняются, внешние объекты не создаются.
func gitLabPermissionResponse(t *testing.T, request Request, r *http.Request, scenario string) *http.Response {
	t.Helper()
	status := http.StatusOK
	var value any
	path := r.URL.EscapedPath()
	projectPath := "/api/v4/projects/group%2Fproject"
	switch {
	case path == "/api/v4/version":
		version := "19.2.9-ee"
		if scenario == "old version" {
			version = "19.1.9-ee"
		}
		value = map[string]string{"version": version}
		if scenario == "401" {
			status = 401
		} else if scenario == "403" {
			status = 403
		} else if scenario == "404" {
			status = 404
		} else if scenario == "429" {
			status = 429
		}
	case path == "/api/v4/user":
		value = map[string]any{"id": 7, "state": "active"}
	case path == "/api/v4/personal_access_tokens/self" || path == "/api/v4/personal_access_tokens/11":
		token := map[string]any{"id": 11, "user_id": 7, "active": true, "revoked": false, "granular": false, "scopes": []string{"api"}, "expires_at": "2099-01-01"}
		switch scenario {
		case "read only":
			token["scopes"] = []string{"read_api"}
		case "missing granular":
			delete(token, "granular")
		case "inactive":
			token["active"] = false
		case "revoked":
			token["revoked"] = true
		case "expired":
			token["expires_at"] = "2000-01-01"
		case "missing expiry":
			delete(token, "expires_at")
		case "foreign user":
			token["user_id"] = 8
		case "fine project", "fine all memberships", "fine wrong project", "fine missing action", "fine group", "fine missing detail", "fine changed token":
			token["granular"] = true
			if path != "/api/v4/personal_access_tokens/self" {
				action := map[string]string{"gitlab.issue.create": "create_issue", "gitlab.branch.create": "create_branch", "gitlab.merge_request.create": "create_merge_request"}[request.Operation]
				scope := map[string]any{"access": "selected_memberships", "project_id": 42, "group_id": nil, "permissions": []string{action}}
				switch scenario {
				case "fine all memberships":
					scope["access"], scope["project_id"] = "all_memberships", nil
				case "fine wrong project":
					scope["project_id"] = 43
				case "fine missing action":
					scope["permissions"] = []string{"read_project"}
				case "fine group":
					scope["project_id"], scope["group_id"] = nil, 42
				case "fine changed token":
					token["id"] = 12
				}
				if scenario != "fine missing detail" {
					token["granular_scopes"] = []any{scope}
				}
			}
		}
		value = token
	case path == projectPath:
		project := map[string]any{"id": 42, "path_with_namespace": "group/project", "archived": false,
			"permissions":         map[string]any{"project_access": nil, "group_access": map[string]int{"access_level": 30}},
			"issues_access_level": "enabled", "repository_access_level": "private", "merge_requests_access_level": "enabled", "marked_for_deletion_at": nil}
		switch scenario {
		case "wrong project":
			project["path_with_namespace"] = "other/project"
		case "archived":
			project["archived"] = true
		case "deleting":
			project["marked_for_deletion_at"] = "2099-01-01"
		case "reporter":
			project["permissions"] = map[string]any{"project_access": map[string]int{"access_level": 20}, "group_access": nil}
		case "missing role":
			delete(project, "permissions")
		case "disabled feature":
			project["issues_access_level"], project["repository_access_level"], project["merge_requests_access_level"] = "disabled", "disabled", "disabled"
		}
		value = project
	case path == projectPath+"/issues" && r.Method == http.MethodGet:
		value = []any{}
	case path == projectPath+"/protected_branches":
		if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") == "" {
			t.Fatal("protected branch pagination changed its fixed bound")
		}
		rules := []any{map[string]any{"name": "main", "inherited": true}}
		switch scenario {
		case "protected exact":
			rules = []any{map[string]any{"name": "feature", "inherited": true}}
		case "protected wildcard":
			rules = []any{map[string]string{"name": "feat*"}}
		case "unknown pattern":
			rules = []any{map[string]string{"name": "feat?re"}}
		case "rule pages", "rule exhaustion", "protected second page":
			if r.URL.Query().Get("page") == "1" || scenario == "rule exhaustion" {
				rules = make([]any, 100)
				for i := range rules {
					rules[i] = map[string]string{"name": fmt.Sprintf("guarded-%03d", i)}
				}
			} else if scenario == "protected second page" {
				rules = []any{map[string]string{"name": "feat*"}}
			} else {
				rules = []any{}
			}
		}
		value = rules
	case strings.HasPrefix(path, projectPath+"/repository/branches/"):
		name, err := url.PathUnescape(strings.TrimPrefix(path, projectPath+"/repository/branches/"))
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "wrong branch" {
			name = "other"
		}
		value = map[string]any{"name": name, "commit": map[string]string{"id": strings.Repeat("a", 40)}}
	default:
		t.Fatalf("unexpected personal permission metadata route: %s", path)
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "duplicate" && path == "/api/v4/version" {
		body = []byte(`{"version":"19.2.9-ee","Version":"19.1.0"}`)
	}
	if scenario == "null rules" && path == projectPath+"/protected_branches" {
		body = []byte(" \nnull\n ")
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}
}

// TestGitLabPersonalWritePermissions verifies all three real typed dispatches, legacy and
// exact granular scopes, current project facts, protected refs, finite pagination and unknown
// outcomes after the single POST boundary. No metadata refusal may create an external effect.
// TestGitLabPersonalWritePermissions проверяет три настоящих typed dispatch, legacy и exact
// granular scopes, текущий проект, protected refs, конечную pagination и UNKNOWN после одного
// POST. Отказ metadata не может создавать внешний effect.
func TestGitLabPersonalWritePermissions(t *testing.T) {
	for _, operation := range []string{"gitlab.issue.create", "gitlab.branch.create", "gitlab.merge_request.create"} {
		for _, scenario := range []string{"allow", "read only", "missing granular", "inactive", "revoked", "expired", "missing expiry", "foreign user", "fine project", "fine all memberships", "fine wrong project", "fine missing action", "fine group", "fine missing detail", "fine changed token", "wrong project", "archived", "deleting", "reporter", "missing role", "disabled feature", "old version", "duplicate", "401", "403", "404", "429", "metadata timeout", "post timeout", "post malformed", "missing profile", "protected exact", "protected wildcard", "unknown pattern", "rule pages", "rule exhaustion", "protected second page", "null rules", "wrong branch"} {
			branchOnly := strings.HasPrefix(scenario, "protected ") || scenario == "unknown pattern" || strings.HasPrefix(scenario, "rule ") || scenario == "null rules"
			if branchOnly && operation != "gitlab.branch.create" || scenario == "wrong branch" && operation != "gitlab.merge_request.create" {
				continue
			}
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				adapter := legacyGitLabTestAdapter(t)
				credential := testCredential(t, adapter, "synthetic-gitlab-personal-key")
				var input map[string]any
				if json.Unmarshal([]byte(catalogInputs()[operation]), &input) != nil {
					t.Fatal("missing GitLab input fixture")
				}
				request := invocationRequest(t, adapter.definitions["gitlab"], operation, input, credential)
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor("gitlab", operation)
				if !exists {
					t.Fatal("missing GitLab permission profile")
				}
				request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, contract.Profile
				if scenario == "missing profile" {
					request.ResourcePermissionProfile = ""
				}
				wantSuccess := scenario == "allow" || scenario == "fine project" || scenario == "fine all memberships" || scenario == "rule pages"
				var validate func(*http.Request, *http.Response)
				if wantSuccess {
					validate = gitLabWireValidator(t, request)
				}
				gets, posts := 0, 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Scheme != "https" || r.URL.Host != strings.TrimPrefix(request.Configuration["base_url"].(string), "https://") || r.Header.Get("Authorization") != "Bearer synthetic-gitlab-personal-key" {
						t.Fatal("personal GitLab changed its configured origin or exact key")
					}
					if r.Method == http.MethodPost {
						posts++
						if posts > 1 {
							t.Fatal("personal mutation was replayed")
						}
						if scenario == "post timeout" {
							return nil, context.DeadlineExceeded
						}
						body := catalogResponse(t, "gitlab", operation, r)
						if scenario == "post malformed" {
							body = "{"
						}
						response := &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
						if validate != nil {
							validate(r, response)
						}
						return response, nil
					}
					if r.Method != http.MethodGet {
						t.Fatal("permission check changed metadata instead of reading")
					}
					gets++
					if scenario == "metadata timeout" && r.URL.Path == "/api/v4/version" {
						return nil, context.DeadlineExceeded
					}
					response := gitLabPermissionResponse(t, request, r, scenario)
					if validate != nil {
						validate(r, response)
					}
					return response, nil
				})}
				result, err := adapter.Execute(t.Context(), request)
				wantUnknown := scenario == "post timeout" || scenario == "post malformed"
				if wantSuccess {
					if err != nil || posts != 1 || result.Receipt.ProviderEffectRef == "" {
						t.Fatal("valid personal GitLab write did not retain its one effect", err)
					}
				} else if err == nil || posts != 0 && !wantUnknown || IsUnknownOutcome(err) != wantUnknown {
					t.Fatalf("metadata/effect classification changed: posts=%d unknown=%v error=%v", posts, IsUnknownOutcome(err), err)
				}
				if wantUnknown && posts != 1 || scenario == "missing profile" && gets != 0 || gets > 12 {
					t.Fatal("personal permission path crossed its finite dispatch boundary")
				}
			})
		}
	}
}
