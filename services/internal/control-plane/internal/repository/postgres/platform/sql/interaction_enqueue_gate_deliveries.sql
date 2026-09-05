-- name: interaction_enqueue_gate_deliveries :exec
INSERT INTO control_plane.interaction_deliveries (
    ref,
    organization_id,
    project_id,
    connection_id,
    grant_id,
    root_run_id,
    gate_id,
    capability_key,
    message_key,
    template_data,
    state
)
SELECT
    'idl_' || replace(gen_random_uuid()::text, '-', ''),
    @organization_id::uuid,
    @project_id::uuid,
    integration_grant.connection_id,
    integration_grant.id,
    @root_run_id::uuid,
    @gate_id::uuid,
    integration_grant.capability_key,
    'MATTERMOST_GATE_REQUEST',
    jsonb_build_object(
        'title', gate.title,
        'context', left(gate.context_summary, 2000),
        'gateRef', gate.ref
    ),
    'DUE'
FROM control_plane.owner_gates gate
JOIN control_plane.runs run ON run.id = @root_run_id::uuid
JOIN control_plane.integration_grants integration_grant
  ON integration_grant.organization_id = @organization_id::uuid
 AND integration_grant.target_kind = run.target_type
 AND integration_grant.target_ref = run.target_ref
 AND integration_grant.capability_key = 'mattermost.gate_decisions'
 AND integration_grant.enabled
JOIN control_plane.integration_connections connection
  ON connection.id = integration_grant.connection_id
 AND connection.definition_key = 'mattermost'
 AND connection.enabled
 AND connection.organization_id = integration_grant.organization_id
 AND connection.lifecycle_state = 'ACTIVE'
 AND connection.definition_version = integration_grant.definition_version
 AND connection.definition_digest = integration_grant.definition_digest
 AND connection.state IN ('CONNECTED', 'DEGRADED')
WHERE gate.id = @gate_id::uuid
ON CONFLICT DO NOTHING
