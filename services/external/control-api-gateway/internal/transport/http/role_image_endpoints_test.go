package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func roleImageHTTPHandler(client *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{RoleImages: controlplanev1.NewRoleImageServiceClient(client)}})
}

func roleImageRecipeFixture() *controlplanev1.RoleImageRecipe {
	return &controlplanev1.RoleImageRecipe{SourceAvailable: true, Ref: "imgrec_fixture01", ProjectRef: "prj_fixture01", RoleDefinitionRef: "role_fixture01", Version: 2, Generation: 3, Name: "Окружение", State: "ACTIVE", CreatedAt: timestamppb.New(time.Now().UTC()), UpdatedAt: timestamppb.New(time.Now().UTC()), Environment: &controlplanev1.RoleEnvironmentSelection{EnvironmentKey: "custom", Dockerfile: "FROM scratch"}, ManagedLineage: &controlplanev1.RoleImageManagedLineage{ConfigurationRef: "cfg_fixture01", RevisionRef: "cfr_fixture01", Revision: 4, ManagedBy: "UI", Origin: "MANAGED", SourceRef: "ui:recipe", SourceRevision: "4"}}
}

func TestRoleImageMetadataAndSourceAdmissionStaySeparate(t *testing.T) {
	missing := roleImageRecipeFixture()
	missing.Environment.Dockerfile = ""
	if validRoleImageSource(missing) || validRoleImageBuildSource(&controlplanev1.ImageBuild{SourceAvailable: true}) {
		t.Fatal("available source omitted its authoritative Dockerfile")
	}
	for _, available := range []bool{false, true} {
		recipe := roleImageRecipeFixture()
		recipe.SourceAvailable = available
		recipe.Environment.PackageKeys = []string{"python"}
		recipe.Environment.ToolKeys = []string{"git"}
		recipe.Environment.Dockerfile = ""
		build := &controlplanev1.ImageBuild{SourceAvailable: available}
		if available {
			recipe.Environment.Dockerfile = "FROM scratch"
			recipe.Environment.InstallationBlock = "RUN true"
			build.Dockerfile = "FROM scratch"
		}
		w := httptest.NewRecorder()
		roleImageHTTPHandler(&catalogRPCRecorder{response: &controlplanev1.GetRoleImageRecipeResponse{Recipe: recipe, Builds: []*controlplanev1.ImageBuild{build}}}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes/imgrec_fixture01", nil))
		var detail generated.RoleImageRecipeDetail
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Recipe.SourceAvailable != available || detail.Builds[0].SourceAvailable != available || detail.Recipe.Environment.PackageKeys == nil {
			t.Fatal("metadata source admission was lost")
		}
		if available != (detail.Recipe.Environment.Dockerfile != nil) || available != (detail.Builds[0].Dockerfile != nil) || available != (detail.Recipe.Environment.InstallationBlock != nil) {
			t.Fatal("source redaction changed metadata or exposed source")
		}
	}
	for _, field := range []string{"dockerfile", "installationBlock", "build"} {
		t.Run(field, func(t *testing.T) {
			recipe := roleImageRecipeFixture()
			recipe.SourceAvailable = false
			recipe.Environment.Dockerfile = ""
			build := &controlplanev1.ImageBuild{}
			switch field {
			case "dockerfile":
				recipe.Environment.Dockerfile = "private-source-marker"
			case "installationBlock":
				recipe.Environment.InstallationBlock = "private-source-marker"
			case "build":
				build.Dockerfile = "private-source-marker"
			}
			w := httptest.NewRecorder()
			roleImageHTTPHandler(&catalogRPCRecorder{response: &controlplanev1.GetRoleImageRecipeResponse{Recipe: recipe, Builds: []*controlplanev1.ImageBuild{build}}}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes/imgrec_fixture01", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "private-source-marker") {
				t.Fatal("contradictory source admission escaped detail")
			}
			w = httptest.NewRecorder()
			if validRoleImageReceipt(w, &controlplanev1.ManageRoleImageRecipeResponse{Recipe: recipe, ImageBuild: build}, recipe.ProjectRef, recipe.Ref) || w.Code != 502 {
				t.Fatal("contradictory source admission escaped mutation receipt")
			}
			if field != "build" {
				w = httptest.NewRecorder()
				roleImageHTTPHandler(&catalogRPCRecorder{response: &controlplanev1.ListRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{recipe}, Total: 1}}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes", nil))
				if w.Code != 502 || strings.Contains(w.Body.String(), "private-source-marker") {
					t.Fatal("contradictory source admission escaped list")
				}
			}
		})
	}
}

func TestRoleImageCatalogPreservesServerFilterTotalAndLineage(t *testing.T) {
	response := &controlplanev1.ListRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{roleImageRecipeFixture()}, Total: 21, Page: &controlplanev1.PageInfo{NextPageToken: "owner-next"}}
	client := &catalogRPCRecorder{response: response}
	request := httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes?query="+url.QueryEscape("Окружение")+"&state=ACTIVE&roleDefinitionRef=role_fixture01&pageSize=20&pageToken=owner-before", nil)
	w := httptest.NewRecorder()
	roleImageHTTPHandler(client).ServeHTTP(w, request)
	if w.Code != 200 || client.method != controlplanev1.RoleImageService_ListRoleImageRecipes_FullMethodName {
		t.Fatalf("role image route: %d", w.Code)
	}
	r := client.request.(*controlplanev1.ListRoleImageRecipesRequest)
	if r.Query != "Окружение" || r.State != "ACTIVE" || r.RoleDefinitionRef != "role_fixture01" || r.ProjectRef != "prj_fixture01" || r.Page.PageSize != 20 || r.Page.PageToken != "owner-before" {
		t.Fatal("owner filter or pagination changed")
	}
	var result generated.RoleImageRecipePage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Total != 21 || len(result.Items) != 1 || result.Items[0].ManagedLineage == nil || *result.Items[0].ManagedLineage.Revision != 4 || *result.NextPageToken != "owner-next" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("lineage or authoritative count lost")
	}
	response.Recipes, response.Total, response.Page = nil, 0, nil
	w = httptest.NewRecorder()
	roleImageHTTPHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("empty owner page lost required zero fields")
	}
}

