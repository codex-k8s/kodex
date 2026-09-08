package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Те же public JSON scopes проверяются отдельно через generated BFF decoder/caster.
// Здесь реальные owner transactions и PostgreSQL, без materialized locator fixture.
func testPublicAccessQueryScopes(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Access query owner", CallerWorkload: "control-api-gateway", Operation: "platform.query.bootstrap"}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	createProject := func(key string) entity.Project {
		result, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.ProjectInput{Name: key, Language: "en"}})
		if err != nil || result.Project == nil {
			t.Fatalf("create query project: %v", err)
		}
		return *result.Project
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	project, hidden := createProject("access-query-project"), createProject("access-query-hidden")
	candidateInput := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000009330", ExternalTenantID: ownerInput.ExternalTenantID, ExternalDisplayName: "Public query viewer", CallerWorkload: "control-api-gateway", Operation: ownerInput.Operation}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound principal accepted: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: candidateInput.ExternalDisplayName, Page: query.Page{Size: 10}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resolve query viewer: %v", err)
	}
	viewerRole := createRoleImageAccessRole(t, ctx, service, owner, "query-viewer-role", "Query viewer", []string{"project.view"}, []string{"PROJECT"})
	createRoleImageAccessBinding(t, ctx, service, owner, "query-viewer-binding", subjects[0].Ref, viewerRole.CurrentVersion.Ref, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	viewer := resolvedTestPrincipal(t, ctx, repository, candidateInput, "control-api-gateway")
	read := func(principal value.Principal, body string) (entity.EffectiveAccess, error) {
		var request struct {
			SubjectRef     string
			Target         entity.AccessScope
			PermissionKeys []string
		}
		decoder := json.NewDecoder(strings.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			t.Fatalf("decode public fixture: %v", err)
		}
		return service.QueryEffectiveAccess(ctx, principal, request.SubjectRef, request.Target, request.PermissionKeys, time.Time{})
	}
	for _, scope := range []struct {
		name, target string
		allowed      []bool
	}{
		{"organization", `{"kind":"ORGANIZATION"}`, []bool{false, true, true}},
		{"project", fmt.Sprintf(`{"kind":"PROJECT","projectRef":%q}`, project.Ref), []bool{true, true, true}},
		{"resource kind", fmt.Sprintf(`{"kind":"RESOURCE_KIND","projectRef":%q,"resourceKind":"ROLE_IMAGE"}`, project.Ref), []bool{true, true, true}},
		{"resource instance", fmt.Sprintf(`{"kind":"RESOURCE_INSTANCE","projectRef":%q,"resourceKind":"PROJECT","resourceRef":%q}`, project.Ref, project.Ref), []bool{true, true, true}},
	} {
		t.Run(scope.name, func(t *testing.T) {
			body := `{"target":` + scope.target + `,"permissionKeys":["image.build","image.source.view","image.source.manage"]}`
			result, err := read(owner, body)
			if err != nil || len(result.Decisions) != 3 {
				t.Fatalf("public owner query: %v", err)
			}
			for i, want := range scope.allowed {
				if result.Decisions[i].Allowed != want {
					t.Fatalf("permission decision %d: actual=%v expected=%v", i, result.Decisions[i].Allowed, want)
				}
			}
			denied, err := read(viewer, body)
			if err != nil || len(denied.Decisions) != 3 {
				t.Fatalf("public viewer query: %v", err)
			}
			for _, decision := range denied.Decisions {
				if decision.Allowed {
					t.Fatal("viewer gained image permission")
				}
			}
			// ExplainAccess RPC вызывает тот же query с одной permission, без другого resolver.
			explained, err := read(owner, `{"target":`+scope.target+`,"permissionKeys":["image.source.view"]}`)
			if err != nil || len(explained.Decisions) != 1 || !explained.Decisions[0].Allowed {
				t.Fatalf("single explanation decision: %v", err)
			}
			var target entity.AccessScope
			if err := json.Unmarshal([]byte(scope.target), &target); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Decisions[0].Target, target) || !reflect.DeepEqual(explained.Decisions[0].Target, target) {
				t.Fatal("public response scope changed")
			}
			simulation := command.AccessSimulationInput{SubjectRef: subjects[0].Ref, PermissionKey: "image.source.view", Target: target, Role: command.AccessRoleInput{PermissionKeys: []string{"image.source.view"}, AllowedScopes: []string{"ORGANIZATION"}}, Binding: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: "simulation", Scope: entity.AccessScope{Kind: "ORGANIZATION"}}}
			simulated, err := service.SimulateAccess(ctx, owner, simulation)
			if err != nil || simulated.Current.Allowed || !simulated.Simulated.Allowed || !reflect.DeepEqual(simulated.Current.Target, target) || !reflect.DeepEqual(simulated.Simulated.Target, target) {
				t.Fatalf("simulation differs from query: current=%v simulated=%v err=%v", simulated.Current.Allowed, simulated.Simulated.Allowed, err)
			}
			if _, err := service.SimulateAccess(ctx, viewer, simulation); !errors.Is(err, domainerrs.ErrNotFound) {
				t.Fatalf("viewer simulated another grant: %v", err)
			}
			after, err := read(viewer, body)
			if err != nil {
				t.Fatal(err)
			}
			for _, decision := range after.Decisions {
				if decision.Allowed {
					t.Fatal("simulation persisted authority")
				}
			}
		})
	}
	t.Run("foreign tenant authority", func(t *testing.T) {
		foreign := viewer
		foreign.AuthorityTenant = "20000000-0000-4000-8000-000000009999"
		if _, err := read(foreign, `{"target":{"kind":"PROJECT","projectRef":`+fmt.Sprintf("%q", project.Ref)+`},"permissionKeys":["project.view"]}`); !errors.Is(err, domainerrs.ErrForbidden) {
			t.Fatalf("foreign tenant authority accepted: %v", err)
		}
	})

	for _, check := range []struct {
		name, body string
		want       error
	}{
		{"unknown kind", `{"target":{"kind":"UNKNOWN"},"permissionKeys":["project.view"]}`, domainerrs.ErrInvalid},
		{"organization contradictory project", fmt.Sprintf(`{"target":{"kind":"ORGANIZATION","projectRef":%q},"permissionKeys":["project.view"]}`, project.Ref), domainerrs.ErrInvalid},
		{"project contradictory resource", fmt.Sprintf(`{"target":{"kind":"PROJECT","projectRef":%q,"resourceKind":"PROJECT"},"permissionKeys":["project.view"]}`, project.Ref), domainerrs.ErrInvalid},
		{"hidden project", fmt.Sprintf(`{"target":{"kind":"PROJECT","projectRef":%q},"permissionKeys":["project.view"]}`, hidden.Ref), domainerrs.ErrNotFound},
		{"hidden resource kind", fmt.Sprintf(`{"target":{"kind":"RESOURCE_KIND","projectRef":%q,"resourceKind":"ROLE_IMAGE"},"permissionKeys":["image.source.view"]}`, hidden.Ref), domainerrs.ErrNotFound},
		{"foreign project", `{"target":{"kind":"PROJECT","projectRef":"prj_foreign"},"permissionKeys":["project.view"]}`, domainerrs.ErrNotFound},
		{"foreign organization", `{"target":{"kind":"ORGANIZATION","resourceRef":"org_foreign"},"permissionKeys":["project.view"]}`, domainerrs.ErrNotFound},
		{"unknown permission", `{"target":{"kind":"ORGANIZATION"},"permissionKeys":["unknown.permission"]}`, domainerrs.ErrInvalid},
		{"other subject", fmt.Sprintf(`{"subjectRef":%q,"target":{"kind":"ORGANIZATION"},"permissionKeys":["project.view"]}`, ownerScope.actorRef), domainerrs.ErrNotFound},
	} {
		t.Run(check.name, func(t *testing.T) {
			if _, err := read(viewer, check.body); !errors.Is(err, check.want) {
				t.Fatalf("public negative: actual=%v expected=%v", err, check.want)
			}
		})
	}
}
