-- name: interaction_list_sources :many
SELECT
    c.ref,
    c.credential_materialization_ref,
    c.public_configuration->>'base_url',
    c.public_configuration->>'team_name',
    c.public_configuration->>'channel_name',
    min(project.language),
    array_agg(DISTINCT g.capability_key ORDER BY g.capability_key)
FROM control_plane.integration_connections c
JOIN control_plane.integration_grants g ON g.connection_id = c.id
LEFT JOIN control_plane.agents agent
  ON g.target_kind = 'AGENT'
 AND agent.organization_id = c.organization_id
 AND agent.ref = g.target_ref
LEFT JOIN control_plane.workflows workflow
  ON g.target_kind = 'WORKFLOW'
 AND workflow.organization_id = c.organization_id
 AND workflow.ref = g.target_ref
JOIN control_plane.projects project
  ON project.id = COALESCE(agent.project_id, workflow.project_id)
WHERE c.organization_id = @organization_id::uuid
  AND c.definition_key = 'mattermost'
  AND c.enabled
  AND c.state IN ('CONNECTED', 'DEGRADED')
  AND g.enabled
  AND g.capability_key IN ('mattermost.inbound', 'mattermost.gate_decisions')
GROUP BY c.id
ORDER BY c.ref
LIMIT 100
