package platform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// pinHistoricalGitHubComponent preserves the preexisting shared owner/repository fixture contract under its exact frozen6.4 version/digest. It never changes production defaults or introduces a second runtime implementation.
// pinHistoricalGitHubComponent сохраняет прежний shared owner/repository fixture contract по точным frozen6.4 version/digest. Не меняет production defaults и не вводит вторую runtime implementation.
func pinHistoricalGitHubComponent(t *testing.T, r *Repository) {
	t.Helper()
	current := r.integrationDefinitions["github"]
	previous, ok := integrationpackage.ResolveShippedRevision(current, "6.4.0", "bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da")
	if !ok {
		t.Fatal("frozen GitHub6.4 fixture contract is unavailable")
	}
	r.integrationDefinitions["github"] = previous
}

// currentGitHubComponent restores only the trusted currently shipped6.5 contract before Bootstrap for the new personal scope component scenarios.
// currentGitHubComponent возвращает лишь trusted currently shipped6.5 contract перед Bootstrap для новых personal scope component scenarios.
func currentGitHubComponent(t *testing.T) func(*Repository) {
	t.Helper()
	return func(r *Repository) {
		definitions, err := integrationpackage.LoadShipped()
		if err != nil {
			t.Fatal(err)
		}
		if definitions["github"].Metadata.Version != "6.5.0" {
			t.Fatal("current GitHub scope fixture has an unexpected version")
		}
		r.integrationDefinitions["github"] = definitions["github"]
	}
}

// TestManagedGitHubScopeKeepsBoundary proves an approved narrowed revision with another version retains scoped classification and a personal bundle's closed worker profile.
// TestManagedGitHubScopeKeepsBoundary проверяет сохранение scoped classification и закрытого personal bundle worker profile у approved narrowed revision с другой версией.
func TestManagedGitHubScopeKeepsBoundary(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	candidate := definitions["github"]
	candidate.Metadata.Version = "8.0.0"
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	managed, _, err := integrationpackage.NormalizeManagedRevision(raw, "UI", definitions)
	if err != nil || !githubScopedDefinition(managed) {
		t.Fatal("managed version removed scoped GitHub boundary", err)
	}
	previous, ok := integrationpackage.ResolveShippedRevision(definitions["github"], "6.4.0", "bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da")
	if !ok || githubScopedDefinition(previous) {
		t.Fatal("historical repository profile was widened")
	}
	row := repositoryBundleRow{}
	row.snapshot.DefinitionVersion = managed.Metadata.Version
	row.snapshot.PersonalBindingRef = "ipc_synthetic"
	row.snapshot.ResourceScope = map[string]string{"scope_kind": "ALL_ACCESSIBLE"}
	work := repositoryBundleWork(row)
	if work.ResourceKind != "GITHUB_SCOPE" || work.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionGitHubScopedV1 {
		t.Fatal("managed version lost personal bundle profile")
	}
}

// rebindManagedGitHubScopedComponent publishes and binds a version-changed narrow package through native owner commands so managed authority tests never synthesize an approved revision in SQL.
// rebindManagedGitHubScopedComponent публикует и связывает narrow package с изменённой версией через native owner commands; managed authority tests не подделывают approved revision в SQL.
func rebindManagedGitHubScopedComponent(t *testing.T, ctx context.Context, r *Repository, service *platform.Service, owner value.Principal, connection entity.IntegrationConnection) *entity.IntegrationConnection {
	t.Helper()
	definition := r.integrationDefinitions["github"]
	definition.Metadata.Version = "8.0.0"
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "github-managed-create"}, Payload: command.ManagedConfigurationInput{Name: "Synthetic scoped GitHub8", ContentFormat: "JSON", Content: string(asJSON(definition))}})
	if err != nil {
		t.Fatal("create managed scoped GitHub fixture", err)
	}
	for _, kind := range []command.Kind{command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition} {
		version := result.ManagedConfiguration.Version
		result, err = service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "github-managed-" + string(kind), ExpectedVersion: &version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref, RevisionRef: result.ManagedRevision.Ref}})
		if err != nil {
			t.Fatal("publish managed scoped GitHub fixture", err)
		}
	}
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, result.ManagedConfiguration.Ref, result.ManagedRevision.Ref, query.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	version := result.ManagedConfiguration.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.RebindIntegrationDefinition, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "github-managed-bind", ExpectedVersion: &version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref, RevisionRef: result.ManagedRevision.Ref, ImpactDigest: impact.Digest, Consumers: []entity.ManagedConfigurationConsumer{{Kind: "INTEGRATION_CONNECTION", Ref: connection.Ref, ExpectedAbsent: true}}}}); err != nil {
		t.Fatal("bind managed scoped GitHub fixture", err)
	}
	updated, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil || updated.DefinitionVersion != "8.0.0" {
		t.Fatal("managed scoped GitHub pins were lost", err)
	}
	return &updated
}
