-- name: commands_changerun_update_owner_gates_state_decision_decision_comment :many
UPDATE control_plane.owner_gates gate
SET state = 'CANCELLED',
    decision = 'CANCEL',
    decision_comment = 'i18n:RUN_CANCELLED_BY_OWNER',
    resolved_by = $2::uuid,
    resolved_at = clock_timestamp(),
    version = gate.version + 1
WHERE gate.root_run_id = $1::uuid
  AND gate.state = 'OPEN'
RETURNING gate.ref,
          (SELECT node.ref FROM control_plane.run_nodes node WHERE node.id = gate.node_id)
