package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// TestGitLabProviderSubsetContract checks the authored versioned subset without claiming
// complete API coverage or promoting internal metadata endpoints to executable model tools.
// TestGitLabProviderSubsetContract проверяет авторский versioned subset, не заявляя полное
// API coverage и не превращая internal metadata endpoints в исполняемые model tools.
func TestGitLabProviderSubsetContract(t *testing.T) {
	raw, document := documentedProviderContract(t, "gitlab")
	inspection, err := integrationpackage.InspectOpenAPI(t.Context(), raw)
	if err != nil || len(inspection.Operations) != 18 || len(document.Paths.Map()) != 16 {
		t.Fatal("GitLab subset coverage changed", err)
	}
	definition := testAdapter(t).definitions["gitlab"]
	internal := map[string]bool{"gitlab.permission.version.read": true, "gitlab.permission.identity.read": true,
		"gitlab.permission.token.self.read": true, "gitlab.permission.token.details.read": true, "gitlab.permission.branch_rules.read": true, "gitlab.permission.group.read": true}
	for _, operation := range inspection.Operations {
		_, exposed := definition.Capability(operation.ID)
		if internal[operation.ID] {
			if exposed || operation.Method != http.MethodGet {
				t.Fatal("internal metadata route became an executable tool or mutation")
			}
		} else if !exposed {
			t.Fatal("documented GitLab operation has no existing typed consumer")
		}
	}
	if bytes.Contains(raw, []byte("$ref: https:")) || bytes.Contains(raw, []byte("$ref: http:")) {
		t.Fatal("provider subset acquired external schema references")
	}
}

// TestGitLabMergeRequestApprovalScope exercises the scoped GET, exact object binding and safe output through an ordinary adapter claim.
// TestGitLabMergeRequestApprovalScope проверяет GET в заданной области, точную привязку объекта и безопасный вывод через обычный вызов адаптера.
func TestGitLabMergeRequestApprovalScope(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     string
		expected string
	}{
		{"satisfied without rules", `{"project_id":20,"iid":4,"approved":true,"approvals_required":0,"approvals_left":0,"approved_by":[]}`,
			`{"approved":true,"approvals_left":0,"approvals_required":0,"approver_count":0,"merge_request_iid":4,"project_id":20}`},
		{"pending without optional counts", `{"project_id":20,"iid":4,"approved":false,"approved_by":[{"user":{"name":"synthetic-name"}}]}`,
			`{"approved":false,"approver_count":1,"merge_request_iid":4,"project_id":20}`},
		{"foreign project", `{"project_id":21,"iid":4,"approved":true}`, ""},
		{"different merge request", `{"project_id":20,"iid":5,"approved":true}`, ""},
		{"missing state", `{"project_id":20,"iid":4}`, ""},
		{"malformed approver", `{"project_id":20,"iid":4,"approved":true,"approved_by":[null]}`, ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			request := scopedGitLabTestRequest(t, adapter, "gitlab.merge_request.approval.read",
				map[string]string{"base_url": "https://git.example.invalid", "scope_kind": "GROUP", "group_path": "team"},
				map[string]any{"project_id": 20, "project_path": "team/service", "merge_request_iid": 4})
			request.Credential = testCredential(t, adapter, "synthetic-gitlab-personal-key")
			verifyWire := gitLabWireValidator(t, request)
			reads := 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "git.example.invalid" || r.Header.Get("Authorization") != "Bearer synthetic-gitlab-personal-key" {
					t.Fatal("request escaped the scoped GET or credential")
				}
				var body string
				switch r.URL.Path {
				case "/api/v4/groups/team":
					body = `{"id":10,"full_path":"team"}`
				case "/api/v4/projects/team/service":
					body = `{"id":20,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"team","kind":"group"}}`
				case "/api/v4/projects/20/merge_requests/4/approvals":
					reads++
					body = scenario.body
				default:
					t.Fatalf("unexpected provider path: %s", r.URL.Path)
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
				if reads > 0 && scenario.expected != "" {
					verifyWire(r, response)
				}
				return response, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if reads != 1 {
				t.Fatalf("expected one exact approvals GET, got %d", reads)
			}
			if scenario.expected == "" {
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Receipt.EffectKey != "" {
					t.Fatal("invalid approval response issued a receipt", err)
				}
				return
			}
			var observed, expected map[string]any
			if err != nil || json.Unmarshal([]byte(result.Summary), &observed) != nil ||
				json.Unmarshal([]byte(scenario.expected), &expected) != nil ||
				!reflect.DeepEqual(observed, expected) || result.Receipt.EffectKey != request.EffectKey {
				t.Fatal("safe approval projection or receipt did not match", err)
			}
		})
	}
}

