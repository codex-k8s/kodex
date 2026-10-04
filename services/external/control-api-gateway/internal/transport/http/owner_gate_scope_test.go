package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestOwnerGateOrganizationScopePreservesNoProjectOnEveryPath(t *testing.T) {
	for _, kind := range []string{"get", "list", "resolve"} {
		t.Run(kind, func(t *testing.T) {
			gate := integrationGateFixture()
			gate.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
			gate.ProjectRef = ""
			gate.IntegrationIntent = nil
			code, result := gateProjectionResponse(t, kind, gate)
			if code != 200 || result["scopeKind"] != "ORGANIZATION" || result["organizationRef"] != "org_fixture01" {
				t.Fatalf("organization gate projection lost owner scope: %d", code)
			}
			if _, exists := result["projectRef"]; exists {
				t.Fatal("organization owner gate invented a project locator")
			}
		})
	}
}

func TestOwnerGateRejectsInvalidOwnerScopeOnEveryPath(t *testing.T) {
	for name, mutate := range map[string]func(*cp.OwnerGate){
		"unknown scope": func(g *cp.OwnerGate) { g.ScopeKind = cp.RuntimeResourceScopeKind(999) },
		"missing scope": func(g *cp.OwnerGate) {
			g.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_UNSPECIFIED
		},
		"missing organization":    func(g *cp.OwnerGate) { g.OrganizationRef = "" },
		"invalid organization":    func(g *cp.OwnerGate) { g.OrganizationRef = "org/invalid" },
		"project without locator": func(g *cp.OwnerGate) { g.ProjectRef = "" },
		"invalid project locator": func(g *cp.OwnerGate) { g.ProjectRef = "prj/invalid" },
		"organization with project": func(g *cp.OwnerGate) {
			g.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"get", "list", "resolve"} {
				gate := integrationGateFixture()
				mutate(gate)
				code, result := gateProjectionResponse(t, kind, gate)
				if code != 502 || result["gate"] != nil || result["items"] != nil || result["integrationIntent"] != nil || result["scopeKind"] != nil {
					t.Fatalf("%s disclosed malformed owner scope: %d", kind, code)
				}
			}
		})
	}
}

func TestOwnerGateOrganizationCannotSatisfyProjectFilteredCatalog(t *testing.T) {
	response := gateCatalogFixture()
	response.Gates[0].ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	response.Gates[0].ProjectRef = ""
	client := &catalogRPCRecorder{response: response}
	writer := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(writer, httptest.NewRequest("GET", "/api/v1/owner-gates?projectRef=prj_fixture01", nil))
	if writer.Code != 502 || !strings.Contains(writer.Body.String(), "INVALID_UPSTREAM_RESPONSE") || strings.Contains(writer.Body.String(), "gate_fixture01") {
		t.Fatal("organization gate satisfied a project-specific read boundary")
	}
}
