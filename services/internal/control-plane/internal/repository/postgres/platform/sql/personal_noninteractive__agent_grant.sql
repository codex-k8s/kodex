-- name: personal_noninteractive__agent_grant :one
SELECT connection.ref, connection.definition_key, connection.definition_version,
       connection.definition_digest, grant_row.capability_key
FROM control_plane.integration_grants grant_row
JOIN control_plane.integration_connections connection
  ON connection.id=grant_row.connection_id
 AND connection.organization_id=grant_row.organization_id
WHERE grant_row.organization_id=@organization_id::uuid
  AND grant_row.ref=@grant_ref
  AND grant_row.target_kind='AGENT'
  AND grant_row.target_ref=@agent_ref
  AND grant_row.definition_version=connection.definition_version
  AND grant_row.definition_digest=connection.definition_digest
  AND connection.lifecycle_state='ACTIVE';
