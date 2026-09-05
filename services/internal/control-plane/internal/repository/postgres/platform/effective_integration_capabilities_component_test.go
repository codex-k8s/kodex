package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func narrowedSyntheticPackageFixture(t *testing.T, repository *Repository) string {
	t.Helper()
	definition, err := integrationpackage.Parse(asJSON(repository.integrationDefinitions["synthetic"]))
	if err != nil {
		t.Fatal(err)
	}
	for index := range definition.Spec.Capabilities {
		capability := &definition.Spec.Capabilities[index]
		if capability.Key != "synthetic.journal.write" {
			continue
		}
		for index := range capability.InputFields {
			if capability.InputFields[index].Key == "value" {
				capability.InputFields[index].MaximumLength = 16
				return string(asJSON(definition))
			}
		}
	}
	t.Fatal("synthetic value constraint is absent")
	return ""
}

func testManagedRuntimeCapabilityAuthority(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, allowed entity.IntegrationConnection, definition integrationpackage.Package) {
	t.Helper()
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-project"}, Payload: command.ProjectInput{Name: "Managed capability scope", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "managed-cap-agent", "Managed capability agent")
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-other"}, Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Unrelated same-key connection", PublicConfiguration: map[string]any{"journal": "managed-cap-other"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-other-test", ExpectedVersion: &created.Connection.Version}, Payload: command.ConnectionInput{Ref: created.Connection.Ref}}); err != nil {
		t.Fatal(err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-tests.claim"}, "integration-gateway")
	claims, err := service.ClaimIntegrationConnectionTests(ctx, worker, "managed-cap-other-worker", 32)
	if err != nil {
		t.Fatal(err)
	}
	var other *entity.IntegrationConnection
	for _, claim := range claims {
		if stringMap(claim, "connectionRef") != created.Connection.Ref {
			continue
		}
		result, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "managed-cap-other-complete"}, Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: true}})
		if err != nil {
			t.Fatal(err)
		}
		other = result.Connection
	}
	if other == nil {
		t.Fatal("second connection readiness was not materialized")
	}
	for _, connection := range []entity.IntegrationConnection{allowed, *other} {
		if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-grant-" + connection.Ref, ExpectedVersion: &connection.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "synthetic.journal.write", Enabled: true}}); err != nil {
			t.Fatal(err)
		}
	}
	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000004632", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Exact connection writer", CallerWorkload: "control-api-gateway", Operation: "platform.query.agent-effective-capabilities.get"}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound writer: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("writer identity: %v", err)
	}
	for _, assignment := range []struct{ key, permission, kind, ref, project string }{{"agent", "agent.view", "AGENT", agent.Ref, project.Project.Ref}, {"connection", "integration.manage", "INTEGRATION", allowed.Ref, ""}} {
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-role-" + assignment.key}, Payload: command.AccessRoleInput{Name: "Managed capability " + assignment.key, PermissionKeys: []string{assignment.permission}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Exact capability fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "managed-cap-binding-" + assignment.key}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: assignment.project, ResourceKind: assignment.kind, ResourceRef: assignment.ref}}}); err != nil {
			t.Fatal(err)
		}
	}
	candidate := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	projection, err := service.GetAgentEffectiveCapabilities(ctx, candidate, agent.Ref, "", "", query.Filter{Query: "synthetic.journal.write"})
	if err != nil || projection.Total != 2 || len(projection.Items) != 2 {
		t.Fatalf("same-key projection: total=%d err=%v", projection.Total, err)
	}
	for _, item := range projection.Items {
		if item.ConnectionRef == allowed.Ref && !item.Grantable || item.ConnectionRef == other.Ref && (item.Grantable || item.Effective || item.Reason != capabilityActorDenied) {
			t.Fatal("same-key public connection authority escaped")
		}
	}
	resolved, err := repository.ResolvePrincipal(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, grants, err := repository.agentCapabilityAuthority(ctx, tx, current, project.Project.Ref, agent.Ref, nil)
	if err != nil || len(grants) != 1 || grants[0]["connectionRef"] != allowed.Ref || grants[0]["definitionDigest"] != definition.Digest {
		t.Fatalf("same-key runtime grants: count=%d err=%v", len(grants), err)
	}
	capability, _ := definition.Capability("synthetic.journal.write")
	expected, err := capability.InputSchema()
	if err != nil {
		t.Fatal(err)
	}
	baseline, _ := repository.integrationDefinitions["synthetic"].Capability("synthetic.journal.write")
	shipped, err := baseline.InputSchema()
	if err != nil {
		t.Fatal(err)
	}
	if string(expected) == string(shipped) || grants[0]["inputSchema"] != string(expected) {
		t.Fatal("runtime replaced narrowed package schema with shipped constraints")
	}
}
