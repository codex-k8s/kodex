-- name: commands_changerun_select_root_run_id :one
SELECT root_run_id::text
FROM control_plane.runs
WHERE organization_id=$1::uuid AND ref=$2
