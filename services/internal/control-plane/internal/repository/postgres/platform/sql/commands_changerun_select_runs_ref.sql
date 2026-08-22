-- name: platform__commands_changerun_select_runs_ref :one
SELECT id::text,root_run_id::text FROM control_plane.runs WHERE ref=$1
