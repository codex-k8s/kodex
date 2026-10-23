package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// roleImageRecipePageFixture returns fresh deterministic owner metadata with a valid source-visibility tuple; organization scope removes the project binding. It contains no promoted artifact or admission assertion.
// roleImageRecipePageFixture возвращает новые детерминированные metadata владельца с согласованной видимостью source; организационная область убирает привязку проекта. Допущенного артефакта и утверждения о допуске здесь нет.
func roleImageRecipePageFixture(organization bool) *controlplanev1.RoleImageRecipe {
	recipe := &controlplanev1.RoleImageRecipe{
		Ref: "imgrec_fixture01", Version: 7, Name: "Recipe", State: "ACTIVE",
		ProjectRef: "prj_fixture01", OrganizationRef: "org_fixture01",
		ScopeKind:   controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT,
		Environment: &controlplanev1.RoleEnvironmentSelection{EnvironmentKey: "system-base"},
	}
	if organization {
		recipe.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		recipe.ProjectRef = ""
	}
	return recipe
}

// TestWriteRoleImageRecipePagePreservesValidation exercises both concrete reply types, typed nil, count/item/source/lineage/cursor failures and successful allocation. It checks exact public errors, no partial items or input mutation, and the specialized scope predicate's original short-circuit order without live RPC or image admission.
// TestWriteRoleImageRecipePagePreservesValidation проверяет оба конкретных ответа, типизированный nil, ошибки счётчиков, элементов, source, lineage, cursor и успешное выделение страницы. Сверяет точные публичные ошибки, отсутствие частичных элементов и изменений входа, исходный порядок короткого замыкания предиката области без живого RPC и допуска образа.
func TestWriteRoleImageRecipePagePreservesValidation(t *testing.T) {
	t.Parallel()
	for _, organization := range []bool{false, true} {
		for _, test := range []struct {
			name      string
			change    func(*controlplanev1.ListRoleImageRecipesResponse)
			pageSize  int
			pageToken string
			nilReply  bool
			wantOK    bool
			wantCalls int
		}{
			{name: "valid", wantOK: true, wantCalls: 1},
			{name: "typed nil", nilReply: true},
			{name: "empty page", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes = nil; r.Total = 0 }, wantOK: true},
			{name: "maximum safe total", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = maximumSafeJSONInteger }, wantOK: true, wantCalls: 1},
			{name: "exact size", pageSize: 1, wantOK: true, wantCalls: 1},
			{name: "negative total", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = -1 }},
			{name: "total below item count", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = 0 }},
			{name: "unsafe total", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Total = maximumSafeJSONInteger + 1 }},
			{name: "hard item limit", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes = make([]*controlplanev1.RoleImageRecipe, 101)
				r.Total = 101
			}},
			{name: "requested item limit", pageSize: 1, change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes = append(r.Recipes, proto.Clone(r.Recipes[0]).(*controlplanev1.RoleImageRecipe))
				r.Total = 2
			}},
			{name: "nil item", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0] = nil }},
			{name: "duplicate item", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes = append(r.Recipes, proto.Clone(r.Recipes[0]).(*controlplanev1.RoleImageRecipe))
				r.Total = 2
			}, wantCalls: 2},
			{name: "foreign project binding", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].ProjectRef = "prj_foreign01" }, wantCalls: 1},
			{name: "invalid reference", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].Ref = "invalid/ref" }, wantCalls: 1},
			{name: "invalid lineage", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes[0].ManagedLineage = &controlplanev1.RoleImageManagedLineage{ManagedBy: "UNKNOWN"}
			}, wantCalls: 1},
			{name: "missing environment", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].Environment = nil }, wantCalls: 1},
			{name: "missing available source", change: func(r *controlplanev1.ListRoleImageRecipesResponse) { r.Recipes[0].SourceAvailable = true }, wantCalls: 1},
			{name: "hidden Dockerfile", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes[0].Environment.Dockerfile = "private-source-marker"
			}, wantCalls: 1},
			{name: "hidden installation block", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes[0].Environment.InstallationBlock = "private-source-marker"
			}, wantCalls: 1},
			{name: "available source", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes[0].SourceAvailable = true
				r.Recipes[0].Environment.Dockerfile = "FROM scratch"
			}, wantOK: true, wantCalls: 1},
			{name: "advancing cursor", pageToken: "previous", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Page = &controlplanev1.PageInfo{NextPageToken: "next"}
			}, wantOK: true, wantCalls: 1},
			{name: "repeated cursor", pageToken: "next", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Page = &controlplanev1.PageInfo{NextPageToken: "next"}
			}, wantCalls: 1},
			{name: "oversized cursor", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Page = &controlplanev1.PageInfo{NextPageToken: strings.Repeat("x", 513)}
			}, wantCalls: 1},
			{name: "malformed cursor", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Page = &controlplanev1.PageInfo{NextPageToken: string([]byte{0xff})}
			}, wantCalls: 1},
			{name: "control in cursor", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Page = &controlplanev1.PageInfo{NextPageToken: "next\x00token"}
			}, wantCalls: 1},
			{name: "empty continuing page", change: func(r *controlplanev1.ListRoleImageRecipesResponse) {
				r.Recipes = nil
				r.Total = 0
				r.Page = &controlplanev1.PageInfo{NextPageToken: "next"}
			}},
		} {
			t.Run(fmt.Sprintf("organization=%t/%s", organization, test.name), func(t *testing.T) {
				response := &controlplanev1.ListRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{roleImageRecipePageFixture(organization)}, Total: 1}
				if test.change != nil {
					test.change(response)
				}
				before := proto.Clone(response)
				var pageSize *int
				if test.pageSize != 0 {
					size := test.pageSize
					pageSize = &size
				}
				var pageToken *string
				if test.pageToken != "" {
					token := test.pageToken
					pageToken = &token
				}
				calls := 0
				inScope := func(recipe *controlplanev1.RoleImageRecipe) bool {
					calls++
					if organization {
						return validOrganizationRoleImageRecipe(recipe)
					}
					return recipe.GetProjectRef() == "prj_fixture01"
				}
				writer := httptest.NewRecorder()
				if organization {
					var reply *controlplanev1.ListOrganizationRoleImageRecipesResponse
					if !test.nilReply {
						reply = &controlplanev1.ListOrganizationRoleImageRecipesResponse{Recipes: response.Recipes, Total: response.Total, Page: response.Page}
					}
					writeRoleImageRecipePage(writer, reply, pageSize, pageToken, inScope)
				} else {
					reply := response
					if test.nilReply {
						reply = nil
					}
					writeRoleImageRecipePage(writer, reply, pageSize, pageToken, inScope)
				}
				if calls != test.wantCalls || !proto.Equal(response, before) {
					t.Fatalf("scope calls=%d want=%d or input mutated", calls, test.wantCalls)
				}
				if !test.wantOK {
					expected := httptest.NewRecorder()
					writeLocalProblem(expected, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
					assertReadRPCProjection(t, writer, expected)
					if strings.Contains(writer.Body.String(), "private-source-marker") || strings.Contains(writer.Body.String(), "imgrec_fixture01") {
						t.Fatal("invalid page leaked partial item")
					}
					return
				}
				var result generated.RoleImageRecipePage
				if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &result) != nil || result.Items == nil || len(result.Items) != len(response.Recipes) || result.Total != response.Total {
					t.Fatalf("invalid public page: %d %s", writer.Code, writer.Body.String())
				}
				for index, item := range result.Items {
					if item.Ref != response.Recipes[index].Ref || item.Version != 7 || item.PromotedImageReady {
						t.Fatal("recipe mapping changed or invented admission")
					}
				}
				next := response.GetPage().GetNextPageToken()
				if next == "" && result.NextPageToken != nil || next != "" && (result.NextPageToken == nil || *result.NextPageToken != next) {
					t.Fatal("continuation mapping changed")
				}
			})
		}
	}
}

