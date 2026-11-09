package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestGitLabReadOnlyCreateRecovery executes the public Recover path in both credential modes.
// Exact author/project/payload, exhaustive bounded search and detail readback are required;
// every request is GET and unproven outcomes produce neither a receipt nor a repeated mutation.
// TestGitLabReadOnlyCreateRecovery выполняет публичный Recover path в обоих credential modes.
// Нужны exact author/project/payload, полный bounded search и detail readback; все запросы GET,
// а недоказанные исходы не создают receipt либо повторного изменения.
func TestGitLabReadOnlyCreateRecovery(t *testing.T) {
	for _, operation := range []string{"gitlab.issue.create", "gitlab.merge_request.create"} {
		for _, personal := range []bool{false, true} {
			for _, scenario := range []string{"allow", "second page", "empty", "duplicate marker", "foreign author", "foreign project", "wrong title", "wrong description", "missing author", "wrong branches", "detail changed", "detail ID changed", "duplicate JSON", "page exhausted", "repeated row", "401", "403", "404", "429", "timeout", "old version"} {
				t.Run(operation+"/"+strconv.FormatBool(personal)+"/"+scenario, func(t *testing.T) {
					adapter := legacyGitLabTestAdapter(t)
					credential := testCredential(t, adapter, "synthetic-gitlab-personal-key")
					var input map[string]any
					if json.Unmarshal([]byte(catalogInputs()[operation]), &input) != nil {
						t.Fatal("missing create payload fixture")
					}
					request := invocationRequest(t, adapter.definitions["gitlab"], operation, input, credential)
					if personal {
						contract, _ := integrationpackage.CredentialResourcePermissionContractFor("gitlab", operation)
						request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, contract.Profile
					}
					var payload gitLabRecoveryInput
					encoded, _ := json.Marshal(input)
					_ = json.Unmarshal(encoded, &payload)
					marker := "[kodex-effect:" + request.EffectKey + "]"
					description := strings.TrimSpace(payload.Description)
					if description != "" {
						description += "\n\n"
					}
					description += marker
					candidate := map[string]any{"id": 101, "iid": 11, "title": payload.Title, "description": description, "state": "opened", "web_url": request.Configuration["base_url"].(string) + "/group/project/-/issues/11", "author": map[string]int{"id": 7}, "project_id": 42, "source_project_id": 42, "target_project_id": 42, "source_branch": payload.SourceBranch, "target_branch": payload.TargetBranch}
					switch scenario {
					case "foreign author":
						candidate["author"] = map[string]int{"id": 8}
					case "foreign project":
						candidate["project_id"], candidate["target_project_id"] = 43, 43
					case "wrong title":
						candidate["title"] = "other"
					case "wrong description":
						candidate["description"] = "other\n\n" + marker
					case "missing author":
						delete(candidate, "author")
					case "wrong branches":
						if operation == "gitlab.merge_request.create" {
							candidate["source_branch"] = "other"
						} else {
							candidate["project_id"] = 43
						}
					}
					path := "/api/v4/projects/group%2Fproject/issues"
					if operation == "gitlab.merge_request.create" {
						path = "/api/v4/projects/group%2Fproject/merge_requests"
					}
					gets, lists, details := 0, 0, 0
					validate := gitLabWireValidator(t, request)
					adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						gets++
						if gets > 10 || r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != strings.TrimPrefix(request.Configuration["base_url"].(string), "https://") || r.Header.Get("Authorization") != "Bearer synthetic-gitlab-personal-key" {
							t.Fatal("recovery escaped fixed GET/current-key/budget boundary")
						}
						var body []byte
						if r.URL.EscapedPath() == path {
							lists++
							query := r.URL.Query()
							if query.Get("scope") != "all" || query.Get("state") != "all" || query.Get("search") != marker || query.Get("in") != "description" || query.Get("per_page") != "100" || query.Get("order_by") != "created_at" || query.Get("sort") != "asc" {
								t.Fatal("recovery search lost exact scope or finite pagination")
							}
							page, e := strconv.Atoi(query.Get("page"))
							if e != nil || page != lists || page > 5 {
								t.Fatal("recovery page repeated or out of bounds")
							}
							rows := []any{candidate}
							switch scenario {
							case "empty":
								rows = []any{}
							case "duplicate marker":
								other := map[string]any{}
								for k, v := range candidate {
									other[k] = v
								}
								other["id"], other["iid"] = 102, 12
								rows = append(rows, other)
							case "second page", "page exhausted", "repeated row":
								if page == 1 || scenario == "page exhausted" {
									rows = make([]any, 100)
									for i := range rows {
										row := map[string]any{}
										for k, v := range candidate {
											row[k] = v
										}
										row["id"], row["iid"], row["description"] = 1000+page*100+i, 1000+page*100+i, "unrelated"
										rows[i] = row
									}
									if scenario == "repeated row" {
										rows[1] = rows[0]
									}
								}
							}
							body, _ = json.Marshal(rows)
							if scenario == "duplicate JSON" {
								body = []byte(`[{"id":101,"ID":102,"description":"other"}]`)
							}
						} else if r.URL.EscapedPath() == path+"/11" {
							details++
							detail := map[string]any{}
							for k, v := range candidate {
								detail[k] = v
							}
							if scenario == "detail changed" {
								detail["description"] = "changed"
							}
							if scenario == "detail ID changed" {
								detail["id"] = 999
							}
							body, _ = json.Marshal(detail)
						} else {
							if scenario == "timeout" && r.URL.Path == "/api/v4/version" {
								return nil, context.DeadlineExceeded
							}
							response := gitLabPermissionResponse(t, request, r, scenario)
							if scenario == "allow" || scenario == "second page" {
								validate(r, response)
							}
							return response, nil
						}
						response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}
						if scenario == "allow" || scenario == "second page" {
							validate(r, response)
						}
						return response, nil
					})}
					result, err := adapter.Recover(t.Context(), request)
					success := scenario == "allow" || scenario == "second page"
					if success {
						if err != nil || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ProviderEffectRef == "" || details != 1 {
							t.Fatal("exact recovery lost immutable effect receipt", err)
						}
					} else if err == nil || result.Receipt.ProviderEffectRef != "" {
						t.Fatal("unproven recovery fabricated success/receipt")
					}
					if scenario == "page exhausted" && lists != 5 {
						t.Fatal("recovery did not apply its finite page budget")
					}
					if scenario == "empty" && details != 0 {
						t.Fatal("empty search selected arbitrary object")
					}
				})
			}
		}
	}
}

