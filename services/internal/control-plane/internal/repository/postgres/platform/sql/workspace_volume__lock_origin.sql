-- name: workspace_volume__lock_origin :one
SELECT r.id::text
FROM control_plane.runs r
WHERE r.id=@root_run_id::uuid AND r.organization_id=@organization_id::uuid
FOR UPDATE OF r SKIP LOCKED;
