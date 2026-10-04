package platform

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testManagedIntegrationPackageExecution(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connectionRef string, published command.Result) {
	t.Helper()
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil || connection.DefinitionDigest != published.ManagedRevision.Digest || connection.State != "NOT_CONNECTED" {
		t.Fatalf("managed package did not bind actual connection pins: %+v %v", connection, err)
	}
	definition, err := integrationpackage.Parse([]byte(published.ManagedRevision.Content))
	if err != nil || definition.Metadata.Origin != "UI" {
		t.Fatalf("owner did not assign UI origin: %v", err)
	}
	version := connection.Version
	tested, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-test", ExpectedVersion: &version}, Payload: command.ConnectionInput{Ref: connectionRef}})
	if err != nil || tested.Connection == nil {
		t.Fatalf("queue managed package test: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway",
		Operation: "platform.runtime.integrations.tests.claim"}, "integration-gateway")
	claims, err := service.ClaimIntegrationConnectionTests(ctx, worker, "managed-package-worker", 32)
	if err != nil {
		t.Fatalf("claim managed package: %v", err)
	}
	var found map[string]any
	for _, claim := range claims {
		if claim["connectionRef"] == connectionRef {
			found = claim
		}
	}
	if found == nil {
		t.Fatal("managed test claim absent")
	}
	raw, ok := found["definitionPackage"].([]byte)
	claimed, err := integrationpackage.Parse(raw)
	if !ok || err != nil || claimed.Digest != definition.Digest || claimed.Metadata.Origin != "UI" {
		t.Fatalf("private claim lost exact package: %v", err)
	}
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-test-complete"}, Payload: command.IntegrationConnectionTestInput{
			TestRef: found["testRef"].(string), LeaseRef: found["leaseRef"].(string), Fence: found["fence"].(string),
			Generation: found["generation"].(int64), Success: true}})
	if err != nil || completed.Connection == nil || completed.Connection.State != "CONNECTED" {
		t.Fatalf("managed test completion: %v", err)
	}
	testManagedRuntimeCapabilityAuthority(t, ctx, repository, service, owner, *completed.Connection, definition)
	// Перепривязка не вправе наследовать Secret descriptor прежней ревизии,
	// даже если новый пакет пока не требует credential.
	if _, err := repository.pool.Exec(ctx, `WITH revision AS (
INSERT INTO control_plane.integration_credential_revisions
(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
SELECT 'icr_managed_rebind',organization_id,id,1,'kodex-system/kodex-integration-credentials#managed-rebind',gen_random_uuid(),'1',repeat('d',64),created_by
FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
UPDATE control_plane.integration_connections connection SET credential_revision_id=revision.id,masked_credentials_state='CONFIGURED',credential_materialization_ref='managed-rebind'
FROM revision WHERE connection.id=revision.connection_id`, connectionRef); err != nil {
		t.Fatalf("seed obsolete credential binding: %v", err)
	}
	before, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	gated, err := integrationpackage.Parse(asJSON(definition))
	if err != nil {
		t.Fatal(err)
	}
	for index := range gated.Spec.Capabilities {
		if gated.Spec.Capabilities[index].Operation == gated.Spec.HealthCheck.Operation {
			gated.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
		}
	}
	gated.Spec.Name = "Интеграция с недопустимым подтверждением чтения"
	invalid, err := service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-gated-health-create"},
		Payload:  command.ManagedConfigurationInput{Name: gated.Spec.Name, ContentFormat: "JSON", Content: string(asJSON(gated))}})
	if err != nil || invalid.ManagedConfiguration == nil || invalid.ManagedRevision == nil || invalid.ManagedRevision.State != "DRAFT" {
		t.Fatalf("create invalid health policy draft: %v", err)
	}
	version = invalid.ManagedConfiguration.Version
	invalid, err = service.Execute(ctx, command.Command{Kind: command.ValidateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-gated-health-validate", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: invalid.ManagedConfiguration.Ref, RevisionRef: invalid.ManagedRevision.Ref}})
	if err != nil || invalid.ManagedConfiguration == nil || invalid.ManagedRevision == nil || invalid.ManagedRevision.State != "INVALID" {
		t.Fatalf("READ health approval policy was not rejected: %v", err)
	}
	version = invalid.ManagedConfiguration.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-gated-health-publish", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: invalid.ManagedConfiguration.Ref, RevisionRef: invalid.ManagedRevision.Ref}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("invalid READ health policy publication was accepted: %v", err)
	}
	unchanged, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil || unchanged.Version != before.Version || unchanged.DefinitionDigest != before.DefinitionDigest || unchanged.DefinitionVersion != before.DefinitionVersion ||
		before.CredentialRevision == nil || unchanged.CredentialRevision == nil || unchanged.CredentialRevision.Ref != before.CredentialRevision.Ref {
		t.Fatalf("invalid health policy changed exact connection binding: %v", err)
	}
	definition.Spec.HealthCheck.TimeoutSeconds--
	definition.Spec.Name = "Интеграция с ограниченным временем проверки"
	second := publishAndRebindManagedConfiguration(t, ctx, service, owner, "managed-package-narrowed-health", command.CreateIntegrationDefinition,
		command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.ManagedConfigurationInput{Name: definition.Spec.Name, ContentFormat: "JSON", Content: string(asJSON(definition))},
		entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: connectionRef})
	connection, err = service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil || connection.DefinitionDigest != second.ManagedRevision.Digest || connection.CredentialRevision != nil || !slices.Contains(connection.NextActions, "TEST") || connection.TestRequiresApproval {
		t.Fatalf("narrowed health binding retained obsolete credential or unsafe test policy: %v", err)
	}
}
