package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowDispatchCompletion resolves the final physical producer from completed lease pins.
// queryWorkflowDispatchCompletion разрешает завершающего исполнителя по pins завершённого lease.
//
//go:embed sql/workflow_dispatch__completion.sql
var queryWorkflowDispatchCompletion string

// queryWorkflowDispatchPending fences acceptance while physical or external work is unresolved.
// queryWorkflowDispatchPending запрещает приёмку при незавершённой физической или внешней работе.
//
//go:embed sql/workflow_dispatch__pending.sql
var queryWorkflowDispatchPending string

// queryWorkflowDispatchException persists a business rejection before any recovery is scheduled.
// queryWorkflowDispatchException сохраняет бизнес-отказ до назначения восстановления.
//
//go:embed sql/workflow_dispatch__exception.sql
var queryWorkflowDispatchException string

// queryWorkflowDispatchFinishTurn closes the root intent without inventing a coordinator execution.
// queryWorkflowDispatchFinishTurn закрывает root intent без выдуманного исполнения координатора.
//
//go:embed sql/workflow_dispatch__finish_turn.sql
var queryWorkflowDispatchFinishTurn string

// pendingServerWorkflowTx reads a same-attempt barrier or the whole root when origin is empty.
// It never cancels an effect to make a successful completion appear ready for acceptance.
// pendingServerWorkflowTx читает барьер одной попытки либо всего root при пустом origin.
// Проверка не отменяет внешнее действие ради видимости готовности успешного completion к приёмке.
func pendingServerWorkflowTx(ctx context.Context, tx pgx.Tx, current scope, rootID, originID string, attempt int32) (bool, error) {
	var pending bool
	if tx.QueryRow(ctx, queryWorkflowDispatchPending, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "origin_node_id": originID, "business_attempt": attempt}).Scan(&pending) != nil {
		return false, errs.ErrUnavailable
	}
	return pending, nil
}

// completeServerWorkflowTx replaces only the server profile's legacy callback/terminal path after
// capture and usage persistence. An accepted journal, never model success, enables downstream dispatch.
// completeServerWorkflowTx заменяет legacy callback/terminal только серверного профиля после capture
// и usage. Зависимости запускает принятый журнал, а не успешное завершение модели.
func (repository *Repository) completeServerWorkflowTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, payload command.CompleteExecutionInput, artifactRefs []string) (commandOutcome, bool, error) {
	root, err := readServerWorkflowRootTx(ctx, tx, current, stringMap(lease, "rootRunID"))
	if err != nil || root == nil {
		return commandOutcome{}, false, err
	}
	if contains([]string{"RUNNING", "WAITING_HUMAN"}, root.State) {
		var key, originID string
		var attempt int32
		if tx.QueryRow(ctx, queryWorkflowDispatchCompletion, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "node_id": stringMap(lease, "nodeID"), "runtime_revision_id": stringMap(lease, "runtimeRevisionID"), "lease_id": stringMap(lease, "leaseID")}).Scan(&key, &attempt, &originID) != nil {
			return commandOutcome{}, true, errs.ErrConflict
		}
		pending, err := pendingServerWorkflowTx(ctx, tx, current, root.ID, originID, attempt)
		if err != nil {
			return commandOutcome{}, true, err
		}
		if !pending {
			if payload.Success {
				for _, step := range root.Version.Steps {
					if step.Key != key {
						continue
					}
					continued, err := repository.continueWorkflowCaptureTx(ctx, tx, current, root, lease, step, payload.ResultSummary)
					if err != nil {
						return commandOutcome{}, true, err
					}
					pending = continued
				}
			}
		}
		if !pending {
			results, reason, err := repository.evaluateWorkflowStepResultTx(ctx, tx, current, lease, payload)
			if err != nil {
				return commandOutcome{}, true, err
			}
			if _, err := appendWorkflowAttemptTx(ctx, tx, current, lease, reason == "", reason, results); err != nil {
				return commandOutcome{}, true, err
			}
			if reason != "" {
				exception := serverWorkflowException{StepKey: key, Attempt: attempt, Reason: reason, OriginNodeID: originID, FinalNodeID: stringMap(lease, "nodeID")}
				if _, err := tx.Exec(ctx, queryWorkflowDispatchException, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "exception": asJSON(exception)}); err != nil {
					return commandOutcome{}, true, errs.ErrUnavailable
				}
			} else {
				for _, step := range root.Version.Steps {
					if step.Key == key && step.HumanGateAfter {
						if err := repository.openServerWorkflowResultGateTx(ctx, tx, current, root, lease, step); err != nil {
							return commandOutcome{}, true, err
						}
					}
				}
				if err := repository.dispatchServerWorkflowTx(ctx, tx, current, root.ID); err != nil {
					return commandOutcome{}, true, err
				}
				if err := repository.finishAcceptedServerWorkflowTx(ctx, tx, current, root.ID); err != nil {
					return commandOutcome{}, true, err
				}
			}
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, stringMap(lease, "nodeRef"), "TURN_COMPLETED", stringMap(lease, "nodeRef"), "", "", "", nonEmptyResult(payload), "", "")
	if err != nil {
		return commandOutcome{}, true, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, true, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event, CreatedRefs: artifactRefs}, projectID: root.ProjectID, projectRef: root.ProjectRef, resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "i18n:RUNTIME_EXECUTION_COMPLETED", runtimeGraphChanged: true}, true, nil
}

