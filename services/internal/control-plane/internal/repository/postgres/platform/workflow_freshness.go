package platform

import (
	"bytes"
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// effectiveServerWorkflowResults evaluates the bounded published DAG in its validated order.
// Historical accepted receipts remain unchanged. Only the explicit rework profile requires exact
// current dependency receipts; legacy keeps the old local-step acceptance semantics.
// effectiveServerWorkflowResults проверяет bounded published DAG в валидированном порядке.
// Historical accepted receipts неизменны. Только explicit rework profile требует exact current
// dependency receipts; legacy сохраняет прежнюю приёмку отдельного шага.
func effectiveServerWorkflowResults(root *serverWorkflowRoot, anchors map[string]serverWorkflowAnchor) map[string]entity.WorkflowAttemptResult {
	effective := make(map[string]entity.WorkflowAttemptResult)
	if root == nil || len(root.Version.Steps) > workflowservice.MaximumSteps || len(anchors) != len(root.Version.Steps) {
		return effective
	}
	for _, step := range root.Version.Steps {
		anchor, found := anchors[step.Key]
		if !found {
			return map[string]entity.WorkflowAttemptResult{}
		}
		receipt, accepted := acceptedServerWorkflowAttempt(anchor, step)
		if root.Version.ReworkPolicy == workflowservice.OwnerApprovedUpstreamV1 && receipt.Attempt < root.ReworkFloors[step.Key] {
			accepted = false
		}
		if !accepted {
			continue
		}
		if root.Version.ReworkPolicy == workflowservice.OwnerApprovedUpstreamV1 {
			if len(anchor.Inputs) != len(step.DependsOn) {
				continue
			}
			inputs := make(map[string]workflowInputReceipt, len(anchor.Inputs))
			valid := true
			for _, input := range anchor.Inputs {
				if _, duplicate := inputs[input.StepKey]; duplicate {
					valid = false
					break
				}
				inputs[input.StepKey] = input
			}
			for _, key := range step.DependsOn {
				input, present := inputs[key]
				current, currentAccepted := effective[key]
				if !present || !currentAccepted || input.SourceNodeID != anchors[key].ID || !bytes.Equal(asJSON(input.Receipt), asJSON(current)) {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
		}
		effective[step.Key] = receipt
	}
	return effective
}

// queryWorkflowReworkFloors reads monotonic minima only from exact approved owner plans.
// queryWorkflowReworkFloors читает монотонные минимумы лишь exact approved owner plans.
//
//go:embed sql/workflow_rework__floors.sql
var queryWorkflowReworkFloors string

// readWorkflowReworkFloorsTx invalidates the complete approved plan before deferred nodes exist.
// It never lowers a floor, creates work or treats an open/unapproved Gate as authority.
// readWorkflowReworkFloorsTx инвалидирует весь approved plan до появления deferred nodes.
// Функция не снижает floor, не создаёт работу и не даёт authority открытому Gate.
func readWorkflowReworkFloorsTx(ctx context.Context, tx queryRunner, current scope, root *serverWorkflowRoot) error {
	var floors map[string]int32
	if tx.QueryRow(ctx, queryWorkflowReworkFloors, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&floors) != nil {
		return errs.ErrUnavailable
	}
	if len(floors) > workflowservice.MaximumSteps {
		return errs.ErrConflict
	}
	for key, floor := range floors {
		found := false
		for _, step := range root.Version.Steps {
			found = found || step.Key == key
		}
		if !found || floor < 1 || floor > 4 {
			return errs.ErrConflict
		}
	}
	root.ReworkFloors = floors
	return nil
}
