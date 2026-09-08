package platform

import (
	"context"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Восстановление сохраняет старое содержимое, создавая новую публикацию;
// существующее подключение не следует за указателем configuration автоматически.
func testCFGForwardRevision(t *testing.T, ctx context.Context, service *platformservice.Service, owner, reader value.Principal, connectionRef string, original command.Result) command.Result {
	t.Helper()
	before, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, original.ManagedConfiguration.Ref, query.Page{Size: 100})
	if err != nil || before.CurrentRevision == nil {
		t.Fatalf("restore source history: %v", err)
	}
	version := before.Version
	restore := command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "cfg-forward-revision", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: before.Ref, Name: before.Name,
			ContentFormat: original.ManagedRevision.ContentFormat, Content: original.ManagedRevision.Content}}
	created, err := service.Execute(ctx, restore)
	if err != nil || created.ManagedRevision == nil || created.ManagedRevision.State != "DRAFT" ||
		created.ManagedRevision.Ref == original.ManagedRevision.Ref || created.ManagedRevision.Revision <= before.CurrentRevision.Revision ||
		created.ManagedRevision.ParentRevisionRef != before.CurrentRevision.Ref || created.ManagedRevision.Content != original.ManagedRevision.Content {
		t.Fatalf("restore did not create forward draft: %v", err)
	}
	if replay, err := service.Execute(ctx, restore); err != nil || replay.ManagedRevision.Ref != created.ManagedRevision.Ref {
		t.Fatalf("restore draft replay: %v", err)
	}
	unchanged, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, before.Ref, query.Page{Size: 100})
	if err != nil || unchanged.CurrentRevision == nil || unchanged.CurrentRevision.Ref != before.CurrentRevision.Ref {
		t.Fatalf("restore draft moved published pointer: %v", err)
	}
	for _, kind := range []command.Kind{command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition} {
		v := created.ManagedConfiguration.Version
		created, err = service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "cfg-forward-" + string(kind), ExpectedVersion: &v},
			Payload:  command.ManagedConfigurationInput{ConfigurationRef: before.Ref, RevisionRef: created.ManagedRevision.Ref}})
		if err != nil {
			t.Fatalf("restore transition %s: %v", kind, err)
		}
	}
	after, revisions, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, before.Ref, query.Page{Size: 100})
	if err != nil || after.CurrentRevision == nil || after.CurrentRevision.Ref != created.ManagedRevision.Ref {
		t.Fatalf("restore publication pointer: %v", err)
	}
	found := false
	for _, revision := range revisions {
		if revision.Ref == original.ManagedRevision.Ref {
			found = revision.Content == original.ManagedRevision.Content && revision.Digest == original.ManagedRevision.Digest
		}
	}
	if !found {
		t.Fatal("restore changed immutable source history")
	}
	bound, err := service.GetEffectiveManagedConfiguration(ctx, reader, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", connectionRef)
	if err != nil || bound.Revision.Ref != original.ManagedRevision.Ref || bound.Revision.Digest != original.ManagedRevision.Digest {
		t.Fatalf("restore publication changed previous connection pin: %v", err)
	}
	return created
}
