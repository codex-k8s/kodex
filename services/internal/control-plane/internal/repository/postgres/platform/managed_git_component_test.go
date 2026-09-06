package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testManagedGitOwnership(t *testing.T, ctx context.Context, service *platformservice.Service, pool *pgxpool.Pool, owner value.Principal, configuration entity.ManagedConfigurationSet, publishedRef string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets
SET managed_by='GIT',source='https://git.example.test/config/prompt.txt',source_revision=repeat('a',40) WHERE ref=$1`, configuration.Ref); err != nil {
		t.Fatal(err)
	}
	for index, kind := range []command.Kind{command.CreatePromptTemplateDraft, command.ValidatePromptTemplateDraft, command.PublishPromptTemplateDraft} {
		_, err := executePromptPublicationFixture(t, ctx, service, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("managed-git-forbidden-%d", index), ExpectedVersion: &configuration.Version},
			Payload:  command.ManagedConfigurationInput{ConfigurationRef: configuration.Ref, RevisionRef: publishedRef, Name: "Forbidden Git edit", ContentFormat: "TEXT", Content: "Replacement"}})
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("Git UI write %s: %v", kind, err)
		}
	}
	copyCommand := command.Command{Kind: command.CopyGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-git-owner-copy", ExpectedVersion: &configuration.Version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: configuration.Ref, Name: "Independent UI copy"}}
	copied, err := service.Execute(ctx, copyCommand)
	if err != nil || copied.ManagedRevision == nil || copied.ManagedRevision.State != "DRAFT" || copied.ManagedRevision.ParentRevisionRef != publishedRef || copied.ManagedConfiguration.ManagedBy != "UI" {
		t.Fatalf("Git copy lineage: %v", err)
	}
	if replayed, err := service.Execute(ctx, copyCommand); err != nil || replayed.ManagedRevision.Ref != copied.ManagedRevision.Ref {
		t.Fatalf("Git copy replay: %v", err)
	}
	detach := command.Command{Kind: command.DetachGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-git-owner-detach", ExpectedVersion: &configuration.Version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: configuration.Ref}}
	detached, err := service.Execute(ctx, detach)
	if err != nil || detached.ManagedRevision == nil || detached.ManagedRevision.State != "DRAFT" || detached.ManagedRevision.ParentRevisionRef != publishedRef ||
		detached.ManagedConfiguration.ManagedBy != "UI" || detached.ManagedConfiguration.CurrentRevision == nil || detached.ManagedConfiguration.CurrentRevision.Ref != publishedRef {
		t.Fatalf("Git detach draft/current snapshot: %v", err)
	}
	if replayed, err := service.Execute(ctx, detach); err != nil || replayed.ManagedRevision.Ref != detached.ManagedRevision.Ref {
		t.Fatalf("Git detach replay: %v", err)
	}
	var currentRef string
	if err := pool.QueryRow(ctx, `SELECT revision.ref FROM control_plane.managed_configuration_sets configuration
JOIN control_plane.managed_configuration_revisions revision ON revision.id=configuration.current_revision_id WHERE configuration.ref=$1`, configuration.Ref).Scan(&currentRef); err != nil || currentRef != publishedRef {
		t.Fatalf("detach changed published revision: %v", err)
	}
}
