-- name: commands_changerun_select_retry_root_node :one
SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS' LIMIT 1
