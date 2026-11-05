package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serverFixtureJSONResponse validates a custom synthetic readback against the same provider contract as normal operation fixtures.
// serverFixtureJSONResponse проверяет synthetic readback по тому же provider contract, что и обычные fixtures.
func serverFixtureJSONResponse(t *testing.T, invocation Request, request *http.Request, value any) *http.Response {
	t.Helper()
	input := validateAtlassianWireRequest(t, invocation, request)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: request}
	validateAtlassianWireResponse(t, input, response)
	return response
}

// TestJiraServerCreateRejoinsExactLabelAndPayload proves adapter readback can rejoin one known effect without creating another issue.
// TestJiraServerCreateRejoinsExactLabelAndPayload доказывает readback одного effect без нового issue create.
func TestJiraServerCreateRejoinsExactLabelAndPayload(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.create", atlassianServerInput("jira.issue.create"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/search") || !strings.Contains(r.URL.Query().Get("jql"), "kodex-effect-"+request.EffectKey) {
			t.Fatal("known create effect was repeated or searched without its marker")
		}
		issue := jiraServerIssueFixture()
		issue["fields"].(map[string]any)["labels"] = []string{"kodex-effect-" + request.EffectKey}
		return serverFixtureJSONResponse(t, request, r, map[string]any{"startAt": 0, "maxResults": 2, "total": 1, "issues": []any{issue}}), nil
	})}
	first, err := adapter.Execute(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Execute(t.Context(), request)
	if err != nil || calls != 2 || first.Receipt.ProviderEffectRef != second.Receipt.ProviderEffectRef || second.Receipt.ProviderEffectRef != "jira-issue:DEMO-1" {
		t.Fatalf("readback did not preserve the same effect: calls=%d, %v", calls, err)
	}
}

// TestConfluenceServerCreateRejoinsMarkerAndInput requires the exact existing page body and immutable marker before reusing its receipt.
// TestConfluenceServerCreateRejoinsMarkerAndInput требует точные body и immutable marker перед повторным чтением receipt.
func TestConfluenceServerCreateRejoinsMarkerAndInput(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.create", atlassianServerInput("confluence.page.create"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/content") {
			t.Fatal("known page effect was repeated")
		}
		page := confluenceServerPageFixture("102", "New fixture", 1)
		page["body"].(map[string]any)["storage"].(map[string]string)["value"] = "<p>body</p>\n" + confluenceServerMarker(request)
		return serverFixtureJSONResponse(t, request, r, map[string]any{"start": 0, "limit": 2, "size": 1, "results": []any{page}, "_links": map[string]any{}}), nil
	})}
	first, err := adapter.Execute(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Execute(t.Context(), request)
	if err != nil || calls != 2 || first.Receipt.ProviderEffectRef != second.Receipt.ProviderEffectRef || second.Receipt.ProviderEffectRef != "confluence-page:102" {
		t.Fatalf("page readback did not rejoin: calls=%d, %v", calls, err)
	}
}

// TestConfluenceServerRejectsVersionConflictBeforePut prevents write when the actual provider version differs from the pinned input.
// TestConfluenceServerRejectsVersionConflictBeforePut запрещает write при несовпадении provider version и pinned input.
func TestConfluenceServerRejectsVersionConflictBeforePut(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	input := atlassianServerInput("confluence.page.update")
	input["expected_version"] = 9
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.update", input, credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("version conflict reached PUT")
		}
		return atlassianServerResponse(t, request, r), nil
	})}
	if _, err := adapter.Execute(t.Context(), request); err == nil || IsUnknownOutcome(err) || calls != 2 {
		t.Fatalf("known conflict was not rejected before write: calls=%d, %v", calls, err)
	}
}

// TestConfluenceServerSecondReadCannotInheritOldMetadata rejects missing scope/version fields on the second HTTP observation.
// TestConfluenceServerSecondReadCannotInheritOldMetadata отклоняет пропавшие scope/version поля во втором HTTP-наблюдении.
func TestConfluenceServerSecondReadCannotInheritOldMetadata(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.read", atlassianServerInput("confluence.page.read"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return atlassianServerResponse(t, request, r), nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"body":{"storage":{"value":"UNPROVEN_BODY","representation":"storage"}}}`)), Request: r}, nil
	})}
	result, err := adapter.Execute(t.Context(), request)
	if err == nil || result.Summary != "" || calls != 2 {
		t.Fatalf("unproven second body escaped: calls=%d, %v", calls, err)
	}
}

// TestJiraServerForeignProjectStopsComment checks actual returned project metadata, not only the requested issue-key prefix.
// TestJiraServerForeignProjectStopsComment проверяет возвращённый проект, а не только префикс запрошенного ключа.
func TestJiraServerForeignProjectStopsComment(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.comment.write", atlassianServerInput("jira.issue.comment.write"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("foreign project reached comment write")
		}
		issue := jiraServerIssueFixture()
		issue["fields"].(map[string]any)["project"] = map[string]string{"key": "OTHER"}
		return serverFixtureJSONResponse(t, request, r, issue), nil
	})}
	if _, err := adapter.Execute(t.Context(), request); err == nil || IsUnknownOutcome(err) || calls != 1 {
		t.Fatalf("foreign project was not denied before write: calls=%d, %v", calls, err)
	}
}

