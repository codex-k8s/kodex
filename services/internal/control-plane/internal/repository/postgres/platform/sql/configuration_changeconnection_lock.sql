-- name: configuration_changeconnection_lock :one
SELECT id::text,
       definition_key,
       lifecycle_state,
       state,
       enabled,
       version,
       definition_version,
       definition_digest
FROM control_plane.integration_connections
WHERE organization_id = @organization_id::uuid
  AND ref = @connection_ref
  AND lifecycle_state = 'ACTIVE'
FOR UPDATE
