-- name: interaction_list_sources :many
SELECT
    c.ref,
    c.credential_materialization_ref,
    c.public_configuration->>'base_url',
    c.public_configuration->>'team_name',
    c.public_configuration->>'channel_name',
    o.locale,
    array_agg(DISTINCT g.capability_key ORDER BY g.capability_key)
FROM control_plane.integration_connections c
JOIN control_plane.organizations o ON o.id = c.organization_id
JOIN control_plane.integration_grants g ON g.connection_id = c.id
WHERE c.organization_id = @organization_id::uuid
  AND c.definition_key = 'mattermost'
  AND c.enabled
  AND c.state IN ('CONNECTED', 'DEGRADED')
  AND g.enabled
  AND g.capability_key IN ('mattermost.inbound', 'mattermost.gate_decisions')
GROUP BY c.id, o.locale
ORDER BY c.ref
LIMIT 100