// TestAtlassianServerMutationDeadlineStaysUnknown proves in-flight request cancellation does not trigger a repeated external action.
// TestAtlassianServerMutationDeadlineStaysUnknown доказывает отсутствие повторного внешнего действия после отмены in-flight request.
func TestAtlassianServerMutationDeadlineStaysUnknown(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(key, func(t *testing.T) {
			operation := "jira.issue.create"
			if key == confluenceServerDefinition {
				operation = "confluence.page.create"
			}
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			request := atlassianServerInvocation(t, adapter, key, operation, atlassianServerInput(operation), credential)
			mutations := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					return atlassianServerResponse(t, request, r), nil
				}
				mutations++
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if _, err := adapter.Execute(ctx, request); !IsUnknownOutcome(err) || mutations != 1 {
				t.Fatalf("deadline lost UNKNOWN or repeated effect: count=%d, %v", mutations, err)
			}
		})
	}
}

// TestAtlassianServerBasicAuthenticationSupportsNonEmailUsername checks the explicit Server option without changing resource scope or returning credentials.
// TestAtlassianServerBasicAuthenticationSupportsNonEmailUsername проверяет явный Server option без изменения resource scope и выдачи credentials.
func TestAtlassianServerBasicAuthenticationSupportsNonEmailUsername(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(key, func(t *testing.T) {
			operation := "jira.identity.read"
			identity := map[string]any{"key": "synthetic-user", "name": "synthetic-user", "displayName": "Synthetic User"}
			if key == confluenceServerDefinition {
				operation = "confluence.identity.read"
				identity = map[string]any{"type": "known", "userKey": "synthetic-user", "username": "synthetic-user", "displayName": "Synthetic User"}
			}
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			request := atlassianServerInvocation(t, adapter, key, operation, map[string]any{}, credential)
			request.Configuration["auth_scheme"], request.Configuration["username"] = "BASIC", "synthetic-user"
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				username, password, present := r.BasicAuth()
				if !present || username != "synthetic-user" || password != "synthetic-server-token" {
					t.Fatal("explicit Basic credential binding changed")
				}
				return serverFixtureJSONResponse(t, request, r, identity), nil
			})}
			if result, err := adapter.Execute(t.Context(), request); err != nil || calls != 1 || strings.Contains(result.Summary, "synthetic-server-token") {
				t.Fatalf("Server Basic identity failed: calls=%d, %v", calls, err)
			}
		})
	}
}

// TestJiraServerTransitionWithRequiredFieldsStopsBeforePost refuses unsupported transition screens rather than guessing missing fields.
// TestJiraServerTransitionWithRequiredFieldsStopsBeforePost отклоняет неподдержанные transition screens без угадывания полей.
func TestJiraServerTransitionWithRequiredFieldsStopsBeforePost(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.transition.apply", atlassianServerInput("jira.issue.transition.apply"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("unsupported required transition fields reached POST")
		}
		if strings.HasSuffix(r.URL.Path, "/transitions") {
			return serverFixtureJSONResponse(t, request, r, map[string]any{"transitions": []any{map[string]any{
				"id": "11", "name": "Synthetic transition", "fields": map[string]any{"resolution": map[string]any{"required": true}},
			}}}), nil
		}
		return atlassianServerResponse(t, request, r), nil
	})}
	if _, err := adapter.Execute(t.Context(), request); err == nil || IsUnknownOutcome(err) || calls != 2 {
		t.Fatalf("required transition fields were not rejected: calls=%d, %v", calls, err)
	}
}

// TestConfluenceServerUpdateRejoinsExactVersionAndMarker verifies the observed successful version without sending a second PUT.
// TestConfluenceServerUpdateRejoinsExactVersionAndMarker проверяет уже успешную версию без повторного PUT.
func TestConfluenceServerUpdateRejoinsExactVersionAndMarker(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.update", atlassianServerInput("confluence.page.update"), credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("confirmed update was repeated")
		}
		page := confluenceServerPageFixture("101", "Updated fixture", 3)
		page["body"].(map[string]any)["storage"].(map[string]string)["value"] = "<p>new</p>\n" + confluenceServerMarker(request)
		return serverFixtureJSONResponse(t, request, r, page), nil
	})}
	if result, err := adapter.Execute(t.Context(), request); err != nil || calls != 2 || result.Receipt.ProviderEffectRef != "confluence-page:101" {
		t.Fatalf("known version was not rejoined: calls=%d, %v", calls, err)
	}
}

// TestAtlassianServerRecoveryNeverReplaysMutation keeps unsupported owner recovery UNKNOWN and performs no provider request.
// TestAtlassianServerRecoveryNeverReplaysMutation сохраняет UNKNOWN для неподдержанного owner recovery без provider request.
func TestAtlassianServerRecoveryNeverReplaysMutation(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		definition := serverDefinitionFixture(t, key)
		for _, capability := range definition.Spec.Capabilities {
			if capability.Risk == "READ" {
				continue
			}
			t.Run(capability.Operation, func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-server-token")
				request := atlassianServerInvocation(t, adapter, key, capability.Operation, atlassianServerInput(capability.Operation), credential)
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					t.Fatal("unsupported recovery reached provider transport")
					return nil, nil
				})}
				if _, err := adapter.Recover(t.Context(), request); !IsUnknownOutcome(err) {
					t.Fatalf("unsupported recovery lost UNKNOWN: %v", err)
				}
			})
		}
	}
}
