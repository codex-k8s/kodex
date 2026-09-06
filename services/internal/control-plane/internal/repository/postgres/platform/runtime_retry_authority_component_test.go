package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRetryTargetAuthority(t *testing.T, ctx context.Context, repository *Repository) {
	for _, kind := range []string{"AGENT", "WORKFLOW"} {
		t.Run(kind, func(t *testing.T) { testRetryTargetKindAuthority(t, ctx, repository, kind) })
	}
}

func testRetryTargetKindAuthority(t *testing.T, ctx context.Context, repository *Repository, kind string) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.retry"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(c command.Command) command.Result {
		t.Helper()
		c.Principal = owner
		c.Mutation.IdempotencyKey = kind + "-" + c.Mutation.IdempotencyKey
		r, e := service.Execute(ctx, c)
		if e != nil {
			t.Fatalf("%s: %v", c.Kind, e)
		}
		return r
	}
	project := execute(command.Command{Kind: command.CreateProject, Mutation: value.Mutation{IdempotencyKey: "retry-authority-project"}, Payload: command.ProjectInput{Name: "Retry authority " + kind, Language: "en"}}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, kind+"-retry-authority-agent", "Retry authority")
	target := entity.RunTarget{Type: kind, Ref: agent.Ref}
	permission := "agent.launch"
	if kind == "WORKFLOW" {
		draft := entity.WorkflowVersion{Name: "Retry workflow", Purpose: "Retry authority", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Bounded result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "step", Position: 1, Name: "Step", AgentRef: agent.Ref, Instructions: "Complete fixture.", ExpectedResult: "Fixture result", TimeoutSeconds: 900}}}
		workflow := execute(command.Command{Kind: command.CreateWorkflow, Mutation: value.Mutation{IdempotencyKey: "retry-workflow-create"}, Payload: command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}}).Workflow
		workflow = execute(command.Command{Kind: command.ValidateWorkflow, Mutation: value.Mutation{IdempotencyKey: "retry-workflow-validate", ExpectedVersion: &workflow.Version}, Payload: command.WorkflowInput{Ref: workflow.Ref}}).Workflow
		workflow = execute(command.Command{Kind: command.PublishWorkflow, Mutation: value.Mutation{IdempotencyKey: "retry-workflow-publish", ExpectedVersion: &workflow.Version}, Payload: command.WorkflowInput{Ref: workflow.Ref}}).Workflow
		target.Ref, permission = workflow.Ref, "workflow.launch"
	}
	run := execute(command.Command{Kind: command.LaunchRun, Mutation: value.Mutation{IdempotencyKey: "retry-authority-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Ref, Target: target, Task: "Retry only with current target permission."}}).Run
	cancelled := execute(command.Command{Kind: command.CancelRun, Mutation: value.Mutation{IdempotencyKey: "retry-authority-cancel", ExpectedVersion: &run.Version}, Payload: command.RunCommandInput{RunRef: run.Ref}}).Run
	reader := contextProjectReader(t, ctx, repository, service, owner, project.Ref, "RETRY_TARGET_"+kind)
	resolved, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	bind := func(key, permission string, target entity.AccessScope) entity.AccessBinding {
		t.Helper()
		key = kind + "-" + key
		role := execute(command.Command{Kind: command.CreateAccessRole, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: []string{permission}, AllowedScopes: []string{target.Kind}, ChangeComment: "Retry target fixture"}}).AccessRole
		return *execute(command.Command{Kind: command.CreateAccessBinding, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: resolved.ActorID, RoleVersionRef: role.CurrentVersion.Ref, Scope: target}}).AccessBinding
	}
	bind("retry-view", "run.view", entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	visible, err := service.GetRun(ctx, reader, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if contains(visible.NextActions, "RETRY") {
		t.Fatal("run view advertised target launch authority")
	}
	retry := command.Command{Kind: command.RetryRun, Principal: reader, Mutation: value.Mutation{IdempotencyKey: kind + "-retry-exact-authority", ExpectedVersion: &cancelled.Version}, Payload: command.RunCommandInput{RunRef: run.Ref}}
	denied := func(c command.Command) {
		t.Helper()
		if _, err := service.Execute(ctx, c); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("view-only retry: %v", err)
		}
	}
	denied(retry)
	stale := retry
	oldVersion := int64(1)
	stale.Mutation.ExpectedVersion = &oldVersion
	denied(stale)
	launchBinding := bind("retry-launch", permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: kind, ResourceRef: target.Ref, ProjectRef: project.Ref})
	accepted, err := service.Execute(ctx, retry)
	if err != nil || accepted.Run == nil {
		t.Fatalf("authorized retry: %v", err)
	}
	execute(command.Command{Kind: command.RevokeAccessBinding, Mutation: value.Mutation{IdempotencyKey: "retry-launch-revoke", ExpectedVersion: &launchBinding.Version}, Payload: command.AccessBindingInput{BindingRef: launchBinding.Ref}})
	denied(retry)
	execute(command.Command{Kind: command.CancelRun, Mutation: value.Mutation{IdempotencyKey: "retry-fixture-cleanup", ExpectedVersion: &accepted.Run.Version}, Payload: command.RunCommandInput{RunRef: accepted.Run.Ref}})
	continuation := command.Command{Kind: command.AddSessionTurn, Principal: reader, Mutation: value.Mutation{IdempotencyKey: kind + "-continuation-exact-authority"}, Payload: command.SessionTurnInput{SessionRef: accepted.Run.SessionRef, RunRef: accepted.Run.Ref, Task: "Continue with fresh target authority."}}
	denied(continuation)
	continuationBinding := bind("continuation-launch", permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: kind, ResourceRef: target.Ref, ProjectRef: project.Ref})
	continued, err := service.Execute(ctx, continuation)
	if err != nil || continued.Run == nil {
		t.Fatalf("authorized continuation: %v", err)
	}
	execute(command.Command{Kind: command.RevokeAccessBinding, Mutation: value.Mutation{IdempotencyKey: "continuation-launch-revoke", ExpectedVersion: &continuationBinding.Version}, Payload: command.AccessBindingInput{BindingRef: continuationBinding.Ref}})
	denied(continuation)
	withoutRun := continuation
	withoutRun.Payload = command.SessionTurnInput{SessionRef: accepted.Run.SessionRef, Task: "A session ref does not confer launch authority."}
	denied(withoutRun)
	execute(command.Command{Kind: command.CancelRun, Mutation: value.Mutation{IdempotencyKey: "continuation-fixture-cleanup", ExpectedVersion: &continued.Run.Version}, Payload: command.RunCommandInput{RunRef: continued.Run.Ref}})
}