// openServerWorkflowResultGateTx opens the published gate only after its accepted receipt exists.
// openServerWorkflowResultGateTx открывает опубликованный gate только после принятой квитанции.
func (repository *Repository) openServerWorkflowResultGateTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, lease map[string]any, step entity.WorkflowStep) error {
	gateNodeRef, err := newRef("nod")
	if err != nil {
		return err
	}
	var gateNodeID string
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId, gateNodeRef, current.organizationID, root.ID, lease["runID"], lease["nodeID"]).Scan(&gateNodeID) != nil {
		return errs.ErrUnavailable
	}
	edgeRef, err := newRef("edg")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, current.organizationID, root.ID, lease["nodeID"], gateNodeID); err != nil {
		return errs.ErrUnavailable
	}
	gateRef, err := newRef("gat")
	if err != nil {
		return err
	}
	var gateID string
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId, pgx.StrictNamedArgs{"gate_ref": gateRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "root_run_id": root.ID, "node_id": gateNodeID, "context_summary": "i18n:WORKFLOW_RESULT_ACCEPTED", "allowed_decisions": step.GateDecisions, "approval_timeout_seconds": step.ApprovalTimeoutSeconds}).Scan(&gateID) != nil {
		return errs.ErrUnavailable
	}
	if err := repository.enqueueGateInteractionDeliveries(ctx, tx, current, root.ProjectID, root.ID, gateID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt, root.ID); err != nil {
		return errs.ErrUnavailable
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, gateRef, "OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:OWNER_DECISION_REQUIRED", "WAITING_HUMAN", "WAITING")
	return err
}

// finishAcceptedServerWorkflowTx requires every latest accepted journal, all published gates and
// no remaining physical/external work before atomically completing the root, schedule and intent.
// finishAcceptedServerWorkflowTx требует последние принятые записи, все опубликованные gates и
// отсутствие незавершённой работы до атомарного завершения root, расписания и intent.
func (repository *Repository) finishAcceptedServerWorkflowTx(ctx context.Context, tx pgx.Tx, current scope, rootID string) error {
	root, err := readServerWorkflowRootTx(ctx, tx, current, rootID)
	if err != nil || root == nil {
		return err
	}
	if !contains([]string{"RUNNING", "WAITING_HUMAN"}, root.State) || string(root.Exception) != "null" {
		return nil
	}
	anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
	if err != nil {
		return err
	}
	for _, step := range root.Version.Steps {
		if _, accepted := acceptedServerWorkflowAttempt(anchors[step.Key], step); !accepted {
			return nil
		}
	}
	pending, err := pendingServerWorkflowTx(ctx, tx, current, root.ID, "", 0)
	if err != nil {
		return err
	}
	if pending {
		return nil
	}
	var open int64
	if tx.QueryRow(ctx, queryCommandsResolvegateCountOpenGates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&open) != nil {
		return errs.ErrUnavailable
	}
	if open > 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, queryWorkflowDispatchFinishTurn, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "state": "COMPLETED"}); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateCompleteRootRun, root.ID); err != nil {
		return errs.ErrUnavailable
	}
	var nodeRef string
	if tx.QueryRow(ctx, queryCommandsResolvegateUpdateRootNodeState, root.ID, "SUCCEEDED").Scan(&nodeRef) != nil {
		return errs.ErrUnavailable
	}
	var scheduleID string
	err = tx.QueryRow(ctx, queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest, root.ID, "COMPLETED").Scan(&scheduleID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if err == nil {
		if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtUpdatedAt, scheduleID); err != nil {
			return errs.ErrUnavailable
		}
	}
	if _, err := repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, root.Ref, "RUN_STATE_CHANGED", nodeRef, "", "", "", "i18n:ROOT_PROCESS_COMPLETED", "SUCCEEDED", "SUCCEEDED"); err != nil {
		return err
	}
	return repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, root.ProjectID, root.ID)
}
