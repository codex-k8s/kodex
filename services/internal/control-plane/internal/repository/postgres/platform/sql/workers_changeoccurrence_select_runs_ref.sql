-- name: workers_changeoccurrence_select_runs_ref :one
SELECT id::text FROM control_plane.runs WHERE ref=$1
