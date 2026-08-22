-- name: platform__commands_launchrun_update_run_nodes_workflow_step_key_human_gate_after :exec
UPDATE control_plane.run_nodes SET workflow_step_key=$2,human_gate_after=$3 WHERE id=$1::uuid
