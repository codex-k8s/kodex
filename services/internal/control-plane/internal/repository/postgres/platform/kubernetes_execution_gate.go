package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesChildGateRead finds the sole durable approval for an immutable child.
// queryKubernetesChildGateRead находит единственное durable approval immutable child.
//
//go:embed sql/kubernetes_execution__gate_read.sql
var queryKubernetesChildGateRead string

// queryKubernetesChildGateInsert binds the existing Gate aggregate to one original child.
// queryKubernetesChildGateInsert связывает existing Gate aggregate с original child.
//
//go:embed sql/kubernetes_execution__gate_insert.sql
var queryKubernetesChildGateInsert string

// queryKubernetesChildGateOrigin resolves server-owned root/project/run lineage before Gate writes.
// queryKubernetesChildGateOrigin разрешает server-owned root/project/run lineage до Gate writes.
//
//go:embed sql/kubernetes_execution__gate_origin.sql
var queryKubernetesChildGateOrigin string

// requireKubernetesChildGateTx records the existing HumanGate graph without issuing attempt credit.
// Its caller commits a newly opened Gate before returning the typed waiting precondition. Approval
// consumes no new lease and cannot authorize another child, plan, invocation or generation.
// requireKubernetesChildGateTx фиксирует existing HumanGate graph без attempt credit.
// Caller commits новый Gate до typed waiting precondition. Approval не создаёт lease и не
// разрешает другой child, plan, invocation либо generation.
func (repository *Repository) requireKubernetesChildGateTx(ctx context.Context, tx pgx.Tx, owner kubernetesOperationOwner, planRef, childID, childRef string) (*platformservice.KubernetesApprovalRequiredError, error) {
	args := pgx.StrictNamedArgs{"organization_id": owner.current.organizationID, "child_id": childID}
	var ref, state string
	err := tx.QueryRow(ctx, queryKubernetesChildGateRead, args).Scan(&ref, &state)
	if err == nil {
		switch state {
		case "APPROVED":
			return nil, nil
		case "OPEN":
			return &platformservice.KubernetesApprovalRequiredError{GateRef: ref, ChildRef: childRef, PlanRef: planRef}, nil
		default:
			return nil, errs.ErrForbidden
		}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrUnavailable
	}
	var rootID, projectID, runID string
	if err := tx.QueryRow(ctx, queryKubernetesChildGateOrigin, pgx.StrictNamedArgs{"organization_id": owner.current.organizationID, "invocation_id": owner.candidate.id}).Scan(&rootID, &projectID, &runID); err != nil {
		return nil, errs.ErrUnavailable
	}
	nodeRef, err := newRef("nod")
	if err != nil {
		return nil, err
	}
	edgeRef, err := newRef("edg")
	if err != nil {
		return nil, err
	}
	ref, err = newRef("gat")
	if err != nil {
		return nil, err
	}
	var nodeID string
	if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertGateNode, nodeRef, owner.current.organizationID, rootID, runID, owner.candidate.nodeID).Scan(&nodeID); err != nil {
		return nil, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationInsertGateEdge, edgeRef, owner.current.organizationID, rootID, owner.candidate.nodeID, nodeID); err != nil {
		return nil, mapWriteError(err)
	}
	var gateID string
	if err := tx.QueryRow(ctx, queryKubernetesChildGateInsert, pgx.StrictNamedArgs{"gate_ref": ref, "organization_id": owner.current.organizationID, "gate_node_id": nodeID, "context_summary": owner.candidate.operation, "invocation_id": owner.candidate.id, "child_id": childID}).Scan(&gateID); err != nil {
		return nil, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman, rootID); err != nil {
		return nil, mapWriteError(err)
	}
	if _, err := repository.emitRunEvent(ctx, tx, owner.current, projectID, rootID, ref, "OWNER_GATE_OPENED", nodeRef, edgeRef, ref, "", "i18n:INTEGRATION_EFFECT_OWNER_DECISION_REQUIRED", "WAITING_HUMAN", "WAITING"); err != nil {
		return nil, err
	}
	return &platformservice.KubernetesApprovalRequiredError{GateRef: ref, ChildRef: childRef, PlanRef: planRef}, nil
}
