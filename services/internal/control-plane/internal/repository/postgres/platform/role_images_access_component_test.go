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
		Recipe: entity.RoleImageRecipeInput{Dockerfile: "FROM scratch\n# source boundary fixture\n", InstallationBlock: "# private installation fixture"},
	})
	if err != nil || created.Recipe.Ref == "" || created.Build == nil {
		t.Fatalf("owner create role image: result=%#v err=%v", created, err)
	}
	foreignProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "role-image-signed-other-project"}, Payload: command.ProjectInput{Name: "Other signed scope", Language: "en"}})
	if err != nil || foreignProject.Project == nil {
		t.Fatalf("create other signed project: %v", err)
	}
	foreignScope := roleImageOwner
	if err := repository.pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE ref=$1`, foreignProject.Project.Ref).Scan(&foreignScope.ProjectRef); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, foreignScope, created.Recipe.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("signed foreign project read source through owner role: %v", err)
	}
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: foreignScope, Action: "CREATE", ProjectRef: project.Ref, RoleDefinitionRef: agent.RoleDefinitionRef,
		Name: "Application RBAC image", Mutation: roleImageTestMutation("role-image-access-create", "CREATE", nil),
		Recipe: entity.RoleImageRecipeInput{Dockerfile: "FROM scratch\n# source boundary fixture\n", InstallationBlock: "# private installation fixture"},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("signed foreign project replayed owner source receipt: %v", err)
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

	items, _, total, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Page: query.Page{Size: 20}})
	if err != nil || total != int64(len(items)) {
		t.Fatalf("role image list count mismatch: %v", err)
	}
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
	if listed.SourceAvailable || listed.Input.Dockerfile != "" || listed.Input.InstallationBlock != "" {
		t.Fatal("metadata viewer received recipe source")
	}
	filtered, _, filteredTotal, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Query: "Application RBAC", State: "ACTIVE", Page: query.Page{Size: 1}})
	if err != nil || filteredTotal != 1 || len(filtered) != 1 || filtered[0].Ref != created.Recipe.Ref || filtered[0].ManagedLineage == nil {
		t.Fatalf("filtered recipe lineage/count mismatch: %v", err)
	}
	literal, _, literalTotal, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Query: "%", Page: query.Page{Size: 1}})
	if err != nil || literalTotal != 0 || len(literal) != 0 {
		t.Fatalf("recipe query treated wildcard as pattern: %v", err)
	}
	seen, token := map[string]bool{}, ""
	for {
		page, next, count, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Page: query.Page{Size: 1, Token: token}})
		if err != nil || count != total || len(page) != 1 || seen[page[0].Ref] {
			t.Fatalf("recipe pagination mismatch: %v", err)
		}
		seen[page[0].Ref] = true
		if next == "" {
			break
		}
		if _, _, _, err := repository.List(ctx, roleImageCandidate, roleimagerepo.Filter{ProjectRef: project.Ref, Query: "changed", Page: query.Page{Size: 1, Token: next}}); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("recipe cursor escaped query: %v", err)
		}
		if _, _, _, err := repository.List(ctx, roleImageOwner, roleimagerepo.Filter{ProjectRef: project.Ref, Page: query.Page{Size: 1, Token: next}}); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("recipe cursor escaped actor: %v", err)
		}
		token = next
		if int64(len(seen)) >= total {
			t.Fatal("recipe cursor did not terminate")
		}
	}
	if int64(len(seen)) != total {
		t.Fatal("recipe pagination omitted visible items")
	}
	viewerDetail, err := repository.Get(ctx, roleImageCandidate, created.Recipe.Ref)
	if err != nil {
		t.Fatalf("project viewer cannot read exact role image: %v", err)
	}
	if viewerDetail.Recipe.SourceAvailable || viewerDetail.Recipe.Input.Dockerfile != "" || viewerDetail.Recipe.Input.InstallationBlock != "" {
		t.Fatal("metadata detail exposed recipe source")
	}
	for _, build := range viewerDetail.Builds {
		if build.SourceAvailable || build.Dockerfile != "" {
			t.Fatal("metadata detail exposed build source")
		}
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

	detail, err := repository.Get(ctx, roleImageCandidate, created.Recipe.Ref)
	current := detail.Recipe
	if err != nil || containsString(current.NextActions, "UPDATE") || !containsString(current.NextActions, "REQUEST_BUILD") || current.SourceAvailable {
		t.Fatalf("exact builder actions mismatch: recipe=%#v err=%v", current, err)
	}
	deniedVersion := int64(current.Version)
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "UPDATE", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Name: "Denied source change", Mutation: roleImageTestMutation("role-image-source-denied-update", "UPDATE", &deniedVersion),
	}); !errors.Is(err, domainerrs.ErrNotFound) && !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("image.build allowed source mutation without source authority: %v", err)
	}
	sourceRole := createRoleImageAccessRole(t, ctx, service, owner, "role-image-source-role", "Exact image source editor", []string{"image.source.view", "image.source.manage"}, []string{"RESOURCE_INSTANCE"})
	sourceBinding := createRoleImageAccessBinding(t, ctx, service, owner, "role-image-source-binding", subjects[0].Ref, sourceRole.CurrentVersion.Ref,
		entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "ROLE_IMAGE", ResourceRef: current.Ref})
	detail, err = repository.Get(ctx, roleImageCandidate, current.Ref)
	if err != nil || !detail.Recipe.SourceAvailable || detail.Recipe.Input.Dockerfile != created.Recipe.Input.Dockerfile || detail.Recipe.Input.InstallationBlock != created.Recipe.Input.InstallationBlock || !containsString(detail.Recipe.NextActions, "UPDATE") {
		t.Fatalf("source editor exact read failed: %v", err)
	}
	current = detail.Recipe
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
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "role-image-source-revoke", ExpectedVersion: &sourceBinding.Version},
		Payload:  command.AccessBindingInput{BindingRef: sourceBinding.Ref}}); err != nil {
		t.Fatalf("revoke image source authority: %v", err)
	}
	replayed, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "REQUEST_BUILD", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Mutation: roleImageTestMutation("role-image-access-request-build", "REQUEST_BUILD", &buildVersion),
	})
	if err != nil || replayed.Build == nil || replayed.Build.Ref != requested.Build.Ref || replayed.Build.SourceAvailable || replayed.Build.Dockerfile != "" || replayed.Recipe.SourceAvailable || replayed.Recipe.Input.InstallationBlock != "" {
		t.Fatalf("build receipt replay did not recheck source authority: %v", err)
	}
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: roleImageCandidate, Action: "UPDATE", ProjectRef: project.Ref, RecipeRef: current.Ref,
		Name: "Updated application RBAC image", Mutation: roleImageTestMutation("role-image-access-update", "UPDATE", &wrongProjectVersion),
	}); !errors.Is(err, domainerrs.ErrNotFound) && !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("source update receipt bypassed revoked authority: %v", err)
	}
	if created.Recipe.ManagedLineage == nil {
		t.Fatal("role image is missing managed history lineage")
	}
	set, revisions, _, _, err := repository.ListManagedConfigurationHistory(ctx, roleImageCandidate, created.Recipe.ManagedLineage.ConfigurationRef, query.Page{Size: 20})
	if err != nil || len(revisions) == 0 {
		t.Fatalf("read metadata history after source revoke: %v", err)
	}
	if set.SourceEditable == nil || *set.SourceEditable {
		t.Fatal("managed source edit authority was not projected after revoke")
	}
	if set.CurrentRevision != nil && (set.CurrentRevision.Content != "" || set.CurrentRevision.SourceAvailable == nil || *set.CurrentRevision.SourceAvailable) {
		t.Fatal("managed current revision exposed revoked source")
	}
	for _, revision := range revisions {
		if revision.Content != "" || revision.SourceAvailable == nil || *revision.SourceAvailable || len(revision.ValidationDiagnostics) != 0 {
			t.Fatal("managed history exposed revoked source")
		}
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

func createRoleImageAccessBinding(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, key, subjectRef, roleVersionRef string, scope entity.AccessScope) entity.AccessBinding {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjectRef, RoleVersionRef: roleVersionRef, Scope: scope,
		}})
	if err != nil || result.AccessBinding == nil {
		t.Fatalf("create %s: binding=%#v err=%v", key, result.AccessBinding, err)
	}
	return *result.AccessBinding
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
