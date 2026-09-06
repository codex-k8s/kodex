-- name: runtime_catalog__lock_agent :one
SELECT id::text FROM control_plane.agents
WHERE organization_id = $1::uuid AND ref = $2
FOR SHARE;
