package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testPromptContextPreview(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-preview-project"}, Payload: command.ProjectInput{Name: "Preview project", Purpose: "Verify owner prompt context", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "prompt-preview-agent", "Preview agent")
	context := query.PromptPreviewContext{ExpectedAgentVersion: agent.Version}
	first, err := service.PreviewPromptTemplateWithContext(ctx, owner, `Agent {{.agent.name}} {{slot "PURPOSE"}}`, "AGENT", agent.Ref, false, context, "")
	if err != nil || first.ServiceTemplateRevision != promptservice.ServiceTemplateRevision || first.ContextPin.AgentRef != agent.Ref || first.ContextPin.RuntimeConfigurationRef == "" {
		t.Fatalf("agent preview: %#v err=%v", first.ContextPin, err)
	}
	second, err := service.PreviewPromptTemplateWithContext(ctx, owner, `Agent {{.agent.name}} {{slot "PURPOSE"}}`, "AGENT", agent.Ref, false, context, first.ContextPin.Digest)
	if err != nil || first.Digest != second.Digest {
		t.Fatalf("repeat preview: %v", err)
	}
	if strings.Contains(first.SafePrompt, "Preview agent") {
		t.Fatal("safe preview leaked agent value")
	}
	if _, err := service.PreviewPromptTemplateWithContext(ctx, owner, "Agent", "AGENT", agent.Ref, false, context, strings.Repeat("f", 64)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale context accepted: %v", err)
	}
	context.ExpectedAgentVersion++
	if _, err := service.PreviewPromptTemplateWithContext(ctx, owner, "Agent", "AGENT", agent.Ref, false, context, ""); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale agent accepted: %v", err)
	}
	draft := entity.WorkflowVersion{Name: "Preview workflow", Purpose: "Verify rendered stage", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "A bounded result", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "analyze", Position: 1, Name: "Analyze", AgentRef: agent.Ref, Instructions: "Analyze {{.project.name}}.", ExpectedResult: "Result by {{.agent.name}}.", TimeoutSeconds: 900}}}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-preview-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil {
		t.Fatal(err)
	}
	stageContext := query.PromptPreviewContext{WorkflowRevisionRef: created.Workflow.Draft.Ref, WorkflowStageKey: "analyze", ExpectedWorkflowVersion: created.Workflow.Version}
	stage, err := service.PreviewPromptTemplateWithContext(ctx, owner, `{{slot "PURPOSE"}} {{slot "EXPECTED_RESULT"}}`, "WORKFLOW_STAGE", created.Workflow.Ref, false, stageContext, "")
	if err != nil || !strings.Contains(stage.Prompt, "Analyze Preview project.") || !strings.Contains(stage.Prompt, "Result by Preview agent.") || stage.ContextPin.WorkflowStageKey != "analyze" {
		t.Fatalf("stage preview: prompt=%q err=%v", stage.Prompt, err)
	}
	if strings.Contains(stage.SafePrompt, "Preview project") {
		t.Fatal("stage safe preview leaked contextual value")
	}
	stageContext.AgentRef = "agt_missingpreview"
	if _, err := service.PreviewPromptTemplateWithContext(ctx, owner, "Agent", "WORKFLOW_STAGE", created.Workflow.Ref, false, stageContext, ""); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("foreign stage agent accepted: %v", err)
	}
	staleScope := testPromptDeclaredScope(t, ctx, service, owner, agent)
	testPromptVariableContextPin(t, ctx, repository, service, owner, agent.Ref)
	version := staleScope.ManagedConfiguration.Version
	_, err = service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scoped-stale", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: staleScope.ManagedConfiguration.Ref, RevisionRef: staleScope.ManagedRevision.Ref}})
	if !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale declared scope was accepted: %v", err)
	}
}

