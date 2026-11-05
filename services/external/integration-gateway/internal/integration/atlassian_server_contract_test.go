package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// atlassianProviderContract reads only the repository-owned documented subset and disables external references.
// atlassianProviderContract читает только repo-owned documented subset и запрещает внешние ссылки.
func atlassianProviderContract(t *testing.T, key string) ([]byte, *openapi3.T) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", "integrations", "providers", key, "v1", "openapi.yaml")
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 256<<10 {
		t.Fatal("bounded provider contract unavailable")
	}
	loader := openapi3.NewLoader()
	loader.Context, loader.IsExternalRefsAllowed = t.Context(), false
	document, err := loader.LoadFromData(raw)
	if err != nil || document == nil || document.Validate(t.Context()) != nil {
		t.Fatalf("invalid provider contract: %v", err)
	}
	return raw, document
}

// TestAtlassianProviderContractsMatchDefinitions requires exact operation coverage and records import inspection without granting execution.
// TestAtlassianProviderContractsMatchDefinitions требует exact operation coverage; inspection не выдаёт права исполнения.
func TestAtlassianProviderContractsMatchDefinitions(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(key, func(t *testing.T) {
			raw, _ := atlassianProviderContract(t, key)
			inspection, err := integrationpackage.InspectOpenAPI(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			definition := serverDefinitionFixture(t, key)
			if len(inspection.Operations) != len(definition.Spec.Capabilities) {
				t.Fatal("provider/package operation coverage differs")
			}
			for _, operation := range inspection.Operations {
				if _, found := definition.Capability(operation.ID); !found {
					t.Fatalf("unmapped provider operation: %s", operation.ID)
				}
			}
		})
	}
}

// validateAtlassianWireRequest validates the actual synthetic native request after removing only its owner-configured context.
// It does not change real adapter routing, mint grants or enable transport replay for mutations.
// validateAtlassianWireRequest проверяет synthetic native request, убирая только owner-configured context.
// Реальный маршрут, grants и запрет transport replay этим не меняются.
func validateAtlassianWireRequest(t *testing.T, invocation Request, request *http.Request) *openapi3filter.RequestValidationInput {
	t.Helper()
	if request.Method != http.MethodGet && request.GetBody != nil {
		t.Fatal("native mutation transport replay is enabled")
	}
	_, document := atlassianProviderContract(t, invocation.DefinitionKey)
	router, err := legacy.NewRouter(document)
	if err != nil {
		t.Fatal(err)
	}
	copy := request.Clone(t.Context())
	address := *request.URL
	copy.URL = &address
	prefix := invocation.Configuration["context_path"].(string)
	if prefix == "/" {
		prefix = ""
	}
	if !strings.HasPrefix(copy.URL.Path, prefix+"/rest/api/") {
		t.Fatal("wire request escaped configured API context")
	}
	copy.URL.Path, copy.URL.RawPath = strings.TrimPrefix(copy.URL.Path, prefix), ""
	if request.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(request.Body, maximumResponseBytes+1))
		if err != nil || len(raw) > maximumResponseBytes {
			t.Fatal("fixture request exceeded native budget")
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		copy.Body = io.NopCloser(bytes.NewReader(raw))
	}
	route, parameters, err := router.FindRoute(copy)
	if err != nil {
		t.Fatalf("native request has no provider contract route: %v", err)
	}
	input := &openapi3filter.RequestValidationInput{Request: copy, PathParams: parameters, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, authentication *openapi3filter.AuthenticationInput) error {
			wire := authentication.RequestValidationInput.Request
			if invocation.Configuration["auth_scheme"] == "BASIC" {
				username, password, present := wire.BasicAuth()
				if authentication.SecuritySchemeName == "basicAuth" && present && username == "synthetic-user" && password == "synthetic-server-token" {
					return nil
				}
			} else if authentication.SecuritySchemeName == "bearerAuth" && wire.Header.Get("Authorization") == "Bearer synthetic-server-token" {
				return nil
			}
			return errors.New("synthetic provider authentication rejected")
		}}}
	if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
		t.Fatalf("native wire request violates provider contract: %v", err)
	}
	return input
}

// validateAtlassianWireResponse checks the full synthetic response against the matched provider operation before native projection.
// validateAtlassianWireResponse проверяет synthetic response по provider operation до native projection.
func validateAtlassianWireResponse(t *testing.T, request *openapi3filter.RequestValidationInput, response *http.Response) {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	input := &openapi3filter.ResponseValidationInput{RequestValidationInput: request, Status: response.StatusCode, Header: response.Header}
	input.SetBodyBytes(raw)
	if err := openapi3filter.ValidateResponse(t.Context(), input); err != nil {
		t.Fatalf("synthetic response violates provider contract: %v", err)
	}
}
