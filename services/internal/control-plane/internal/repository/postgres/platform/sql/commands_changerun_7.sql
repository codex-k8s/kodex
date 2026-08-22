-- name: platform__commands_changerun_7 :one
SELECT id::text,root_run_id::text FROM control_plane.runs WHERE ref=$1
