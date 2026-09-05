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
}
