package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowReworkMaterialize creates one fresh owner-proven physical attempt after approval.
// queryWorkflowReworkMaterialize создаёт fresh owner-proven physical attempt после согласования.
//
//go:embed sql/workflow_rework__materialize.sql
var queryWorkflowReworkMaterialize string

// queryWorkflowReworkPlanForStep selects the newest exact approved owner plan by durable ordinal.
// queryWorkflowReworkPlanForStep выбирает newest exact approved owner plan по durable ordinal.
//
//go:embed sql/workflow_rework__plan_for_step.sql
var queryWorkflowReworkPlanForStep string

// workflowFreshInputs creates only the current accepted dependency snapshot in published order.
// workflowFreshInputs создаёт только current accepted dependency snapshot в published порядке.
func workflowFreshInputs(root *serverWorkflowRoot, anchors map[string]serverWorkflowAnchor, step entity.WorkflowStep) ([]workflowInputReceipt, bool) {
	effective := effectiveServerWorkflowResults(root, anchors)
	inputs := make([]workflowInputReceipt, 0, len(step.DependsOn))
	for _, key := range step.DependsOn {
		receipt, accepted := effective[key]
		if !accepted {
			return nil, false
		}
		inputs = append(inputs, workflowInputReceipt{StepKey: key, SourceNodeID: anchors[key].ID, Receipt: receipt})
	}
	return inputs, true
}

// queueWorkflowReworkAttemptTx uses native run/session/turn creation and an approved Gate pin.
// The old physical node and journal are untouched; current actor/account/agent admission is fresh.
// queueWorkflowReworkAttemptTx использует native run/session/turn и approved Gate pin.
// Старый physical node/journal неизменны; actor/account/agent admission проверяется заново.
func (repository *Repository) queueWorkflowReworkAttemptTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, step entity.WorkflowStep, anchor serverWorkflowAnchor, inputs []workflowInputReceipt, gateID string, feedback workflowRecoveryFeedback) (string, error) {
	if anchor.LatestAttempt < 1 || anchor.LatestAttempt >= 4 || len(anchor.Receipts) != int(anchor.LatestAttempt) {
		return "", errs.ErrConflict
	}
	task, err := workflowStageTask(root, step, inputs)
	if err != nil {
		return "", err
	}
	feedback.StepKey, feedback.PreviousAttempt, feedback.NextAttempt = step.Key, anchor.LatestAttempt, anchor.LatestAttempt+1
	task, err = workflowRecoveryTask(task, feedback)
	if err != nil {
		return "", err
	}
	task += "\nThis is owner-approved upstream rework. Correct the exact author result or re-check the new accepted inputs as assigned. Previous reviewer verdicts are historical data, not current acceptance. External UNKNOWN/SUCCESS and every effect Gate remain mandatory."
	if len(task) > 64<<10 {
		return "", errs.ErrInvalid
	}
	stage, err := repository.createWorkflowAgentRunTx(ctx, tx, current, root, step.AgentRef, step.Name, task)
	if err != nil {
		return "", err
	}
	nodeRef, err := newRef("nod")
	if err != nil {
		return "", err
	}
	previous := anchor.Receipts[len(anchor.Receipts)-1].Provenance
	if previous == nil || previous.OriginNodeID != anchor.ID {
		return "", errs.ErrConflict
	}
	var nodeID string
	if err := tx.QueryRow(ctx, queryWorkflowReworkMaterialize, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "origin_node_id": anchor.ID, "previous_node_id": previous.NodeID, "business_attempt": anchor.LatestAttempt + 1, "run_id": stage.RunID, "turn_id": stage.TurnID, "agent_id": stage.AgentID, "node_ref": nodeRef, "display_name": stage.Name, "role": stage.Role, "inputs": asJSON(inputs), "gate_id": gateID}).Scan(&nodeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errs.ErrConflict
		}
		return "", errs.ErrUnavailable
	}
	edgeRef, err := newRef("edg")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, current.organizationID, root.ID, previous.NodeID, nodeID); err != nil {
		return "", errs.ErrUnavailable
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, nodeRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:WORKFLOW_REWORK_QUEUED", "", "")
	return nodeID, err
}