func testPromptDeclaredScope(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, agent entity.Agent) command.Result {
	t.Helper()
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "prompt-scoped-draft"}, Payload: command.ManagedConfigurationInput{ProjectRef: agent.ProjectRef, Name: "Contextual instructions", ContentFormat: "TEXT", Content: `Instructions for {{.agent.name}}. {{slot "PURPOSE"}}`,
			PromptScope: &command.PromptTemplateScopeInput{TargetKind: "AGENT", TargetRef: agent.Ref, TemplateKind: "INSTRUCTIONS"}}})
	if err != nil || draft.ManagedRevision == nil || draft.ManagedRevision.PromptScope == nil {
		t.Fatalf("create declared prompt scope: %v", err)
	}
	version := draft.ManagedConfiguration.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scoped-validate", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: draft.ManagedConfiguration.Ref, RevisionRef: draft.ManagedRevision.Ref}})
	if err != nil || validated.ManagedRevision == nil || validated.ManagedRevision.State != "VALID" || validated.ManagedRevision.PromptScope == nil {
		t.Fatalf("validate declared prompt scope: %v", err)
	}
	version = validated.ManagedConfiguration.Version
	published, err := executePromptPublicationFixture(t, ctx, service, command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scoped-publish", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: draft.ManagedConfiguration.Ref, RevisionRef: draft.ManagedRevision.Ref}})
	if err != nil || published.ManagedRevision == nil || published.ManagedRevision.PromptScope == nil || published.ManagedRevision.PromptScope.ContextPin.Digest != draft.ManagedRevision.PromptScope.ContextPin.Digest {
		t.Fatalf("publish changed declared scope: %v", err)
	}
	version = published.ManagedConfiguration.Version
	saved, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scoped-new-draft", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: published.ManagedConfiguration.Ref, ProjectRef: agent.ProjectRef, Name: "Contextual instructions", ContentFormat: "TEXT", Content: "Updated instructions with declared context.",
			PromptScope: &command.PromptTemplateScopeInput{TargetKind: "AGENT", TargetRef: agent.Ref, TemplateKind: "INSTRUCTIONS"}}})
	if err != nil || saved.ManagedRevision == nil {
		t.Fatalf("save fresh declared scope: %v", err)
	}
	for _, test := range []struct{ key, content, state, kind string }{
		{"files", `{{.project.files_count}}`, "INVALID", "INSTRUCTIONS"},
		{"late", `Run {{.run.ref}}`, "VALID", "INSTRUCTIONS"},
		{"continuation", `Continue {{.turn.ref}}`, "VALID", "CONTINUATION"},
	} {
		created, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scope-availability-" + test.key}, Payload: command.ManagedConfigurationInput{ProjectRef: agent.ProjectRef, Name: "Scope " + test.key, ContentFormat: "TEXT", Content: test.content, PromptScope: &command.PromptTemplateScopeInput{TargetKind: "AGENT", TargetRef: agent.Ref, TemplateKind: test.kind}}})
		if err != nil {
			t.Fatal(err)
		}
		payload := command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}
		checked, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scope-check-" + test.key, ExpectedVersion: &created.ManagedConfiguration.Version}, Payload: payload})
		if err != nil || checked.ManagedRevision == nil || checked.ManagedRevision.State != test.state || len(checked.ManagedRevision.ValidationDiagnostics) == 0 {
			t.Fatalf("scope availability %s: state=%v err=%v", test.key, checked.ManagedRevision, err)
		}
		published, err := executePromptPublicationFixture(t, ctx, service, command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-scope-publish-" + test.key, ExpectedVersion: &checked.ManagedConfiguration.Version}, Payload: payload})
		if test.state == "INVALID" {
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("unavailable scope published: %v", err)
			}
		} else if err != nil || published.ManagedRevision == nil || published.ManagedRevision.State != "PUBLISHED" {
			t.Fatalf("valid late runtime scope did not publish: %v", err)
		}
	}
	return saved
}

func testPromptVariableContextPin(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, agentRef string) {
	t.Helper()
	filter := query.Filter{Page: query.Page{Size: 1}, TemplateContext: &query.TemplateVariableContext{TargetKind: "AGENT", TargetRef: agentRef}}
	first, err := service.ListPromptContextVariables(ctx, owner, filter)
	if err != nil || len(first.Variables) != 1 || first.Total < 30 || first.ContextPin.Digest == "" || first.NextPageToken == "" {
		t.Fatalf("context catalog: %v", err)
	}
	filter.Page.Token = first.NextPageToken
	second, err := service.ListPromptContextVariables(ctx, owner, filter)
	if err != nil || len(second.Variables) != 1 || second.Variables[0].Name == first.Variables[0].Name || second.Total != first.Total || second.ContextPin.Digest != first.ContextPin.Digest {
		t.Fatalf("context catalog next page: %v", err)
	}
	filter.Query = "files"
	if _, err := service.ListPromptContextVariables(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("changed query reused cursor: %v", err)
	}
	filter.Page = query.Page{Size: 100}
	files, err := service.ListPromptContextVariables(ctx, owner, filter)
	if err != nil || len(files.Variables) == 0 {
		t.Fatalf("file catalog: %v", err)
	}
	for _, item := range files.Variables {
		if item.Available {
			t.Fatalf("unselected file family available: %s", item.Name)
		}
	}
	filter.Query = ""
	filter.Page = query.Page{Size: 1, Token: first.NextPageToken}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.agents SET version=version+1 WHERE ref=$1`, agentRef); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListPromptContextVariables(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("changed agent reused catalog cursor: %v", err)
	}
	filter.Page.Token = ""
	filter.TemplateContext.ExpectedContextDigest = first.ContextPin.Digest
	if _, err := service.ListPromptContextVariables(ctx, owner, filter); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("changed agent reused explicit pin: %v", err)
	}
}
