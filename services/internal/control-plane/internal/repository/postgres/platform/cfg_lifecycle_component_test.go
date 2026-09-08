package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testCFGLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, reader value.Principal, projectRef, connectionRef string, published command.Result) {
	t.Helper()
	retained, err := service.GetEffectiveManagedConfiguration(ctx, reader, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	published.ManagedConfiguration, published.ManagedRevision = &retained.Configuration, &retained.Revision
	definition := repository.integrationDefinitions["synthetic"]
	var version int64
	if err := repository.pool.QueryRow(ctx, `SELECT version FROM control_plane.integration_definitions WHERE stable_key='synthetic'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	copyInput := command.Command{Kind: command.CopyIntegrationDefinitionConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "cfg-shipped-copy", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{Name: "Independent synthetic", DefinitionKey: "synthetic", DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest}}
	copied, err := service.Execute(ctx, copyInput)
	if err != nil || copied.ManagedRevision == nil || copied.ManagedRevision.State != "DRAFT" || copied.ManagedConfiguration.CopyProvenance == nil || copied.ManagedConfiguration.CopyProvenance.Origin != "SHIPPED" || copied.ManagedConfiguration.ManagedBy != "UI" {
		t.Fatalf("copy shipped definition: %v", err)
	}
	if replay, err := service.Execute(ctx, copyInput); err != nil || replay.ManagedConfiguration.Ref != copied.ManagedConfiguration.Ref {
		t.Fatalf("copy replay: %v", err)
	}
	stale := copyInput
	stale.Mutation.IdempotencyKey = "cfg-shipped-stale"
	staleVersion := version + 1
	stale.Mutation.ExpectedVersion = &staleVersion
	if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale shipped version: %v", err)
	}
	foreign := copyInput
	foreign.Principal.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if _, err := service.Execute(ctx, foreign); err == nil {
		t.Fatal("foreign copy receipt was exposed")
	}
	for _, item := range []struct {
		kind command.Kind
		key  string
	}{{command.ValidateIntegrationDefinition, "validate"}, {command.PublishIntegrationDefinition, "publish"}} {
		v := copied.ManagedConfiguration.Version
		copied, err = service.Execute(ctx, command.Command{Kind: item.kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-copy-" + item.key, ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: copied.ManagedConfiguration.Ref, RevisionRef: copied.ManagedRevision.Ref}})
		if err != nil {
			t.Fatalf("copied definition %s: %v", item.key, err)
		}
	}
	for _, origin := range []string{"UI", "GIT"} {
		if origin == "GIT" {
			if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET managed_by='GIT',source='fixture',source_revision=repeat('a',40) WHERE ref=$1`, copied.ManagedConfiguration.Ref); err != nil {
				t.Fatal(err)
			}
		}
		v := copied.ManagedConfiguration.Version
		independent, err := service.Execute(ctx, command.Command{Kind: command.CopyIntegrationDefinitionConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-origin-" + origin, ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: copied.ManagedConfiguration.Ref, Name: "Copy " + origin}})
		if err != nil || independent.ManagedConfiguration.CopyProvenance.Origin != origin || independent.ManagedRevision.ParentRevisionRef != copied.ManagedRevision.Ref {
			t.Fatalf("copy %s provenance: %v", origin, err)
		}
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET copy_provenance='{}' WHERE ref=$1`, copied.ManagedConfiguration.Ref); err == nil {
		t.Fatal("copy provenance changed")
	}
	published.ManagedConfiguration = testCFGForwardRevision(t, ctx, service, owner, reader, connectionRef, published).ManagedConfiguration
	archiveVersion := published.ManagedConfiguration.Version
	archive := command.Command{Kind: command.ArchiveIntegrationDefinitionConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-definition-archive", ExpectedVersion: &archiveVersion}, Payload: command.ManagedConfigurationInput{ConfigurationRef: published.ManagedConfiguration.Ref}}
	archived, err := service.Execute(ctx, archive)
	if err != nil || !archived.ManagedConfiguration.Archived {
		t.Fatalf("archive definition: %v", err)
	}
	if replay, err := service.Execute(ctx, archive); err != nil || !replay.ManagedConfiguration.Archived {
		t.Fatalf("archive replay: %v", err)
	}
	binding, err := service.GetEffectiveManagedConfiguration(ctx, reader, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", connectionRef)
	if err != nil || binding.Revision.Ref != published.ManagedRevision.Ref || !binding.Configuration.Archived {
		t.Fatalf("archive lost immutable binding: %v", err)
	}
	for index, kind := range []command.Kind{command.CreateIntegrationDefinition, command.PublishIntegrationDefinition, command.RebindIntegrationDefinition} {
		v := archived.ManagedConfiguration.Version
		_, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("cfg-archive-block-%d", index), ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: archived.ManagedConfiguration.Ref, RevisionRef: published.ManagedRevision.Ref, Name: "blocked", ContentFormat: "JSON", Content: "{}"}})
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("archived mutation %s: %v", kind, err)
		}
	}
	items, _, _, err := service.ListManagedConfigurations(ctx, owner, query.Filter{Category: "INTEGRATION_DEFINITION", Page: query.Page{Size: 200}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Ref == archived.ManagedConfiguration.Ref {
			t.Fatal("archived configuration selected")
		}
	}
	_, history, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, archived.ManagedConfiguration.Ref, query.Page{Size: 100})
	if err != nil || len(history) == 0 {
		t.Fatalf("archived history unavailable: %v", err)
	}
	testCFGRoleImageLifecycle(t, ctx, repository, service, owner, projectRef)
}

func testCFGRoleImageLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef string) {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	var recipeRef string
	if err := repository.pool.QueryRow(ctx, `SELECT recipe.ref FROM control_plane.role_image_recipes recipe JOIN control_plane.projects project ON project.id=recipe.project_id WHERE project.ref=$1 AND recipe.specification->>'SourceRef'=$2`, projectRef, platformOwnedRoleImageSource).Scan(&recipeRef); err != nil {
		t.Fatal(err)
	}
	detail, err := repository.Get(ctx, resolved, recipeRef)
	if err != nil {
		t.Fatal(err)
	}
	if !shippedRoleImage(detail.Recipe) {
		t.Fatal("fixture is not canonical shipped image")
	}
	_, catalogInput := promotionComponentCatalog(t)
	catalogInput.BaseImageReference, catalogInput.BaseImageDigest = detail.Recipe.Input.BaseImageReference, detail.Recipe.Input.BaseImageDigest
	catalog, err := roleimageservice.NewCatalog([]roleimageservice.Environment{{Key: "standard", NameMessageKey: "role-environments.standard.name", DescriptionMessageKey: "role-environments.standard.description", Recommended: true, Available: true, Input: catalogInput}})
	if err != nil {
		t.Fatal(err)
	}
	repository.ConfigureRoleImageCatalog(catalog)
	version := int64(detail.Recipe.Version)
	copyInput := command.Command{Kind: command.CopyRoleImageConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-image-shipped-copy", ExpectedVersion: &version}, Payload: command.ManagedConfigurationInput{RecipeRef: recipeRef, ProjectRef: projectRef, Name: "Independent image"}}
	copied, err := service.Execute(ctx, copyInput)
	if err != nil || copied.ManagedConfiguration.CopyProvenance == nil || copied.ManagedConfiguration.CopyProvenance.Origin != "SHIPPED" {
		t.Fatalf("copy shipped image: %v", err)
	}
	for _, item := range []struct {
		kind command.Kind
		key  string
	}{{command.ValidateRoleImageRevision, "validate"}, {command.PublishRoleImageRevision, "publish"}} {
		v := copied.ManagedConfiguration.Version
		copied, err = service.Execute(ctx, command.Command{Kind: item.kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-image-" + item.key, ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: copied.ManagedConfiguration.Ref, RevisionRef: copied.ManagedRevision.Ref}})
		if err != nil {
			t.Fatalf("image copy %s: %v", item.key, err)
		}
	}
	var copiedRecipe string
	if err := repository.pool.QueryRow(ctx, `SELECT recipe.ref FROM control_plane.managed_role_image_recipes mapping JOIN control_plane.managed_configuration_sets configuration ON configuration.id=mapping.configuration_set_id JOIN control_plane.role_image_recipes recipe ON recipe.id=mapping.recipe_id WHERE configuration.ref=$1`, copied.ManagedConfiguration.Ref).Scan(&copiedRecipe); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"UI", "GIT"} {
		if origin == "GIT" {
			if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET managed_by='GIT',source='fixture',source_revision=repeat('a',40) WHERE ref=$1`, copied.ManagedConfiguration.Ref); err != nil {
				t.Fatal(err)
			}
		}
		v := copied.ManagedConfiguration.Version
		independent, err := service.Execute(ctx, command.Command{Kind: command.CopyRoleImageConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-image-source-" + origin, ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: copied.ManagedConfiguration.Ref, ProjectRef: projectRef, Name: "Independent " + origin}})
		if err != nil || independent.ManagedConfiguration.CopyProvenance.Origin != origin || independent.ManagedRevision.ParentRevisionRef != copied.ManagedRevision.Ref {
			t.Fatalf("image copy %s lineage: %v", origin, err)
		}
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET managed_by='UI',source='control-center',source_revision='' WHERE ref=$1`, copied.ManagedConfiguration.Ref); err != nil {
		t.Fatal(err)
	}
	v := copied.ManagedConfiguration.Version
	archived, err := service.Execute(ctx, command.Command{Kind: command.ArchiveRoleImageConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-image-archive", ExpectedVersion: &v}, Payload: command.ManagedConfigurationInput{ConfigurationRef: copied.ManagedConfiguration.Ref}})
	if err != nil || !archived.ManagedConfiguration.Archived {
		t.Fatalf("image archive: %v", err)
	}
	if replay, err := service.Execute(ctx, copyInput); err != nil || !replay.ManagedConfiguration.Archived || replay.ManagedConfiguration.Ref != copied.ManagedConfiguration.Ref {
		t.Fatalf("copy replay returned stale archive metadata: %v", err)
	}
	var openBuilds int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.image_builds build JOIN control_plane.role_image_recipes recipe ON recipe.id=build.recipe_id WHERE recipe.ref=$1 AND build.stage NOT IN ('COMPLETED','CANCELLED','DEAD_LETTER')`, copiedRecipe).Scan(&openBuilds); err != nil || openBuilds != 0 {
		t.Fatalf("archive retained build work: %v", err)
	}
	after, err := repository.Get(ctx, resolved, copiedRecipe)
	if err != nil || after.Recipe.State != "ARCHIVED" {
		t.Fatalf("recipe archive state: %v", err)
	}
	rv := int64(after.Recipe.Version)
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "REQUEST_BUILD", RecipeRef: copiedRecipe, ProjectRef: projectRef, Mutation: roleImageTestMutation("cfg-image-forbidden-build", "REQUEST_BUILD", &rv)}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("archived build bypass: %v", err)
	}
	restored, err := repository.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "RESTORE", RecipeRef: copiedRecipe, ProjectRef: projectRef, Mutation: roleImageTestMutation("cfg-image-restore", "RESTORE", &rv)})
	if err != nil || restored.Recipe.State != "ACTIVE" {
		t.Fatalf("legacy restore: %v", err)
	}
	set, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, copied.ManagedConfiguration.Ref, query.Page{Size: 100})
	if err != nil || set.Archived {
		t.Fatalf("restore managed state: %v", err)
	}
	rv = int64(restored.Recipe.Version)
	if _, err := repository.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "ARCHIVE", RecipeRef: copiedRecipe, ProjectRef: projectRef, Mutation: roleImageTestMutation("cfg-image-legacy-archive", "ARCHIVE", &rv)}); err != nil {
		t.Fatalf("legacy archive: %v", err)
	}
	set, _, _, _, err = service.ListManagedConfigurationHistory(ctx, owner, copied.ManagedConfiguration.Ref, query.Page{Size: 100})
	if err != nil || !set.Archived {
		t.Fatalf("legacy archive managed state: %v", err)
	}
	unchanged, err := repository.Get(ctx, resolved, recipeRef)
	if err != nil || unchanged.Recipe.Version != detail.Recipe.Version || unchanged.Recipe.SpecSHA256 != detail.Recipe.SpecSHA256 {
		t.Fatalf("shipped source changed: %v", err)
	}
}
