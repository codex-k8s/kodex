-- name: workflow_gate_decisions_proof :one
WITH selected AS (SELECT id,project_id FROM control_plane.runs WHERE ref=@root_ref)
SELECT
 EXISTS(SELECT 1 FROM control_plane.run_nodes node,selected root WHERE node.root_run_id=root.id AND node.workflow_step_key='workflow.coordinator.initial' AND node.human_gate_after),
 (SELECT count(*) FROM control_plane.run_nodes node,selected root WHERE node.root_run_id=root.id AND node.workflow_step_key='review' AND node.human_gate_after),
 (SELECT count(*) FROM control_plane.owner_gates gate,selected root WHERE gate.root_run_id=root.id),
 (SELECT count(*) FROM control_plane.run_nodes node,selected root WHERE node.root_run_id=root.id),
 (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.runs run ON run.id=turn.run_id,selected root WHERE run.root_run_id=root.id),
 (SELECT count(*) FROM control_plane.run_events event,selected root WHERE event.root_run_id=root.id),
 (SELECT count(*) FROM control_plane.audit_events audit,selected root WHERE audit.project_id=root.project_id),
 (SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.idempotency_key=@receipt_key),
 (SELECT count(*) FROM control_plane.callback_receipts receipt JOIN control_plane.runs child ON child.id=receipt.child_run_id,selected root WHERE child.root_run_id=root.id)
