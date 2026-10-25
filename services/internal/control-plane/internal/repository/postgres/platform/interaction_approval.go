package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// enqueueApprovedTerminalInteraction loads owner terminal-run delivery candidates and checks exact
// WRITE/HUMAN_EACH_EFFECT capability against the pinned active package. It creates approval
// gate/node/delivery intent, emits OWNER_GATE_OPENED and enqueues gate notices in the same
// transaction. External notification waits for human resolution and a later delivery claim; no
// core run is resumed here.
// enqueueApprovedTerminalInteraction читает owner candidates доставки конечного запуска и
// проверяет точную WRITE/HUMAN_EACH_EFFECT capability по pinned active package. Создаёт approval
// gate/node/delivery intent, пишет OWNER_GATE_OPENED и ставит gate notices в той же транзакции.
// Внешнее уведомление ждёт решения человека и последующего delivery claim; core run здесь не
// возобновляется.
func (repository *Repository) enqueueApprovedTerminalInteraction(ctx context.Context, tx pgx.Tx, current scope, projectID, rootRunID string) error {
	rows, err := tx.Query(ctx, queryInteractionTerminalCandidates, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID, "root_run_id": rootRunID,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	// candidate holds one server-selected connection/grant/capability tuple while SQL rows are closed
	// before per-candidate writes. It cannot supply caller-defined delivery authority.
	// candidate хранит одну серверную connection/grant/capability tuple до закрытия SQL rows и
	// последующих записей по candidates. Не задаёт права доставки от caller.
	type candidate struct{ connectionRef, grantID, capabilityKey string }
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.connectionRef, &item.grantID, &item.capabilityKey); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range candidates {
		_, definition, err := repository.interactionConnectionPackage(ctx, tx, current.organizationID, item.connectionRef)
		if err != nil {
			return err
		}
		capability, ok := definition.Capability(item.capabilityKey)
		if !ok || capability.Operation != item.capabilityKey || capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
			return errs.ErrForbidden
		}
		gateRef, err := newRef("gat")
		if err != nil {
			return err
		}
		nodeRef, err := newRef("nod")
		if err != nil {
			return err
		}
		deliveryRef, err := newRef("idl")
		if err != nil {
			return err
		}
		var gateID, gateNodeRef, createdDeliveryRef string
		err = tx.QueryRow(ctx, queryInteractionTerminalCreateApproval, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": projectID, "root_run_id": rootRunID,
			"connection_ref": item.connectionRef, "grant_id": item.grantID, "capability_key": item.capabilityKey,
			"capability_name": capability.Name, "max_attempts": capability.Execution.MaxAttempts,
			"gate_ref": gateRef, "node_ref": nodeRef, "delivery_ref": deliveryRef,
		}).Scan(&gateID, &gateNodeRef, &createdDeliveryRef)
		if err != nil {
			return errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, projectID, rootRunID, gateRef, "OWNER_GATE_OPENED", gateNodeRef, "", gateRef, "", "i18n:INTERACTION_DELIVERY_APPROVAL_REQUIRED", "", "WAITING"); err != nil {
			return err
		}
		if err := repository.enqueueGateInteractionDeliveries(ctx, tx, current, projectID, rootRunID, gateID); err != nil {
			return err
		}
	}
	return nil
}

