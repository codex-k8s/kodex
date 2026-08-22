-- name: platform__runtime_completeexecution_select_runs_id :one
SELECT session_id::text,target_type FROM control_plane.runs WHERE id=$1::uuid