// TestGitLabBranchRecoveryRemainsUnproven refuses to infer branch creation from branch existence.
// Unsupported recovery performs no provider request and cannot replay the original branch POST.
// TestGitLabBranchRecoveryRemainsUnproven не выводит создание ветки из её существования.
// Unsupported recovery не обращается к provider и не повторяет первоначальный branch POST.
func TestGitLabBranchRecoveryRemainsUnproven(t *testing.T) {
	adapter := legacyGitLabTestAdapter(t)
	credential := testCredential(t, adapter, "synthetic-gitlab-personal-key")
	request := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.branch.create", map[string]any{"branch": "feature", "ref": "main"}, credential)
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("branch recovery fabricated a provenance read")
		return nil, nil
	})}
	_, err := adapter.Recover(t.Context(), request)
	if !IsUnknownOutcome(err) {
		t.Fatal("branch existence became creation proof", err)
	}
}

// TestGitLabMarkerRequiresReadOnlyReconciliation rejects marker-only adoption in the real
// Execute path for shared and personal keys; a known marker cannot cause another POST.
// TestGitLabMarkerRequiresReadOnlyReconciliation отклоняет marker-only adoption в настоящем
// Execute path для shared и personal keys; известный marker не приводит к повторному POST.
func TestGitLabMarkerRequiresReadOnlyReconciliation(t *testing.T) {
	for _, personal := range []bool{false, true} {
		adapter := legacyGitLabTestAdapter(t)
		credential := testCredential(t, adapter, "synthetic-gitlab-personal-key")
		request := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.issue.create", map[string]any{"title": "Synthetic issue", "description": "Synthetic body"}, credential)
		if personal {
			contract, _ := integrationpackage.CredentialResourcePermissionContractFor("gitlab", request.CapabilityKey)
			request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, contract.Profile
		}
		calls := 0
		adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls != 1 || r.Method != http.MethodGet || !strings.HasSuffix(r.URL.EscapedPath(), "/issues") {
				t.Fatal("marker-only evidence replayed a mutation")
			}
			body, _ := json.Marshal([]map[string]any{{"id": 101, "iid": 11, "title": "unrelated", "description": "[kodex-effect:" + request.EffectKey + "]", "state": "opened", "web_url": "https://git.example.invalid/fixture/11"}})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
		})}
		result, err := adapter.Execute(t.Context(), request)
		if !IsUnknownOutcome(err) || result.Receipt.ProviderEffectRef != "" || calls != 1 {
			t.Fatal("marker without provenance became a successful creation", err)
		}
	}
}
