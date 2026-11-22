-- name: kubernetes_setup__connection_lock_any :one
SELECT id::text FROM control_plane.integration_connections
WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid FOR UPDATE;
