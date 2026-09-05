-- name: interaction_find_gate_delivery :one
SELECT
    d.id::text,
    g.id::text,
    gate.id::text,
    gate.ref,
    gate.version,
    gate.state,
    gate.allowed_decisions,
    p.id::text,
    p.ref,
    r.id::text,
    r.ref
FROM control_plane.interaction_deliveries d
JOIN control_plane.integration_connections c ON c.id = d.connection_id
JOIN control_plane.integration_grants g ON g.id = d.grant_id
JOIN control_plane.owner_gates gate ON gate.id = d.gate_id
JOIN control_plane.projects p ON p.id = d.project_id
JOIN control_plane.runs r ON r.id = d.root_run_id
WHERE d.organization_id = @organization_id::uuid
  AND c.ref = @connection_ref
  AND c.enabled
  AND c.state IN ('CONNECTED', 'DEGRADED')
  AND g.enabled
  AND g.capability_key = 'mattermost.gate_decisions'
  AND d.capability_key = 'mattermost.gate_decisions'
  AND d.state = 'SUCCEEDED'
  AND d.external_team_ref = @external_team_ref AND d.external_channel_ref = @external_channel_ref
  AND d.external_post_ref = COALESCE(NULLIF(@external_root_post_ref, ''), @external_post_ref)
LIMIT 1
FOR UPDATE OF gate
