-- name: interaction_count_active_adapters :one
SELECT count(DISTINCT connection.id)
FROM control_plane.integration_connections connection
JOIN control_plane.integration_grants integration_grant ON integration_grant.connection_id = connection.id
WHERE connection.organization_id = @organization_id::uuid
  AND connection.definition_key = 'mattermost'
  AND connection.enabled
  AND connection.state IN ('CONNECTED', 'DEGRADED')
  AND integration_grant.enabled
  AND integration_grant.capability_key IN (
      'mattermost.inbound',
      'mattermost.notifications',
      'mattermost.result_mirror',
      'mattermost.gate_decisions'
  )
