-- name: historical_secret_admission_snapshot :one
SELECT jsonb_build_object(
    'environment', jsonb_build_array(environment.ref, environment.state, environment.version, environment.current_version_id),
    'versions', (SELECT jsonb_agg(jsonb_build_array(version.ref, version.version_number, version.digest, version.secret_descriptors) ORDER BY version.version_number)
                 FROM control_plane.runtime_environment_versions version WHERE version.environment_set_id=environment.id),
    'agent', jsonb_build_array(agent.ref, agent.version),
    'binding', (SELECT jsonb_build_array(binding.ref, binding.version, binding.environment_set_id, binding.environment_version_id, binding.digest)
                FROM control_plane.agent_runtime_environment_bindings binding WHERE binding.agent_id=agent.id),
    'receipts', (SELECT count(*) FROM control_plane.idempotency_receipts),
    'audit', (SELECT count(*) FROM control_plane.audit_events),
    'outbox', (SELECT count(*) FROM control_plane.outbox_events)
)::text
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.agents agent ON agent.ref=$2 AND agent.organization_id=environment.organization_id
WHERE environment.ref=$1;
