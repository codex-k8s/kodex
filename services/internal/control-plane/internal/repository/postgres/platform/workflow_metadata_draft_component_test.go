package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workflowMetadataDraftCounts reads only synthetic fixture cardinalities to prove replay and rollback.
// workflowMetadataDraftCounts читает лишь cardinalities синтетической оснастки для доказательства replay и rollback.
//
//go:embed testdata/sql/workflow_metadata_draft_counts.sql
var workflowMetadataDraftCounts string

// TestWorkflowMetadataDraftComponent follows the real metadata-only UI payload under the API caller,
// including actor action readback, Get/List, strict execution gates, editing/publication and empty archive.
// It uses the existing isolated migrated database fixture and never claims an execution or contacts a provider.
// TestWorkflowMetadataDraftComponent проводит настоящий metadata-only UI payload от API caller,
// включая actor action readback, Get/List, строгие execution gates, редактирование/публикацию и пустой archive.
// Использует прежнюю изолированную мигрированную БД, не захватывает execution и не обращается к провайдеру.
func TestWorkflowMetadataDraftComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated workflow metadata database")
	}
	defer pool.Close()
	r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap isolated workflow metadata fixture")
	}
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.workflows.create"}, "control-api-gateway")
	if owner.CallerWorkload != "control-api-gateway" {
		t.Fatal("fixture would bypass API action readback")
	}
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal("construct workflow metadata service")
	}
	mutate := func(actor value.Principal, kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "metadata-" + key, ExpectedVersion: version}, Payload: payload})
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := mutate(owner, kind, key, version, payload)
		if err != nil {
			t.Fatalf("workflow fixture command %s failed: not_found=%t invalid=%t unavailable=%t conflict=%t version_mismatch=%t", kind, errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrInvalid), errors.Is(err, errs.ErrUnavailable), errors.Is(err, errs.ErrConflict), errors.Is(err, errs.ErrVersionMismatch))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Metadata workflow", Language: "ru"}).Project
	other := execute(command.CreateProject, "other-project", nil, command.ProjectInput{Name: "Other metadata workflow", Language: "ru"}).Project
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "metadata-manager", "Менеджер проекта")
	otherCoordinator := createLifecycleAgent(t, ctx, service, owner, other.Ref, "metadata-other-manager", "Other manager")
	owner.ProjectRef = gateTestProjectID(t, ctx, r, owner, project.Ref)
	// Метаданные совпадают с телом формы: draft/steps отсутствуют, Project назначает route context.
	body, err := json.Marshal(map[string]any{"name": "Ответ поддержки в пределах срока согласования — QA1830", "purpose": "Подготовить короткий ответ по GitHub Issue 2 проекта «Журнал обращений», передать результат владельцу на согласование и запретить публикацию после тайм-аута. Изолированная проверка жизненного цикла процесса; истечение самостоятельного Human Gate этим не подменяется.", "coordinatorAgentRef": coordinator.Ref})
	if err != nil {
		t.Fatal("encode UI workflow metadata")
	}
	var input command.WorkflowInput
	if json.Unmarshal(body, &input) != nil || input.Draft != nil {
		t.Fatal("fixture inserted an executable draft instead of UI metadata")
	}
	input.ProjectRef = project.Ref
	created := execute(command.CreateWorkflow, "create", nil, input).Workflow
	if created == nil || created.ProjectRef != project.Ref || created.CoordinatorAgentRef != coordinator.Ref || created.State != "DRAFT" || created.Draft == nil || len(created.Draft.Steps) != 0 || created.Draft.TimeoutSeconds != 3600 || created.Published != nil || !contains(created.NextActions, "EDIT") || contains(created.NextActions, "PUBLISH") || contains(created.NextActions, "LAUNCH") || created.LaunchReadiness == nil || created.LaunchReadiness.AllowedToSubmit {
		t.Fatal("API workflow action response lost editable closed metadata draft")
	}
	counts := func(key string) [5]int64 {
		t.Helper()
		var count [5]int64
		if pool.QueryRow(ctx, workflowMetadataDraftCounts, project.Ref, "metadata-"+key).Scan(&count[0], &count[1], &count[2], &count[3], &count[4]) != nil {
			t.Fatal("read workflow fixture cardinality")
		}
		return count
	}
	if counts("create") != [5]int64{1, 1, 1, 1, 0} {
		t.Fatal("create did not commit one workflow/receipt/audit/event")
	}
	replayed := execute(command.CreateWorkflow, "create", nil, input).Workflow
	if replayed == nil || replayed.Ref != created.Ref || replayed.Version != created.Version || len(replayed.Draft.Steps) != 0 || counts("create") != [5]int64{1, 1, 1, 1, 0} {
		t.Fatal("metadata-only replay duplicated or changed owner result")
	}
	read, err := service.GetWorkflow(ctx, owner, created.Ref)
	if err != nil || read.Draft == nil || len(read.Draft.Steps) != 0 || !contains(read.NextActions, "EDIT") {
		t.Fatal("actor-scoped single read cannot open metadata draft")
	}
	list, _, err := service.ListWorkflows(ctx, owner, query.Filter{ProjectRef: project.Ref, Limit: 20})
	if err != nil || len(list) != 1 || list[0].Ref != created.Ref || len(list[0].Draft.Steps) != 0 {
		t.Fatal("actor-scoped list cannot expose metadata draft")
	}
	foreign := owner
	foreign.ProjectRef = gateTestProjectID(t, ctx, r, owner, other.Ref)
	if _, err := service.GetWorkflow(ctx, foreign, created.Ref); err == nil {
		t.Fatal("other project scope read a foreign workflow")
	}
	if items, _, err := service.ListWorkflows(ctx, foreign, query.Filter{ProjectRef: project.Ref, Limit: 20}); err == nil && len(items) != 0 {
		t.Fatal("other project list widened workflow eligibility")
	}
	for _, kind := range []command.Kind{command.ValidateWorkflow, command.PublishWorkflow} {
		if _, err := mutate(owner, kind, string(kind), &created.Version, command.WorkflowInput{Ref: created.Ref}); err == nil {
			t.Fatal("empty draft passed validation or publication")
		}
	}
	if _, err := mutate(owner, command.LaunchRun, "empty-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Empty workflow denied", Task: "Do not start an empty workflow", Target: entity.RunTarget{Type: "WORKFLOW", Ref: created.Ref}}); err == nil {
		t.Fatal("empty draft launched an execution")
	}
	bad := input
	bad.Name = "Foreign coordinator rejected"
	bad.CoordinatorAgentRef = otherCoordinator.Ref
	if _, err := mutate(owner, command.CreateWorkflow, "foreign-coordinator", nil, bad); err == nil {
		t.Fatal("cross-project coordinator was accepted")
	}
	if counts("foreign-coordinator") != [5]int64{1, 0, 1, 1, 0} {
		t.Fatal("failed create/validation/launch left a partial workflow or effects")
	}
	changed := input
	changed.Name = "Different idempotent intent"
	if _, err := mutate(owner, command.CreateWorkflow, "create", nil, changed); err == nil {
		t.Fatal("idempotency key accepted a different intent")
	}
	if counts("create") != [5]int64{1, 1, 1, 1, 0} {
		t.Fatal("failed replay changed workflow owner state")
	}
	// Обычный editor добавляет настоящий этап; update/validate/publish сохраняют прежние проверки.
	draft := *created.Draft
	draft.Steps = []entity.WorkflowStep{{Key: "answer", Position: 1, Name: "Подготовить ответ", AgentRef: coordinator.Ref, Instructions: "Подготовить короткий ответ владельцу.", ExpectedResult: "Проверенный ответ", TimeoutSeconds: 60}}
	updated := execute(command.UpdateWorkflow, "edit", &created.Version, command.WorkflowInput{Ref: created.Ref, Draft: &draft}).Workflow
	validated := execute(command.ValidateWorkflow, "validate-complete", &updated.Version, command.WorkflowInput{Ref: created.Ref}).Workflow
	published := execute(command.PublishWorkflow, "publish-complete", &validated.Version, command.WorkflowInput{Ref: created.Ref}).Workflow
	if published.Published == nil || len(published.Published.Steps) != 1 || !validWorkflowVersion(*published.Published) {
		t.Fatal("editor-to-published lifecycle failed after metadata creation")
	}
	archivedInput := input
	archivedInput.Name = "Пустой draft для архива"
	empty := execute(command.CreateWorkflow, "archive-create", nil, archivedInput).Workflow
	archived := execute(command.ArchiveWorkflow, "archive-empty", &empty.Version, command.WorkflowInput{Ref: empty.Ref}).Workflow
	if archived == nil || archived.State != "ARCHIVED" || archived.Draft == nil || len(archived.Draft.Steps) != 0 || contains(archived.NextActions, "EDIT") || contains(archived.NextActions, "LAUNCH") {
		t.Fatal("empty draft archive lost closed action readback")
	}
	archiveCounts := counts("archive-empty")
	// Терминальный ресурс скрывается прежним owner resolver до replay; новый reader не расширяет authority.
	if _, err := mutate(owner, command.ArchiveWorkflow, "archive-empty", &empty.Version, command.WorkflowInput{Ref: empty.Ref}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("archive replay widened terminal workflow eligibility: error_nil=%t not_found=%t unavailable=%t", err == nil, errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrUnavailable))
	}
	read, err = service.GetWorkflow(ctx, owner, empty.Ref)
	if err != nil || read.State != "ARCHIVED" || read.Draft == nil || len(read.Draft.Steps) != 0 || contains(read.NextActions, "EDIT") || contains(read.NextActions, "LAUNCH") || read.LaunchReadiness == nil || read.LaunchReadiness.AllowedToSubmit {
		t.Fatalf("archived workflow read lost its closed state: error_nil=%t not_found=%t unavailable=%t archived=%t draft_present=%t edit=%t launch=%t readiness_present=%t", err == nil, errors.Is(err, errs.ErrNotFound), errors.Is(err, errs.ErrUnavailable), read.State == "ARCHIVED", read.Draft != nil, contains(read.NextActions, "EDIT"), contains(read.NextActions, "LAUNCH"), read.LaunchReadiness != nil)
	}
	if counts("archive-empty") != archiveCounts {
		t.Fatal("closed terminal archive replay or read changed owner state")
	}
}
