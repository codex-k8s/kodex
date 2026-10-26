package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// TestProspectiveWorkflowPromptComponent checks prospective workflow prompt eligibility and
// immutable input provenance, distinguishing deferred preview values from required execution
// input.
// TestProspectiveWorkflowPromptComponent проверяет допустимость предварительного промпта workflow
// и происхождение неизменяемого ввода, отличая отложенные значения предпросмотра от обязательного
// ввода исполнения.
func TestProspectiveWorkflowPromptComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prospective-project"}, Payload: command.ProjectInput{Name: "Prospective preview", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, s, owner, project.Project.Ref, "prospective-agent", "Preview agent")
	draft := entity.WorkflowVersion{Name: "Prospective workflow", Purpose: "Preview before runtime inputs", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Bounded result", ResultSchema: map[string]any{},
		Inputs: []entity.WorkflowInputField{{Key: "field-001", Label: "Text", Type: "TEXT", Required: true}, {Key: "field-002", Label: "Long text", Type: "LONG_TEXT", Required: true}, {Key: "field-003", Label: "Number", Type: "NUMBER", Required: true}, {Key: "field-004", Label: "Boolean", Type: "BOOLEAN", Required: true}},
		Steps:  []entity.WorkflowStep{{Key: "step-019", Position: 1, Name: "Analyze", AgentRef: agent.Ref, Instructions: "Analyze the selected project.", ExpectedResult: "Bounded result", TimeoutSeconds: 900}}}
	created, err := s.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prospective-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil {
		t.Fatal(err)
	}
	selection := query.PromptPreviewContext{ExpectedWorkflowVersion: created.Workflow.Version, WorkflowRevisionRef: created.Workflow.Draft.Ref, WorkflowStageKey: "step-019"}
	readOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	filter := query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 100}, TemplateContext: &query.TemplateVariableContext{TargetKind: promptservice.TargetWorkflowStage, TargetRef: created.Workflow.Ref, Preview: selection}}
	catalog, err := s.ListPromptContextVariables(ctx, owner, filter)
	if err != nil || catalog.ContextPin.WorkflowStageKey != "step-019" {
		t.Fatalf("prospective catalog failed: %v", err)
	}
	found := false
	for _, item := range catalog.Variables {
		if item.Name == "input.values" {
			found = true
			if item.Available || item.Reason != "RUNTIME_CONTEXT_REQUIRED" {
				t.Fatal("missing runtime values were advertised as available")
			}
		}
	}
	if !found {
		t.Fatal("input.values descriptor is missing")
	}
	preview, err := s.PreviewPromptTemplateWithContext(ctx, owner, `{{slot "PURPOSE"}} {{slot "EXPECTED_RESULT"}}`, promptservice.TargetWorkflowStage, created.Workflow.Ref, false, selection, catalog.ContextPin.Digest)
	if err != nil || !preview.Complete || preview.ContextPin.Digest != catalog.ContextPin.Digest {
		t.Fatalf("prospective preview/catalog pin mismatch: %v", err)
	}
	validation, err := s.ValidatePromptTemplateWithContext(ctx, owner, `{{.input.values}}`, promptservice.TargetWorkflowStage, created.Workflow.Ref, selection, catalog.ContextPin.Digest)
	if err != nil || validation.Complete {
		t.Fatalf("missing input was materialized: %v", err)
	}
	if len(validation.Diagnostics) == 0 || validation.Diagnostics[0].Code != "RUNTIME_CONTEXT_REQUIRED" {
		t.Fatal("missing declared values have no unavailable diagnostic")
	}
	for _, input := range []map[string]any{{"field-003": "wrong type"}, {"unknown": "value"}, {"field-001": strings.Repeat("x", 4001)}, {"field-001": ""}} {
		invalid := selection
		invalid.Input = input
		if _, err := r.GetPromptPreviewContextSnapshot(ctx, readOwner, promptservice.TargetWorkflowStage, created.Workflow.Ref, invalid); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid supplied prospective input accepted: %v", err)
		}
	}
	partial := selection
	partial.Input = map[string]any{"field-003": float64(1)}
	partialSnapshot, err := r.GetPromptPreviewContextSnapshot(ctx, readOwner, promptservice.TargetWorkflowStage, created.Workflow.Ref, partial)
	if err != nil || partialSnapshot.ContextPin.Digest == catalog.ContextPin.Digest || partialSnapshot.UnavailableVariables["input.values.field-003"] != "" || partialSnapshot.UnavailableVariables["input.values.field-001"] != "RUNTIME_CONTEXT_REQUIRED" {
		t.Fatalf("partial values or immutable input digest mismatch: %v", err)
	}
	stale := selection
	stale.ExpectedWorkflowVersion++
	if _, err := r.GetPromptPreviewContextSnapshot(ctx, readOwner, promptservice.TargetWorkflowStage, created.Workflow.Ref, stale); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale workflow version accepted: %v", err)
	}
	foreign := filter
	foreign.ProjectRef = "prj_foreignpreview"
	if _, err := s.ListPromptContextVariables(ctx, owner, foreign); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign project catalog accepted: %v", err)
	}
	if _, err := r.GetPromptPreviewContextSnapshot(ctx, readOwner, promptservice.TargetWorkflowStage, "wfl_missingpreview", selection); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign or missing workflow exposed: %v", err)
	}
	if _, err := s.PreviewPromptTemplateWithContext(ctx, owner, "Stage", promptservice.TargetWorkflowStage, created.Workflow.Ref, false, selection, strings.Repeat("f", 64)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale digest accepted: %v", err)
	}
	current, err := r.resolveScope(ctx, readOwner)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := r.promptPreviewContextForActorTx(ctx, tx, current, current, promptservice.TargetWorkflowStage, created.Workflow.Ref, selection); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("actual lower materialization accepted missing required inputs: %v", err)
	}
	declared := selection
	declared.ScopeOnly = true
	declaredSnapshot, err := r.promptPreviewContextForActorTx(ctx, tx, current, current, promptservice.TargetWorkflowStage, created.Workflow.Ref, declared)
	if err != nil || declaredSnapshot.UnavailableVariables["input.values"] != "" {
		t.Fatalf("declared-template eligibility semantics changed: %v", err)
	}
	complete := selection
	complete.Input = map[string]any{"field-001": "Provided", "field-002": "Provided", "field-003": float64(1), "field-004": false}
	actual, err := r.promptPreviewContextForActorTx(ctx, tx, current, current, promptservice.TargetWorkflowStage, created.Workflow.Ref, complete)
	if err != nil {
		t.Fatalf("actual complete input rejected: %v", err)
	}
	prospective, err := r.GetPromptPreviewContextSnapshot(ctx, readOwner, promptservice.TargetWorkflowStage, created.Workflow.Ref, complete)
	if err != nil || actual.ContextPin.Digest != prospective.ContextPin.Digest || prospective.UnavailableVariables["input.values"] != "" {
		t.Fatalf("complete prospective/actual provenance differs: %v", err)
	}
}
