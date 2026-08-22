-- name: platform__commands_addsessionturn_select_runs_ref :one
SELECT root_run_id::text FROM control_plane.runs WHERE ref=$1
