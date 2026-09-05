package platform

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testEffectiveCapabilities(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-project"}, Payload: command.ProjectInput{Name: "Capability scope", Purpose: "Current authority intersection", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "effective-cap-agent", "Capability agent")
	other := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "effective-cap-other", "Hidden agent")
	projection, err := service.GetAgentEffectiveCapabilities(ctx, owner, agent.Ref, "", "", query.Filter{Page: query.Page{Size: 1}})
	if err != nil || projection.AgentRef != agent.Ref || projection.Total < 2 || projection.NextPageToken == "" || projection.RuntimeConfigurationRef == "" {
		t.Fatalf("owner capability projection: total=%d err=%v", projection.Total, err)
	}
	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000004631", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Capability scoped manager", CallerWorkload: "control-api-gateway", Operation: "platform.query.agent-effective-capabilities.get"}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound actor authorized: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("subject registration: %v", err)
	}
	bind := func(key string, permissions []string) entity.AccessBinding {
		t.Helper()
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Capability authority fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Project.Ref, ResourceKind: "AGENT", ResourceRef: agent.Ref}}})
		if err != nil {
			t.Fatal(err)
		}
		return *binding.AccessBinding
	}
	bind("effective-cap-manager", []string{"agent.view", "agent.manage"})
	launchBinding := bind("effective-cap-launcher", []string{"agent.launch"})
	candidate := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	read := func(p value.Principal) entity.AgentEffectiveCapabilities {
		t.Helper()
		result, err := service.GetAgentEffectiveCapabilities(ctx, p, agent.Ref, "", "", query.Filter{})
		if err != nil {
			t.Fatalf("exact agent projection: %v", err)
		}
		return result
	}
	capability := func(result entity.AgentEffectiveCapabilities, key string) entity.EffectiveCapability {
		t.Helper()
		for _, item := range result.Items {
			if item.Key == key {
				return item
			}
		}
		t.Fatalf("missing capability %s", key)
		return entity.EffectiveCapability{}
	}
	before := read(candidate)
	if !capability(before, "platform.run.launch").Grantable || capability(before, "platform.project.manage").Grantable {
		t.Fatal("scoped authority was replaced by a legacy role")
	}
	stale := before.AgentVersion + 100
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: candidate, Mutation: value.Mutation{IdempotencyKey: "effective-cap-escalation", ExpectedVersion: &stale}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.project.manage", Enabled: true}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("capability escalation was checked after OCC: %v", err)
	}
	for _, key := range []string{"platform.run.launch", "platform.project.manage"} {
		version := read(owner).AgentVersion
		if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-enable-" + key, ExpectedVersion: &version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: key, Enabled: true}}); err != nil {
			t.Fatalf("owner capability assignment: %v", err)
		}
	}
	before = read(candidate)
	if item := capability(before, "platform.project.manage"); !item.Requested || item.Effective || item.Reason != capabilityActorDenied {
		t.Fatal("agent capability expanded actor authority")
	}
	if _, err := service.GetAgentEffectiveCapabilities(ctx, candidate, other.Ref, "", "", query.Filter{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("hidden agent projection: %v", err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetAgentEffectiveCapabilities(ctx, candidate, assistant.Ref, "", "", query.Filter{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("system assistant accepted project authority: %v", err)
	}
	resolvedCandidate, err := repository.ResolvePrincipal(ctx, candidate)
	if err != nil {
		t.Fatalf("resolve runtime actor: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolvedCandidate)
	if err != nil {
		t.Fatal(err)
	}
	checkRuntime := func(expected bool) {
		t.Helper()
		tx, err := repository.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		keys, _, err := repository.agentCapabilityAuthority(ctx, tx, current, project.Project.Ref, agent.Ref, []string{"platform.run.launch", "platform.project.manage"})
		if err != nil || slices.Contains(keys, "platform.run.launch") != expected || slices.Contains(keys, "platform.project.manage") {
			t.Fatalf("fresh runtime authority: count=%d err=%v", len(keys), err)
		}
	}
	checkRuntime(true)
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-revoke", ExpectedVersion: &launchBinding.Version}, Payload: command.AccessBindingInput{BindingRef: launchBinding.Ref}}); err != nil {
		t.Fatal(err)
	}
	after := read(candidate)
	if after.Digest == before.Digest || capability(after, "platform.run.launch").Grantable {
		t.Fatal("revoked authority retained eligibility snapshot")
	}
	checkRuntime(false)

	draft := entity.WorkflowVersion{Ref: "draft", Name: "Capability workflow", Purpose: "Verify exact stage capabilities", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "One bounded result", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "execute", Position: 1, Name: "Execute", AgentRef: agent.Ref, Instructions: "Execute one bounded task", ExpectedResult: "One result", TimeoutSeconds: 60, RequiredCapabilityKeys: []string{"platform.run.launch"}}}}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil {
		t.Fatalf("create capability workflow: %v", err)
	}
	version := created.Workflow.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-workflow-validate", ExpectedVersion: &version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil || validated.Workflow.State != "VALID" {
		t.Fatalf("validate capability workflow: %v", err)
	}
	version = validated.Workflow.Version
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "effective-cap-workflow-publish", ExpectedVersion: &version}, Payload: command.WorkflowInput{Ref: created.Workflow.Ref}})
	if err != nil {
		t.Fatalf("publish capability workflow: %v", err)
	}
	stage, err := service.GetAgentEffectiveCapabilities(ctx, owner, agent.Ref, published.Workflow.Ref, "execute", query.Filter{})
	if err != nil || stage.WorkflowVersionRef == "" || !capability(stage, "platform.run.launch").Required || capability(stage, "platform.project.manage").Reason != capabilityWorkflowExcluded {
		t.Fatalf("workflow capability intersection: %v", err)
	}
	if _, err := service.GetAgentEffectiveCapabilities(ctx, candidate, agent.Ref, published.Workflow.Ref, "execute", query.Filter{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("hidden workflow projection: %v", err)
	}
	if _, err := service.GetAgentEffectiveCapabilities(ctx, owner, other.Ref, published.Workflow.Ref, "execute", query.Filter{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("wrong assigned agent projection: %v", err)
	}
}
