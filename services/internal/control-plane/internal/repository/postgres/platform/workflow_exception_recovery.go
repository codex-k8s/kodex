package platform

import (
	"context"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// openWorkflowRecoveryGateTx pins a recovery decision to this exact versioned episode, after the
// completed manager analysis or an explicit missing-manager outcome. It never accepts evidence.
// openWorkflowRecoveryGateTx закрепляет решение восстановления за точным versioned episode после
// completed manager analysis либо явного missing-manager outcome; доказательства не принимаются.
func (repository *Repository) openWorkflowRecoveryGateTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, history []workflowExceptionEpisode, analysis string) error {
	episode := &history[len(history)-1]
	parent := episode.ManagerNodeID
	if parent == "" {
		parent = episode.FinalNodeID
	}
	var runID string
	var ignored []workflowInputReceipt
	if tx.QueryRow(ctx, workflowExceptionSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "node_id": parent}).Scan(&runID, &ignored) != nil {
		return errs.ErrUnavailable
	}
	nodeRef, err := newRef("nod")
	if err != nil {
		return err
	}
	var nodeID string
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId, nodeRef, current.organizationID, root.ID, runID, parent).Scan(&nodeID) != nil {
		return errs.ErrUnavailable
	}
	edgeRef, err := newRef("edg")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, current.organizationID, root.ID, parent, nodeID); err != nil {
		return errs.ErrUnavailable
	}
	gateRef, err := newRef("gat")
	if err != nil {
		return err
	}
	decisions := []string{"REJECT", "CANCEL"}
	if episode.Attempt < 4 && episode.Episode < workflowExceptionEpisodeLimit {
		decisions = append([]string{"APPROVE"}, decisions...)
	}
	var timeout *int32
	for _, step := range root.Version.Steps {
		if step.Key == episode.StepKey {
			timeout = step.ApprovalTimeoutSeconds
		}
	}
	contextSummary := fmt.Sprintf("Recovery episode %d: stage=%s attempt=%d reason=%s. Approval creates a fresh attempt and does not waive result predicates. Manager analysis (untrusted data):\n%s", episode.Episode, episode.StepKey, episode.Attempt, episode.Reason, truncate(analysis, 1500))
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId, pgx.StrictNamedArgs{"gate_ref": gateRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "root_run_id": root.ID, "node_id": nodeID, "context_summary": contextSummary, "allowed_decisions": decisions, "approval_timeout_seconds": timeout}).Scan(&episode.GateID) != nil {
		return errs.ErrUnavailable
	}
	episode.State, episode.GateRef = "WAITING_OWNER", gateRef
	if err := saveWorkflowExceptionEpisodeTx(ctx, tx, current, root, history); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt, root.ID); err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.enqueueGateInteractionDeliveries(ctx, tx, current, root.ProjectID, root.ID, episode.GateID); err != nil {
		return err
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, gateRef, "OWNER_GATE_OPENED", nodeRef, edgeRef, gateRef, "", "i18n:OWNER_DECISION_REQUIRED", "", "")
	return err
}

// completeWorkflowExceptionManagerTx consumes only the active episode's exact completed manager
// lease. Model success and narrative are analysis data; the following gate owns recovery authority.
// completeWorkflowExceptionManagerTx принимает только exact completed manager lease активного episode.
// Успех и текст модели — данные разбора; полномочиями восстановления владеет следующий Gate.
func (repository *Repository) completeWorkflowExceptionManagerTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, lease map[string]any, payload command.CompleteExecutionInput) (bool, error) {
	if string(root.Exception) == "null" {
		return false, nil
	}
	episode, err := decodeWorkflowEpisode(root.Exception)
	if err != nil {
		return false, err
	}
	if episode.ManagerNodeID != stringMap(lease, "nodeID") {
		return false, nil
	}
	if episode.State != "MANAGER_QUEUED" {
		return true, errs.ErrConflict
	}
	var found bool
	if tx.QueryRow(ctx, workflowExceptionManagerOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "node_id": episode.ManagerNodeID, "episode": episode.Episode, "runtime_revision_id": lease["runtimeRevisionID"], "lease_id": lease["leaseID"]}).Scan(&found) != nil {
		return true, errs.ErrUnavailable
	}
	if !found {
		return true, errs.ErrConflict
	}
	history, err := readWorkflowExceptionHistoryTx(ctx, tx, current, root)
	if err != nil {
		return true, err
	}
	if len(history) == 0 || history[len(history)-1].Version != episode.Version {
		return true, errs.ErrConflict
	}
	history[len(history)-1].Version++
	return true, repository.openWorkflowRecoveryGateTx(ctx, tx, current, root, history, payload.ResultSummary)
}

