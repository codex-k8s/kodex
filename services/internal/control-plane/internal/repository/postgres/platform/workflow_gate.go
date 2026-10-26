package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// resolveServerWorkflowResultGateTx runs after the owner gate decision is persisted in this same
// transaction. It bypasses the legacy no-active-nodes shortcut and never rewrites a physical attempt.
// resolveServerWorkflowResultGateTx вызывается после записи решения в той же owner-транзакции.
// Функция исключает legacy завершение по отсутствию active nodes и не переписывает физическую попытку.
func (repository *Repository) resolveServerWorkflowResultGateTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command, rootID, gateNodeRef, predecessorID, integrationID string) (commandOutcome, bool, error) {
	root, err := readServerWorkflowRootTx(ctx, tx, current, rootID)
	if err != nil || root == nil {
		return commandOutcome{}, false, err
	}
	payload, ok := input.Payload.(command.GateResolutionInput)
	if !ok {
		return commandOutcome{}, true, errs.ErrInvalid
	}
	if integrationID == "" {
		switch payload.Decision {
		case "REJECT", "CANCEL":
			nested := input
			nested.Kind = command.CancelRun
			nested.Payload = command.RunCommandInput{RunRef: root.Ref}
			nested.Mutation.ExpectedVersion = &root.VersionNumber
			if _, err := repository.changeRun(ctx, tx, current, nested); err != nil {
				return commandOutcome{}, true, err
			}
			if payload.Decision == "REJECT" {
				if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionFailRootRun, root.ID, "i18n:OWNER_GATE_RESOLVED", "WORKFLOW_REJECTED", ""); err != nil {
					return commandOutcome{}, true, errs.ErrUnavailable
				}
				var ignored string
				if tx.QueryRow(ctx, queryCommandsResolvegateUpdateRootNodeState, root.ID, "FAILED").Scan(&ignored) != nil {
					return commandOutcome{}, true, errs.ErrUnavailable
				}
				if _, err := tx.Exec(ctx, queryWorkflowDispatchFinishTurn, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "state": "FAILED"}); err != nil {
					return commandOutcome{}, true, errs.ErrUnavailable
				}
			}
			if err := repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, root.ProjectID, root.ID); err != nil {
				return commandOutcome{}, true, err
			}
		case "REQUEST_CHANGES":
			if strings.TrimSpace(payload.Comment) == "" {
				return commandOutcome{}, true, errs.ErrInvalid
			}
			anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
			if err != nil {
				return commandOutcome{}, true, err
			}
			found := false
			for _, anchor := range anchors {
				if len(anchor.Receipts) == 0 {
					continue
				}
				receipt := anchor.Receipts[len(anchor.Receipts)-1]
				if receipt.Provenance == nil || receipt.Provenance.NodeID != predecessorID || receipt.Attempt != anchor.LatestAttempt || !receipt.Accepted {
					continue
				}
				found = true
				exception := serverWorkflowException{StepKey: anchor.Key, Attempt: anchor.LatestAttempt, Reason: "OWNER_CHANGES_REQUESTED", OriginNodeID: anchor.ID, FinalNodeID: predecessorID}
				if _, err := tx.Exec(ctx, queryWorkflowDispatchException, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "exception": asJSON(exception)}); err != nil {
					return commandOutcome{}, true, errs.ErrUnavailable
				}
			}
			if !found {
				return commandOutcome{}, true, errs.ErrConflict
			}
		case "APPROVE":
		default:
			return commandOutcome{}, true, errs.ErrInvalid
		}
	}
	if integrationID != "" || payload.Decision == "APPROVE" || payload.Decision == "REQUEST_CHANGES" {
		var open int64
		if tx.QueryRow(ctx, queryCommandsResolvegateCountOpenGates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&open) != nil {
			return commandOutcome{}, true, errs.ErrUnavailable
		}
		state := "RUNNING"
		if open > 0 {
			state = "WAITING_HUMAN"
		}
		if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt, root.ID, state); err != nil {
			return commandOutcome{}, true, errs.ErrUnavailable
		}
		if err := repository.dispatchServerWorkflowTx(ctx, tx, current, root.ID); err != nil {
			return commandOutcome{}, true, err
		}
		if err := repository.finishAcceptedServerWorkflowTx(ctx, tx, current, root.ID); err != nil {
			return commandOutcome{}, true, err
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, payload.GateRef, "OWNER_GATE_RESOLVED", gateNodeRef, "", payload.GateRef, "", "i18n:OWNER_GATE_RESOLVED", "", "")
	if err != nil {
		return commandOutcome{}, true, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, root.Ref)
	if err != nil {
		return commandOutcome{}, true, err
	}
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, current.organizationID, payload.GateRef, current.role, current.actorID), true)
	if err != nil {
		return commandOutcome{}, true, err
	}
	if err := repository.projectGateIntent(ctx, tx, current, &gate, true); err != nil {
		return commandOutcome{}, true, err
	}
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event}, projectID: root.ProjectID, projectRef: root.ProjectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "i18n:OWNER_GATE_RESOLVED", runtimeGraphChanged: true}, true, nil
}