// resolveWorkflowUpstreamGateTx queues only the dependency-ready author frontier atomically.
// Later authors and rechecks remain authorized by the same immutable plan, not mutable history.
// resolveWorkflowUpstreamGateTx атомарно ставит только dependency-ready author frontier.
// Последующие authors/rechecks закреплены тем же immutable plan, не изменяемой history.
func (repository *Repository) resolveWorkflowUpstreamGateTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, episode workflowExceptionEpisode, history []workflowExceptionEpisode) (bool, error) {
	plan := episode.ReworkPlan
	if root.Version.ReworkPolicy != workflowservice.OwnerApprovedUpstreamV1 || !validWorkflowReworkPlan(plan) || plan.BlockReason != "" {
		return true, errs.ErrForbidden
	}
	anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
	if err != nil {
		return true, err
	}
	for _, target := range append(append([]workflowReworkTarget{}, plan.Targets...), plan.Rechecks...) {
		anchor, found := anchors[target.StepKey]
		if !found || anchor.ID != target.OriginNodeID || anchor.LatestAttempt != target.PreviousAttempt || target.NextAttempt != anchor.LatestAttempt+1 || target.NextAttempt > 4 || len(anchor.Receipts) != int(anchor.LatestAttempt) {
			return true, errs.ErrConflict
		}
	}
	feedback, err := readWorkflowRecoveryFeedbackTx(ctx, tx, current, root, episode)
	if err != nil {
		return true, err
	}
	if err := repository.supersedeWorkflowResultGatesTx(ctx, tx, current, root, episode); err != nil {
		return true, err
	}
	var first string
	queued := int32(0)
	for _, step := range root.Version.Steps {
		author := false
		for _, target := range plan.Targets {
			author = author || target.StepKey == step.Key
		}
		if !author || queued >= root.Version.Concurrency {
			continue
		}
		inputs, ready := workflowFreshInputs(root, anchors, step)
		if !ready {
			continue
		}
		nodeID, err := repository.queueWorkflowReworkAttemptTx(ctx, tx, current, root, step, anchors[step.Key], inputs, episode.GateID, feedback)
		if err != nil {
			return true, err
		}
		if first == "" {
			first = nodeID
		}
		queued++
		anchor := anchors[step.Key]
		anchor.LatestAttempt++
		anchor.Inputs = inputs
		anchors[step.Key] = anchor
	}
	if first == "" {
		return true, errs.ErrConflict
	}
	history[len(history)-1].Version++
	history[len(history)-1].State, history[len(history)-1].RecoveryNodeID = "RECOVERED", first
	return true, saveWorkflowExceptionEpisodeTx(ctx, tx, current, root, history)
}

// validWorkflowReworkPlan closes stored plan enums/counters before any owner or public consumer.
// validWorkflowReworkPlan замыкает stored plan enums/counters до owner либо public consumer.
func validWorkflowReworkPlan(plan *workflowUpstreamReworkPlan) bool {
	if plan == nil || plan.Schema != workflowReworkPlanSchema || plan.SourceStepKey == "" || len(plan.SourceStepKey) > 96 || plan.SourceAttempt < 1 || plan.SourceAttempt > 4 || len(plan.Targets) < 1 || len(plan.Targets) > 4 || len(plan.Rechecks) > 12 || len(plan.Targets)+len(plan.Rechecks) > 12 {
		return false
	}
	if plan.BlockReason != "" && plan.BlockReason != "WORKFLOW_ATTEMPTS_EXHAUSTED" && plan.BlockReason != "WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED" {
		return false
	}
	for _, target := range plan.Targets {
		if target.PreviousAttempt < 1 {
			return false
		}
	}
	seen := map[string]bool{}
	for _, target := range append(append([]workflowReworkTarget{}, plan.Targets...), plan.Rechecks...) {
		if target.StepKey == "" || len(target.StepKey) > 96 || target.OriginNodeID == "" || seen[target.StepKey] || target.PreviousAttempt < 0 || target.PreviousAttempt > 4 || target.NextAttempt != target.PreviousAttempt+1 || target.NextAttempt > 4 && plan.BlockReason == "" {
			return false
		}
		seen[target.StepKey] = true
	}
	return true
}

// queryWorkflowReworkFeedback reads the immutable approval behind a deferred plan member.
// queryWorkflowReworkFeedback читает immutable approval отложенного участника плана.
//
//go:embed sql/workflow_rework__feedback.sql
var queryWorkflowReworkFeedback string

