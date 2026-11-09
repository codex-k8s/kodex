package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestPersonalObjectReadsExecuteCurrentScopedGet verifies real adapter dispatch for all nine
// minimal resource reads. Old/mismatched profiles reach no HTTP; the actual GET rechecks access,
// 401 retains key revalidation,403 refuses only the object, and shared semantics stay unchanged.
// TestPersonalObjectReadsExecuteCurrentScopedGet проверяет настоящий adapter dispatch девяти
// минимальных resource READ. Старый/чужой профиль не достигает HTTP; GET заново проверяет доступ,
// 401 сохраняет revalidation ключа,403 запрещает объект; shared semantics не меняются.
func TestPersonalObjectReadsExecuteCurrentScopedGet(t *testing.T) {
	for definition, operations := range map[string][]string{
		"gitlab":               {"gitlab.repository.file.read", "gitlab.issue.read", "gitlab.merge_request.read", "gitlab.pipeline.read"},
		"jira-server-dc":       {"jira.issue.search", "jira.issue.read", "jira.issue.transition.list"},
		"confluence-server-dc": {"confluence.page.search", "confluence.page.read"},
	} {
		for _, operation := range operations {
			for _, scenario := range []string{"allowed", "401", "403", "404", "old profile", "foreign profile", "shared403"} {
				t.Run(operation+"/"+scenario, func(t *testing.T) {
					adapter := legacyGitLabTestAdapter(t)
					credential := testCredential(t, adapter, "synthetic-server-token")
					var request Request
					if definition == "gitlab" {
						var input map[string]any
						if json.Unmarshal([]byte(catalogInputs()[operation]), &input) != nil {
							t.Fatal("object READ fixture input missing")
						}
						request = invocationRequest(t, adapter.definitions[definition], operation, input, credential)
					} else {
						request = atlassianServerInvocation(t, adapter, definition, operation, atlassianServerInput(operation), credential)
					}
					contract, exists := integrationpackage.CredentialResourcePermissionContractFor(definition, operation)
					if !exists {
						t.Fatal("scoped object contract missing")
					}
					request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, contract.Profile
					status, wantCode := http.StatusOK, ""
					switch scenario {
					case "401":
						status, wantCode = http.StatusUnauthorized, "INTEGRATION_AUTH_REJECTED"
					case "403":
						status, wantCode = http.StatusForbidden, "INTEGRATION_REQUEST_REJECTED"
					case "404":
						status, wantCode = http.StatusNotFound, "INTEGRATION_REQUEST_REJECTED"
					case "old profile":
						request.ResourcePermissionProfile, wantCode = "", "INTEGRATION_REQUEST_REJECTED"
					case "foreign profile":
						request.ResourcePermissionProfile, wantCode = integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, "INTEGRATION_REQUEST_REJECTED"
					case "shared403":
						request.PersonalCredentialAdmission, request.ResourcePermissionProfile = false, ""
						status, wantCode = http.StatusForbidden, "INTEGRATION_AUTH_REJECTED"
					}
					calls := 0
					adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.Header.Get("Authorization") == "" && r.Header.Get("PRIVATE-TOKEN") == "" {
							t.Fatal("scoped READ changed transport, auth or mutation boundary")
						}
						if definition != "gitlab" {
							response := atlassianServerResponse(t, request, r)
							response.StatusCode = status
							return response, nil
						}
						if r.URL.Hostname() != strings.TrimPrefix(request.Configuration["base_url"].(string), "https://") || !strings.HasPrefix(r.URL.EscapedPath(), "/api/v4/projects/group%2Fproject/") {
							t.Fatal("GitLab object READ escaped its configured project")
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, "gitlab", operation, r))), Request: r}, nil
					})}
					result, err := adapter.Execute(t.Context(), request)
					if scenario == "old profile" || scenario == "foreign profile" {
						if calls != 0 {
							t.Fatal("unsupported object profile reached provider HTTP")
						}
					} else if calls < 1 || calls > 2 {
						t.Fatal("scoped READ omitted or replayed its bounded provider observation")
					}
					if wantCode == "" {
						if err != nil || result.Receipt.ProviderEffectRef == "" {
							t.Fatal("successful scoped GET lost its typed result", err)
						}
					} else {
						var safe *SafeError
						if !errors.As(err, &safe) || safe.Code != wantCode || IsUnknownOutcome(err) || result.Receipt.ProviderEffectRef != "" {
							t.Fatalf("scoped READ refusal changed lifecycle: %v", err)
						}
					}
				})
			}
		}
	}
}
