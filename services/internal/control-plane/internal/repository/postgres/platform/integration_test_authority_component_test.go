package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testIntegrationTestAuthority(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	worker := func(workload, operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: operation}, workload)
	}
	generic := worker("integration-gateway", "platform.runtime.integration-tests.claim")
	interaction := worker("interaction-gateway", "platform.interactions.connection-tests.claim")
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "route-test-create"},
		Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Connection test isolation", PublicConfiguration: map[string]any{"journal": "route-isolation"}}})
	if err != nil || created.Connection == nil {
		t.Fatalf("create test connection: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "route-test-start", ExpectedVersion: &created.Connection.Version},
		Payload: command.ConnectionInput{Ref: created.Connection.Ref}}); err != nil {
		t.Fatal(err)
	}
	if claims, err := service.ClaimIntegrationConnectionTests(ctx, interaction, "interaction-test", 32); err != nil || len(claims) != 0 {
		t.Fatalf("interaction claimed generic connection test: %d %v", len(claims), err)
	}
	claims, err := service.ClaimIntegrationConnectionTests(ctx, generic, "generic-test", 32)
	if err != nil {
		t.Fatal(err)
	}
	var claim map[string]any
	for _, item := range claims {
		if stringMap(item, "connectionRef") == created.Connection.Ref {
			claim = item
		}
	}
	if claim == nil {
		t.Fatal("generic test claim missing")
	}
	complete := command.Command{Kind: command.CompleteConnectionTest, Principal: interaction, Mutation: value.Mutation{IdempotencyKey: "route-test-complete"},
		Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: true}}
	if _, err := service.Execute(ctx, complete); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("wrong test workload with exact fence: %v", err)
	}
	complete.Principal = generic
	if result, err := service.Execute(ctx, complete); err != nil || result.Connection == nil || result.Connection.State != "CONNECTED" {
		t.Fatalf("complete test: %v", err)
	}
	if _, err := service.Execute(ctx, complete); err != nil {
		t.Fatalf("complete test replay: %v", err)
	}
	complete.Principal = interaction
	if _, err := service.Execute(ctx, complete); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("cross-workload test receipt replay: %v", err)
	}
}
