package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowCaptureParent refreshes the root actor and parent execution before capture continuation.
// queryWorkflowCaptureParent проверяет root actor и родительское исполнение перед продолжением capture.
//
//go:embed sql/workflow_dispatch__capture_parent.sql
var queryWorkflowCaptureParent string

// continueWorkflowCaptureTx resolves every explicit progress key from this completed capture and
// creates one fresh physical continuation. It never appends a business verdict or waives a missing PR.
// continueWorkflowCaptureTx разрешает каждый явный progress key из текущего capture и создаёт одно
// свежее физическое продолжение. Функция не принимает бизнес-исход и не отменяет требование PR.
func (repository *Repository) continueWorkflowCaptureTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, lease map[string]any, step entity.WorkflowStep, raw string) (bool, error) {
	declaration, err := workflowservice.DecodeCaptureProgress(raw, root.Version.ExecutionMode, step.ResultRequirements)
	if err != nil {
		return false, nil
	}
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, root.ID); err != nil {
		return true, err
	} else if expired {
		return true, errs.ErrConflict
	}
	for _, key := range declaration.ArtifactKeys {
		for _, requirement := range step.ResultRequirements {
			if requirement.Key != key {
				continue
			}
			_, found, err := resolveWorkflowArtifactTx(ctx, tx, current, lease, requirement, entity.WorkflowDeclaredResult{Key: key, Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}})
			if err != nil {
				return true, err
			}
			if !found {
				return false, nil
			}
		}
	}
	var agentID, name, role, sessionID, agentRef, task string
	var attempt int32
	if tx.QueryRow(ctx, queryWorkflowCaptureParent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": lease["nodeID"], "root_run_id": root.ID, "run_id": lease["runID"]}).Scan(&agentID, &attempt, &name, &role, &sessionID, &agentRef, &task) != nil {
		return true, errs.ErrConflict
	}
	task += "\nYour explicitly requested intermediate files have been captured. Read their exact revision and digest from the fresh file catalog before using integrations. Continue the assigned step; this is the same business attempt and no downstream step has been accepted. Return the final typed result declaration after all mandatory work finishes."
	if len(task) > 64<<10 {
		return true, errs.ErrInvalid
	}
	var lockedSession string
	var number int64
	if tx.QueryRow(ctx, queryRuntimeCallbackSelectParentSession, pgx.StrictNamedArgs{"organization_id": current.organizationID, "parent_run_id": lease["runID"]}).Scan(&lockedSession, &number) != nil || lockedSession != sessionID {
		return true, errs.ErrUnavailable
	}
	turnRef, err := newRef("trn")
	if err != nil {
		return true, err
	}
	var turnID string
	if tx.QueryRow(ctx, queryRuntimeCallbackInsertContinuationTurn, pgx.StrictNamedArgs{"turn_ref": turnRef, "organization_id": current.organizationID, "session_id": sessionID, "parent_run_id": lease["runID"], "turn_number": number, "agent_ref": agentRef, "content": task}).Scan(&turnID) != nil {
		return true, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCallbackUpdateSession, pgx.StrictNamedArgs{"session_id": sessionID}); err != nil {
		return true, errs.ErrUnavailable
	}
	nodeRef, err := newRef("nod")
	if err != nil {
		return true, err
	}
	var nodeID string
	if tx.QueryRow(ctx, queryRuntimeCallbackInsertContinuationNode, pgx.StrictNamedArgs{"node_ref": nodeRef, "organization_id": current.organizationID, "root_run_id": root.ID, "parent_run_id": lease["runID"], "parent_node_id": lease["nodeID"], "display_name": name, "role": role, "agent_id": agentID, "turn_id": turnID, "workflow_step_key": step.Key, "human_gate_after": false, "attempt": attempt + 1, "input_summary": truncate(task, 1000)}).Scan(&nodeID) != nil {
		return true, errs.ErrConflict
	}
	edgeRef, err := newRef("edg")
	if err != nil {
		return true, err
	}
	if _, err := tx.Exec(ctx, queryRuntimeCallbackInsertContinuesEdge, pgx.StrictNamedArgs{"edge_ref": edgeRef, "organization_id": current.organizationID, "root_run_id": root.ID, "source_node_id": lease["nodeID"], "target_node_id": nodeID}); err != nil {
		return true, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeIntegrationResumeChildRun, pgx.StrictNamedArgs{"run_id": lease["runID"], "organization_id": current.organizationID}); err != nil {
		return true, errs.ErrUnavailable
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, nodeRef, "TURN_QUEUED", nodeRef, edgeRef, "", "", "i18n:CALLBACK_CONTINUATION_QUEUED", root.State, "QUEUED")
	return true, err
}
