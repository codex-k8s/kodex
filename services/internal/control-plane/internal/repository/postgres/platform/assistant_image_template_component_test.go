package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Вызывается обязательной изолированной profile suite без provider или браузера.
func testAssistantProjectImageTemplateSelection(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, lease map[string]any) {
	t.Helper()
	catalog, template := promotionComponentCatalog(t)
	r.ConfigureRoleImageCatalog(catalog)
	defer r.ConfigureRoleImageCatalog(catalog)
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, stringMap(lease, "agentRef"))
	if err != nil {
		t.Fatal(err)
	}
	projectRef := stringMap(lease, "projectRef")
	custom := template.Dockerfile + "\n# project custom\n\n"
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantRoleImageRecipe, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-template-create"}, Payload: command.AssistantRoleImageRecipeInput{
			ProjectRef: projectRef, AgentRef: stringMap(lease, "agentRef"), AgentVersion: view.AgentVersion, Name: "Project custom template",
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: "promotion", Dockerfile: custom}}})
	if err != nil || len(created.CreatedRefs) != 1 {
		t.Fatalf("create project template fixture: %v", err)
	}
	ref := created.CreatedRefs[0]
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	hydrate := func(parameters map[string]any, targetProject string) (entity.AssistantPlanOperation, error) {
		t.Helper()
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		return r.hydrateAssistantRoleImageUpdate(ctx, tx, scope, targetProject, entity.AssistantPlanOperation{
			Type: "UPDATE_ROLE_IMAGE_RECIPE", Key: "project-template-update", Title: "Project template", Summary: "Synthetic template selection", Parameters: parameters})
	}
	nameOnly, err := hydrate(map[string]any{"recipeRef": ref, "name": "Renamed custom"}, projectRef)
	if err != nil || assistantRoleImageDockerfile(nameOnly.After) != custom || assistantRoleImageDockerfile(nameOnly.Before) != custom {
		t.Fatalf("name-only changed project custom bytes: %v", err)
	}
	explicit := template.Dockerfile + "\n# explicit project bytes\n\n"
	explicitPlan, err := hydrate(map[string]any{"recipeRef": ref, "environmentKey": "promotion", "dockerfile": explicit}, projectRef)
	if err != nil || assistantRoleImageDockerfile(explicitPlan.After) != explicit {
		t.Fatalf("explicit project Dockerfile bytes changed: %v", err)
	}
	environments := catalog.List()
	environments[0].Input.BaseImageDigest = "sha256:" + strings.Repeat("c", 64)
	freshCatalog, err := roleimageservice.NewCatalog(environments)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfigureRoleImageCatalog(freshCatalog)
	fresh, err := freshCatalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "promotion"})
	if err != nil {
		t.Fatal(err)
	}
	for _, parameters := range []map[string]any{
		{"recipeRef": ref, "name": "Do not heal old custom"},
		{"recipeRef": ref, "environmentKey": "promotion", "dockerfile": custom},
	} {
		if _, err := hydrate(parameters, projectRef); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("old project Dockerfile was silently repinned")
		}
	}
	parameters := map[string]any{"recipeRef": ref, "environmentKey": "promotion"}
	repair, err := hydrate(parameters, projectRef)
	if err != nil || assistantRoleImageDockerfile(repair.Before) != custom || assistantRoleImageDockerfile(repair.After) != fresh.Dockerfile {
		t.Fatalf("same project environment key did not select fresh template: %v", err)
	}
	if _, err := hydrate(parameters, "prj_unknown0001"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("template reset crossed project authority")
	}
	repair, err = normalizeAssistantOperation(repair)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := r.Get(ctx, resolved, ref)
	if err != nil || unchanged.Recipe.Input.Dockerfile != custom || unchanged.Recipe.Generation != 1 || len(unchanged.Builds) != 1 {
		t.Fatal("preparing template repair changed persisted recipe")
	}
	forged := repair
	forged.Parameters = cloneAssistantFields(repair.Parameters)
	forged.Parameters["specSha256"] = strings.Repeat("e", 64)
	if _, err := rehydrateEditedAssistantRoleImageUpdate(repair, forged); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("project template edit changed server specification pin")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	matching, err := r.assistantRoleImageUpdateSnapshotMatches(ctx, tx, scope, repair)
	_ = tx.Rollback(ctx)
	if err != nil || !matching {
		t.Fatalf("fresh project template snapshot mismatch: %v", err)
	}
	drifted := freshCatalog.List()
	drifted[0].Input.BaseImageDigest = "sha256:" + strings.Repeat("d", 64)
	driftedCatalog, err := roleimageservice.NewCatalog(drifted)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfigureRoleImageCatalog(driftedCatalog)
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	matching, err = r.assistantRoleImageUpdateSnapshotMatches(ctx, tx, scope, repair)
	_ = tx.Rollback(ctx)
	if matching || err == nil {
		t.Fatal("project template snapshot accepted changed catalog dependency")
	}
	r.ConfigureRoleImageCatalog(freshCatalog)
	prepared, err := assistantOperationCommand(repair)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Principal = owner
	wrong := *prepared.Mutation.ExpectedVersion + 1
	stale := prepared
	stale.Mutation = value.Mutation{IdempotencyKey: "project-template-wrong-version", ExpectedVersion: &wrong}
	if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("template reset ignored recipe OCC")
	}
	prepared.Mutation.IdempotencyKey = "project-template-confirmed-apply"
	forgedCommand := prepared
	forgedPayload := prepared.Payload.(command.AssistantRoleImageUpdateInput)
	forgedPayload.SpecSHA256 = strings.Repeat("e", 64)
	forgedCommand.Payload = forgedPayload
	forgedCommand.Mutation.IdempotencyKey = "project-template-forged-specification"
	if _, err := service.Execute(ctx, forgedCommand); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("project template effect ignored confirmed specification pin")
	}
	if _, err := service.Execute(ctx, prepared); err != nil {
		t.Fatalf("apply confirmed project template: %v", err)
	}
	if _, err := service.Execute(ctx, prepared); err != nil {
		t.Fatalf("replay confirmed project template: %v", err)
	}
	unpinnedReplay := prepared
	unpinnedPayload := prepared.Payload.(command.AssistantRoleImageUpdateInput)
	unpinnedPayload.SpecSHA256 = ""
	unpinnedReplay.Payload = unpinnedPayload
	if _, err := service.Execute(ctx, unpinnedReplay); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("historical unpinned image command replay bypassed closed command protocol")
	}
	detail, err := r.Get(ctx, resolved, ref)
	if err != nil || detail.Recipe.Input.Dockerfile != fresh.Dockerfile || detail.Recipe.Generation != 2 || len(detail.Builds) != 2 {
		t.Fatalf("project template reset lost immutable generation: %v", err)
	}
	if _, err := hydrate(parameters, projectRef); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("same current template no-op was accepted")
	}
}