// gitLabWireValidator checks synthetic native requests and responses against one loaded subset.
// Only the configured fixture origin is mapped to the document's synthetic server; paths,
// escaped project/branch values, queries, headers and payloads remain the actual wire values.
// gitLabWireValidator сверяет synthetic native requests/responses с одним загруженным subset.
// Только configured fixture origin заменяется synthetic server документа; paths, escaped
// project/branch, query, headers и payload остаются настоящими wire values.
func gitLabWireValidator(t *testing.T, invocation Request) func(*http.Request, *http.Response) {
	t.Helper()
	_, document := documentedProviderContract(t, "gitlab")
	router, err := legacy.NewRouter(document)
	if err != nil {
		t.Fatal(err)
	}
	return func(request *http.Request, response *http.Response) {
		t.Helper()
		if request.URL.Scheme+"://"+request.URL.Host != invocation.Configuration["base_url"] || !strings.HasPrefix(request.URL.EscapedPath(), "/api/v4/") || request.Method != http.MethodGet && request.GetBody != nil {
			t.Fatal("GitLab wire escaped configured origin/API or enabled mutation replay")
		}
		copy := request.Clone(t.Context())
		address := *request.URL
		copy.URL, copy.Host = &address, "git.example.invalid"
		copy.URL.Host = copy.Host
		if request.Body != nil {
			raw, err := io.ReadAll(io.LimitReader(request.Body, maximumResponseBytes+1))
			if err != nil || len(raw) > maximumResponseBytes {
				t.Fatal("GitLab fixture request exceeded bounded body")
			}
			request.Body, copy.Body = io.NopCloser(bytes.NewReader(raw)), io.NopCloser(bytes.NewReader(raw))
		}
		route, parameters, err := router.FindRoute(copy)
		if err != nil {
			t.Fatal("GitLab native request has no documented subset route", err)
		}
		input := &openapi3filter.RequestValidationInput{Request: copy, PathParams: parameters, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, auth *openapi3filter.AuthenticationInput) error {
				if auth.SecuritySchemeName == "bearerAuth" && auth.RequestValidationInput.Request.Header.Get("Authorization") == "Bearer synthetic-gitlab-personal-key" {
					return nil
				}
				return errors.New("synthetic provider authentication rejected")
			}}}
		if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
			t.Fatal("GitLab native request violates the versioned subset", err)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
		if err != nil || len(raw) > maximumResponseBytes {
			t.Fatal("GitLab fixture response exceeded bounded body")
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
		output := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: response.StatusCode, Header: response.Header}
		output.SetBodyBytes(raw)
		if err := openapi3filter.ValidateResponse(t.Context(), output); err != nil {
			t.Fatal("GitLab native response violates the versioned subset", err)
		}
	}
}

// TestGitLabSubsetObjectReadWire qualifies the existing four project-scoped READ adapters
// against the authored contract. This is synthetic HTTP dispatch, not an installed role run.
// TestGitLabSubsetObjectReadWire проверяет четыре прежних project-scoped READ adapter по
// авторскому контракту. Это synthetic HTTP dispatch, не installed role run.
func TestGitLabSubsetObjectReadWire(t *testing.T) {
	for _, operation := range []string{"gitlab.repository.file.read", "gitlab.issue.read", "gitlab.merge_request.read", "gitlab.pipeline.read"} {
		t.Run(operation, func(t *testing.T) {
			adapter := legacyGitLabTestAdapter(t)
			credential := testCredential(t, adapter, "synthetic-gitlab-personal-key")
			var input map[string]any
			if json.Unmarshal([]byte(catalogInputs()[operation]), &input) != nil {
				t.Fatal("GitLab object fixture missing")
			}
			request := invocationRequest(t, adapter.definitions["gitlab"], operation, input, credential)
			request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, integrationpackage.CredentialResourcePermissionGitLabObjectReadV1
			validate := gitLabWireValidator(t, request)
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet {
					t.Fatal("object read acquired an external mutation")
				}
				response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, "gitlab", operation, r))), Request: r}
				validate(r, response)
				return response, nil
			})}
			if _, err := adapter.Execute(t.Context(), request); err != nil || calls != 1 {
				t.Fatal("documented GitLab object read failed its one scoped GET", err)
			}
		})
	}
}
