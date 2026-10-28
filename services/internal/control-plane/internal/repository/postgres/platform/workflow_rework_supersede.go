package platform

import (
	"context"
	_ "embed"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowReworkSupersededGates locks only obsolete ordinary result Gates in the exact plan.
// queryWorkflowReworkSupersededGates блокирует лишь obsolete ordinary result Gates exact plan.
//
//go:embed sql/workflow_rework__superseded_gates.sql
var queryWorkflowReworkSupersededGates string

// queryWorkflowReworkSupersedeGate preserves a separate immutable system closure kind.
// queryWorkflowReworkSupersedeGate сохраняет отдельный immutable system closure kind.
//
//go:embed sql/workflow_rework__supersede_gate.sql
var queryWorkflowReworkSupersedeGate string

// workflowSupersededGate retains exact locked identities and OCC, never caller-supplied targets.
// workflowSupersededGate хранит exact locked identities/OCC, а не caller-supplied targets.
type workflowSupersededGate struct {
	ID, Ref, NodeID, NodeRef string
	Version                  int64
}

// supersedeWorkflowResultGatesTx verifies fresh resolve authority for every affected Gate before
// any closure. All updates, events and audits share the approval transaction; terminal, foreign,
// integration and delivery Gates never enter this list. SUPERSEDED cannot cancel the root.
// supersedeWorkflowResultGatesTx проверяет fresh resolve authority каждого Gate до закрытия.
// Updates/events/audits входят в approval transaction; terminal/foreign/integration/delivery
// Gates не выбираются. SUPERSEDED не отменяет root.
func (repository *Repository) supersedeWorkflowResultGatesTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, episode workflowExceptionEpisode) error {
	rows, err := tx.Query(ctx, queryWorkflowReworkSupersededGates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "approval_gate_id": episode.GateID, "plan": asJSON(episode.ReworkPlan)})
	if err != nil {
		return errs.ErrUnavailable
	}
	gates := []workflowSupersededGate{}
	for rows.Next() {
		var gate workflowSupersededGate
		if rows.Scan(&gate.ID, &gate.Ref, &gate.Version, &gate.NodeID, &gate.NodeRef) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		gates = append(gates, gate)
		if len(gates) > 12 {
			rows.Close()
			return errs.ErrConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, gate := range gates {
		if err := repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: gate.Ref}); err != nil {
			return err
		}
	}
	for _, gate := range gates {
		tag, err := tx.Exec(ctx, queryWorkflowReworkSupersedeGate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "actor_id": current.actorID, "gate_id": gate.ID, "gate_ref": gate.Ref, "gate_version": gate.Version, "node_id": gate.NodeID})
		if err != nil {
			return errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion, gate.NodeID, "CANCELLED"); err != nil {
			return errs.ErrUnavailable
		}
		auditRef, err := newRef("aud")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, root.ProjectID, current.actorID, "workflow.rework.supersede", "OWNER_GATE", gate.Ref, "i18n:WORKFLOW_GATE_SUPERSEDED", ""); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, gate.Ref, "OWNER_GATE_RESOLVED", gate.NodeRef, "", gate.Ref, "", "i18n:WORKFLOW_GATE_SUPERSEDED", "", ""); err != nil {
			return err
		}
	}
	return nil
}
