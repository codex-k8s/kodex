-- name: interaction_list_sources :many
SELECT
    c.ref,
    max(COALESCE(c.credential_materialization_ref, credential_revision.ref)),
    c.public_configuration->>'base_url',
    c.public_configuration->>'team_name',
    c.public_configuration->>'channel_name',
    min(project.language),
    array_agg(DISTINCT g.capability_key ORDER BY g.capability_key),
    c.version,max(credential_revision.ref),max(credential_revision.revision),
    max(credential_revision.secret_ref),max(credential_revision.secret_uid::text),
    max(credential_revision.secret_resource_version),max(credential_revision.content_sha256),max(credential_revision.created_at)
FROM control_plane.integration_connections c
JOIN control_plane.integration_credential_revisions credential_revision
  ON credential_revision.id = c.credential_revision_id
 AND credential_revision.organization_id=c.organization_id AND credential_revision.connection_id=c.id
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
  AND g.organization_id = c.organization_id
  AND g.definition_version = c.definition_version
  AND g.definition_digest = c.definition_digest
  AND c.lifecycle_state = 'ACTIVE'
  AND project.organization_id = c.organization_id AND project.lifecycle = 'ACTIVE'
  AND g.capability_key IN ('mattermost.inbound', 'mattermost.gate_decisions')
GROUP BY c.id
ORDER BY c.ref
LIMIT 100