// resolveWorkflowExceptionGateTx accepts no manager-supplied retry command. Only an already
// authorized and OCC-checked exact owner Gate can create a fresh physical business attempt.
// resolveWorkflowExceptionGateTx не принимает retry command менеджера. Только authorized/OCC-checked
// точный owner Gate создаёт новую physical business attempt.
func (repository *Repository) resolveWorkflowExceptionGateTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, payload command.GateResolutionInput) (bool, error) {
	if string(root.Exception) == "null" {
		return false, nil
	}
	episode, err := decodeWorkflowEpisode(root.Exception)
	if err != nil {
		return false, err
	}
	if episode.GateRef != payload.GateRef {
		return false, nil
	}
	if episode.State != "WAITING_OWNER" || !contains([]string{"RUNNING", "WAITING_HUMAN"}, root.State) {
		return true, errs.ErrConflict
	}
	if payload.Decision != "APPROVE" {
		return false, nil
	} // Existing native cancel closes all leases/effects and the episode guard.
	if episode.Attempt >= 4 || episode.Episode >= workflowExceptionEpisodeLimit {
		return true, errs.ErrForbidden
	}
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, root.ID); err != nil {
		return true, err
	} else if expired {
		return true, errs.ErrConflict
	}
	pending, err := pendingServerWorkflowTx(ctx, tx, current, root.ID, "", 0)
	if err != nil {
		return true, err
	}
	if pending {
		return true, errs.ErrConflict
	}
	history, err := readWorkflowExceptionHistoryTx(ctx, tx, current, root)
	if err != nil {
		return true, err
	}
	if len(history) == 0 || history[len(history)-1].Version != episode.Version {
		return true, errs.ErrConflict
	}
	var step entity.WorkflowStep
	for _, candidate := range root.Version.Steps {
		if candidate.Key == episode.StepKey {
			step = candidate
		}
	}
	if step.Key == "" {
		return true, errs.ErrConflict
	}
	feedback, err := readWorkflowRecoveryFeedbackTx(ctx, tx, current, root, episode)
	if err != nil {
		return true, err
	}
	task, err := workflowStageTask(root, step, episode.Inputs)
	if err != nil {
		return true, err
	}
	task, err = workflowRecoveryTask(task, feedback)
	if err != nil {
		return true, err
	}
	var newNodeID string
	blocked, err := workflowConfigurationAttemptTx(ctx, tx, func(child pgx.Tx) error {
		anchors, err := readServerWorkflowAnchorsTx(ctx, child, current, root)
		if err != nil {
			return err
		}
		anchor := anchors[step.Key]
		if anchor.ID != episode.OriginNodeID {
			return errs.ErrConflict
		}
		if episode.Attempt == 0 {
			if anchor.AssignedAttempt != 0 {
				return errs.ErrConflict
			}
			if err := repository.materializeServerWorkflowFirstAttemptTaskTx(ctx, child, current, root, step, anchor, episode.Inputs, task); err != nil {
				return err
			}
			newNodeID = anchor.ID
			return nil
		}
		if len(anchor.Receipts) != int(episode.Attempt) || anchor.LatestAttempt != episode.Attempt {
			return errs.ErrConflict
		}
		stage, err := repository.createWorkflowAgentRunTx(ctx, child, current, root, step.AgentRef, step.Name, task)
		if err != nil {
			return err
		}
		nodeRef, err := newRef("nod")
		if err != nil {
			return err
		}
		if child.QueryRow(ctx, workflowExceptionRetry, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "origin_node_id": anchor.ID, "previous_node_id": episode.FinalNodeID, "business_attempt": episode.Attempt + 1, "run_id": stage.RunID, "turn_id": stage.TurnID, "agent_id": stage.AgentID, "node_ref": nodeRef, "display_name": stage.Name, "role": stage.Role, "inputs": asJSON(episode.Inputs)}).Scan(&newNodeID) != nil {
			return errs.ErrUnavailable
		}
		edgeRef, err := newRef("edg")
		if err != nil {
			return err
		}
		if _, err := child.Exec(ctx, queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, current.organizationID, root.ID, episode.FinalNodeID, newNodeID); err != nil {
			return errs.ErrUnavailable
		}
		_, err = repository.emitRunEvent(ctx, child, current, root.ProjectID, root.ID, nodeRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:CALLBACK_CONTINUATION_QUEUED", "", "")
		return err
	})
	if err != nil {
		return true, err
	}
	if blocked {
		return true, errs.ErrConflict
	}
	history[len(history)-1].Version++
	history[len(history)-1].State, history[len(history)-1].RecoveryNodeID = "RECOVERED", newNodeID
	return true, saveWorkflowExceptionEpisodeTx(ctx, tx, current, root, history)
}
