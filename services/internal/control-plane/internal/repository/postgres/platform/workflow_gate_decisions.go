package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeCompleteexecutionSelectGatePolicy resolves immutable workflow policy through the
// already verified execution lease; caller-provided workflow/step locators never grant authority.
// queryRuntimeCompleteexecutionSelectGatePolicy разрешает immutable policy процесса по уже
// проверенному lease; переданные вызывающим workflow/step locators не предоставляют полномочия.
//
//go:embed sql/runtime_completeexecution_select_gate_policy.sql
var queryRuntimeCompleteexecutionSelectGatePolicy string

// workflowStepGateDecisions selects only the exact published gated step owned by the executing
// agent. Invalid structure, ambiguous step identity and unknown decisions fail closed.
// workflowStepGateDecisions выбирает только точный опубликованный gated step исполняющего агента.
// Неверная структура, неоднозначный step и неизвестные решения закрыто отклоняются.
func workflowStepGateDecisions(version entity.WorkflowVersion, stepKey, agentRef string) ([]string, error) {
	if !validWorkflowVersion(version) || stepKey == "" || agentRef == "" {
		return nil, errs.ErrConflict
	}
	for _, step := range version.Steps {
		if step.Key != stepKey {
			continue
		}
		if !step.HumanGateAfter || step.AgentRef != agentRef || len(step.GateDecisions) == 0 {
			return nil, errs.ErrConflict
		}
		return append([]string(nil), step.GateDecisions...), nil
	}
	return nil, errs.ErrConflict
}

// executionStepGateDecisions reads the exact owner-pinned published version and node agent through
// the leased RuntimeRevision in the current completion transaction, never the mutable draft.
// executionStepGateDecisions читает закреплённую владельцем published version и агента узла по
// RuntimeRevision lease в текущей completion-транзакции, не используя изменяемый draft.
func (repository *Repository) executionStepGateDecisions(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) ([]string, error) {
	step, err := repository.executionStepGatePolicy(ctx, tx, current, lease)
	return step.GateDecisions, err
}

// executionStepGatePolicy resolves decisions and optional wait time from one immutable published
// step reached through the verified lease; an absent policy never receives a default timeout.
// executionStepGatePolicy читает решения и необязательный срок из одного immutable published
// этапа по проверенному lease; отсутствующему сроку не назначается значение по умолчанию.
func (repository *Repository) executionStepGatePolicy(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) (entity.WorkflowStep, error) {
	var raw []byte
	var stepKey, agentRef string
	err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectGatePolicy, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "lease_id": lease["leaseID"],
		"runtime_revision_id": lease["runtimeRevisionID"], "run_id": lease["runID"],
		"root_run_id": lease["rootRunID"], "node_id": lease["nodeID"], "generation": lease["generation"],
	}).Scan(&raw, &stepKey, &agentRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkflowStep{}, errs.ErrConflict
	}
	if err != nil {
		return entity.WorkflowStep{}, errs.ErrUnavailable
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(raw, &version) != nil {
		return entity.WorkflowStep{}, errs.ErrConflict
	}
	decisions, err := workflowStepGateDecisions(version, stepKey, agentRef)
	if err != nil {
		return entity.WorkflowStep{}, err
	}
	for _, step := range version.Steps {
		if step.Key == stepKey {
			step.GateDecisions = decisions
			return step, nil
		}
	}
	return entity.WorkflowStep{}, errs.ErrConflict
}
