package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testManagedDraftLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-draft-edit-project"}, Payload: command.ProjectInput{Name: "Managed draft edit", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, format, content string
		create, save, discard command.Kind
		project               bool
	}{
		{"prompt", "TEXT", "Original prompt", command.CreatePromptTemplateDraft, command.SavePromptTemplateDraft, command.DiscardPromptTemplateDraft, true},
		{"role", "JSON", "{}", command.CreateRoleImageRevisionDraft, command.SaveRoleImageRevisionDraft, command.DiscardRoleImageRevisionDraft, true},
		{"integration", "JSON", "{}", command.CreateIntegrationDefinition, command.SaveIntegrationDefinitionDraft, command.DiscardIntegrationDefinitionDraft, false},
		{"stt", "JSON", "{}", command.CreateSystemSTTDraft, command.SaveSystemSTTConfigurationDraft, command.DiscardSystemSTTConfigurationDraft, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			invoke := func(kind command.Kind, suffix string, version *int64, payload command.ManagedConfigurationInput) (command.Result, error) {
				return executePromptPublicationFixture(t, ctx, service, command.Command{Kind: kind, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "managed-edit-" + scenario.name + suffix, ExpectedVersion: version}, Payload: payload})
			}
			payload := command.ManagedConfigurationInput{Name: "Editable " + scenario.name, ContentFormat: scenario.format, Content: scenario.content}
			if scenario.project {
				payload.ProjectRef = project.Project.Ref
			}
			created, err := invoke(scenario.create, "-create", nil, payload)
			if err != nil || created.ManagedRevision == nil {
				t.Fatalf("create: %v", err)
			}
			edit := command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref, ContentFormat: scenario.format}
			saved, err := invoke(scenario.save, "-empty", &created.ManagedConfiguration.Version, edit)
			if err != nil || saved.ManagedRevision == nil || saved.ManagedRevision.Content != "" || saved.ManagedRevision.State != "DRAFT" || saved.ManagedRevision.ParentRevisionRef != created.ManagedRevision.Ref {
				t.Fatalf("save incomplete immutable draft: %v", err)
			}
			if _, err := invoke(scenario.save, "-stale", &created.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("stale save: %v", err)
			}
			_, history, total, _, err := service.ListManagedConfigurationHistory(ctx, owner, created.ManagedConfiguration.Ref, query.Page{Size: 10})
			if err != nil || total != 2 || len(history) != 2 || history[1].State != "DISCARDED" || history[1].Content != scenario.content {
				t.Fatalf("immutable saved history: %v", err)
			}
			edit.RevisionRef, edit.Content = saved.ManagedRevision.Ref, scenario.content
			repaired, err := invoke(scenario.save, "-repair", &saved.ManagedConfiguration.Version, edit)
			if err != nil || repaired.ManagedRevision == nil || repaired.ManagedRevision.ParentRevisionRef != saved.ManagedRevision.Ref {
				t.Fatalf("repair: %v", err)
			}
			edit.RevisionRef = repaired.ManagedRevision.Ref
			discarded, err := invoke(scenario.discard, "-discard", &repaired.ManagedConfiguration.Version, edit)
			if err != nil || discarded.ManagedRevision == nil || discarded.ManagedRevision.State != "DISCARDED" {
				t.Fatalf("discard: %v", err)
			}
			if _, err := invoke(scenario.save, "-after-discard", &discarded.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("discarded draft reopened: %v", err)
			}
			payload.ConfigurationRef = created.ManagedConfiguration.Ref
			next, err := invoke(scenario.create, "-next", &discarded.ManagedConfiguration.Version, payload)
			if err != nil {
				t.Fatalf("new draft after discard: %v", err)
			}
			edit.RevisionRef = next.ManagedRevision.Ref
			if scenario.name == "prompt" {
				validated, err := invoke(command.ValidatePromptTemplateDraft, "-validate", &next.ManagedConfiguration.Version, edit)
				if err != nil {
					t.Fatalf("validate repaired prompt: %v", err)
				}
				published, err := invoke(command.PublishPromptTemplateDraft, "-publish", &validated.ManagedConfiguration.Version, edit)
				if err != nil {
					t.Fatalf("publish repaired prompt: %v", err)
				}
				next = published
				if _, err := invoke(scenario.save, "-published-save", &next.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrConflict) {
					t.Fatalf("published revision edited: %v", err)
				}
				if _, err := invoke(scenario.discard, "-published-discard", &next.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrConflict) {
					t.Fatalf("published revision discarded: %v", err)
				}
			}
			if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET managed_by='GIT',source='https://git.example.invalid/context.git',source_revision='0123456789012345678901234567890123456789' WHERE ref=$1`, next.ManagedConfiguration.Ref); err != nil {
				t.Fatalf("Git owner fixture: %v", err)
			}
			if _, err := invoke(scenario.save, "-git-save", &next.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("Git revision edited by UI: %v", err)
			}
			if _, err := invoke(scenario.discard, "-git-discard", &next.ManagedConfiguration.Version, edit); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("Git revision discarded by UI: %v", err)
			}
		})
	}
}
