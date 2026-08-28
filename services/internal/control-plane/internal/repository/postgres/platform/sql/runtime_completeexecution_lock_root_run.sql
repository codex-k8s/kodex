-- name: runtime_completeexecution_lock_root_run :one
SELECT id::text FROM control_plane.runs WHERE id=$1::uuid FOR UPDATE
