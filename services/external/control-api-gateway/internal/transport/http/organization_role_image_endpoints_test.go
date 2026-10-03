package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func organizationImageFixture() *controlplanev1.RoleImageRecipe {
	image := roleImageRecipeFixture()
	image.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	image.OrganizationRef = "org_fixture01"
	image.ProjectRef = ""
	return image
}

func TestOrganizationRoleImageHTTPRoutesUseSpecializedOwnerRPC(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create", "update", "command"} {
		t.Run(name, func(t *testing.T) {
			client := &catalogRPCRecorder{response: &controlplanev1.ManageOrganizationRoleImageRecipeResponse{Recipe: organizationImageFixture()}}
			method, path, body := "POST", "/api/v1/organization/role-image-recipes", `{"name":"Image","environment":{"environmentKey":"standard"}}`
			if name == "update" {
				method = "PATCH"
				path += "/imgrec_fixture01"
			}
			if name == "command" {
				path += "/imgrec_fixture01/commands"
				body = `{"action":"REQUEST_BUILD"}`
			}
			request := httptest.NewRequest(method, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", "synthetic-csrf")
			request.Header.Set("Idempotency-Key", "org-image-http")
			if name != "create" {
				request.Header.Set("If-Match", `"2"`)
			}
			writer := httptest.NewRecorder()
			roleImageHTTPHandler(client).ServeHTTP(writer, request)
			if writer.Code != http.StatusOK && writer.Code != http.StatusCreated {
				t.Fatalf("%s route rejected: %d %s", name, writer.Code, writer.Body.String())
			}
			if client.method != controlplanev1.RoleImageService_ManageOrganizationRoleImageRecipe_FullMethodName {
				t.Fatal("organization image routed through project mutation")
			}
			input := client.request.(*controlplanev1.ManageOrganizationRoleImageRecipeRequest)
			if name == "create" && (input.GetRecipeRef() != "" || input.GetEnvironment().GetEnvironmentKey() != "standard") {
				t.Fatal("organization image create mapping mismatch")
			}
		})
	}
	client := &catalogRPCRecorder{response: &controlplanev1.ListOrganizationRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{organizationImageFixture()}, Total: 1}}
	writer := httptest.NewRecorder()
	roleImageHTTPHandler(client).ServeHTTP(writer, httptest.NewRequest("GET", "/api/v1/organization/role-image-recipes?pageSize=50", nil))
	var result generated.RoleImageRecipePage
	if writer.Code != 200 || json.Unmarshal(writer.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].ScopeKind != generated.RuntimeResourceScopeKindORGANIZATION || result.Items[0].ProjectRef != "" {
		t.Fatal("organization image list lost exact scope")
	}
}

func TestOrganizationRoleImageHTTPRejectsForeignScopeSnapshots(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"project", "foreign-build", "foreign-artifact", "wrong-recipe"} {
		t.Run(name, func(t *testing.T) {
			recipe := organizationImageFixture()
			response := &controlplanev1.GetOrganizationRoleImageRecipeResponse{Recipe: recipe}
			switch name {
			case "project":
				recipe.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
				recipe.ProjectRef = "prj_fixture01"
			case "wrong-recipe":
				recipe.Ref = "imgrec_foreign01"
			case "foreign-build":
				response.Builds = []*controlplanev1.ImageBuild{{ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_foreign01", RecipeRef: recipe.Ref}}
			case "foreign-artifact":
				response.ActiveArtifact = &controlplanev1.ImageArtifact{ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_foreign01", RecipeRef: recipe.Ref}
			}
			writer := httptest.NewRecorder()
			roleImageHTTPHandler(&catalogRPCRecorder{response: response}).ServeHTTP(writer, httptest.NewRequest("GET", "/api/v1/organization/role-image-recipes/imgrec_fixture01", nil))
			if writer.Code != 502 || strings.Contains(writer.Body.String(), "foreign01") {
				t.Fatal("foreign organization image owner tuple escaped upstream boundary")
			}
		})
	}
}