func TestRoleImageCatalogRejectsInvalidInputAndUpstream(t *testing.T) {
	path := "/api/v1/projects/prj_fixture01/role-image-recipes"
	for _, query := range []string{"state=UNKNOWN", "pageSize=0", "pageSize=101", "query=" + strings.Repeat("a", 129), "query=" + url.QueryEscape(strings.Repeat("я", 65)), "roleDefinitionRef=bad!", "pageToken=" + strings.Repeat("a", 513)} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		roleImageHTTPHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path+"?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid input reached owner: %d", w.Code)
		}
	}
	for name, mutate := range map[string]func(*controlplanev1.ListRoleImageRecipesResponse){
		"negative total":  func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = -1 },
		"inexact total":   func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = maximumSafeJSONInteger + 1 },
		"short total":     func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = 0 },
		"foreign project": func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].ProjectRef = "prj_foreign01" },
		"duplicate":       func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes = append(r.Recipes, r.Recipes[0]) },
		"unknown provenance": func(r *controlplanev1.ListRoleImageRecipesResponse) {
			r.Recipes[0].ManagedLineage.ManagedBy = "private-unknown"
		},
		"partial revision": func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].ManagedLineage.RevisionRef = "" },
		"oversized cursor": func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Page.NextPageToken = strings.Repeat("a", 513) },
		"repeated cursor":  func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Page.NextPageToken = "before" },
	} {
		t.Run(name, func(t *testing.T) {
			response := &controlplanev1.ListRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{roleImageRecipeFixture()}, Total: 9, Page: &controlplanev1.PageInfo{NextPageToken: "after"}}
			mutate(response)
			w := httptest.NewRecorder()
			roleImageHTTPHandler(&catalogRPCRecorder{response: response}).ServeHTTP(w, httptest.NewRequest("GET", path+"?pageToken=before", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "private-unknown") {
				t.Fatalf("corrupt owner response accepted: %d", w.Code)
			}
		})
	}
}