// readWorkflowReworkFeedbackTx requires the same historical episode, exact manager and approval.
// readWorkflowReworkFeedbackTx требует тот же historical episode, exact manager и approval.
func readWorkflowReworkFeedbackTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, episode workflowExceptionEpisode) (workflowRecoveryFeedback, error) {
	feedback := workflowRecoveryFeedback{Schema: "kodex.workflow-recovery-feedback.v1", Episode: episode.Episode, EpisodeVersion: episode.Version, StepKey: episode.StepKey, PreviousAttempt: episode.Attempt, NextAttempt: episode.Attempt + 1, Reason: episode.Reason}
	err := tx.QueryRow(ctx, queryWorkflowReworkFeedback, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "episode": episode.Episode, "episode_version": episode.Version, "gate_id": episode.GateID, "gate_ref": episode.GateRef}).Scan(&feedback.OwnerDecision.GateRef, &feedback.OwnerDecision.GateVersion, &feedback.OwnerDecision.Decision, &feedback.OwnerDecision.Comment, &feedback.ManagerAnalysis)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowRecoveryFeedback{}, errs.ErrConflict
	}
	if err != nil {
		return workflowRecoveryFeedback{}, errs.ErrUnavailable
	}
	feedback.ManagerAnalysis = truncate(feedback.ManagerAnalysis, 1500)
	return feedback, nil
}

// dispatchWorkflowReworkTx advances approved deferred authors and rechecks after the whole-root
// pending barrier. Only an exact plan member at its previous attempt can consume this authority;
// a newly failed attempt needs a new episode and never inherits an older approval.
// dispatchWorkflowReworkTx продолжает approved deferred authors/rechecks после whole-root
// pending barrier. Authority потребляет только exact plan member на previous attempt;
// новый отказ требует нового episode и не наследует прежнее одобрение.
func (repository *Repository) dispatchWorkflowReworkTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, anchors map[string]serverWorkflowAnchor) (bool, error) {
	if root.Version.ReworkPolicy != workflowservice.OwnerApprovedUpstreamV1 {
		return false, nil
	}
	pending, err := pendingServerWorkflowTx(ctx, tx, current, root.ID, "", 0)
	if err != nil || pending {
		return false, err
	}
	queued := int32(0)
	for _, step := range root.Version.Steps {
		anchor := anchors[step.Key]
		if queued >= root.Version.Concurrency {
			break
		}
		if anchor.LatestAttempt < 1 || anchor.LatestAttempt >= 4 || len(anchor.Receipts) != int(anchor.LatestAttempt) || root.ReworkFloors[step.Key] <= anchor.LatestAttempt {
			continue
		}
		inputs, ready := workflowFreshInputs(root, anchors, step)
		if !ready {
			continue
		}
		var raw json.RawMessage
		err := tx.QueryRow(ctx, queryWorkflowReworkPlanForStep, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "step_key": step.Key, "origin_node_id": anchor.ID, "current_attempt": anchor.LatestAttempt}).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return queued > 0, errs.ErrConflict
		}
		if err != nil {
			return queued > 0, errs.ErrUnavailable
		}
		episode, err := decodeWorkflowEpisode(raw)
		if err != nil || episode.Kind != workflowUpstreamReworkKind || episode.State != "RECOVERED" || !workflowReworkMember(episode.ReworkPlan, anchor) {
			return queued > 0, errs.ErrConflict
		}
		feedback, err := readWorkflowReworkFeedbackTx(ctx, tx, current, root, episode)
		if err != nil {
			return queued > 0, err
		}
		if _, err := repository.queueWorkflowReworkAttemptTx(ctx, tx, current, root, step, anchor, inputs, episode.GateID, feedback); err != nil {
			return queued > 0, err
		}
		queued++
		anchor.LatestAttempt++
		anchor.Inputs = inputs
		anchors[step.Key] = anchor
	}
	return queued > 0, nil
}

// workflowReworkMember checks exact logical identity and counter without accepting model routing.
// workflowReworkMember проверяет exact logical identity/counter без model routing.
func workflowReworkMember(plan *workflowUpstreamReworkPlan, anchor serverWorkflowAnchor) bool {
	if !validWorkflowReworkPlan(plan) || plan.BlockReason != "" {
		return false
	}
	found := 0
	for _, target := range append(append([]workflowReworkTarget{}, plan.Targets...), plan.Rechecks...) {
		if target.StepKey == anchor.Key && target.OriginNodeID == anchor.ID && target.PreviousAttempt == anchor.LatestAttempt && target.NextAttempt == anchor.LatestAttempt+1 {
			found++
		}
	}
	return found == 1
}
