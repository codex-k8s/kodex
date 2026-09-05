-- name: interaction_list_inbound_grants :many
SELECT
    g.id::text,
    g.ref,
    g.target_kind,
    g.target_ref,
    p.id::text,
    p.ref
FROM control_plane.integration_connections c
JOIN control_plane.integration_grants g ON g.connection_id = c.id
JOIN LATERAL (
    SELECT a.project_id
    FROM control_plane.agents a
    WHERE g.target_kind = 'AGENT'
      AND a.organization_id = c.organization_id
      AND a.ref = g.target_ref
      AND a.enabled
      AND a.state = 'READY'
      AND EXISTS (SELECT 1 FROM control_plane.instruction_versions instruction
          WHERE instruction.agent_id=a.id AND instruction.state='PUBLISHED')
    UNION ALL
    SELECT w.project_id
    FROM control_plane.workflows w
    WHERE g.target_kind = 'WORKFLOW'
      AND w.organization_id = c.organization_id
      AND w.ref = g.target_ref
      AND w.state = 'PUBLISHED'
) target ON true
JOIN control_plane.projects p ON p.id = target.project_id AND p.lifecycle = 'ACTIVE'
WHERE c.organization_id = @organization_id::uuid
  AND c.ref = @connection_ref
  AND c.definition_key = 'mattermost'
  AND c.enabled
  AND c.state IN ('CONNECTED', 'DEGRADED')
  AND g.enabled
  AND g.capability_key = 'mattermost.inbound'
ORDER BY g.ref
LIMIT 2
FOR UPDATE OF g
