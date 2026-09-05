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
	for index := range definition.Spec.Capabilities {
		if definition.Spec.Capabilities[index].Operation == definition.Spec.HealthCheck.Operation {
			definition.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
		}
	}
	definition.Spec.Name = "Интеграция с подтверждением чтения"
	second := publishAndRebindManagedConfiguration(t, ctx, service, owner, "managed-package-gated-health", command.CreateIntegrationDefinition,
		command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.ManagedConfigurationInput{Name: definition.Spec.Name, ContentFormat: "JSON", Content: string(asJSON(definition))},
		entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: connectionRef})
	connection, err = service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil || connection.DefinitionDigest != second.ManagedRevision.Digest || slices.Contains(connection.NextActions, "TEST") || !connection.TestRequiresApproval {
		t.Fatalf("gated health readback exposed unsafe test: %+v %v", connection, err)
	}
	version = connection.Version
	_, err = service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-package-gated-denied", ExpectedVersion: &version}, Payload: command.ConnectionInput{Ref: connectionRef}})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("health gate bypass: %v", err)
	}
}