// TestWriteRoleImageRecipePageRetainsCallerScope keeps project and organization eligibility distinct and checks unique-item order across independent responses. The helper must not silently add organization checks to the existing project predicate or retain seen refs between requests.
// TestWriteRoleImageRecipePageRetainsCallerScope сохраняет различие проектной и организационной проверок и сверяет порядок уникальных элементов в независимых ответах. Helper не должен незаметно добавлять организационные условия к прежнему проектному предикату или сохранять увиденные ссылки между запросами.
func TestWriteRoleImageRecipePageRetainsCallerScope(t *testing.T) {
	t.Parallel()
	for _, organization := range []bool{false, true} {
		for attempt := 0; attempt < 2; attempt++ {
			recipes := []*controlplanev1.RoleImageRecipe{roleImageRecipePageFixture(organization), roleImageRecipePageFixture(organization)}
			recipes[1].Ref = "imgrec_fixture02"
			writer := httptest.NewRecorder()
			if organization {
				writeRoleImageRecipePage(writer, &controlplanev1.ListOrganizationRoleImageRecipesResponse{Recipes: recipes, Total: 2}, nil, nil, validOrganizationRoleImageRecipe)
			} else {
				recipes[0].ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_UNSPECIFIED
				recipes[0].OrganizationRef = ""
				writeRoleImageRecipePage(writer, &controlplanev1.ListRoleImageRecipesResponse{Recipes: recipes, Total: 2}, nil, nil, func(recipe *controlplanev1.RoleImageRecipe) bool { return recipe.GetProjectRef() == "prj_fixture01" })
			}
			var result generated.RoleImageRecipePage
			if writer.Code != 200 || json.Unmarshal(writer.Body.Bytes(), &result) != nil || len(result.Items) != 2 || result.Items[0].Ref != recipes[0].Ref || result.Items[1].Ref != recipes[1].Ref {
				t.Fatalf("scope or order changed: %d %s", writer.Code, writer.Body.String())
			}
		}
	}
	for _, change := range []func(*controlplanev1.RoleImageRecipe){
		func(recipe *controlplanev1.RoleImageRecipe) {
			recipe.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
		},
		func(recipe *controlplanev1.RoleImageRecipe) { recipe.OrganizationRef = "invalid/ref" },
	} {
		recipe := roleImageRecipePageFixture(true)
		change(recipe)
		writer := httptest.NewRecorder()
		writeRoleImageRecipePage(writer, &controlplanev1.ListOrganizationRoleImageRecipesResponse{Recipes: []*controlplanev1.RoleImageRecipe{recipe}, Total: 1}, nil, nil, validOrganizationRoleImageRecipe)
		if writer.Code != 502 {
			t.Fatal("organization scope predicate was bypassed")
		}
	}
}
