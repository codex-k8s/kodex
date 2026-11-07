package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestJiraPersonalWritePermission checks real typed HTTP dispatch for every personal Server
// write. Only exact context/permission metadata may precede one POST; malformed/auth/timeout
// results never create effects or become global permission confirmations.
// TestJiraPersonalWritePermission проверяет настоящий typed HTTP dispatch каждой personal Server
// записи. Только exact context/permission допускают один POST; malformed/auth/timeout
// не создают effects и не становятся глобальными подтверждениями прав.
func TestJiraPersonalWritePermission(t *testing.T) {
	for _, operation := range []string{"jira.issue.create", "jira.issue.comment.write", "jira.issue.transition.apply"} {
		for _, scenario := range []string{"allow", "deny", "missing", "null", "wrong key", "global", "deprecated", "duplicate", "case alias", "401", "403", "404", "429", "timeout", "post timeout", "post response", "missing profile", "foreign issue"} {
			if scenario == "foreign issue" && operation == "jira.issue.create" {
				continue
			}
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-server-token")
				request := atlassianServerInvocation(t, adapter, jiraServerDefinition, operation, atlassianServerInput(operation), credential)
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor(jiraServerDefinition, operation)
				if !exists {
					t.Fatal("registered Jira permission contract missing")
				}
				request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, contract.Profile
				if scenario == "missing profile" {
					request.ResourcePermissionProfile = ""
				}
				permission := "CREATE_ISSUES"
				if operation == "jira.issue.comment.write" {
					permission = "ADD_COMMENTS"
				} else if operation == "jira.issue.transition.apply" {
					permission = "TRANSITION_ISSUES"
				}
				calls, probes, posts := 0, 0, 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method == http.MethodPost {
						posts++
						if probes != 1 || scenario != "allow" && scenario != "post timeout" && scenario != "post response" {
							t.Fatal("Jira mutation started without a positive exact permission check")
						}
						if scenario == "post timeout" {
							return nil, context.DeadlineExceeded
						}
						if scenario == "post response" {
							if operation == "jira.issue.transition.apply" {
								return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maximumResponseBytes+1))), Request: r}, nil
							}
							return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":`)), Request: r}, nil
						}
					}
					if scenario == "foreign issue" && r.Method == http.MethodGet && r.URL.Path == "/tracker/rest/api/2/issue/DEMO-1" {
						issue := jiraServerIssueFixture()
						issue["fields"].(map[string]any)["project"] = map[string]string{"key": "OTHER"}
						return serverFixtureJSONResponse(t, request, r, issue), nil
					}
					if r.URL.Path != "/tracker/rest/api/2/mypermissions" {
						return atlassianServerResponse(t, request, r), nil
					}
					probes++
					selector, value := "issueKey", "DEMO-1"
					if operation == "jira.issue.create" {
						selector, value = "projectKey", "DEMO"
					}
					if r.Method != http.MethodGet || len(r.URL.Query()) != 1 || r.URL.Query().Get(selector) != value || posts != 0 {
						t.Fatal("permission probe changed exact Server context or ordering")
					}
					validateAtlassianWireRequest(t, request, r)
					if scenario == "timeout" {
						return nil, context.DeadlineExceeded
					}
					status := http.StatusOK
					entry := map[string]any{"key": permission, "type": "PROJECT", "havePermission": true}
					permissions := map[string]any{permission: entry}
					switch scenario {
					case "deny":
						entry["havePermission"] = false
					case "missing":
						permissions = map[string]any{}
					case "null":
						entry["havePermission"] = nil
					case "wrong key":
						entry["key"] = "ADMINISTER_PROJECTS"
					case "global":
						entry["type"] = "GLOBAL"
					case "deprecated":
						entry["deprecatedKey"] = true
					case "401":
						status = http.StatusUnauthorized
					case "403":
						status = http.StatusForbidden
					case "404":
						status = http.StatusNotFound
					case "429":
						status = http.StatusTooManyRequests
					}
					raw, err := json.Marshal(map[string]any{"permissions": permissions})
					if err != nil {
						t.Fatal(err)
					}
					if scenario == "duplicate" || scenario == "case alias" {
						key := "havePermission"
						if scenario == "case alias" {
							key = "HavePermission"
						}
						raw = []byte(`{"permissions":{"` + permission + `":{"key":"` + permission + `","type":"PROJECT","havePermission":false,"` + key + `":true}}}`)
					}
					response := &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}
					if scenario == "allow" {
						wire := validateAtlassianWireRequest(t, request, r)
						validateAtlassianWireResponse(t, wire, response)
					}
					return response, nil
				})}
				result, err := adapter.Execute(t.Context(), request)
				if scenario == "allow" {
					if err != nil || probes != 1 || posts != 1 || result.Receipt.ProviderEffectRef == "" {
						t.Fatal("exact personal Jira permission did not authorize one bounded write", err)
					}
				} else if scenario == "post timeout" || scenario == "post response" {
					if !IsUnknownOutcome(err) || posts != 1 || probes != 1 {
						t.Fatal("a started Jira mutation lost uncertainty or was repeated", err)
					}
				} else if err == nil || posts != 0 || IsUnknownOutcome(err) {
					t.Fatal("failed pre-effect permission probe became a write or unknown external effect", err)
				}
				if scenario == "missing profile" && calls != 0 || scenario == "foreign issue" && probes != 0 {
					t.Fatal("invalid profile or foreign issue reached permission transport")
				}
			})
		}
	}
}

// TestJiraPersonalCreateRejoinWithoutWritePermission keeps marker-only prior evidence unknown,
// without consulting current CREATE_ISSUES or replaying the already completed POST.
// TestJiraPersonalCreateRejoinWithoutWritePermission сохраняет marker-only прежнее evidence неизвестным,
// не проверяя текущий CREATE_ISSUES и не повторяя завершённый POST.
func TestJiraPersonalCreateRejoinWithoutWritePermission(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.create", atlassianServerInput("jira.issue.create"), credential)
	request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, integrationpackage.CredentialResourcePermissionJiraIssueCreateV1
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/tracker/rest/api/2/search" {
			t.Fatal("known personal Jira effect reached a permission probe or repeated mutation")
		}
		issue := jiraServerIssueFixture()
		fields := issue["fields"].(map[string]any)
		fields["summary"], fields["description"], fields["labels"] = "Fixture", "body", []string{"kodex-effect-" + request.EffectKey}
		return serverFixtureJSONResponse(t, request, r, map[string]any{"startAt": 0, "maxResults": 2, "total": 1, "issues": []any{issue}}), nil
	})}
	result, err := adapter.Execute(t.Context(), request)
	if !IsUnknownOutcome(err) || calls != 1 || result.Receipt.ProviderEffectRef != "" {
		t.Fatal("known personal Jira create was not reconciled read-only", err)
	}
}
