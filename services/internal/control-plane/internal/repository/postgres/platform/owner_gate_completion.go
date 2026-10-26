package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryOwnerGateCompletion reads the pinned plan and exact gate producer without locking or mutating execution.
// queryOwnerGateCompletion читает закреплённый план и точного производителя Gate без блокировки либо изменения исполнения.
//
//go:embed sql/owner_gate__completion.sql
var queryOwnerGateCompletion string

// openOwnerGateCompletesRun predicts APPROVE using the same accepted-attempt and pending-work predicates
// as SERVER completion, hypothetically approving only this exact producer's gate. Recovery and external
// effects remain nonterminal; legacy ordinary transitions retain their existing physical-node predicate.
// This read grants no execution authority and never changes receipts, attempts or immutable history.
// openOwnerGateCompletesRun прогнозирует APPROVE по тем же accepted-attempt и pending-work условиям,
// что SERVER completion, гипотетически одобряя только Gate точного производителя. Recovery и внешние
// действия остаются nonterminal; legacy ordinary сохраняет прежнее условие физических узлов.
// Чтение не выдаёт полномочий и не меняет receipts, attempts либо неизменную историю.
func openOwnerGateCompletesRun(ctx context.Context, runner queryRunner, current scope, gateRef string, integration, delivery, legacyCompletes bool) (bool, error) {
	if integration || delivery || !legacyCompletes {
		return false, nil
	}
	var rootID, state, producer string
	var spec, exception []byte
	var otherOpen, producerBlocked bool
	if err := runner.QueryRow(ctx, queryOwnerGateCompletion, pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_ref": gateRef}).Scan(
		&rootID, &state, &spec, &exception, &producer, &otherOpen, &producerBlocked,
	); err != nil {
		return false, errs.ErrUnavailable
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(spec, &version) != nil {
		return false, errs.ErrUnavailable
	}
	if version.ExecutionMode == "" || version.ExecutionMode == "MANAGER_DRIVEN" {
		return legacyCompletes, nil
	}
	if version.ExecutionMode != workflowservice.ServerDAGV1 || !validWorkflowVersion(version) {
		return false, errs.ErrUnavailable
	}
	if !contains([]string{"RUNNING", "WAITING_HUMAN"}, state) || string(exception) != "null" || otherOpen || producerBlocked || producer == "" {
		return false, nil
	}
	root := &serverWorkflowRoot{ID: rootID, Version: version}
	anchors, err := readServerWorkflowAnchorsTx(ctx, runner, current, root)
	if err != nil {
		return false, err
	}
	matched := 0
	for _, step := range version.Steps {
		anchor := anchors[step.Key]
		if len(anchor.Receipts) > 0 {
			receipt := anchor.Receipts[len(anchor.Receipts)-1]
			if receipt.Provenance != nil && receipt.Provenance.NodeID == producer {
				// Меняется только локальная копия точного Gate. Другой blocker уже исключён owner query.
				anchor.GateApproved, anchor.GateBlocked = true, false
				matched++
			}
		}
		if _, accepted := acceptedServerWorkflowAttempt(anchor, step); !accepted {
			return false, nil
		}
	}
	if matched != 1 {
		return false, nil
	}
	pending, err := pendingServerWorkflowTx(ctx, runner, current, rootID, "", 0)
	return !pending && err == nil, err
}
