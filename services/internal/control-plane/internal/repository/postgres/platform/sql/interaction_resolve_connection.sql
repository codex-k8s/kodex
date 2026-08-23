-- name: interaction_resolve_connection :one
SELECT c.id::text
FROM control_plane.integration_connections c
WHERE c.organization_id = @organization_id::uuid
  AND c.ref = @connection_ref
  AND c.definition_key = 'mattermost'
  AND c.enabled
  AND c.state IN ('CONNECTED', 'DEGRADED')
LIMIT 1
