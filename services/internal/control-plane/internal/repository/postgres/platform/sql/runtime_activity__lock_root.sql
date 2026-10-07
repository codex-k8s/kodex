-- name: runtime_activity__lock_root :one
SELECT ref FROM control_plane.runs
WHERE organization_id = @organization_id::uuid AND id = @root_run_id::uuid
FOR UPDATE
