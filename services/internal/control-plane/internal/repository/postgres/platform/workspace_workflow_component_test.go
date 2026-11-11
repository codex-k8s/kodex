package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestWorkflowWorkspaceOwnerComponent requires the pinned coordinator to pass the same full
// writer/checkpoint/restore lifecycle as an employee; physical Pod and storage facts are fixtures.
// TestWorkflowWorkspaceOwnerComponent проводит закреплённого координатора через полный
// writer/checkpoint/restore lifecycle сотрудника; physical Pod и storage facts — fixtures.
func TestWorkflowWorkspaceOwnerComponent(t *testing.T) {
	testWorkspaceWriterOwnerComponent(t, true)
}

// testWorkflowWorkspaceVersionConflict publishes a legitimate successor and proves that its
// same-session run cannot reuse or replace the previous version's workspace before any volume task.
// testWorkflowWorkspaceVersionConflict публикует допустимого successor и доказывает, что его
// same-session run не переиспользует и не заменяет workspace прежней версии до любого volume task.
func testWorkflowWorkspaceVersionConflict(t *testing.T, ctx context.Context, repository *Repository,
	service *platformservice.Service, owner value.Principal, workflowRef string,
	binding *runtimecontract.WorkspaceStorageBinding,
	execute func(command.Kind, value.Principal, string, any) command.Result,
) {
	t.Helper()
	workflow, err := service.GetWorkflow(ctx, owner, workflowRef)
	if err != nil || workflow.Published == nil {
		t.Fatal("read workflow before successor publication", err)
	}
	previousVersion := workflow.Published.Ref
	draft := *workflow.Published
	draft.Name = "Новая версия процесса"
	for _, kind := range []command.Kind{command.UpdateWorkflow, command.ValidateWorkflow, command.PublishWorkflow} {
		input := command.WorkflowInput{Ref: workflow.Ref}
		if kind == command.UpdateWorkflow {
			input.Draft = &draft
		}
		result, updateErr := service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "workflow-workspace-successor-" + string(kind), ExpectedVersion: &workflow.Version}, Payload: input})
		if updateErr != nil || result.Workflow == nil {
			t.Fatal("publish legitimate workflow successor", updateErr)
		}
		workflow = *result.Workflow
	}
	if workflow.Published == nil || workflow.Published.Ref == previousVersion {
		t.Fatal("workflow successor did not receive a new immutable version")
	}
	run := execute(command.LaunchRun, owner, "successor-launch", command.LaunchRunInput{
		ProjectRef: binding.Scope.ProjectRef, SessionRef: binding.Scope.SessionRef, Title: "Новый workflow pin",
		Task: "Проверить запрет подмены рабочего каталога", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin successor workspace inspection", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var current scope
	var candidate claimableExecution
	if err := tx.QueryRow(ctx, `SELECT workspace.organization_id::text,workspace.project_id::text,
  workspace.session_id::text,workspace.agent_id::text,run.id::text
  FROM control_plane.session_workspace_storage workspace
  JOIN control_plane.runs run ON run.session_id=workspace.session_id AND run.organization_id=workspace.organization_id
  WHERE workspace.ref=$1 AND run.ref=$2`, binding.WorkspaceRef, run.Ref).Scan(
		&current.organizationID, &candidate.projectID, &candidate.sessionID, &candidate.agentID, &candidate.runID); err != nil {
		t.Fatal("read owner-selected successor scope", err)
	}
	prepared, ready, err := repository.prepareSessionWorkspaceTx(ctx, tx, current, candidate)
	if prepared != nil || ready || !errors.Is(err, errs.ErrConflict) {
		t.Fatal("new workflow version reused or replaced an existing workspace", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("close successor workspace inspection", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workflow-workspace-successor-cancel", ExpectedVersion: &run.Version},
		Payload:  command.RunCommandInput{RunRef: run.Ref}}); err != nil {
		t.Fatal("cancel blocked successor fixture", err)
	}
}

// testWorkspaceWorkflowTarget publishes one coordinator and one specialist through owner
// commands and returns the immutable version expected by the workspace fixture.
// testWorkspaceWorkflowTarget публикует координатора и специалиста штатными owner commands
// и возвращает immutable version, ожидаемую workspace fixture.
func testWorkspaceWorkflowTarget(t *testing.T, ctx context.Context, service *platformservice.Service,
	owner value.Principal, projectRef string, coordinator entity.Agent,
	execute func(command.Kind, value.Principal, string, any) command.Result,
) (entity.RunTarget, string) {
	t.Helper()
	changed, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "workflow-workspace-delegate", ExpectedVersion: &coordinator.Version},
		Payload:  command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}})
	if err != nil || changed.Agent == nil {
		t.Fatal("configure coordinator delegation", err)
	}
	specialist := createLifecycleAgent(t, ctx, service, owner, projectRef, "workflow-workspace-specialist", "Исполнитель разработки")
	draft := entity.WorkflowVersion{Name: "Процесс с устойчивым каталогом", Purpose: "Проверить workspace координатора",
		CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Проверенный результат",
		ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "implement", Position: 1, Name: "Разработка",
			AgentRef: specialist.Ref, Instructions: "Подготовить исходники и результаты тестов", ExpectedResult: "Проверенные исходники", TimeoutSeconds: 900}}}
	workflow := execute(command.CreateWorkflow, owner, "workflow-create", command.WorkflowInput{ProjectRef: projectRef,
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}).Workflow
	for _, kind := range []command.Kind{command.ValidateWorkflow, command.PublishWorkflow} {
		result, workflowErr := service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "workflow-workspace-" + string(kind), ExpectedVersion: &workflow.Version},
			Payload:  command.WorkflowInput{Ref: workflow.Ref}})
		if workflowErr != nil || result.Workflow == nil {
			t.Fatal("publish workflow workspace fixture", workflowErr)
		}
		workflow = result.Workflow
	}
	if workflow.Published == nil {
		t.Fatal("workflow publication returned no immutable version")
	}
	return entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, workflow.Published.Ref
}