func TestRoleImageDetailAndReceiptPreserveBuildRevision(t *testing.T) {
	recipe := roleImageRecipeFixture()
	build := &controlplanev1.ImageBuild{ConfigurationRevisionRef: recipe.ManagedLineage.RevisionRef}
	response := &controlplanev1.GetRoleImageRecipeResponse{Recipe: recipe, Builds: []*controlplanev1.ImageBuild{build}}
	w := httptest.NewRecorder()
	roleImageHTTPHandler(&catalogRPCRecorder{response: response}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes/imgrec_fixture01", nil))
	var detail generated.RoleImageRecipeDetail
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Recipe.ManagedLineage == nil || len(detail.Builds) != 1 || *detail.Builds[0].ConfigurationRevisionRef != recipe.ManagedLineage.RevisionRef {
		t.Fatal("build revision or recipe lineage lost")
	}
	for _, owner := range []string{"UI", "GIT", "SHIPPED"} {
		lineage := recipe.ManagedLineage
		lineage.ManagedBy = owner
		if owner == "SHIPPED" {
			lineage.Origin, lineage.ConfigurationRef, lineage.RevisionRef, lineage.Revision = "BASELINE", "", "", 0
		}
		if !validRoleImageLineage(lineage) || publicRoleImageRecipe(recipe).ManagedLineage == nil {
			t.Fatalf("valid provenance rejected: %s", owner)
		}
	}
	if !validRoleImageLineage(nil) {
		t.Fatal("legacy absence is not a malformed tuple")
	}
	for _, invalid := range []string{"project", "recipe", "lineage", "build"} {
		receipt := &controlplanev1.ManageRoleImageRecipeResponse{Recipe: roleImageRecipeFixture()}
		switch invalid {
		case "project":
			receipt.Recipe.ProjectRef = "prj_foreign01"
		case "recipe":
			receipt.Recipe.Ref = "imgrec_foreign01"
		case "lineage":
			receipt.Recipe.ManagedLineage.Revision = 0
		case "build":
			receipt.ImageBuild = &controlplanev1.ImageBuild{ConfigurationRevisionRef: "private!"}
		}
		w := httptest.NewRecorder()
		if validRoleImageReceipt(w, receipt, "prj_fixture01", "imgrec_fixture01") || w.Code != 502 {
			t.Fatal("invalid mutation receipt accepted")
		}
	}
}

func TestRoleImageCatalogPreservesOwnerDenials(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.NotFound: 404, codes.PermissionDenied: 403, codes.InvalidArgument: 400, codes.Unavailable: 503} {
		w := httptest.NewRecorder()
		roleImageHTTPHandler(&catalogRPCRecorder{failure: status.Error(code, "private detail")}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/role-image-recipes", nil))
		if w.Code != expected || strings.Contains(w.Body.String(), "private detail") {
			t.Fatalf("owner denial mapping: %d", w.Code)
		}
	}
}

func TestPublicRoleImageArtifactPreservesPromotionIdentity(t *testing.T) {
	t.Parallel()
	provenance := strings.Repeat("a", 64)
	artifact := publicRoleImageArtifact(&controlplanev1.ImageArtifact{
		Ref: "imgart_12345678", Version: 1, RecipeRef: "imgrec_12345678",
		RecipeGeneration: 2, ManifestDigest: "sha256:" + strings.Repeat("b", 64),
		ProvenanceSha256: provenance,
		AdmissionVerdict: controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_ACCEPTED,
	})

	if artifact.ProvenanceSha256 != provenance || artifact.AdmissionVerdict != "ACCEPTED" {
		t.Fatalf("promotion identity was not preserved: %#v", artifact)
	}
}