// resolveInteractionDeliveryGate handles only an optional delivery gate; missing rows return
// handled=false for the normal gate path. It requires typed input and expected version established
// by caller, current gate.resolve, OPEN/WAITING_APPROVAL and allowed decision without attachments.
// Approve rechecks package/grant pins; reject/cancel close delivery.
// Gate/node/notices/event/readback change in the caller transaction while terminal core run state
// stays unchanged.
// resolveInteractionDeliveryGate обрабатывает только optional delivery gate; отсутствие строки
// даёт handled=false для обычного gate path. Требует установленные caller typed input и expected
// version, текущее gate.resolve, OPEN/WAITING_APPROVAL и допустимое решение без attachments.
// Approve заново проверяет package/grant pins; reject/cancel закрывают delivery.
// Gate/node/notices/event/readback меняются в переданной транзакции, состояние конечного core run
// сохраняется.
func (repository *Repository) resolveInteractionDeliveryGate(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, bool, error) {
	payload := input.Payload.(command.GateResolutionInput)
	var gateID, nodeID, rootRunID, projectID, projectRef, gateState, nodeRef, runState string
	var deliveryID, deliveryState, connectionRef, definitionVersion, definitionDigest string
	var gateVersion, connectionVersion int64
	var allowed []string
	var grantEligible bool
	err := tx.QueryRow(ctx, queryInteractionApprovalResolve, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "gate_ref": payload.GateRef,
	}).Scan(&gateID, &nodeID, &rootRunID, &projectID, &projectRef, &gateVersion, &gateState, &allowed,
		&nodeRef, &runState, &deliveryID, &deliveryState, &connectionRef, &connectionVersion,
		&definitionVersion, &definitionDigest, &grantEligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, false, nil
	}
	if err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: payload.GateRef}); err != nil {
		return commandOutcome{}, true, err
	}
	if gateState != "OPEN" {
		return commandOutcome{}, true, errs.ErrAlreadyResolved
	}
	if gateVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, true, errs.ErrVersionMismatch
	}
	if !contains(allowed, payload.Decision) || payload.AttachmentSetRef != "" {
		return commandOutcome{}, true, errs.ErrInvalid
	}
	if deliveryState != "WAITING_APPROVAL" {
		return commandOutcome{}, true, errs.ErrConflict
	}
	gateNext, deliveryNext, nodeNext, safeError := "APPROVED", "DUE", "SUCCEEDED", ""
	switch payload.Decision {
	case "APPROVE":
		connection, _, err := repository.interactionConnectionPackage(ctx, tx, current.organizationID, connectionRef)
		if err != nil || !grantEligible || connection.version != connectionVersion || connection.definitionVersion != definitionVersion || connection.definitionDigest != definitionDigest {
			return commandOutcome{}, true, errs.ErrConflict
		}
	case "REJECT":
		gateNext, deliveryNext, nodeNext, safeError = "REJECTED", "CANCELLED", "FAILED", "INTERACTION_REJECTED_BY_OWNER"
	case "CANCEL":
		gateNext, deliveryNext, nodeNext, safeError = "CANCELLED", "CANCELLED", "CANCELLED", "INTERACTION_CANCELLED_BY_OWNER"
	default:
		return commandOutcome{}, true, errs.ErrInvalid
	}
	tag, err := tx.Exec(ctx, queryInteractionApprovalUpdate, pgx.StrictNamedArgs{
		"delivery_id": deliveryID, "next_state": deliveryNext, "safe_error_code": safeError,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, true, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment,
		gateID, gateNext, payload.Decision, truncate(payload.Comment, 2000), current.actorID); err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion, nodeID, nodeNext); err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryInteractionCancelPendingGateDeliveries, pgx.StrictNamedArgs{"gate_id": gateID}); err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, current, projectID, rootRunID, payload.GateRef, "OWNER_GATE_RESOLVED", nodeRef, "", payload.GateRef, "", "i18n:OWNER_GATE_RESOLVED", runState, nodeNext)
	if err != nil {
		return commandOutcome{}, true, err
	}
	runRef, err := mustRunRef(ctx, tx, rootRunID)
	if err != nil {
		return commandOutcome{}, true, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, runRef)
	if err != nil {
		return commandOutcome{}, true, err
	}
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId,
		current.organizationID, payload.GateRef, current.role, current.actorID), true)
	if err != nil {
		return commandOutcome{}, true, err
	}
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event},
		projectID: projectID, projectRef: projectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "i18n:OWNER_GATE_RESOLVED"}, true, nil
}

// cancelInteractionIntents invalidates connection delivery intents in SQL, turning claimed effects
// UNKNOWN_OUTCOME and other eligible intents CANCELLED with leases cleared. It then cancels each
// still-open approval gate/node and pending notices and emits resolution events without changing
// core run state. Partial work requires outer rollback on error; it does not assert that an
// external effect was undone.
// cancelInteractionIntents инвалидирует connection delivery intents в SQL, переводя claimed
// effects в UNKNOWN_OUTCOME, прочие допустимые intents — в CANCELLED с очисткой leases. Затем
// отменяет каждый ещё открытый approval gate/node и pending notices и пишет resolution events без
// изменения core run state. При ошибке частичная работа требует outer rollback; отмена не
// доказывает откат внешнего эффекта.
func (repository *Repository) cancelInteractionIntents(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) error {
	rows, err := tx.Query(ctx, queryInteractionApprovalInvalidate, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_ref": connectionRef,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	// pendingGate carries owner-resolved gate/node/root/project references and the unchanged core run
	// state for invalidation events after closing rows. It is not a new grant to cancel arbitrary
	// graphs.
	// pendingGate хранит owner-resolved gate/node/root/project refs и неизменённое core run state для
	// invalidation events после закрытия rows. Не выдаёт новый grant отмены произвольных графов.
	type pendingGate struct{ id, ref, nodeID, nodeRef, rootRunID, projectID, runState string }
	gates := []pendingGate{}
	for rows.Next() {
		var gate pendingGate
		if err := rows.Scan(&gate.id, &gate.ref, &gate.nodeID, &gate.nodeRef, &gate.rootRunID, &gate.projectID, &gate.runState); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		gates = append(gates, gate)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, gate := range gates {
		if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment,
			gate.id, "CANCELLED", "CANCEL", "i18n:INTERACTION_AUTHORITY_CHANGED", current.actorID); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion, gate.nodeID, "CANCELLED"); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryInteractionCancelPendingGateDeliveries, pgx.StrictNamedArgs{"gate_id": gate.id}); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, gate.projectID, gate.rootRunID, gate.ref,
			"OWNER_GATE_RESOLVED", gate.nodeRef, "", gate.ref, "", "i18n:INTERACTION_AUTHORITY_CHANGED", gate.runState, "CANCELLED"); err != nil {
			return err
		}
	}
	return nil
}
