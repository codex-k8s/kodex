package platform

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
)

func testIntegrationGrantSelectors(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, project entity.Project, agent entity.Agent, connection entity.IntegrationConnection) {
	t.Helper()
	input := query.IntegrationCandidates{Purpose: "USE", Context: query.IntegrationCandidateContext{ProjectRef: project.Ref, RecipientKind: "AGENT", RecipientRef: agent.Ref, CapabilityKey: "synthetic.journal.write"}, Filter: query.Filter{Page: query.Page{Size: 1}}}
	first, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, input)
	if err != nil || first.Total != 2 || len(first.Items) != 1 || first.NextPageToken == "" || !first.Items[0].Usable || first.Items[0].Grantable {
		t.Fatalf("USE page total=%d items=%d err=%v", first.Total, len(first.Items), err)
	}
	input.Filter.Page.Token = first.NextPageToken
	second, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, input)
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].ConnectionRef == first.Items[0].ConnectionRef || second.NextPageToken != "" {
		t.Fatalf("USE next page: %+v %v", second, err)
	}
	input.Filter.Query = "changed"
	if _, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, input); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("cursor filter mismatch: %v", err)
	}
	projects, err := service.ListIntegrationGrantProjectCandidates(ctx, owner, query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref}, Filter: query.Filter{Query: project.Name}})
	if err != nil || projects.Total != 1 || len(projects.Items) != 1 || projects.Items[0].ProjectRef != project.Ref {
		t.Fatalf("projects: %+v %v", projects, err)
	}
	recipients, err := service.ListIntegrationGrantRecipientCandidates(ctx, owner, query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref, ProjectRef: project.Ref, RecipientKind: "AGENT"}, Filter: query.Filter{Query: agent.Name}})
	if err != nil || recipients.Total != 1 || len(recipients.Items) != 1 || recipients.Items[0].RecipientRef != agent.Ref {
		t.Fatalf("recipients: %+v %v", recipients, err)
	}
	capInput := query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref, ProjectRef: project.Ref, RecipientKind: "AGENT", RecipientRef: agent.Ref}}
	capabilities, err := service.ListIntegrationGrantCapabilityCandidates(ctx, owner, capInput)
	if err != nil || capabilities.Total != 2 || len(capabilities.Items) != 2 || capabilities.Pins.ConnectionVersion < 1 || capabilities.Pins.RecipientVersion != agent.Version {
		t.Fatalf("capabilities: %+v %v", capabilities, err)
	}
	capInput.Filter.Query = "%"
	literal, err := service.ListIntegrationGrantCapabilityCandidates(ctx, owner, capInput)
	if err != nil || literal.Total != 0 || len(literal.Items) != 0 {
		t.Fatalf("literal search: %+v %v", literal, err)
	}
}

