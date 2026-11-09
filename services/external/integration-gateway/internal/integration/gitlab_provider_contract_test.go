package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
	if err != nil || len(inspection.Operations) != 16 || len(document.Paths.Map()) != 14 {
		t.Fatal("GitLab subset coverage changed", err)
	}
	definition := legacyGitLabTestAdapter(t).definitions["gitlab"]
	internal := map[string]bool{"gitlab.permission.version.read": true, "gitlab.permission.identity.read": true,
		"gitlab.permission.token.self.read": true, "gitlab.permission.token.details.read": true, "gitlab.permission.branch_rules.read": true}
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
