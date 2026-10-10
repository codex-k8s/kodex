-- name: runtime_managed_mcp__startup_lock :one
SELECT id::text FROM control_plane.integration_connections
WHERE organization_id=@organization_id::uuid AND ref=@connection_ref
  AND version=@connection_version AND definition_key='context7'
FOR UPDATE;