func testIntegrationGrantRevocation(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, project entity.Project, agent entity.Agent, connection entity.IntegrationConnection) {
	t.Helper()
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	pinTx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pinTx.Exec(ctx, `UPDATE control_plane.integration_connections SET definition_digest=repeat('0',64),version=version+1 WHERE organization_id=$1::uuid AND ref=$2`, ownerScope.organizationID, connection.Ref); err != nil {
		_ = pinTx.Rollback(ctx)
		t.Fatalf("stale package fixture: %v", err)
	}
	var staleCount int64
	err = pinTx.QueryRow(ctx, `SELECT count(*) FROM control_plane.integration_grant_admission($1::uuid,$2::uuid,NULL,$3,$4,'AGENT',$5,'','GRANT',NULL)`, ownerScope.organizationID, ownerScope.actorID, connection.Ref, project.Ref, agent.Ref).Scan(&staleCount)
	_ = pinTx.Rollback(ctx)
	if err != nil || staleCount != 0 {
		t.Fatalf("stale package counted as grantable: %d %v", staleCount, err)
	}
	proof := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000004633", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Grant candidate manager", CallerWorkload: "control-api-gateway", Operation: "platform.query.integration-grant-candidates.connections.list"}
	if _, err := repository.ResolveProofAuthority(ctx, proof); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound candidate: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: proof.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("candidate subject: %v", err)
	}
	var connectionBinding entity.AccessBinding
	for _, assignment := range []struct {
		key, kind, ref, project string
		permissions             []string
	}{
		{"connection", "INTEGRATION", connection.Ref, "", []string{"integration.view", "integration.manage"}},
		{"agent", "AGENT", agent.Ref, project.Ref, []string{"agent.view", "agent.manage"}},
		{"project", "PROJECT", project.Ref, project.Ref, []string{"project.view"}},
	} {
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-role-" + assignment.key}, Payload: command.AccessRoleInput{Name: "Selector " + assignment.key, PermissionKeys: assignment.permissions, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Selector exact authority fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-binding-" + assignment.key}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: assignment.project, ResourceKind: assignment.kind, ResourceRef: assignment.ref}}})
		if err != nil {
			t.Fatal(err)
		}
		if assignment.key == "connection" {
			connectionBinding = *binding.AccessBinding
		}
	}
	actor := resolvedTestPrincipal(t, ctx, repository, proof, "control-api-gateway")
	generic, _, err := service.ListIntegrationConnections(ctx, actor, query.Filter{})
	if err != nil || len(generic) != 1 || generic[0].Ref != connection.Ref {
		t.Fatalf("generic connection read scope: count=%d err=%v", len(generic), err)
	}
	single, err := service.GetIntegrationConnection(ctx, actor, connection.Ref)
	if err != nil || !contains(single.NextActions, "MANAGE_GRANTS") || !contains(generic[0].NextActions, "MANAGE_GRANTS") || len(single.Grants) != 1 {
		t.Fatalf("single/list connection authority parity: actions=%v grants=%d err=%v", single.NextActions, len(single.Grants), err)
	}
	if generic, _, err := service.ListIntegrationConnections(ctx, actor, query.Filter{Query: "%"}); err != nil || len(generic) != 0 {
		t.Fatalf("generic literal query: count=%d err=%v", len(generic), err)
	}
	list, err := service.ListIntegrationGrantConnectionCandidates(ctx, actor, query.IntegrationCandidates{Purpose: "GRANT"})
	if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].ConnectionRef != connection.Ref {
		t.Fatalf("exact candidate visibility: %+v %v", list, err)
	}
	fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	grant := command.Command{Kind: command.ChangeIntegrationGrant, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "selector-authorized-grant", ExpectedVersion: &fresh.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "synthetic.journal.read", Enabled: true}}
	if _, err := service.Execute(ctx, grant); err != nil {
		t.Fatalf("fresh candidate grant: %v", err)
	}
	if _, err := service.Execute(ctx, grant); err != nil {
		t.Fatalf("authorized receipt replay: %v", err)
	}
	foreign := actor
	foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
	foreignReplay := grant
	foreignReplay.Principal = foreign
	if _, err := service.Execute(ctx, foreignReplay); err == nil {
		t.Fatal("foreign tenant replayed grant receipt")
	}
	if _, err := service.ListIntegrationGrantConnectionCandidates(ctx, foreign, query.IntegrationCandidates{Purpose: "GRANT"}); err == nil {
		t.Fatal("foreign tenant received candidate count")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-revoke", ExpectedVersion: &connectionBinding.Version}, Payload: command.AccessBindingInput{BindingRef: connectionBinding.Ref}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, grant); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked grant authority replay: %v", err)
	}
	list, err = service.ListIntegrationGrantConnectionCandidates(ctx, actor, query.IntegrationCandidates{Purpose: "GRANT"})
	if err != nil || list.Total != 0 || len(list.Items) != 0 {
		t.Fatalf("revoked candidate count: %+v %v", list, err)
	}
	fresh, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-cleanup-grant", ExpectedVersion: &fresh.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "synthetic.journal.read", Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	unavailable, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-unavailable"}, Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Selector unavailable connection", PublicConfiguration: map[string]any{"journal": "selector-unavailable"}}})
	if err != nil {
		t.Fatal(err)
	}
	stale := int64(999)
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-unavailable-grant", ExpectedVersion: &stale}, Payload: command.IntegrationGrantInput{ConnectionRef: unavailable.Connection.Ref, AgentRef: agent.Ref, CapabilityKey: "synthetic.journal.read", Enabled: true}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("unavailable connection before OCC: %v", err)
	}
	projection, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, query.IntegrationCandidates{Purpose: "GRANT", Filter: query.Filter{Query: unavailable.Connection.Name}})
	if err != nil || projection.Total != 1 || len(projection.Items) != 1 || projection.Items[0].Grantable || projection.Items[0].Reason != "CONNECTION_UNAVAILABLE" {
		t.Fatalf("unavailable grant candidate: %+v %v", projection, err)
	}
}

func testIntegrationGrantWorkflow(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, project entity.Project, agent entity.Agent, connection entity.IntegrationConnection) {
	t.Helper()
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-workflow-capability", ExpectedVersion: &agent.Version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.run.launch", Enabled: true}}); err != nil {
		t.Fatalf("selector workflow prerequisite: %v", err)
	}
	draft := entity.WorkflowVersion{Ref: "draft", Name: "Selector workflow", Purpose: "Exact stage intersection", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Bounded result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "execute", Position: 1, Name: "Execute", AgentRef: agent.Ref, Instructions: "Produce bounded result", ExpectedResult: "One result", TimeoutSeconds: 60, RequiredCapabilityKeys: []string{"platform.run.launch"}}}}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil {
		t.Fatal(err)
	}
	input := query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref, ProjectRef: project.Ref, RecipientKind: "WORKFLOW"}, Filter: query.Filter{Query: draft.Name}}
	recipients, err := service.ListIntegrationGrantRecipientCandidates(ctx, owner, input)
	if err != nil || recipients.Total != 1 || len(recipients.Items) != 1 || !recipients.Items[0].Grantable || recipients.Items[0].RecipientRef != created.Workflow.Ref {
		t.Fatalf("Workflow candidate: %+v %v", recipients, err)
	}
	fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-workflow-grant", ExpectedVersion: &fresh.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connection.Ref, WorkflowRef: created.Workflow.Ref, CapabilityKey: "synthetic.journal.read", Enabled: true}}); err != nil {
		t.Fatalf("Workflow grant owner transition: %v", err)
	}
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-workflow-validate", ExpectedVersion: &created.Workflow.Version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil {
		t.Fatalf("selector workflow validation: %v", err)
	}
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "selector-workflow-publish", ExpectedVersion: &validated.Workflow.Version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil {
		t.Fatalf("selector workflow publication: %v", err)
	}
	use := query.IntegrationCandidates{Purpose: "USE", Context: query.IntegrationCandidateContext{ProjectRef: project.Ref, RecipientKind: "AGENT", RecipientRef: agent.Ref, CapabilityKey: "synthetic.journal.write", WorkflowRef: published.Workflow.Ref, StepKey: "execute"}}
	result, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, use)
	if err != nil || result.Total != 0 || len(result.Items) != 0 || result.Pins.WorkflowRevisionRef == "" {
		t.Fatalf("Workflow excluded capability: %+v %v", result, err)
	}
	use.Context.StepKey = "unknown"
	if _, err := service.ListIntegrationGrantConnectionCandidates(ctx, owner, use); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("unknown stage: %v", err)
	}
}
