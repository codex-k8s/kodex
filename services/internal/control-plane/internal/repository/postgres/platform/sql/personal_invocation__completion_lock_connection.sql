-- name: personal_invocation__completion_lock_connection :one
SELECT id::text FROM control_plane.integration_connections
WHERE organization_id=@organization_id::uuid AND ref=@connection_ref
FOR UPDATE;