// testWorkflowWorkspaceScope verifies the persisted immutable version and rejects another
// employee, a missing pin and direct removal of the pin without changing the real workspace.
// testWorkflowWorkspaceScope проверяет сохранённую immutable version и отказ другому
// сотруднику, отсутствующему pin и прямому удалению pin без изменения рабочего workspace.
func testWorkflowWorkspaceScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	binding *runtimecontract.WorkspaceStorageBinding, expectedVersionRef string,
) {
	t.Helper()
	var versionRef string
	var coordinatorAllowed, specialistDenied, missingPinDenied bool
	if err := pool.QueryRow(ctx, `SELECT version.ref,
  control_plane.workspace_session_scope_matches(workspace.session_id,workspace.agent_id,workspace.workflow_version_id),
  NOT control_plane.workspace_session_scope_matches(workspace.session_id,specialist.id,workspace.workflow_version_id),
  NOT control_plane.workspace_session_scope_matches(workspace.session_id,workspace.agent_id,NULL)
  FROM control_plane.session_workspace_storage workspace
  JOIN control_plane.workflow_versions version ON version.id=workspace.workflow_version_id
  JOIN control_plane.agents specialist ON specialist.project_id=workspace.project_id AND specialist.id<>workspace.agent_id
    AND specialist.organization_id=workspace.organization_id
  WHERE workspace.ref=$1`, binding.WorkspaceRef).Scan(&versionRef, &coordinatorAllowed, &specialistDenied, &missingPinDenied); err != nil {
		t.Fatal("read immutable workflow workspace scope", err)
	}
	if versionRef != expectedVersionRef || !coordinatorAllowed || !specialistDenied || !missingPinDenied {
		t.Fatal("workspace accepted another employee or lost its pinned workflow version")
	}
	_, err := pool.Exec(ctx, `UPDATE control_plane.session_workspace_storage
  SET workflow_version_id=NULL,version=version+1 WHERE ref=$1`, binding.WorkspaceRef)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" || databaseError.Message != "session workspace scope is invalid" {
		t.Fatal("workspace guard did not reject removal of the workflow pin")
	}
}
