package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/role_image_managed_readback.sql
var queryRoleImageManagedReadback string

func assertManagedRoleImageBuild(t *testing.T, ctx context.Context, repository *Repository, recipe entity.RoleImageRecipe, build entity.ImageBuild) {
	t.Helper()
	var configurationRef, revisionRef, state, content, buildRef string
	var generation uint64
	err := repository.pool.QueryRow(ctx, queryRoleImageManagedReadback, recipe.Ref, build.Ref).Scan(&configurationRef, &revisionRef, &state, &content, &generation, &buildRef)
	if err != nil || configurationRef == "" || revisionRef == "" || state != "PUBLISHED" || generation != recipe.Generation || buildRef != build.Ref {
		t.Fatalf("managed recipe/build lineage mismatch: %v", err)
	}
	name, roleRef, selection, err := revisionservice.ParseRoleImage("JSON", content)
	if err != nil || name != recipe.Name || roleRef != recipe.RoleDefinitionRef || selection.EnvironmentKey != recipe.Input.EnvironmentKey {
		t.Fatalf("managed recipe snapshot mismatch: %v", err)
	}
}

func testManagedRoleImageDraftFence(t *testing.T, ctx context.Context, repository *Repository, owner, resolvedOwner value.Principal, recipe entity.RoleImageRecipe, build entity.ImageBuild) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	var configurationRef string
	current, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, queryRoleImageManagedConfiguration, current.organizationID, recipe.Ref).Scan(&configurationRef); err != nil {
		t.Fatal(err)
	}
	configuration, revisions, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, configurationRef, query.Page{Size: 10})
	if err != nil || len(revisions) != 1 || revisions[0].State != "PUBLISHED" {
		t.Fatalf("managed recipe history unavailable: %v", err)
	}
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreateRoleImageRevisionDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-recipe-parallel-draft", ExpectedVersion: &configuration.Version},
		Payload:  command.ManagedConfigurationInput{ProjectRef: recipe.ProjectRef, ConfigurationRef: configurationRef, Name: recipe.Name, ContentFormat: "JSON", Content: revisions[0].Content}})
	if err != nil {
		t.Fatalf("create parallel managed draft: %v", err)
	}
	version := int64(recipe.Version)
	_, err = repository.Manage(ctx, roleimagerepo.ManageInput{Principal: resolvedOwner, Action: "UPDATE", RecipeRef: recipe.Ref,
		ProjectRef: recipe.ProjectRef, Name: recipe.Name, Recipe: recipe.Input,
		Mutation: roleImageTestMutation("managed-recipe-conflicting-update", "UPDATE", &version)})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("recipe update overwrote a parallel draft: %v", err)
	}
	assertManagedRoleImageBuild(t, ctx, repository, recipe, build)
	_, err = service.Execute(ctx, command.Command{Kind: command.DiscardRoleImageRevisionDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-recipe-discard-parallel", ExpectedVersion: &draft.ManagedConfiguration.Version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: draft.ManagedRevision.Ref}})
	if err != nil {
		t.Fatalf("discard parallel managed draft: %v", err)
	}
}
