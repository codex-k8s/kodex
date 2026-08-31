package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRoleImageApplicationAccess(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Role image owner", CallerWorkload: "control-api-gateway", Operation: "platform.role-images.recipes.manage",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	roleImageOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve role image owner principal: %v", err)
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	projectResult, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "role-image-access-project"},
		Payload:  command.ProjectInput{Name: "Role image application access", Language: "en"}})
	if err != nil || projectResult.Project == nil {
		t.Fatalf("create role image project: project=%#v err=%v", projectResult.Project, err)
	}
	project := *projectResult.Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "role-image-access-agent", "Role image specialist")

	created, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageOwner, Action: "CREATE", ProjectRef: project.Ref, RoleDefinitionRef: agent.RoleDefinitionRef,
		Name: "Application RBAC image", Mutation: roleImageTestMutation("role-image-access-create", "CREATE", nil),
	})
	if err != nil || created.Recipe.Ref == "" || created.Build == nil {
		t.Fatalf("owner create role image: result=%#v err=%v", created, err)
	}
	worker := roleImageOwner
	worker.CallerWorkload = "role-image-builder"
	worker.Permission = "platform.role-images.builds.claim"
	worker.CorrelationRef = "role-image-access-build-claim"
	claimed, err := repository.ClaimBuild(ctx, worker, "role-image-access-build-claim")
	if err != nil || claimed.Build.Ref != created.Build.Ref || claimed.Build.Stage != "MATERIALIZATION" ||
		claimed.Input.RecipeRef != created.Recipe.Ref || claimed.Input.SpecSHA256 != created.Recipe.SpecSHA256 {
		t.Fatalf("claim created role image build: created=%#v claim=%#v err=%v", created.Build, claimed, err)
	}

	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009912", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Role image viewer", CallerWorkload: "control-api-gateway", Operation: "platform.role-images.recipes.list",
	}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound role image candidate received authority: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: candidateInput.ExternalDisplayName, Page: query.Page{Size: 20}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resolve role image candidate subject: subjects=%#v err=%v", subjects, err)
	}
	viewerRole := createRoleImageAccessRole(t, ctx, service, owner, "role-image-viewer-role", "Role image viewer", []string{"project.view"}, []string{"PROJECT"})
	createRoleImageAccessBinding(t, ctx, service, owner, "role-image-viewer-binding", subjects[0].Ref, viewerRole.CurrentVersion.Ref,
		entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	authority, err := repository.ResolveProofAuthority(ctx, candidateInput)
	if err != nil {
		t.Fatalf("resolve bound role image candidate: %v", err)
	}
	candidate := value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID,
		Permission: candidateInput.Operation, CorrelationRef: "role-image-access-candidate", CallerWorkload: "control-api-gateway", CredentialRevision: 1}
	roleImageCandidate, err := repository.ResolvePrincipal(ctx, candidate)
	if err != nil {
		t.Fatalf("resolve role image candidate principal: %v", err)
	}

	items, _, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Page: query.Page{Size: 20}})
	var listed *entity.RoleImageRecipe
	for index := range items {
		if !sameStrings(items[index].NextActions, []string{"OPEN"}) {
			t.Fatalf("project viewer received mutation actions: item=%#v", items[index])
		}
		if items[index].Ref == created.Recipe.Ref {
			listed = &items[index]
		}
	}
	if err != nil || listed == nil {
		t.Fatalf("project viewer list mismatch: items=%#v err=%v", items, err)
	}
	if _, _, _, err := repository.Get(ctx, roleImageCandidate, created.Recipe.Ref); err != nil {
		t.Fatalf("project viewer cannot read exact role image: %v", err)
	}
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "CREATE", ProjectRef: project.Ref, RoleDefinitionRef: agent.RoleDefinitionRef,
		Name: "Denied image", Mutation: roleImageTestMutation("role-image-access-denied-create", "CREATE", nil),
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("viewer created role image without image.build: %v", err)
	}

	builderRole := createRoleImageAccessRole(t, ctx, service, owner, "role-image-builder-role", "Exact role image builder", []string{"image.build"}, []string{"RESOURCE_INSTANCE"})
	createRoleImageAccessBinding(t, ctx, service, owner, "role-image-builder-binding", subjects[0].Ref, builderRole.CurrentVersion.Ref,
		entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "ROLE_IMAGE", ResourceRef: created.Recipe.Ref})

	current, _, _, err := repository.Get(ctx, roleImageCandidate, created.Recipe.Ref)
	if err != nil || !containsString(current.NextActions, "UPDATE") || !containsString(current.NextActions, "REQUEST_BUILD") {
		t.Fatalf("exact builder actions mismatch: recipe=%#v err=%v", current, err)
	}
	wrongProjectVersion := int64(current.Version)
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "UPDATE", ProjectRef: "prj_hidden", RecipeRef: current.Ref,
		Name: "Hidden project", Mutation: roleImageTestMutation("role-image-access-hidden-project", "UPDATE", &wrongProjectVersion),
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("payload projectRef was trusted for exact role image: %v", err)
	}

	updated, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "UPDATE", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Name: "Updated application RBAC image", Mutation: roleImageTestMutation("role-image-access-update", "UPDATE", &wrongProjectVersion),
	})
	if err != nil || updated.Recipe.Version <= current.Version {
		t.Fatalf("exact builder update failed: result=%#v err=%v", updated, err)
	}
	archiveVersion := int64(updated.Recipe.Version)
	archived, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "ARCHIVE", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Mutation: roleImageTestMutation("role-image-access-archive", "ARCHIVE", &archiveVersion),
	})
	if err != nil || archived.Recipe.State != "ARCHIVED" {
		t.Fatalf("exact builder archive failed: result=%#v err=%v", archived, err)
	}
	restoreVersion := int64(archived.Recipe.Version)
	restored, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "RESTORE", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Mutation: roleImageTestMutation("role-image-access-restore", "RESTORE", &restoreVersion),
	})
	if err != nil || restored.Recipe.State != "ACTIVE" {
		t.Fatalf("exact builder restore failed: result=%#v err=%v", restored, err)
	}
	buildVersion := int64(restored.Recipe.Version)
	requested, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "REQUEST_BUILD", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Mutation: roleImageTestMutation("role-image-access-request-build", "REQUEST_BUILD", &buildVersion),
	})
	if err != nil || requested.Build == nil {
		t.Fatalf("exact builder request build failed: result=%#v err=%v", requested, err)
	}
}

func roleImageTestMutation(key, action string, expectedVersion *int64) value.Mutation {
	return value.Mutation{
		Operation: "role-image-recipe." + strings.ToLower(action), IdempotencyKey: key,
		ExpectedVersion: expectedVersion, IntentDigest: strings.Repeat("f", 64),
	}
}

func createRoleImageAccessRole(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, key, name string, permissions, scopes []string) entity.AccessRole {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AccessRoleInput{
			Name: name, PermissionKeys: permissions, AllowedScopes: scopes, ChangeComment: "role image application RBAC component scenario",
		}})
	if err != nil || result.AccessRole == nil {
		t.Fatalf("create %s: role=%#v err=%v", key, result.AccessRole, err)
	}
	return *result.AccessRole
}

func createRoleImageAccessBinding(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, key, subjectRef, roleVersionRef string, scope entity.AccessScope) {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjectRef, RoleVersionRef: roleVersionRef, Scope: scope,
		}})
	if err != nil || result.AccessBinding == nil {
		t.Fatalf("create %s: binding=%#v err=%v", key, result.AccessBinding, err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
